package candidatememory

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryTriageRepositoryScansOnlyUntriagedCandidates(t *testing.T) {
	ctx := context.Background()
	candidateRepo := NewInMemoryRepository()
	triageRepo := NewInMemoryTriageRepository(candidateRepo)

	if _, err := candidateRepo.CreateCandidate(ctx, Candidate{
		CandidateID: "cand-1",
		OrgID:       "org-1",
		ProjectID:   "proj-1",
		SourceKey:   "local/a",
		UserID:      "user-1",
		AgentID:     "codex",
		ThreadID:    "thread-1",
		Content:     "reuse this",
		MemoryType:  MemoryTypeFact,
		Confidence:  0.93,
		RiskLevel:   RiskLow,
		Status:      StatusPending,
		CreatedAt:   time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create candidate-1: %v", err)
	}
	if _, err := candidateRepo.CreateCandidate(ctx, Candidate{
		CandidateID: "cand-2",
		OrgID:       "org-1",
		ProjectID:   "proj-1",
		SourceKey:   "local/a",
		UserID:      "user-1",
		AgentID:     "codex",
		ThreadID:    "thread-1",
		Content:     "already triaged",
		MemoryType:  MemoryTypeFact,
		Confidence:  0.91,
		RiskLevel:   RiskLow,
		Status:      StatusPending,
		CreatedAt:   time.Date(2026, 7, 8, 10, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create candidate-2: %v", err)
	}
	if _, err := triageRepo.UpsertTriageResult(ctx, TriageResult{
		OrgID:           "org-1",
		CandidateID:     "cand-2",
		SourceProjectID: "proj-1",
		SourceKey:       "local/a",
		TriageScope:     TriageScopeInbox,
		Confidence:      0.8,
		ReviewState:     TriageReviewWeak,
	}); err != nil {
		t.Fatalf("upsert triage result: %v", err)
	}

	candidates, err := triageRepo.ListCandidatesNeedingTriage(ctx, TriageScanFilter{OrgID: "org-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListCandidatesNeedingTriage: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates len = %d, want 1", len(candidates))
	}
	if candidates[0].CandidateID != "cand-1" {
		t.Fatalf("candidate = %s, want cand-1", candidates[0].CandidateID)
	}
}

func TestInMemoryTriageRepositoryUpsertAndGetResultIsIdempotent(t *testing.T) {
	ctx := context.Background()
	candidateRepo := NewInMemoryRepository()
	triageRepo := NewInMemoryTriageRepository(candidateRepo)

	_, err := triageRepo.UpsertTriageResult(ctx, TriageResult{
		OrgID:                "org-1",
		CandidateID:          "cand-1",
		SourceProjectID:      "proj-1",
		SourceKey:            "local/a",
		TriageScope:          TriageScopeTooling,
		Confidence:           0.9,
		ReviewState:          TriageReviewWeak,
		Reason:               "first",
		PromotedHotMemoryIDs: []string{"hm-a"},
	})
	if err != nil {
		t.Fatalf("upsert result: %v", err)
	}
	_, err = triageRepo.UpsertTriageResult(ctx, TriageResult{
		OrgID:                "org-1",
		CandidateID:          "cand-1",
		SourceProjectID:      "proj-1",
		SourceKey:            "local/a",
		TriageScope:          TriageScopeTooling,
		Confidence:           0.95,
		ReviewState:          TriageReviewAutoApplied,
		Reason:               "second",
		PromotedHotMemoryIDs: []string{"hm-a", "hm-b"},
	})
	if err != nil {
		t.Fatalf("upsert result again: %v", err)
	}

	result, err := triageRepo.GetTriageResult(ctx, "org-1", "cand-1")
	if err != nil {
		t.Fatalf("GetTriageResult: %v", err)
	}
	if result.ReviewState != TriageReviewAutoApplied {
		t.Fatalf("review_state = %s, want %s", result.ReviewState, TriageReviewAutoApplied)
	}
	if len(result.PromotedHotMemoryIDs) != 2 {
		t.Fatalf("promoted ids len = %d, want 2", len(result.PromotedHotMemoryIDs))
	}
}

func TestInMemoryTriageRepositoryListTriageResultsFiltersByProject(t *testing.T) {
	ctx := context.Background()
	candidateRepo := NewInMemoryRepository()
	triageRepo := NewInMemoryTriageRepository(candidateRepo)

	_, err := triageRepo.UpsertTriageResult(ctx, TriageResult{
		OrgID:           "org-1",
		CandidateID:     "cand-1",
		SourceProjectID: "project-1",
		SourceKey:       "local/a",
		TriageScope:     TriageScopeTooling,
		Confidence:      0.9,
		ReviewState:     TriageReviewAutoApplied,
	})
	if err != nil {
		t.Fatalf("UpsertTriageResult: %v", err)
	}
	_, err = triageRepo.UpsertTriageResult(ctx, TriageResult{
		OrgID:           "org-1",
		CandidateID:     "cand-2",
		SourceProjectID: "project-2",
		SourceKey:       "local/b",
		TriageScope:     TriageScopeInbox,
		Confidence:      0.5,
		ReviewState:     TriageReviewWeak,
	})
	if err != nil {
		t.Fatalf("UpsertTriageResult 2: %v", err)
	}

	results, err := triageRepo.ListTriageResults(ctx, TriageListFilter{OrgID: "org-1", SourceProjectID: "project-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListTriageResults: %v", err)
	}
	if len(results) != 1 || results[0].CandidateID != "cand-1" {
		t.Fatalf("results = %#v, want cand-1", results)
	}
}

func TestInMemoryTriageRepositoryReplaceAndListProjectLinks(t *testing.T) {
	ctx := context.Background()
	candidateRepo := NewInMemoryRepository()
	triageRepo := NewInMemoryTriageRepository(candidateRepo)

	links := []CandidateProjectLink{{
		CandidateID:     "cand-1",
		LinkedProjectID: "project-2",
		LinkedSourceKey: "github.com/acme/api",
		Confidence:      0.91,
		Evidence:        "mentions acme api",
	}, {
		CandidateID:     "cand-1",
		LinkedProjectID: "project-1",
		LinkedSourceKey: "github.com/acme/ui",
		Confidence:      0.84,
		Evidence:        "mentions dashboard ui",
	}}
	if err := triageRepo.ReplaceProjectLinks(ctx, "org-1", "cand-1", links); err != nil {
		t.Fatalf("ReplaceProjectLinks: %v", err)
	}

	got, err := triageRepo.ListProjectLinks(ctx, CandidateProjectLinksFilter{OrgID: "org-1", MinConfidence: 0.9})
	if err != nil {
		t.Fatalf("ListProjectLinks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("project links len = %d, want 1", len(got))
	}
	if got[0].LinkedProjectID != "project-2" {
		t.Fatalf("linked project = %s, want project-2", got[0].LinkedProjectID)
	}
}

func TestInMemoryTriageRepositoryUpdateProjectLinkPromotion(t *testing.T) {
	ctx := context.Background()
	candidateRepo := NewInMemoryRepository()
	triageRepo := NewInMemoryTriageRepository(candidateRepo)

	err := triageRepo.ReplaceProjectLinks(ctx, "org-1", "cand-1", []CandidateProjectLink{{
		LinkedProjectID: "project-2",
		Confidence:      0.9,
	}})
	if err != nil {
		t.Fatalf("ReplaceProjectLinks: %v", err)
	}
	if err := triageRepo.UpdateProjectLinkPromotion(ctx, "org-1", "cand-1", "project-2", "hm-1"); err != nil {
		t.Fatalf("UpdateProjectLinkPromotion: %v", err)
	}
	links, err := triageRepo.ListProjectLinks(ctx, CandidateProjectLinksFilter{OrgID: "org-1", CandidateID: "cand-1"})
	if err != nil {
		t.Fatalf("ListProjectLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("links len = %d, want 1", len(links))
	}
	if links[0].PromotedHotMemoryID != "hm-1" {
		t.Fatalf("promoted_hot_memory_id = %q, want hm-1", links[0].PromotedHotMemoryID)
	}
}

func TestInMemoryTriageRepositoryPromotedHotMemoryIDsOnlyUpdatesKnownCandidate(t *testing.T) {
	ctx := context.Background()
	candidateRepo := NewInMemoryRepository()
	triageRepo := NewInMemoryTriageRepository(candidateRepo)

	if err := triageRepo.UpdatePromotedHotMemoryIDs(ctx, "org-1", "missing", []string{"hm-1"}); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
