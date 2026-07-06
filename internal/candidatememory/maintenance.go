package candidatememory

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// MaintenanceTriggerType 清洗整合触发类型。
type MaintenanceTriggerType string

const (
	MaintenanceTriggerManual MaintenanceTriggerType = "manual"
	MaintenanceTriggerAuto   MaintenanceTriggerType = "auto"
)

// MaintenanceRunStatus 清洗整合任务状态。
type MaintenanceRunStatus string

const (
	MaintenanceRunRunning MaintenanceRunStatus = "running"
	MaintenanceRunDone    MaintenanceRunStatus = "done"
	MaintenanceRunFailed  MaintenanceRunStatus = "failed"
)

// MaintenanceRunStage 清洗整合任务阶段。
type MaintenanceRunStage string

const (
	StageQueued            MaintenanceRunStage = "queued"
	StageLoadingCandidates MaintenanceRunStage = "loading_candidates"
	StageCallingLLM        MaintenanceRunStage = "calling_llm"
	StageValidating        MaintenanceRunStage = "validating"
	StageApplying          MaintenanceRunStage = "applying"
	StageComposing         MaintenanceRunStage = "composing"
	StageDone              MaintenanceRunStage = "done"
	StageFailed            MaintenanceRunStage = "failed"
)

// MaintenanceRun 一次清洗整合操作的审计记录。
type MaintenanceRun struct {
	ID              int64
	RunID           string
	OrgID           string
	ProjectID       string
	SourceKey       string
	ThreadID        string
	TriggerType     MaintenanceTriggerType
	Status          MaintenanceRunStatus
	Stage           MaintenanceRunStage
	TotalCandidates int
	Processed       int
	Discarded       int
	Kept            int
	Composed        int
	ArchiveID       string
	Summary         string
	LastError       string
	LockedBy        string
	StartedAt       time.Time
	CompletedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// MaintenanceRequest 清洗整合请求。
type MaintenanceRequest struct {
	OrgID     string
	ProjectID string
	SourceKey string
	ThreadID  string
	Trigger   MaintenanceTriggerType
}

// MaintenanceResult 清洗整合结果。
type MaintenanceResult struct {
	RunID     string
	Processed int
	Discarded int
	Kept      int
	Composed  int
	ArchiveID string
}

// MaintenanceRepository 清洗整合持久化接口。
type MaintenanceRepository interface {
	CreateRun(ctx context.Context, run MaintenanceRun) (MaintenanceRun, error)
	GetRun(ctx context.Context, runID string) (MaintenanceRun, error)
	UpdateRun(ctx context.Context, runID string, status MaintenanceRunStatus, result MaintenanceRunUpdate) error
	GetRunningRun(ctx context.Context, orgID, projectID string) (*MaintenanceRun, error)
	UpdateStage(ctx context.Context, runID string, stage MaintenanceRunStage, totalCandidates int) error
	MarkStaleRunningAsFailed(ctx context.Context, before time.Time) (int, error)
}

// MaintenanceRunUpdate 清洗整合更新字段。
type MaintenanceRunUpdate struct {
	Processed   int
	Discarded   int
	Kept        int
	Composed    int
	ArchiveID   string
	Summary     string
	LastError   string
	CompletedAt *time.Time
}

// StageProgress 返回阶段对应的进度百分比。
func StageProgress(stage MaintenanceRunStage) int {
	switch stage {
	case StageQueued:
		return 5
	case StageLoadingCandidates:
		return 10
	case StageCallingLLM:
		return 45
	case StageValidating:
		return 65
	case StageApplying:
		return 80
	case StageComposing:
		return 90
	case StageDone:
		return 100
	case StageFailed:
		return 0
	default:
		return 0
	}
}

// MaintenanceStatusDTO 统一任务状态响应 DTO。
type MaintenanceStatusDTO struct {
	Active          bool    `json:"active"`
	RunID           string  `json:"run_id"`
	Status          string  `json:"status"`
	Stage           string  `json:"stage"`
	ProgressPercent int     `json:"progress_percent"`
	TotalCandidates int     `json:"total_candidates"`
	Processed       int     `json:"processed"`
	Discarded       int     `json:"discarded"`
	Kept            int     `json:"kept"`
	Composed        int     `json:"composed"`
	ArchiveID       string  `json:"archive_id"`
	Summary         string  `json:"summary"`
	LastError       string  `json:"last_error"`
	StartedAt       string  `json:"started_at"`
	CompletedAt     *string `json:"completed_at"`
}

// ToStatusDTO 将 MaintenanceRun 转换为统一 DTO。
func (r MaintenanceRun) ToStatusDTO() MaintenanceStatusDTO {
	dto := MaintenanceStatusDTO{
		Active:          r.Status == MaintenanceRunRunning,
		RunID:           r.RunID,
		Status:          string(r.Status),
		Stage:           string(r.Stage),
		ProgressPercent: StageProgress(r.Stage),
		TotalCandidates: r.TotalCandidates,
		Processed:       r.Processed,
		Discarded:       r.Discarded,
		Kept:            r.Kept,
		Composed:        r.Composed,
		ArchiveID:       r.ArchiveID,
		Summary:         r.Summary,
		LastError:       r.LastError,
		StartedAt:       r.StartedAt.Format(time.RFC3339),
	}
	if r.CompletedAt != nil {
		s := r.CompletedAt.Format(time.RFC3339)
		dto.CompletedAt = &s
	}
	return dto
}

// MaintenanceService 清洗整合业务逻辑。
// 手动触发与自动触发复用同一套逻辑。
type MaintenanceService struct {
	repo          MaintenanceRepository
	candidateRepo Repository
	composer      *TopicComposer
	cleaner       MaintenanceCleaner
}

// MaintenanceCleaner LLM 清洗器接口。
type MaintenanceCleaner interface {
	Clean(ctx context.Context, candidates []Candidate) (CleanResult, error)
}

// CleanResult LLM 清洗结果。
type CleanResult struct {
	DiscardIDs  []string   `json:"discard_ids"`  // 应丢弃的候选 ID
	KeepIDs     []string   `json:"keep_ids"`     // 应保留的候选 ID
	MergeGroups [][]string `json:"merge_groups"` // 应合并的候选 ID 组
	Summary     string     `json:"summary"`      // 清洗摘要
}

// NewMaintenanceService 创建清洗整合服务。
func NewMaintenanceService(
	repo MaintenanceRepository,
	candidateRepo Repository,
	composer *TopicComposer,
	cleaner MaintenanceCleaner,
) *MaintenanceService {
	return &MaintenanceService{
		repo:          repo,
		candidateRepo: candidateRepo,
		composer:      composer,
		cleaner:       cleaner,
	}
}

var (
	// ErrMaintenanceAlreadyRunning 同项目已有运行中的清洗任务。
	ErrMaintenanceAlreadyRunning = errors.New("maintenance already running")
	// ErrMaintenanceNotFound 清洗任务不存在。
	ErrMaintenanceNotFound = errors.New("maintenance run not found")
	// ErrNoCandidatesToClean 没有可清洗的候选。
	ErrNoCandidatesToClean = errors.New("no candidates to clean")
)

// StartRun 创建清洗任务并返回,实际执行由 ExecuteRun 在后台完成。
func (s *MaintenanceService) StartRun(ctx context.Context, req MaintenanceRequest) (MaintenanceRun, error) {
	// 1. 检查是否有运行中的任务(项目级防重入),返回已有任务
	if existing, err := s.repo.GetRunningRun(ctx, req.OrgID, req.ProjectID); err == nil && existing != nil {
		return *existing, nil
	}

	// 2. 创建审计记录(queued 阶段)
	runID := fmt.Sprintf("maint_%s_%d", req.OrgID, time.Now().UnixNano())
	run, err := s.repo.CreateRun(ctx, MaintenanceRun{
		RunID:       runID,
		OrgID:       req.OrgID,
		ProjectID:   req.ProjectID,
		SourceKey:   req.SourceKey,
		ThreadID:    req.ThreadID,
		TriggerType: req.Trigger,
		Status:      MaintenanceRunRunning,
		Stage:       StageQueued,
		StartedAt:   time.Now().UTC(),
	})
	if err != nil {
		return MaintenanceRun{}, err
	}
	return run, nil
}

// ExecuteRun 在后台执行清洗整合逻辑,阶段会持久化到数据库。
func (s *MaintenanceService) ExecuteRun(ctx context.Context, req MaintenanceRequest, runID string) {
	// 0. 清理超时 running 任务(服务重启保护)
	_, _ = s.repo.MarkStaleRunningAsFailed(ctx, time.Now().Add(-10*time.Minute))

	// 1. 加载待清洗候选
	if err := s.repo.UpdateStage(ctx, runID, StageLoadingCandidates, 0); err != nil {
		return
	}
	candidates, err := s.candidateRepo.ListCandidates(ctx, ListFilter{
		OrgID:     req.OrgID,
		ProjectID: req.ProjectID,
		SourceKey: req.SourceKey,
		ThreadID:  req.ThreadID,
		Status:    StatusPending,
	})
	if err != nil {
		s.failRun(ctx, runID, err)
		return
	}
	if len(candidates) == 0 {
		s.failRun(ctx, runID, ErrNoCandidatesToClean)
		return
	}

	// 更新 total_candidates
	_ = s.repo.UpdateStage(ctx, runID, StageCallingLLM, len(candidates))

	// 2. LLM 清洗(失败时零写入)
	cleanResult, err := s.cleaner.Clean(ctx, candidates)
	if err != nil {
		s.failRun(ctx, runID, err)
		return
	}

	// 3. 校验 candidate_id 是否存在(防幻觉)
	if err := s.repo.UpdateStage(ctx, runID, StageValidating, len(candidates)); err != nil {
		return
	}
	discardSet := make(map[string]bool, len(cleanResult.DiscardIDs))
	for _, id := range cleanResult.DiscardIDs {
		discardSet[id] = true
	}
	keepSet := make(map[string]bool, len(cleanResult.KeepIDs))
	for _, id := range cleanResult.KeepIDs {
		keepSet[id] = true
	}

	for _, id := range cleanResult.DiscardIDs {
		if _, err := s.candidateRepo.GetCandidate(ctx, req.OrgID, id); err != nil {
			s.failRun(ctx, runID, fmt.Errorf("candidate_id %s not found: %w", id, err))
			return
		}
	}
	for _, id := range cleanResult.KeepIDs {
		if _, err := s.candidateRepo.GetCandidate(ctx, req.OrgID, id); err != nil {
			s.failRun(ctx, runID, fmt.Errorf("candidate_id %s not found: %w", id, err))
			return
		}
	}

	// 4. 执行清洗动作
	if err := s.repo.UpdateStage(ctx, runID, StageApplying, len(candidates)); err != nil {
		return
	}
	discarded := 0
	kept := 0
	for _, c := range candidates {
		if discardSet[c.CandidateID] {
			// 高风险候选不能被 AI 自动丢弃
			if c.RiskLevel == RiskHigh {
				kept++
				continue
			}
			if _, err := s.candidateRepo.UpdateCandidateStatus(ctx, req.OrgID, c.CandidateID, StatusDiscarded, c.Scores); err != nil {
				s.failRun(ctx, runID, fmt.Errorf("discard candidate %s failed: %s", c.CandidateID, err.Error()))
				return
			}
			discarded++
		} else if keepSet[c.CandidateID] {
			kept++
		}
	}

	// 5. 触发 TopicComposer 沉淀
	if err := s.repo.UpdateStage(ctx, runID, StageComposing, len(candidates)); err != nil {
		return
	}
	composed := 0
	archiveID := ""
	if s.composer != nil {
		result, err := s.composer.Compose(ctx, ComposeRequest{
			OrgID:     req.OrgID,
			ProjectID: req.ProjectID,
			SourceKey: req.SourceKey,
			ThreadID:  req.ThreadID,
			Force:     true,
		})
		if err == nil && result.Ready {
			composed = result.Composed
			archiveID = result.ArchiveID
		}
	}

	// 6. 更新审计记录(成功)
	now := time.Now().UTC()
	_ = s.repo.UpdateRun(ctx, runID, MaintenanceRunDone, MaintenanceRunUpdate{
		Processed:   len(candidates),
		Discarded:   discarded,
		Kept:        kept,
		Composed:    composed,
		ArchiveID:   archiveID,
		Summary:     cleanResult.Summary,
		CompletedAt: &now,
	})
	// 成功时也更新 stage 为 done
	_ = s.repo.UpdateStage(ctx, runID, StageDone, len(candidates))
}

// failRun 标记任务失败。
func (s *MaintenanceService) failRun(ctx context.Context, runID string, err error) {
	_ = s.repo.UpdateRun(ctx, runID, MaintenanceRunFailed, MaintenanceRunUpdate{
		LastError: err.Error(),
	})
	_ = s.repo.UpdateStage(ctx, runID, StageFailed, 0)
}

// GetActiveRun 获取项目当前运行中的任务。
func (s *MaintenanceService) GetActiveRun(ctx context.Context, orgID, projectID string) (*MaintenanceRun, error) {
	return s.repo.GetRunningRun(ctx, orgID, projectID)
}

// GetRun 按 runID 查询任务。
func (s *MaintenanceService) GetRun(ctx context.Context, runID string) (MaintenanceRun, error) {
	return s.repo.GetRun(ctx, runID)
}

// ShouldAutoClean 判断是否应该自动触发清洗整合。
// 条件:同项目待处理候选累计 >= 100 且最近 5 分钟没有新候选注入。
func (s *MaintenanceService) ShouldAutoClean(ctx context.Context, orgID, projectID string) bool {
	// 检查是否有运行中的任务
	if existing, err := s.repo.GetRunningRun(ctx, orgID, projectID); err == nil && existing != nil {
		return false
	}

	// 统计待处理候选数量
	candidates, err := s.candidateRepo.ListCandidates(ctx, ListFilter{
		OrgID:     orgID,
		ProjectID: projectID,
		Status:    StatusPending,
		Limit:     101, // 多取一个判断是否 >= 100
	})
	if err != nil || len(candidates) < 100 {
		return false
	}

	// 检查最近是否有新候选(5分钟内)
	if len(candidates) > 0 {
		newest := candidates[0].CreatedAt
		if time.Since(newest) < 5*time.Minute {
			return false
		}
	}

	return true
}
