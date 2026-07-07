// Package secretlocal 实现本机 MCP 侧的设备密钥与 AES-256-GCM 加解密。
// 明文只在本进程内出现；服务端永远只见到 EncryptedBlob。
package secretlocal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"memory-os/internal/secret"
)

const algorithmAESGCM = "AES-256-GCM"

// DeviceKey 是本机设备密钥，持有 32 字节 AES key。
type DeviceKey struct {
	id  string
	key []byte
	gcm cipher.AEAD
}

// GenerateDeviceKey 生成一把新的 32 字节设备 key。
func GenerateDeviceKey(id string) (DeviceKey, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return DeviceKey{}, err
	}
	return NewDeviceKey(id, key)
}

// NewDeviceKey 从已有的 key 字节构造 DeviceKey。
func NewDeviceKey(id string, key []byte) (DeviceKey, error) {
	if id == "" {
		return DeviceKey{}, errors.New("device key id is required")
	}
	if len(key) != 32 {
		return DeviceKey{}, errors.New("device key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return DeviceKey{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return DeviceKey{}, err
	}
	return DeviceKey{id: id, key: key, gcm: gcm}, nil
}

// ID 返回设备 key 标识。
func (k DeviceKey) ID() string {
	return k.id
}

// Fingerprint 返回 key 的 sha256 前 16 字节 hex，用于服务端标注而不泄露 key。
func (k DeviceKey) Fingerprint() string {
	sum := sha256.Sum256(k.key)
	return hex.EncodeToString(sum[:8])
}

// Encrypt 对明文做 AES-256-GCM 加密，返回可上传的密文 blob。
func (k DeviceKey) Encrypt(plaintext []byte) (secret.EncryptedBlob, error) {
	if len(plaintext) == 0 {
		return secret.EncryptedBlob{}, errors.New("plaintext is required")
	}
	nonce := make([]byte, k.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return secret.EncryptedBlob{}, err
	}
	ciphertext := k.gcm.Seal(nil, nonce, plaintext, []byte(k.id))
	return secret.EncryptedBlob{
		Algorithm:      algorithmAESGCM,
		DeviceKeyID:    k.id,
		KeyFingerprint: k.Fingerprint(),
		Nonce:          nonce,
		Ciphertext:     ciphertext,
	}, nil
}

// Decrypt 用本地 key 还原明文。
func (k DeviceKey) Decrypt(blob secret.EncryptedBlob) ([]byte, error) {
	if blob.Algorithm != algorithmAESGCM {
		return nil, errors.New("unsupported algorithm")
	}
	if len(blob.Nonce) != k.gcm.NonceSize() {
		return nil, errors.New("invalid nonce size")
	}
	plaintext, err := k.gcm.Open(nil, blob.Nonce, blob.Ciphertext, []byte(blob.DeviceKeyID))
	if err != nil {
		return nil, errors.New("decrypt failed")
	}
	return plaintext, nil
}
