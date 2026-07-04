package workspace

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeCommandRunner struct {
	outputs map[string]string
	errors  map[string]error
	calls   []string
}

func (f *fakeCommandRunner) Output(_ context.Context, _ string, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if err := f.errors[key]; err != nil {
		return "", err
	}
	return f.outputs[key], nil
}

func TestGitDetectorDetectsWorkspaceWithoutLeakingRemoteCredentials(t *testing.T) {
	runner := &fakeCommandRunner{outputs: map[string]string{
		"git rev-parse --show-toplevel":      "/work/memory-os\n",
		"git config --get remote.origin.url": "https://user:secret-token@gitlab.example.com/team/memory-os.git\n",
		"git branch --show-current":          "main\n",
		"git rev-parse HEAD":                 "abc123\n",
	}, errors: map[string]error{}}
	detector := NewGitDetector(runner)

	identity, err := detector.Detect(context.Background(), "/work/memory-os/web")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	if identity.CWD != "/work/memory-os/web" || identity.GitRoot != "/work/memory-os" {
		t.Fatalf("workspace paths = %#v, want cwd and git root", identity)
	}
	if identity.GitRemote != "gitlab.example.com/team/memory-os" {
		t.Fatalf("GitRemote = %q, want credential-free source key", identity.GitRemote)
	}
	if identity.GitBranch != "main" || identity.GitCommit != "abc123" {
		t.Fatalf("git metadata = %#v, want branch main and commit abc123", identity)
	}
	if reflect.DeepEqual(runner.calls, []string{}) {
		t.Fatal("Detect() did not run git commands")
	}
}

func TestGitDetectorRequiresRemote(t *testing.T) {
	runner := &fakeCommandRunner{outputs: map[string]string{
		"git rev-parse --show-toplevel":      "/work/no-remote\n",
		"git config --get remote.origin.url": "",
	}, errors: map[string]error{
		"git config --get remote.origin.url": errors.New("exit status 1"),
	}}
	detector := NewGitDetector(runner)

	_, err := detector.Detect(context.Background(), "/work/no-remote")
	if err == nil {
		t.Fatal("Detect() error = nil, want missing remote error")
	}
}
