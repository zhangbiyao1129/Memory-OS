package secret

import (
	"bytes"
	"testing"
)

func TestAESGCMEncryptDecryptRoundTrip(t *testing.T) {
	codec, err := NewAESGCMCodec("key-1", bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatalf("NewAESGCMCodec() error = %v", err)
	}

	encrypted, err := codec.Encrypt([]byte("fake-secret-value"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Contains(encrypted.Ciphertext, []byte("fake-secret-value")) {
		t.Fatal("ciphertext contains plaintext secret")
	}

	decrypted, err := codec.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(decrypted) != "fake-secret-value" {
		t.Fatalf("decrypted = %q, want fake-secret-value", decrypted)
	}
}

func TestAESGCMRejectsWrongKey(t *testing.T) {
	codecA, _ := NewAESGCMCodec("key-1", bytes.Repeat([]byte{1}, 32))
	codecB, _ := NewAESGCMCodec("key-2", bytes.Repeat([]byte{2}, 32))
	encrypted, err := codecA.Encrypt([]byte("fake-secret-value"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	_, err = codecB.Decrypt(encrypted)

	if err == nil {
		t.Fatal("Decrypt() error = nil, want wrong key rejection")
	}
}

func TestAESGCMRejectsTamperedCiphertext(t *testing.T) {
	codec, _ := NewAESGCMCodec("key-1", bytes.Repeat([]byte{1}, 32))
	encrypted, err := codec.Encrypt([]byte("fake-secret-value"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	encrypted.Ciphertext[0] ^= 0xff

	_, err = codec.Decrypt(encrypted)

	if err == nil {
		t.Fatal("Decrypt() error = nil, want tamper rejection")
	}
}

func TestAESGCMRejectsInvalidKeyLength(t *testing.T) {
	_, err := NewAESGCMCodec("key-1", []byte("short"))
	if err == nil {
		t.Fatal("NewAESGCMCodec() error = nil, want invalid key length")
	}
}
