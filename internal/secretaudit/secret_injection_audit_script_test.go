package secretaudit_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretInjectionAuditScriptWritesReport(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "secret-injection-audit.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("secret injection audit script must exist at %s: %v", scriptPath, err)
	}

	reportPath := filepath.Join(t.TempDir(), "secret-injection-audit.md")
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"SECRET_INJECTION_AUDIT_REPORT_PATH="+reportPath,
		"RUNTIME_SECRET_INJECTION_CMD=printf '{\"status\":\"pass\",\"request_id\":\"req_1\",\"secret_ref\":\"secret_ref_1\",\"audit_log_count\":1,\"runtime_leak_counts\":{\"audit_metadata_hits\":0,\"archive_markdown_hits\":0,\"archive_chunk_hits\":0,\"hot_memory_hits\":0,\"archive_qdrant_payload_hits\":0,\"hot_memory_qdrant_payload_hits\":0,\"qdrant_live_payload_hits\":0},\"cleanup\":{\"secret_disabled\":true,\"project_deleted\":true,\"org_deleted\":true,\"user_disabled\":true}}'",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("secret injection audit script failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	for _, want := range []string{
		"# Secret Injection Audit Report",
		"status: pass",
		"secret.inject",
		"`audit_log_count`: 1",
		"`qdrant_live_payload_hits`: 0",
		"cleanup",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
	if !strings.Contains(string(output), "secret injection audit report written") {
		t.Fatalf("output should mention report path, got:\n%s", output)
	}
}

func TestMakefileExposesSecretInjectionAuditTarget(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)
	if !strings.Contains(makefile, "secret-injection-audit:") {
		t.Fatal("Makefile must expose secret-injection-audit target")
	}
	if !strings.Contains(makefile, "scripts/secret-injection-audit.sh") {
		t.Fatal("secret-injection-audit target must invoke scripts/secret-injection-audit.sh")
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}
