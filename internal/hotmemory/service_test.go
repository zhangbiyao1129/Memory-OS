package hotmemory

import (
	"strings"
	"testing"
	"time"

	"memory-os/internal/eventlog"
)

func TestServiceUpsertsAndDeduplicatesFactWithinScope(t *testing.T) {
	service := NewService(NewMemoryRepository())
	request := UpsertRequest{
		OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project",
		PermissionLabels: []string{"project:project_1:read"}, Fact: "Project uses docker compose on T480", SourceType: SourceTurnEvent, SourceRef: "event_1", Confidence: 0.8,
	}

	first, err := service.Upsert(request)
	if err != nil {
		t.Fatalf("first Upsert() error = %v", err)
	}
	second, err := service.Upsert(request)
	if err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}
	if first.MemoryID != second.MemoryID {
		t.Fatalf("memory id mismatch: %s != %s", first.MemoryID, second.MemoryID)
	}

	results, err := service.Search(SearchRequest{Query: "docker", Filter: mustFilter(t, FilterContext{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}})})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
}

func TestServiceUpsertSetsInitialHotScoreBeforeRepository(t *testing.T) {
	repository := &capturingRepository{}
	service := NewService(repository)

	memory, err := service.Upsert(UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Initial hot score must persist in Postgres", SourceType: SourceArchive, SourceRef: "archive_1", Confidence: 0.8})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if repository.upserted.HotScore <= 0 {
		t.Fatalf("repository received hot_score = %f, want positive initial score", repository.upserted.HotScore)
	}
	if memory.HotScore != repository.upserted.HotScore {
		t.Fatalf("returned hot_score = %f, repository hot_score = %f", memory.HotScore, repository.upserted.HotScore)
	}
}

func TestServiceSearchDoesNotReturnDeletedMemory(t *testing.T) {
	service := NewService(NewMemoryRepository())
	memory, err := service.Upsert(UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Archive RAG uses query time filter", SourceType: SourceArchive, SourceRef: "archive_1", Confidence: 0.7})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := service.Delete(memory.MemoryID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	results, err := service.Search(SearchRequest{Query: "Archive", Filter: mustFilter(t, FilterContext{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}})})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results len = %d, want 0", len(results))
	}
}

func TestAgentSpecificMemoryDefaultsToAgentIsolation(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if _, err := service.Upsert(UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeAgentSpecific, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Codex prefers compact turn summaries", SourceType: SourceTurnEvent, SourceRef: "event_1", Confidence: 0.9}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	results, err := service.Search(SearchRequest{Query: "Codex", Filter: mustFilter(t, FilterContext{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "claude", Scope: ScopeAgentSpecific, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}})})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("cross-agent results len = %d, want 0", len(results))
	}
}

func TestPromoteDemoteAndMarkUsedUpdateStateAndScore(t *testing.T) {
	service := NewService(NewMemoryRepository())
	memory, err := service.Upsert(UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Use bge-m3 for embeddings", SourceType: SourceTurnEvent, SourceRef: "event_1", Confidence: 0.75})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	promoted, err := service.Promote(memory.MemoryID)
	if err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	if promoted.Status != StatusPromoted || promoted.HotScore <= memory.HotScore {
		t.Fatalf("promoted = %#v, original = %#v", promoted, memory)
	}
	used, err := service.MarkUsed(memory.MemoryID)
	if err != nil {
		t.Fatalf("MarkUsed() error = %v", err)
	}
	if used.UsedCount != 1 || used.AccessCount != 1 {
		t.Fatalf("used counts = %d/%d, want 1/1", used.UsedCount, used.AccessCount)
	}
	demoted, err := service.Demote(memory.MemoryID)
	if err != nil {
		t.Fatalf("Demote() error = %v", err)
	}
	if demoted.Status != StatusDemoted || demoted.HotScore >= promoted.HotScore {
		t.Fatalf("demoted = %#v, promoted = %#v", demoted, promoted)
	}
}

func TestServiceEditUpdatesFactHashConfidenceAndSanitizesSecrets(t *testing.T) {
	service := NewService(NewMemoryRepository())
	memory, err := service.Upsert(UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Use bge-m3 for embeddings", SourceType: SourceTurnEvent, SourceRef: "event_1", Confidence: 0.75})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	edited, err := service.Edit(EditRequest{MemoryID: memory.MemoryID, Fact: "Use local embeddings via sk-test-redacted-example safely", Confidence: 0.9})
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}

	if edited.MemoryID != memory.MemoryID {
		t.Fatalf("edited memory id = %s, want %s", edited.MemoryID, memory.MemoryID)
	}
	if edited.FactHash == memory.FactHash {
		t.Fatalf("edited fact hash did not change: %s", edited.FactHash)
	}
	if edited.Confidence != 0.9 {
		t.Fatalf("edited confidence = %f, want 0.9", edited.Confidence)
	}
	if strings.Contains(edited.Fact, "sk-test-redacted-example") {
		t.Fatalf("edited fact leaked fake secret: %s", edited.Fact)
	}
	if !strings.Contains(edited.Fact, "secret_ref_hot_memory") {
		t.Fatalf("edited fact missing secret_ref replacement: %s", edited.Fact)
	}
}

func TestServiceWithVectorIndexIndexesMutationsAndSearchesQdrant(t *testing.T) {
	repository := NewMemoryRepository()
	vectorIndex := &fakeVectorIndex{
		searchResults: []SearchResult{{
			Memory: Memory{MemoryID: "hm_vector", Fact: "Vector indexed hot memory", HotScore: 9.1},
			Score:  0.91,
		}},
	}
	service := NewServiceWithVectorIndex(repository, vectorIndex)
	request := UpsertRequest{
		OrgID:            "org_1",
		ProjectID:        "project_1",
		UserID:           "user_1",
		AgentID:          "codex",
		Scope:            ScopeProject,
		Visibility:       "project",
		PermissionLabels: []string{"project:project_1:read"},
		Fact:             "Vector indexed hot memory",
		SourceType:       SourceArchive,
		SourceRef:        "archive_1",
		Confidence:       0.9,
	}

	memory, err := service.Upsert(request)
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if len(vectorIndex.indexed) != 1 || vectorIndex.indexed[0].MemoryID != memory.MemoryID {
		t.Fatalf("indexed memories = %#v, want upserted memory", vectorIndex.indexed)
	}

	edited, err := service.Edit(EditRequest{MemoryID: memory.MemoryID, Fact: "Vector indexed hot memory edited", Confidence: 0.8})
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if len(vectorIndex.indexed) != 2 || vectorIndex.indexed[1].Fact != edited.Fact {
		t.Fatalf("indexed after edit = %#v, want edited fact", vectorIndex.indexed)
	}

	filter := mustFilter(t, FilterContext{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}})
	results, err := service.Search(SearchRequest{Query: "Vector", Filter: filter})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].Memory.MemoryID != "hm_vector" {
		t.Fatalf("Search() results = %#v, want vector index results", results)
	}
	if vectorIndex.searchRequest.Filter.Must["doc_type"][0] != "hot_memory" {
		t.Fatalf("vector search filter = %#v, want hot_memory doc_type", vectorIndex.searchRequest.Filter.Must)
	}
	if got := vectorIndex.searchRequest.Filter.Must["status"]; len(got) != 3 || got[0] != string(StatusActive) {
		t.Fatalf("vector search status filter = %#v, want active/promoted/demoted", got)
	}

	if err := service.Delete(memory.MemoryID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(vectorIndex.deleted) != 1 || vectorIndex.deleted[0].MemoryID != memory.MemoryID || vectorIndex.deleted[0].Status != StatusDeleted {
		t.Fatalf("deleted memories = %#v, want deleted vector tombstone", vectorIndex.deleted)
	}
}

func TestExtractorSanitizesSecretsAndDeduplicatesCandidates(t *testing.T) {
	extractor := NewExtractor()
	event := eventlog.TurnEvent{EventID: "event_1", CreatedAt: time.Now().UTC(), Actor: eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"}, Payload: map[string]any{"text": "Project uses docker compose on T480. Project uses docker compose on T480. token sk-test-redacted-example"}}

	candidates := extractor.ExtractFromTurnEvent(event)
	if len(candidates) != 1 {
		t.Fatalf("candidates len = %d, want 1", len(candidates))
	}
	if strings.Contains(candidates[0].Fact, "sk-test-redacted-example") {
		t.Fatalf("candidate leaked fake secret: %s", candidates[0].Fact)
	}
	if candidates[0].SourceRef != "event_1" || candidates[0].Scope != ScopeProject {
		t.Fatalf("candidate source/scope mismatch: %#v", candidates[0])
	}
}

func TestExtractorSkipsLowValueText(t *testing.T) {
	extractor := NewExtractor()
	event := eventlog.TurnEvent{EventID: "event_1", Payload: map[string]any{"text": "ok"}}

	candidates := extractor.ExtractFromTurnEvent(event)

	if len(candidates) != 0 {
		t.Fatalf("candidates len = %d, want 0", len(candidates))
	}
}

func TestBuildFilterRejectsMissingPermissionAndAgentSpecificAgent(t *testing.T) {
	if _, err := BuildFilter(FilterContext{UserID: "user_1", Scope: ScopeProject, Visibility: "project"}); err == nil {
		t.Fatal("BuildFilter() error = nil, want missing permission labels")
	}
	if _, err := BuildFilter(FilterContext{UserID: "user_1", Scope: ScopeAgentSpecific, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}}); err == nil {
		t.Fatal("BuildFilter() error = nil, want missing agent id")
	}
}

func mustFilter(t *testing.T, ctx FilterContext) PayloadFilter {
	t.Helper()
	filter, err := BuildFilter(ctx)
	if err != nil {
		t.Fatalf("BuildFilter() error = %v", err)
	}
	return filter
}

type capturingRepository struct {
	upserted Memory
}

func (r *capturingRepository) Upsert(memory Memory) (Memory, error) {
	r.upserted = memory
	return memory, nil
}

func (r *capturingRepository) Get(memoryID string) (Memory, error) {
	return Memory{}, nil
}

func (r *capturingRepository) Search(filter map[string][]string) []Memory {
	return nil
}

func (r *capturingRepository) Update(memory Memory) (Memory, error) {
	return memory, nil
}

type fakeVectorIndex struct {
	indexed       []Memory
	deleted       []Memory
	searchRequest SearchRequest
	searchResults []SearchResult
}

func (i *fakeVectorIndex) Index(memory Memory) error {
	i.indexed = append(i.indexed, memory)
	return nil
}

func (i *fakeVectorIndex) Delete(memory Memory) error {
	i.deleted = append(i.deleted, memory)
	return nil
}

func (i *fakeVectorIndex) Search(request SearchRequest) ([]SearchResult, error) {
	i.searchRequest = request
	return append([]SearchResult(nil), i.searchResults...), nil
}
