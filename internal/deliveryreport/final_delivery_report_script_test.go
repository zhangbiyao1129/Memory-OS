package deliveryreport_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalDeliveryReportScriptWritesDraftReport(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "final-delivery-report.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("final delivery report script must exist at %s: %v", scriptPath, err)
	}

	tempDir := t.TempDir()
	reportPath := filepath.Join(tempDir, "final-delivery-report.md")
	auditPath := filepath.Join(tempDir, "completion-audit.md")
	bundlePath := filepath.Join(tempDir, "browser-acceptance-bundle.md")
	securityBundlePath := filepath.Join(tempDir, "security-evidence-bundle.md")
	permissionBundlePath := filepath.Join(tempDir, "permission-isolation-bundle.md")
	checklistAuditPath := filepath.Join(tempDir, "completion-checklist-audit.md")
	if err := os.WriteFile(auditPath, []byte("# Memory OS Completion Audit Report\n\nstatus: pass\n"), 0o600); err != nil {
		t.Fatalf("write audit report: %v", err)
	}
	if err := os.WriteFile(bundlePath, []byte("# Browser Acceptance Bundle\n\n- dashboard screenshot captured\n"), 0o600); err != nil {
		t.Fatalf("write browser acceptance bundle: %v", err)
	}
	if err := os.WriteFile(securityBundlePath, []byte("# Security Evidence Bundle\n\n- source tree secret scan: pass\n"), 0o600); err != nil {
		t.Fatalf("write security evidence bundle: %v", err)
	}
	if err := os.WriteFile(permissionBundlePath, []byte("# Permission Isolation Bundle\n\n- unauthenticated request returns 401\n"), 0o600); err != nil {
		t.Fatalf("write permission isolation bundle: %v", err)
	}
	if err := os.WriteFile(checklistAuditPath, []byte("# Completion Checklist Audit\n\n- password login: unverified\n"), 0o600); err != nil {
		t.Fatalf("write completion checklist audit: %v", err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"FINAL_REPORT_PATH="+reportPath,
		"AUDIT_REPORT_PATH="+auditPath,
		"BROWSER_ACCEPTANCE_BUNDLE_PATH="+bundlePath,
		"SECURITY_EVIDENCE_BUNDLE_PATH="+securityBundlePath,
		"PERMISSION_ISOLATION_BUNDLE_PATH="+permissionBundlePath,
		"CHECKLIST_AUDIT_PATH="+checklistAuditPath,
		"RUN_RUNTIME_CHECKS=1",
		"VERSION_CMD=printf '{\"version\":\"0.9.0-dev\"}'",
		"HEALTHZ_CMD=printf '{\"status\":\"ok\"}'",
		"OPENAPI_COUNT_CMD=printf '50'",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("final delivery report script failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	for _, want := range []string{
		"# Memory OS Final Delivery Report (Draft)",
		"Completion audit status: pass",
		"| `/users` | pass |",
		"| `/` | pass | consolidated browser acceptance bundle",
		"## Security Summary",
		"Browser acceptance bundle:",
		"Security evidence bundle:",
		"Permission isolation bundle:",
		"Checklist audit ledger:",
		"## Permission Isolation Summary",
		"Production permission isolation bundle",
		"/search-test",
		"RUN_RUNTIME_CHECKS=1 make final-delivery-report",
		"{\"version\":\"0.9.0-dev\"}",
		"{\"status\":\"ok\"}",
		"50",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
	if !strings.Contains(string(output), "final delivery report written") {
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
