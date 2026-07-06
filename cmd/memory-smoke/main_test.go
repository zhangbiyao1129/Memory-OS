package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSmokeTimeoutDefaultAllowsColdGoRun(t *testing.T) {
	t.Setenv("SMOKE_TIMEOUT", "")

	if got := smokeTimeout(); got != time.Minute {
		t.Fatalf("smokeTimeout() = %s, want 1m", got)
	}
}

func TestSmokeTimeoutReadsEnvironment(t *testing.T) {
	t.Setenv("SMOKE_TIMEOUT", "2m")

	if got := smokeTimeout(); got != 2*time.Minute {
		t.Fatalf("smokeTimeout() = %s, want 2m", got)
	}
}

func TestResolveSmokeHealthURLDefaults(t *testing.T) {
	got := resolveSmokeHealthURL("")
	if got != "http://localhost:18081/healthz" {
		t.Fatalf("resolveSmokeHealthURL(\"\") = %q, want %q", got, "http://localhost:18081/healthz")
	}
}

func TestResolveSmokeHealthURLRootPath(t *testing.T) {
	got := resolveSmokeHealthURL("http://127.0.0.1:18081")
	if got != "http://127.0.0.1:18081/healthz" {
		t.Fatalf("resolveSmokeHealthURL(root) = %q, want %q", got, "http://127.0.0.1:18081/healthz")
	}
}

func TestResolveSmokeHealthURLKeepsExplicitHealthPath(t *testing.T) {
	got := resolveSmokeHealthURL("http://127.0.0.1:18081/healthz")
	if got != "http://127.0.0.1:18081/healthz" {
		t.Fatalf("resolveSmokeHealthURL(explicit) = %q, want %q", got, "http://127.0.0.1:18081/healthz")
	}
}

func TestResolveSmokeHealthURLKeepsExplicitCustomPath(t *testing.T) {
	got := resolveSmokeHealthURL("http://127.0.0.1:18081/health")
	if got != "http://127.0.0.1:18081/health" {
		t.Fatalf("resolveSmokeHealthURL(explicit custom) = %q, want %q", got, "http://127.0.0.1:18081/health")
	}
}

func TestSmokePostgresDSNFallsBackToPostgresDSN(t *testing.T) {
	t.Setenv("SMOKE_POSTGRES_DSN", "")
	t.Setenv("POSTGRES_DSN", "postgres://memory-os-prod")

	if got := smokePostgresDSN(); got != "postgres://memory-os-prod" {
		t.Fatalf("smokePostgresDSN() = %q, want postgres://memory-os-prod", got)
	}
}

func TestMemorySearchSmokeAcceptsRetrievalNotConfigured(t *testing.T) {
	t.Setenv("SMOKE_REQUIRE_CONFIGURED_RETRIEVAL", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"retrieval_not_configured"}`))
	}))
	defer server.Close()

	if err := memorySearchSmoke(context.Background(), server.URL); err != nil {
		t.Fatalf("memorySearchSmoke() error = %v", err)
	}
}

func TestMemorySearchSmokeAcceptsForbiddenTenantBoundary(t *testing.T) {
	t.Setenv("SMOKE_REQUIRE_CONFIGURED_RETRIEVAL", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"memory_search_forbidden"}`))
	}))
	defer server.Close()

	if err := memorySearchSmoke(context.Background(), server.URL); err != nil {
		t.Fatalf("memorySearchSmoke() error = %v", err)
	}
}

func TestMemorySearchSmokeStrictModeRejectsRetrievalNotConfigured(t *testing.T) {
	t.Setenv("SMOKE_REQUIRE_CONFIGURED_RETRIEVAL", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"retrieval_not_configured"}`))
	}))
	defer server.Close()

	if err := memorySearchSmoke(context.Background(), server.URL); err == nil {
		t.Fatal("memorySearchSmoke() error = nil, want strict retrieval failure")
	}
}

func TestMemorySearchSmokeStrictModeRejectsForbiddenTenantBoundary(t *testing.T) {
	t.Setenv("SMOKE_REQUIRE_CONFIGURED_RETRIEVAL", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"memory_search_forbidden"}`))
	}))
	defer server.Close()

	if err := memorySearchSmoke(context.Background(), server.URL); err == nil {
		t.Fatal("memorySearchSmoke() error = nil, want strict forbidden failure")
	}
}

func TestMemorySearchSmokeStrictModeCanUseProvisionedSearchToken(t *testing.T) {
	t.Setenv("SMOKE_REQUIRE_CONFIGURED_RETRIEVAL", "true")
	t.Setenv("SMOKE_POSTGRES_DSN", "postgres://memory-os-smoke")
	t.Setenv("SMOKE_SEARCH_PAT", "")
	t.Logf("test env token=%q require=%q", os.Getenv("SMOKE_SEARCH_PAT"), os.Getenv("SMOKE_REQUIRE_CONFIGURED_RETRIEVAL"))
	originalProvisioner := provisionPipelineE2EActor
	t.Cleanup(func() { provisionPipelineE2EActor = originalProvisioner })
	provisionedCalled := false
	provisionPipelineE2EActor = func(ctx context.Context, dsn, marker string) (pipelineE2EActor, error) {
		provisionedCalled = true
		if dsn != "postgres://memory-os-smoke" {
			t.Fatalf("provision dsn = %q", dsn)
		}
		if marker == "" {
			t.Fatal("expected non-empty marker")
		}
		return pipelineE2EActor{
			SearchToken: "pat-provisioned",
			Scope: smokeActorScope{
				UserID:          "provisioned_user",
				OrgID:           "provisioned_org",
				ProjectID:       "provisioned_project",
				AgentID:         "codex",
				PermissionLabel: "project:provisioned_project:read",
			},
		}, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pat-provisioned" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if !strings.Contains(fmtAny(payload), "provisioned_user") || !strings.Contains(fmtAny(payload), "provisioned_org") || !strings.Contains(fmtAny(payload), "provisioned_project") || !strings.Contains(fmtAny(payload), "project:provisioned_project:read") {
			t.Fatalf("search request did not use provisioned actor: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"memory-search-smoke-1","context":"ok","results":[{"source":{"kind":"hot_memory"}},{"source":{"kind":"archive_chunk"}}],"rerank_degraded":true}`))
	}))
	defer server.Close()

	if err := memorySearchSmoke(context.Background(), server.URL); err != nil {
		t.Fatalf("memorySearchSmoke() error = %v", err)
	}
	if !provisionedCalled {
		t.Fatal("provision pipeline actor was not called")
	}
}

func TestMemorySearchSmokeUsesConfiguredActorScope(t *testing.T) {
	t.Setenv("SMOKE_SEARCH_USER_ID", "user_configured")
	t.Setenv("SMOKE_SEARCH_ORG_ID", "org_configured")
	t.Setenv("SMOKE_SEARCH_PROJECT_ID", "project_configured")
	t.Setenv("SMOKE_SEARCH_AGENT_ID", "agent_configured")
	t.Setenv("SMOKE_SEARCH_PERMISSION_LABEL", "project:project_configured:read")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode search request: %v", err)
		}
		if !strings.Contains(fmtAny(payload), "org_configured") || !strings.Contains(fmtAny(payload), "project_configured") || !strings.Contains(fmtAny(payload), "project:project_configured:read") {
			t.Fatalf("search request did not use configured actor: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		if payload["request_id"] == "memory-search-smoke-eve" {
			_, _ = w.Write([]byte(`{"request_id":"memory-search-smoke-eve","context":"","results":[],"rerank_degraded":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"request_id":"search","context":"ok","results":[{"text":"ok","source":{"kind":"hot_memory"}},{"text":"ok","source":{"kind":"archive_chunk"}}],"rerank_degraded":true}`))
	}))
	defer server.Close()

	if err := memorySearchSmoke(context.Background(), server.URL); err != nil {
		t.Fatalf("memorySearchSmoke() error = %v", err)
	}
}

func TestMemorySearchSmokeUsesPATWhenConfigured(t *testing.T) {
	t.Setenv("SMOKE_SEARCH_PAT", "pat-search-token")
	t.Logf("test env token=%q require=%q", os.Getenv("SMOKE_SEARCH_PAT"), os.Getenv("SMOKE_REQUIRE_CONFIGURED_RETRIEVAL"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pat-search-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"search","context":"ok","results":[{"text":"ok","source":{"kind":"hot_memory"}},{"text":"ok","source":{"kind":"archive_chunk"}}],"rerank_degraded":true}`))
	}))
	defer server.Close()

	if err := memorySearchSmoke(context.Background(), server.URL); err != nil {
		t.Fatalf("memorySearchSmoke() error = %v", err)
	}
}

func TestTenantGovernanceSmokeUsesProvisionedTokensAndReloadsPersistedState(t *testing.T) {
	t.Setenv("SMOKE_ENABLE_TENANT_GOVERNANCE", "true")
	t.Setenv("SMOKE_POSTGRES_DSN", "postgres://memory-os-smoke")
	originalProvisioner := provisionPipelineE2EActor
	t.Cleanup(func() { provisionPipelineE2EActor = originalProvisioner })

	cleanupCalled := false
	provisionPipelineE2EActor = func(ctx context.Context, dsn, marker string) (pipelineE2EActor, error) {
		if dsn != "postgres://memory-os-smoke" {
			t.Fatalf("provision dsn = %q", dsn)
		}
		if marker != "tenant-governance-smoke" {
			t.Fatalf("provision marker = %q", marker)
		}
		return pipelineE2EActor{
			WriteToken:  "pat-write",
			SearchToken: "pat-read",
			Scope: smokeActorScope{
				UserID:          "owner-user",
				OrgID:           "org-1",
				ProjectID:       "project-1",
				AgentID:         "codex",
				PermissionLabel: "project:project-1:read",
			},
			Cleanup: func(context.Context) error {
				cleanupCalled = true
				return nil
			},
		}, nil
	}

	var seenCreateUser bool
	var seenDisableUser bool
	var seenRoleUpsert bool
	var seenMembershipAdd bool
	var seenMembershipUpdate bool
	var seenMembershipRemove bool
	var userID = "user-managed"
	var createdUserEmail = "tenant-smoke@example.invalid"
	var createdUserDisplayName = "Tenant Smoke User"
	var customRoleName = "tenant_smoke_reader"
	var customRoleLabel = "project:project-1:read"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/memory/tenant/users/create":
			if r.Header.Get("Authorization") != "Bearer pat-write" {
				t.Fatalf("users/create Authorization = %q", r.Header.Get("Authorization"))
			}
			seenCreateUser = true
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode users/create request: %v", err)
			}
			createdUserEmail = fmt.Sprintf("%v", request["email"])
			createdUserDisplayName = fmt.Sprintf("%v", request["display_name"])
			_, _ = w.Write([]byte(fmt.Sprintf(`{"user":{"user_id":"user-managed","email":"%s","display_name":"%s","status":"active"}}`, createdUserEmail, createdUserDisplayName)))
		case "/memory/tenant/users/update-status":
			if r.Header.Get("Authorization") != "Bearer pat-write" {
				t.Fatalf("users/update-status Authorization = %q", r.Header.Get("Authorization"))
			}
			seenDisableUser = true
			_, _ = w.Write([]byte(`{"user":{"user_id":"user-managed","email":"tenant-smoke@example.invalid","display_name":"Tenant Smoke User","status":"disabled"}}`))
		case "/memory/tenant/users/list":
			if r.Header.Get("Authorization") != "Bearer pat-read" {
				t.Fatalf("users/list Authorization = %q", r.Header.Get("Authorization"))
			}
			if !seenDisableUser {
				_, _ = w.Write([]byte(fmt.Sprintf(`{"users":[{"user_id":"user-managed","email":"%s","display_name":"%s","status":"active"}]}`, createdUserEmail, createdUserDisplayName)))
				return
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"users":[{"user_id":"user-managed","email":"%s","display_name":"%s","status":"disabled"}]}`, createdUserEmail, createdUserDisplayName)))
		case "/memory/tenant/roles/upsert":
			if r.Header.Get("Authorization") != "Bearer pat-write" {
				t.Fatalf("roles/upsert Authorization = %q", r.Header.Get("Authorization"))
			}
			seenRoleUpsert = true
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode roles/upsert request: %v", err)
			}
			customRoleName = fmt.Sprintf("%v", request["role"])
			if labels, ok := request["permission_labels"].([]any); ok && len(labels) > 0 {
				customRoleLabel = fmt.Sprintf("%v", labels[0])
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"role":{"role":"%s","display_name":"Tenant Smoke Reader","description":"smoke","permission_labels":["%s"]}}`, customRoleName, customRoleLabel)))
		case "/memory/tenant/roles/list":
			if r.Header.Get("Authorization") != "Bearer pat-read" {
				t.Fatalf("roles/list Authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"roles":[{"role":"owner","display_name":"Owner","description":"owner","permission_labels":["project:project-1:write"]},{"role":"%s","display_name":"Tenant Smoke Reader","description":"smoke","permission_labels":["%s"]}]}`, customRoleName, customRoleLabel)))
		case "/memory/tenant/memberships/add":
			if r.Header.Get("Authorization") != "Bearer pat-write" {
				t.Fatalf("memberships/add Authorization = %q", r.Header.Get("Authorization"))
			}
			seenMembershipAdd = true
			_, _ = w.Write([]byte(fmt.Sprintf(`{"user_id":"user-managed","org_id":"org-1","project_id":"project-1","role":"%s","status":"active"}`, customRoleName)))
		case "/memory/tenant/memberships/update-role":
			if r.Header.Get("Authorization") != "Bearer pat-write" {
				t.Fatalf("memberships/update-role Authorization = %q", r.Header.Get("Authorization"))
			}
			seenMembershipUpdate = true
			_, _ = w.Write([]byte(`{"user_id":"user-managed","org_id":"org-1","project_id":"project-1","role":"admin","status":"active"}`))
		case "/memory/tenant/memberships/remove":
			if r.Header.Get("Authorization") != "Bearer pat-write" {
				t.Fatalf("memberships/remove Authorization = %q", r.Header.Get("Authorization"))
			}
			seenMembershipRemove = true
			_, _ = w.Write([]byte(`{"user_id":"user-managed","org_id":"org-1","project_id":"project-1","role":"admin","status":"disabled"}`))
		case "/memory/tenant/memberships/list":
			if r.Header.Get("Authorization") != "Bearer pat-read" {
				t.Fatalf("memberships/list Authorization = %q", r.Header.Get("Authorization"))
			}
			status := "active"
			role := customRoleName
			if seenMembershipUpdate {
				role = "admin"
			}
			if seenMembershipRemove {
				status = "disabled"
			}
			_, _ = w.Write([]byte(`{"memberships":[{"user_id":"` + userID + `","org_id":"org-1","project_id":"project-1","role":"` + role + `","status":"` + status + `"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	if err := tenantGovernanceSmoke(context.Background(), server.URL); err != nil {
		t.Fatalf("tenantGovernanceSmoke() error = %v", err)
	}
	if !cleanupCalled {
		t.Fatal("tenantGovernanceSmoke() did not run cleanup")
	}
	for label, seen := range map[string]bool{
		"create_user":       seenCreateUser,
		"disable_user":      seenDisableUser,
		"role_upsert":       seenRoleUpsert,
		"membership_add":    seenMembershipAdd,
		"membership_update": seenMembershipUpdate,
		"membership_remove": seenMembershipRemove,
	} {
		if !seen {
			t.Fatalf("tenantGovernanceSmoke() missing step %s", label)
		}
	}
}

func TestTurnEventSmokeAcceptsAdapterTokenRequiredWhenNoTokenConfigured(t *testing.T) {
	t.Setenv("SMOKE_ADAPTER_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"adapter_token_required"}`))
	}))
	defer server.Close()

	if err := turnEventSmoke(context.Background(), server.URL); err != nil {
		t.Fatalf("turnEventSmoke() error = %v", err)
	}
}

func TestPipelineE2ESmokeDisabledByDefault(t *testing.T) {
	t.Setenv("SMOKE_ENABLE_PIPELINE_E2E", "")
	t.Setenv("SMOKE_ADAPTER_TOKEN", "")

	if err := pipelineE2ESmoke(context.Background(), "http://example.local"); err != nil {
		t.Fatalf("pipelineE2ESmoke() error = %v", err)
	}
}

func TestPipelineE2ESmokeRequiresAdapterTokenWhenEnabled(t *testing.T) {
	t.Setenv("SMOKE_ENABLE_PIPELINE_E2E", "true")
	t.Setenv("SMOKE_ADAPTER_TOKEN", "")
	t.Setenv("SMOKE_POSTGRES_DSN", "")

	if err := pipelineE2ESmoke(context.Background(), "http://example.local"); err == nil {
		t.Fatal("pipelineE2ESmoke() error = nil, want missing token error")
	}
}

func TestPipelineE2ESmokeCanProvisionActorFromPostgresDSN(t *testing.T) {
	t.Setenv("SMOKE_ENABLE_PIPELINE_E2E", "true")
	t.Setenv("SMOKE_ADAPTER_TOKEN", "")
	t.Setenv("SMOKE_POSTGRES_DSN", "postgres://memory-os-smoke")
	t.Setenv("SMOKE_PIPELINE_E2E_MARKER", "pipeline-marker-provisioned")
	originalProvisioner := provisionPipelineE2EActor
	originalCandidateWaiter := waitForPipelineCandidateJob
	t.Cleanup(func() {
		provisionPipelineE2EActor = originalProvisioner
		waitForPipelineCandidateJob = originalCandidateWaiter
	})
	cleanupCalled := false
	candidateWaitCalled := false
	provisionPipelineE2EActor = func(ctx context.Context, dsn, marker string) (pipelineE2EActor, error) {
		if dsn != "postgres://memory-os-smoke" || marker != "pipeline-marker-provisioned" {
			t.Fatalf("provision args dsn=%q marker=%q", dsn, marker)
		}
		return pipelineE2EActor{
			Token:       "adapter-provisioned-token",
			SearchToken: "pat-provisioned-token",
			Scope: smokeActorScope{
				UserID:          "user_provisioned",
				OrgID:           "org_provisioned",
				ProjectID:       "project_provisioned",
				AgentID:         "codex",
				PermissionLabel: "project:project_provisioned:read",
			},
			Cleanup: func(ctx context.Context) error {
				cleanupCalled = true
				return nil
			},
		}, nil
	}
	waitForPipelineCandidateJob = func(ctx context.Context, dsn string, eventID string, deadline time.Time) error {
		candidateWaitCalled = true
		if dsn != "postgres://memory-os-smoke" || eventID != "event_pipeline-marker-provisioned" {
			t.Fatalf("candidate wait args dsn=%q eventID=%q", dsn, eventID)
		}
		return nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if !strings.Contains(fmtAny(payload), "user_provisioned") || !strings.Contains(fmtAny(payload), "project_provisioned") {
			t.Fatalf("pipeline request did not use provisioned actor: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/memory/turn-event":
			if r.Header.Get("Authorization") != "Bearer adapter-provisioned-token" {
				t.Fatalf("turn event Authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"event_id":"event_pipeline-marker-provisioned","status":"accepted","deduped":false}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	if err := pipelineE2ESmoke(context.Background(), server.URL); err != nil {
		t.Fatalf("pipelineE2ESmoke() error = %v", err)
	}
	if !cleanupCalled {
		t.Fatal("pipelineE2ESmoke() did not cleanup provisioned actor")
	}
	if !candidateWaitCalled {
		t.Fatal("pipelineE2ESmoke() did not wait for candidate job")
	}
}

func TestPipelineE2ESmokeReturnsProvisionedActorCleanupError(t *testing.T) {
	t.Setenv("SMOKE_ENABLE_PIPELINE_E2E", "true")
	t.Setenv("SMOKE_ADAPTER_TOKEN", "")
	t.Setenv("SMOKE_POSTGRES_DSN", "postgres://memory-os-smoke")
	t.Setenv("SMOKE_PIPELINE_E2E_MARKER", "pipeline-marker-cleanup")
	originalProvisioner := provisionPipelineE2EActor
	originalCandidateWaiter := waitForPipelineCandidateJob
	t.Cleanup(func() {
		provisionPipelineE2EActor = originalProvisioner
		waitForPipelineCandidateJob = originalCandidateWaiter
	})
	provisionPipelineE2EActor = func(ctx context.Context, dsn, marker string) (pipelineE2EActor, error) {
		return pipelineE2EActor{
			Token:       "adapter-provisioned-token",
			SearchToken: "pat-provisioned-token",
			Scope: smokeActorScope{
				UserID:          "user_provisioned",
				OrgID:           "org_provisioned",
				ProjectID:       "project_provisioned",
				AgentID:         "codex",
				PermissionLabel: "project:project_provisioned:read",
			},
			Cleanup: func(ctx context.Context) error {
				return errors.New("cleanup failed")
			},
		}, nil
	}
	waitForPipelineCandidateJob = func(ctx context.Context, dsn string, eventID string, deadline time.Time) error {
		return nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/memory/turn-event":
			_, _ = w.Write([]byte(`{"event_id":"event_pipeline-marker-cleanup","status":"accepted","deduped":false}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	err := pipelineE2ESmoke(context.Background(), server.URL)
	if err == nil {
		t.Fatal("pipelineE2ESmoke() error = nil, want cleanup failure")
	}
	if !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("pipelineE2ESmoke() error = %v, want cleanup failure", err)
	}
}

func TestPipelineE2ESmokeWritesTurnEventAndFindsArchiveChunk(t *testing.T) {
	t.Setenv("SMOKE_ENABLE_PIPELINE_E2E", "true")
	t.Setenv("SMOKE_ADAPTER_TOKEN", "adapter-test-token")
	t.Setenv("SMOKE_SEARCH_PAT", "pat-test-token")
	t.Setenv("SMOKE_PIPELINE_E2E_MARKER", "pipeline-marker-test")
	t.Setenv("SMOKE_SEARCH_USER_ID", "user_pipeline")
	t.Setenv("SMOKE_SEARCH_ORG_ID", "org_pipeline")
	t.Setenv("SMOKE_SEARCH_PROJECT_ID", "project_pipeline")
	t.Setenv("SMOKE_SEARCH_AGENT_ID", "codex")
	t.Setenv("SMOKE_SEARCH_PERMISSION_LABEL", "project:project_pipeline:read")
	turnEventCalled := false
	searchCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/memory/turn-event":
			if r.Header.Get("Authorization") != "Bearer adapter-test-token" {
				t.Fatalf("turn event Authorization = %q", r.Header.Get("Authorization"))
			}
			turnEventCalled = true
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode turn event: %v", err)
			}
			if !strings.Contains(fmtAny(payload), "pipeline-marker-test") {
				t.Fatalf("turn event missing marker: %#v", payload)
			}
			if !strings.Contains(fmtAny(payload), "user_pipeline") {
				t.Fatalf("turn event did not use configured actor: %#v", payload)
			}
			if !strings.Contains(fmtAny(payload), "local/pipeline-e2e/pipeline-marker-test") {
				t.Fatalf("turn event missing pipeline workspace identity: %#v", payload)
			}
			if !strings.Contains(fmtAny(payload), "assistant_final") {
				t.Fatalf("turn event must use candidate-triggering event type: %#v", payload)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"event_id":"event_pipeline-marker-test","status":"accepted","deduped":false}`))
		case "/memory/search":
			if r.Header.Get("Authorization") != "Bearer pat-test-token" {
				t.Fatalf("search Authorization = %q", r.Header.Get("Authorization"))
			}
			searchCalled = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"pipeline","context":"pipeline-marker-test archived","results":[{"text":"pipeline-marker-test archived","score":0.9,"source":{"kind":"archive_chunk","archive_id":"archive_pipeline","chunk_id":"chunk_pipeline"}}],"rerank_degraded":true}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	if err := pipelineE2ESmoke(context.Background(), server.URL); err != nil {
		t.Fatalf("pipelineE2ESmoke() error = %v", err)
	}
	if !turnEventCalled || !searchCalled {
		t.Fatalf("turnEventCalled=%v searchCalled=%v", turnEventCalled, searchCalled)
	}
}

func TestPipelineE2ESmokeChecksMCPMemorySearchWhenConfigured(t *testing.T) {
	t.Setenv("SMOKE_ENABLE_PIPELINE_E2E", "true")
	t.Setenv("SMOKE_ADAPTER_TOKEN", "adapter-test-token")
	t.Setenv("SMOKE_SEARCH_PAT", "pat-test-token")
	t.Setenv("SMOKE_PIPELINE_E2E_MARKER", "pipeline-marker-mcp")
	t.Setenv("SMOKE_SEARCH_USER_ID", "user_pipeline")
	t.Setenv("SMOKE_SEARCH_ORG_ID", "org_pipeline")
	t.Setenv("SMOKE_SEARCH_PROJECT_ID", "project_pipeline")
	t.Setenv("SMOKE_SEARCH_AGENT_ID", "codex")
	mcpCalled := false
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/memory/turn-event":
			_, _ = w.Write([]byte(`{"event_id":"event_pipeline-marker-mcp","status":"accepted","deduped":false}`))
		case "/memory/search":
			_, _ = w.Write([]byte(`{"request_id":"pipeline","context":"pipeline-marker-mcp archived","results":[{"text":"pipeline-marker-mcp archived","score":0.9,"source":{"kind":"archive_chunk","archive_id":"archive_pipeline","chunk_id":"chunk_pipeline"}}],"rerank_degraded":true}`))
		default:
			t.Fatalf("unexpected API path %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/call" {
			t.Fatalf("unexpected MCP path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer pat-test-token" {
			t.Fatalf("unexpected MCP authorization header %q", got)
		}
		mcpCalled = true
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode MCP request: %v", err)
		}
		if payload["name"] != "memory_search" || !strings.Contains(fmtAny(payload), "pipeline-marker-mcp") || !strings.Contains(fmtAny(payload), "project:project_pipeline:read") {
			t.Fatalf("unexpected MCP request: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"ok","search":{"request_id":"pipeline","context":"pipeline-marker-mcp archived","results":[{"text":"pipeline-marker-mcp archived","score":0.9,"source":{"kind":"archive_chunk","archive_id":"archive_pipeline","chunk_id":"chunk_pipeline"}}],"rerank_degraded":true,"access_log_count":1,"marked_used_count":1}}`))
	}))
	defer mcpServer.Close()
	t.Setenv("SMOKE_MCP_URL", mcpServer.URL)

	if err := pipelineE2ESmoke(context.Background(), apiServer.URL); err != nil {
		t.Fatalf("pipelineE2ESmoke() error = %v", err)
	}
	if !mcpCalled {
		t.Fatal("pipelineE2ESmoke() did not call MCP memory_search")
	}
}

func TestPipelineE2ESmokeRejectsMCPMismatchWhenConfigured(t *testing.T) {
	t.Setenv("SMOKE_ENABLE_PIPELINE_E2E", "true")
	t.Setenv("SMOKE_ADAPTER_TOKEN", "adapter-test-token")
	t.Setenv("SMOKE_SEARCH_PAT", "pat-test-token")
	t.Setenv("SMOKE_PIPELINE_E2E_MARKER", "pipeline-marker-mismatch")
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/memory/turn-event":
			_, _ = w.Write([]byte(`{"event_id":"event_pipeline-marker-mismatch","status":"accepted","deduped":false}`))
		case "/memory/search":
			_, _ = w.Write([]byte(`{"request_id":"pipeline","context":"pipeline-marker-mismatch archived","results":[{"text":"pipeline-marker-mismatch archived","source":{"kind":"archive_chunk","archive_id":"archive_pipeline","chunk_id":"chunk_pipeline"}}],"rerank_degraded":true}`))
		default:
			t.Fatalf("unexpected API path %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"ok","search":{"request_id":"pipeline","context":"","results":[]}}`))
	}))
	defer mcpServer.Close()
	t.Setenv("SMOKE_MCP_URL", mcpServer.URL)

	err := pipelineE2ESmoke(context.Background(), apiServer.URL)
	if err == nil {
		t.Fatal("pipelineE2ESmoke() error = nil, want MCP mismatch failure")
	}
	if !strings.Contains(err.Error(), "mcp memory_search") {
		t.Fatalf("pipelineE2ESmoke() error = %v, want MCP mismatch", err)
	}
}

func TestPipelineE2ESmokeRejectsTurnEventResponseWithoutAcceptedStatus(t *testing.T) {
	t.Setenv("SMOKE_ENABLE_PIPELINE_E2E", "true")
	t.Setenv("SMOKE_ADAPTER_TOKEN", "adapter-test-token")
	t.Setenv("SMOKE_SEARCH_PAT", "pat-test-token")
	t.Setenv("SMOKE_PIPELINE_E2E_MARKER", "pipeline-marker-unaccepted")
	t.Setenv("SMOKE_PIPELINE_E2E_TIMEOUT", "1ms")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/memory/turn-event":
			_, _ = w.Write([]byte(`{"event_id":"event_pipeline-marker-unaccepted","status":"ignored","deduped":false}`))
		case "/memory/search":
			_, _ = w.Write([]byte(`{"request_id":"pipeline","context":"","results":[]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	err := pipelineE2ESmoke(context.Background(), server.URL)
	if err == nil {
		t.Fatal("pipelineE2ESmoke() error = nil, want unaccepted turn event response rejection")
	}
	if !strings.Contains(err.Error(), "turn event response") {
		t.Fatalf("pipelineE2ESmoke() error = %v, want turn event response rejection", err)
	}
}

func TestAdapterFixtureE2ESmokeWritesAndSearchesAllFixtures(t *testing.T) {
	t.Setenv("SMOKE_ENABLE_ADAPTER_FIXTURE_E2E", "true")
	actor := smokeActorScope{
		UserID:          "user_fixture",
		OrgID:           "org_fixture",
		ProjectID:       "project_fixture",
		AgentID:         "codex",
		PermissionLabel: "project:project_fixture:read",
	}
	expectedQueries := map[string]bool{
		"fixture-marker remember deploy uses docker compose": false,
		"fixture-marker open code should index archive":      false,
		"fixture-marker Hermes adapter sends TurnEvent":      false,
		"fixture-marker please migrate memory adapters":      false,
	}
	turnEventCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/memory/turn-event" && r.URL.Path != "/memory/search" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/memory/turn-event":
			if r.Header.Get("Authorization") != "Bearer adapter-fixture-token" {
				t.Fatalf("turn event Authorization = %q", r.Header.Get("Authorization"))
			}
			turnEventCount++
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode turn event payload: %v", err)
			}
			text := fmtAny(payload)
			if !strings.Contains(text, "user_fixture") || !strings.Contains(text, "project_fixture") {
				t.Fatalf("fixture event did not use smoke actor: %#v", payload)
			}
			if strings.Contains(text, "sk-test-redacted-example") {
				t.Fatalf("fixture event leaked fake secret: %#v", payload)
			}
			_, _ = w.Write([]byte(`{"event_id":"fixture_event","status":"accepted","deduped":false}`))
		case "/memory/search":
			if r.Header.Get("Authorization") != "Bearer adapter-fixture-pat" {
				t.Fatalf("search Authorization = %q", r.Header.Get("Authorization"))
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode search payload: %v", err)
			}
			query, _ := payload["query"].(string)
			if _, ok := expectedQueries[query]; !ok {
				t.Fatalf("unexpected fixture search query %q", query)
			}
			expectedQueries[query] = true
			_, _ = w.Write([]byte(`{"request_id":"fixture-search","context":"` + query + ` archived","results":[{"text":"` + query + ` archived","source":{"kind":"archive_chunk","archive_id":"archive_fixture","chunk_id":"chunk_fixture"}}],"rerank_degraded":true}`))
		}
	}))
	defer server.Close()

	err := adapterFixtureE2ESmoke(context.Background(), server.URL, "adapter-fixture-token", "adapter-fixture-pat", actor, "fixture-marker")
	if err != nil {
		t.Fatalf("adapterFixtureE2ESmoke() error = %v", err)
	}
	if turnEventCount < 4 {
		t.Fatalf("turnEventCount = %d, want at least one event per fixture", turnEventCount)
	}
	for query, seen := range expectedQueries {
		if !seen {
			t.Fatalf("fixture query %q was not searched", query)
		}
	}
}

func TestMakeSmokeDoesNotForceDevEndpoints(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(findRepoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	if strings.Contains(string(content), "SMOKE_ENABLE_DEV_ENDPOINTS=true") {
		t.Fatal("make smoke must not force dev endpoints in production mode")
	}
}

func TestMakeSmokePassesStrictPipelineEnvironmentToDocker(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(findRepoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	text := string(content)
	for _, host := range []string{"memory-api", "memory-web", "memory-mcp", "memory-llm-mock"} {
		if !strings.Contains(text, host) {
			t.Fatalf("Makefile NO_PROXY default must include %s", host)
		}
	}
	if !strings.Contains(text, "--network $${SMOKE_DOCKER_NETWORK:-host}") {
		t.Fatal("Makefile docker smoke branch must allow SMOKE_DOCKER_NETWORK override")
	}
	for _, variable := range []string{
		"SMOKE_API_URL",
		"SMOKE_QDRANT_URL",
		"SMOKE_WEB_URL",
		"SMOKE_MCP_URL",
		"SMOKE_LLM_BASE_URL",
		"SMOKE_LLM_API_KEY",
		"SMOKE_TIMEOUT",
		"SMOKE_ENABLE_TENANT_GOVERNANCE",
		"SMOKE_REQUIRE_CONFIGURED_RETRIEVAL",
		"SMOKE_ENABLE_PIPELINE_E2E",
		"SMOKE_ADAPTER_TOKEN",
		"SMOKE_PIPELINE_E2E_MARKER",
		"SMOKE_PIPELINE_E2E_TIMEOUT",
		"SMOKE_POSTGRES_DSN",
		"SMOKE_SEARCH_USER_ID",
		"SMOKE_SEARCH_ORG_ID",
		"SMOKE_SEARCH_PROJECT_ID",
		"SMOKE_SEARCH_AGENT_ID",
		"SMOKE_SEARCH_PERMISSION_LABEL",
	} {
		if !strings.Contains(text, "-e "+variable+"=$${"+variable) {
			t.Fatalf("Makefile docker smoke branch does not pass %s", variable)
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

func fmtAny(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
