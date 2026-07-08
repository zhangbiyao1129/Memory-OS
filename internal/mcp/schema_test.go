package mcp

import (
	"testing"

	"memory-os/internal/archive"
	"memory-os/internal/hotmemory"
	"memory-os/internal/rag"
	"memory-os/internal/retrieval"
)

func TestToolsContainRequiredMemoryTools(t *testing.T) {
	tools := Tools()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}

	required := []string{
		"memory_search",
		"memory_archive",
		"memory_append_event",
		"memory_get_archive",
		"memory_mark_used",
		"memory_stats",
	}
	for _, name := range required {
		if !names[name] {
			t.Fatalf("missing MCP tool %q", name)
		}
	}
}

func TestToolsExposeActionableInputSchemas(t *testing.T) {
	search := findTool(t, "memory_search")
	searchProperties := schemaProperties(t, search.InputSchema)
	for _, name := range []string{"query", "workspace", "actor", "limit"} {
		if _, ok := searchProperties[name]; !ok {
			t.Fatalf("memory_search schema missing property %q: %#v", name, search.InputSchema)
		}
	}
	searchRequired := stringSetFromAnySlice(t, search.InputSchema["required"])
	if !searchRequired["query"] {
		t.Fatalf("memory_search schema required = %#v, want query", search.InputSchema["required"])
	}

	appendEvent := findTool(t, "memory_append_event")
	appendProperties := schemaProperties(t, appendEvent.InputSchema)
	for _, name := range []string{"request_id", "workspace", "event"} {
		if _, ok := appendProperties[name]; !ok {
			t.Fatalf("memory_append_event schema missing property %q: %#v", name, appendEvent.InputSchema)
		}
	}
	appendRequired := stringSetFromAnySlice(t, appendEvent.InputSchema["required"])
	if !appendRequired["event"] {
		t.Fatalf("memory_append_event schema required = %#v, want event", appendEvent.InputSchema["required"])
	}
	if appendRequired["workspace"] {
		t.Fatalf("memory_append_event workspace must be optional for inbox/no-project capture: %#v", appendEvent.InputSchema["required"])
	}

	stats := findTool(t, "memory_stats")
	statsProperties := schemaProperties(t, stats.InputSchema)
	for _, name := range []string{"org_id", "project_id"} {
		if _, ok := statsProperties[name]; !ok {
			t.Fatalf("memory_stats schema missing property %q: %#v", name, stats.InputSchema)
		}
	}

	getArchive := findTool(t, "memory_get_archive")
	getArchiveProperties := schemaProperties(t, getArchive.InputSchema)
	if _, ok := getArchiveProperties["archive_id"]; !ok {
		t.Fatalf("memory_get_archive schema missing archive_id: %#v", getArchive.InputSchema)
	}
	getArchiveRequired := stringSetFromAnySlice(t, getArchive.InputSchema["required"])
	if !getArchiveRequired["archive_id"] {
		t.Fatalf("memory_get_archive required = %#v, want archive_id", getArchive.InputSchema["required"])
	}

	archive := findTool(t, "memory_archive")
	archiveProperties := schemaProperties(t, archive.InputSchema)
	for _, name := range []string{"request_id", "archive_id", "title", "content", "workspace", "actor"} {
		if _, ok := archiveProperties[name]; !ok {
			t.Fatalf("memory_archive schema missing property %q: %#v", name, archive.InputSchema)
		}
	}
	archiveRequired := stringSetFromAnySlice(t, archive.InputSchema["required"])
	if !archiveRequired["request_id"] || !archiveRequired["title"] || !archiveRequired["content"] {
		t.Fatalf("memory_archive required = %#v, want request_id/title/content", archive.InputSchema["required"])
	}
}

func TestHandleToolRunsMemoryMarkUsed(t *testing.T) {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	memory, err := hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "claude", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Mark used should update hot memory usage", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_1", Confidence: 0.8})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	handler := NewHandler(HandlerOptions{HotMemory: hot})

	response := handler.HandleTool("memory_mark_used", map[string]any{"memory_id": memory.MemoryID})

	if response.Code != "ok" {
		t.Fatalf("code = %q, want ok", response.Code)
	}
	updated, err := hot.Get(memory.MemoryID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if updated.UsedCount != 1 {
		t.Fatalf("used count = %d, want 1", updated.UsedCount)
	}
	if updated.LastUsedAt.IsZero() {
		t.Fatal("LastUsedAt should be set after memory_mark_used")
	}
}

func TestHandleToolRunsMemorySearch(t *testing.T) {
	handler := NewHandler(HandlerOptions{Retrieval: fixtureRetrievalService()})
	response := handler.HandleTool("memory_search", map[string]any{
		"request_id":               "mcp_search_1",
		"query":                    "deploy API",
		"actor":                    map[string]any{"user_id": "user_1", "org_id": "org_1", "project_id": "project_1", "agent_id": "claude"},
		"scope":                    "project",
		"visibility":               "project",
		"permission_labels":        []any{"project:project_1:read"},
		"archive_index_generation": float64(2),
		"max_context_bytes":        float64(512),
	})

	if response.Error != "" {
		t.Fatalf("response error = %q, want empty", response.Error)
	}
	if response.Code != "ok" {
		t.Fatalf("code = %q, want ok", response.Code)
	}
	if response.Search == nil {
		t.Fatal("Search result = nil, want unified retrieval response")
	}
	if response.Search.RequestID != "mcp_search_1" {
		t.Fatalf("request id = %q, want mcp_search_1", response.Search.RequestID)
	}
	if response.Search.AccessLogCount == 0 || response.Search.MarkedUsedCount == 0 {
		t.Fatalf("search did not log access or mark used: %#v", response.Search)
	}
	kinds := map[retrieval.SourceKind]bool{}
	for _, result := range response.Search.Results {
		kinds[result.Source.Kind] = true
	}
	if !kinds[retrieval.SourceHotMemory] || !kinds[retrieval.SourceArchiveChunk] {
		t.Fatalf("source kinds = %#v, want hot_memory and archive_chunk", kinds)
	}
}

func findTool(t *testing.T, name string) Tool {
	t.Helper()
	for _, tool := range Tools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return Tool{}
}

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want object", schema["properties"])
	}
	return properties
}

func stringSetFromAnySlice(t *testing.T, value any) map[string]bool {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("required = %#v, want []any", value)
	}
	set := map[string]bool{}
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("required item = %#v, want string", item)
		}
		set[text] = true
	}
	return set
}

func TestHandleToolMemorySearchDefaultsToProjectScopeAndVisibility(t *testing.T) {
	handler := NewHandler(HandlerOptions{Retrieval: fixtureRetrievalService()})
	response := handler.HandleTool("memory_search", map[string]any{
		"request_id":               "mcp_default_scope",
		"query":                    "deploy API",
		"actor":                    map[string]any{"user_id": "user_1", "org_id": "org_1", "project_id": "project_1", "agent_id": "claude"},
		"permission_labels":        []any{"project:project_1:read"},
		"archive_index_generation": float64(2),
		"max_context_bytes":        float64(512),
	})

	if response.Error != "" {
		t.Fatalf("response error = %q, want default project scope search", response.Error)
	}
	if response.Code != "ok" || response.Search == nil {
		t.Fatalf("response = %#v, want search ok", response)
	}
	if response.Search.RequestID != "mcp_default_scope" {
		t.Fatalf("request id = %q, want mcp_default_scope", response.Search.RequestID)
	}
}

func TestHandleToolMemorySearchMatchesHTTPRetrievalSemantics(t *testing.T) {
	service := fixtureRetrievalService()
	handler := NewHandler(HandlerOptions{Retrieval: service})
	args := map[string]any{
		"request_id":               "same_semantics",
		"query":                    "deploy API",
		"actor":                    map[string]any{"user_id": "user_1", "org_id": "org_1", "project_id": "project_1", "agent_id": "claude"},
		"scope":                    "project",
		"visibility":               "project",
		"permission_labels":        []any{"project:project_1:read"},
		"archive_index_generation": float64(2),
		"max_context_bytes":        float64(512),
	}

	mcpResponse := handler.HandleTool("memory_search", args)
	httpEquivalent, err := service.Search(retrieval.SearchRequest{
		RequestID:              "same_semantics",
		Query:                  "deploy API",
		Actor:                  retrieval.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "claude"},
		Scope:                  hotmemory.ScopeProject,
		Visibility:             "project",
		PermissionLabels:       []string{"project:project_1:read"},
		ArchiveIndexGeneration: 2,
		MaxContextBytes:        512,
	})
	if err != nil {
		t.Fatalf("HTTP-equivalent search error = %v", err)
	}

	if mcpResponse.Code != "ok" || mcpResponse.Search == nil {
		t.Fatalf("MCP response = %#v, want search ok", mcpResponse)
	}
	if len(mcpResponse.Search.Results) != len(httpEquivalent.Results) {
		t.Fatalf("MCP results len = %d, HTTP-equivalent len = %d", len(mcpResponse.Search.Results), len(httpEquivalent.Results))
	}
	for i := range httpEquivalent.Results {
		if mcpResponse.Search.Results[i].Source.Kind != httpEquivalent.Results[i].Source.Kind {
			t.Fatalf("result %d kind = %q, want %q", i, mcpResponse.Search.Results[i].Source.Kind, httpEquivalent.Results[i].Source.Kind)
		}
	}
	if mcpResponse.Search.Context != httpEquivalent.Context {
		t.Fatalf("MCP context = %q, HTTP-equivalent context = %q", mcpResponse.Search.Context, httpEquivalent.Context)
	}
}

func TestHandleToolMemorySearchRejectsUnconfiguredRetrieval(t *testing.T) {
	response := NewHandler(HandlerOptions{}).HandleTool("memory_search", map[string]any{"query": "hello"})

	if response.Code != "retrieval_not_configured" {
		t.Fatalf("code = %q, want retrieval_not_configured", response.Code)
	}
}

func TestHandleToolRejectsUnknownTool(t *testing.T) {
	response := HandleTool("unknown", nil)

	if response.Code != "unknown_tool" {
		t.Fatalf("code = %q, want unknown_tool", response.Code)
	}
}

func fixtureRetrievalService() retrieval.Service {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	_, _ = hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Project deploy API with docker compose on T480", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_1", Confidence: 0.8})
	_, _ = hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_2", ProjectID: "project_2", UserID: "user_2", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_2:read"}, Fact: "cross_tenant_leaked deploy note", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_cross", Confidence: 0.9})

	ragService := rag.NewService(rag.NewMemoryStore())
	_ = ragService.Index(rag.IndexRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Chunks: []archive.Chunk{{ChunkID: "chunk_1", ArchiveID: "archive_1", IndexGeneration: 2, Content: "Archive says deploy API through docker compose on T480", ContentHash: "hash_1", SourceEventIDs: []string{"turn_event_2"}}}})
	_ = ragService.Index(rag.IndexRequest{OrgID: "org_2", ProjectID: "project_2", UserID: "user_2", Visibility: "project", PermissionLabels: []string{"project:project_2:read"}, Chunks: []archive.Chunk{{ChunkID: "chunk_cross", ArchiveID: "archive_cross", IndexGeneration: 2, Content: "cross_tenant_leaked archive note", ContentHash: "hash_cross"}}})

	return retrieval.NewService(retrieval.Options{HotMemory: hot, ArchiveRAG: ragService, Reranker: retrieval.FailingReranker{}, AccessLog: retrieval.NewMemoryAccessLog()})
}
