package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

type EncryptedValue struct {
	KeyID      string
	Nonce      []byte
	Ciphertext []byte
}

type AESGCMCodec struct {
	keyID string
	gcm   cipher.AEAD
}

func NewAESGCMCodec(keyID string, key []byte) (AESGCMCodec, error) {
	if keyID == "" {
		return AESGCMCodec{}, errors.New("key id is required")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return AESGCMCodec{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return AESGCMCodec{}, err
	}
	return AESGCMCodec{keyID: keyID, gcm: gcm}, nil
}

func (c AESGCMCodec) Encrypt(plaintext []byte) (EncryptedValue, error) {
	if len(plaintext) == 0 {
		return EncryptedValue{}, errors.New("plaintext is required")
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return EncryptedValue{}, err
	}
	ciphertext := c.gcm.Seal(nil, nonce, plaintext, []byte(c.keyID))
	return EncryptedValue{KeyID: c.keyID, Nonce: nonce, Ciphertext: ciphertext}, nil
}

func (c AESGCMCodec) Decrypt(value EncryptedValue) ([]byte, error) {
	if value.KeyID != c.keyID {
		return nil, errors.New("secret key id mismatch")
	}
	if len(value.Nonce) != c.gcm.NonceSize() {
		return nil, errors.New("secret nonce is invalid")
	}
	plaintext, err := c.gcm.Open(nil, value.Nonce, value.Ciphertext, []byte(value.KeyID))
	if err != nil {
		return nil, errors.New("secret decrypt failed")
	}
	return plaintext, nil
}
