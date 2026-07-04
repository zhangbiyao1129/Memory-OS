package config

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("ENABLE_DEV_ENDPOINTS", "")
	t.Setenv("MEMORY_API_ADDR", "")
	t.Setenv("MEMORY_WEB_ADDR", "")
	t.Setenv("MEMORY_MCP_ADDR", "")
	t.Setenv("POSTGRES_DSN", "")
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("QDRANT_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.APIAddr != ":18081" {
		t.Fatalf("APIAddr = %q, want %q", cfg.APIAddr, ":18081")
	}
	if cfg.WebAddr != ":18080" {
		t.Fatalf("WebAddr = %q, want %q", cfg.WebAddr, ":18080")
	}
	if cfg.MCPAddr != ":18082" {
		t.Fatalf("MCPAddr = %q, want %q", cfg.MCPAddr, ":18082")
	}
	if cfg.QdrantURL != "http://localhost:18083" {
		t.Fatalf("QdrantURL = %q, want %q", cfg.QdrantURL, "http://localhost:18083")
	}
	if cfg.ArchiveDir != "/data/memory-os" {
		t.Fatalf("ArchiveDir = %q, want /data/memory-os", cfg.ArchiveDir)
	}
	if cfg.AppEnv != "production" {
		t.Fatalf("AppEnv = %q, want production", cfg.AppEnv)
	}
	if cfg.EnableDevEndpoints {
		t.Fatal("EnableDevEndpoints = true, want false by default")
	}
}

func TestLoadReadsArchiveDir(t *testing.T) {
	t.Setenv("ARCHIVE_DIR", "/srv/memory-os")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ArchiveDir != "/srv/memory-os" {
		t.Fatalf("ArchiveDir = %q, want /srv/memory-os", cfg.ArchiveDir)
	}
}

func TestLoadReadsSecretVaultKey(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	t.Setenv("SECRET_VAULT_KEY_ID", "key-2026-07")
	t.Setenv("SECRET_VAULT_KEY_B64", base64.StdEncoding.EncodeToString(key))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.SecretVaultKeyID != "key-2026-07" {
		t.Fatalf("SecretVaultKeyID = %q, want key-2026-07", cfg.SecretVaultKeyID)
	}
	if !bytes.Equal(cfg.SecretVaultKey, key) {
		t.Fatal("SecretVaultKey was not decoded from SECRET_VAULT_KEY_B64")
	}
}

func TestLoadRejectsInvalidSecretVaultKey(t *testing.T) {
	t.Setenv("SECRET_VAULT_KEY_ID", "key-2026-07")
	t.Setenv("SECRET_VAULT_KEY_B64", "not-base64")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid secret vault key error")
	}
}

func TestLoadEnablesDevEndpointsOnlyWithExplicitFlag(t *testing.T) {
	t.Setenv("ENABLE_DEV_ENDPOINTS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.EnableDevEndpoints {
		t.Fatal("EnableDevEndpoints = false, want true when ENABLE_DEV_ENDPOINTS=true")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("MEMORY_API_ADDR", ":not-a-port")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid port error")
	}
}

func TestRedactPostgresDSN(t *testing.T) {
	dsn := "postgres://memory_user:very-secret-password@localhost:5432/memory_os?sslmode=disable"

	got := RedactDSN(dsn)

	if got == dsn {
		t.Fatal("RedactDSN() returned original DSN")
	}
	if got != "postgres://memory_user:xxxxx@localhost:5432/memory_os?sslmode=disable" {
		t.Fatalf("RedactDSN() = %q", got)
	}
}
