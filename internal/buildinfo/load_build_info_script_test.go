package buildinfo_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBuildInfoScriptUsesMetadataFileWhenGitUnavailable(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "load-build-info.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("load build info script must exist at %s: %v", scriptPath, err)
	}

	tempDir := t.TempDir()
	metadataPath := filepath.Join(tempDir, ".build-info.env")
	if err := os.WriteFile(metadataPath, []byte("BUILD_VERSION=0.4.0-sync\nBUILD_COMMIT=abc1234\nBUILD_DIRTY=false\n"), 0o600); err != nil {
		t.Fatalf("write metadata file: %v", err)
	}

	cmd := exec.Command("bash", "-lc", `. "$SCRIPT_PATH"; printf '%s|%s|%s|%s' "$BUILD_VERSION" "$BUILD_COMMIT" "$BUILD_DIRTY" "$BUILD_TIME"`)
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "SCRIPT_PATH="+scriptPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("load build info script failed: %v\n%s", err, output)
	}

	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(parts) != 4 {
		t.Fatalf("unexpected output %q", output)
	}
	if parts[0] != "0.4.0-sync" {
		t.Fatalf("version = %q, want 0.4.0-sync", parts[0])
	}
	if parts[1] != "abc1234" {
		t.Fatalf("commit = %q, want abc1234", parts[1])
	}
	if parts[2] != "false" {
		t.Fatalf("dirty = %q, want false", parts[2])
	}
	if parts[3] == "" || parts[3] == "unknown" {
		t.Fatalf("build time must be populated, got %q", parts[3])
	}
}

func TestLoadBuildInfoScriptPreservesExplicitEnvironmentOverrides(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "load-build-info.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("load build info script must exist at %s: %v", scriptPath, err)
	}

	tempDir := t.TempDir()
	metadataPath := filepath.Join(tempDir, ".build-info.env")
	if err := os.WriteFile(metadataPath, []byte("BUILD_VERSION=0.4.0-sync\nBUILD_COMMIT=filecommit\nBUILD_DIRTY=false\n"), 0o600); err != nil {
		t.Fatalf("write metadata file: %v", err)
	}

	cmd := exec.Command("bash", "-lc", `. "$SCRIPT_PATH"; printf '%s|%s|%s|%s' "$BUILD_VERSION" "$BUILD_COMMIT" "$BUILD_DIRTY" "$BUILD_TIME"`)
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(),
		"SCRIPT_PATH="+scriptPath,
		"BUILD_VERSION=0.4.0-explicit",
		"BUILD_COMMIT=overridecommit",
		"BUILD_DIRTY=true",
		"BUILD_TIME=2026-07-03T13:00:00Z",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("load build info script failed: %v\n%s", err, output)
	}

	got := strings.TrimSpace(string(output))
	want := "0.4.0-explicit|overridecommit|true|2026-07-03T13:00:00Z"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
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
