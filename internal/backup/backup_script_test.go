package backup_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupScriptDryRunCreatesAuditableBackup(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "backup.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("backup script must exist at %s: %v", scriptPath, err)
	}

	tempDir := t.TempDir()
	archiveDir := filepath.Join(tempDir, "archives")
	if err := os.MkdirAll(filepath.Join(archiveDir, "org_demo", "project_demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "org_demo", "project_demo", "archive_demo.md"), []byte("# demo archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	backupRoot := filepath.Join(tempDir, "backups")
	oldBackup := filepath.Join(backupRoot, "old-run")
	if err := os.MkdirAll(oldBackup, 0o755); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().AddDate(0, 0, -31)
	if err := os.Chtimes(oldBackup, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=1",
		"RUN_ID=test-run",
		"BACKUP_ROOT="+backupRoot,
		"ARCHIVE_DIR="+archiveDir,
		"RETENTION_DAYS=30",
		"QDRANT_URL=http://qdrant:6333",
		"QDRANT_COLLECTION=memory_os",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("backup dry-run failed: %v\n%s", err, output)
	}

	runDir := filepath.Join(backupRoot, "test-run")
	assertFileContains(t, filepath.Join(runDir, "manifest.json"), `"run_id":"test-run"`)
	assertFileContains(t, filepath.Join(runDir, "postgres", "pg_dump.command"), "pg_dump")
	assertFileContains(t, filepath.Join(runDir, "qdrant", "snapshot.command"), "/collections/memory_os/snapshots")

	if _, err := os.Stat(filepath.Join(runDir, "archives", "markdown-archive.tar.gz")); err != nil {
		t.Fatalf("markdown archive tarball must exist: %v", err)
	}
	if _, err := os.Stat(oldBackup); !os.IsNotExist(err) {
		t.Fatalf("old backup older than retention must be removed, stat err=%v", err)
	}
	if !strings.Contains(string(output), "backup completed") {
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
