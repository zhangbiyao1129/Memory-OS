package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"memory-os/internal/health"
	"memory-os/internal/llm"
	"memory-os/internal/qdrant"
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
	if apiURL == "" {
		apiURL = "http://localhost:18081/healthz"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	if err := phase2Smoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/dev/smoke/phase2"); err != nil {
		return err
	}
	if err := turnEventSmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/memory/turn-event"); err != nil {
		return err
	}
	if err := archiveSmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/dev/smoke/archive"); err != nil {
		return err
	}
	if err := ragSmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/dev/smoke/rag"); err != nil {
		return err
	}
	if err := hotMemorySmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/dev/smoke/hot-memory"); err != nil {
		return err
	}
	if err := memorySearchSmoke(ctx, strings.TrimSuffix(apiURL, "/healthz")+"/memory/search"); err != nil {
		return err
	}
	if err := adapterDryRunSmoke(ctx); err != nil {
		return err
	}
	if err := importerSmoke(ctx); err != nil {
		return err
	}
	if err := webSmoke(ctx); err != nil {
		return err
	}

	return nil
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
	if err := client.EnsureCollection(ctx, qdrant.CollectionConfig{Name: qdrant.DefaultCollectionName, VectorSize: qdrant.DefaultVectorSize, Distance: qdrant.DefaultDistance}); err != nil {
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
	first, err := postJSON(ctx, endpoint, body)
	if err != nil {
		return err
	}
	if err := assertNoSecretLeak("turn event smoke", first); err != nil {
		return err
	}
	second, err := postJSON(ctx, endpoint, body)
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
	body := `{"request_id":"memory-search-smoke-1","query":"deploy API","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"claude"},"scope":"project","visibility":"project","permission_labels":["project:project_1:read"],"archive_index_generation":2,"max_context_bytes":512}`
	payload, err := postJSON(ctx, endpoint, body)
	if err != nil {
		return err
	}
	if strings.Contains(payload, "cross_tenant_leaked") || strings.Contains(payload, "sk-test-redacted-example") || strings.Contains(payload, "Codex private") {
		return fmt.Errorf("memory search smoke leaked isolated or secret content")
	}
	if !strings.Contains(payload, `"rerank_degraded":true`) || !strings.Contains(payload, `"kind":"hot_memory"`) || !strings.Contains(payload, `"kind":"archive_chunk"`) {
		return fmt.Errorf("memory search smoke missing unified retrieval evidence: %s", payload)
	}
	eveBody := `{"request_id":"memory-search-smoke-eve","query":"deploy","actor":{"user_id":"user_eve","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"scope":"project","visibility":"project","permission_labels":["project:project_1:read"],"archive_index_generation":2,"max_context_bytes":512}`
	evePayload, err := postJSON(ctx, endpoint, eveBody)
	if err != nil {
		return err
	}
	if strings.Contains(evePayload, `"kind":"hot_memory"`) || strings.Contains(evePayload, `"kind":"archive_chunk"`) {
		return fmt.Errorf("memory search smoke returned results for isolated user: %s", evePayload)
	}
	return nil
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

	apply := exec.CommandContext(ctx, "go", "run", "./cmd/memory-importer", "--source", "mem0", "--batch", "smoke_import_mem0", "--apply", "--export-bundle", "--input", "internal/importer/fixtures/mem0_sample.jsonl")
	var applyErr bytes.Buffer
	apply.Stderr = &applyErr
	applyOutput, err := apply.Output()
	if err != nil {
		return fmt.Errorf("importer apply smoke: %w: %s", err, applyErr.String())
	}
	applyText := string(applyOutput)
	if !strings.Contains(applyText, "Memory OS Export Bundle") || !strings.Contains(applyText, "source_refs") {
		return fmt.Errorf("importer export smoke unexpected output: %s", applyText)
	}
	if strings.Contains(applyText, "sk-test-redacted-example") {
		return fmt.Errorf("importer export smoke leaked fake secret")
	}
	return nil
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
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
	if resp.StatusCode != http.StatusOK {
		return string(encoded), fmt.Errorf("post %s status %d: %s", endpoint, resp.StatusCode, encoded)
	}
	return string(encoded), nil
}
