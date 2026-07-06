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

func (r *InMemoryMaintenanceRepository) UpdateStage(ctx context.Context, runID string, stage MaintenanceRunStage, totalCandidates int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return ErrMaintenanceNotFound
	}
	run.Stage = stage
	if totalCandidates > 0 {
		run.TotalCandidates = totalCandidates
	}
	run.UpdatedAt = time.Now().UTC()
	r.runs[runID] = run
	return nil
}

func (r *InMemoryMaintenanceRepository) MarkStaleRunningAsFailed(ctx context.Context, before time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for id, run := range r.runs {
		if run.Status == MaintenanceRunRunning && run.StartedAt.Before(before) {
			run.Status = MaintenanceRunFailed
			run.Stage = StageFailed
			run.LastError = "stale: exceeded timeout"
			now := time.Now().UTC()
			run.CompletedAt = &now
			run.UpdatedAt = now
			r.runs[id] = run
			count++
		}
	}
	return count, nil
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

// --- 测试:创建任务后立即返回,不等待 LLM ---

func TestMaintenanceStartRunReturnsImmediately(t *testing.T) {
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

	// StartRun 应立即返回
	run, err := service.StartRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.RunID == "" {
		t.Fatal("run_id should not be empty")
	}
	if run.Status != MaintenanceRunRunning {
		t.Fatalf("expected status running, got %s", run.Status)
	}
	if run.Stage != StageQueued {
		t.Fatalf("expected stage queued, got %s", run.Stage)
	}
	// 此时 cleaner 不应被调用
	if cleaner.called {
		t.Fatal("cleaner should not be called yet")
	}

	// 执行后台清洗
	service.ExecuteRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	}, run.RunID)

	// 验证最终结果
	final, err := maintRepo.GetRun(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if final.Status != MaintenanceRunDone {
		t.Fatalf("expected status done, got %s", final.Status)
	}
	if final.Processed != 2 {
		t.Fatalf("expected 2 processed, got %d", final.Processed)
	}
	if final.Discarded != 1 {
		t.Fatalf("expected 1 discarded, got %d", final.Discarded)
	}
	if final.Kept != 1 {
		t.Fatalf("expected 1 kept, got %d", final.Kept)
	}
	if !cleaner.called {
		t.Fatal("cleaner should be called")
	}
}

// --- 测试:已有 running 任务时返回已有任务 ---

func TestMaintenanceAlreadyRunningReturnsExisting(t *testing.T) {
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
		Stage:     StageCallingLLM,
	})

	// 第二次触发应返回已有任务,不报错
	run, err := service.StartRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.RunID != "existing-run" {
		t.Fatalf("expected existing run_id, got %s", run.RunID)
	}
}

// --- 测试:阶段能正确更新 ---

func TestMaintenanceStageUpdates(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{
		result: CleanResult{
			DiscardIDs: []string{"cand-1"},
			KeepIDs:    []string{},
			Summary:    "ok",
		},
	}
	service, maintRepo := newTestMaintenanceService(t, cleaner,
		Candidate{CandidateID: "cand-1", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending, RiskLevel: RiskLow},
	)

	run, _ := service.StartRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})

	service.ExecuteRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
	}, run.RunID)

	// 最终阶段应为 done
	final, _ := maintRepo.GetRun(context.Background(), run.RunID)
	if final.Stage != StageDone {
		t.Fatalf("expected stage done, got %s", final.Stage)
	}
	if final.TotalCandidates != 1 {
		t.Fatalf("expected total_candidates 1, got %d", final.TotalCandidates)
	}
}

// --- 测试:成功后 processed/discarded/kept/composed 正确落库 ---

func TestMaintenanceResultsPersisted(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{
		result: CleanResult{
			DiscardIDs: []string{"cand-noise"},
			KeepIDs:    []string{"cand-valuable"},
			Summary:    "保留高价值",
		},
	}
	service, maintRepo := newTestMaintenanceService(t, cleaner,
		Candidate{CandidateID: "cand-noise", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending, RiskLevel: RiskLow},
		Candidate{CandidateID: "cand-valuable", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending, RiskLevel: RiskLow},
	)

	run, _ := service.StartRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})

	service.ExecuteRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
	}, run.RunID)

	final, _ := maintRepo.GetRun(context.Background(), run.RunID)
	if final.Processed != 2 {
		t.Fatalf("expected 2 processed, got %d", final.Processed)
	}
	if final.Discarded != 1 {
		t.Fatalf("expected 1 discarded, got %d", final.Discarded)
	}
	if final.Kept != 1 {
		t.Fatalf("expected 1 kept, got %d", final.Kept)
	}
}

// --- 测试:LLM 失败时任务 failed,候选状态零写入 ---

func TestMaintenanceLLMFailureZeroWrite(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{err: errors.New("llm timeout")}
	candidateRepo := NewInMemoryRepository()
	candidateRepo.CreateCandidate(context.Background(), Candidate{
		CandidateID: "cand-1", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending,
	})
	maintRepo := NewInMemoryMaintenanceRepository()
	service := NewMaintenanceService(maintRepo, candidateRepo, nil, cleaner)

	run, _ := service.StartRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})

	service.ExecuteRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
	}, run.RunID)

	// 验证零写入:候选状态不变
	cand, _ := candidateRepo.GetCandidate(context.Background(), "org-1", "cand-1")
	if cand.Status != StatusPending {
		t.Fatalf("candidate status should remain pending, got %s", cand.Status)
	}

	// 验证审计记录为 failed
	final, _ := maintRepo.GetRun(context.Background(), run.RunID)
	if final.Status != MaintenanceRunFailed {
		t.Fatalf("expected status failed, got %s", final.Status)
	}
	if final.LastError != "llm timeout" {
		t.Fatalf("expected last_error llm timeout, got %s", final.LastError)
	}
}

// --- 测试:LLM 返回不存在 candidate_id 时任务 failed ---

func TestMaintenanceInvalidCandidateIDFails(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{
		result: CleanResult{
			DiscardIDs: []string{"nonexistent-id"},
			KeepIDs:    []string{},
			Summary:    "测试不存在的 ID",
		},
	}
	service, maintRepo := newTestMaintenanceService(t, cleaner,
		Candidate{CandidateID: "cand-1", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending},
	)

	run, _ := service.StartRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})

	service.ExecuteRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
	}, run.RunID)

	final, _ := maintRepo.GetRun(context.Background(), run.RunID)
	if final.Status != MaintenanceRunFailed {
		t.Fatalf("expected status failed, got %s", final.Status)
	}
	if final.Stage != StageFailed {
		t.Fatalf("expected stage failed, got %s", final.Stage)
	}
}

// --- 测试:高风险候选不会被自动丢弃 ---

func TestMaintenanceHighRiskNotDiscarded(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{
		result: CleanResult{
			DiscardIDs: []string{"cand-high-risk"},
			KeepIDs:    []string{},
			Summary:    "测试高风险不丢弃",
		},
	}
	service, maintRepo := newTestMaintenanceService(t, cleaner,
		Candidate{CandidateID: "cand-high-risk", OrgID: "org-1", ProjectID: "proj-1", Status: StatusPending, RiskLevel: RiskHigh},
	)

	run, _ := service.StartRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Trigger:   MaintenanceTriggerManual,
	})

	service.ExecuteRun(context.Background(), MaintenanceRequest{
		OrgID:     "org-1",
		ProjectID: "proj-1",
	}, run.RunID)

	final, _ := maintRepo.GetRun(context.Background(), run.RunID)
	if final.Discarded != 0 {
		t.Fatalf("expected 0 discarded for high risk, got %d", final.Discarded)
	}
	if final.Kept != 1 {
		t.Fatalf("expected 1 kept for high risk, got %d", final.Kept)
	}
}

// --- 测试:status 接口支持 run_id 查询 ---

func TestMaintenanceGetRunWithRunID(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{result: CleanResult{Summary: "ok"}}
	service, maintRepo := newTestMaintenanceService(t, cleaner)

	maintRepo.CreateRun(context.Background(), MaintenanceRun{
		RunID:     "test-run-123",
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Status:    MaintenanceRunRunning,
		Stage:     StageCallingLLM,
	})

	run, err := service.GetRun(context.Background(), "test-run-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.RunID != "test-run-123" {
		t.Fatalf("expected run_id test-run-123, got %s", run.RunID)
	}
}

// --- 测试:status 接口支持不传 run_id 恢复当前项目 running 任务 ---

func TestMaintenanceGetActiveRun(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{result: CleanResult{Summary: "ok"}}
	service, maintRepo := newTestMaintenanceService(t, cleaner)

	maintRepo.CreateRun(context.Background(), MaintenanceRun{
		RunID:     "active-run",
		OrgID:     "org-1",
		ProjectID: "proj-1",
		Status:    MaintenanceRunRunning,
		Stage:     StageApplying,
	})

	run, err := service.GetActiveRun(context.Background(), "org-1", "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run == nil {
		t.Fatal("expected active run, got nil")
	}
	if run.RunID != "active-run" {
		t.Fatalf("expected run_id active-run, got %s", run.RunID)
	}
}

// --- 测试:没有任务时返回 nil ---

func TestMaintenanceGetActiveRunNone(t *testing.T) {
	cleaner := &fakeMaintenanceCleaner{result: CleanResult{Summary: "ok"}}
	service, _ := newTestMaintenanceService(t, cleaner)

	run, err := service.GetActiveRun(context.Background(), "org-1", "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run != nil {
		t.Fatalf("expected nil, got %v", run)
	}
}

// --- 测试:响应 DTO 数字字段永远存在且为数字 ---

func TestMaintenanceStatusDTODefaults(t *testing.T) {
	run := MaintenanceRun{
		RunID:  "test",
		Status: MaintenanceRunRunning,
		Stage:  StageQueued,
	}
	dto := run.ToStatusDTO()
	if dto.ProgressPercent != 5 {
		t.Fatalf("expected progress 5 for queued, got %d", dto.ProgressPercent)
	}
	if dto.TotalCandidates != 0 {
		t.Fatalf("expected total_candidates 0, got %d", dto.TotalCandidates)
	}
	if dto.Processed != 0 {
		t.Fatalf("expected processed 0, got %d", dto.Processed)
	}
	if dto.Discarded != 0 {
		t.Fatalf("expected discarded 0, got %d", dto.Discarded)
	}
	if dto.Kept != 0 {
		t.Fatalf("expected kept 0, got %d", dto.Kept)
	}
	if dto.Composed != 0 {
		t.Fatalf("expected composed 0, got %d", dto.Composed)
	}
}

func TestMaintenanceStageProgress(t *testing.T) {
	tests := []struct {
		stage MaintenanceRunStage
		want  int
	}{
		{StageQueued, 5},
		{StageLoadingCandidates, 10},
		{StageCallingLLM, 45},
		{StageValidating, 65},
		{StageApplying, 80},
		{StageComposing, 90},
		{StageDone, 100},
		{StageFailed, 0},
	}
	for _, tt := range tests {
		got := StageProgress(tt.stage)
		if got != tt.want {
			t.Errorf("StageProgress(%s) = %d, want %d", tt.stage, got, tt.want)
		}
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
