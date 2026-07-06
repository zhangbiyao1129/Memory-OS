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

// MaintenanceRun 一次清洗整合操作的审计记录。
type MaintenanceRun struct {
	ID          int64
	RunID       string
	OrgID       string
	ProjectID   string
	SourceKey   string
	ThreadID    string
	TriggerType MaintenanceTriggerType
	Status      MaintenanceRunStatus
	Processed   int
	Discarded   int
	Kept        int
	Composed    int
	ArchiveID   string
	Summary     string
	LastError   string
	LockedBy    string
	StartedAt   time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
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

// Run 执行清洗整合(手动或自动触发)。
// 复用同一套逻辑,区别仅在 trigger_type 审计记录。
func (s *MaintenanceService) Run(ctx context.Context, req MaintenanceRequest) (MaintenanceResult, error) {
	// 1. 检查是否有运行中的任务(项目级防重入)
	if existing, err := s.repo.GetRunningRun(ctx, req.OrgID, req.ProjectID); err == nil && existing != nil {
		return MaintenanceResult{}, ErrMaintenanceAlreadyRunning
	}

	// 2. 加载待清洗候选
	candidates, err := s.candidateRepo.ListCandidates(ctx, ListFilter{
		OrgID:     req.OrgID,
		ProjectID: req.ProjectID,
		SourceKey: req.SourceKey,
		ThreadID:  req.ThreadID,
		Status:    StatusPending,
	})
	if err != nil {
		return MaintenanceResult{}, err
	}
	if len(candidates) == 0 {
		return MaintenanceResult{}, ErrNoCandidatesToClean
	}

	// 3. 创建审计记录
	runID := fmt.Sprintf("maint_%s_%d", req.OrgID, time.Now().UnixNano())
	run, err := s.repo.CreateRun(ctx, MaintenanceRun{
		RunID:       runID,
		OrgID:       req.OrgID,
		ProjectID:   req.ProjectID,
		SourceKey:   req.SourceKey,
		ThreadID:    req.ThreadID,
		TriggerType: req.Trigger,
		Status:      MaintenanceRunRunning,
		StartedAt:   time.Now().UTC(),
	})
	if err != nil {
		return MaintenanceResult{}, err
	}

	// 4. LLM 清洗(失败时零写入)
	cleanResult, err := s.cleaner.Clean(ctx, candidates)
	if err != nil {
		_ = s.repo.UpdateRun(ctx, run.RunID, MaintenanceRunFailed, MaintenanceRunUpdate{
			LastError: err.Error(),
		})
		return MaintenanceResult{}, err
	}

	// 5. 校验 candidate_id 是否存在(防幻觉)
	discardSet := make(map[string]bool, len(cleanResult.DiscardIDs))
	for _, id := range cleanResult.DiscardIDs {
		discardSet[id] = true
	}
	keepSet := make(map[string]bool, len(cleanResult.KeepIDs))
	for _, id := range cleanResult.KeepIDs {
		keepSet[id] = true
	}

	// 验证所有 ID 都存在
	for _, id := range cleanResult.DiscardIDs {
		if _, err := s.candidateRepo.GetCandidate(ctx, req.OrgID, id); err != nil {
			_ = s.repo.UpdateRun(ctx, run.RunID, MaintenanceRunFailed, MaintenanceRunUpdate{
				LastError: fmt.Sprintf("candidate_id %s not found", id),
			})
			return MaintenanceResult{}, fmt.Errorf("candidate_id %s not found: %w", id, err)
		}
	}
	for _, id := range cleanResult.KeepIDs {
		if _, err := s.candidateRepo.GetCandidate(ctx, req.OrgID, id); err != nil {
			_ = s.repo.UpdateRun(ctx, run.RunID, MaintenanceRunFailed, MaintenanceRunUpdate{
				LastError: fmt.Sprintf("candidate_id %s not found", id),
			})
			return MaintenanceResult{}, fmt.Errorf("candidate_id %s not found: %w", id, err)
		}
	}

	// 6. 执行清洗动作
	discarded := 0
	kept := 0
	for _, c := range candidates {
		if discardSet[c.CandidateID] {
			// 高风险候选不能被 AI 自动丢弃
			if c.RiskLevel == RiskHigh {
				// 高风险保留并写入风险说明
				kept++
				continue
			}
			if _, err := s.candidateRepo.UpdateCandidateStatus(ctx, req.OrgID, c.CandidateID, StatusDiscarded, c.Scores); err != nil {
				_ = s.repo.UpdateRun(ctx, run.RunID, MaintenanceRunFailed, MaintenanceRunUpdate{
					LastError: fmt.Sprintf("discard candidate %s failed: %s", c.CandidateID, err.Error()),
				})
				return MaintenanceResult{}, err
			}
			discarded++
		} else if keepSet[c.CandidateID] {
			kept++
		}
	}

	// 7. 触发 TopicComposer 沉淀(复用现有逻辑)
	composed := 0
	archiveID := ""
	if s.composer != nil {
		result, err := s.composer.Compose(ctx, ComposeRequest{
			OrgID:     req.OrgID,
			ProjectID: req.ProjectID,
			SourceKey: req.SourceKey,
			ThreadID:  req.ThreadID,
			Force:     true, // 清洗后强制沉淀
		})
		if err == nil && result.Ready {
			composed = result.Composed
			archiveID = result.ArchiveID
		}
	}

	// 8. 更新审计记录(成功)
	now := time.Now().UTC()
	_ = s.repo.UpdateRun(ctx, run.RunID, MaintenanceRunDone, MaintenanceRunUpdate{
		Processed:   len(candidates),
		Discarded:   discarded,
		Kept:        kept,
		Composed:    composed,
		ArchiveID:   archiveID,
		Summary:     cleanResult.Summary,
		CompletedAt: &now,
	})

	return MaintenanceResult{
		RunID:     runID,
		Processed: len(candidates),
		Discarded: discarded,
		Kept:      kept,
		Composed:  composed,
		ArchiveID: archiveID,
	}, nil
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
