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

func TestRestoreScriptCanRestoreQdrantSnapshotInsideDockerNetwork(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	auditDir := filepath.Join(tempDir, "restore-audit")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=1",
		"BACKUP_DIR="+backupDir,
		"RESTORE_AUDIT_DIR="+auditDir,
		"QDRANT_URL=http://qdrant:6333",
		"QDRANT_RESTORE_DOCKER_NETWORK=memory-os-restore-rehearsal_default",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore dry-run failed: %v\n%s", err, output)
	}

	commandPath := filepath.Join(auditDir, "qdrant.restore.command")
	assertFileContains(t, commandPath, "docker run --rm")
	assertFileContains(t, commandPath, "--user 0:0")
	assertFileContains(t, commandPath, "--network memory-os-restore-rehearsal_default")
	assertFileContains(t, commandPath, "-e NO_PROXY=qdrant,postgres,redis,memory-api,memory-worker,memory-mcp,memory-web,localhost,127.0.0.1")
	assertFileContains(t, commandPath, "curlimages/curl")
	assertFileContains(t, commandPath, "http://qdrant:6333/collections/memory_os/snapshots/upload")
	assertFileContains(t, commandPath, "-F 'snapshot=@/snapshot/memory_os.snapshot'")
}

func TestRestoreScriptSupportsSingleComposeFileForRehearsal(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	auditDir := filepath.Join(tempDir, "restore-audit")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=1",
		"BACKUP_DIR="+backupDir,
		"RESTORE_AUDIT_DIR="+auditDir,
		"COMPOSE_FILE=deploy/docker-compose.restore-rehearsal.yml",
		"COMPOSE_T480_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore dry-run failed: %v\n%s", err, output)
	}
	commandPath := filepath.Join(auditDir, "postgres.restore.command")
	assertFileContains(t, commandPath, "-f 'deploy/docker-compose.restore-rehearsal.yml'")
	data, err := os.ReadFile(commandPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "docker-compose.t480.yml") {
		t.Fatalf("rehearsal restore command must not include T480 production overlay when COMPOSE_T480_FILE is empty:\n%s", data)
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

func TestRestoreScriptRejectsRealRestoreWithoutTargetEnvironment(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=0",
		"CONFIRM_RESTORE=I_UNDERSTAND",
		"BACKUP_DIR="+backupDir,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("restore without target env succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(string(output), "RESTORE_TARGET_ENV") {
		t.Fatalf("output should explain RESTORE_TARGET_ENV requirement, got: %s", output)
	}
}

func TestRestoreScriptRequiresExtraConfirmationForProductionTarget(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=0",
		"CONFIRM_RESTORE=I_UNDERSTAND",
		"RESTORE_TARGET_ENV=production",
		"BACKUP_DIR="+backupDir,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("production restore without extra confirmation succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(string(output), "CONFIRM_PRODUCTION_RESTORE=I_UNDERSTAND_PRODUCTION_DATA_OVERWRITE") {
		t.Fatalf("output should explain production confirmation requirement, got: %s", output)
	}
}

func TestRestoreScriptRejectsBackupChecksumMismatch(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)

	if err := os.WriteFile(filepath.Join(backupDir, "archives", "markdown-archive.tar.gz"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=1",
		"BACKUP_DIR="+backupDir,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("restore with checksum mismatch succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(string(output), "checksum mismatch") {
		t.Fatalf("output should explain checksum mismatch, got: %s", output)
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
	manifest := `{"run_id":"fixture","postgres":{"database":"memory_os","file":"postgres/memory_os.sql","sha256":"e6b2c34c546356e9f5f50881f244a1be2e79f69955027fc53addaf0a5d2dccc1"},"archives":{"file":"archives/markdown-archive.tar.gz","sha256":"db4b4d0d1cb480bf9aeea253771c00febe627f236765fa37d6a5614f079a3aa0"},"qdrant":{"collection":"memory_os","file":"qdrant/memory_os.snapshot","sha256":"16a0eeb0791b6c92451fd284dd9f599e0a7dbe7f6ebea6e2d2d06c7f74aec112"}}`
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
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
