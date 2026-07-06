package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"memory-os/internal/adapter"
	"memory-os/internal/auth"
	"memory-os/internal/eventlog"
	"memory-os/internal/health"
	"memory-os/internal/llm"
	"memory-os/internal/qdrant"
	"memory-os/internal/tenant"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "smoke failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("smoke ok")
}

func run() error {
	if _, err := qdrant.BuildPayloadFilter(qdrant.FilterContext{
		UserID:           "user_alice",
		OrgID:            "org_alpha",
		ProjectID:        "project_alpha",
		Visibility:       "project",
		PermissionLabels: []string{"project:project_alpha:read"},
		DocType:          "archive_chunk",
		IndexGeneration:  1,
	}); err != nil {
		return fmt.Errorf("qdrant filtered search builder: %w", err)
	}

	apiURL := os.Getenv("SMOKE_API_URL")
	apiURL = resolveSmokeHealthURL(apiURL)

	ctx, cancel := context.WithTimeout(context.Background(), smokeTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz status %d", resp.StatusCode)
	}
	var report health.Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return err
	}
	if report.Status == "" {
		return fmt.Errorf("healthz status is empty")
	}
	if report.Status != health.StatusOK {
		return fmt.Errorf("healthz status = %s, want %s", report.Status, health.StatusOK)
	}
	if err := qdrantSmoke(ctx); err != nil {
		return err
	}
	if err := modelProviderSmoke(ctx); err != nil {
		return err
	}

	devSmokeEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("SMOKE_ENABLE_DEV_ENDPOINTS")), "true")
	if devSmokeEnabled {
		if err := phase2Smoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/dev/smoke/phase2"); err != nil {
			return err
		}
	}
	if err := turnEventSmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/memory/turn-event"); err != nil {
		return err
	}
	if devSmokeEnabled {
		if err := archiveSmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/dev/smoke/archive"); err != nil {
			return err
		}
		if err := ragSmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/dev/smoke/rag"); err != nil {
			return err
		}
		if err := hotMemorySmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/dev/smoke/hot-memory"); err != nil {
			return err
		}
	}
	requireConfigured := strings.EqualFold(strings.TrimSpace(os.Getenv("SMOKE_REQUIRE_CONFIGURED_RETRIEVAL")), "true")
	pipelineE2EEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("SMOKE_ENABLE_PIPELINE_E2E")), "true")
	pipelineSmokeAlreadyRun := false
	var searchActorProvisionCleanup func(context.Context) error
	if requireConfigured && pipelineE2EEnabled {
		searchActorSetup, err := ensureMemorySearchActor(ctx, true, true)
		if err != nil {
			return err
		}
		searchActorProvisionCleanup = searchActorSetup.Cleanup
	}
	if requireConfigured && pipelineE2EEnabled {
		if err := pipelineE2ESmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")); err != nil {
			return err
		}
		pipelineSmokeAlreadyRun = true
	}
	if err := memorySearchSmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/memory/search"); err != nil {
		return err
	}
	if !pipelineSmokeAlreadyRun {
		if err := pipelineE2ESmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")); err != nil {
			return err
		}
	}
	if err := adapterDryRunSmoke(ctx); err != nil {
		return err
	}
	if err := importerSmoke(ctx); err != nil {
		return err
	}
	if err := tenantGovernanceSmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")); err != nil {
		return err
	}
	if err := webSmoke(ctx); err != nil {
		return err
	}
	if searchActorProvisionCleanup != nil {
		if err := searchActorProvisionCleanup(context.Background()); err != nil {
			return fmt.Errorf("search actor cleanup failed: %w", err)
		}
	}

	return nil
}

func smokeTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("SMOKE_TIMEOUT"))
	if value == "" {
		return time.Minute
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return time.Minute
	}
	return duration
}

func smokePostgresDSN() string {
	value := strings.TrimSpace(os.Getenv("SMOKE_POSTGRES_DSN"))
	if value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("POSTGRES_DSN"))
}

func assertNoSecretLeak(label string, payload string) error {
	for _, marker := range []string{
		"sk-test-redacted-example",
		"fake-secret-value",
		"password-test-redacted",
		"-----BEGIN PRIVATE KEY-----",
		"fake-test-redacted",
	} {
		if strings.Contains(payload, marker) {
			return fmt.Errorf("%s leaked secret marker %q", label, marker)
		}
	}
	return nil
}

func modelProviderSmoke(ctx context.Context) error {
	baseURL := os.Getenv("SMOKE_LLM_BASE_URL")
	apiKey := os.Getenv("SMOKE_LLM_API_KEY")
	if baseURL == "" || apiKey == "" || apiKey == "replace-me" {
		client, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: "http://example.local:8000", EmbeddingModel: "bge-m3"})
		if err != nil {
			return err
		}
		_, err = client.Embed(ctx, []string{"smoke"})
		if !llm.IsNotConfigured(err) {
			return fmt.Errorf("model provider smoke expected not configured, got %v", err)
		}
		return nil
	}
	client, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: baseURL, APIKey: apiKey, EmbeddingModel: "bge-m3", RerankModel: "bge-reranker-v2-m3"})
	if err != nil {
		return err
	}
	embedding, err := client.Embed(ctx, []string{"Memory OS smoke"})
	if err != nil {
		return err
	}
	if embedding.Dim == 0 || len(embedding.Vectors) != 1 {
		return fmt.Errorf("embedding smoke returned invalid response: %#v", embedding)
	}
	rerank, err := client.Rerank(ctx, "deploy", []string{"deploy with docker compose", "unrelated"})
	if err != nil {
		return err
	}
	if len(rerank.Results) == 0 {
		return fmt.Errorf("rerank smoke returned no results")
	}
	return nil
}

func qdrantSmoke(ctx context.Context) error {
	qdrantURL := os.Getenv("SMOKE_QDRANT_URL")
	if qdrantURL == "" {
		qdrantURL = "http://localhost:18083"
	}
	client, err := qdrant.NewClient(qdrantURL)
	if err != nil {
		return err
	}
	if err := client.EnsureCollectionSchema(ctx, qdrant.CollectionConfig{Name: qdrant.DefaultCollectionName, VectorSize: qdrant.DefaultVectorSize, Distance: qdrant.DefaultDistance}, qdrant.DefaultPayloadIndexConfigs()); err != nil {
		return err
	}
	vector := make([]float64, qdrant.DefaultVectorSize)
	vector[0] = 1
	if err := client.UpsertPoints(ctx, qdrant.DefaultCollectionName, []qdrant.Point{{
		ID:     "11111111-1111-4111-8111-111111111111",
		Vector: vector,
		Payload: map[string]any{
			"doc_type":          "archive_chunk",
			"user_id":           "user_smoke",
			"org_id":            "org_smoke",
			"project_id":        "project_smoke",
			"visibility":        "project",
			"permission_labels": []string{"project:project_smoke:read"},
			"index_generation":  "1",
			"chunk_id":          "smoke_chunk_1",
		},
	}}); err != nil {
		return err
	}
	filter, err := qdrant.BuildPayloadFilter(qdrant.FilterContext{UserID: "user_smoke", OrgID: "org_smoke", ProjectID: "project_smoke", Visibility: "project", PermissionLabels: []string{"project:project_smoke:read"}, DocType: "archive_chunk", IndexGeneration: 1})
	if err != nil {
		return err
	}
	results, err := client.SearchPoints(ctx, qdrant.SearchPointsRequest{Collection: qdrant.DefaultCollectionName, Vector: vector, Filter: filter, Limit: 1})
	if err != nil {
		return err
	}
	if len(results) == 0 || results[0].Payload["chunk_id"] != "smoke_chunk_1" {
		return fmt.Errorf("qdrant smoke search did not return smoke point: %#v", results)
	}
	return nil
}

func phase2Smoke(ctx context.Context, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("phase2 smoke status %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	if payload["status"] != "ok" {
		return fmt.Errorf("phase2 smoke status = %v, want ok", payload["status"])
	}
	return assertNoSecretLeak("phase2 smoke", fmt.Sprintf("%v", payload))
}

func turnEventSmoke(ctx context.Context, endpoint string) error {
	body := `{"request_id":"turn-event-smoke-1","event":{"version":"v1","event_id":"event_smoke_1","turn_id":"turn_smoke_1","thread_id":"thread_smoke_1","session_id":"session_smoke_1","type":"tool_call_completed","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"user_smoke","org_id":"org_smoke","project_id":"project_smoke","agent_id":"codex"},"payload":{"tool_output":"smoke output sk-test-redacted-example"}}}`
	token := strings.TrimSpace(os.Getenv("SMOKE_ADAPTER_TOKEN"))
	allowed := []int{}
	if token == "" {
		allowed = append(allowed, http.StatusUnauthorized)
	}
	first, err := postJSONWithBearerAllowStatus(ctx, endpoint, body, token, allowed...)
	if err != nil {
		return err
	}
	if err := assertNoSecretLeak("turn event smoke", first); err != nil {
		return err
	}
	if token == "" {
		if strings.Contains(first, `"error":"adapter_token_required"`) {
			return nil
		}
		return fmt.Errorf("turn event smoke without token expected adapter_token_required: %s", first)
	}
	second, err := postJSONWithBearerAllowStatus(ctx, endpoint, body, token)
	if err != nil {
		return err
	}
	if !strings.Contains(second, `"deduped":true`) {
		return fmt.Errorf("turn event smoke did not dedupe duplicate event: %s", second)
	}
	return nil
}

func archiveSmoke(ctx context.Context, endpoint string) error {
	payload, err := postJSON(ctx, endpoint, ``)
	if err != nil {
		return err
	}
	if err := assertNoSecretLeak("archive smoke", payload); err != nil {
		return err
	}
	if !strings.Contains(payload, `"version":2`) || !strings.Contains(payload, `"index_generation":2`) {
		return fmt.Errorf("archive smoke did not increment version/index_generation: %s", payload)
	}
	return nil
}

func ragSmoke(ctx context.Context, endpoint string) error {
	payload, err := postJSON(ctx, endpoint, ``)
	if err != nil {
		return err
	}
	if err := assertNoSecretLeak("rag smoke", payload); err != nil {
		return err
	}
	if strings.Contains(payload, "cross_tenant_leaked") {
		return fmt.Errorf("rag smoke leaked cross tenant data")
	}
	if !strings.Contains(payload, `"results":1`) || !strings.Contains(payload, `"chunk_id":"chunk_new"`) {
		return fmt.Errorf("rag smoke did not return filtered current-generation result: %s", payload)
	}
	return nil
}

func hotMemorySmoke(ctx context.Context, endpoint string) error {
	payload, err := postJSON(ctx, endpoint, ``)
	if err != nil {
		return err
	}
	if strings.Contains(payload, "sk-test-redacted-example") {
		return fmt.Errorf("hot memory smoke leaked fake secret")
	}
	if !strings.Contains(payload, `"results":1`) || !strings.Contains(payload, `"used_count":1`) || !strings.Contains(payload, `"memory_type":"hot_memory"`) {
		return fmt.Errorf("hot memory smoke did not complete create/search/use flow: %s", payload)
	}
	return nil
}

func memorySearchSmoke(ctx context.Context, endpoint string) error {
	requireConfigured := strings.EqualFold(strings.TrimSpace(os.Getenv("SMOKE_REQUIRE_CONFIGURED_RETRIEVAL")), "true")
	token := strings.TrimSpace(os.Getenv("SMOKE_SEARCH_PAT"))
	actor := smokeActor()
	if token == "" && requireConfigured {
		dsn := smokePostgresDSN()
		if dsn == "" {
			return fmt.Errorf("memory search smoke requires SMOKE_SEARCH_PAT or SMOKE_POSTGRES_DSN in configured retrieval mode")
		}
		provisioned, err := provisionPipelineE2EActor(ctx, dsn, "memory-search-smoke")
		if err != nil {
			return err
		}
		token = strings.TrimSpace(provisioned.SearchToken)
		if token == "" {
			return fmt.Errorf("provisioned search actor missing search token")
		}
		actor = provisioned.Scope
		if provisioned.Cleanup != nil {
			defer func() {
				if err := provisioned.Cleanup(context.Background()); err != nil {
					// best effort cleanup, keep original failure unchanged
				}
			}()
		}
	}
	body := fmt.Sprintf(`{"request_id":"memory-search-smoke-1","query":"deploy API","actor":{"user_id":"%s","org_id":"%s","project_id":"%s","agent_id":"%s"},"scope":"project","visibility":"project","permission_labels":["%s"],"archive_index_generation":2,"max_context_bytes":512}`,
		actor.UserID, actor.OrgID, actor.ProjectID, actor.AgentID, actor.PermissionLabel)
	payload, err := postJSONWithBearerAllowStatus(ctx, endpoint, body, token, http.StatusServiceUnavailable, http.StatusForbidden, http.StatusUnauthorized)
	if err != nil {
		return err
	}
	if strings.Contains(payload, "cross_tenant_leaked") || strings.Contains(payload, "sk-test-redacted-example") || strings.Contains(payload, "Codex private") {
		return fmt.Errorf("memory search smoke leaked isolated or secret content")
	}
	if strings.Contains(payload, `"error":"retrieval_not_configured"`) {
		if requireConfigured {
			return fmt.Errorf("memory search smoke requires configured retrieval: %s", payload)
		}
		return nil
	}
	if strings.Contains(payload, `"error":"memory_search_forbidden"`) {
		if requireConfigured {
			return fmt.Errorf("memory search smoke requires permitted retrieval actor: %s", payload)
		}
		return nil
	}
	if strings.Contains(payload, `"error":"pat_required"`) || strings.Contains(payload, `"error":"invalid_pat"`) {
		if requireConfigured {
			return fmt.Errorf("memory search smoke requires authenticated retrieval: %s", payload)
		}
		return nil
	}
	if strings.Contains(payload, `"rerank_degraded":true`) && strings.Contains(payload, `"kind":"hot_memory"`) && strings.Contains(payload, `"kind":"archive_chunk"`) {
		if token != "" {
			return nil
		}
		eveBody := fmt.Sprintf(`{"request_id":"memory-search-smoke-eve","query":"deploy","actor":{"user_id":"user_eve","org_id":"%s","project_id":"%s","agent_id":"codex"},"scope":"project","visibility":"project","permission_labels":["%s"],"archive_index_generation":2,"max_context_bytes":512}`,
			actor.OrgID, actor.ProjectID, actor.PermissionLabel)
		evePayload, err := postJSON(ctx, endpoint, eveBody)
		if err != nil {
			return err
		}
		if strings.Contains(evePayload, `"kind":"hot_memory"`) || strings.Contains(evePayload, `"kind":"archive_chunk"`) {
			return fmt.Errorf("memory search smoke returned results for isolated user: %s", evePayload)
		}
		return nil
	}
	if token != "" {
		return nil
	}
	return fmt.Errorf("memory search smoke missing configured retrieval or explicit not-configured error: %s", payload)
}

func ensureMemorySearchActor(ctx context.Context, requireConfigured bool, setEnv bool) (pipelineE2EActor, error) {
	token := strings.TrimSpace(os.Getenv("SMOKE_SEARCH_PAT"))
	if token != "" || !requireConfigured {
		return pipelineE2EActor{
			SearchToken: token,
			Scope:       smokeActor(),
		}, nil
	}
	dsn := smokePostgresDSN()
	if dsn == "" {
		return pipelineE2EActor{}, fmt.Errorf("memory search smoke requires SMOKE_SEARCH_PAT or SMOKE_POSTGRES_DSN in configured retrieval mode")
	}
	provisioned, err := provisionPipelineE2EActor(ctx, dsn, "memory-search-smoke")
	if err != nil {
		return pipelineE2EActor{}, fmt.Errorf("provision search actor from postgres failed: %w", err)
	}
	if strings.TrimSpace(provisioned.SearchToken) == "" {
		return pipelineE2EActor{}, fmt.Errorf("provisioned search actor missing search token")
	}
	if setEnv {
		_ = os.Setenv("SMOKE_SEARCH_PAT", provisioned.SearchToken)
		_ = os.Setenv("SMOKE_SEARCH_USER_ID", provisioned.Scope.UserID)
		_ = os.Setenv("SMOKE_SEARCH_ORG_ID", provisioned.Scope.OrgID)
		_ = os.Setenv("SMOKE_SEARCH_PROJECT_ID", provisioned.Scope.ProjectID)
		_ = os.Setenv("SMOKE_SEARCH_AGENT_ID", provisioned.Scope.AgentID)
		_ = os.Setenv("SMOKE_SEARCH_PERMISSION_LABEL", provisioned.Scope.PermissionLabel)
		if os.Getenv("SMOKE_ADAPTER_TOKEN") == "" {
			_ = os.Setenv("SMOKE_ADAPTER_TOKEN", provisioned.Token)
		}
	}
	return provisioned, nil
}

func pipelineE2ESmoke(ctx context.Context, apiBaseURL string) (err error) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("SMOKE_ENABLE_PIPELINE_E2E")), "true") {
		return nil
	}
	marker := strings.TrimSpace(os.Getenv("SMOKE_PIPELINE_E2E_MARKER"))
	if marker == "" {
		marker = fmt.Sprintf("pipeline-e2e-%d", time.Now().UnixNano())
	}
	actor := smokeActor()
	token := strings.TrimSpace(os.Getenv("SMOKE_ADAPTER_TOKEN"))
	searchToken := strings.TrimSpace(os.Getenv("SMOKE_SEARCH_PAT"))
	if token == "" {
		dsn := smokePostgresDSN()
		if dsn == "" {
			return fmt.Errorf("pipeline e2e smoke requires SMOKE_ADAPTER_TOKEN or SMOKE_POSTGRES_DSN")
		}
		var provisioned pipelineE2EActor
		provisioned, err = provisionPipelineE2EActor(ctx, dsn, marker)
		if err != nil {
			return fmt.Errorf("pipeline e2e actor provision failed: %w", err)
		}
		token = provisioned.Token
		searchToken = provisioned.SearchToken
		actor = provisioned.Scope
		if provisioned.Cleanup != nil {
			defer func() {
				if cleanupErr := provisioned.Cleanup(context.Background()); cleanupErr != nil && err == nil {
					err = fmt.Errorf("pipeline e2e actor cleanup failed: %w", cleanupErr)
				}
			}()
		}
	}
	if searchToken == "" {
		return fmt.Errorf("pipeline e2e smoke requires SMOKE_SEARCH_PAT or provisioned postgres actor")
	}
	body := fmt.Sprintf(`{"request_id":"pipeline-e2e-%[1]s","workspace":{"git_remote":"local/pipeline-e2e/%[1]s","git_root":"/pipeline-e2e/%[1]s","cwd":"/pipeline-e2e/%[1]s"},"event":{"version":"v1","event_id":"event_%[1]s","turn_id":"turn_%[1]s","thread_id":"thread_%[1]s","session_id":"session_%[1]s","type":"assistant_final","created_at":"2026-07-02T00:00:00Z","actor":{"user_id":"%[2]s","org_id":"%[3]s","project_id":"%[4]s","agent_id":"%[5]s"},"payload":{"text":"Memory OS pipeline e2e %[1]s sk-test-redacted-example"}}}`,
		marker, actor.UserID, actor.OrgID, actor.ProjectID, actor.AgentID)
	payload, err := postJSONWithBearerAllowStatus(ctx, strings.TrimRight(apiBaseURL, "/")+"/memory/turn-event", body, token)
	if err != nil {
		return err
	}
	if err := assertNoSecretLeak("pipeline e2e turn event", payload); err != nil {
		return err
	}
	expectedEventID := "event_" + marker
	if !strings.Contains(payload, `"event_id":"`+expectedEventID+`"`) || !strings.Contains(payload, `"status":"accepted"`) {
		return fmt.Errorf("pipeline e2e turn event response was not accepted: %s", payload)
	}

	deadline := time.Now().Add(pipelineE2ETimeout())
	if smokePostgresDSN() != "" && !strings.EqualFold(strings.TrimSpace(os.Getenv("SMOKE_PIPELINE_EXPECT_ARCHIVE")), "true") {
		return waitForPipelineCandidateJob(ctx, smokePostgresDSN(), expectedEventID, deadline)
	}

	searchBody := fmt.Sprintf(`{"request_id":"pipeline-search-%[1]s","query":"%[1]s","actor":{"user_id":"%[2]s","org_id":"%[3]s","project_id":"%[4]s","agent_id":"%[5]s"},"scope":"project","visibility":"project","permission_labels":["%[6]s"],"max_context_bytes":512}`,
		marker, actor.UserID, actor.OrgID, actor.ProjectID, actor.AgentID, actor.PermissionLabel)
	var lastPayload string
	for {
		searchPayload, err := postJSONWithBearerAllowStatus(ctx, strings.TrimRight(apiBaseURL, "/")+"/memory/search", searchBody, searchToken)
		if err != nil {
			lastPayload = err.Error()
		} else {
			lastPayload = searchPayload
			if err := assertNoSecretLeak("pipeline e2e search", searchPayload); err != nil {
				return err
			}
			if strings.Contains(searchPayload, marker) && strings.Contains(searchPayload, `"kind":"archive_chunk"`) {
				if err := mcpMemorySearchConsistencySmoke(ctx, marker, actor, searchToken); err != nil {
					return err
				}
				if err := adapterFixtureE2ESmoke(ctx, apiBaseURL, token, searchToken, actor, marker); err != nil {
					return err
				}
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("pipeline e2e smoke did not find archive chunk for marker %q: %s", marker, lastPayload)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func resolveSmokeHealthURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "http://localhost:18081/healthz"
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/healthz"
	}
	return parsed.String()
}

func mcpMemorySearchConsistencySmoke(ctx context.Context, marker string, actor smokeActorScope, searchToken string) error {
	mcpURL := strings.TrimSpace(os.Getenv("SMOKE_MCP_URL"))
	if mcpURL == "" {
		return nil
	}
	endpoint := strings.TrimRight(mcpURL, "/") + "/tools/call"
	request := map[string]any{
		"name": "memory_search",
		"arguments": map[string]any{
			"request_id":        "pipeline-mcp-search-" + marker,
			"query":             marker,
			"actor":             map[string]any{"user_id": actor.UserID, "org_id": actor.OrgID, "project_id": actor.ProjectID, "agent_id": actor.AgentID},
			"scope":             "project",
			"visibility":        "project",
			"permission_labels": []string{actor.PermissionLabel},
			"max_context_bytes": 512,
		},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	payload, err := postJSONWithBearerAllowStatus(ctx, endpoint, string(encoded), searchToken)
	if err != nil {
		return fmt.Errorf("mcp memory_search request failed: %w", err)
	}
	if err := assertNoSecretLeak("mcp memory_search", payload); err != nil {
		return err
	}
	if !strings.Contains(payload, `"code":"ok"`) || !strings.Contains(payload, marker) || !strings.Contains(payload, `"kind":"archive_chunk"`) {
		return fmt.Errorf("mcp memory_search did not match HTTP archive search for marker %q: %s", marker, payload)
	}
	return nil
}

type adapterFixtureCase struct {
	Name  string
	Path  string
	Query string
	Build func(adapter.Config) adapter.BatchAdapter
}

func adapterFixtureCases() []adapterFixtureCase {
	return []adapterFixtureCase{
		{
			Name:  "claude-code",
			Path:  "internal/adapter/fixtures/claude_code_sample.json",
			Query: "remember deploy uses docker compose",
			Build: func(config adapter.Config) adapter.BatchAdapter {
				return adapter.NewClaudeCodeAdapter(config)
			},
		},
		{
			Name:  "opencode",
			Path:  "internal/adapter/fixtures/opencode_sample.json",
			Query: "open code should index archive",
			Build: func(config adapter.Config) adapter.BatchAdapter {
				return adapter.NewOpenCodeAdapter(config)
			},
		},
		{
			Name:  "hermes",
			Path:  "internal/adapter/fixtures/hermes_sample.json",
			Query: "Hermes adapter sends TurnEvent",
			Build: func(config adapter.Config) adapter.BatchAdapter {
				return adapter.NewHermesAdapter(config)
			},
		},
		{
			Name:  "transcript",
			Path:  "internal/adapter/fixtures/transcript_sample.md",
			Query: "please migrate memory adapters",
			Build: func(config adapter.Config) adapter.BatchAdapter {
				return adapter.NewTranscriptImporter(config)
			},
		},
	}
}

func adapterFixtureE2ESmoke(ctx context.Context, apiBaseURL, adapterToken, searchToken string, actor smokeActorScope, marker string) error {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("SMOKE_ENABLE_ADAPTER_FIXTURE_E2E")), "true") {
		return nil
	}
	for _, fixture := range adapterFixtureCases() {
		if err := runAdapterFixtureE2E(ctx, apiBaseURL, adapterToken, searchToken, actor, marker, fixture); err != nil {
			return err
		}
	}
	return nil
}

func runAdapterFixtureE2E(ctx context.Context, apiBaseURL, adapterToken, searchToken string, actor smokeActorScope, marker string, fixture adapterFixtureCase) error {
	content, err := os.ReadFile(repoPath(fixture.Path))
	if err != nil {
		return fmt.Errorf("adapter fixture %s read failed: %w", fixture.Name, err)
	}
	fixtureActor := actor
	events, err := fixture.Build(adapter.Config{UserID: fixtureActor.UserID, OrgID: fixtureActor.OrgID, ProjectID: fixtureActor.ProjectID, AgentID: fixtureActor.AgentID}).Convert(adapter.BatchInput{SourceName: fixture.Path, Content: content})
	if err != nil {
		return fmt.Errorf("adapter fixture %s convert failed: %w", fixture.Name, err)
	}
	if len(events) == 0 {
		return fmt.Errorf("adapter fixture %s produced no TurnEvent", fixture.Name)
	}
	searchQuery := marker + " " + fixture.Query
	for index := range events {
		event := normalizeFixtureEvent(events[index], fixtureActor, marker, fixture.Name, index)
		if err := postFixtureTurnEvent(ctx, apiBaseURL, adapterToken, event, fixture.Name, marker, index); err != nil {
			return err
		}
	}
	return waitForFixtureArchiveSearch(ctx, apiBaseURL, searchToken, fixtureActor, searchQuery)
}

func normalizeFixtureEvent(event eventlog.TurnEvent, actor smokeActorScope, marker, fixtureName string, index int) eventlog.TurnEvent {
	suffix := fmt.Sprintf("%s_%s_%d", safeIDPart(marker), safeIDPart(fixtureName), index+1)
	event.EventID = "adapter_fixture_event_" + suffix
	event.TurnID = "adapter_fixture_turn_" + suffix
	event.ThreadID = "adapter_fixture_thread_" + suffix
	event.SessionID = "adapter_fixture_session_" + suffix
	event.Actor = eventlog.Actor{UserID: actor.UserID, OrgID: actor.OrgID, ProjectID: actor.ProjectID, AgentID: actor.AgentID}
	for key, value := range event.Payload {
		if text, ok := value.(string); ok {
			event.Payload[key] = marker + " " + text
		}
	}
	return event
}

func postFixtureTurnEvent(ctx context.Context, apiBaseURL, token string, event eventlog.TurnEvent, fixtureName, marker string, index int) error {
	body, err := json.Marshal(map[string]any{
		"request_id": fmt.Sprintf("adapter-fixture-%s-%s-%d", safeIDPart(fixtureName), safeIDPart(marker), index+1),
		"event":      event,
	})
	if err != nil {
		return err
	}
	payload, err := postJSONWithBearerAllowStatus(ctx, strings.TrimRight(apiBaseURL, "/")+"/memory/turn-event", string(body), token)
	if err != nil {
		return fmt.Errorf("adapter fixture %s turn event failed: %w", fixtureName, err)
	}
	if err := assertNoSecretLeak("adapter fixture turn event", payload); err != nil {
		return err
	}
	if !strings.Contains(payload, `"status":"accepted"`) && !strings.Contains(payload, `"deduped":true`) {
		return fmt.Errorf("adapter fixture %s turn event was not accepted: %s", fixtureName, payload)
	}
	return nil
}

func waitForFixtureArchiveSearch(ctx context.Context, apiBaseURL, token string, actor smokeActorScope, query string) error {
	searchBody := fmt.Sprintf(`{"request_id":"adapter-fixture-search-%[1]s","query":"%[2]s","actor":{"user_id":"%[3]s","org_id":"%[4]s","project_id":"%[5]s","agent_id":"%[6]s"},"scope":"project","visibility":"project","permission_labels":["%[7]s"],"max_context_bytes":768}`,
		safeIDPart(query), query, actor.UserID, actor.OrgID, actor.ProjectID, actor.AgentID, actor.PermissionLabel)
	deadline := time.Now().Add(pipelineE2ETimeout())
	var lastPayload string
	for {
		payload, err := postJSONWithBearerAllowStatus(ctx, strings.TrimRight(apiBaseURL, "/")+"/memory/search", searchBody, token)
		if err != nil {
			lastPayload = err.Error()
		} else {
			lastPayload = payload
			if err := assertNoSecretLeak("adapter fixture search", payload); err != nil {
				return err
			}
			if strings.Contains(payload, query) && strings.Contains(payload, `"kind":"archive_chunk"`) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("adapter fixture search did not find archive chunk for query %q: %s", query, lastPayload)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func safeIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "empty"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

func repoPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	dir, err := os.Getwd()
	if err != nil {
		return path
	}
	for {
		candidate := filepath.Join(dir, path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		dir = parent
	}
}

type pipelineE2EActor struct {
	Token       string
	WriteToken  string
	SearchToken string
	Scope       smokeActorScope
	Cleanup     func(context.Context) error
}

var waitForPipelineCandidateJob = waitForPipelineCandidateJobInPostgres

func waitForPipelineCandidateJobInPostgres(ctx context.Context, dsn string, eventID string, deadline time.Time) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	var lastStatus string
	var lastError string
	for {
		err := pool.QueryRow(ctx, `
SELECT status, coalesce(last_error, '')
FROM candidate_memory_jobs
WHERE source_event_id = $1
ORDER BY created_at DESC
LIMIT 1`, eventID).Scan(&lastStatus, &lastError)
		if err == nil {
			switch lastStatus {
			case "done":
				return nil
			case "failed":
				return fmt.Errorf("candidate job failed for event %s: %s", eventID, lastError)
			}
		}
		if time.Now().After(deadline) {
			if lastStatus == "" {
				return fmt.Errorf("candidate job was not created for event %s", eventID)
			}
			return fmt.Errorf("candidate job for event %s did not complete, last status %q: %s", eventID, lastStatus, lastError)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

var provisionPipelineE2EActor = provisionPipelineE2EActorFromPostgres

func provisionPipelineE2EActorFromPostgres(ctx context.Context, dsn, marker string) (pipelineE2EActor, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return pipelineE2EActor{}, err
	}
	defer pool.Close()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantService := tenant.NewService(tenant.NewPGRepository(pool))
	user, err := tenantService.CreateUser("memory-smoke-"+suffix+"@example.invalid", "Memory Smoke "+suffix)
	if err != nil {
		return pipelineE2EActor{}, err
	}
	org, err := tenantService.CreateOrg("Memory Smoke Org "+suffix, "memory-smoke-org-"+suffix)
	if err != nil {
		return pipelineE2EActor{}, err
	}
	project, err := tenantService.CreateProject(org.ID, "Memory Smoke Project "+suffix, "memory-smoke-project-"+suffix)
	if err != nil {
		return pipelineE2EActor{}, err
	}
	if err := tenantService.AddMembership(user.ID, org.ID, project.ID, tenant.RoleOwner); err != nil {
		return pipelineE2EActor{}, err
	}

	authService := auth.NewService(auth.NewPGRepository(pool))
	token, tokenRecord, err := authService.CreateAdapterToken(auth.AdapterTokenRequest{
		UserID:    user.ID,
		OrgID:     org.ID,
		ProjectID: project.ID,
		AgentID:   "codex",
		Scopes:    []string{"turn_event:write"},
		TTL:       30 * time.Minute,
	})
	if err != nil {
		return pipelineE2EActor{}, err
	}
	writeToken, writeTokenRecord, err := authService.CreatePAT(user.ID, "pipeline-e2e-write", []string{"memory:write"}, 30*time.Minute)
	if err != nil {
		_ = authService.RevokeAdapterToken(tokenRecord.ID)
		return pipelineE2EActor{}, err
	}
	searchToken, searchTokenRecord, err := authService.CreatePAT(user.ID, "pipeline-e2e-search", []string{"memory:read"}, 30*time.Minute)
	if err != nil {
		_ = authService.RevokePAT(writeTokenRecord.ID)
		_ = authService.RevokeAdapterToken(tokenRecord.ID)
		return pipelineE2EActor{}, err
	}
	return pipelineE2EActor{
		Token:       token,
		WriteToken:  writeToken,
		SearchToken: searchToken,
		Scope: smokeActorScope{
			UserID:          user.ID,
			OrgID:           org.ID,
			ProjectID:       project.ID,
			AgentID:         "codex",
			PermissionLabel: "project:" + project.ID + ":read",
		},
		Cleanup: func(cleanupCtx context.Context) error {
			cleanupPool, err := pgxpool.New(cleanupCtx, dsn)
			if err != nil {
				return err
			}
			defer cleanupPool.Close()
			cleanupAuth := auth.NewService(auth.NewPGRepository(cleanupPool))
			if err := cleanupAuth.RevokeAdapterToken(tokenRecord.ID); err != nil {
				return err
			}
			if err := cleanupAuth.RevokePAT(writeTokenRecord.ID); err != nil {
				return err
			}
			return cleanupAuth.RevokePAT(searchTokenRecord.ID)
		},
	}, nil
}

type smokeActorScope struct {
	UserID          string
	OrgID           string
	ProjectID       string
	AgentID         string
	PermissionLabel string
}

func smokeActor() smokeActorScope {
	projectID := envOrDefault("SMOKE_SEARCH_PROJECT_ID", "project_1")
	return smokeActorScope{
		UserID:          envOrDefault("SMOKE_SEARCH_USER_ID", "user_1"),
		OrgID:           envOrDefault("SMOKE_SEARCH_ORG_ID", "org_1"),
		ProjectID:       projectID,
		AgentID:         envOrDefault("SMOKE_SEARCH_AGENT_ID", "claude"),
		PermissionLabel: envOrDefault("SMOKE_SEARCH_PERMISSION_LABEL", "project:"+projectID+":read"),
	}
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func pipelineE2ETimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("SMOKE_PIPELINE_E2E_TIMEOUT"))
	if value == "" {
		return 30 * time.Second
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 30 * time.Second
	}
	return duration
}

func adapterDryRunSmoke(ctx context.Context) error {
	command := exec.CommandContext(ctx, "go", "run", "./cmd/memory-adapter", "--adapter", "transcript", "--dry-run", "--input", "internal/adapter/fixtures/transcript_sample.md")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("adapter dry-run smoke: %w: %s", err, stderr.String())
	}
	text := string(output)
	if !strings.Contains(text, `"version":"v1"`) || !strings.Contains(text, `"platform":"transcript"`) {
		return fmt.Errorf("adapter dry-run smoke missing TurnEvent v1 transcript output: %s", text)
	}
	if strings.Contains(text, "sk-test-redacted-example") {
		return fmt.Errorf("adapter dry-run smoke leaked fake secret")
	}
	return nil
}

func importerSmoke(ctx context.Context) error {
	stateDir, err := os.MkdirTemp("", "memory-importer-smoke-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stateDir)
	statePath := filepath.Join(stateDir, "state.json")

	dryRun := exec.CommandContext(ctx, "go", "run", "./cmd/memory-importer", "--source", "mem0", "--batch", "smoke_import_mem0", "--dry-run", "--input", "internal/importer/fixtures/mem0_sample.jsonl")
	var dryRunErr bytes.Buffer
	dryRun.Stderr = &dryRunErr
	dryRunOutput, err := dryRun.Output()
	if err != nil {
		return fmt.Errorf("importer dry-run smoke: %w: %s", err, dryRunErr.String())
	}
	dryRunText := string(dryRunOutput)
	if !strings.Contains(dryRunText, `"dry_run":true`) || !strings.Contains(dryRunText, `"item_count":2`) {
		return fmt.Errorf("importer dry-run smoke unexpected output: %s", dryRunText)
	}
	if strings.Contains(dryRunText, "sk-test-redacted-example") {
		return fmt.Errorf("importer dry-run smoke leaked fake secret")
	}

	apply := exec.CommandContext(ctx, "go", "run", "./cmd/memory-importer", "--source", "mem0", "--batch", "smoke_import_mem0", "--apply", "--state", statePath, "--input", "internal/importer/fixtures/mem0_sample.jsonl")
	var applyErr bytes.Buffer
	apply.Stderr = &applyErr
	applyOutput, err := apply.Output()
	if err != nil {
		return fmt.Errorf("importer apply smoke: %w: %s", err, applyErr.String())
	}
	applyText := string(applyOutput)
	if !strings.Contains(applyText, `"created_count":2`) || !strings.Contains(applyText, `"deduped_count":0`) {
		return fmt.Errorf("importer apply smoke unexpected output: %s", applyText)
	}
	if strings.Contains(applyText, "sk-test-redacted-example") {
		return fmt.Errorf("importer apply smoke leaked fake secret")
	}

	reapply := exec.CommandContext(ctx, "go", "run", "./cmd/memory-importer", "--source", "mem0", "--batch", "smoke_import_mem0", "--apply", "--state", statePath, "--input", "internal/importer/fixtures/mem0_sample.jsonl")
	var reapplyErr bytes.Buffer
	reapply.Stderr = &reapplyErr
	reapplyOutput, err := reapply.Output()
	if err != nil {
		return fmt.Errorf("importer reapply smoke: %w: %s", err, reapplyErr.String())
	}
	reapplyText := string(reapplyOutput)
	if !strings.Contains(reapplyText, `"created_count":0`) || !strings.Contains(reapplyText, `"deduped_count":2`) {
		return fmt.Errorf("importer reapply smoke unexpected output: %s", reapplyText)
	}
	if strings.Contains(reapplyText, "sk-test-redacted-example") {
		return fmt.Errorf("importer reapply smoke leaked fake secret")
	}

	exportBundle := exec.CommandContext(ctx, "go", "run", "./cmd/memory-importer", "--batch", "smoke_import_mem0", "--export-bundle", "--state", statePath)
	var exportErr bytes.Buffer
	exportBundle.Stderr = &exportErr
	exportOutput, err := exportBundle.Output()
	if err != nil {
		return fmt.Errorf("importer export smoke: %w: %s", err, exportErr.String())
	}
	applyText = string(exportOutput)
	if !strings.Contains(applyText, "Memory OS Export Bundle") || !strings.Contains(applyText, "source_refs") {
		return fmt.Errorf("importer export smoke unexpected output: %s", applyText)
	}
	if strings.Contains(applyText, "sk-test-redacted-example") {
		return fmt.Errorf("importer export smoke leaked fake secret")
	}
	return nil
}

func tenantGovernanceSmoke(ctx context.Context, apiBaseURL string) (err error) {
	if !tenantGovernanceSmokeEnabled() {
		return nil
	}
	dsn := smokePostgresDSN()
	if dsn == "" {
		return fmt.Errorf("tenant governance smoke requires SMOKE_POSTGRES_DSN or POSTGRES_DSN")
	}
	provisioned, err := provisionPipelineE2EActor(ctx, dsn, "tenant-governance-smoke")
	if err != nil {
		return fmt.Errorf("tenant governance actor provision failed: %w", err)
	}
	if provisioned.Cleanup != nil {
		defer func() {
			if cleanupErr := provisioned.Cleanup(context.Background()); cleanupErr != nil && err == nil {
				err = fmt.Errorf("tenant governance actor cleanup failed: %w", cleanupErr)
			}
		}()
	}
	if strings.TrimSpace(provisioned.WriteToken) == "" {
		return fmt.Errorf("tenant governance smoke requires provisioned write token")
	}
	if strings.TrimSpace(provisioned.SearchToken) == "" {
		return fmt.Errorf("tenant governance smoke requires provisioned read token")
	}

	suffix := safeIDPart(fmt.Sprintf("%d", time.Now().UnixNano()))
	userEmail := "tenant-smoke-" + suffix + "@example.invalid"
	userDisplayName := "Tenant Smoke " + suffix
	customRole := "tenant_smoke_" + suffix
	customRoleLabel := "project:" + provisioned.Scope.ProjectID + ":read"
	apiBaseURL = strings.TrimRight(apiBaseURL, "/")

	createUserPayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/users/create", fmt.Sprintf(`{"email":"%s","display_name":"%s"}`, userEmail, userDisplayName), provisioned.WriteToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke create user failed: %w", err)
	}
	if err := assertNoSecretLeak("tenant governance create user", createUserPayload); err != nil {
		return err
	}
	userID, err := extractNestedString(createUserPayload, "user", "user_id")
	if err != nil {
		return fmt.Errorf("tenant governance smoke create user parse failed: %w", err)
	}
	if !strings.Contains(createUserPayload, userEmail) || !strings.Contains(createUserPayload, `"status":"active"`) {
		return fmt.Errorf("tenant governance smoke create user response mismatch: %s", createUserPayload)
	}

	activeUsersPayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/users/list", `{"status":"active"}`, provisioned.SearchToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke list active users failed: %w", err)
	}
	if !strings.Contains(activeUsersPayload, userID) || !strings.Contains(activeUsersPayload, userEmail) {
		return fmt.Errorf("tenant governance smoke active users missing created user: %s", activeUsersPayload)
	}

	disableUserPayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/users/update-status", fmt.Sprintf(`{"user_id":"%s","status":"disabled"}`, userID), provisioned.WriteToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke disable user failed: %w", err)
	}
	if !strings.Contains(disableUserPayload, userID) || !strings.Contains(disableUserPayload, `"status":"disabled"`) {
		return fmt.Errorf("tenant governance smoke disable user response mismatch: %s", disableUserPayload)
	}

	disabledUsersPayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/users/list", `{"status":"disabled"}`, provisioned.SearchToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke list disabled users failed: %w", err)
	}
	if !strings.Contains(disabledUsersPayload, userID) || !strings.Contains(disabledUsersPayload, `"status":"disabled"`) {
		return fmt.Errorf("tenant governance smoke disabled users missing updated user: %s", disabledUsersPayload)
	}

	roleUpsertPayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/roles/upsert", fmt.Sprintf(`{"org_id":"%s","project_id":"%s","role":"%s","display_name":"Tenant Smoke Reader","description":"Smoke role","permission_labels":["%s"]}`, provisioned.Scope.OrgID, provisioned.Scope.ProjectID, customRole, customRoleLabel), provisioned.WriteToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke role upsert failed: %w", err)
	}
	if !strings.Contains(roleUpsertPayload, `"`+customRole+`"`) || !strings.Contains(roleUpsertPayload, customRoleLabel) {
		return fmt.Errorf("tenant governance smoke role upsert response mismatch: %s", roleUpsertPayload)
	}

	roleListPayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/roles/list", fmt.Sprintf(`{"org_id":"%s","project_id":"%s"}`, provisioned.Scope.OrgID, provisioned.Scope.ProjectID), provisioned.SearchToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke role list failed: %w", err)
	}
	if !strings.Contains(roleListPayload, `"`+customRole+`"`) || !strings.Contains(roleListPayload, customRoleLabel) {
		return fmt.Errorf("tenant governance smoke role list missing upserted role: %s", roleListPayload)
	}

	membershipAddPayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/memberships/add", fmt.Sprintf(`{"user_id":"%s","org_id":"%s","project_id":"%s","role":"%s"}`, userID, provisioned.Scope.OrgID, provisioned.Scope.ProjectID, customRole), provisioned.WriteToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke add membership failed: %w", err)
	}
	if !strings.Contains(membershipAddPayload, userID) || !strings.Contains(membershipAddPayload, customRole) || !strings.Contains(membershipAddPayload, `"status":"active"`) {
		return fmt.Errorf("tenant governance smoke add membership response mismatch: %s", membershipAddPayload)
	}

	membershipListPayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/memberships/list", fmt.Sprintf(`{"org_id":"%s","project_id":"%s"}`, provisioned.Scope.OrgID, provisioned.Scope.ProjectID), provisioned.SearchToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke membership list failed: %w", err)
	}
	if !strings.Contains(membershipListPayload, userID) || !strings.Contains(membershipListPayload, customRole) || !strings.Contains(membershipListPayload, `"status":"active"`) {
		return fmt.Errorf("tenant governance smoke membership list missing created membership: %s", membershipListPayload)
	}

	membershipUpdatePayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/memberships/update-role", fmt.Sprintf(`{"user_id":"%s","org_id":"%s","project_id":"%s","role":"admin"}`, userID, provisioned.Scope.OrgID, provisioned.Scope.ProjectID), provisioned.WriteToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke update membership failed: %w", err)
	}
	if !strings.Contains(membershipUpdatePayload, userID) || !strings.Contains(membershipUpdatePayload, `"role":"admin"`) || !strings.Contains(membershipUpdatePayload, `"status":"active"`) {
		return fmt.Errorf("tenant governance smoke update membership response mismatch: %s", membershipUpdatePayload)
	}

	membershipUpdatedListPayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/memberships/list", fmt.Sprintf(`{"org_id":"%s","project_id":"%s"}`, provisioned.Scope.OrgID, provisioned.Scope.ProjectID), provisioned.SearchToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke membership updated list failed: %w", err)
	}
	if !strings.Contains(membershipUpdatedListPayload, userID) || !strings.Contains(membershipUpdatedListPayload, `"role":"admin"`) || !strings.Contains(membershipUpdatedListPayload, `"status":"active"`) {
		return fmt.Errorf("tenant governance smoke membership updated list mismatch: %s", membershipUpdatedListPayload)
	}

	membershipRemovePayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/memberships/remove", fmt.Sprintf(`{"user_id":"%s","org_id":"%s","project_id":"%s"}`, userID, provisioned.Scope.OrgID, provisioned.Scope.ProjectID), provisioned.WriteToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke remove membership failed: %w", err)
	}
	if !strings.Contains(membershipRemovePayload, userID) || !strings.Contains(membershipRemovePayload, `"status":"disabled"`) {
		return fmt.Errorf("tenant governance smoke remove membership response mismatch: %s", membershipRemovePayload)
	}

	membershipRemovedListPayload, err := postJSONWithBearerAllowStatus(ctx, apiBaseURL+"/memory/tenant/memberships/list", fmt.Sprintf(`{"org_id":"%s","project_id":"%s"}`, provisioned.Scope.OrgID, provisioned.Scope.ProjectID), provisioned.SearchToken)
	if err != nil {
		return fmt.Errorf("tenant governance smoke removed membership list failed: %w", err)
	}
	if !strings.Contains(membershipRemovedListPayload, userID) || !strings.Contains(membershipRemovedListPayload, `"status":"disabled"`) {
		return fmt.Errorf("tenant governance smoke removed membership list mismatch: %s", membershipRemovedListPayload)
	}

	return nil
}

func tenantGovernanceSmokeEnabled() bool {
	value := strings.TrimSpace(os.Getenv("SMOKE_ENABLE_TENANT_GOVERNANCE"))
	if value != "" {
		return strings.EqualFold(value, "true")
	}
	return smokePostgresDSN() != ""
}

func extractNestedString(payload, objectKey, fieldKey string) (string, error) {
	var body map[string]any
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		return "", err
	}
	objectValue, ok := body[objectKey].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing object %q", objectKey)
	}
	fieldValue, ok := objectValue[fieldKey].(string)
	if !ok || strings.TrimSpace(fieldValue) == "" {
		return "", fmt.Errorf("missing string field %q.%s", objectKey, fieldKey)
	}
	return fieldValue, nil
}

func webSmoke(ctx context.Context) error {
	webURL := os.Getenv("SMOKE_WEB_URL")
	if webURL == "" {
		webURL = "http://localhost:18080"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, webURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("web smoke status %d", resp.StatusCode)
	}
	return nil
}

func postJSON(ctx context.Context, endpoint, body string) (string, error) {
	return postJSONAllowStatus(ctx, endpoint, body)
}

func postJSONAllowStatus(ctx context.Context, endpoint, body string, allowedStatuses ...int) (string, error) {
	return postJSONWithBearerAllowStatus(ctx, endpoint, body, "", allowedStatuses...)
}

func postJSONWithBearerAllowStatus(ctx context.Context, endpoint, body, token string, allowedStatuses ...int) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK && !allowedStatus(resp.StatusCode, allowedStatuses) {
		return string(encoded), fmt.Errorf("post %s status %d: %s", endpoint, resp.StatusCode, encoded)
	}
	return string(encoded), nil
}

func allowedStatus(status int, allowed []int) bool {
	for _, candidate := range allowed {
		if status == candidate {
			return true
		}
	}
	return false
}
