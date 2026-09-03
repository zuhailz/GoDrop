package transfer

import (
	"bytes"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/zuhailz/GoDrop/internal/protocol"
)

func TestNewTransfer(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testData := []byte("test file content for transfer")
	if _, err := tmpFile.Write(testData); err != nil {
		t.Fatalf("failed to write test data: %v", err)
	}
	_ = tmpFile.Close()

	transfer, err := NewTransfer(tmpFile.Name(), "test-sender")
	if err != nil {
		t.Fatalf("NewTransfer failed: %v", err)
	}

	if transfer.TransferID == "" {
		t.Error("TransferID should not be empty")
	}

	if transfer.Filename == "" {
		t.Error("Filename should not be empty")
	}

	if transfer.FileSize != int64(len(testData)) {
		t.Errorf("FileSize = %d, want %d", transfer.FileSize, len(testData))
	}

	if transfer.Sender != "test-sender" {
		t.Errorf("Sender = %s, want test-sender", transfer.Sender)
	}
}

func TestTransferState(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_ = tmpFile.Close()

	transfer, _ := NewTransfer(tmpFile.Name(), "sender")

	peerID := "peer-123"

	if state := transfer.GetState(peerID); state != StateWaiting {
		t.Errorf("initial state = %v, want StateWaiting", state)
	}

	transfer.SetState(peerID, StateAccepted)
	if state := transfer.GetState(peerID); state != StateAccepted {
		t.Errorf("state = %v, want StateAccepted", state)
	}

	transfer.SetState(peerID, StateInProgress)
	if state := transfer.GetState(peerID); state != StateInProgress {
		t.Errorf("state = %v, want StateInProgress", state)
	}
}

func TestTransferOffset(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_ = tmpFile.Close()

	transfer, _ := NewTransfer(tmpFile.Name(), "sender")

	peerID := "peer-456"

	if offset := transfer.GetOffset(peerID); offset != 0 {
		t.Errorf("initial offset = %d, want 0", offset)
	}

	transfer.UpdateOffset(peerID, 1024)
	if offset := transfer.GetOffset(peerID); offset != 1024 {
		t.Errorf("offset = %d, want 1024", offset)
	}
}

func TestReadChunks(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testData := make([]byte, protocol.ChunkSize+100)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	if _, err := tmpFile.Write(testData); err != nil {
		t.Fatalf("failed to write test data: %v", err)
	}
	_ = tmpFile.Close()

	transfer, _ := NewTransfer(tmpFile.Name(), "sender")

	chunkCh, errCh := transfer.ReadChunks()

	var chunks []protocol.Chunk

	for chunk := range chunkCh {
		chunks = append(chunks, chunk)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("ReadChunks error: %v", err)
	}

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	var receivedData []byte
	for _, c := range chunks {
		receivedData = append(receivedData, c.Data...)
	}

	if !bytes.Equal(receivedData, testData) {
		t.Error("received data doesn't match original")
	}
}

func TestWriteChunkToFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_ = tmpFile.Close()

	file, err := os.OpenFile(tmpFile.Name(), os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer func() { _ = file.Close() }()

	testData := []byte("test chunk data")

	chunk := protocol.Chunk{
		TransferID: "test-transfer",
		Offset:     0,
		Data:       testData,
		Checksum:   crc32.ChecksumIEEE(testData),
	}

	if err := WriteChunkToFile(chunk, file); err != nil {
		t.Fatalf("WriteChunkToFile failed: %v", err)
	}
}

func TestFileWriter(t *testing.T) {
	tmpDir := os.TempDir()

	filePath := filepath.Join(tmpDir, "test-output.bin")
	defer func() { _ = os.Remove(filePath) }()

	fw, err := NewFileWriter(filePath, 1024)
	if err != nil {
		t.Fatalf("NewFileWriter failed: %v", err)
	}
	defer func() { _ = fw.Close() }()

	testData := []byte("chunk data here")

	chunk := protocol.Chunk{
		TransferID: "tx-1",
		Offset:     0,
		Data:       testData,
		Checksum:   crc32.ChecksumIEEE(testData),
	}

	if err := fw.WriteChunk(chunk); err != nil {
		t.Fatalf("WriteChunk failed: %v", err)
	}

	offset, total := fw.Progress()
	if offset != int64(len(testData)) {
		t.Errorf("offset = %d, want %d", offset, len(testData))
	}
	if total != 1024 {
		t.Errorf("total = %d, want 1024", total)
	}
}

func TestFileWriterRejectsOutOfBoundsChunks(t *testing.T) {
	filePath := filepath.Join(os.TempDir(), "test-oob.bin")
	defer func() { _ = os.Remove(filePath) }()

	fw, err := NewFileWriter(filePath, 16)
	if err != nil {
		t.Fatalf("NewFileWriter failed: %v", err)
	}
	defer func() { _ = fw.Close() }()

	data := []byte("0123456789abcdef")
	chunk := protocol.Chunk{
		TransferID: "tx-oob",
		Data:       data,
		Checksum:   crc32.ChecksumIEEE(data),
	}

	// Offset beyond EOF must be rejected even with a valid checksum.
	chunk.Offset = 32
	if err := fw.WriteChunk(chunk); err == nil {
		t.Error("WriteChunk with out-of-range offset should fail")
	}

	// Chunk extending past the advertised size must be rejected too.
	chunk.Offset = 8 // 8 + len(data)=16 > 16
	if err := fw.WriteChunk(chunk); err == nil {
		t.Error("WriteChunk overflowing file size should fail")
	}

	// Negative offsets must be rejected.
	chunk.Offset = -1
	if err := fw.WriteChunk(chunk); err == nil {
		t.Error("WriteChunk with negative offset should fail")
	}

	// A valid chunk still lands correctly.
	valid := data[:8]
	chunk.Offset = 0
	chunk.Data = valid
	chunk.Checksum = crc32.ChecksumIEEE(valid)
	if err := fw.WriteChunk(chunk); err != nil {
		t.Fatalf("valid WriteChunk failed: %v", err)
	}
}

func TestGenerateTransferID(t *testing.T) {
	id1, err := generateTransferID()
	if err != nil {
		t.Fatalf("generateTransferID failed: %v", err)
	}

	id2, err := generateTransferID()
	if err != nil {
		t.Fatalf("generateTransferID failed: %v", err)
	}

	if id1 == id2 {
		t.Error("transfer IDs should be unique")
	}

	if len(id1) != 16 {
		t.Errorf("transfer ID length = %d, want 16", len(id1))
	}
}

func TestBinaryChunkCodecRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		chunk  protocol.Chunk
	}{
		{
			name: "small",
			chunk: protocol.Chunk{
				TransferID: "abcdef0123456789",
				Offset:     0,
				Data:       []byte("hello world"),
				Checksum:   crc32.ChecksumIEEE([]byte("hello world")),
			},
		},
		{
			name: "large_offset",
			chunk: protocol.Chunk{
				TransferID: "aabbccdd11223344",
				Offset:     123456789,
				Data:       make([]byte, protocol.ChunkSize),
				Checksum:   0xDEADBEEF,
			},
		},
		{
			name: "max_values",
			chunk: protocol.Chunk{
				TransferID: "FFFFFFFFFFFFFFFF",
				Offset:     1<<63 - 1,
				Data:       make([]byte, 1),
				Checksum:   0xFFFFFFFF,
			},
		},
		{
			name: "empty_data",
			chunk: protocol.Chunk{
				TransferID: "0000000000000000",
				Offset:     42,
				Data:       []byte{},
				Checksum:   12345,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blob, err := marshalChunkBody(tc.chunk)
			if err != nil {
				t.Fatalf("marshalChunkBody failed: %v", err)
			}
			got, err := DecodeChunkBlob(blob)
			if err != nil {
				t.Fatalf("DecodeChunkBlob failed: %v", err)
			}
			if got.TransferID != tc.chunk.TransferID {
				t.Errorf("TransferID = %q, want %q", got.TransferID, tc.chunk.TransferID)
			}
			if got.Offset != tc.chunk.Offset {
				t.Errorf("Offset = %d, want %d", got.Offset, tc.chunk.Offset)
			}
			if got.Checksum != tc.chunk.Checksum {
				t.Errorf("Checksum = %d, want %d", got.Checksum, tc.chunk.Checksum)
			}
			if !bytes.Equal(got.Data, tc.chunk.Data) {
				t.Errorf("Data length = %d, want %d", len(got.Data), len(tc.chunk.Data))
			}
		})
	}
}
