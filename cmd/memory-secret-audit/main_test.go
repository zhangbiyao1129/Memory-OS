package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRuntimeSecretAuditUsesEnvConfig(t *testing.T) {
	restore := runRuntimeSecretAudit
	t.Cleanup(func() {
		runRuntimeSecretAudit = restore
	})

	t.Setenv("SECRET_VAULT_KEY_ID", "vault-key-1")
	t.Setenv("SECRET_VAULT_KEY_B64", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	t.Setenv("SECRET_AUDIT_PROBE_VALUE", "runtime-secret-audit-probe")

	runRuntimeSecretAudit = func(_ context.Context, request runtimeAuditRequest) (runtimeAuditResult, error) {
		if request.DSN != "postgres://memory-os" {
			t.Fatalf("request.DSN = %q, want postgres://memory-os", request.DSN)
		}
		if request.ArchiveDir != "/tmp/archive" {
			t.Fatalf("request.ArchiveDir = %q, want /tmp/archive", request.ArchiveDir)
		}
		if request.QdrantURL != "http://qdrant:6333" {
			t.Fatalf("request.QdrantURL = %q, want http://qdrant:6333", request.QdrantURL)
		}
		if request.SecretVaultKeyID != "vault-key-1" {
			t.Fatalf("request.SecretVaultKeyID = %q", request.SecretVaultKeyID)
		}
		if string(request.SecretVaultKey) != "0123456789abcdef0123456789abcdef" {
			t.Fatalf("request.SecretVaultKey = %q", string(request.SecretVaultKey))
		}
		if request.ProbeValue != "runtime-secret-audit-probe" {
			t.Fatalf("request.ProbeValue = %q", request.ProbeValue)
		}
		return runtimeAuditResult{
			Status:        "pass",
			RequestID:     "req_1",
			SecretRef:     "secret_ref_1",
			AuditLogCount: 1,
		}, nil
	}

	out, err := run([]string{"runtime", "--dsn", "postgres://memory-os", "--archive-dir", "/tmp/archive", "--qdrant-url", "http://qdrant:6333"})
	if err != nil {
		t.Fatalf("run runtime error = %v", err)
	}
	for _, want := range []string{`"status": "pass"`, `"request_id": "req_1"`, `"audit_log_count": 1`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunRuntimeSecretAuditRequiresVaultEnv(t *testing.T) {
	restore := runRuntimeSecretAudit
	t.Cleanup(func() {
		runRuntimeSecretAudit = restore
	})

	runRuntimeSecretAudit = func(context.Context, runtimeAuditRequest) (runtimeAuditResult, error) {
		t.Fatal("runRuntimeSecretAudit should not be called when vault env is missing")
		return runtimeAuditResult{}, nil
	}

	_, err := run([]string{"runtime", "--dsn", "postgres://memory-os", "--archive-dir", "/tmp/archive", "--qdrant-url", "http://qdrant:6333"})
	if err == nil {
		t.Fatal("run runtime error = nil, want missing vault env")
	}
	if !strings.Contains(err.Error(), "SECRET_VAULT_KEY_ID") {
		t.Fatalf("error = %v, want missing SECRET_VAULT_KEY_ID", err)
	}
}

func TestScanQdrantLivePayloadHitsPagesAndCountsProbe(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/collections/memory_os/points/scroll" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if requests == 1 {
			if body["offset"] != nil {
				t.Fatalf("first scroll offset = %#v, want nil", body["offset"])
			}
			_, _ = w.Write([]byte(`{"result":{"points":[{"payload":{"doc_type":"archive_chunk","content":"safe"}},{"payload":{"fact":"runtime-secret-audit-probe"}}],"next_page_offset":"page-2"}}`))
			return
		}
		if body["offset"] != "page-2" {
			t.Fatalf("second scroll offset = %#v, want page-2", body["offset"])
		}
		_, _ = w.Write([]byte(`{"result":{"points":[{"payload":{"fact_hash":"still-safe"}}],"next_page_offset":null}}`))
	}))
	defer server.Close()

	hits, scanned, err := scanQdrantLivePayloadHits(context.Background(), server.URL, "memory_os", "runtime-secret-audit-probe")
	if err != nil {
		t.Fatalf("scanQdrantLivePayloadHits() error = %v", err)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1", hits)
	}
	if scanned != 3 {
		t.Fatalf("scanned = %d, want 3", scanned)
	}
}

func TestRuntimeSecretAuditPassesAgainstPostgresAndFakeQdrant(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}

	archiveDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(archiveDir, "existing.md"), []byte("# safe\n"), 0o644); err != nil {
		t.Fatalf("write archive fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/memory_os/points/scroll" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"result":{"points":[],"next_page_offset":null}}`))
	}))
	defer server.Close()

	result, err := runtimeSecretAudit(context.Background(), runtimeAuditRequest{
		DSN:              dsn,
		ArchiveDir:       archiveDir,
		QdrantURL:        server.URL,
		SecretVaultKeyID: "vault-key-test",
		SecretVaultKey:   []byte("0123456789abcdef0123456789abcdef"),
		ProbeValue:       "runtime-secret-audit-probe",
	})
	if err != nil {
		t.Fatalf("runtimeSecretAudit() error = %v", err)
	}
	if result.Status != "pass" {
		t.Fatalf("status = %q, want pass", result.Status)
	}
	if result.AuditLogCount != 1 {
		t.Fatalf("audit_log_count = %d, want 1", result.AuditLogCount)
	}
	if result.RuntimeLeakCounts.AuditMetadataHits != 0 ||
		result.RuntimeLeakCounts.ArchiveMarkdownHits != 0 ||
		result.RuntimeLeakCounts.ArchiveChunkHits != 0 ||
		result.RuntimeLeakCounts.HotMemoryHits != 0 ||
		result.RuntimeLeakCounts.ArchiveQdrantPayloadHits != 0 ||
		result.RuntimeLeakCounts.HotMemoryQdrantPayloadHits != 0 ||
		result.RuntimeLeakCounts.QdrantLivePayloadHits != 0 {
		t.Fatalf("runtime leak counts = %#v, want all zero", result.RuntimeLeakCounts)
	}
	if !result.Cleanup.SecretDisabled || !result.Cleanup.ProjectDeleted || !result.Cleanup.OrgDeleted || !result.Cleanup.UserDisabled {
		t.Fatalf("cleanup = %#v, want all true", result.Cleanup)
	}
}
