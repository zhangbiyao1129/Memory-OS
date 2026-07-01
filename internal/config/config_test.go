package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
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
