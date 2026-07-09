package memorykernel

import (
	"context"
	"testing"
)

// --- fakes for service tests ---

type fakeCandidateGovernanceRepo struct {
	items      []CandidateInput
	statusByID map[string]string
}

func newFakeCandidateGovernanceRepo(items []CandidateInput) *fakeCandidateGovernanceRepo {
	return &fakeCandidateGovernanceRepo{items: items, statusByID: map[string]string{}}
}

func (f *fakeCandidateGovernanceRepo) ListKernelCandidates(_ context.Context, _ Scope, _ int) ([]CandidateInput, error) {
	return f.items, nil
}

func (f *fakeCandidateGovernanceRepo) UpdateCandidateGovernance(_ context.Context, _, candidateID string, status interface{}, _ bool, _, _ string) (interface{}, error) {
	f.statusByID[candidateID] = status.(string)
	return nil, nil
}

type fakeClassifier struct {
	result ClassifyResult
	err    error
}

func (f fakeClassifier) Classify(_ context.Context, _ ClassifyInput) (ClassifyResult, error) {
	return f.result, f.err
}

type fakeCollector struct {
	candidates []CandidateInput
}

func (f fakeCollector) Collect(_ context.Context, scope Scope) (ClassifyInput, error) {
	return ClassifyInput{
		Scope:      scope,
		Candidates: f.candidates,
	}, nil
}

type fakeHotMemoryApplier struct {
	memories map[string]HotMemoryGetResult
	updated  map[string]string
}

func newFakeHotMemoryApplier() *fakeHotMemoryApplier {
	return &fakeHotMemoryApplier{
		memories: map[string]HotMemoryGetResult{},
		updated:  map[string]string{},
	}
}

func (f *fakeHotMemoryApplier) Get(memoryID string) (HotMemoryGetResult, error) {
	if m, ok := f.memories[memoryID]; ok {
		return m, nil
	}
	return HotMemoryGetResult{MemoryID: memoryID, Status: "active"}, nil
}

func (f *fakeHotMemoryApplier) Update(req HotMemoryUpdateRequest) (HotMemoryUpdateResult, error) {
	f.updated[req.MemoryID] = req.Status
	return HotMemoryUpdateResult{MemoryID: req.MemoryID, Status: req.Status}, nil
}

type fakeCorrectionArchiveCreator struct {
	createdTitle string
	archiveID    string
}

func (f *fakeCorrectionArchiveCreator) CreateCorrectionArchive(_ context.Context, req CorrectionArchiveRequest) (CorrectionArchiveResult, error) {
	f.createdTitle = "记忆修订: Memory Kernel 当前可信结论"
	f.archiveID = "archive_correction_1"
	return CorrectionArchiveResult{ArchiveID: f.archiveID, Title: f.createdTitle}, nil
}

type fakeCIRunner struct {
	lastCaseID string
	result     CIResult
}

func (f *fakeCIRunner) RunCase(_ context.Context, caseID string) (CIResult, error) {
	f.lastCaseID = caseID
	return f.result, nil
}

// --- tests ---

func TestServiceRunGovernanceSupersedesStaleCandidateAndCreatesCurrentUnit(t *testing.T) {
	repo := NewInMemoryRepository()
	candidates := newFakeCandidateGovernanceRepo([]CandidateInput{
		{ID: "cand_old", Content: "memory_archive 尚未实现", RiskLevel: "low"},
		{ID: "cand_new", Content: "memory_archive 已实现并部署", RiskLevel: "low"},
	})
	classifier := fakeClassifier{result: ClassifyResult{
		Units: []MemoryUnit{{UnitID: "unit_memory_archive_current", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Type: UnitFact, Content: "memory_archive 已实现并部署", Status: UnitCurrent, TrustScore: 0.95}},
		Claims: []MemoryClaim{{ClaimID: "claim_memory_archive_current", UnitID: "unit_memory_archive_current", OrgID: "org_1", ProjectID: "project_1", Subject: "memory_archive", Predicate: "implementation_status", Value: "implemented_and_deployed", Confidence: 0.95}},
		Actions: []GovernanceAction{{ActionID: "act_discard_old", OrgID: "org_1", ProjectID: "project_1", TargetType: "candidate", TargetID: "cand_old", Action: ActionDiscardStale, Reason: "旧事实已被 cand_new 覆盖"}},
		CICases: []CICase{{CaseID: "ci_memory_archive_status", OrgID: "org_1", ProjectID: "project_1", Question: "memory_archive 现在实现了吗？", MustInclude: []string{"已实现", "已部署"}, MustNotInclude: []string{"尚未实现"}}},
		Summary: "生成当前事实并丢弃旧候选",
	}}
	service := NewService(ServiceOptions{Repository: repo, Collector: fakeCollector{candidates: candidates.items}, Classifier: classifier, CandidateApplier: candidates})
	run, err := service.RunGovernance(context.Background(), GovernanceRequest{OrgID: "org_1", ProjectID: "project_1", TriggerType: "manual"})
	if err != nil {
		t.Fatalf("RunGovernance() error = %v", err)
	}
	if run.CreatedUnits != 1 || run.StaleCandidates != 1 || run.CICasesCreated != 1 {
		t.Fatalf("run = %#v", run)
	}
	if candidates.statusByID["cand_old"] != "discarded" {
		t.Fatalf("cand_old status = %q", candidates.statusByID["cand_old"])
	}
}

func TestServiceDemotesHotMemory(t *testing.T) {
	repo := NewInMemoryRepository()
	hm := newFakeHotMemoryApplier()
	hm.memories["hm_old"] = HotMemoryGetResult{MemoryID: "hm_old", Pinned: false, Status: "active"}
	classifier := fakeClassifier{result: ClassifyResult{
		Units:   []MemoryUnit{{UnitID: "unit_1", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Type: UnitFact, Content: "新事实", Status: UnitCurrent}},
		Actions: []GovernanceAction{{ActionID: "act_demote", TargetType: "hot_memory", TargetID: "hm_old", Action: ActionDemoteHotMemory, Reason: "低信号热记忆"}},
		Summary: "降级热记忆",
	}}
	service := NewService(ServiceOptions{Repository: repo, Collector: fakeCollector{}, Classifier: classifier, HotMemoryApplier: hm})
	run, err := service.RunGovernance(context.Background(), GovernanceRequest{OrgID: "org_1", ProjectID: "project_1", TriggerType: "manual"})
	if err != nil {
		t.Fatalf("RunGovernance() error = %v", err)
	}
	if run.DemotedHotMemories != 1 {
		t.Fatalf("demoted = %d, want 1", run.DemotedHotMemories)
	}
	if hm.updated["hm_old"] != "demoted" {
		t.Fatalf("hm_old status = %q", hm.updated["hm_old"])
	}
}

func TestServiceSkipsDemoteForPinnedHotMemory(t *testing.T) {
	repo := NewInMemoryRepository()
	hm := newFakeHotMemoryApplier()
	hm.memories["hm_pinned"] = HotMemoryGetResult{MemoryID: "hm_pinned", Pinned: true, Status: "active"}
	classifier := fakeClassifier{result: ClassifyResult{
		Actions: []GovernanceAction{{ActionID: "act_demote_pinned", TargetType: "hot_memory", TargetID: "hm_pinned", Action: ActionDemoteHotMemory, Reason: "pinned"}},
		Summary: "尝试降级 pinned",
	}}
	service := NewService(ServiceOptions{Repository: repo, Collector: fakeCollector{}, Classifier: classifier, HotMemoryApplier: hm})
	run, err := service.RunGovernance(context.Background(), GovernanceRequest{OrgID: "org_1", ProjectID: "project_1", TriggerType: "manual"})
	if err != nil {
		t.Fatalf("RunGovernance() error = %v", err)
	}
	if run.DemotedHotMemories != 0 {
		t.Fatalf("demoted = %d, want 0 for pinned", run.DemotedHotMemories)
	}
	if _, ok := hm.updated["hm_pinned"]; ok {
		t.Fatal("pinned hot memory should not be updated")
	}
}

func TestServiceCreatesCorrectionArchiveWithoutEditingOldArchive(t *testing.T) {
	creator := &fakeCorrectionArchiveCreator{}
	service := NewService(ServiceOptions{
		Repository: NewInMemoryRepository(),
		Collector:  fakeCollector{},
		Classifier: fakeClassifier{result: ClassifyResult{
			Actions: []GovernanceAction{{ActionID: "act_correction", TargetType: "archive", TargetID: "archive_old", Action: ActionCreateCorrection, Reason: "旧归档包含过期结论"}},
			Units:   []MemoryUnit{{UnitID: "unit_current", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Type: UnitFact, Content: "memory_archive 已实现并部署", Status: UnitCurrent}},
			Summary: "创建修订归档",
		}},
		ArchiveCreator: creator,
	})
	run, err := service.RunGovernance(context.Background(), GovernanceRequest{OrgID: "org_1", ProjectID: "project_1", TriggerType: "manual"})
	if err != nil {
		t.Fatalf("RunGovernance() error = %v", err)
	}
	if creator.createdTitle != "记忆修订: Memory Kernel 当前可信结论" {
		t.Fatalf("title = %q", creator.createdTitle)
	}
	_ = run
}

func TestServiceRecordsActionsInRepository(t *testing.T) {
	repo := NewInMemoryRepository()
	classifier := fakeClassifier{result: ClassifyResult{
		Actions: []GovernanceAction{{ActionID: "act_1", TargetType: "candidate", TargetID: "cand_1", Action: ActionDiscardStale, Reason: "过期"}},
		Summary: "记录 action",
	}}
	service := NewService(ServiceOptions{Repository: repo, Collector: fakeCollector{}, Classifier: classifier})
	_, err := service.RunGovernance(context.Background(), GovernanceRequest{OrgID: "org_1", ProjectID: "project_1", TriggerType: "manual"})
	if err != nil {
		t.Fatalf("RunGovernance() error = %v", err)
	}
	actions, _ := repo.ListActions(context.Background(), ActionFilter{OrgID: "org_1"})
	if len(actions) != 1 || actions[0].ActionID != "act_1" {
		t.Fatalf("actions = %#v", actions)
	}
}

func TestServiceRunGovernanceFailOnClassifyError(t *testing.T) {
	repo := NewInMemoryRepository()
	service := NewService(ServiceOptions{
		Repository: repo,
		Collector:  fakeCollector{},
		Classifier: fakeClassifier{err: ErrUnitNotFound},
	})
	_, err := service.RunGovernance(context.Background(), GovernanceRequest{OrgID: "org_1", ProjectID: "project_1", TriggerType: "manual"})
	if err == nil {
		t.Fatal("expected error on classify failure")
	}
	runs, _ := repo.ListRuns(context.Background(), RunFilter{OrgID: "org_1"})
	if len(runs) != 1 || runs[0].Status != RunFailed {
		t.Fatalf("run should be failed: %#v", runs)
	}
}
