package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		msgType MessageType
		payload any
	}{
		{
			name:    "KeyExchange",
			msgType: MsgKeyExchange,
			payload: KeyExchange{PublicKey: []byte("test-public-key")},
		},
		{
			name:    "PeerJoin",
			msgType: MsgPeerJoin,
			payload: PeerJoin{PeerID: "peer-123", PeerName: "suhail"},
		},
		{
			name:    "FileOffer",
			msgType: MsgFileOffer,
			payload: FileOffer{TransferID: "tx-1", Filename: "test.txt", FileSize: 1024, Sender: "host"},
		},
		{
			name:    "Chunk",
			msgType: MsgChunk,
			payload: Chunk{TransferID: "tx-1", Offset: 0, Data: []byte("test data"), Checksum: 12345},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := Marshal(&buf, tt.msgType, tt.payload)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			msgType, data, err := Unmarshal(&buf)
			if err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if msgType != tt.msgType {
				t.Errorf("MessageType = %d, want %d", msgType, tt.msgType)
			}

			if len(data) == 0 {
				t.Error("Data should not be empty")
			}
		})
	}
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	original := FileOffer{
		TransferID: "tx-abc123",
		Filename:   "document.pdf",
		FileSize:   2048576,
		Sender:     "suhail",
	}

	var buf bytes.Buffer
	err := Marshal(&buf, MsgFileOffer, original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	msgType, data, err := Unmarshal(&buf)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if msgType != MsgFileOffer {
		t.Errorf("MessageType = %d, want %d", msgType, MsgFileOffer)
	}

	var recovered FileOffer
	err = json.Unmarshal(data, &recovered)
	if err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if recovered.TransferID != original.TransferID {
		t.Errorf("TransferID = %s, want %s", recovered.TransferID, original.TransferID)
	}
	if recovered.Filename != original.Filename {
		t.Errorf("Filename = %s, want %s", recovered.Filename, original.Filename)
	}
	if recovered.FileSize != original.FileSize {
		t.Errorf("FileSize = %d, want %d", recovered.FileSize, original.FileSize)
	}
	if recovered.Sender != original.Sender {
		t.Errorf("Sender = %s, want %s", recovered.Sender, original.Sender)
	}
}

func TestMarshalRaw(t *testing.T) {
	rawData := []byte("raw-binary-data")
	var buf bytes.Buffer

	err := MarshalRaw(&buf, MsgChunk, rawData)
	if err != nil {
		t.Fatalf("MarshalRaw failed: %v", err)
	}

	msgType, data, err := Unmarshal(&buf)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if msgType != MsgChunk {
		t.Errorf("MessageType = %d, want %d", msgType, MsgChunk)
	}

	if !bytes.Equal(data, rawData) {
		t.Error("Data doesn't match original raw data")
	}
}

func TestChunkSize(t *testing.T) {
	// Sanity check: should stay comfortably in the 128 KiB - 1 MiB range so
	// the per-chunk fixed costs don't dominate a transfer.
	if ChunkSize < 128*1024 || ChunkSize > 1024*1024 {
		t.Errorf("ChunkSize = %d, want between %d and %d", ChunkSize, 128*1024, 1024*1024)
	}
}
