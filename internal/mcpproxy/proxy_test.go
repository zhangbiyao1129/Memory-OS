package mcpproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"memory-os/internal/workspace"
)

type fakeDetector struct {
	called bool
	id     workspace.Identity
	err    error
}

func (f *fakeDetector) Detect(_ context.Context, _ string) (workspace.Identity, error) {
	f.called = true
	return f.id, f.err
}

func TestProxyCallToolInjectsWorkspaceAndAgentForMemorySearch(t *testing.T) {
	detector := &fakeDetector{id: workspace.Identity{
		CWD:       "/work/memory-os",
		GitRoot:   "/work/memory-os",
		GitRemote: "gitlab.example.com/team/memory-os",
		GitBranch: "main",
		GitCommit: "abc123",
	}}
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/call" {
			t.Fatalf("path = %s, want /tools/call", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want bearer token", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"code":"ok","search":{"request_id":"r1","context":"memory result"}}`))
	}))
	defer server.Close()

	proxy := New(Config{MCPURL: server.URL, Token: "test-token", AgentID: "codex", CWD: "/work/memory-os", Detector: detector})
	result, err := proxy.CallTool(context.Background(), "memory_search", map[string]any{"query": "deploy"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}

	if result.IsError {
		t.Fatalf("result.IsError = true, text = %s", result.Text)
	}
	if !strings.Contains(result.Text, "memory result") {
		t.Fatalf("result text = %s, want proxied search payload", result.Text)
	}
	arguments := received["arguments"].(map[string]any)
	workspaceArg := arguments["workspace"].(map[string]any)
	if workspaceArg["git_remote"] != "gitlab.example.com/team/memory-os" {
		t.Fatalf("workspace git_remote = %#v, want credential-free source key", workspaceArg["git_remote"])
	}
	actor := arguments["actor"].(map[string]any)
	if actor["agent_id"] != "codex" {
		t.Fatalf("actor agent_id = %#v, want codex", actor["agent_id"])
	}
	if !detector.called {
		t.Fatal("workspace detector was not called")
	}
}

func TestProxyCallToolKeepsExplicitProjectActorWithoutGitDetection(t *testing.T) {
	detector := &fakeDetector{id: workspace.Identity{GitRemote: "gitlab.example.com/team/unused"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var received map[string]any
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		arguments := received["arguments"].(map[string]any)
		if _, ok := arguments["workspace"]; ok {
			t.Fatalf("workspace was injected despite explicit project actor: %#v", arguments["workspace"])
		}
		_, _ = w.Write([]byte(`{"code":"ok","search":{"request_id":"r2","context":"explicit project"}}`))
	}))
	defer server.Close()

	proxy := New(Config{MCPURL: server.URL, Token: "test-token", AgentID: "codex", Detector: detector})
	_, err := proxy.CallTool(context.Background(), "memory_search", map[string]any{
		"query": "deploy",
		"actor": map[string]any{"project_id": "project_1", "agent_id": "claude"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if detector.called {
		t.Fatal("workspace detector was called even though project_id was explicit")
	}
}
