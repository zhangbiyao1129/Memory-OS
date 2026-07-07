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
		"RESTORE_REHEARSAL_PREFLIGHT_CMD="+mark("restore-rehearsal-preflight"),
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
	want := strings.Join([]string{"preflight", "secret-scan", "go-test", "web-build", "npm-audit", "smoke", "backup-dry-run", "restore-dry-run", "restore-rehearsal-preflight", "backup-cron-dry-run"}, "\n")
	if got != want {
		t.Fatalf("steps = %q, want %q", got, want)
	}
	if !strings.Contains(string(output), "verify completed") {
		t.Fatalf("output should report completion, got: %s", output)
	}
}

func TestPostDeployVerifyRunsRuntimeGatesInOrder(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("post deploy verify script must exist at %s: %v", scriptPath, err)
	}

	tempDir := t.TempDir()
	stepsPath := filepath.Join(tempDir, "steps.log")
	mark := func(name string) string {
		return "printf '%s\\n' " + name + " >> " + shellQuote(stepsPath)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		testProductionEnv()...,
	)
	cmd.Env = append(cmd.Env,
		"COMPOSE_PS_CMD="+mark("compose-ps"),
		"VERSION_CMD="+mark("version"),
		"HEALTHZ_CMD="+mark("healthz"),
		"OPENAPI_CMD="+mark("openapi"),
		"OPENAPI_VALIDATE_CMD="+mark("openapi-validate"),
		"SMOKE_CMD="+mark("smoke"),
		"PIPELINE_E2E_CMD="+mark("pipeline-e2e"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("post deploy verify script failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(stepsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	want := strings.Join([]string{"compose-ps", "version", "healthz", "openapi", "openapi-validate", "smoke", "pipeline-e2e"}, "\n")
	if got != want {
		t.Fatalf("steps = %q, want %q", got, want)
	}
	if !strings.Contains(string(output), "post deploy verify completed") {
		t.Fatalf("output should report completion, got: %s", output)
	}
}

func TestPostDeployVerifyValidatesOpenAPIContent(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh"))
	if err != nil {
		t.Fatalf("read post deploy verify script: %v", err)
	}
	script := string(content)
	for _, required := range []string{
		"OPENAPI_VALIDATE_CMD",
		"OPENAPI_SPEC_SOURCE",
		"scripts/validate-openapi-runtime.py",
		"python3",
		"shell_quote \"$OPENAPI_SPEC_SOURCE\"",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("post deploy verify must validate OpenAPI content marker %q", required)
		}
	}
	validatorContent, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "validate-openapi-runtime.py"))
	if err != nil {
		t.Fatalf("read OpenAPI runtime validator: %v", err)
	}
	validator := string(validatorContent)
	for _, required := range []string{
		"/memory/turn-event",
		"/memory/search",
		"/memory/qdrant/status",
		"missing openapi path",
	} {
		if !strings.Contains(validator, required) {
			t.Fatalf("OpenAPI runtime validator missing marker %q", required)
		}
	}
}

func TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh"))
	if err != nil {
		t.Fatalf("read post deploy verify script: %v", err)
	}
	script := string(content)
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "PIPELINE_E2E_CMD=") && strings.Contains(line, "SMOKE_POSTGRES_DSN") {
			t.Fatalf("post deploy verify must not inline POSTGRES_DSN into PIPELINE_E2E_CMD: %s", line)
		}
	}
	if !strings.Contains(script, "docker exec deploy-memory-api-1 printenv POSTGRES_DSN") {
		t.Fatal("post deploy verify must still source POSTGRES_DSN from the running API container")
	}
}

func TestPostDeployVerifyRunsPipelineSmokeInsideComposeNetwork(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh"))
	if err != nil {
		t.Fatalf("read post deploy verify script: %v", err)
	}
	script := string(content)
	for _, required := range []string{
		"docker run --rm --network deploy_default",
		"-e SMOKE_API_URL=http://memory-api:18081/healthz",
		"-e SMOKE_QDRANT_URL=http://qdrant:6333",
		"golang:1.25-bookworm go run ./cmd/memory-smoke",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("post deploy pipeline e2e must run smoke inside compose network, missing %q", required)
		}
	}
	if strings.Contains(script, "\n    make smoke\n") {
		t.Fatal("post deploy pipeline e2e must not call host make smoke because host Go bypasses the compose network")
	}
}

func TestPostDeployVerifySetsWebURLForComposeNetworkPipeline(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh"))
	if err != nil {
		t.Fatalf("read post deploy verify script: %v", err)
	}
	script := string(content)
	if !strings.Contains(script, "-e SMOKE_WEB_URL=http://memory-web:18080") {
		t.Fatal("post deploy pipeline e2e must use memory-web URL inside compose network")
	}
}

func TestPostDeployVerifySetsMCPURLForComposeNetworkPipeline(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh"))
	if err != nil {
		t.Fatalf("read post deploy verify script: %v", err)
	}
	script := string(content)
	if !strings.Contains(script, "-e SMOKE_MCP_URL=http://memory-mcp:18082") {
		t.Fatal("post deploy pipeline e2e must use memory-mcp URL inside compose network")
	}
}

func TestPostDeployVerifyUsesDedicatedLogDir(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh")
	logDir := filepath.Join(t.TempDir(), "post-deploy-logs")
	stepsPath := filepath.Join(t.TempDir(), "steps.log")
	mark := func(name string) string {
		return "printf '%s\\n' " + name + " >> " + shellQuote(stepsPath)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		testProductionEnv()...,
	)
	cmd.Env = append(cmd.Env,
		"LOG_DIR="+logDir,
		"COMPOSE_PS_CMD="+mark("compose-ps"),
		"VERSION_CMD="+mark("version"),
		"HEALTHZ_CMD="+mark("healthz"),
		"OPENAPI_CMD="+mark("openapi"),
		"OPENAPI_VALIDATE_CMD="+mark("openapi-validate"),
		"SMOKE_CMD="+mark("smoke"),
		"PIPELINE_E2E_CMD="+mark("pipeline-e2e"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("post deploy verify script failed: %v\n%s", err, output)
	}
	for _, name := range []string{"compose-ps", "version", "healthz", "openapi", "openapi-validate", "smoke", "pipeline-e2e"} {
		if _, err := os.Stat(filepath.Join(logDir, name+".log")); err != nil {
			t.Fatalf("expected log for %s in LOG_DIR: %v", name, err)
		}
	}
}

func TestPostDeployVerifyRestrictsExistingLogDirPermissions(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh")
	logDir := filepath.Join(t.TempDir(), "post-deploy-logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stepsPath := filepath.Join(t.TempDir(), "steps.log")
	mark := func(name string) string {
		return "printf '%s\\n' " + name + " >> " + shellQuote(stepsPath)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		testProductionEnv()...,
	)
	cmd.Env = append(cmd.Env,
		"LOG_DIR="+logDir,
		"COMPOSE_PS_CMD="+mark("compose-ps"),
		"VERSION_CMD="+mark("version"),
		"HEALTHZ_CMD="+mark("healthz"),
		"OPENAPI_CMD="+mark("openapi"),
		"OPENAPI_VALIDATE_CMD="+mark("openapi-validate"),
		"SMOKE_CMD="+mark("smoke"),
		"PIPELINE_E2E_CMD="+mark("pipeline-e2e"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("post deploy verify script failed: %v\n%s", err, output)
	}
	info, err := os.Stat(logDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("LOG_DIR mode = %o, want 700", got)
	}
}

func TestPostDeployVerifyRejectsSymlinkLogDir(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh")
	tempDir := t.TempDir()
	targetDir := filepath.Join(tempDir, "target")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(tempDir, "post-deploy-logs-link")
	if err := os.Symlink(targetDir, logDir); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		testProductionEnv()...,
	)
	cmd.Env = append(cmd.Env,
		"LOG_DIR="+logDir,
		"COMPOSE_PS_CMD=printf compose-ok",
		"VERSION_CMD=printf version-ok",
		"HEALTHZ_CMD=printf healthz-ok",
		"OPENAPI_CMD=printf openapi-ok",
		"SMOKE_CMD=printf smoke-ok",
		"PIPELINE_E2E_CMD=printf pipeline-ok",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("post deploy verify script succeeded, want symlink LOG_DIR rejection")
	}
	if !strings.Contains(string(output), "LOG_DIR must not be a symlink") {
		t.Fatalf("output should explain symlink rejection, got: %s", output)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "compose-ps.log")); !os.IsNotExist(err) {
		t.Fatalf("script wrote through symlink target, stat error = %v", err)
	}
}

func TestPostDeployVerifyPrintsLogDirOnFailure(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh")
	logDir := filepath.Join(t.TempDir(), "post-deploy-logs")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		testProductionEnv()...,
	)
	cmd.Env = append(cmd.Env,
		"LOG_DIR="+logDir,
		"COMPOSE_PS_CMD=printf compose-ok",
		"VERSION_CMD=printf version-ok",
		"HEALTHZ_CMD=false",
		"OPENAPI_CMD=printf openapi-ok",
		"SMOKE_CMD=printf smoke-ok",
		"PIPELINE_E2E_CMD=printf pipeline-ok",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("post deploy verify script succeeded, want healthz failure")
	}
	if !strings.Contains(string(output), "post deploy verify logs: "+logDir) {
		t.Fatalf("failure output should include log dir %q, got: %s", logDir, output)
	}
	if _, err := os.Stat(filepath.Join(logDir, "healthz.log")); err != nil {
		t.Fatalf("expected failed step log in LOG_DIR: %v", err)
	}
}

func TestPostDeployVerifyCapturesStderrInStepLog(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh")
	logDir := filepath.Join(t.TempDir(), "post-deploy-logs")
	stderrMarker := "stderr-sensitive-marker"

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		testProductionEnv()...,
	)
	cmd.Env = append(cmd.Env,
		"LOG_DIR="+logDir,
		"COMPOSE_PS_CMD=printf compose-ok",
		"VERSION_CMD=printf version-ok",
		"HEALTHZ_CMD=printf "+shellQuote(stderrMarker)+" >&2; false",
		"OPENAPI_CMD=printf openapi-ok",
		"SMOKE_CMD=printf smoke-ok",
		"PIPELINE_E2E_CMD=printf pipeline-ok",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("post deploy verify script succeeded, want healthz failure")
	}
	if strings.Contains(string(output), stderrMarker) {
		t.Fatalf("stderr marker must be captured in step log, not terminal output: %s", output)
	}
	logContent, readErr := os.ReadFile(filepath.Join(logDir, "healthz.log"))
	if readErr != nil {
		t.Fatalf("read healthz log: %v", readErr)
	}
	if !strings.Contains(string(logContent), stderrMarker) {
		t.Fatalf("healthz log missing stderr marker %q: %s", stderrMarker, logContent)
	}
}

func TestPostDeployVerifyDoesNotUseFixedTmpLogPath(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh"))
	if err != nil {
		t.Fatalf("read post deploy verify script: %v", err)
	}
	if strings.Contains(string(content), "/tmp/memory-os-post-deploy-") {
		t.Fatal("post deploy verify must not write logs to predictable fixed /tmp paths")
	}
}

func TestMakefileExposesPostDeployVerifyTarget(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)
	if !strings.Contains(makefile, "post-deploy-verify:") {
		t.Fatal("Makefile missing post-deploy-verify target")
	}
	if !strings.Contains(makefile, "scripts/post-deploy-verify.sh") {
		t.Fatal("post-deploy-verify target must call scripts/post-deploy-verify.sh")
	}
}

func TestMakefileExposesBackupRestoreDryRunTarget(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)
	if !strings.Contains(makefile, "backup-restore-dry-run:") {
		t.Fatal("Makefile missing backup-restore-dry-run target")
	}
	for _, required := range []string{
		"DRY_RUN=1 scripts/backup.sh",
		"BACKUP_DIR=$$backup_dir DRY_RUN=1 scripts/restore.sh",
		"backup-restore dry-run completed",
	} {
		if !strings.Contains(makefile, required) {
			t.Fatalf("backup-restore-dry-run target missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"CONFIRM_RESTORE=I_UNDERSTAND",
		"DRY_RUN=0 scripts/restore.sh",
	} {
		if strings.Contains(makefile, forbidden) {
			t.Fatalf("backup-restore-dry-run target must not allow real restore marker %q", forbidden)
		}
	}
}

func TestMakefileExposesRestoreRehearsalDryRunTarget(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)
	if !strings.Contains(makefile, "restore-rehearsal-dry-run:") {
		t.Fatal("Makefile missing restore-rehearsal-dry-run target")
	}
	for _, required := range []string{
		"BACKUP_DIR is required",
		"RESTORE_REHEARSAL_MODE=dry-run bash scripts/restore-rehearsal.sh",
	} {
		if !strings.Contains(makefile, required) {
			t.Fatalf("restore-rehearsal-dry-run target missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE",
		"CONFIRM_RESTORE=I_UNDERSTAND",
		"RESTORE_REHEARSAL_MODE=real",
	} {
		if strings.Contains(makefile, forbidden) {
			t.Fatalf("restore-rehearsal-dry-run target must not include real restore marker %q", forbidden)
		}
	}
}

func TestMakefileExposesRestoreRehearsalPreflightTarget(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)
	if !strings.Contains(makefile, "restore-rehearsal-preflight:") {
		t.Fatal("Makefile missing restore-rehearsal-preflight target")
	}
	for _, required := range []string{
		"BACKUP_DIR is required",
		"bash scripts/restore-rehearsal-preflight.sh",
	} {
		if !strings.Contains(makefile, required) {
			t.Fatalf("restore-rehearsal-preflight target missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE",
		"RESTORE_REHEARSAL_MODE=real",
		"down -v",
	} {
		if strings.Contains(makefile, forbidden) {
			t.Fatalf("restore-rehearsal-preflight target must not include destructive marker %q", forbidden)
		}
	}
}

func TestDockerCleanupPlanScriptIsDryRunOnly(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "docker-cleanup-plan.sh")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read docker cleanup plan script: %v", err)
	}
	script := string(content)

	for _, required := range []string{
		"DRY_RUN_ONLY=1",
		"docker system df",
		"docker ps --format",
		"docker image ls --filter dangling=true",
		"docker volume prune is intentionally excluded",
		"docker image prune -f",
		"docker image prune -a --filter \"until=24h\" -f",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("docker cleanup plan script missing safety marker %q", required)
		}
	}

	for _, forbidden := range []string{
		"\ndocker image prune",
		"\ndocker system prune",
		"\ndocker volume prune",
		"\ndocker container prune",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("docker cleanup plan script must not execute destructive command %q", forbidden)
		}
	}
}

func TestMakefileExposesDockerCleanupPlanTarget(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)
	if !strings.Contains(makefile, "docker-cleanup-plan:") {
		t.Fatal("Makefile missing docker-cleanup-plan target")
	}
	if !strings.Contains(makefile, "bash scripts/docker-cleanup-plan.sh") {
		t.Fatal("docker-cleanup-plan target must call scripts/docker-cleanup-plan.sh through bash")
	}
	if strings.Contains(makefile, "docker-cleanup-plan:\n\tdocker image prune") {
		t.Fatal("docker-cleanup-plan target must not directly execute docker image prune")
	}
}

func TestDockerCleanupImagesScriptDryRunWritesAuditableCommand(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "docker-cleanup-images.sh")
	auditDir := filepath.Join(t.TempDir(), "docker-cleanup-audit")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=1",
		"DOCKER_IMAGE_CLEANUP_MODE=dangling",
		"DOCKER_CLEANUP_AUDIT_DIR="+auditDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker cleanup dry-run failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "docker image cleanup dry-run completed") {
		t.Fatalf("output should report dry-run completion, got: %s", output)
	}
	commandPath := filepath.Join(auditDir, "docker-image-cleanup.command")
	content, err := os.ReadFile(commandPath)
	if err != nil {
		t.Fatalf("read cleanup command audit: %v", err)
	}
	if got := strings.TrimSpace(string(content)); got != "docker image prune -f" {
		t.Fatalf("cleanup command = %q, want dangling prune", got)
	}
}

func TestDockerCleanupImagesScriptRejectsRealRunWithoutConfirmation(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "docker-cleanup-images.sh")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=0",
		"DOCKER_IMAGE_CLEANUP_MODE=dangling",
		"DOCKER_CLEANUP_AUDIT_DIR="+t.TempDir(),
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("docker cleanup real run without confirmation succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(string(output), "CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE") {
		t.Fatalf("output should explain confirmation requirement, got: %s", output)
	}
}

func TestDockerCleanupImagesScriptSupportsOnlyImagePruneModes(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "docker-cleanup-images.sh")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read docker cleanup images script: %v", err)
	}
	script := string(content)
	for _, required := range []string{
		"DOCKER_IMAGE_CLEANUP_MODE",
		"docker image prune -f",
		"docker image prune -a --filter \"until=24h\" -f",
		"CONFIRM_DOCKER_IMAGE_CLEANUP",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("docker cleanup images script missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"docker system prune",
		"docker volume prune",
		"docker container prune",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("docker cleanup images script must not contain %q", forbidden)
		}
	}
}

func TestMakefileExposesDockerCleanupImagesTarget(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)
	if !strings.Contains(makefile, "docker-cleanup-images:") {
		t.Fatal("Makefile missing docker-cleanup-images target")
	}
	if !strings.Contains(makefile, "bash scripts/docker-cleanup-images.sh") {
		t.Fatal("docker-cleanup-images target must call scripts/docker-cleanup-images.sh through bash")
	}
	if strings.Contains(makefile, "docker-cleanup-images:\n\tdocker image prune") {
		t.Fatal("docker-cleanup-images target must not directly execute docker image prune")
	}
}

func TestInstallDockerCleanupCronScriptIsSafeByDefault(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "install-docker-cleanup-cron.sh")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read docker cleanup cron installer: %v", err)
	}
	script := string(content)
	for _, required := range []string{
		"DRY_RUN=\"${DRY_RUN:-1}\"",
		"DOCKER_IMAGE_CLEANUP_MODE=dangling",
		"CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE",
		"docker-cleanup-images",
		"CONFIRM_CRON_INSTALL=I_UNDERSTAND",
		"memory-os docker dangling image cleanup",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("docker cleanup cron installer missing safety marker %q", required)
		}
	}
	for _, forbidden := range []string{
		"docker system prune",
		"docker volume prune",
		"docker container prune",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("docker cleanup cron installer must not contain %q", forbidden)
		}
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=1",
		"CRON_SCHEDULE=5 4 * * *",
		"PROJECT_DIR=/opt/memory-os",
		"MAKE_BIN=/usr/bin/make",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker cleanup cron dry-run failed: %v\n%s", err, output)
	}
	out := string(output)
	for _, required := range []string{
		"5 4 * * * cd /opt/memory-os",
		"DRY_RUN=0 DOCKER_IMAGE_CLEANUP_MODE=dangling",
		"/usr/bin/make docker-cleanup-images",
		"docker cleanup cron dry-run completed",
	} {
		if !strings.Contains(out, required) {
			t.Fatalf("docker cleanup cron dry-run output missing %q:\n%s", required, out)
		}
	}
}

func TestInstallDockerCleanupCronRejectsRealRunWithoutConfirmation(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "install-docker-cleanup-cron.sh")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DRY_RUN=0",
		"CRONTAB_CMD=true",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("docker cleanup cron real install without confirmation succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(string(output), "CONFIRM_CRON_INSTALL=I_UNDERSTAND") {
		t.Fatalf("output should explain confirmation requirement, got: %s", output)
	}
}

func TestMakefileExposesInstallDockerCleanupCronTarget(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)
	if !strings.Contains(makefile, "install-docker-cleanup-cron:") {
		t.Fatal("Makefile missing install-docker-cleanup-cron target")
	}
	if !strings.Contains(makefile, "bash scripts/install-docker-cleanup-cron.sh") {
		t.Fatal("install-docker-cleanup-cron target must call scripts/install-docker-cleanup-cron.sh through bash")
	}
}

func TestProductionCommandsLoadEnvironmentWithoutInliningSecrets(t *testing.T) {
	repoRoot := findRepoRoot(t)
	makefileContent, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	postDeployContent, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "post-deploy-verify.sh"))
	if err != nil {
		t.Fatalf("read post deploy verify script: %v", err)
	}
	loaderContent, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "load-prod-env.sh"))
	if err != nil {
		t.Fatalf("read production env loader: %v", err)
	}
	makefile := string(makefileContent)
	postDeploy := string(postDeployContent)
	loader := string(loaderContent)

	if !strings.Contains(makefile, ". scripts/load-prod-env.sh") {
		t.Fatal("prod-up must source scripts/load-prod-env.sh before docker-compose")
	}
	if !strings.Contains(makefile, "prod-up:\n\t. scripts/load-prod-env.sh && \\") {
		t.Fatal("prod-up must source scripts/load-prod-env.sh before production preflight")
	}
	if !strings.Contains(makefile, ". scripts/load-build-info.sh") {
		t.Fatal("prod-up must source scripts/load-build-info.sh before docker-compose")
	}
	if !strings.Contains(makefile, "ALLOW_EXISTING_DEPLOYMENT=1 scripts/preflight.sh && \\") {
		t.Fatal("prod-up must run preflight with ALLOW_EXISTING_DEPLOYMENT=1 before docker-compose build")
	}
	if !strings.Contains(makefile, "DRY_RUN=0 DOCKER_IMAGE_CLEANUP_MODE=dangling CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE bash scripts/docker-cleanup-images.sh") {
		t.Fatal("prod-up must prune dangling Docker images through bash after successful compose build")
	}
	if !strings.Contains(postDeploy, ". scripts/load-prod-env.sh") {
		t.Fatal("post-deploy-verify must source scripts/load-prod-env.sh before compose checks")
	}
	if !strings.Contains(makefile, "backup:\n\t. scripts/load-prod-env.sh && export COMPOSE_PROJECT_NAME=deploy && scripts/backup.sh") {
		t.Fatal("backup target must source scripts/load-prod-env.sh and export COMPOSE_PROJECT_NAME=deploy before scripts/backup.sh")
	}
	if !strings.Contains(makefile, "restore:\n\t. scripts/load-prod-env.sh && export COMPOSE_PROJECT_NAME=deploy && scripts/restore.sh") {
		t.Fatal("restore target must source scripts/load-prod-env.sh and export COMPOSE_PROJECT_NAME=deploy before scripts/restore.sh")
	}
	for _, forbidden := range []string{
		"echo $POSTGRES_PASSWORD",
		"echo $LLM_API_KEY",
		"echo $SECRET_VAULT_KEY_B64",
		"cat > .env",
		"tee .env",
	} {
		if strings.Contains(loader, forbidden) {
			t.Fatalf("production env loader must not print or write secrets, found %q", forbidden)
		}
	}
	for _, required := range []string{
		"MEMORY_OS_ENV_FILE",
		"docker inspect deploy-memory-api-1",
		"docker inspect deploy-postgres-1",
	} {
		if !strings.Contains(loader, required) {
			t.Fatalf("production env loader missing safety marker %q", required)
		}
	}
}

func TestT480DirectSyncWorkflowIsSafeByDefault(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "sync-t480.sh")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read T480 sync script: %v", err)
	}
	script := string(content)

	for _, required := range []string{
		`TARGET_HOST="${TARGET_HOST:-thinkpad}"`,
		`TARGET_DIR="${TARGET_DIR:-/opt/memory-os}"`,
		`rsync`,
		`--no-owner`,
		`--no-group`,
		`--exclude=.git/`,
		`--exclude=.env`,
		`--exclude=.env.*`,
		`--include=.env.example`,
		`--exclude=frontend/node_modules/`,
		`--exclude=frontend/.nuxt/`,
		`--exclude=frontend/.output/`,
		`--exclude=.gocache/`,
		`--exclude=.playwright-mcp/`,
		`--exclude=artifacts/`,
		`--exclude=docs/`,
		`--exclude=specs/`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("T480 sync script missing safety marker %q", required)
		}
	}
	if strings.Contains(script, "--delete") {
		t.Fatal("T480 sync script must not delete remote files by default")
	}
	includeExample := strings.Index(script, `--include=.env.example`)
	excludeEnvPattern := strings.Index(script, `--exclude=.env.*`)
	if includeExample < 0 || excludeEnvPattern < 0 || includeExample > excludeEnvPattern {
		t.Fatal("T480 sync script must include .env.example before excluding .env.*")
	}
}

func TestMakefileExposesT480DirectWorkflowTargets(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)

	for _, required := range []string{
		"t480-sync:",
		"bash scripts/sync-t480.sh",
		"t480-build-check:",
		"make test && make build-web",
		"t480-deploy:",
		"make prod-up && make post-deploy-verify",
	} {
		if !strings.Contains(makefile, required) {
			t.Fatalf("Makefile missing T480 workflow marker %q", required)
		}
	}
	if strings.Contains(makefile, "git pull") {
		t.Fatal("daily T480 workflow must not require git pull")
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

func testProductionEnv() []string {
	return []string{
		"POSTGRES_PASSWORD=test-postgres-password",
		"LLM_BASE_URL=http://llm.example.test",
		"LLM_API_KEY=test-llm-key",
	}
}
