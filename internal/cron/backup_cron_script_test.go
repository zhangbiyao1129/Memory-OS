package cron_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallBackupCronDryRunPrintsAuditableEntry(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "install-backup-cron.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("cron install script must exist at %s: %v", scriptPath, err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=1",
		"PROJECT_DIR=/opt/memory-os",
		"CRON_SCHEDULE=17 3 * * *",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cron dry-run failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"17 3 * * * cd /opt/memory-os && /usr/bin/make backup",
		"memory-os-backup.log",
		"cron dry-run completed",
	} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestInstallBackupCronRejectsRealInstallWithoutConfirmation(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "install-backup-cron.sh")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "DRY_RUN=0")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("cron install succeeded without confirmation:\n%s", output)
	}
	if !strings.Contains(string(output), "CONFIRM_CRON_INSTALL=I_UNDERSTAND") {
		t.Fatalf("output should explain confirmation requirement, got:\n%s", output)
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
