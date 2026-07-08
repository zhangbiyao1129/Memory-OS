package candidatememory

import (
	"context"
	"testing"
)

func TestPGTriageRepositoryScansAndUpserts(t *testing.T) {
	pool := candidatePGTestPool(t)
	ctx := context.Background()
	suffix := candidateSuffix()
	orgID := "org-triage-" + suffix
	projectID := "project-triage-" + suffix
	candidateID := "cand-triage-" + suffix

	candidateRepo := NewPGRepository(pool)
	triageRepo := NewPGTriageRepository(pool)

	if _, err := candidateRepo.CreateCandidate(ctx, Candidate{
		CandidateID: candidateID,
		OrgID:       orgID,
		ProjectID:   projectID,
		SourceKey:   "local/triage/" + suffix,
		UserID:      "user-" + suffix,
		AgentID:     "codex",
		ThreadID:    "thread-" + suffix,
		MemoryType:  MemoryTypeFact,
		Content:     "Codex hook setup should be reusable",
		RiskLevel:   RiskLow,
		Confidence:  0.91,
		Status:      StatusPending,
	}); err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}

	candidates, err := triageRepo.ListCandidatesNeedingTriage(ctx, TriageScanFilter{OrgID: orgID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCandidatesNeedingTriage: %v", err)
	}
	if len(candidates) != 1 || candidates[0].CandidateID != candidateID {
		t.Fatalf("pending = %#v, want one %s", candidates, candidateID)
	}

	_, err = triageRepo.UpsertTriageResult(ctx, TriageResult{
		OrgID:                orgID,
		CandidateID:          candidateID,
		SourceProjectID:      projectID,
		SourceKey:            "local/triage/" + suffix,
		TriageScope:          TriageScopeTooling,
		Confidence:           0.92,
		ReviewState:          TriageReviewAutoApplied,
		Reason:               "tooling setup",
		SourceRefs:           []TriageSourceRef{{Kind: "candidate", ID: candidateID}},
		PromotedHotMemoryIDs: []string{"hm-1"},
	})
	if err != nil {
		t.Fatalf("UpsertTriageResult: %v", err)
	}

	pending, err := triageRepo.ListCandidatesNeedingTriage(ctx, TriageScanFilter{OrgID: orgID, Limit: 10})
	if err != nil {
		t.Fatalf("ListCandidatesNeedingTriage after triage: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after triage = %d, want 0", len(pending))
	}

	result, err := triageRepo.GetTriageResult(ctx, orgID, candidateID)
	if err != nil {
		t.Fatalf("GetTriageResult: %v", err)
	}
	if result.ReviewState != TriageReviewAutoApplied {
		t.Fatalf("review_state = %s, want %s", result.ReviewState, TriageReviewAutoApplied)
	}

	if err := triageRepo.ReplaceProjectLinks(ctx, orgID, candidateID, []CandidateProjectLink{{
		CandidateID:     candidateID,
		LinkedProjectID: "project-1",
		LinkedSourceKey: "github.com/acme/api",
		Confidence:      0.85,
		Evidence:        "mentions api",
		Status:          "active",
	}, {
		CandidateID:     candidateID,
		LinkedProjectID: "project-2",
		LinkedSourceKey: "github.com/acme/ui",
		Confidence:      0.91,
		Evidence:        "mentions ui",
		Status:          "active",
	}}); err != nil {
		t.Fatalf("ReplaceProjectLinks: %v", err)
	}

	links, err := triageRepo.ListProjectLinks(ctx, CandidateProjectLinksFilter{OrgID: orgID, CandidateID: candidateID, MinConfidence: 0.9})
	if err != nil {
		t.Fatalf("ListProjectLinks: %v", err)
	}
	if len(links) != 1 || links[0].LinkedProjectID != "project-2" {
		t.Fatalf("links = %#v, want only project-2", links)
	}

	if err := triageRepo.UpdateProjectLinkPromotion(ctx, orgID, candidateID, "project-2", "hm-9"); err != nil {
		t.Fatalf("UpdateProjectLinkPromotion: %v", err)
	}
	updated, err := triageRepo.ListProjectLinks(ctx, CandidateProjectLinksFilter{OrgID: orgID, CandidateID: candidateID})
	if err != nil {
		t.Fatalf("ListProjectLinks after promotion: %v", err)
	}
	if len(updated) != 2 {
		t.Fatalf("updated links len = %d, want 2", len(updated))
	}
	for _, link := range updated {
		if link.LinkedProjectID == "project-2" && link.PromotedHotMemoryID != "hm-9" {
			t.Fatalf("promoted memory id = %q, want hm-9", link.PromotedHotMemoryID)
		}
	}
}

func TestPGTriageRepositoryErrorsIfNotConfigured(t *testing.T) {
	var repo *PGTriageRepository
	if _, err := repo.ListCandidatesNeedingTriage(context.Background(), TriageScanFilter{OrgID: "org-1"}); err == nil {
		t.Fatal("ListCandidatesNeedingTriage should return error when repo is not configured")
	}
	if _, err := repo.UpsertTriageResult(context.Background(), TriageResult{OrgID: "org-1", CandidateID: "cand-1", TriageScope: TriageScopeInbox}); err == nil {
		t.Fatal("UpsertTriageResult should return error when repo is not configured")
	}
	if err := repo.UpdatePromotedHotMemoryIDs(context.Background(), "org-1", "cand-1", nil); err == nil {
		t.Fatal("UpdatePromotedHotMemoryIDs should return error when repo is not configured")
	}
}
