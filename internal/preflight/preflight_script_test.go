package preflight_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightScriptPassesWithFreePorts(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "preflight.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("preflight script must exist at %s: %v", scriptPath, err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"SS_CMD=printf ''",
		"DF_CMD=printf 'Filesystem 1K-blocks Used Available Use%% Mounted on\\n/dev/disk 200000000 100000000 80000000 56%% /\\n'",
		"DOCKER_CMD=printf 'Docker version 27.0.0\\n'",
		"COMPOSE_CMD=printf 'docker-compose version 1.29.2\\n'",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("preflight failed unexpectedly: %v\n%s", err, output)
	}
	for _, want := range []string{"preflight ok", "18080", "18081", "18082", "18083"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestPreflightScriptRejectsPortConflict(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "preflight.sh")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"SS_CMD=printf 'LISTEN 0 4096 0.0.0.0:18081 0.0.0.0:* users:(\"other\",pid=1,fd=3)\\n'",
		"DF_CMD=printf 'Filesystem 1K-blocks Used Available Use%% Mounted on\\n/dev/disk 200000000 100000000 80000000 56%% /\\n'",
		"DOCKER_CMD=printf 'Docker version 27.0.0\\n'",
		"COMPOSE_CMD=printf 'docker-compose version 1.29.2\\n'",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("preflight succeeded with port conflict:\n%s", output)
	}
	if !strings.Contains(string(output), "port 18081 is already in use") {
		t.Fatalf("output should mention port conflict, got:\n%s", output)
	}
}

func TestPreflightScriptAllowsExistingComposeDeployment(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "preflight.sh")

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ALLOW_EXISTING_DEPLOYMENT=1",
		"SS_CMD=printf 'LISTEN 0 4096 0.0.0.0:18080 0.0.0.0:* users:(\"docker-proxy\",pid=1,fd=3)\\n'",
		"COMPOSE_PS_CMD=printf 'deploy-memory-web-1 0.0.0.0:18080->18080/tcp\\n'",
		"DF_CMD=printf 'Filesystem 1K-blocks Used Available Use%% Mounted on\\n/dev/disk 200000000 100000000 80000000 56%% /\\n'",
		"DOCKER_CMD=printf 'Docker version 27.0.0\\n'",
		"COMPOSE_CMD=printf 'docker-compose version 1.29.2\\n'",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("preflight should allow current compose deployment: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "port 18080 is already used by current deployment") {
		t.Fatalf("output should explain allowed existing deployment, got:\n%s", output)
	}
}

func TestPreflightScriptLoadsProductionEnvForDefaultComposePS(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "preflight.sh"))
	if err != nil {
		t.Fatalf("read preflight script: %v", err)
	}
	script := string(content)

	for _, required := range []string{
		"load_compose_env_for_ps()",
		". scripts/load-prod-env.sh >/dev/null 2>&1 || true",
		"load_compose_env_for_ps",
		`COMPOSE_PS_CMD="${COMPOSE_PS_CMD:-docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps}"`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("preflight script missing production compose env marker %q", required)
		}
	}
}

func TestMakefileProdUpMockExportsDefaultEnvForCompose(t *testing.T) {
	repoRoot := findRepoRoot(t)
	content, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)

	for _, required := range []string{
		"if [[ -f .env.production || -f .env ]] || docker inspect deploy-memory-api-1 >/dev/null 2>&1; then",
		". scripts/load-prod-env.sh;",
		"export POSTGRES_PASSWORD=$${POSTGRES_PASSWORD:-replace-me-mock-password};",
		"export LLM_BASE_URL=$${LLM_BASE_URL:-http://memory-llm-mock:11434};",
		"export LLM_API_KEY=$${LLM_API_KEY:-memory-llm-mock-key};",
		"export SECRET_VAULT_KEY_ID=$${SECRET_VAULT_KEY_ID:-dev-vault-id};",
		"export SECRET_VAULT_KEY_B64=$${SECRET_VAULT_KEY_B64:-dGVzdC1rZXktMTIzNDU2Nw==};",
	} {
		if !strings.Contains(makefile, required) {
			t.Fatalf("prod-up-mock must export default env for docker-compose interpolation, missing %q", required)
		}
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
