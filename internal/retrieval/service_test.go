package retrieval

import (
	"errors"
	"strings"
	"testing"

	"memory-os/internal/archive"
	"memory-os/internal/hotmemory"
	"memory-os/internal/rag"
)

func TestSearchRejectsInvalidRequest(t *testing.T) {
	service := NewService(Options{})
	_, err := service.Search(SearchRequest{Query: "", Actor: Actor{UserID: "user_1"}, Visibility: "project", Scope: hotmemory.ScopeProject, PermissionLabels: []string{"project:project_1:read"}})
	if err == nil {
		t.Fatal("Search() error = nil, want empty query rejection")
	}

	_, err = service.Search(SearchRequest{Query: "deploy", Actor: Actor{UserID: "user_1"}, Visibility: "project", Scope: hotmemory.ScopeProject})
	if err == nil {
		t.Fatal("Search() error = nil, want missing permission rejection")
	}
}

func TestSearchMergesHotMemoryAndArchiveWithTraceableSources(t *testing.T) {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	shared, err := hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Project deploys API with docker compose", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_1", Confidence: 0.8})
	if err != nil {
		t.Fatalf("hot Upsert shared error = %v", err)
	}
	if _, err := hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeAgentSpecific, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Codex private deploy shortcut", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_agent", Confidence: 0.9}); err != nil {
		t.Fatalf("hot Upsert agent_specific error = %v", err)
	}
	ragService := rag.NewService(rag.NewMemoryStore())
	if err := ragService.Index(rag.IndexRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Chunks: []archive.Chunk{{ChunkID: "chunk_1", ArchiveID: "archive_1", IndexGeneration: 2, Content: "Archive says deploy API through docker compose on T480", ContentHash: "hash_1", SourceEventIDs: []string{"turn_event_2"}}}}); err != nil {
		t.Fatalf("rag Index current error = %v", err)
	}
	if err := ragService.Index(rag.IndexRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Chunks: []archive.Chunk{{ChunkID: "chunk_old", ArchiveID: "archive_1", IndexGeneration: 1, Content: "old deploy API note", ContentHash: "hash_old"}}}); err != nil {
		t.Fatalf("rag Index old error = %v", err)
	}

	log := NewMemoryAccessLog()
	service := NewService(Options{HotMemory: hot, ArchiveRAG: ragService, Reranker: StaticReranker{Scores: map[string]float64{"archive:chunk_1": 0.95, "hot_memory:" + shared.MemoryID: 0.9}}, AccessLog: log})
	response, err := service.Search(SearchRequest{RequestID: "req_1", Query: "deploy API", Actor: Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "claude"}, Visibility: "project", Scope: hotmemory.ScopeProject, PermissionLabels: []string{"project:project_1:read"}, ArchiveIndexGeneration: 2, MaxContextBytes: 512})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if response.RerankDegraded {
		t.Fatal("RerankDegraded = true, want false")
	}
	if len(response.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(response.Results))
	}
	if response.Results[0].Source.Kind != SourceArchiveChunk || response.Results[0].Source.ChunkID != "chunk_1" {
		t.Fatalf("top result source = %#v, want archive chunk_1", response.Results[0].Source)
	}
	if strings.Contains(response.Context, "chunk_old") || strings.Contains(response.Context, "Codex private") {
		t.Fatalf("context leaked old generation or cross-agent memory: %s", response.Context)
	}
	if log.Requests() != 1 || log.Results() != 2 {
		t.Fatalf("access logs = %d/%d, want 1/2", log.Requests(), log.Results())
	}
	updated, err := hot.MarkUsed(shared.MemoryID)
	if err != nil {
		t.Fatalf("MarkUsed verification error = %v", err)
	}
	if updated.UsedCount != 2 {
		t.Fatalf("used count after verification mark = %d, want 2", updated.UsedCount)
	}
}

func TestSearchDoesNotReturnDifferentUserMemory(t *testing.T) {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	if _, err := hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_2", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Eve deploy secret note", SourceType: hotmemory.SourceTurnEvent, SourceRef: "event_eve", Confidence: 0.8}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	service := NewService(Options{HotMemory: hot})

	response, err := service.Search(SearchRequest{RequestID: "req_2", Query: "deploy", Actor: Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"}, Visibility: "project", Scope: hotmemory.ScopeProject, PermissionLabels: []string{"project:project_1:read"}})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(response.Results) != 0 {
		t.Fatalf("results len = %d, want 0", len(response.Results))
	}
}

func TestRerankFailureDegradesAndCompressionSanitizesSecrets(t *testing.T) {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	if _, err := hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Deploy note with sk-test-redacted-example should be sanitized", SourceType: hotmemory.SourceTurnEvent, SourceRef: "event_1", Confidence: 0.8}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	service := NewService(Options{HotMemory: hot, Reranker: FailingReranker{Err: errors.New("rerank unavailable")}})

	response, err := service.Search(SearchRequest{RequestID: "req_3", Query: "Deploy", Actor: Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"}, Visibility: "project", Scope: hotmemory.ScopeProject, PermissionLabels: []string{"project:project_1:read"}, MaxContextBytes: 40})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !response.RerankDegraded {
		t.Fatal("RerankDegraded = false, want true")
	}
	if len(response.Context) > 40 {
		t.Fatalf("context len = %d, want <= 40", len(response.Context))
	}
	if strings.Contains(response.Context, "sk-test-redacted-example") {
		t.Fatalf("context leaked fake secret: %s", response.Context)
	}
	if len(response.Results) != 1 || response.Results[0].Source.Kind != SourceHotMemory {
		t.Fatalf("results = %#v, want one hot memory result", response.Results)
	}
}
