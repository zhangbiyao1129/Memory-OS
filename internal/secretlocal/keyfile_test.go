package secretlocal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateDeviceKeyCreatesWithSecurePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory-os")
	path := filepath.Join(dir, "secret-device-key.json")

	key, err := LoadOrCreateDeviceKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateDeviceKey() error = %v", err)
	}
	if key.ID() == "" || key.Fingerprint() == "" {
		t.Fatal("device key id or fingerprint empty")
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir perm = %o, want 700", perm)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perm = %o, want 600", perm)
	}
}

func TestLoadOrCreateDeviceKeyReturnsSameKeyOnSecondLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory-os", "secret-device-key.json")

	first, err := LoadOrCreateDeviceKey(path)
	if err != nil {
		t.Fatalf("first load error = %v", err)
	}
	second, err := LoadOrCreateDeviceKey(path)
	if err != nil {
		t.Fatalf("second load error = %v", err)
	}
	if first.ID() != second.ID() || first.Fingerprint() != second.Fingerprint() {
		t.Fatalf("key mismatch: %q/%q vs %q/%q", first.ID(), first.Fingerprint(), second.ID(), second.Fingerprint())
	}
}

func TestLoadOrCreateDeviceKeyRejectsInsecureFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory-os", "secret-device-key.json")
	if _, err := LoadOrCreateDeviceKey(path); err != nil {
		t.Fatalf("initial create error = %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod file: %v", err)
	}

	if _, err := LoadOrCreateDeviceKey(path); err == nil {
		t.Fatal("LoadOrCreateDeviceKey() error = nil, want insecure file permission rejection")
	}
}

func TestLoadOrCreateDeviceKeyRejectsInsecureDirPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory-os")
	path := filepath.Join(dir, "secret-device-key.json")
	if _, err := LoadOrCreateDeviceKey(path); err != nil {
		t.Fatalf("initial create error = %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}

	if _, err := LoadOrCreateDeviceKey(path); err == nil {
		t.Fatal("LoadOrCreateDeviceKey() error = nil, want insecure dir permission rejection")
	}
}
