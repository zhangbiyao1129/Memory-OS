package http

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/test/assert"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"memory-os/internal/health"
)

func TestHealthzReturnsReport(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(router, "GET", "/healthz", nil)

	assert.DeepEqual(t, 200, response.Code)

	var report health.Report
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatalf("healthz response is not health report JSON: %v", err)
	}
	if report.Status != health.StatusOK {
		t.Fatalf("status = %q, want %q", report.Status, health.StatusOK)
	}
}

func TestNotFound(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(router, "GET", "/missing", nil)

	assert.DeepEqual(t, 404, response.Code)
}

func TestOpenAPIJSON(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(router, "GET", "/openapi.json", nil)

	assert.DeepEqual(t, 200, response.Code)

	var spec map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &spec); err != nil {
		t.Fatalf("openapi response is not JSON: %v", err)
	}
	if spec["openapi"] == "" {
		t.Fatal("openapi version is empty")
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("openapi paths missing")
	}
	if _, ok := paths["/healthz"]; !ok {
		t.Fatal("openapi missing /healthz path")
	}
}

func TestDevPhase2SmokeDisabledOutsideDevelopment(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(router, "POST", "/dev/smoke/phase2", nil)

	assert.DeepEqual(t, 404, response.Code)
}

func TestDevPhase2SmokeRunsInDevelopment(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, health.NewService(nil), "development")

	response := ut.PerformRequest(h.Engine, "POST", "/dev/smoke/phase2", nil)

	assert.DeepEqual(t, 200, response.Code)

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("phase2 smoke response is not JSON: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status = %v, want ok", payload["status"])
	}
}

func TestTurnEventEndpointAcceptsEvent(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, health.NewService(nil), "development")

	body := `{"request_id":"request_1","event":{"version":"v1","event_id":"event_1","turn_id":"turn_1","thread_id":"thread_1","session_id":"session_1","type":"user_message","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"hello"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"status":"accepted"`) {
		t.Fatalf("response missing accepted status: %s", response.Body.String())
	}
}

func TestDevArchiveSmokeRunsInDevelopment(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, health.NewService(nil), "development")

	response := ut.PerformRequest(h.Engine, "POST", "/dev/smoke/archive", nil)

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"index_generation":2`) {
		t.Fatalf("archive smoke missing index generation increment: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "sk-test-redacted-example") {
		t.Fatal("archive smoke leaked fake secret")
	}
}

func TestDevRAGSmokeRunsInDevelopment(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, health.NewService(nil), "development")

	response := ut.PerformRequest(h.Engine, "POST", "/dev/smoke/rag", nil)

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"results":1`) {
		t.Fatalf("rag smoke missing result count: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "cross_tenant_leaked") {
		t.Fatal("rag smoke leaked cross tenant data")
	}
}

func TestDevHotMemorySmokeRunsInDevelopment(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, health.NewService(nil), "development")

	response := ut.PerformRequest(h.Engine, "POST", "/dev/smoke/hot-memory", nil)

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"results":1`) {
		t.Fatalf("hot memory smoke missing result count: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"used_count":1`) {
		t.Fatalf("hot memory smoke missing mark_used count: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "sk-test-redacted-example") {
		t.Fatal("hot memory smoke leaked fake secret")
	}
}

func TestMemorySearchEndpointReturnsUnifiedResults(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, health.NewService(nil), "development")

	body := `{"request_id":"search_smoke_1","query":"deploy API","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"claude"},"scope":"project","visibility":"project","permission_labels":["project:project_1:read"],"archive_index_generation":2,"max_context_bytes":512}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/search", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"rerank_degraded":true`) {
		t.Fatalf("memory search response missing rerank degraded fallback: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"kind":"hot_memory"`) || !strings.Contains(response.Body.String(), `"kind":"archive_chunk"`) {
		t.Fatalf("memory search response missing unified sources: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "cross_tenant_leaked") || strings.Contains(response.Body.String(), "sk-test-redacted-example") {
		t.Fatalf("memory search leaked isolated or secret content: %s", response.Body.String())
	}
}

func TestHealthHandlerUsesService(t *testing.T) {
	called := false
	service := health.NewService(map[string]health.Checker{
		"api": checkerFunc(func(context.Context) error {
			called = true
			return nil
		}),
	})
	handler := HealthHandler(service)
	ctx := app.NewContext(0)

	handler(context.Background(), ctx)

	if !called {
		t.Fatal("health checker was not called")
	}
	assert.DeepEqual(t, 200, ctx.Response.StatusCode())
}

type checkerFunc func(context.Context) error

func (f checkerFunc) Check(ctx context.Context) error {
	return f(ctx)
}
