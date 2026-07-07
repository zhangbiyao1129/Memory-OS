package secretlocal

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultKeyPath 是本机设备密钥文件的默认位置。
func DefaultKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "memory-os", "secret-device-key.json"), nil
}

type deviceKeyFile struct {
	DeviceKeyID    string `json:"device_key_id"`
	Algorithm      string `json:"algorithm"`
	KeyB64         string `json:"key_b64"`
	KeyFingerprint string `json:"key_fingerprint"`
	CreatedAt      string `json:"created_at"`
}

// LoadOrCreateDeviceKey 加载设备 key；不存在则生成。
// 目录权限必须 700、文件权限必须 600，否则拒绝运行（不静默修复）。
func LoadOrCreateDeviceKey(path string) (DeviceKey, error) {
	dir := filepath.Dir(path)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return createDeviceKey(path, dir)
	} else if err != nil {
		return DeviceKey{}, err
	}
	return loadDeviceKey(path, dir)
}

func createDeviceKey(path, dir string) (DeviceKey, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return DeviceKey{}, err
	}
	// MkdirAll 可能因已存在而不改权限，显式收紧。
	if err := os.Chmod(dir, 0o700); err != nil {
		return DeviceKey{}, err
	}
	id := "device_" + randomID()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return DeviceKey{}, err
	}
	key, err := NewDeviceKey(id, raw)
	if err != nil {
		return DeviceKey{}, err
	}
	file := deviceKeyFile{
		DeviceKeyID:    id,
		Algorithm:      algorithmAESGCM,
		KeyB64:         base64.StdEncoding.EncodeToString(raw),
		KeyFingerprint: key.Fingerprint(),
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return DeviceKey{}, err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return DeviceKey{}, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return DeviceKey{}, err
	}
	return key, nil
}

func loadDeviceKey(path, dir string) (DeviceKey, error) {
	if err := ensureSecurePermissions(path, dir); err != nil {
		return DeviceKey{}, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return DeviceKey{}, err
	}
	var file deviceKeyFile
	if err := json.Unmarshal(body, &file); err != nil {
		return DeviceKey{}, fmt.Errorf("parse device key file: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(file.KeyB64)
	if err != nil {
		return DeviceKey{}, fmt.Errorf("decode device key: %w", err)
	}
	return NewDeviceKey(file.DeviceKeyID, raw)
}

func ensureSecurePermissions(path, dir string) error {
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		return fmt.Errorf("device key dir %s permission %o is insecure, require 700", dir, perm)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		return err
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		return fmt.Errorf("device key file %s permission %o is insecure, require 600", path, perm)
	}
	return nil
}

func randomID() string {
	raw := make([]byte, 9)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
