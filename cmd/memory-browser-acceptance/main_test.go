package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunProvisionWritesSessionStateFile(t *testing.T) {
	restoreProvision := provisionBrowserAcceptanceSession
	restoreCleanup := cleanupBrowserAcceptanceSession
	t.Cleanup(func() {
		provisionBrowserAcceptanceSession = restoreProvision
		cleanupBrowserAcceptanceSession = restoreCleanup
	})

	statePath := filepath.Join(t.TempDir(), "browser-session.json")
	provisionBrowserAcceptanceSession = func(_ context.Context, dsn, namePrefix, agentID string, ttl time.Duration) (browserAcceptanceState, error) {
		if dsn != "postgres://memory-os" {
			t.Fatalf("dsn = %q, want postgres://memory-os", dsn)
		}
		if namePrefix != "browser-acceptance" {
			t.Fatalf("namePrefix = %q, want browser-acceptance", namePrefix)
		}
		if agentID != "codex" {
			t.Fatalf("agentID = %q, want codex", agentID)
		}
		if ttl != 45*time.Minute {
			t.Fatalf("ttl = %s, want 45m", ttl)
		}
		return browserAcceptanceState{
			UserEmail:        "browser@memory.local",
			WriteToken:       "pat-write",
			SearchToken:      "pat-read",
			AdapterToken:     "adapter-token",
			UserID:           "user_1",
			OrgID:            "org_1",
			ProjectID:        "project_1",
			AgentID:          "codex",
			PermissionLabel:  "project:project_1:read",
			AdapterTokenID:   "adapter_1",
			WriteTokenID:     "pat_write_1",
			SearchTokenID:    "pat_read_1",
			ProvisionedAtUTC: "2026-07-03T15:00:00Z",
		}, nil
	}
	cleanupBrowserAcceptanceSession = func(context.Context, string, browserAcceptanceState) error {
		t.Fatal("cleanup should not be called during provision")
		return nil
	}

	out, err := run([]string{"provision", "--dsn", "postgres://memory-os", "--state", statePath, "--ttl", "45m"})
	if err != nil {
		t.Fatalf("run provision error = %v", err)
	}
	if !strings.Contains(out, statePath) {
		t.Fatalf("output = %q, want state path", out)
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var state browserAcceptanceState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if state.UserEmail != "browser@memory.local" || state.SearchToken != "pat-read" || state.WriteToken != "pat-write" {
		t.Fatalf("state = %#v, want provisioned session tokens and email", state)
	}
}

func TestRunCleanupRevokesSessionAndRemovesStateFile(t *testing.T) {
	restoreProvision := provisionBrowserAcceptanceSession
	restoreCleanup := cleanupBrowserAcceptanceSession
	t.Cleanup(func() {
		provisionBrowserAcceptanceSession = restoreProvision
		cleanupBrowserAcceptanceSession = restoreCleanup
	})

	statePath := filepath.Join(t.TempDir(), "browser-session.json")
	state := browserAcceptanceState{
		UserEmail:      "browser@memory.local",
		SearchToken:    "pat-read",
		WriteToken:     "pat-write",
		AdapterTokenID: "adapter_1",
		WriteTokenID:   "pat_write_1",
		SearchTokenID:  "pat_read_1",
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(statePath, encoded, 0o600); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	called := false
	provisionBrowserAcceptanceSession = func(context.Context, string, string, string, time.Duration) (browserAcceptanceState, error) {
		t.Fatal("provision should not be called during cleanup")
		return browserAcceptanceState{}, nil
	}
	cleanupBrowserAcceptanceSession = func(_ context.Context, dsn string, got browserAcceptanceState) error {
		called = true
		if dsn != "postgres://memory-os" {
			t.Fatalf("dsn = %q, want postgres://memory-os", dsn)
		}
		if got.SearchTokenID != "pat_read_1" || got.WriteTokenID != "pat_write_1" || got.AdapterTokenID != "adapter_1" {
			t.Fatalf("state = %#v, want token ids", got)
		}
		return nil
	}

	out, err := run([]string{"cleanup", "--dsn", "postgres://memory-os", "--state", statePath})
	if err != nil {
		t.Fatalf("run cleanup error = %v", err)
	}
	if !called {
		t.Fatal("cleanup function was not called")
	}
	if !strings.Contains(out, statePath) {
		t.Fatalf("output = %q, want state path", out)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state file should be removed, stat err = %v", err)
	}
}

func TestRunProvisionRequiresStatePath(t *testing.T) {
	_, err := run([]string{"provision", "--dsn", "postgres://memory-os"})
	if err == nil {
		t.Fatal("run provision error = nil, want missing state path")
	}
	if !strings.Contains(err.Error(), "--state is required") {
		t.Fatalf("error = %v, want missing state path", err)
	}
}

func TestRunCleanupRequiresStatePath(t *testing.T) {
	_, err := run([]string{"cleanup", "--dsn", "postgres://memory-os"})
	if err == nil {
		t.Fatal("run cleanup error = nil, want missing state path")
	}
	if !strings.Contains(err.Error(), "--state is required") {
		t.Fatalf("error = %v, want missing state path", err)
	}
}
