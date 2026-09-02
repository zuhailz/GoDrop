package crypto

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
)

// Room key display parameters.
const (
	roomKeyBytes = 16 // 128 bits of entropy
	groupSize    = 8  // hex chars per group
)

// Group separator for the room key display format.
const roomKeySep = "-"

// GenerateRoomKey creates 128 bits of cryptographically random key material.
// Use FormatRoomKey to render it as four groups of eight uppercase hex
// characters separated by dashes, e.g. 4F8A2C61-B0D3E79A-15C6F2B8-9E3D4A07.
func GenerateRoomKey() ([]byte, error) {
	raw := make([]byte, roomKeyBytes)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return nil, fmt.Errorf("failed to generate room key: %w", err)
	}
	return raw, nil
}

// FormatRoomKey encodes raw key bytes into the display format.
func FormatRoomKey(raw []byte) string {
	hex := fmt.Sprintf("%X", raw) // uppercase hex
	groups := make([]string, 0, len(hex)/groupSize)
	for i := 0; i < len(hex); i += groupSize {
		end := i + groupSize
		if end > len(hex) {
			end = len(hex)
		}
		groups = append(groups, hex[i:end])
	}
	return strings.Join(groups, roomKeySep)
}

// ParseRoomKey strips dashes and decodes the hex back to raw bytes.
// Returns an error if the formatted key is malformed.
func ParseRoomKey(key string) ([]byte, error) {
	clean := strings.ReplaceAll(key, roomKeySep, "")
	if len(clean) != roomKeyBytes*2 {
		return nil, fmt.Errorf("invalid room key length: expected %d hex chars, got %d", roomKeyBytes*2, len(clean))
	}
	raw := make([]byte, len(clean)/2)
	for i := 0; i < len(clean); i += 2 {
		hi, err := hexDigit(clean[i])
		if err != nil {
			return nil, err
		}
		lo, err := hexDigit(clean[i+1])
		if err != nil {
			return nil, err
		}
		raw[i/2] = (hi << 4) | lo
	}
	return raw, nil
}

func hexDigit(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	default:
		return 0, fmt.Errorf("invalid hex character: %c", c)
	}
}

// DeriveServiceName produces the mDNS instance name from a room key.
// The result is "godrop-" followed by 16 hex chars derived via HMAC-SHA256,
// giving a valid DNS-SD label (< 63 bytes, no dots).
//
// One-wayness: an attacker who sees the advertised name cannot recover the
// room key from it (HMAC is computationally one-way without the key).
func DeriveServiceName(roomKey []byte) string {
	mac := hmac.New(sha256.New, roomKey)
	mac.Write([]byte("godrop-mdns-salt"))
	sum := mac.Sum(nil)
	return fmt.Sprintf("godrop-%X", sum[:8]) // 16 hex chars
}

const (
	authInfo = "godrop-v1-pin-auth"
)

// Role labels for HMAC tags. The two sides compute different tags so a
// replay of one side's tag can't be forwarded as the other's.
const (
	AuthRoleHost     = "host"
	AuthRoleReceiver = "receiver"
)

// DeriveAuthKey derives an authentication key from the ECDH shared secret,
// both raw public keys (as salt), and the room key (as extra input).
// Binding the room key into the derivation means an attacker who doesn't
// know it can't produce a valid tag, even if they relay the key exchange
// transparently.
func DeriveAuthKey(sharedSecret, hostPub, recvPub, roomKey []byte) ([]byte, error) {
	salt := sha256.Sum256(append(hostPub, recvPub...))
	info := authInfo + string(roomKey)
	return hkdf.Key(sha256.New, sharedSecret, salt[:], info, keySize)
}

// ComputeAuthTag returns an HMAC-SHA256 tag keyed by authKey, bound to the
// given role ("host" or "receiver"). The two sides must compute different
// tags so a replay of one side's tag can't be forwarded as the other.
func ComputeAuthTag(authKey []byte, role string) []byte {
	mac := hmac.New(sha256.New, authKey)
	mac.Write([]byte(role))
	return mac.Sum(nil)
}

// VerifyAuthTag checks a received tag against the expected tag using
// constant-time comparison to prevent timing side-channels.
func VerifyAuthTag(authKey []byte, role string, receivedTag []byte) bool {
	expected := ComputeAuthTag(authKey, role)
	return hmac.Equal(expected, receivedTag)
}
