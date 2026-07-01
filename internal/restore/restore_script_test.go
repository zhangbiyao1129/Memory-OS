package restore_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreScriptDryRunWritesAuditableCommands(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("restore script must exist at %s: %v", scriptPath, err)
	}

	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	auditDir := filepath.Join(tempDir, "restore-audit")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=1",
		"BACKUP_DIR="+backupDir,
		"RESTORE_AUDIT_DIR="+auditDir,
		"ARCHIVE_DIR="+filepath.Join(tempDir, "archives"),
		"QDRANT_URL=http://qdrant:6333",
		"QDRANT_COLLECTION=memory_os",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore dry-run failed: %v\n%s", err, output)
	}

	assertFileContains(t, filepath.Join(auditDir, "postgres.restore.command"), "psql -U memory_os -d memory_os")
	assertFileContains(t, filepath.Join(auditDir, "archives.restore.command"), "tar -C")
	assertFileContains(t, filepath.Join(auditDir, "qdrant.restore.command"), "/collections/memory_os/snapshots/upload")
	if !strings.Contains(string(output), "restore dry-run completed") {
		t.Fatalf("output should report dry-run completion, got: %s", output)
	}
}

func TestRestoreScriptRejectsRealRestoreWithoutConfirmation(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=0",
		"BACKUP_DIR="+backupDir,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("restore without confirmation succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(string(output), "CONFIRM_RESTORE=I_UNDERSTAND") {
		t.Fatalf("output should explain confirmation requirement, got: %s", output)
	}
}

func createBackupFixture(t *testing.T, root string) string {
	t.Helper()
	backupDir := filepath.Join(root, "backup")
	for _, dir := range []string{"postgres", "archives", "qdrant"} {
		if err := os.MkdirAll(filepath.Join(backupDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(backupDir, "postgres", "memory_os.sql"), []byte("-- dump\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "archives", "markdown-archive.tar.gz"), []byte("tarball"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "qdrant", "memory_os.snapshot"), []byte("snapshot"), 0o644); err != nil {
		t.Fatal(err)
	}
	return backupDir
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

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, data)
	}
}
