package candidatememory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"memory-os/internal/hotmemory"
)

// MaintenanceTriggerType 整理归档触发类型。
type MaintenanceTriggerType string

const (
	MaintenanceTriggerManual MaintenanceTriggerType = "manual"
	MaintenanceTriggerAuto   MaintenanceTriggerType = "auto"
)

// MaintenanceRunStatus 整理归档任务状态。
type MaintenanceRunStatus string

const (
	MaintenanceRunRunning MaintenanceRunStatus = "running"
	MaintenanceRunDone    MaintenanceRunStatus = "done"
	MaintenanceRunFailed  MaintenanceRunStatus = "failed"
)

// MaintenanceRunStage 整理归档任务阶段。
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

// MaintenanceRun 一次整理归档操作的审计记录。
type MaintenanceRun struct {
	ID               int64
	RunID            string
	OrgID            string
	ProjectID        string
	SourceKey        string
	ThreadID         string
	TriggerType      MaintenanceTriggerType
	Status           MaintenanceRunStatus
	Stage            MaintenanceRunStage
	TotalCandidates  int
	Processed        int
	Discarded        int
	Kept             int
	Composed         int
	ArchiveMaterial  int
	PromotedHot      int
	NeedsReview      int
	HotMemoryDemoted int
	ArchiveID        string
	Summary          string
	LastError        string
	LockedBy         string
	StartedAt        time.Time
	CompletedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// MaintenanceRequest 整理归档请求。
type MaintenanceRequest struct {
	OrgID     string
	ProjectID string
	SourceKey string
	ThreadID  string
	Trigger   MaintenanceTriggerType
}

// MaintenanceResult 整理归档结果。
type MaintenanceResult struct {
	RunID     string
	Processed int
	Discarded int
	Kept      int
	Composed  int
	ArchiveID string
}

// MaintenanceRepository 整理归档持久化接口。
type MaintenanceRepository interface {
	CreateRun(ctx context.Context, run MaintenanceRun) (MaintenanceRun, error)
	GetRun(ctx context.Context, runID string) (MaintenanceRun, error)
	UpdateRun(ctx context.Context, runID string, status MaintenanceRunStatus, result MaintenanceRunUpdate) error
	GetRunningRun(ctx context.Context, orgID, projectID string) (*MaintenanceRun, error)
	GetRunningRunInScope(ctx context.Context, orgID, projectID, sourceKey, threadID string) (*MaintenanceRun, error)
	UpdateStage(ctx context.Context, runID string, stage MaintenanceRunStage, totalCandidates int) error
	MarkStaleRunningAsFailed(ctx context.Context, before time.Time) (int, error)
}

// MaintenanceRunUpdate 整理归档更新字段。
type MaintenanceRunUpdate struct {
	Processed        int
	Discarded        int
	Kept             int
	Composed         int
	ArchiveMaterial  int
	PromotedHot      int
	NeedsReview      int
	HotMemoryDemoted int
	ArchiveID        string
	Summary          string
	LastError        string
	CompletedAt      *time.Time
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
	Active           bool    `json:"active"`
	RunID            string  `json:"run_id"`
	Status           string  `json:"status"`
	Stage            string  `json:"stage"`
	ProgressPercent  int     `json:"progress_percent"`
	TotalCandidates  int     `json:"total_candidates"`
	Processed        int     `json:"processed"`
	Discarded        int     `json:"discarded"`
	Kept             int     `json:"kept"`
	Composed         int     `json:"composed"`
	ArchiveMaterial  int     `json:"archive_material"`
	PromotedHot      int     `json:"promoted_hot"`
	NeedsReview      int     `json:"needs_review"`
	HotMemoryDemoted int     `json:"hot_memory_demoted"`
	ArchiveID        string  `json:"archive_id"`
	Summary          string  `json:"summary"`
	LastError        string  `json:"last_error"`
	StartedAt        string  `json:"started_at"`
	CompletedAt      *string `json:"completed_at"`
}

// ToStatusDTO 将 MaintenanceRun 转换为统一 DTO。
func (r MaintenanceRun) ToStatusDTO() MaintenanceStatusDTO {
	dto := MaintenanceStatusDTO{
		Active:           r.Status == MaintenanceRunRunning,
		RunID:            r.RunID,
		Status:           string(r.Status),
		Stage:            string(r.Stage),
		ProgressPercent:  StageProgress(r.Stage),
		TotalCandidates:  r.TotalCandidates,
		Processed:        r.Processed,
		Discarded:        r.Discarded,
		Kept:             r.Kept,
		Composed:         r.Composed,
		ArchiveMaterial:  r.ArchiveMaterial,
		PromotedHot:      r.PromotedHot,
		NeedsReview:      r.NeedsReview,
		HotMemoryDemoted: r.HotMemoryDemoted,
		ArchiveID:        r.ArchiveID,
		Summary:          r.Summary,
		LastError:        r.LastError,
		StartedAt:        r.StartedAt.Format(time.RFC3339),
	}
	if r.CompletedAt != nil {
		s := r.CompletedAt.Format(time.RFC3339)
		dto.CompletedAt = &s
	}
	return dto
}

// MaintenanceService 整理归档业务逻辑。
// 手动触发与自动触发复用同一套逻辑。
type MaintenanceService struct {
	repo          MaintenanceRepository
	candidateRepo Repository
	composer      *TopicComposer
	cleaner       MaintenanceCleaner
	organizer     Organizer     // 统一整理决策器(优先于 cleaner)
	hotMemory     HotMemorySink // promote_hot 复用(triage_service 同包使用)
	triage        AutoTriage
}

// Organizer 统一 AI 整理决策器接口(替代 MaintenanceCleaner 主路径)。
type Organizer interface {
	Organize(ctx context.Context, candidates []Candidate, projects []string) (OrganizeResult, error)
}

func (s *MaintenanceService) WithOrganizer(o Organizer) *MaintenanceService {
	s.organizer = o
	return s
}

func (s *MaintenanceService) OrganizerConfigured() bool {
	return s != nil && s.organizer != nil
}

func (s *MaintenanceService) HotMemoryConfigured() bool {
	return s != nil && s.hotMemory != nil
}

func (s *MaintenanceService) WithHotMemory(h HotMemorySink) *MaintenanceService {
	s.hotMemory = h
	return s
}

// AutoTriage 是后台整理前的候选自动整理入口。
type AutoTriage interface {
	RunAutoTriage(ctx context.Context, filter TriageScanFilter) (TriageRunResult, error)
}

// MaintenanceCleaner 旧版 LLM 整理器接口。
type MaintenanceCleaner interface {
	Clean(ctx context.Context, candidates []Candidate) (CleanResult, error)
}

// CleanResult 旧版 LLM 整理结果。
type CleanResult struct {
	DiscardIDs  []string   `json:"discard_ids"`  // 应丢弃的候选 ID
	KeepIDs     []string   `json:"keep_ids"`     // 应保留的候选 ID
	MergeGroups [][]string `json:"merge_groups"` // 应合并的候选 ID 组
	Summary     string     `json:"summary"`      // 整理摘要
}

// NewMaintenanceService 创建整理归档服务。
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

func (s *MaintenanceService) WithTriage(triage AutoTriage) *MaintenanceService {
	s.triage = triage
	return s
}

var (
	// ErrMaintenanceAlreadyRunning 同项目已有运行中的整理任务。
	ErrMaintenanceAlreadyRunning = errors.New("maintenance already running")
	// ErrMaintenanceNotFound 整理任务不存在。
	ErrMaintenanceNotFound = errors.New("maintenance run not found")
	// ErrNoCandidatesToClean 没有可整理的候选。
	ErrNoCandidatesToClean = errors.New("no candidates to organize")
)

const autoCleanIdleThreshold = 5 * time.Minute
const workspaceMaintenanceSourceKey = "__workspace__"

// StartRun 创建整理任务并返回,实际执行由 ExecuteRun 在后台完成。
func (s *MaintenanceService) StartRun(ctx context.Context, req MaintenanceRequest) (MaintenanceRun, error) {
	// 1. 检查是否有同 scope 运行中的任务,返回已有任务。
	if existing, err := s.repo.GetRunningRunInScope(ctx, req.OrgID, req.ProjectID, req.SourceKey, req.ThreadID); err == nil && existing != nil {
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

// StartWorkspaceRun 创建一个工作区级整理任务,具体项目由 ExecuteWorkspaceRun 串行处理。
func (s *MaintenanceService) StartWorkspaceRun(ctx context.Context, orgID string) (MaintenanceRun, error) {
	return s.StartRun(ctx, MaintenanceRequest{
		OrgID:     orgID,
		ProjectID: "",
		SourceKey: workspaceMaintenanceSourceKey,
		Trigger:   MaintenanceTriggerManual,
	})
}

// ExecuteRun 在后台执行整理归档逻辑,阶段会持久化到数据库。
func (s *MaintenanceService) ExecuteRun(ctx context.Context, req MaintenanceRequest, runID string) {
	// 0. 清理超时 running 任务(服务重启保护)
	_, _ = s.repo.MarkStaleRunningAsFailed(ctx, time.Now().Add(-10*time.Minute))

	// 1. 加载待整理候选
	if err := s.repo.UpdateStage(ctx, runID, StageLoadingCandidates, 0); err != nil {
		return
	}
	candidates, err := s.listCleanableCandidates(ctx, req)
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

	// 2. AI 整理(失败零写入) — 优先用 organizer,cleaner 作为 fallback/兼容。
	var organizeResult OrganizeResult
	if s.organizer != nil {
		var err error
		organizeResult, err = s.organizer.Organize(ctx, candidates, nil)
		if err != nil {
			s.failRun(ctx, runID, err)
			return
		}
	} else if s.cleaner != nil {
		cleanRes, err := s.cleaner.Clean(ctx, candidates)
		if err != nil {
			s.failRun(ctx, runID, err)
			return
		}
		organizeResult = cleanToOrganizeResult(cleanRes)
	} else {
		s.failRun(ctx, runID, errors.New("no organizer or cleaner configured"))
		return
	}

	// 3. 幻觉校验已由 organizer 内部完成(若走 cleaner 兼容路径也通过 applyOrganizerAction 处理)。
	if err := s.repo.UpdateStage(ctx, runID, StageValidating, len(candidates)); err != nil {
		return
	}

	// 4. 应用整理决策
	if err := s.repo.UpdateStage(ctx, runID, StageApplying, len(candidates)); err != nil {
		return
	}
	discarded, kept, archiveMaterial, promotedHot, needsReview := 0, 0, 0, 0, 0
	decisionByID := make(map[string]OrganizerDecision, len(organizeResult.Decisions))
	for _, d := range organizeResult.Decisions {
		decisionByID[d.CandidateID] = d
	}
	for _, c := range candidates {
		d, ok := decisionByID[c.CandidateID]
		if !ok {
			// 未出决策的候选默认 needs_review(保守)。
			d = OrganizerDecision{Action: OrganizerActionNeedsReview, CandidateID: c.CandidateID}
		}
		newStatus, setReview := applyOrganizerAction(d)
		if setReview {
			c.NeedsReview = true
		}
		// 高风险降级(和 organizer 内部一致): 禁止 discard_noise/promote_hot/archive_material, 强制 needs_review。
		if c.RiskLevel == RiskHigh {
			if d.Action == OrganizerActionDiscardNoise || d.Action == OrganizerActionPromoteHot || d.Action == OrganizerActionArchiveMaterial {
				newStatus = StatusPending
				c.NeedsReview = true
			}
		}
		promoted := 0
		if newStatus == StatusPromotedToHot {
			if s.hotMemory == nil {
				newStatus = StatusPending
				c.NeedsReview = true
			} else {
				promoted, err = s.promoteCandidateToHotMemory(c, d)
				if err != nil {
					s.failRun(ctx, runID, fmt.Errorf("promote hot memory for %s: %w", c.CandidateID, err))
					return
				}
			}
		}
		if _, err := s.candidateRepo.UpdateCandidateStatus(ctx, req.OrgID, c.CandidateID, newStatus, c.Scores, c.NeedsReview); err != nil {
			s.failRun(ctx, runID, fmt.Errorf("apply action %s for %s: %w", d.Action, c.CandidateID, err))
			return
		}
		switch newStatus {
		case StatusDiscarded:
			discarded++
		case StatusAccepted:
			kept++
		case StatusInComposePool:
			archiveMaterial++
		case StatusPromotedToHot:
			promotedHot += promoted
		case StatusPending:
			needsReview++
		}
	}

	// 5. 归档: 只对整理后 in_compose_pool 且非空 source+thread 的分组执行,不 Force。
	if err := s.repo.UpdateStage(ctx, runID, StageComposing, len(candidates)); err != nil {
		return
	}
	composed := 0
	archiveID := ""
	if s.composer != nil && req.SourceKey != "" && req.ThreadID != "" {
		result, err := s.composer.Compose(ctx, ComposeRequest{
			OrgID:     req.OrgID,
			ProjectID: req.ProjectID,
			SourceKey: req.SourceKey,
			ThreadID:  req.ThreadID,
			Force:     false, // 自动整理不再 Force
		})
		if err == nil && result.Ready {
			composed = result.Composed
			archiveID = result.ArchiveID
		}
	}

	// 6. 更新审计记录(成功)
	now := time.Now().UTC()
	_ = s.repo.UpdateRun(ctx, runID, MaintenanceRunDone, MaintenanceRunUpdate{
		Processed:       len(candidates),
		Discarded:       discarded,
		Kept:            kept,
		Composed:        composed,
		ArchiveMaterial: archiveMaterial,
		PromotedHot:     promotedHot,
		NeedsReview:     needsReview,
		ArchiveID:       archiveID,
		Summary:         organizeResult.Summary,
		CompletedAt:     &now,
	})
	// 成功时也更新 stage 为 done
	_ = s.repo.UpdateStage(ctx, runID, StageDone, len(candidates))
}

func (s *MaintenanceService) promoteCandidateToHotMemory(candidate Candidate, decision OrganizerDecision) (int, error) {
	if s == nil || s.hotMemory == nil {
		return 0, nil
	}
	confidence := decision.Confidence
	if confidence <= 0 {
		confidence = candidate.Confidence
	}
	if confidence <= 0 {
		confidence = 0.8
	}
	request := hotmemory.UpsertRequest{
		OrgID:      candidate.OrgID,
		ProjectID:  candidate.ProjectID,
		UserID:     candidate.UserID,
		AgentID:    candidate.AgentID,
		Scope:      hotmemory.ScopeProject,
		Visibility: "project",
		Fact:       candidate.Content,
		SourceType: hotmemory.SourceTurnEvent,
		SourceRef:  candidateSourceRef(candidate),
		Confidence: confidence,
	}
	if decision.Scope == "global" || decision.Scope == "user" || candidate.ProjectID == "" {
		request.ProjectID = GlobalHotMemoryProjectID
		request.Scope = hotmemory.ScopeUser
		request.Visibility = "private"
		request.PermissionLabels = []string{}
	} else {
		request.PermissionLabels = []string{"project:" + candidate.ProjectID + ":read"}
	}
	if _, err := s.hotMemory.Upsert(request); err != nil {
		return 0, err
	}
	return 1, nil
}

// applyOrganizerAction 把决策动作映射到目标状态 + 是否置 needs_review。
func applyOrganizerAction(d OrganizerDecision) (Status, bool) {
	switch d.Action {
	case OrganizerActionDiscardNoise:
		return StatusDiscarded, false
	case OrganizerActionKeepCandidate:
		return StatusAccepted, false
	case OrganizerActionArchiveMaterial:
		return StatusInComposePool, false
	case OrganizerActionDuplicateOf:
		return StatusDiscarded, false // 去重: 标记丢弃,summary 写明合并目标
	case OrganizerActionPromoteHot:
		return StatusPromotedToHot, false // 实际提升在调用方处理
	case OrganizerActionNeedsReview:
		return StatusPending, true
	default:
		return StatusPending, true
	}
}

// cleanToOrganizeResult 把旧 CleanResult(discard/keep/merge)映射为 OrganizeResult,保持兼容。
func cleanToOrganizeResult(c CleanResult) OrganizeResult {
	decisions := make([]OrganizerDecision, 0, len(c.DiscardIDs)+len(c.KeepIDs))
	for _, id := range c.DiscardIDs {
		decisions = append(decisions, OrganizerDecision{CandidateID: id, Action: OrganizerActionDiscardNoise})
	}
	for _, id := range c.KeepIDs {
		decisions = append(decisions, OrganizerDecision{CandidateID: id, Action: OrganizerActionKeepCandidate})
	}
	for _, group := range c.MergeGroups {
		if len(group) < 2 {
			continue
		}
		// group[0] 作为合并目标保留。
		decisions = append(decisions, OrganizerDecision{CandidateID: group[0], Action: OrganizerActionKeepCandidate})
		for i := 1; i < len(group); i++ {
			decisions = append(decisions, OrganizerDecision{CandidateID: group[i], Action: OrganizerActionDuplicateOf, MergeTarget: group[0]})
		}
	}
	return OrganizeResult{Decisions: decisions, Summary: c.Summary}
}

func (s *MaintenanceService) listCleanableCandidates(ctx context.Context, req MaintenanceRequest) ([]Candidate, error) {
	candidates := []Candidate{}
	for _, status := range []Status{StatusPending, StatusInComposePool} {
		items, err := s.candidateRepo.ListCandidates(ctx, ListFilter{
			OrgID:     req.OrgID,
			ProjectID: req.ProjectID,
			SourceKey: req.SourceKey,
			ThreadID:  req.ThreadID,
			Status:    status,
		})
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, items...)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].CreatedAt.After(candidates[j].CreatedAt) })
	return candidates, nil
}

// ExecuteWorkspaceRun 按项目串行执行整理,空项目跳过,避免模型 provider 并发打爆。
func (s *MaintenanceService) ExecuteWorkspaceRun(ctx context.Context, orgID string, projectIDs []string, runID string) {
	_, _ = s.repo.MarkStaleRunningAsFailed(ctx, time.Now().Add(-10*time.Minute))
	if err := s.repo.UpdateStage(ctx, runID, StageLoadingCandidates, 0); err != nil {
		return
	}

	totalCandidates := 0
	processed := 0
	discarded := 0
	kept := 0
	composed := 0
	archiveMaterial := 0
	promotedHot := 0
	needsReview := 0
	projectsWithCandidates := 0
	for _, projectID := range projectIDs {
		req := MaintenanceRequest{OrgID: orgID, ProjectID: projectID, Trigger: MaintenanceTriggerManual}
		candidates, err := s.listCleanableCandidates(ctx, req)
		if err != nil {
			s.failRun(ctx, runID, err)
			return
		}
		if len(candidates) == 0 {
			continue
		}
		if existing, err := s.repo.GetRunningRunInScope(ctx, orgID, projectID, "", ""); err == nil && existing != nil {
			continue
		}
		projectsWithCandidates++
		totalCandidates += len(candidates)
		_ = s.repo.UpdateStage(ctx, runID, StageCallingLLM, totalCandidates)

		child, err := s.StartRun(ctx, req)
		if err != nil {
			s.failRun(ctx, runID, err)
			return
		}
		s.ExecuteRun(ctx, req, child.RunID)
		final, err := s.repo.GetRun(ctx, child.RunID)
		if err != nil {
			s.failRun(ctx, runID, err)
			return
		}
		if final.Status == MaintenanceRunFailed {
			s.failRun(ctx, runID, errors.New(final.LastError))
			return
		}
		processed += final.Processed
		discarded += final.Discarded
		kept += final.Kept
		composed += final.Composed
		archiveMaterial += final.ArchiveMaterial
		promotedHot += final.PromotedHot
		needsReview += final.NeedsReview
	}

	now := time.Now().UTC()
	summary := fmt.Sprintf("工作区整理完成：处理 %d 个项目，跳过 %d 个空项目。", projectsWithCandidates, len(projectIDs)-projectsWithCandidates)
	if projectsWithCandidates == 0 {
		summary = "工作区没有可整理候选。"
	}
	_ = s.repo.UpdateRun(ctx, runID, MaintenanceRunDone, MaintenanceRunUpdate{
		Processed:       processed,
		Discarded:       discarded,
		Kept:            kept,
		Composed:        composed,
		ArchiveMaterial: archiveMaterial,
		PromotedHot:     promotedHot,
		NeedsReview:     needsReview,
		Summary:         summary,
		CompletedAt:     &now,
	})
	_ = s.repo.UpdateStage(ctx, runID, StageDone, totalCandidates)
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

// RunAutoClean 扫描已满足归档条件的 topic,并按项目/source/thread 自动执行整理归档。
func (s *MaintenanceService) RunAutoClean(ctx context.Context) (int, error) {
	if s == nil || s.candidateRepo == nil {
		return 0, errors.New("maintenance service is not configured")
	}
	if s.triage != nil {
		_, _ = s.triage.RunAutoTriage(ctx, TriageScanFilter{Limit: defaultTriageScanLimit})
	}
	topics, err := s.candidateRepo.ListTopicStates(ctx, TopicStateFilter{Limit: 1000})
	if err != nil {
		return 0, err
	}
	started := 0
	for _, topic := range topics {
		if topic.ComposedArchiveID != "" {
			continue
		}
		if !s.shouldAutoCleanScope(ctx, topic.OrgID, topic.ProjectID, topic.SourceKey, topic.ThreadID) {
			continue
		}
		req := MaintenanceRequest{
			OrgID:     topic.OrgID,
			ProjectID: topic.ProjectID,
			SourceKey: topic.SourceKey,
			ThreadID:  topic.ThreadID,
			Trigger:   MaintenanceTriggerAuto,
		}
		run, err := s.StartRun(ctx, req)
		if err != nil {
			return started, err
		}
		s.ExecuteRun(ctx, req, run.RunID)
		started++
	}
	return started, nil
}

// ShouldAutoClean 判断是否应该自动触发整理归档。
// 条件:同项目待整理候选累计达到归档阈值且最近 5 分钟没有新候选注入。
func (s *MaintenanceService) ShouldAutoClean(ctx context.Context, orgID, projectID string) bool {
	return s.shouldAutoCleanScope(ctx, orgID, projectID, "", "")
}

func (s *MaintenanceService) shouldAutoCleanScope(ctx context.Context, orgID, projectID, sourceKey, threadID string) bool {
	// 检查是否有运行中的任务
	if existing, err := s.repo.GetRunningRunInScope(ctx, orgID, projectID, sourceKey, threadID); err == nil && existing != nil {
		return false
	}

	// 统计待整理候选数量
	candidates, err := s.candidateRepo.ListCandidates(ctx, ListFilter{
		OrgID:     orgID,
		ProjectID: projectID,
		SourceKey: sourceKey,
		ThreadID:  threadID,
		Status:    StatusPending,
		Limit:     composeMinCandidates,
	})
	if err != nil || len(candidates) < composeMinCandidates {
		return false
	}

	// 检查最近是否有新候选(5分钟内)
	if len(candidates) > 0 {
		newest := candidates[0].CreatedAt
		if time.Since(newest) < autoCleanIdleThreshold {
			return false
		}
	}

	return true
}
