package transfer

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"

	"github.com/zuhailz/GoDrop/internal/protocol"
)

type TransferState int

const (
	StateWaiting TransferState = iota
	StateAccepted
	StateRejected
	StateInProgress
	StateCompleted
	StateFailed
	StateDisconnected
)

func (s TransferState) String() string {
	switch s {
	case StateWaiting:
		return "WAITING"
	case StateAccepted:
		return "ACCEPTED"
	case StateRejected:
		return "REJECTED"
	case StateInProgress:
		return "IN_PROGRESS"
	case StateCompleted:
		return "COMPLETED"
	case StateFailed:
		return "FAILED"
	case StateDisconnected:
		return "DISCONNECTED"
	default:
		return "UNKNOWN"
	}
}

type Transfer struct {
	TransferID string
	Filename   string
	FilePath   string
	FileSize   int64
	Sender     string

	mu         sync.RWMutex
	peerStates map[string]TransferState
	peerOffset map[string]int64
	progressCh chan PeerProgress
}

type PeerProgress struct {
	PeerID     string
	TransferID string
	Offset     int64
	Total      int64
	State      TransferState
}

func NewTransfer(filePath, sender string) (*Transfer, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	transferID, err := generateTransferID()
	if err != nil {
		return nil, err
	}

	return &Transfer{
		TransferID: transferID,
		Filename:   info.Name(),
		FilePath:   filePath,
		FileSize:   info.Size(),
		Sender:     sender,
		peerStates: make(map[string]TransferState),
		peerOffset: make(map[string]int64),
		progressCh: make(chan PeerProgress, 100),
	}, nil
}

// IsTerminal reports whether s is an end state for a peer: no further
// transitions are expected from it.
func IsTerminal(s TransferState) bool {
	switch s {
	case StateRejected, StateCompleted, StateFailed, StateDisconnected:
		return true
	default:
		return false
	}
}

// AllPeersSettled reports whether every tracked peer has reached a terminal
// state. Always false when no peer has been offered the transfer yet.
func (t *Transfer) AllPeersSettled() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.peerStates) == 0 {
		return false
	}
	for _, s := range t.peerStates {
		if !IsTerminal(s) {
			return false
		}
	}
	return true
}

func (t *Transfer) GetState(peerID string) TransferState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.peerStates[peerID]
}

func (t *Transfer) SetState(peerID string, state TransferState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peerStates[peerID] = state
	t.progressCh <- PeerProgress{
		PeerID:     peerID,
		TransferID: t.TransferID,
		Offset:     t.peerOffset[peerID],
		Total:      t.FileSize,
		State:      state,
	}
}

func (t *Transfer) GetOffset(peerID string) int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.peerOffset[peerID]
}

func (t *Transfer) UpdateOffset(peerID string, offset int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peerOffset[peerID] = offset
}

func (t *Transfer) ProgressChan() <-chan PeerProgress {
	return t.progressCh
}

func (t *Transfer) ReadChunks() (<-chan protocol.Chunk, <-chan error) {
	chunkCh := make(chan protocol.Chunk, 10)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		file, err := os.Open(t.FilePath)
		if err != nil {
			errCh <- fmt.Errorf("failed to open file: %w", err)
			return
		}
		defer func() { _ = file.Close() }()

		buffer := make([]byte, protocol.ChunkSize)
		offset := int64(0)

		for {
			n, err := file.Read(buffer)
			if err != nil && err != io.EOF {
				errCh <- fmt.Errorf("failed to read file: %w", err)
				return
			}

			if n > 0 {
				data := make([]byte, n)
				copy(data, buffer[:n])

				chunk := protocol.Chunk{
					TransferID: t.TransferID,
					Offset:     offset,
					Data:       data,
					Checksum:   crc32.ChecksumIEEE(data),
				}

				chunkCh <- chunk
				offset += int64(n)
			}

			if err == io.EOF {
				break
			}
		}
	}()

	return chunkCh, errCh
}

func (t *Transfer) SendToPeer(peerID string, writer io.Writer, encryptor func([]byte) ([]byte, error)) error {
	t.SetState(peerID, StateInProgress)

	// Coalesce the type/length/payload writes for each frame into a single
	// underlying Write instead of three syscalls per chunk.
	bw := bufio.NewWriterSize(writer, 256*1024)

	chunkCh, errCh := t.ReadChunks()

	for {
		select {
		case chunk, ok := <-chunkCh:
			if !ok {
				if err := bw.Flush(); err != nil {
					t.SetState(peerID, StateFailed)
					return err
				}
				t.SetState(peerID, StateCompleted)
				return nil
			}

			chunkData, err := encodeChunk(chunk)
			if err != nil {
				t.SetState(peerID, StateFailed)
				return err
			}

			encrypted, err := encryptor(chunkData)
			if err != nil {
				t.SetState(peerID, StateFailed)
				return err
			}

			if err := protocol.MarshalRaw(bw, protocol.MsgEncryptedPacket, encrypted); err != nil {
				t.SetState(peerID, StateFailed)
				return err
			}

			t.UpdateOffset(peerID, chunk.Offset+int64(len(chunk.Data)))

			// Progress is a best-effort signal for the UI: never let a
			// slow consumer stall the transfer hot loop.
			select {
			case t.progressCh <- PeerProgress{
				PeerID:     peerID,
				TransferID: t.TransferID,
				Offset:     chunk.Offset + int64(len(chunk.Data)),
				Total:      t.FileSize,
				State:      StateInProgress,
			}:
			default:
			}

		case err := <-errCh:
			if err != nil {
				t.SetState(peerID, StateFailed)
				return err
			}
		}
	}
}

func (t *Transfer) ToFileOffer() protocol.FileOffer {
	return protocol.FileOffer{
		TransferID: t.TransferID,
		Filename:   t.Filename,
		FileSize:   t.FileSize,
		Sender:     t.Sender,
	}
}

func generateTransferID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func encodeChunk(chunk protocol.Chunk) ([]byte, error) {
	blob, err := marshalChunkBody(chunk)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := protocol.MarshalRaw(&buf, protocol.MsgChunk, blob); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func marshalChunkBody(chunk protocol.Chunk) ([]byte, error) {
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.BigEndian, byte(len(chunk.TransferID))); err != nil {
		return nil, err
	}
	buf.WriteString(chunk.TransferID)
	if err := binary.Write(&buf, binary.BigEndian, chunk.Offset); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(chunk.Data))); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.BigEndian, chunk.Checksum); err != nil {
		return nil, err
	}
	buf.Write(chunk.Data)
	return buf.Bytes(), nil
}

// DecodeChunkBlob decodes the binary form written by marshalChunkBody.
func DecodeChunkBlob(blob []byte) (protocol.Chunk, error) {
	var chunk protocol.Chunk
	r := bytes.NewReader(blob)

	var idLen byte
	if err := binary.Read(r, binary.BigEndian, &idLen); err != nil {
		return chunk, err
	}
	id := make([]byte, idLen)
	if _, err := io.ReadFull(r, id); err != nil {
		return chunk, err
	}
	chunk.TransferID = string(id)

	if err := binary.Read(r, binary.BigEndian, &chunk.Offset); err != nil {
		return chunk, err
	}
	var dataLen uint32
	if err := binary.Read(r, binary.BigEndian, &dataLen); err != nil {
		return chunk, err
	}
	if err := binary.Read(r, binary.BigEndian, &chunk.Checksum); err != nil {
		return chunk, err
	}
	chunk.Data = make([]byte, dataLen)
	if _, err := io.ReadFull(r, chunk.Data); err != nil {
		return chunk, err
	}
	return chunk, nil
}

func WriteChunkToFile(chunk protocol.Chunk, file *os.File) error {
	if _, err := file.Seek(chunk.Offset, io.SeekStart); err != nil {
		return err
	}

	checksum := crc32.ChecksumIEEE(chunk.Data)
	if checksum != chunk.Checksum {
		return fmt.Errorf("checksum mismatch: expected %d, got %d", chunk.Checksum, checksum)
	}

	_, err := file.Write(chunk.Data)
	return err
}

type FileWriter struct {
	file     *os.File
	filePath string
	fileSize int64
	offset   int64
	mu       sync.Mutex
}

func NewFileWriter(filePath string, fileSize int64) (*FileWriter, error) {
	file, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	return &FileWriter{
		file:     file,
		filePath: filePath,
		fileSize: fileSize,
	}, nil
}

func (fw *FileWriter) WriteChunk(chunk protocol.Chunk) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	// Clamp the write window to the advertised file size so a bad
	// sender can't seek past the end of the file.
	if chunk.Offset < 0 || chunk.Offset >= fw.fileSize || chunk.Offset+int64(len(chunk.Data)) > fw.fileSize {
		return fmt.Errorf("chunk offset %d (len %d) out of range for file size %d", chunk.Offset, len(chunk.Data), fw.fileSize)
	}

	checksum := crc32.ChecksumIEEE(chunk.Data)
	if checksum != chunk.Checksum {
		return fmt.Errorf("checksum mismatch")
	}

	if _, err := fw.file.Seek(chunk.Offset, io.SeekStart); err != nil {
		return err
	}

	if _, err := fw.file.Write(chunk.Data); err != nil {
		return err
	}

	fw.offset = chunk.Offset + int64(len(chunk.Data))
	return nil
}

func (fw *FileWriter) Close() error {
	return fw.file.Close()
}

func (fw *FileWriter) Finish(destPath string) error {
	if err := fw.file.Close(); err != nil {
		return err
	}
	if destPath != "" && destPath != fw.filePath {
		return os.Rename(fw.filePath, destPath)
	}
	return nil
}

func (fw *FileWriter) Progress() (int64, int64) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return fw.offset, fw.fileSize
}
