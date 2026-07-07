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

func TestSearchMarksAccessedReturnedAndDoesNotMarkUsedOnHotResults(t *testing.T) {
	hot := &trackingHotMemory{results: []hotmemory.SearchResult{{Memory: hotmemory.Memory{MemoryID: "hm_1", Fact: "deploy api note"}, Score: 0.9}}}
	service := NewService(Options{HotMemory: hot})

	response, err := service.Search(SearchRequest{RequestID: "req_1", Query: "deploy api", Actor: Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"}, Visibility: "project", Scope: hotmemory.ScopeProject, PermissionLabels: []string{"project:project_1:read"}})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(response.Results))
	}
	if hot.accessed != 2 || hot.returned != 2 {
		t.Fatalf("usage signals = accessed:%d returned:%d, want 2/2", hot.accessed, hot.returned)
	}
	if hot.used != 0 {
		t.Fatalf("used count = %d, want 0", hot.used)
	}
}

type trackingHotMemory struct {
	results  []hotmemory.SearchResult
	accessed int
	returned int
	used     int
}

func (h *trackingHotMemory) Search(request hotmemory.SearchRequest) ([]hotmemory.SearchResult, error) {
	return append([]hotmemory.SearchResult(nil), h.results...), nil
}

func (h *trackingHotMemory) MarkAccessed(string) (hotmemory.Memory, error) {
	h.accessed++
	return hotmemory.Memory{}, nil
}

func (h *trackingHotMemory) MarkReturned(string) (hotmemory.Memory, error) {
	h.returned++
	return hotmemory.Memory{}, nil
}

func (h *trackingHotMemory) MarkUsed(string) (hotmemory.Memory, error) {
	h.used++
	return hotmemory.Memory{}, nil
}

func TestSearchMergesHotMemoryAndArchiveWithTraceableSources(t *testing.T) {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	shared, err := hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Project deploy API uses docker compose", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_1", Confidence: 0.8})
	if err != nil {
		t.Fatalf("hot Upsert shared error = %v", err)
	}
	if _, err := hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeAgentSpecific, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Codex private deploy API shortcut", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_agent", Confidence: 0.9}); err != nil {
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
	if len(response.Results) != 3 {
		t.Fatalf("results len = %d, want 3", len(response.Results))
	}
	if response.Results[0].Source.Kind != SourceArchiveChunk || response.Results[0].Source.ChunkID != "chunk_1" {
		t.Fatalf("top result source = %#v, want archive chunk_1", response.Results[0].Source)
	}
	if strings.Contains(response.Context, "chunk_old") || !strings.Contains(response.Context, "Codex private deploy API shortcut") {
		t.Fatalf("context should exclude old generation and include cross-agent source memory: %s", response.Context)
	}
	if log.Requests() != 1 || log.Results() != 3 {
		t.Fatalf("access logs = %d/%d, want 1/3", log.Requests(), log.Results())
	}
	updated, err := hot.MarkUsed(shared.MemoryID)
	if err != nil {
		t.Fatalf("MarkUsed verification error = %v", err)
	}
	if updated.UsedCount != 1 {
		t.Fatalf("used count after verification mark = %d, want 1", updated.UsedCount)
	}
}

func TestSearchUsesFullQueryForRecall(t *testing.T) {
	archiveRAG := &capturingArchiveRAG{results: []rag.SearchResult{{Text: "deploy API archive", Score: 0.9, Source: rag.SourceRef{ArchiveID: "archive_1", ChunkID: "chunk_1"}}}}
	service := NewService(Options{ArchiveRAG: archiveRAG})

	_, err := service.Search(SearchRequest{
		RequestID:              "req_full_query",
		Query:                  "deploy API",
		Actor:                  Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Visibility:             "project",
		Scope:                  hotmemory.ScopeProject,
		PermissionLabels:       []string{"project:project_1:read"},
		ArchiveIndexGeneration: 2,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if archiveRAG.request.Query != "deploy API" {
		t.Fatalf("archive recall query = %q, want full user query", archiveRAG.request.Query)
	}
}

func TestSearchFiltersLowRerankScoresWhenThresholdConfigured(t *testing.T) {
	archiveRAG := &capturingArchiveRAG{results: []rag.SearchResult{
		{Text: "unrelated deployment note", Score: 0.62, Source: rag.SourceRef{ArchiveID: "archive_1", ChunkID: "chunk_bad"}},
		{Text: "backtest latest data shows final equity", Score: 0.58, Source: rag.SourceRef{ArchiveID: "archive_2", ChunkID: "chunk_good"}},
	}}
	service := NewService(Options{
		ArchiveRAG:     archiveRAG,
		Reranker:       StaticReranker{Scores: map[string]float64{"archive:chunk_bad": 0.04, "archive:chunk_good": 0.82}},
		MinRerankScore: 0.2,
	})

	response, err := service.Search(SearchRequest{
		RequestID:              "req_min_rerank",
		Query:                  "回测的最新数据是多少",
		Actor:                  Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Visibility:             "project",
		Scope:                  hotmemory.ScopeProject,
		PermissionLabels:       []string{"project:project_1:read"},
		ArchiveIndexGeneration: 2,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].Source.ChunkID != "chunk_good" {
		t.Fatalf("results = %#v, want only high rerank score candidate", response.Results)
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

func TestSearchRejectsArchiveRAGWithoutIndexGeneration(t *testing.T) {
	service := NewService(Options{ArchiveRAG: &capturingArchiveRAG{}})

	_, err := service.Search(SearchRequest{
		RequestID:              "req_missing_generation",
		Query:                  "deploy",
		Actor:                  Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Visibility:             "project",
		Scope:                  hotmemory.ScopeProject,
		PermissionLabels:       []string{"project:project_1:read"},
		ArchiveIndexGeneration: 0,
	})

	if err == nil {
		t.Fatal("Search() error = nil, want missing archive index generation rejection")
	}
}

func TestSearchResolvesArchiveIndexGenerationWhenMissing(t *testing.T) {
	archiveRAG := &capturingArchiveRAG{results: []rag.SearchResult{{Text: "deploy archive", Score: 0.9, Source: rag.SourceRef{ArchiveID: "archive_1", ChunkID: "chunk_1"}}}}
	resolver := &fakeArchiveGenerationResolver{generation: 7}
	service := NewService(Options{ArchiveRAG: archiveRAG, ArchiveGenerationResolver: resolver})

	_, err := service.Search(SearchRequest{
		RequestID:              "req_resolve_generation",
		Query:                  "deploy",
		Actor:                  Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Visibility:             "project",
		Scope:                  hotmemory.ScopeProject,
		PermissionLabels:       []string{"project:project_1:read"},
		ArchiveIndexGeneration: 0,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if resolver.context != (ArchiveGenerationContext{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1"}) {
		t.Fatalf("resolver context = %#v", resolver.context)
	}
	if got := archiveRAG.request.Filter.Must["index_generation"]; len(got) != 1 || got[0] != "7" {
		t.Fatalf("index_generation filter = %#v, want 7", got)
	}
}

func TestSearchSkipsArchiveRAGWhenNoArchiveGenerationExists(t *testing.T) {
	archiveRAG := &capturingArchiveRAG{}
	service := NewService(Options{ArchiveRAG: archiveRAG, ArchiveGenerationResolver: &fakeArchiveGenerationResolver{generation: 0}})

	response, err := service.Search(SearchRequest{
		RequestID:              "req_no_generation",
		Query:                  "deploy",
		Actor:                  Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Visibility:             "project",
		Scope:                  hotmemory.ScopeProject,
		PermissionLabels:       []string{"project:project_1:read"},
		ArchiveIndexGeneration: 0,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if archiveRAG.called {
		t.Fatal("ArchiveRAG was called without an available archive generation")
	}
	if len(response.Results) != 0 {
		t.Fatalf("results len = %d, want 0", len(response.Results))
	}
}

func TestSearchPassesFullArchiveRAGFilter(t *testing.T) {
	archiveRAG := &capturingArchiveRAG{results: []rag.SearchResult{{Text: "deploy archive", Score: 0.9, Source: rag.SourceRef{ArchiveID: "archive_1", ChunkID: "chunk_1"}}}}
	service := NewService(Options{ArchiveRAG: archiveRAG})

	_, err := service.Search(SearchRequest{
		RequestID:              "req_filter",
		Query:                  "deploy",
		Actor:                  Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Visibility:             "project",
		Scope:                  hotmemory.ScopeProject,
		PermissionLabels:       []string{"project:project_1:read"},
		ArchiveIndexGeneration: 2,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	want := map[string]string{
		"doc_type":         "archive_chunk",
		"user_id":          "user_1",
		"org_id":           "org_1",
		"project_id":       "project_1",
		"visibility":       "project",
		"index_generation": "2",
	}
	for key, value := range want {
		if got := archiveRAG.request.Filter.Must[key]; len(got) != 1 || got[0] != value {
			t.Fatalf("filter[%s] = %#v, want %q", key, got, value)
		}
	}
	if got := archiveRAG.request.Filter.Must["permission_labels"]; len(got) != 1 || got[0] != "project:project_1:read" {
		t.Fatalf("permission filter = %#v", got)
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

type capturingArchiveRAG struct {
	request rag.SearchRequest
	results []rag.SearchResult
	called  bool
}

func (r *capturingArchiveRAG) Search(request rag.SearchRequest) ([]rag.SearchResult, error) {
	r.called = true
	r.request = request
	if len(request.Filter.Must) == 0 {
		return nil, qdrantFilterMissingError{}
	}
	return append([]rag.SearchResult(nil), r.results...), nil
}

type qdrantFilterMissingError struct{}

func (qdrantFilterMissingError) Error() string {
	return "qdrant filter missing"
}

type fakeArchiveGenerationResolver struct {
	context    ArchiveGenerationContext
	generation int
	err        error
}

func (r *fakeArchiveGenerationResolver) CurrentGeneration(context ArchiveGenerationContext) (int, error) {
	r.context = context
	return r.generation, r.err
}

// TestSearchThenMarkUsedDrivesUsageSignalChain 是阶段 F2 的最小冒烟:
// 一次真实检索命中只驱动 access+returned,只有显式 mark_used 才增加 used,
// 且 hot_score 按 access -> returned -> used 单调递增。
func TestSearchThenMarkUsedDrivesUsageSignalChain(t *testing.T) {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	created, err := hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Deploy pipeline runs on T480", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_1", Confidence: 0.8})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	initialScore := created.HotScore

	service := NewService(Options{HotMemory: hot})
	if _, err := service.Search(SearchRequest{RequestID: "req_smoke", Query: "Deploy", Actor: Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"}, Visibility: "project", Scope: hotmemory.ScopeProject, PermissionLabels: []string{"project:project_1:read"}}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	afterSearch, err := hot.Get(created.MemoryID)
	if err != nil {
		t.Fatalf("Get() after search error = %v", err)
	}
	if afterSearch.AccessCount != 1 || afterSearch.ReturnedCount != 1 {
		t.Fatalf("after search access/returned = %d/%d, want 1/1", afterSearch.AccessCount, afterSearch.ReturnedCount)
	}
	if afterSearch.UsedCount != 0 {
		t.Fatalf("after search used = %d, want 0 (search must not mark used)", afterSearch.UsedCount)
	}
	if afterSearch.HotScore <= initialScore {
		t.Fatalf("after search hot_score = %f, want > initial %f", afterSearch.HotScore, initialScore)
	}

	afterUsed, err := hot.MarkUsed(created.MemoryID)
	if err != nil {
		t.Fatalf("MarkUsed() error = %v", err)
	}
	if afterUsed.UsedCount != 1 {
		t.Fatalf("after mark_used used = %d, want 1", afterUsed.UsedCount)
	}
	if afterUsed.HotScore <= afterSearch.HotScore {
		t.Fatalf("after mark_used hot_score = %f, want > after search %f", afterUsed.HotScore, afterSearch.HotScore)
	}
}
