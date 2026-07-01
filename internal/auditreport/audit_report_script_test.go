package auditreport_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditReportScriptWritesDeliveryEvidence(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "audit-report.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("audit report script must exist at %s: %v", scriptPath, err)
	}

	tempDir := t.TempDir()
	reportPath := filepath.Join(tempDir, "completion-audit.md")
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"AUDIT_REPORT_PATH="+reportPath,
		"VERIFY_CMD=printf 'verify completed\\n'",
		"BACKUP_CHECK_CMD=printf 'backup completed: /tmp/backup\\n'",
		"RUN_REAL_VERIFY=1",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("audit report script failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	for _, want := range []string{
		"# Memory OS Completion Audit Report",
		"docs/memory-os-spec.md",
		"make verify",
		"verify completed",
		"backup completed",
		"Secret 明文不得进入日志、Markdown、Qdrant、Hot Memory 或聊天回答",
		"status: pass",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
	if !strings.Contains(string(output), "audit report written") {
		t.Fatalf("output should mention report path, got: %s", output)
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
