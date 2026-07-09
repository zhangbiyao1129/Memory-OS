package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/mcp"
	"memory-os/internal/memorykernel"
)

func TestBuildServerInjectsProductionContextPack(t *testing.T) {
	restore := stubProductionRetrieval(t)
	originalContextPack := newProductionContextPackService
	newProductionContextPackService = func(pool *pgxpool.Pool) memorykernel.ContextPackService {
		return memorykernel.NewContextPackBuilder(memorykernel.NewInMemoryRepository())
	}
	t.Cleanup(func() { newProductionContextPackService = originalContextPack })

	server, err := buildServerWithPool(productionMCPConfig(), &pgxpool.Pool{})
	if err != nil {
		t.Fatalf("buildServerWithPool() error = %v", err)
	}

	response := server.Handler.HandleTool("memory_context_pack", map[string]any{
		"query": "deploy API",
		"actor": map[string]any{"org_id": "org_1", "project_id": "project_1", "agent_id": "codex"},
	})

	if response.Code != "ok" {
		t.Fatalf("response = %#v, want ok", response)
	}
	if !restore.called {
		t.Fatal("production retrieval was not configured")
	}
}

func TestToolsCallContextPackRequiresTenantPermissions(t *testing.T) {
	authService, tenantService, token := mcpAuthFixture(t)
	if _, err := tenantService.CreateProject("org_1", "Project 2", "project-2"); err != nil {
		t.Fatalf("CreateProject(project-2) error = %v", err)
	}

	server := &Server{
		Addr:          ":18082",
		Tools:         mcp.Tools(),
		Handler:       mcp.NewHandler(mcp.HandlerOptions{ContextPackService: memorykernel.NewContextPackBuilder(memorykernel.NewInMemoryRepository())}),
		AuthService:   authService,
		TenantService: tenantService,
		RequireAuth:   true,
	}
	body := `{"name":"memory_context_pack","arguments":{"query":"deploy API","actor":{"org_id":"org_1","project_id":"project_2","agent_id":"codex"}}}`
	request := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s, want 403", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "mcp_forbidden") {
		t.Fatalf("response = %s, want mcp_forbidden", response.Body.String())
	}
}
