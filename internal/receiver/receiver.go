package receiver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zuhailz/GoDrop/internal/crypto"
	"github.com/zuhailz/GoDrop/internal/discovery"
	"github.com/zuhailz/GoDrop/internal/protocol"
	"github.com/zuhailz/GoDrop/internal/transfer"
)

type FileOffer struct {
	TransferID string
	Filename   string
	FileSize   int64
	Sender     string
	Folder     bool
}

type Receiver struct {
	PeerID         string
	PeerName       string
	RoomKey        string // formatted room key (for display)
	roomKeyRaw     []byte // raw room key bytes (for crypto)
	KeyPair        *crypto.KeyPair
	SharedSecret   []byte
	conn           net.Conn
	mu             sync.Mutex
	fileOffers     map[string]FileOffer
	fileWriters    map[string]*transfer.FileWriter
	savePaths      map[string]string
	dirTransfers   map[string]string
	progressCh     chan transfer.PeerProgress
	offerCh        chan FileOffer
	systemCh       chan protocol.SystemEvent
	lastHostPubKey []byte // raw bytes of host's ECDH public key
}

func NewReceiver(peerID, peerName string) (*Receiver, error) {
	keyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	return &Receiver{
		PeerID:       peerID,
		PeerName:     peerName,
		KeyPair:      keyPair,
		fileOffers:   make(map[string]FileOffer),
		fileWriters:  make(map[string]*transfer.FileWriter),
		savePaths:    make(map[string]string),
		dirTransfers: make(map[string]string),
		progressCh:   make(chan transfer.PeerProgress, 100),
		offerCh:      make(chan FileOffer, 20),
		systemCh:     make(chan protocol.SystemEvent, 100),
	}, nil
}

func (r *Receiver) ConnectByRoomKey(roomKey string, timeoutSeconds int) error {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	r.RoomKey = roomKey

	roomKeyRaw, err := crypto.ParseRoomKey(roomKey)
	if err != nil {
		return fmt.Errorf("invalid room key: %w", err)
	}
	r.roomKeyRaw = roomKeyRaw

	resolver := discovery.NewHostResolver(roomKeyRaw, time.Duration(timeoutSeconds)*time.Second)
	ip, port, err := resolver.Resolve()
	if err != nil {
		return fmt.Errorf("failed to find host: %w", err)
	}

	return r.Connect(fmt.Sprintf("%s:%d", ip, port))
}

// SetRoomKey sets the raw room key bytes for use with Connect() when
// connecting via direct IP (--ip flag) instead of mDNS discovery.
func (r *Receiver) SetRoomKey(roomKey string) error {
	roomKeyRaw, err := crypto.ParseRoomKey(roomKey)
	if err != nil {
		return err
	}
	r.RoomKey = roomKey
	r.roomKeyRaw = roomKeyRaw
	return nil
}

const (
	dialTimeout      = 10 * time.Second
	handshakeTimeout = 10 * time.Second
	writeTimeout     = 30 * time.Second
)

func (r *Receiver) Connect(address string) error {
	conn, err := net.DialTimeout("tcp", address, dialTimeout)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	r.conn = conn

	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))

	if err := r.performKeyExchange(); err != nil {
		_ = conn.Close()
		return fmt.Errorf("key exchange failed: %w", err)
	}

	if err := r.handleAuthChallenge(); err != nil {
		_ = conn.Close()
		return fmt.Errorf("room key verification failed: %w", err)
	}

	if err := r.sendPeerJoin(); err != nil {
		_ = conn.Close()
		return fmt.Errorf("peer join failed: %w", err)
	}

	_ = conn.SetDeadline(time.Time{})

	go r.readLoop()

	return nil
}

func (r *Receiver) performKeyExchange() error {
	ourPubKey := crypto.MarshalPublicKey(r.KeyPair.PublicKey)
	keyExchange := protocol.KeyExchange{PublicKey: ourPubKey}
	if err := protocol.Marshal(r.conn, protocol.MsgKeyExchange, keyExchange); err != nil {
		return err
	}

	msgType, raw, err := protocol.Unmarshal(r.conn)
	if err != nil {
		return err
	}
	if msgType != protocol.MsgKeyExchange {
		return fmt.Errorf("expected key exchange response")
	}

	var peerKeyEx protocol.KeyExchange
	if err := json.Unmarshal(raw, &peerKeyEx); err != nil {
		return err
	}

	peerPubKey, err := crypto.UnmarshalPublicKey(peerKeyEx.PublicKey)
	if err != nil {
		return err
	}

	sharedSecret, err := crypto.DeriveSharedSecret(r.KeyPair.PrivateKey, peerPubKey)
	if err != nil {
		return err
	}
	r.SharedSecret = sharedSecret

	// Store the raw host public key bytes for auth key derivation.
	r.lastHostPubKey = peerKeyEx.PublicKey

	return nil
}

// handleAuthChallenge reads the host's MsgPinChallenge and performs mutual
// room key authentication. The receiver proves knowledge of the room key
// by sending an HMAC tag, then verifies the host knows it too.
func (r *Receiver) handleAuthChallenge() error {
	msgType, raw, err := protocol.Unmarshal(r.conn)
	if err != nil {
		return fmt.Errorf("failed to read auth challenge: %w", err)
	}

	if msgType != protocol.MsgPinChallenge {
		return fmt.Errorf("expected auth challenge, got message type %d", msgType)
	}

	var challenge protocol.PinChallenge
	if err := json.Unmarshal(raw, &challenge); err != nil {
		return fmt.Errorf("invalid auth challenge: %w", err)
	}

	if len(r.roomKeyRaw) == 0 {
		return fmt.Errorf("room key required for authentication")
	}

	// Derive the auth key using the same parameters the host uses.
	authKey, err := crypto.DeriveAuthKey(
		r.SharedSecret,
		r.lastHostPubKey,
		crypto.MarshalPublicKey(r.KeyPair.PublicKey),
		r.roomKeyRaw,
	)
	if err != nil {
		return fmt.Errorf("failed to derive auth key: %w", err)
	}

	tag := crypto.ComputeAuthTag(authKey, crypto.AuthRoleReceiver)

	resp := protocol.PinResponse{Tag: tag}
	if err := r.writeMessage(protocol.MsgPinResponse, resp); err != nil {
		return err
	}

	// Wait for the host's auth tag so we can verify it knows the room key.
	// Without this, a rogue host on --ip could impersonate the real host.
	msgType2, raw2, err := protocol.Unmarshal(r.conn)
	if err != nil || msgType2 != protocol.MsgPinResponse {
		return fmt.Errorf("expected host auth response, got type %d", msgType2)
	}

	var hostResp protocol.PinResponse
	if err := json.Unmarshal(raw2, &hostResp); err != nil {
		return fmt.Errorf("invalid host auth response: %w", err)
	}

	if !crypto.VerifyAuthTag(authKey, crypto.AuthRoleHost, hostResp.Tag) {
		return fmt.Errorf("host failed room key verification (wrong room?)")
	}

	return nil
}

func (r *Receiver) sendPeerJoin() error {
	join := protocol.PeerJoin{
		PeerID:   r.PeerID,
		PeerName: r.PeerName,
	}
	return r.writeMessage(protocol.MsgPeerJoin, join)
}

func (r *Receiver) writeMessage(msgType protocol.MessageType, payload any) error {
	_ = r.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	err := protocol.Marshal(r.conn, msgType, payload)
	_ = r.conn.SetWriteDeadline(time.Time{})
	return err
}

func (r *Receiver) readLoop() {
	for {
		msgType, raw, err := protocol.Unmarshal(r.conn)
		if err != nil {
			close(r.progressCh)
			return
		}

		switch msgType {
		case protocol.MsgEncryptedPacket:
			var encPacket protocol.EncryptedPacket
			if err := json.Unmarshal(raw, &encPacket); err != nil {
				continue
			}
			decrypted, err := crypto.Decrypt(r.SharedSecret, encPacket.Data)
			if err != nil {
				continue
			}
			r.handleDecryptedMessage(decrypted)
		}
	}
}

func (r *Receiver) handleDecryptedMessage(data []byte) {
	msgType, payload, err := protocol.Unmarshal(bytes.NewReader(data))
	if err != nil {
		return
	}

	switch msgType {
	case protocol.MsgFileOffer:
		var offer protocol.FileOffer
		if err := json.Unmarshal(payload, &offer); err != nil {
			return
		}
		r.handleFileOffer(offer)

	case protocol.MsgChunk:
		var chunk protocol.Chunk
		if err := json.Unmarshal(payload, &chunk); err != nil {
			return
		}
		r.handleChunk(chunk)

	case protocol.MsgSystemEvent:
		var event protocol.SystemEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return
		}
		select {
		case r.systemCh <- event:
		default:
		}
	}
}

func (r *Receiver) handleFileOffer(offer protocol.FileOffer) {
	clean := sanitizeFilename(offer.Filename)
	if clean == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	fileOffer := FileOffer{
		TransferID: offer.TransferID,
		Filename:   clean,
		FileSize:   offer.FileSize,
		Sender:     offer.Sender,
		Folder:     offer.Folder,
	}

	r.fileOffers[offer.TransferID] = fileOffer
	select {
	case r.offerCh <- fileOffer:
	default:
	}
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	// Reject Windows path syntax before any platform-dependent processing.
	// filepath.Base treats "\\" as a separator only on Windows, which would
	// make this function silently strip traversal (e.g. "..\\..\\x" -> "x")
	// on Windows while rejecting it elsewhere. A filename containing a
	// backslash or NUL is never legitimate, on any OS.
	if strings.ContainsRune(name, '\\') || strings.ContainsRune(name, 0) {
		return ""
	}
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if strings.ContainsRune(name, '/') {
		return ""
	}
	return name
}

func (r *Receiver) OfferChan() <-chan FileOffer {
	return r.offerCh
}

func (r *Receiver) handleChunk(chunk protocol.Chunk) {
	r.mu.Lock()
	fw, exists := r.fileWriters[chunk.TransferID]
	r.mu.Unlock()

	if !exists {
		return
	}

	if err := fw.WriteChunk(chunk); err != nil {
		r.forgetTransfer(chunk.TransferID)
		_ = fw.Close()
		r.progressCh <- transfer.PeerProgress{
			PeerID:     r.PeerID,
			TransferID: chunk.TransferID,
			State:      transfer.StateFailed,
		}
		return
	}

	offset, total := fw.Progress()
	r.progressCh <- transfer.PeerProgress{
		PeerID:     r.PeerID,
		TransferID: chunk.TransferID,
		Offset:     offset,
		Total:      total,
		State:      transfer.StateInProgress,
	}

	if offset >= total {
		r.finishTransfer(chunk.TransferID, fw, offset, total)
	}
}

func (r *Receiver) forgetTransfer(transferID string) (savePath, folderName string) {
	r.mu.Lock()
	delete(r.fileWriters, transferID)
	savePath = r.savePaths[transferID]
	delete(r.savePaths, transferID)
	folderName = r.dirTransfers[transferID]
	delete(r.dirTransfers, transferID)
	r.mu.Unlock()
	return savePath, folderName
}

func (r *Receiver) finishTransfer(transferID string, fw *transfer.FileWriter, offset, total int64) {
	savePath, folderName := r.forgetTransfer(transferID)

	fail := func() {
		r.progressCh <- transfer.PeerProgress{
			PeerID:     r.PeerID,
			TransferID: transferID,
			Offset:     offset,
			Total:      total,
			State:      transfer.StateFailed,
		}
	}

	if err := fw.Finish(savePath); err != nil {
		fail()
		return
	}

	if folderName != "" {
		if err := extractFolder(savePath, filepath.Dir(savePath), folderName); err != nil {
			fail()
			return
		}
		_ = os.Remove(savePath)
	}

	r.progressCh <- transfer.PeerProgress{
		PeerID:     r.PeerID,
		TransferID: transferID,
		Offset:     offset,
		Total:      total,
		State:      transfer.StateCompleted,
	}
}

func (r *Receiver) AcceptTransfer(transferID, savePath string) (string, bool, error) {
	r.mu.Lock()
	offer, exists := r.fileOffers[transferID]
	if !exists {
		r.mu.Unlock()
		return "", false, fmt.Errorf("transfer %s not found", transferID)
	}
	delete(r.fileOffers, transferID)

	var artifactPath string
	renamed := false

	if offer.Folder {
		finalFolder := uniquePath(filepath.Join(filepath.Dir(savePath), offer.Filename))
		renamed = filepath.Base(finalFolder) != offer.Filename
		artifactPath = uniquePath(filepath.Join(filepath.Dir(savePath), filepath.Base(finalFolder)+".zip"))
		r.dirTransfers[transferID] = filepath.Base(finalFolder)
	} else {
		artifactPath = uniquePath(savePath)
		renamed = artifactPath != savePath
	}

	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		r.mu.Unlock()
		return "", false, fmt.Errorf("failed to create save directory: %w", err)
	}

	tempPath := artifactPath + ".part"
	fw, err := transfer.NewFileWriter(tempPath, offer.FileSize)
	if err != nil {
		r.mu.Unlock()
		return "", false, fmt.Errorf("failed to create save file: %w", err)
	}
	r.fileWriters[transferID] = fw
	r.savePaths[transferID] = artifactPath
	r.mu.Unlock()

	accept := protocol.FileAccept{TransferID: transferID}
	encrypted, err := encryptMessage(r.SharedSecret, protocol.MsgFileAccept, accept)
	if err != nil {
		_ = fw.Close()
		r.forgetTransfer(transferID)
		return "", false, err
	}

	encPacket := protocol.EncryptedPacket{Data: encrypted}
	if err := r.writeMessage(protocol.MsgEncryptedPacket, encPacket); err != nil {
		_ = fw.Close()
		r.forgetTransfer(transferID)
		return "", false, err
	}

	if offer.FileSize == 0 {
		r.finishTransfer(transferID, fw, 0, 0)
	}

	return artifactPath, renamed, nil
}

func uniquePath(savePath string) string {
	if _, err := os.Stat(savePath); os.IsNotExist(err) {
		return savePath
	}

	dir := filepath.Dir(savePath)
	ext := filepath.Ext(savePath)
	base := strings.TrimSuffix(filepath.Base(savePath), ext)

	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (r *Receiver) RejectTransfer(transferID string) error {
	r.mu.Lock()
	delete(r.fileOffers, transferID)
	r.mu.Unlock()

	reject := protocol.FileReject{TransferID: transferID}
	encrypted, err := encryptMessage(r.SharedSecret, protocol.MsgFileReject, reject)
	if err != nil {
		return err
	}

	encPacket := protocol.EncryptedPacket{Data: encrypted}
	return r.writeMessage(protocol.MsgEncryptedPacket, encPacket)
}

func (r *Receiver) PendingOffers() []FileOffer {
	r.mu.Lock()
	defer r.mu.Unlock()

	offers := make([]FileOffer, 0, len(r.fileOffers))
	for _, offer := range r.fileOffers {
		offers = append(offers, offer)
	}
	return offers
}

func (r *Receiver) ProgressChan() <-chan transfer.PeerProgress {
	return r.progressCh
}

func (r *Receiver) SystemEventChan() <-chan protocol.SystemEvent {
	return r.systemCh
}

func (r *Receiver) IsDirTransfer(transferID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name, ok := r.dirTransfers[transferID]
	return name, ok
}

func (r *Receiver) Close() error {
	r.mu.Lock()
	for _, fw := range r.fileWriters {
		_ = fw.Close()
	}
	r.fileWriters = make(map[string]*transfer.FileWriter)
	r.savePaths = make(map[string]string)
	r.dirTransfers = make(map[string]string)
	r.mu.Unlock()

	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

func encryptMessage(secret []byte, msgType protocol.MessageType, payload any) ([]byte, error) {
	var buf bytes.Buffer
	if err := protocol.Marshal(&buf, msgType, payload); err != nil {
		return nil, err
	}
	return crypto.Encrypt(secret, buf.Bytes())
}
