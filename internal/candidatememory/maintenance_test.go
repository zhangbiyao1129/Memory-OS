package candidatememory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// InMemoryMaintenanceRepository 内存版 MaintenanceRepository。
type InMemoryMaintenanceRepository struct {
	mu   sync.Mutex
	runs map[string]MaintenanceRun // key: run_id
}

func NewInMemoryMaintenanceRepository() *InMemoryMaintenanceRepository {
	return &InMemoryMaintenanceRepository{runs: make(map[string]MaintenanceRun)}
}

func (r *InMemoryMaintenanceRepository) CreateRun(ctx context.Context, run MaintenanceRun) (MaintenanceRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runs[run.RunID]; exists {
		return MaintenanceRun{}, errors.New("run already exists")
	}
	now := time.Now().UTC()
	run.CreatedAt = now
	run.UpdatedAt = now
	r.runs[run.RunID] = run
	return run, nil
}

func (r *InMemoryMaintenanceRepository) GetRun(ctx context.Context, runID string) (MaintenanceRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return MaintenanceRun{}, ErrMaintenanceNotFound
	}
	return run, nil
}

func (r *InMemoryMaintenanceRepository) UpdateRun(ctx context.Context, runID string, status MaintenanceRunStatus, update MaintenanceRunUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return ErrMaintenanceNotFound
	}
	run.Status = status
	run.Processed = update.Processed
	run.Discarded = update.Discarded
	run.Kept = update.Kept
	run.Composed = update.Composed
	run.ArchiveID = update.ArchiveID
	run.Summary = update.Summary
	run.LastError = update.LastError
	run.CompletedAt = update.CompletedAt
	run.UpdatedAt = time.Now().UTC()
	r.runs[runID] = run
	return nil
}

func (r *InMemoryMaintenanceRepository) GetRunningRun(ctx context.Context, orgID, projectID string) (*MaintenanceRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, run := range r.runs {
		if run.OrgID == orgID && run.ProjectID == projectID && run.Status == MaintenanceRunRunning {
			return &run, nil
		}
	}
	return nil, nil
}

func (r *InMemoryMaintenanceRepository) ListRunningRuns(ctx context.Context, orgID, projectID string) ([]MaintenanceRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []MaintenanceRun
	for _, run := range r.runs {
		if run.OrgID == orgID && run.ProjectID == projectID && run.Status == MaintenanceRunRunning {
			out = append(out, run)
		}
	}
	return out, nil
}

// fakeMaintenanceCleaner 测试用 LLM 清洗器。
type fakeMaintenanceCleaner struct {
	result CleanResult
	err    error
}

func (f fakeMaintenanceCleaner) Clean(ctx context.Context, candidates []Candidate) (CleanResult, error) {
	if f.err != nil {
		return CleanResult{}, f.err
	}
	return f.result, nil
}

// trackingMaintenanceCleaner 追踪是否被调用。
type trackingMaintenanceCleaner struct {
	called bool
	result CleanResult
	err    error
}

func (t *trackingMaintenanceCleaner) Clean(ctx context.Context, candidates []Candidate) (CleanResult, error) {
	t.called = true
	if t.err != nil {
		return CleanResult{}, t.err
	}
	return t.result, nil
}

func newTestMaintenanceService(t *testing.T, cleaner MaintenanceCleaner, candidates ...Candidate) (*MaintenanceService, *InMemoryMaintenanceRepository) {
	t.Helper()
	maintRepo := NewInMemoryMaintenanceRepository()
	candidateRepo := NewInMemoryRepository()
	for _, c := range candidates {
		candidateRepo.CreateCandidate(context.Background(), c)
	}
	composer := NewTopicComposer(candidateRepo, nil) // nil ArchiveCreator
	return NewMaintenanceService(maintRepo, candidateRepo, &composer, cleaner), maintRepo
}

func TestMaintenanceServiceManualTrigger(t *testing.T) {
	cleaner := &trackingMaintenanceCleaner{
		result: CleanResult{
			DiscardIDs: []string{"cand-noise"},
			KeepIDs:    []string{"cand-valuable"},
			Summary:    "保留高价值候选",
		},
	}
	service, maintRepo := newTestMaintenanceService(t, cleaner,
		Candidate{CandidateID: "cand-noise", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending, RiskLevel: RiskLow},
		Candidate{CandidateID: "cand-valuable", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending, RiskLevel: RiskLow},
	)

	result, err := service.Run(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Processed != 2 {
		t.Fatalf("expected 2 processed, got %d", result.Processed)
	}
	if result.Discarded != 1 {
		t.Fatalf("expected 1 discarded, got %d", result.Discarded)
	}
	if result.Kept != 1 {
		t.Fatalf("expected 1 kept, got %d", result.Kept)
	}
	if !cleaner.called {
		t.Fatal("cleaner should be called")
	}

	// 验证审计记录
	run, err := maintRepo.GetRun(context.Background(), result.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != MaintenanceRunDone {
		t.Fatalf("expected status done, got %s", run.Status)
	}
	if run.TriggerType != MaintenanceTriggerManual {
		t.Fatalf("expected trigger manual, got %s", run.TriggerType)
	}
}

func TestMaintenanceServiceHighRiskNotDiscarded(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{
		result: CleanResult{
			DiscardIDs: []string{"cand-high-risk"}, // LLM 建议丢弃高风险
			KeepIDs:    []string{},
			Summary:    "测试高风险不丢弃",
		},
	}
	service, _ := newTestMaintenanceService(t, cleaner,
		Candidate{CandidateID: "cand-high-risk", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending, RiskLevel: RiskHigh},
	)

	result, err := service.Run(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 高风险候选不应被丢弃
	if result.Discarded != 0 {
		t.Fatalf("expected 0 discarded for high risk, got %d", result.Discarded)
	}
	if result.Kept != 1 {
		t.Fatalf("expected 1 kept for high risk, got %d", result.Kept)
	}
}

func TestMaintenanceServiceLLMFailureZeroWrite(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{
		err: errors.New("llm timeout"),
	}
	candidateRepo := NewInMemoryRepository()
	candidateRepo.CreateCandidate(context.Background(), Candidate{
		CandidateID: "cand-1", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending,
	})
	maintRepo := NewInMemoryMaintenanceRepository()
	service := NewMaintenanceService(maintRepo, candidateRepo, nil, cleaner)

	_, err := service.Run(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})
	if err == nil {
		t.Fatal("expected error on LLM failure")
	}

	// 验证零写入:候选状态不变
	cand, _ := candidateRepo.GetCandidate(context.Background(), "org-1", "cand-1")
	if cand.Status != StatusPending {
		t.Fatalf("candidate status should remain pending, got %s", cand.Status)
	}

	// 验证审计记录为 failed
	runs, _ := maintRepo.ListRunningRuns(context.Background(), "org-1", "proj-1")
	if len(runs) != 0 {
		t.Fatal("no running runs should remain")
	}
}

func TestMaintenanceServiceAlreadyRunning(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{result: CleanResult{Summary: "ok"}}
	service, maintRepo := newTestMaintenanceService(t, cleaner,
		Candidate{CandidateID: "cand-1", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending},
	)

	// 创建一个运行中的任务
	maintRepo.CreateRun(context.Background(), MaintenanceRun{
		RunID:     "existing-run",
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Status:    MaintenanceRunRunning,
	})

	// 第二次触发应失败
	_, err := service.Run(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})
	if !errors.Is(err, ErrMaintenanceAlreadyRunning) {
		t.Fatalf("expected ErrMaintenanceAlreadyRunning, got %v", err)
	}
}

func TestMaintenanceServiceNoCandidatesToClean(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{result: CleanResult{Summary: "ok"}}
	service, _ := newTestMaintenanceService(t, cleaner) // 无候选

	_, err := service.Run(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})
	if !errors.Is(err, ErrNoCandidatesToClean) {
		t.Fatalf("expected ErrNoCandidatesToClean, got %v", err)
	}
}

func TestMaintenanceServiceInvalidCandidateID(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{
		result: CleanResult{
			DiscardIDs: []string{"nonexistent-id"},
			KeepIDs:    []string{},
			Summary:    "测试不存在的 ID",
		},
	}
	service, _ := newTestMaintenanceService(t, cleaner,
		Candidate{CandidateID: "cand-1", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending},
	)

	_, err := service.Run(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent candidate ID")
	}
}

func TestShouldAutoClean(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{result: CleanResult{Summary: "ok"}}
	service, _ := newTestMaintenanceService(t, cleaner)

	// 不足 100 个候选
	if service.ShouldAutoClean(context.Background(), "org-1", "proj-1") {
		t.Fatal("should not auto clean with less than 100 candidates")
	}

	// 创建 100 个候选
	for i := 0; i < 100; i++ {
		service.candidateRepo.CreateCandidate(context.Background(), Candidate{
			CandidateID: "cand-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			OrgID:       "org-1",
			ProjectID:   "proj-1",
			Status:      StatusPending,
			CreatedAt:   time.Now().UTC().Add(-10 * time.Minute), // 10分钟前
		})
	}

	// 满足条件
	if !service.ShouldAutoClean(context.Background(), "org-1", "proj-1") {
		t.Fatal("should auto clean with 100+ candidates and 5min idle")
	}
}
