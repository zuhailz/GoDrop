package crypto

import (
	"bytes"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	if kp.PrivateKey == nil {
		t.Error("PrivateKey is nil")
	}

	if kp.PublicKey == nil {
		t.Error("PublicKey is nil")
	}

	if len(kp.PublicKey.Bytes()) == 0 {
		t.Error("PublicKey bytes should not be empty")
	}
}

func TestDeriveSharedSecret(t *testing.T) {
	kp1, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair 1 failed: %v", err)
	}

	kp2, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair 2 failed: %v", err)
	}

	secret1, err := DeriveSharedSecret(kp1.PrivateKey, kp2.PublicKey)
	if err != nil {
		t.Fatalf("DeriveSharedSecret 1 failed: %v", err)
	}

	secret2, err := DeriveSharedSecret(kp2.PrivateKey, kp1.PublicKey)
	if err != nil {
		t.Fatalf("DeriveSharedSecret 2 failed: %v", err)
	}

	if !bytes.Equal(secret1, secret2) {
		t.Error("Shared secrets don't match")
	}

	if len(secret1) != keySize {
		t.Errorf("Shared secret size = %d, want %d", len(secret1), keySize)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	kp1, _ := GenerateKeyPair()
	kp2, _ := GenerateKeyPair()

	sharedSecret, _ := DeriveSharedSecret(kp1.PrivateKey, kp2.PublicKey)

	plaintext := []byte("Hello, GoDrop! This is a test message.")

	ciphertext, err := Encrypt(sharedSecret, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if len(ciphertext) <= len(plaintext) {
		t.Error("Ciphertext should be longer than plaintext due to nonce and auth tag")
	}

	decrypted, err := Decrypt(sharedSecret, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("Decrypted = %s, want %s", decrypted, plaintext)
	}
}

func TestEncryptDecryptLargeData(t *testing.T) {
	kp1, _ := GenerateKeyPair()
	kp2, _ := GenerateKeyPair()

	sharedSecret, _ := DeriveSharedSecret(kp1.PrivateKey, kp2.PublicKey)

	plaintext := make([]byte, 64*1024)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	ciphertext, err := Encrypt(sharedSecret, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(sharedSecret, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Decrypted data doesn't match original")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	kp1, _ := GenerateKeyPair()
	kp2, _ := GenerateKeyPair()
	kp3, _ := GenerateKeyPair()

	sharedSecretCorrect, _ := DeriveSharedSecret(kp1.PrivateKey, kp2.PublicKey)
	sharedSecretWrong, _ := DeriveSharedSecret(kp1.PrivateKey, kp3.PublicKey)

	plaintext := []byte("Secret message")

	ciphertext, _ := Encrypt(sharedSecretCorrect, plaintext)

	_, err := Decrypt(sharedSecretWrong, ciphertext)
	if err == nil {
		t.Error("Decrypt should fail with wrong key")
	}
}

func TestMarshalPublicKey(t *testing.T) {
	kp, _ := GenerateKeyPair()

	data := MarshalPublicKey(kp.PublicKey)

	recovered, err := UnmarshalPublicKey(data)
	if err != nil {
		t.Fatalf("UnmarshalPublicKey failed: %v", err)
	}

	if !bytes.Equal(kp.PublicKey.Bytes(), recovered.Bytes()) {
		t.Error("Recovered public key doesn't match original")
	}
}
