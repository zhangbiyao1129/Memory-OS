package secretscan_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretScanAllowsPlaceholders(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "secret-scan.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("secret scan script must exist at %s: %v", scriptPath, err)
	}

	scanRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(scanRoot, ".env.example"), []byte("LLM_API_KEY=replace-me\nPOSTGRES_PASSWORD=replace-me-local-only\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "SCAN_ROOT="+scanRoot)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("secret scan should allow placeholders: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "secret scan ok") {
		t.Fatalf("output should report success, got:\n%s", output)
	}
}

func TestSecretScanRejectsSensitiveMarkers(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "secret-scan.sh")
	scanRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(scanRoot, "leak.txt"), []byte("token=sk-test-redacted-example\npat=pk_1234567890abcdef1234567890abcdef\n-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "SCAN_ROOT="+scanRoot)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("secret scan succeeded despite sensitive markers:\n%s", output)
	}
	for _, want := range []string{"potential secret", "leak.txt"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("output missing %q:\n%s", want, output)
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
