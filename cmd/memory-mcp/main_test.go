package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
	"memory-os/internal/auth"
	"memory-os/internal/config"
	"memory-os/internal/hotmemory"
	"memory-os/internal/mcp"
	"memory-os/internal/rag"
	"memory-os/internal/retrieval"
	"memory-os/internal/tenant"
)

func TestBuildServer(t *testing.T) {
	server, err := buildServer(config.Config{MCPAddr: ":18082"})
	if err != nil {
		t.Fatalf("buildServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("buildServer() returned nil")
	}
}

func TestMCPLoggerOptionsUsesConfiguredEnvironment(t *testing.T) {
	options := mcpLoggerOptions(config.Config{AppEnv: "production"})

	if options.Environment != "production" {
		t.Fatalf("Environment = %q, want production", options.Environment)
	}
	if options.Service != "memory-mcp" {
		t.Fatalf("Service = %q, want memory-mcp", options.Service)
	}
}

func TestBuildServerRejectsMissingAddr(t *testing.T) {
	_, err := buildServer(config.Config{})
	if err == nil {
		t.Fatal("buildServer() error = nil, want missing addr error")
	}
}

func TestBuildServerRejectsMissingPostgresDSNInProduction(t *testing.T) {
	_, err := buildServer(config.Config{MCPAddr: ":18082", AppEnv: "production"})
	if !errors.Is(err, errMissingProductionPostgresDSN) {
		t.Fatalf("buildServer() error = %v, want %v", err, errMissingProductionPostgresDSN)
	}
}

func TestBuildServerRejectsMissingQdrantURLInProduction(t *testing.T) {
	cfg := productionMCPConfig()
	cfg.QdrantURL = ""

	_, err := buildServerWithPool(cfg, &pgxpool.Pool{})
	if !errors.Is(err, errMissingProductionQdrantURL) {
		t.Fatalf("buildServerWithPool() error = %v, want %v", err, errMissingProductionQdrantURL)
	}
}

func TestBuildServerRejectsMissingEmbeddingConfigInProduction(t *testing.T) {
	cfg := productionMCPConfig()
	cfg.LLMAPIKey = ""

	_, err := buildServerWithPool(cfg, &pgxpool.Pool{})
	if !errors.Is(err, errInvalidProductionEmbeddingConfig) {
		t.Fatalf("buildServerWithPool() error = %v, want %v", err, errInvalidProductionEmbeddingConfig)
	}
}

func TestBuildServerInjectsProductionRetrievalWhenPoolExists(t *testing.T) {
	restore := stubProductionRetrieval(t)
	server, err := buildServerWithPool(productionMCPConfig(), &pgxpool.Pool{})
	if err != nil {
		t.Fatalf("buildServerWithPool() error = %v", err)
	}
	if server == nil {
		t.Fatal("server = nil")
	}
	response := server.Handler.HandleTool("memory_search", map[string]any{
		"request_id":               "mcp_prod_1",
		"query":                    "deploy API",
		"actor":                    map[string]any{"user_id": "user_1", "org_id": "org_1", "project_id": "project_1", "agent_id": "claude"},
		"scope":                    "project",
		"visibility":               "project",
		"permission_labels":        []any{"project:project_1:read"},
		"archive_index_generation": float64(2),
	})
	if response.Code != "ok" || response.Search == nil {
		t.Fatalf("response = %#v, want configured production search", response)
	}
	if !restore.called {
		t.Fatal("production retrieval was not configured")
	}
}

func TestToolsCallRunsMemorySearch(t *testing.T) {
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()})}
	body := `{"name":"memory_search","arguments":{"request_id":"mcp_http_1","query":"deploy API","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"claude"},"scope":"project","visibility":"project","permission_labels":["project:project_1:read"],"archive_index_generation":2,"max_context_bytes":512}}`
	request := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, want := range []string{`"code":"ok"`, `"search"`, `"request_id":"mcp_http_1"`, `"kind":"hot_memory"`, `"kind":"archive_chunk"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("response missing %s: %s", want, response.Body.String())
		}
	}
}

func TestMCPStreamableHTTPInitializeAndToolsList(t *testing.T) {
	authService, tenantService, token := mcpAuthFixture(t)
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()}), AuthService: authService, TenantService: tenantService, RequireAuth: true}

	initialize := postMCPRPC(t, server, token, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"acceptance","version":"0"}}}`)
	if initialize.Code != http.StatusOK {
		t.Fatalf("initialize status = %d body = %s, want 200", initialize.Code, initialize.Body.String())
	}
	for _, want := range []string{`"jsonrpc":"2.0"`, `"protocolVersion":"2025-03-26"`, `"serverInfo"`, `"tools"`} {
		if !strings.Contains(initialize.Body.String(), want) {
			t.Fatalf("initialize response missing %s: %s", want, initialize.Body.String())
		}
	}

	toolsList := postMCPRPC(t, server, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if toolsList.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d body = %s, want 200", toolsList.Code, toolsList.Body.String())
	}
	for _, want := range []string{`"memory_search"`, `"inputSchema"`, `"result"`} {
		if !strings.Contains(toolsList.Body.String(), want) {
			t.Fatalf("tools/list response missing %s: %s", want, toolsList.Body.String())
		}
	}
}

func TestMCPStreamableHTTPToolCallRunsMemorySearch(t *testing.T) {
	authService, tenantService, token := mcpAuthFixture(t)
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()}), AuthService: authService, TenantService: tenantService, RequireAuth: true}
	body := `{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"memory_search","arguments":{"request_id":"mcp_streamable_search","query":"deploy API","actor":{"org_id":"org_1","project_id":"project_1"},"scope":"project","visibility":"project","archive_index_generation":2,"max_context_bytes":512}}}`
	response := postMCPRPC(t, server, token, body)

	if response.Code != http.StatusOK {
		t.Fatalf("tools/call status = %d body = %s, want 200", response.Code, response.Body.String())
	}
	for _, want := range []string{`"jsonrpc":"2.0"`, `"isError":false`, `"content"`, `\"request_id\":\"mcp_streamable_search\"`, `\"kind\":\"archive_chunk\"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("tools/call response missing %s: %s", want, response.Body.String())
		}
	}
}

func TestMCPStreamableHTTPRequiresPATWhenAuthConfigured(t *testing.T) {
	authService, tenantService, _ := mcpAuthFixture(t)
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()}), AuthService: authService, TenantService: tenantService, RequireAuth: true}
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s, want 401", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "pat_required") {
		t.Fatalf("response = %s, want pat_required", response.Body.String())
	}
}

func TestMCPStreamableHTTPRejectsGET(t *testing.T) {
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()})}
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/mcp", nil))

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d body = %s, want 405", response.Code, response.Body.String())
	}
}

func TestToolsCallRejectsInvalidMethod(t *testing.T) {
	server, err := buildServer(config.Config{MCPAddr: ":18082"})
	if err != nil {
		t.Fatalf("buildServer() error = %v", err)
	}
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tools/call", nil))

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", response.Code)
	}
}

func TestToolsListRequiresPATWhenAuthConfigured(t *testing.T) {
	authService, tenantService, _ := mcpAuthFixture(t)
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()}), AuthService: authService, TenantService: tenantService, RequireAuth: true}
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tools", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s, want 401", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "pat_required") {
		t.Fatalf("response = %s, want pat_required", response.Body.String())
	}
}

func TestToolsListAcceptsPATWhenAuthConfigured(t *testing.T) {
	authService, tenantService, token := mcpAuthFixture(t)
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()}), AuthService: authService, TenantService: tenantService, RequireAuth: true}
	request := httptest.NewRequest(http.MethodGet, "/tools", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "memory_search") {
		t.Fatalf("response = %s, want memory_search tool", response.Body.String())
	}
}

func TestToolsCallRequiresPATWhenAuthConfigured(t *testing.T) {
	authService, tenantService, _ := mcpAuthFixture(t)
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()}), AuthService: authService, TenantService: tenantService, RequireAuth: true}
	body := `{"name":"memory_search","arguments":{"request_id":"mcp_auth_required","query":"deploy API","actor":{"org_id":"org_1","project_id":"project_1","agent_id":"codex"},"scope":"project","visibility":"project","max_context_bytes":512}}`
	request := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s, want 401", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "pat_required") {
		t.Fatalf("response = %s, want pat_required", response.Body.String())
	}
}

func TestToolsCallUsesPATSubjectAndTenantPermissions(t *testing.T) {
	authService, tenantService, token := mcpAuthFixture(t)
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()}), AuthService: authService, TenantService: tenantService, RequireAuth: true}
	body := `{"name":"memory_search","arguments":{"request_id":"mcp_auth_search","query":"deploy API","actor":{"user_id":"attacker","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"scope":"project","visibility":"project","permission_labels":["project:attacker:read"],"archive_index_generation":2,"max_context_bytes":512}}`
	request := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"ok"`) {
		t.Fatalf("response = %s, want code ok", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"kind":"archive_chunk"`) || !strings.Contains(response.Body.String(), `"kind":"hot_memory"`) {
		t.Fatalf("response missing expected sources: %s", response.Body.String())
	}
}

func TestToolsCallInfersAgentIDFromUserAgent(t *testing.T) {
	authService, tenantService, token := mcpAuthFixture(t)
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()}), AuthService: authService, TenantService: tenantService, RequireAuth: true}
	body := `{"name":"memory_search","arguments":{"request_id":"mcp_infer_agent","query":"deploy API","actor":{"org_id":"org_1","project_id":"project_1"},"scope":"project","visibility":"project","archive_index_generation":2,"max_context_bytes":512}}`
	request := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", "Claude-Code/2.1.195")
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"ok"`) {
		t.Fatalf("response = %s, want code ok", response.Body.String())
	}
}

func TestInferAgentIDFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		headerName string
		header     string
		userAgent  string
		want       string
	}{
		{name: "explicit header wins", headerName: "X-Memory-Agent-ID", header: "team-agent", userAgent: "Claude-Code/2.1", want: "team-agent"},
		{name: "claude code", userAgent: "Claude-Code/2.1.195", want: "claude-code"},
		{name: "codex", userAgent: "OpenAI Codex", want: "codex"},
		{name: "cursor", userAgent: "Cursor/1.0", want: "cursor"},
		{name: "opencode", userAgent: "opencode", want: "opencode"},
		{name: "fallback", userAgent: "unknown-client", want: "mcp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/tools/call", nil)
			if tt.headerName != "" {
				request.Header.Set(tt.headerName, tt.header)
			}
			request.Header.Set("User-Agent", tt.userAgent)

			if got := inferAgentIDFromRequest(request); got != tt.want {
				t.Fatalf("inferAgentIDFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolsCallUsesWorkspaceIdentityWhenActorIsOmitted(t *testing.T) {
	authService, tenantService, token := mcpAuthFixture(t)
	server := &Server{Addr: ":18082", Tools: mcp.Tools(), Handler: mcp.NewHandler(mcp.HandlerOptions{Retrieval: fixtureRetrievalService()}), AuthService: authService, TenantService: tenantService, RequireAuth: true}
	body := `{"name":"memory_search","arguments":{"request_id":"mcp_workspace_search","query":"deploy API","workspace":{"git_remote":"git@gitlab.example.com:team/memory-os.git","git_root":"/work/memory-os","cwd":"/work/memory-os","git_branch":"main"},"scope":"project","visibility":"project","archive_index_generation":2,"max_context_bytes":512}}`
	request := httptest.NewRequest(http.MethodPost, "/tools/call", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"ok"`) {
		t.Fatalf("response = %s, want code ok", response.Body.String())
	}
}

type retrievalStubState struct {
	called bool
}

func stubProductionRetrieval(t *testing.T) *retrievalStubState {
	t.Helper()
	state := &retrievalStubState{}
	old := newProductionRetrieval
	newProductionRetrieval = func(cfg config.Config, pool *pgxpool.Pool) (retrieval.Service, error) {
		state.called = cfg.AppEnv == "production" && pool != nil
		return fixtureRetrievalService(), nil
	}
	t.Cleanup(func() { newProductionRetrieval = old })
	return state
}

func productionMCPConfig() config.Config {
	return config.Config{
		MCPAddr:        ":18082",
		AppEnv:         "production",
		PostgresDSN:    "postgres://memory_os:secret@postgres:5432/memory_os",
		QdrantURL:      "http://qdrant:6333",
		LLMBaseURL:     "http://llm.local:8000",
		LLMAPIKey:      "test-key",
		EmbeddingModel: "bge-m3",
	}
}

func mcpAuthFixture(t *testing.T) (auth.Service, tenant.Service, string) {
	t.Helper()
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "mcp", []string{"memory:read", "memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := tenant.NewService(tenant.NewMemoryRepository())
	if _, err := tenantService.CreateUser("user_1@example.test", "User 1"); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	org, err := tenantService.CreateOrg("Org 1", "org-1")
	if err != nil {
		t.Fatalf("CreateOrg() error = %v", err)
	}
	project, err := tenantService.CreateProject(org.ID, "Project 1", "project-1")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if org.ID != "org_1" || project.ID != "project_1" {
		t.Fatalf("fixture ids org=%q project=%q, want org_1/project_1", org.ID, project.ID)
	}
	if err := tenantService.AddMembership("user_1", org.ID, project.ID, tenant.RoleOwner); err != nil {
		t.Fatalf("AddMembership() error = %v", err)
	}
	return authService, tenantService, token
}

func fixtureRetrievalService() retrieval.Service {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	_, _ = hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Project deploy API with docker compose on T480", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_1", Confidence: 0.8})

	ragService := rag.NewService(rag.NewMemoryStore())
	_ = ragService.Index(rag.IndexRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Chunks: []archive.Chunk{{ChunkID: "chunk_1", ArchiveID: "archive_1", IndexGeneration: 2, Content: "Archive says deploy API through docker compose on T480", ContentHash: "hash_1", SourceEventIDs: []string{"turn_event_2"}}}})

	return retrieval.NewService(retrieval.Options{HotMemory: hot, ArchiveRAG: ragService, Reranker: retrieval.FailingReranker{}, AccessLog: retrieval.NewMemoryAccessLog()})
}

func postMCPRPC(t *testing.T, server *Server, token string, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", "Claude-Code/2.1.195")
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, request)

	var decoded map[string]any
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("response is not JSON: %v body = %s", err, response.Body.String())
		}
	}
	return response
}
