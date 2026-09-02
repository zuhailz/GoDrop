package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

const (
	keySize   = 32
	nonceSize = 12
)

// hkdfInfo domain-separates derived keys from any other use of the raw ECDH
// shared secret.
const hkdfInfo = "godrop-v1-ecdh-p256-aes-256-gcm"

var (
	ErrInvalidKeySize   = errors.New("invalid key size")
	ErrInvalidNonceSize = errors.New("invalid nonce size")
	ErrDecryptFailed    = errors.New("decryption failed")
)

type KeyPair struct {
	PrivateKey *ecdh.PrivateKey
	PublicKey  *ecdh.PublicKey
}

func GenerateKeyPair() (*KeyPair, error) {
	curve := ecdh.P256()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  privateKey.PublicKey(),
	}, nil
}

// DeriveSharedSecret runs the raw ECDH result through HKDF-SHA256 to produce
// a domain-separated AES-256 key. Both sides must use this exact function;
// changing the info string breaks wire compatibility with older binaries.
func DeriveSharedSecret(privateKey *ecdh.PrivateKey, peerPublicKey *ecdh.PublicKey) ([]byte, error) {
	sharedSecret, err := privateKey.ECDH(peerPublicKey)
	if err != nil {
		return nil, err
	}
	key, err := hkdf.Key(sha256.New, sharedSecret, nil, hkdfInfo, keySize)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func Encrypt(key []byte, plaintext []byte) ([]byte, error) {
	if len(key) != keySize {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := aesgcm.Seal(nil, nonce, plaintext, nil)

	result := make([]byte, 0, nonceSize+len(ciphertext))
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

func Decrypt(key []byte, ciphertext []byte) ([]byte, error) {
	if len(key) != keySize {
		return nil, ErrInvalidKeySize
	}

	if len(ciphertext) < nonceSize {
		return nil, ErrInvalidNonceSize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := ciphertext[:nonceSize]
	encryptedData := ciphertext[nonceSize:]

	plaintext, err := aesgcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}

	return plaintext, nil
}

func MarshalPublicKey(pub *ecdh.PublicKey) []byte {
	return pub.Bytes()
}

func UnmarshalPublicKey(data []byte) (*ecdh.PublicKey, error) {
	curve := ecdh.P256()
	return curve.NewPublicKey(data)
}
