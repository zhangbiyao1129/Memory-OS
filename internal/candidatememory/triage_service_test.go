package candidatememory

import (
	"context"
	"errors"
	"testing"

	"memory-os/internal/hotmemory"
)

type fakeProjectCatalog struct {
	projects []TriageProject
	err      error
}

func (f fakeProjectCatalog) ListProjectsForTriage(userID, orgID string) ([]TriageProject, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.projects, nil
}

type fakeTriageClassifier struct {
	decision TriageDecision
	err      error
}

func (f fakeTriageClassifier) Classify(ctx context.Context, input TriageInput) (TriageDecision, error) {
	if f.err != nil {
		return TriageDecision{}, f.err
	}
	return f.decision, nil
}

type fakeHotMemorySink struct {
	requests []hotmemory.UpsertRequest
	err      error
}

func (f *fakeHotMemorySink) Upsert(request hotmemory.UpsertRequest) (hotmemory.Memory, error) {
	if f.err != nil {
		return hotmemory.Memory{}, f.err
	}
	f.requests = append(f.requests, request)
	return hotmemory.Memory{MemoryID: "hm_test_" + request.ProjectID}, nil
}

func TestTriageServicePromotesGlobalToolingToUserHotMemory(t *testing.T) {
	ctx := context.Background()
	candidateRepo := NewInMemoryRepository()
	triageRepo := NewInMemoryTriageRepository(candidateRepo)
	if _, err := candidateRepo.CreateCandidate(ctx, Candidate{
		CandidateID:    "cand-1",
		OrgID:          "org-1",
		ProjectID:      "project-local",
		SourceKey:      "local/users/kanyun/tmp",
		UserID:         "user-1",
		AgentID:        "codex",
		ThreadID:       "thread-1",
		SourceEventIDs: []string{"event-1"},
		Content:        "Codex hook writes Memory OS turn events",
		MemoryType:     MemoryTypeFact,
		RiskLevel:      RiskLow,
		Confidence:     0.95,
		Status:         StatusPending,
	}); err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	hotSink := &fakeHotMemorySink{}
	service := NewTriageService(TriageServiceOptions{
		Repo:       triageRepo,
		Classifier: fakeTriageClassifier{decision: TriageDecision{Scope: TriageScopeTooling, Confidence: 0.9, Reason: "reusable tooling"}},
		HotMemory:  hotSink,
	})

	result, err := service.RunAutoTriage(ctx, TriageScanFilter{OrgID: "org-1", Limit: 10})
	if err != nil {
		t.Fatalf("RunAutoTriage: %v", err)
	}
	if result.Processed != 1 || result.Promoted != 1 || result.Triaged != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(hotSink.requests) != 1 {
		t.Fatalf("hot memory requests = %#v", hotSink.requests)
	}
	req := hotSink.requests[0]
	if req.ProjectID != GlobalHotMemoryProjectID || req.Scope != hotmemory.ScopeUser || req.Visibility != "private" {
		t.Fatalf("global hot memory request = %#v", req)
	}
	if req.PermissionLabels == nil || len(req.PermissionLabels) != 0 {
		t.Fatalf("global hot memory permission labels = %#v, want empty non-nil slice", req.PermissionLabels)
	}
	stored, err := triageRepo.GetTriageResult(ctx, "org-1", "cand-1")
	if err != nil {
		t.Fatalf("GetTriageResult: %v", err)
	}
	if stored.ReviewState != TriageReviewAutoApplied || len(stored.PromotedHotMemoryIDs) != 1 {
		t.Fatalf("stored triage = %#v", stored)
	}
}

func TestTriageServicePromotesLinkedProjectHotMemory(t *testing.T) {
	ctx := context.Background()
	candidateRepo := NewInMemoryRepository()
	triageRepo := NewInMemoryTriageRepository(candidateRepo)
	if _, err := candidateRepo.CreateCandidate(ctx, Candidate{
		CandidateID:    "cand-2",
		OrgID:          "org-1",
		ProjectID:      "project-local",
		SourceKey:      "local/users/kanyun/tmp",
		UserID:         "user-1",
		AgentID:        "codex",
		ThreadID:       "thread-1",
		SourceEventIDs: []string{"event-2"},
		Content:        "Memory OS non git hook fallback uses local path",
		MemoryType:     MemoryTypeFact,
		RiskLevel:      RiskLow,
		Confidence:     0.92,
		Status:         StatusPending,
	}); err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	hotSink := &fakeHotMemorySink{}
	service := NewTriageService(TriageServiceOptions{
		Repo: triageRepo,
		Classifier: fakeTriageClassifier{decision: TriageDecision{
			Scope:      TriageScopeProject,
			Confidence: 0.88,
			Reason:     "Memory OS project",
			ProjectLinks: []CandidateProjectLink{{
				LinkedProjectID: "project-memory-os",
				LinkedSourceKey: "github.com/acme/memory-os",
				Confidence:      0.86,
				Evidence:        "mentions Memory OS",
			}},
		}},
		HotMemory: hotSink,
	})

	result, err := service.RunAutoTriage(ctx, TriageScanFilter{OrgID: "org-1", Limit: 10})
	if err != nil {
		t.Fatalf("RunAutoTriage: %v", err)
	}
	if result.ProjectLinks != 1 || result.Promoted != 1 {
		t.Fatalf("result = %#v", result)
	}
	req := hotSink.requests[0]
	if req.ProjectID != "project-memory-os" || req.Scope != hotmemory.ScopeProject || req.Visibility != "project" {
		t.Fatalf("linked hot memory request = %#v", req)
	}
	if len(req.PermissionLabels) != 1 || req.PermissionLabels[0] != "project:project-memory-os:read" {
		t.Fatalf("permission labels = %#v", req.PermissionLabels)
	}
	links, err := triageRepo.ListProjectLinks(ctx, CandidateProjectLinksFilter{OrgID: "org-1", CandidateID: "cand-2"})
	if err != nil {
		t.Fatalf("ListProjectLinks: %v", err)
	}
	if len(links) != 1 || links[0].PromotedHotMemoryID == "" {
		t.Fatalf("links = %#v", links)
	}
}

func TestTriageServiceDoesNotPromoteHighRiskCandidate(t *testing.T) {
	ctx := context.Background()
	candidateRepo := NewInMemoryRepository()
	triageRepo := NewInMemoryTriageRepository(candidateRepo)
	if _, err := candidateRepo.CreateCandidate(ctx, Candidate{
		CandidateID: "cand-high",
		OrgID:       "org-1",
		ProjectID:   "project-1",
		UserID:      "user-1",
		AgentID:     "codex",
		Content:     "sensitive operational detail",
		RiskLevel:   RiskHigh,
		Status:      StatusPending,
	}); err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	hotSink := &fakeHotMemorySink{}
	service := NewTriageService(TriageServiceOptions{
		Repo:       triageRepo,
		Classifier: fakeTriageClassifier{decision: TriageDecision{Scope: TriageScopeGlobal, Confidence: 0.95, Reason: "global"}},
		HotMemory:  hotSink,
	})

	result, err := service.RunAutoTriage(ctx, TriageScanFilter{OrgID: "org-1", Limit: 10})
	if err != nil {
		t.Fatalf("RunAutoTriage: %v", err)
	}
	if result.Promoted != 0 || len(hotSink.requests) != 0 {
		t.Fatalf("high risk should not promote, result=%#v requests=%#v", result, hotSink.requests)
	}
	stored, err := triageRepo.GetTriageResult(ctx, "org-1", "cand-high")
	if err != nil {
		t.Fatalf("GetTriageResult: %v", err)
	}
	if stored.ReviewState != TriageReviewNeedsReview {
		t.Fatalf("review_state = %s, want needs_review", stored.ReviewState)
	}
}

func TestTriageServiceFallsBackWhenLLMClassifierFails(t *testing.T) {
	ctx := context.Background()
	candidateRepo := NewInMemoryRepository()
	triageRepo := NewInMemoryTriageRepository(candidateRepo)
	if _, err := candidateRepo.CreateCandidate(ctx, Candidate{
		CandidateID: "cand-fallback",
		OrgID:       "org-1",
		ProjectID:   "project-1",
		SourceKey:   "local/tmp",
		UserID:      "user-1",
		AgentID:     "codex",
		Content:     "Codex MCP 配置",
		RiskLevel:   RiskLow,
		MemoryType:  MemoryTypeFact,
		Status:      StatusPending,
	}); err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	service := NewTriageService(TriageServiceOptions{
		Repo:       triageRepo,
		Classifier: fakeTriageClassifier{err: errors.New("llm unavailable")},
		Fallback:   RuleTriageClassifier{},
	})

	result, err := service.RunAutoTriage(ctx, TriageScanFilter{OrgID: "org-1", Limit: 10})
	if err != nil {
		t.Fatalf("RunAutoTriage: %v", err)
	}
	if result.Triaged != 1 || result.Failed != 0 {
		t.Fatalf("result = %#v", result)
	}
	stored, err := triageRepo.GetTriageResult(ctx, "org-1", "cand-fallback")
	if err != nil {
		t.Fatalf("GetTriageResult: %v", err)
	}
	if stored.TriageScope != TriageScopeTooling || stored.ReviewState != TriageReviewWeak {
		t.Fatalf("stored = %#v", stored)
	}
}
