package verify_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyScriptRunsDeliveryGatesInOrder(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "verify.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("verify script must exist at %s: %v", scriptPath, err)
	}

	tempDir := t.TempDir()
	stepsPath := filepath.Join(tempDir, "steps.log")
	mark := func(name string) string {
		return "printf '%s\\n' " + name + " >> " + shellQuote(stepsPath)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"PREFLIGHT_CMD="+mark("preflight"),
		"SECRET_SCAN_CMD="+mark("secret-scan"),
		"GO_TEST_CMD="+mark("go-test"),
		"WEB_BUILD_CMD="+mark("web-build"),
		"NPM_AUDIT_CMD="+mark("npm-audit"),
		"SMOKE_CMD="+mark("smoke"),
		"BACKUP_DRY_RUN_CMD="+mark("backup-dry-run"),
		"RESTORE_DRY_RUN_CMD="+mark("restore-dry-run"),
		"CRON_DRY_RUN_CMD="+mark("backup-cron-dry-run"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("verify script failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(stepsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	want := strings.Join([]string{"preflight", "secret-scan", "go-test", "web-build", "npm-audit", "smoke", "backup-dry-run", "restore-dry-run", "backup-cron-dry-run"}, "\n")
	if got != want {
		t.Fatalf("steps = %q, want %q", got, want)
	}
	if !strings.Contains(string(output), "verify completed") {
		t.Fatalf("output should report completion, got: %s", output)
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

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
