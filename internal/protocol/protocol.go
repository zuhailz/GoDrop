package protocol

import (
	"encoding/binary"
	"encoding/json"
	"io"
)

type MessageType uint8

const (
	MsgKeyExchange     MessageType = 0
	MsgFileOffer       MessageType = 1
	MsgFileAccept      MessageType = 2
	MsgFileReject      MessageType = 3
	MsgChunk           MessageType = 4
	MsgPeerJoin        MessageType = 5
	MsgPeerLeave       MessageType = 6
	MsgEncryptedPacket MessageType = 7
	MsgProgress        MessageType = 8
	MsgSystemEvent     MessageType = 9
	MsgPinResponse     MessageType = 10
	MsgPinChallenge    MessageType = 11
)

const ChunkSize = 32 * 1024

type KeyExchange struct {
	PublicKey []byte `json:"public_key"`
}

type PeerJoin struct {
	PeerID   string `json:"peer_id"`
	PeerName string `json:"peer_name"`
}

type PeerLeave struct {
	PeerID string `json:"peer_id"`
}

type SystemEvent struct {
	Sender string `json:"sender"`
	Text   string `json:"text"`
}

type FileOffer struct {
	TransferID string `json:"transfer_id"`
	Filename   string `json:"filename"`
	FileSize   int64  `json:"file_size"`
	Sender     string `json:"sender"`
	Folder     bool   `json:"folder,omitempty"`
}

type FileAccept struct {
	TransferID string `json:"transfer_id"`
}

type FileReject struct {
	TransferID string `json:"transfer_id"`
}

type Chunk struct {
	TransferID string `json:"transfer_id"`
	Offset     int64  `json:"offset"`
	Data       []byte `json:"data"`
	Checksum   uint32 `json:"checksum"`
}

type Progress struct {
	TransferID string `json:"transfer_id"`
	PeerID     string `json:"peer_id"`
	Offset     int64  `json:"offset"`
	Total      int64  `json:"total"`
}

type EncryptedPacket struct {
	Data []byte `json:"data"`
}

type PinChallenge struct{}

type PinResponse struct {
	Tag []byte `json:"tag"`
}

func Marshal(w io.Writer, msgType MessageType, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if err := binary.Write(w, binary.BigEndian, msgType); err != nil {
		return err
	}

	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}

	_, err = w.Write(data)
	return err
}

func Unmarshal(r io.Reader) (MessageType, []byte, error) {
	var msgType MessageType
	if err := binary.Read(r, binary.BigEndian, &msgType); err != nil {
		return 0, nil, err
	}

	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, nil, err
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, err
	}

	return msgType, data, nil
}

func MarshalRaw(w io.Writer, msgType MessageType, data []byte) error {
	if err := binary.Write(w, binary.BigEndian, msgType); err != nil {
		return err
	}

	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}

	_, err := w.Write(data)
	return err
}
