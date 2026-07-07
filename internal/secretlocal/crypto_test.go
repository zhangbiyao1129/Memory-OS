package secretlocal

import "testing"

func TestDeviceKeyEncryptDecryptRoundTrip(t *testing.T) {
	key, err := GenerateDeviceKey("device-test")
	if err != nil {
		t.Fatalf("GenerateDeviceKey() error = %v", err)
	}

	blob, err := key.Encrypt([]byte("fake-secret-value"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if blob.Algorithm != "AES-256-GCM" {
		t.Fatalf("algorithm = %q, want AES-256-GCM", blob.Algorithm)
	}
	if len(blob.Ciphertext) == 0 || len(blob.Nonce) == 0 {
		t.Fatal("ciphertext or nonce empty")
	}
	if string(blob.Ciphertext) == "fake-secret-value" {
		t.Fatal("ciphertext equals plaintext")
	}
	if blob.KeyFingerprint == "" || blob.KeyFingerprint != key.Fingerprint() {
		t.Fatalf("fingerprint mismatch: %q vs %q", blob.KeyFingerprint, key.Fingerprint())
	}

	plaintext, err := key.Decrypt(blob)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(plaintext) != "fake-secret-value" {
		t.Fatalf("decrypted = %q, want fake-secret-value", plaintext)
	}
}

func TestDeviceKeyDecryptRejectsWrongKey(t *testing.T) {
	key1, err := GenerateDeviceKey("device-1")
	if err != nil {
		t.Fatalf("GenerateDeviceKey(1) error = %v", err)
	}
	key2, err := GenerateDeviceKey("device-2")
	if err != nil {
		t.Fatalf("GenerateDeviceKey(2) error = %v", err)
	}

	blob, err := key1.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	if _, err := key2.Decrypt(blob); err == nil {
		t.Fatal("Decrypt() with wrong key error = nil, want failure")
	}
}

func TestDeviceKeyRejectsEmptyPlaintext(t *testing.T) {
	key, err := GenerateDeviceKey("device-test")
	if err != nil {
		t.Fatalf("GenerateDeviceKey() error = %v", err)
	}
	if _, err := key.Encrypt(nil); err == nil {
		t.Fatal("Encrypt(nil) error = nil, want empty plaintext rejection")
	}
}
