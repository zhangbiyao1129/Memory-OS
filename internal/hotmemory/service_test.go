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
