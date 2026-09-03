package host

import (
	"bytes"
	"crypto/ecdh"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/zuhailz/GoDrop/internal/crypto"
	"github.com/zuhailz/GoDrop/internal/discovery"
	"github.com/zuhailz/GoDrop/internal/protocol"
	"github.com/zuhailz/GoDrop/internal/transfer"
)

const (
	DefaultPort = 7777

	// handshakeTimeout bounds key exchange + peer join on freshly accepted
	// connections so a stalled client cannot hold a goroutine forever.
	handshakeTimeout = 10 * time.Second

	// writeTimeout bounds every individual framed write. Without it, a
	// stalled TCP send buffer blocks the writing goroutine indefinitely.
	writeTimeout = 30 * time.Second
)

// setWriteDeadline frames a single bounded write window on conn.
func setWriteDeadline(conn net.Conn) func() {
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return func() { _ = conn.SetWriteDeadline(time.Time{}) }
}

type Peer struct {
	ID           string
	Name         string
	Conn         net.Conn
	SharedSecret []byte
	writeMu      sync.Mutex
}

func (p *Peer) writeRawMessage(msgType protocol.MessageType, data []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	restore := setWriteDeadline(p.Conn)
	defer restore()
	return protocol.MarshalRaw(p.Conn, msgType, data)
}

type lockedPeerWriter struct {
	peer *Peer
}

func (w *lockedPeerWriter) Write(p []byte) (int, error) {
	w.peer.writeMu.Lock()
	defer w.peer.writeMu.Unlock()
	restore := setWriteDeadline(w.peer.Conn)
	defer restore()
	return w.peer.Conn.Write(p)
}

type PeerEvent struct {
	PeerName string
	Joined   bool
}

type Host struct {
	RoomKey      string // formatted room key (displayed to user)
	roomKeyRaw   []byte // raw room key bytes (used for crypto)
	Name         string
	Port         int
	KeyPair      *crypto.KeyPair
	mu           sync.RWMutex
	peers        map[string]*Peer
	transfers    map[string]*transfer.Transfer
	displayNames map[string]string
	progressCh   chan transfer.PeerProgress
	eventCh      chan PeerEvent
	systemCh     chan protocol.SystemEvent
	advertiser   *discovery.HostAdvertiser
	listener     net.Listener
}

func NewHost(roomKeyRaw []byte, roomKey string, name string) (*Host, error) {
	keyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	return &Host{
		RoomKey:      roomKey,
		roomKeyRaw:   roomKeyRaw,
		Name:         name,
		Port:         DefaultPort,
		KeyPair:      keyPair,
		peers:        make(map[string]*Peer),
		transfers:    make(map[string]*transfer.Transfer),
		displayNames: make(map[string]string),
		progressCh:   make(chan transfer.PeerProgress, 100),
		eventCh:      make(chan PeerEvent, 100),
		systemCh:     make(chan protocol.SystemEvent, 100),
	}, nil
}

func (h *Host) Start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", h.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	h.listener = ln

	h.advertiser = discovery.NewHostAdvertiser(h.roomKeyRaw, h.Port)
	if err := h.advertiser.Start(); err != nil {
		_ = ln.Close()
		return fmt.Errorf("failed to start mDNS advertiser: %w", err)
	}

	go h.acceptLoop()

	return nil
}

func (h *Host) Stop() {
	if h.advertiser != nil {
		h.advertiser.Stop()
	}
	if h.listener != nil {
		_ = h.listener.Close()
	}
	h.mu.Lock()
	for _, peer := range h.peers {
		_ = peer.Conn.Close()
	}
	h.mu.Unlock()
}

func (h *Host) acceptLoop() {
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			return
		}
		go h.handleConn(conn)
	}
}

func (h *Host) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	// Bound the unauthenticated phase: key exchange + peer join must
	// complete quickly or the connection is dropped.
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))

	peerPubKey, err := h.performKeyExchange(conn)
	if err != nil {
		return
	}

	sharedSecret, err := crypto.DeriveSharedSecret(h.KeyPair.PrivateKey, peerPubKey)
	if err != nil {
		return
	}

	// Room key authentication: the receiver must prove knowledge of the
	// room key before it can register as a peer. This prevents unauthorized
	// LAN machines from joining and blocks MITM attackers.
	peerPubKeyBytes := crypto.MarshalPublicKey(peerPubKey)
	if err := h.verifyAuth(conn, sharedSecret, peerPubKeyBytes); err != nil {
		return
	}

	msgType, raw, err := protocol.Unmarshal(conn)
	if err != nil || msgType != protocol.MsgPeerJoin {
		return
	}

	var join protocol.PeerJoin
	if err := json.Unmarshal(raw, &join); err != nil {
		return
	}

	peer := &Peer{
		ID:           join.PeerID,
		Name:         join.PeerName,
		Conn:         conn,
		SharedSecret: sharedSecret,
	}

	h.mu.Lock()
	h.peers[peer.ID] = peer
	h.mu.Unlock()

	select {
	case h.eventCh <- PeerEvent{PeerName: peer.Name, Joined: true}:
	default:
	}
	h.broadcastEvent(peer.Name + " connected")

	// Handshake complete; switch to open-ended deadlines for the transfer
	// phase (chunk streams may legitimately idle between offers).
	_ = conn.SetDeadline(time.Time{})

	h.readLoop(peer)

	h.mu.Lock()
	delete(h.peers, peer.ID)
	h.mu.Unlock()

	select {
	case h.eventCh <- PeerEvent{PeerName: peer.Name, Joined: false}:
	default:
	}
	h.broadcastEvent(peer.Name + " disconnected")
}

// verifyAuth sends a MsgPinChallenge, waits for the receiver's MsgPinResponse,
// and verifies the HMAC tag. The receiver must derive the same authKey from
// the ECDH shared secret + room key, then send its tag. A mismatch means the
// receiver doesn't know the room key or is a MITM relay.
func (h *Host) verifyAuth(conn net.Conn, sharedSecret, peerPubKey []byte) error {
	// Signal the receiver that room key verification is required.
	if err := protocol.Marshal(conn, protocol.MsgPinChallenge, protocol.PinChallenge{}); err != nil {
		return fmt.Errorf("failed to send auth challenge: %w", err)
	}

	// Wait for the receiver's authentication tag.
	msgType, raw, err := protocol.Unmarshal(conn)
	if err != nil {
		return fmt.Errorf("failed to read auth response: %w", err)
	}
	if msgType != protocol.MsgPinResponse {
		return fmt.Errorf("expected auth response, got message type %d", msgType)
	}

	var pinResp protocol.PinResponse
	if err := json.Unmarshal(raw, &pinResp); err != nil {
		return fmt.Errorf("invalid auth response: %w", err)
	}

	// Derive the same auth key the receiver used.
	authKey, err := crypto.DeriveAuthKey(
		sharedSecret,
		crypto.MarshalPublicKey(h.KeyPair.PublicKey),
		peerPubKey,
		h.roomKeyRaw,
	)
	if err != nil {
		return fmt.Errorf("failed to derive auth key: %w", err)
	}

	// Verify: the receiver's tag must match HMAC(authKey, "receiver").
	if !crypto.VerifyAuthTag(authKey, crypto.AuthRoleReceiver, pinResp.Tag) {
		return fmt.Errorf("room key verification failed")
	}

	// Send our own tag so the receiver can verify this host knows the key.
	// Without this, a rogue host on --ip could impersonate the real host.
	hostTag := crypto.ComputeAuthTag(authKey, crypto.AuthRoleHost)
	if err := protocol.Marshal(conn, protocol.MsgPinResponse, protocol.PinResponse{Tag: hostTag}); err != nil {
		return fmt.Errorf("failed to send host auth tag: %w", err)
	}

	return nil
}

func (h *Host) performKeyExchange(conn net.Conn) (*ecdh.PublicKey, error) {
	msgType, raw, err := protocol.Unmarshal(conn)
	if err != nil || msgType != protocol.MsgKeyExchange {
		return nil, fmt.Errorf("expected key exchange message")
	}

	var keyEx protocol.KeyExchange
	if err := json.Unmarshal(raw, &keyEx); err != nil {
		return nil, err
	}

	peerPubKey, err := crypto.UnmarshalPublicKey(keyEx.PublicKey)
	if err != nil {
		return nil, err
	}

	ourPubKey := crypto.MarshalPublicKey(h.KeyPair.PublicKey)
	keyExchange := protocol.KeyExchange{PublicKey: ourPubKey}
	if err := protocol.Marshal(conn, protocol.MsgKeyExchange, keyExchange); err != nil {
		return nil, err
	}

	return peerPubKey, nil
}

func (h *Host) readLoop(peer *Peer) {
	for {
		msgType, raw, err := protocol.Unmarshal(peer.Conn)
		if err != nil {
			return
		}

		switch msgType {
		case protocol.MsgEncryptedPacket:
			// The envelope payload is the raw ciphertext (no JSON wrapper),
			// so raw is decrypted directly.
			decrypted, err := crypto.Decrypt(peer.SharedSecret, raw)
			if err != nil {
				continue
			}
			h.handleDecryptedMessage(peer, decrypted)

		case protocol.MsgFileAccept:
			var accept protocol.FileAccept
			if err := json.Unmarshal(raw, &accept); err != nil {
				continue
			}
			h.handleFileAccept(peer, accept)

		case protocol.MsgFileReject:
			var reject protocol.FileReject
			if err := json.Unmarshal(raw, &reject); err != nil {
				continue
			}
			h.handleFileReject(peer, reject)
		}
	}
}

func (h *Host) handleDecryptedMessage(peer *Peer, data []byte) {
	msgType, payload, err := protocol.Unmarshal(bytes.NewReader(data))
	if err != nil {
		return
	}

	switch msgType {
	case protocol.MsgFileAccept:
		var accept protocol.FileAccept
		if err := json.Unmarshal(payload, &accept); err != nil {
			return
		}
		h.handleFileAccept(peer, accept)

	case protocol.MsgFileReject:
		var reject protocol.FileReject
		if err := json.Unmarshal(payload, &reject); err != nil {
			return
		}
		h.handleFileReject(peer, reject)
	}
}

func (h *Host) handleFileAccept(peer *Peer, accept protocol.FileAccept) {
	h.mu.RLock()
	t, exists := h.transfers[accept.TransferID]
	h.mu.RUnlock()

	if !exists {
		return
	}

	t.SetState(peer.ID, transfer.StateAccepted)
	h.broadcastEvent(peer.Name + " accepted " + h.transferDisplayName(accept.TransferID))

	go func() {
		encryptor := func(data []byte) ([]byte, error) {
			return crypto.Encrypt(peer.SharedSecret, data)
		}
		// SendToPeer already records the terminal state on t, so the
		// error needs no handling here.
		_ = t.SendToPeer(peer.ID, &lockedPeerWriter{peer: peer}, encryptor)
	}()
}

func (h *Host) handleFileReject(peer *Peer, reject protocol.FileReject) {
	h.mu.RLock()
	t, exists := h.transfers[reject.TransferID]
	h.mu.RUnlock()

	if exists {
		t.SetState(peer.ID, transfer.StateRejected)
		h.broadcastEvent(peer.Name + " rejected " + h.transferDisplayName(reject.TransferID))
	}
}

func (h *Host) OfferFile(filePath string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	isDir := info.IsDir()

	transferPath := filePath
	if isDir {
		transferPath, err = createArchive(filePath)
		if err != nil {
			return "", err
		}
	}

	t, err := transfer.NewTransfer(transferPath, h.Name)
	if err != nil {
		return "", err
	}

	offer := t.ToFileOffer()
	if isDir {
		offer.Filename = info.Name()
		offer.Folder = true
	}

	h.mu.Lock()
	h.transfers[t.TransferID] = t
	h.displayNames[t.TransferID] = offer.Filename
	h.mu.Unlock()

	go func() {
		for progress := range t.ProgressChan() {
			select {
			case h.progressCh <- progress:
			default:
			}

			switch progress.State {
			case transfer.StateCompleted, transfer.StateFailed, transfer.StateDisconnected:
				h.broadcastEvent(h.transferResultText(progress, t.TransferID))
			}

			// Every peer is settled, so nothing more can happen to this
			// transfer (accepts require it to still be registered here).
			// Drop it so a long-running host doesn't accumulate entries.
			if transfer.IsTerminal(progress.State) && t.AllPeersSettled() {
				h.mu.Lock()
				delete(h.transfers, t.TransferID)
				delete(h.displayNames, t.TransferID)
				h.mu.Unlock()
				return
			}
		}
	}()

	h.mu.RLock()
	for _, peer := range h.peers {
		encrypted, err := encryptMessage(peer.SharedSecret, protocol.MsgFileOffer, offer)
		if err != nil {
			continue
		}
		if err := peer.writeRawMessage(protocol.MsgEncryptedPacket, encrypted); err != nil {
			t.SetState(peer.ID, transfer.StateDisconnected)
			continue
		}
		t.SetState(peer.ID, transfer.StateWaiting)
	}
	h.mu.RUnlock()

	h.broadcastEvent(h.Name + " offered " + offer.Filename)

	return t.TransferID, nil
}

func encryptMessage(secret []byte, msgType protocol.MessageType, payload any) ([]byte, error) {
	var buf bytes.Buffer
	if err := protocol.Marshal(&buf, msgType, payload); err != nil {
		return nil, err
	}
	return crypto.Encrypt(secret, buf.Bytes())
}

func (h *Host) ProgressChan() <-chan transfer.PeerProgress {
	return h.progressCh
}

func (h *Host) EventChan() <-chan PeerEvent {
	return h.eventCh
}

func (h *Host) SystemEventChan() <-chan protocol.SystemEvent {
	return h.systemCh
}

func (h *Host) broadcastEvent(text string) {
	event := protocol.SystemEvent{Sender: h.Name, Text: text}
	select {
	case h.systemCh <- event:
	default:
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, p := range h.peers {
		encrypted, err := encryptMessage(p.SharedSecret, protocol.MsgSystemEvent, event)
		if err != nil {
			continue
		}
		// Best-effort: a failed timeline broadcast must not disrupt transfers.
		_ = p.writeRawMessage(protocol.MsgEncryptedPacket, encrypted)
	}
}

func (h *Host) transferDisplayName(transferID string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if name, ok := h.displayNames[transferID]; ok {
		return name
	}
	return transferID
}

func (h *Host) transferResultText(progress transfer.PeerProgress, transferID string) string {
	name := h.PeerName(progress.PeerID)
	display := h.transferDisplayName(transferID)
	switch progress.State {
	case transfer.StateCompleted:
		return fmt.Sprintf("%s received %s", name, display)
	case transfer.StateFailed:
		return fmt.Sprintf("%s failed to receive %s", name, display)
	case transfer.StateDisconnected:
		return fmt.Sprintf("%s disconnected while receiving %s", name, display)
	default:
		return ""
	}
}

func (h *Host) Peers() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	peers := make([]string, 0, len(h.peers))
	for _, p := range h.peers {
		peers = append(peers, p.Name)
	}
	sort.Strings(peers)
	return peers
}

func (h *Host) PeerName(peerID string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if p, ok := h.peers[peerID]; ok {
		return p.Name
	}
	return peerID
}
