package hotmemory

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"memory-os/internal/eventlog"
)

func TestSetPinnedTogglesPinAndBoostsHotScore(t *testing.T) {
	service := NewService(NewMemoryRepository())
	memory, err := service.Upsert(UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Pinned memory must survive demotion", SourceType: SourceTurnEvent, SourceRef: "event_pin", Confidence: 0.2})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if memory.Pinned {
		t.Fatal("memory should not be pinned by default")
	}

	pinned, err := service.SetPinned(memory.MemoryID, true)
	if err != nil {
		t.Fatalf("SetPinned(true) error = %v", err)
	}
	if !pinned.Pinned {
		t.Fatal("memory should be pinned after SetPinned(true)")
	}
	if pinned.HotScore <= memory.HotScore {
		t.Fatalf("pinned hot score = %f, want > %f", pinned.HotScore, memory.HotScore)
	}

	unpinned, err := service.SetPinned(memory.MemoryID, false)
	if err != nil {
		t.Fatalf("SetPinned(false) error = %v", err)
	}
	if unpinned.Pinned {
		t.Fatal("memory should be unpinned after SetPinned(false)")
	}
}

func TestUsageSignalsAreAtomicUnderConcurrency(t *testing.T) {
	service := NewService(NewMemoryRepository())
	memory, err := service.Upsert(UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Concurrent usage signal increments must not be lost", SourceType: SourceTurnEvent, SourceRef: "event_concurrent", Confidence: 0.2})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	const workers = 50
	var wg sync.WaitGroup
	wg.Add(workers * 3)
	for i := 0; i < workers; i++ {
		go func() { defer wg.Done(); service.MarkAccessed(memory.MemoryID) }()
		go func() { defer wg.Done(); service.MarkReturned(memory.MemoryID) }()
		go func() { defer wg.Done(); service.MarkUsed(memory.MemoryID) }()
	}
	wg.Wait()

	got, err := service.Get(memory.MemoryID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.AccessCount != workers {
		t.Fatalf("access count = %d, want %d (lost updates under concurrency)", got.AccessCount, workers)
	}
	if got.ReturnedCount != workers {
		t.Fatalf("returned count = %d, want %d (lost updates under concurrency)", got.ReturnedCount, workers)
	}
	if got.UsedCount != workers {
		t.Fatalf("used count = %d, want %d (lost updates under concurrency)", got.UsedCount, workers)
	}
}

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

func TestAgentSpecificMemoryIsVisibleAcrossAgentsAsSourceMetadata(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if _, err := service.Upsert(UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeAgentSpecific, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Codex prefers compact turn summaries", SourceType: SourceTurnEvent, SourceRef: "event_1", Confidence: 0.9}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	results, err := service.Search(SearchRequest{Query: "Codex", Filter: mustFilter(t, FilterContext{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "claude", Scope: ScopeAgentSpecific, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}})})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("cross-agent results len = %d, want 1", len(results))
	}
	if results[0].Memory.AgentID != "codex" {
		t.Fatalf("source agent = %q, want codex", results[0].Memory.AgentID)
	}
}

func TestServiceDeduplicatesFactAcrossAgentsWithinProject(t *testing.T) {
	service := NewService(NewMemoryRepository())
	request := UpsertRequest{
		OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project",
		PermissionLabels: []string{"project:project_1:read"}, Fact: "Project stores memory by project context", SourceType: SourceTurnEvent, SourceRef: "event_1", Confidence: 0.8,
	}
	first, err := service.Upsert(request)
	if err != nil {
		t.Fatalf("first Upsert() error = %v", err)
	}
	request.AgentID = "claude"
	request.SourceRef = "event_2"
	second, err := service.Upsert(request)
	if err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}
	if first.MemoryID != second.MemoryID {
		t.Fatalf("memory id mismatch across agents: %s != %s", first.MemoryID, second.MemoryID)
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
	if used.UsedCount != 1 || used.AccessCount != 0 {
		t.Fatalf("used counts = %d/%d, want 1/0", used.UsedCount, used.AccessCount)
	}
	if got := readTimeField(t, used, "LastUsedAt"); got.IsZero() {
		t.Fatal("LastUsedAt should be set after MarkUsed")
	}
	demoted, err := service.Demote(memory.MemoryID)
	if err != nil {
		t.Fatalf("Demote() error = %v", err)
	}
	if demoted.Status != StatusDemoted || demoted.HotScore >= promoted.HotScore {
		t.Fatalf("demoted = %#v, promoted = %#v", demoted, promoted)
	}
}

func TestServiceAccessedReturnedAndUsedUpdateUsageSignals(t *testing.T) {
	service := NewService(NewMemoryRepository())
	memory, err := service.Upsert(UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Hot memory should track usage signals", SourceType: SourceTurnEvent, SourceRef: "event_usage", Confidence: 0.2})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	accessed := callUsageSignal(t, service, "MarkAccessed", memory.MemoryID)
	if accessed.AccessCount != 1 || accessed.UsedCount != 0 {
		t.Fatalf("accessed counts = %d/%d, want 1/0", accessed.AccessCount, accessed.UsedCount)
	}
	if got := readTimeField(t, accessed, "LastAccessedAt"); got.IsZero() {
		t.Fatal("LastAccessedAt should be set after MarkAccessed")
	}
	if got := readIntField(t, accessed, "ReturnedCount"); got != 0 {
		t.Fatalf("returned count after access = %d, want 0", got)
	}

	returned := callUsageSignal(t, service, "MarkReturned", memory.MemoryID)
	if returned.AccessCount != 1 || returned.UsedCount != 0 {
		t.Fatalf("returned counts = %d/%d, want 1/0", returned.AccessCount, returned.UsedCount)
	}
	if got := readIntField(t, returned, "ReturnedCount"); got != 1 {
		t.Fatalf("returned count after MarkReturned = %d, want 1", got)
	}
	if got := readTimeField(t, returned, "LastReturnedAt"); got.IsZero() {
		t.Fatal("LastReturnedAt should be set after MarkReturned")
	}
	if returned.HotScore <= accessed.HotScore {
		t.Fatalf("returned hot score = %f, want > accessed %f", returned.HotScore, accessed.HotScore)
	}

	used := callUsageSignal(t, service, "MarkUsed", memory.MemoryID)
	if used.UsedCount != 1 {
		t.Fatalf("used count after MarkUsed = %d, want 1", used.UsedCount)
	}
	if got := readTimeField(t, used, "LastUsedAt"); got.IsZero() {
		t.Fatal("LastUsedAt should be set after MarkUsed")
	}
	if used.HotScore <= returned.HotScore {
		t.Fatalf("used hot score = %f, want > returned %f", used.HotScore, returned.HotScore)
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
	filter, err := BuildFilter(FilterContext{UserID: "user_1", Scope: ScopeAgentSpecific, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}})
	if err != nil {
		t.Fatalf("BuildFilter() missing agent_id error = %v, want nil", err)
	}
	if _, ok := filter.Must["agent_id"]; ok {
		t.Fatalf("BuildFilter() filter includes agent_id = %#v, want source metadata only", filter.Must["agent_id"])
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

func callUsageSignal(t *testing.T, service any, method string, memoryID string) Memory {
	t.Helper()
	switch method {
	case "MarkAccessed":
		marker, ok := service.(interface{ MarkAccessed(string) (Memory, error) })
		if !ok {
			t.Fatalf("service does not implement MarkAccessed")
		}
		memory, err := marker.MarkAccessed(memoryID)
		if err != nil {
			t.Fatalf("MarkAccessed() error = %v", err)
		}
		return memory
	case "MarkReturned":
		marker, ok := service.(interface{ MarkReturned(string) (Memory, error) })
		if !ok {
			t.Fatalf("service does not implement MarkReturned")
		}
		memory, err := marker.MarkReturned(memoryID)
		if err != nil {
			t.Fatalf("MarkReturned() error = %v", err)
		}
		return memory
	case "MarkUsed":
		marker, ok := service.(interface{ MarkUsed(string) (Memory, error) })
		if !ok {
			t.Fatalf("service does not implement MarkUsed")
		}
		memory, err := marker.MarkUsed(memoryID)
		if err != nil {
			t.Fatalf("MarkUsed() error = %v", err)
		}
		return memory
	default:
		t.Fatalf("unsupported method %s", method)
		return Memory{}
	}
}

func readIntField(t *testing.T, memory Memory, name string) int {
	t.Helper()
	field := reflect.ValueOf(memory).FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("memory missing field %s", name)
	}
	return int(field.Int())
}

func readTimeField(t *testing.T, memory Memory, name string) time.Time {
	t.Helper()
	field := reflect.ValueOf(memory).FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("memory missing field %s", name)
	}
	value, ok := field.Interface().(time.Time)
	if !ok {
		t.Fatalf("field %s is not time.Time", name)
	}
	return value
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

func (r *capturingRepository) IncrementUsageSignal(memoryID string, signal UsageSignal) (Memory, error) {
	return Memory{}, nil
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
