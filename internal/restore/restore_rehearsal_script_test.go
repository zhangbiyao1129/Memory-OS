package restore_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreRehearsalDryRunWritesPlanAndRunsRestoreDryRun(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("restore rehearsal script must exist at %s: %v", scriptPath, err)
	}
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	auditDir := filepath.Join(tempDir, "rehearsal")
	restoreMarker := filepath.Join(tempDir, "restore-called")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_AUDIT_DIR="+auditDir,
		"RESTORE_CMD=printf '%s\\n' restore-dry-run > "+shellQuote(restoreMarker),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore rehearsal dry-run failed: %v\n%s", err, output)
	}
	assertFileContains(t, filepath.Join(auditDir, "rehearsal-plan.txt"), "restore_target_env=test")
	assertFileContains(t, filepath.Join(auditDir, "rehearsal-plan.txt"), "mode=dry-run")
	assertFileContains(t, restoreMarker, "restore-dry-run")
	if !strings.Contains(string(output), "restore rehearsal dry-run completed") {
		t.Fatalf("output should report dry-run completion, got: %s", output)
	}
}

func TestRestoreRehearsalDryRunUsesIsolatedComposeAudit(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	auditDir := filepath.Join(tempDir, "rehearsal")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_AUDIT_DIR="+auditDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore rehearsal dry-run failed: %v\n%s", err, output)
	}
	commandPath := filepath.Join(auditDir, "restore", "postgres.restore.command")
	assertFileContains(t, commandPath, "-f 'deploy/docker-compose.restore-rehearsal.yml'")
	data, err := os.ReadFile(commandPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "docker-compose.t480.yml") {
		t.Fatalf("restore rehearsal dry-run must not audit production t480 overlay:\n%s", data)
	}
}

func TestRestoreRehearsalRejectsProductionProjectName(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_PROJECT=deploy",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("restore rehearsal with production project succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(string(output), "must not target production project") {
		t.Fatalf("output should explain production project rejection, got: %s", output)
	}
}

func TestRestoreRehearsalPreflightWritesAudit(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal-preflight.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	auditDir := filepath.Join(tempDir, "preflight")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_AUDIT_DIR="+auditDir,
		"DOCKER_CMD=/bin/false",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore rehearsal preflight failed: %v\n%s", err, output)
	}
	assertFileContains(t, filepath.Join(auditDir, "preflight.txt"), "status=ok")
	assertFileContains(t, filepath.Join(auditDir, "preflight.txt"), "project=memory-os-restore-rehearsal")
	if !strings.Contains(string(output), "restore rehearsal preflight ok") {
		t.Fatalf("output should report preflight ok, got: %s", output)
	}
}

func TestRestoreRehearsalPreflightRejectsExistingProjectResources(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal-preflight.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	dockerMock := filepath.Join(tempDir, "docker")
	if err := os.WriteFile(dockerMock, []byte("#!/usr/bin/env bash\nif [[ \"$1 $2\" == \"ps -a\" ]]; then echo existing-container; exit 0; fi\nif [[ \"$1 $2 $3\" == \"volume ls --filter\" ]]; then echo existing-volume; exit 0; fi\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"DOCKER_CMD="+dockerMock,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("restore rehearsal preflight with existing resources succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(string(output), "existing containers") {
		t.Fatalf("output should explain existing resource rejection, got: %s", output)
	}
}

func TestRestoreRehearsalRealModeRequiresConfirmation(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_MODE=real",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("real restore rehearsal without confirmation succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(string(output), "CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE") {
		t.Fatalf("output should explain confirmation requirement, got: %s", output)
	}
}

func TestRestoreRehearsalRealModeRunsPreflightBeforeUp(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	stepsPath := filepath.Join(tempDir, "steps")
	step := func(name string) string {
		return "printf '%s\\n' " + name + " >> " + shellQuote(stepsPath)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_MODE=real",
		"CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE",
		"PREFLIGHT_CMD="+step("preflight"),
		"RESTORE_REHEARSAL_UP_CMD="+step("up"),
		"RESTORE_CMD="+step("restore"),
		"SMOKE_CMD="+step("smoke"),
		"RESTORE_REHEARSAL_DOWN_CMD="+step("down"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore rehearsal real mode failed: %v\n%s", err, output)
	}
	data, err := os.ReadFile(stepsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	want := strings.Join([]string{"preflight", "up", "restore", "smoke", "down"}, "\n")
	if got != want {
		t.Fatalf("steps = %q, want %q", got, want)
	}
}

func TestRestoreRehearsalRealModePassesQdrantDockerNetwork(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	auditDir := filepath.Join(tempDir, "rehearsal")
	restoreEnvPath := filepath.Join(tempDir, "restore-env")
	smokeMarker := filepath.Join(tempDir, "smoke-called")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_MODE=real",
		"CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE",
		"RESTORE_REHEARSAL_PROJECT=memory-os-restore-rehearsal",
		"RESTORE_REHEARSAL_AUDIT_DIR="+auditDir,
		"RESTORE_REHEARSAL_UP_CMD=printf up",
		"RESTORE_REHEARSAL_DOWN_CMD=printf down",
		"RESTORE_CMD=printf '%s\\n' \"$QDRANT_RESTORE_DOCKER_NETWORK\" > "+shellQuote(restoreEnvPath),
		"SMOKE_CMD=printf '%s\\n' smoke > "+shellQuote(smokeMarker),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore rehearsal real mode failed: %v\n%s", err, output)
	}
	assertFileContains(t, restoreEnvPath, "memory-os-restore-rehearsal_default")
	assertFileContains(t, smokeMarker, "smoke")
}

func TestRestoreRehearsalRealModePassesIsolatedComposeToRestore(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	auditDir := filepath.Join(tempDir, "rehearsal")
	restoreEnvPath := filepath.Join(tempDir, "restore-env")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_MODE=real",
		"CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE",
		"RESTORE_REHEARSAL_PROJECT=memory-os-restore-rehearsal",
		"RESTORE_REHEARSAL_AUDIT_DIR="+auditDir,
		"RESTORE_REHEARSAL_UP_CMD=printf up",
		"RESTORE_REHEARSAL_DOWN_CMD=printf down",
		"RESTORE_CMD=printf 'COMPOSE_FILE=%s\\nCOMPOSE_T480_FILE=%s\\nCOMPOSE_PROJECT_NAME=%s\\nQDRANT_URL=%s\\n' \"$COMPOSE_FILE\" \"$COMPOSE_T480_FILE\" \"$COMPOSE_PROJECT_NAME\" \"$QDRANT_URL\" > "+shellQuote(restoreEnvPath),
		"SMOKE_CMD=printf smoke",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore rehearsal real mode failed: %v\n%s", err, output)
	}
	assertFileContains(t, restoreEnvPath, "COMPOSE_FILE=deploy/docker-compose.restore-rehearsal.yml")
	assertFileContains(t, restoreEnvPath, "COMPOSE_T480_FILE=")
	assertFileContains(t, restoreEnvPath, "COMPOSE_PROJECT_NAME=memory-os-restore-rehearsal")
	assertFileContains(t, restoreEnvPath, "QDRANT_URL=http://qdrant:6333")
}

func TestRestoreRehearsalRealModeLoadsProductionEnvForDefaultComposeUp(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh"))
	if err != nil {
		t.Fatalf("read restore rehearsal script: %v", err)
	}
	script := string(content)
	for _, required := range []string{
		"RESTORE_REHEARSAL_USES_DEFAULT_UP=true",
		". scripts/load-prod-env.sh",
		"RESTORE_REHEARSAL_INFRA_UP_CMD=\"docker-compose -p $(shell_quote \"$RESTORE_REHEARSAL_PROJECT\")",
		"RESTORE_REHEARSAL_APP_UP_CMD=\"docker-compose -p $(shell_quote \"$RESTORE_REHEARSAL_PROJECT\")",
		"RESTORE_REHEARSAL_WAIT_CMD=\"for i in {1..60}; do docker-compose",
		"pg_isready -U memory_os -d memory_os",
		"-e NO_PROXY=qdrant,postgres,redis,memory-api,memory-worker,memory-mcp,memory-web,localhost,127.0.0.1",
		"http://qdrant:6333/healthz",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("restore rehearsal real mode must load production env for default compose up, missing %q", required)
		}
	}
}

func TestRestoreRehearsalDefaultRealModeWaitsAndRestoresBeforeStartingApps(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	stepsPath := filepath.Join(tempDir, "steps")
	step := func(name string) string {
		return "printf '%s\\n' " + name + " >> " + shellQuote(stepsPath)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_MODE=real",
		"CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE",
		"POSTGRES_PASSWORD=test-postgres-password",
		"LLM_BASE_URL=http://llm.example.test",
		"LLM_API_KEY=test-llm-key",
		"SECRET_VAULT_KEY_ID=test-key",
		"SECRET_VAULT_KEY_B64=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"RESTORE_REHEARSAL_INFRA_UP_CMD="+step("infra-up"),
		"RESTORE_REHEARSAL_APP_UP_CMD="+step("app-up"),
		"RESTORE_REHEARSAL_WAIT_CMD="+step("wait"),
		"RESTORE_REHEARSAL_DOWN_CMD="+step("down"),
		"PREFLIGHT_CMD="+step("preflight"),
		"RESTORE_CMD="+step("restore"),
		"SMOKE_CMD="+step("smoke"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore rehearsal default real mode failed: %v\n%s", err, output)
	}
	data, err := os.ReadFile(stepsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	want := strings.Join([]string{"preflight", "infra-up", "wait", "restore", "app-up", "smoke", "down"}, "\n")
	if got != want {
		t.Fatalf("steps = %q, want %q", got, want)
	}
}

func TestRestoreRehearsalComposeIncludesWebService(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker-compose.restore-rehearsal.yml"))
	if err != nil {
		t.Fatalf("read restore rehearsal compose: %v", err)
	}
	compose := string(content)
	for _, required := range []string{
		"  memory-web:",
		"image: ${RESTORE_REHEARSAL_WEB_IMAGE:-deploy-memory-web}",
		"- memory-api",
	} {
		if !strings.Contains(compose, required) {
			t.Fatalf("restore rehearsal compose must include memory-web for smoke coverage, missing %q", required)
		}
	}
}

func TestRestoreRehearsalComposeDoesNotAutoRunMigrationsBeforeRestore(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker-compose.restore-rehearsal.yml"))
	if err != nil {
		t.Fatalf("read restore rehearsal compose: %v", err)
	}
	compose := string(content)
	for _, forbidden := range []string{
		"docker-entrypoint-initdb.d",
		"../migrations",
	} {
		if strings.Contains(compose, forbidden) {
			t.Fatalf("restore rehearsal postgres must start empty and restore dump as the schema source, found %q", forbidden)
		}
	}
}

func TestRestoreRehearsalDefaultSmokeBypassesProxyForComposeServices(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh"))
	if err != nil {
		t.Fatalf("read restore rehearsal script: %v", err)
	}
	script := string(content)
	for _, required := range []string{
		"-e SMOKE_API_URL=http://memory-api:18081/healthz",
		"-e SMOKE_QDRANT_URL=http://qdrant:6333",
		"-e SMOKE_MCP_URL=http://memory-mcp:18082",
		"-e SMOKE_WEB_URL=http://memory-web:18080",
		"-e NO_PROXY=qdrant,postgres,redis,memory-api,memory-worker,memory-mcp,memory-web,localhost,127.0.0.1",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("restore rehearsal smoke must bypass proxy for compose services, missing %q", required)
		}
	}
}

func TestRestoreRehearsalRealModeRunsUpRestoreSmokeAndCleanup(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	stepsPath := filepath.Join(tempDir, "steps")
	step := func(name string) string {
		return "printf '%s\\n' " + name + " >> " + shellQuote(stepsPath)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_MODE=real",
		"CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE",
		"RESTORE_REHEARSAL_UP_CMD="+step("up"),
		"RESTORE_CMD="+step("restore"),
		"SMOKE_CMD="+step("smoke"),
		"RESTORE_REHEARSAL_DOWN_CMD="+step("down"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore rehearsal real mode failed: %v\n%s", err, output)
	}
	data, err := os.ReadFile(stepsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	want := strings.Join([]string{"up", "restore", "smoke", "down"}, "\n")
	if got != want {
		t.Fatalf("steps = %q, want %q", got, want)
	}
}

func TestRestoreRehearsalRealModeCleansUpWhenRestoreFails(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "restore-rehearsal.sh")
	tempDir := t.TempDir()
	backupDir := createBackupFixture(t, tempDir)
	stepsPath := filepath.Join(tempDir, "steps")
	step := func(name string) string {
		return "printf '%s\\n' " + name + " >> " + shellQuote(stepsPath)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"BACKUP_DIR="+backupDir,
		"RESTORE_REHEARSAL_MODE=real",
		"CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE",
		"RESTORE_REHEARSAL_UP_CMD="+step("up"),
		"RESTORE_CMD="+step("restore")+"; exit 23",
		"SMOKE_CMD="+step("smoke"),
		"RESTORE_REHEARSAL_DOWN_CMD="+step("down"),
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("restore rehearsal succeeded unexpectedly:\n%s", output)
	}
	data, err := os.ReadFile(stepsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	want := strings.Join([]string{"up", "restore", "down"}, "\n")
	if got != want {
		t.Fatalf("steps = %q, want %q", got, want)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
