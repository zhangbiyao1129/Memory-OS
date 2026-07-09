package memorykernel

import (
	"context"
	"fmt"
	"time"
)

// CandidateApplier 候选记忆治理状态更新接口。
type CandidateApplier interface {
	UpdateCandidateGovernance(ctx context.Context, orgID, candidateID string, status interface{}, needsReview bool, reason string, supersededBy string) (interface{}, error)
}

// HotMemoryApplier 热记忆降级接口。
type HotMemoryApplier interface {
	Get(memoryID string) (HotMemoryGetResult, error)
	Update(memory HotMemoryUpdateRequest) (HotMemoryUpdateResult, error)
}

// HotMemoryGetResult 热记忆读取结果。
type HotMemoryGetResult struct {
	MemoryID string
	Pinned   bool
	Status   string
}

// HotMemoryUpdateRequest 热记忆更新请求。
type HotMemoryUpdateRequest struct {
	MemoryID string
	Status   string
}

// HotMemoryUpdateResult 热记忆更新结果。
type HotMemoryUpdateResult struct {
	MemoryID string
	Status   string
}

// CorrectionArchiveCreator 修订归档创建接口。
type CorrectionArchiveCreator interface {
	CreateCorrectionArchive(ctx context.Context, req CorrectionArchiveRequest) (CorrectionArchiveResult, error)
}

// CIRunner 记忆 CI 执行接口。
type CIRunner interface {
	RunCase(ctx context.Context, caseID string) (CIResult, error)
}

// Service Memory Kernel 治理服务。
type Service struct {
	repository       Repository
	collector        Collector
	classifier       Classifier
	candidateApplier CandidateApplier
	hotMemoryApplier HotMemoryApplier
	archiveCreator   CorrectionArchiveCreator
	ciRunner         CIRunner
}

// ServiceOptions 服务依赖注入。
type ServiceOptions struct {
	Repository       Repository
	Collector        Collector
	Classifier       Classifier
	CandidateApplier CandidateApplier
	HotMemoryApplier HotMemoryApplier
	ArchiveCreator   CorrectionArchiveCreator
	CIRunner         CIRunner
}

func NewService(opts ServiceOptions) *Service {
	return &Service{
		repository:       opts.Repository,
		collector:        opts.Collector,
		classifier:       opts.Classifier,
		candidateApplier: opts.CandidateApplier,
		hotMemoryApplier: opts.HotMemoryApplier,
		archiveCreator:   opts.ArchiveCreator,
		ciRunner:         opts.CIRunner,
	}
}

// RunGovernance 执行一次完整的记忆治理流程。
func (s *Service) RunGovernance(ctx context.Context, req GovernanceRequest) (GovernanceRun, error) {
	runID := fmt.Sprintf("gov_run_%s_%s_%d", req.OrgID, req.ProjectID, time.Now().UnixNano())
	run, err := s.repository.CreateRun(ctx, GovernanceRun{
		RunID:       runID,
		OrgID:       req.OrgID,
		ProjectID:   req.ProjectID,
		SourceKey:   req.SourceKey,
		ThreadID:    req.ThreadID,
		TriggerType: req.TriggerType,
	})
	if err != nil {
		return GovernanceRun{}, fmt.Errorf("create run: %w", err)
	}

	scope := Scope{
		OrgID:     req.OrgID,
		ProjectID: req.ProjectID,
		SourceKey: req.SourceKey,
		ThreadID:  req.ThreadID,
	}

	// 1. collect
	input, err := s.collector.Collect(ctx, scope)
	if err != nil {
		_ = s.repository.FailRun(ctx, runID, err)
		return run, fmt.Errorf("collect: %w", err)
	}

	// 2. classify
	result, err := s.classifier.Classify(ctx, input)
	if err != nil {
		_ = s.repository.FailRun(ctx, runID, err)
		return run, fmt.Errorf("classify: %w", err)
	}

	// 3. apply actions and upsert units/claims
	update := GovernanceRunUpdate{
		Status:              RunDone,
		ProcessedCandidates: len(input.Candidates),
		ProcessedHotMemories: len(input.HotMemories),
		ProcessedArchives:   len(input.Archives),
		Summary:             result.Summary,
	}

	for _, unit := range result.Units {
		unit.OrgID = req.OrgID
		unit.ProjectID = req.ProjectID
		if unit.UserID == "" {
			unit.UserID = req.UserID
		}
		if _, err := s.repository.UpsertUnit(ctx, unit); err != nil {
			_ = s.repository.FailRun(ctx, runID, err)
			return run, fmt.Errorf("upsert unit: %w", err)
		}
		update.CreatedUnits++
	}

	for _, claim := range result.Claims {
		claim.OrgID = req.OrgID
		claim.ProjectID = req.ProjectID
		if _, err := s.repository.UpsertClaim(ctx, claim); err != nil {
			_ = s.repository.FailRun(ctx, runID, err)
			return run, fmt.Errorf("upsert claim: %w", err)
		}
	}

	for i := range result.Actions {
		result.Actions[i].RunID = runID
		result.Actions[i].OrgID = req.OrgID
		result.Actions[i].ProjectID = req.ProjectID
	}

	for i := range result.Actions {
		action := &result.Actions[i]
		if err := s.applyAction(ctx, action); err != nil {
			_ = s.repository.FailRun(ctx, runID, err)
			return run, fmt.Errorf("apply action %s: %w", action.ActionID, err)
		}
		if _, err := s.repository.RecordAction(ctx, *action); err != nil {
			_ = s.repository.FailRun(ctx, runID, err)
			return run, fmt.Errorf("record action: %w", err)
		}

		if !action.Applied {
			continue
		}
		switch action.Action {
		case ActionDiscardStale, ActionDiscardNoise:
			update.StaleCandidates++
		case ActionMarkSuperseded:
			update.SupersededUnits++
		case ActionDemoteHotMemory:
			update.DemotedHotMemories++
		}
	}

	// 4. upsert CI cases
	for _, ciCase := range result.CICases {
		ciCase.OrgID = req.OrgID
		ciCase.ProjectID = req.ProjectID
		ciCase.SourceRunID = runID
		if _, err := s.repository.UpsertCICase(ctx, ciCase); err != nil {
			_ = s.repository.FailRun(ctx, runID, err)
			return run, fmt.Errorf("upsert ci case: %w", err)
		}
		update.CICasesCreated++
	}

	// 5. run CI if configured
	if s.ciRunner != nil {
		for _, ciCase := range result.CICases {
			ciResult, err := s.ciRunner.RunCase(ctx, ciCase.CaseID)
			if err != nil {
				continue // CI 失败不阻塞治理
			}
			if _, err := s.repository.RecordCIResult(ctx, ciResult); err != nil {
				continue
			}
			if ciResult.Passed {
				update.CICasesPassed++
			}
		}
	}

	// 6. complete run
	if err := s.repository.CompleteRun(ctx, runID, update); err != nil {
		return run, fmt.Errorf("complete run: %w", err)
	}

	run.Status = update.Status
	run.CreatedUnits = update.CreatedUnits
	run.SupersededUnits = update.SupersededUnits
	run.StaleCandidates = update.StaleCandidates
	run.DemotedHotMemories = update.DemotedHotMemories
	run.CICasesCreated = update.CICasesCreated
	run.CICasesPassed = update.CICasesPassed
	run.Summary = update.Summary
	return run, nil
}

func (s *Service) applyAction(ctx context.Context, action *GovernanceAction) error {
	action.Applied = true // 默认已应用，特殊情况设为 false

	switch action.Action {
	case ActionDiscardStale, ActionDiscardNoise:
		if s.candidateApplier != nil && action.TargetType == "candidate" {
			_, err := s.candidateApplier.UpdateCandidateGovernance(ctx, action.OrgID, action.TargetID, "discarded", false, action.Reason, "")
			if err != nil {
				return err
			}
		}
	case ActionMarkSuperseded:
		if action.TargetType == "candidate" && s.candidateApplier != nil {
			supersededBy := ""
			for _, ref := range action.EvidenceRefs {
				if ref.Kind == "unit" {
					supersededBy = ref.ID
					break
				}
			}
			_, err := s.candidateApplier.UpdateCandidateGovernance(ctx, action.OrgID, action.TargetID, "superseded", false, action.Reason, supersededBy)
			if err != nil {
				return err
			}
		}
	case ActionDemoteHotMemory:
		if s.hotMemoryApplier != nil && action.TargetType == "hot_memory" {
			hm, err := s.hotMemoryApplier.Get(action.TargetID)
			if err != nil {
				return err
			}
			if hm.Pinned {
				action.Applied = false
				return nil
			}
			if hm.Status == "active" || hm.Status == "promoted" {
				_, err := s.hotMemoryApplier.Update(HotMemoryUpdateRequest{
					MemoryID: action.TargetID,
					Status:   "demoted",
				})
				if err != nil {
					return err
				}
			}
		}
	case ActionCreateCorrection:
		if s.archiveCreator != nil {
			result, err := s.archiveCreator.CreateCorrectionArchive(ctx, CorrectionArchiveRequest{
				OrgID:     action.OrgID,
				ProjectID: action.ProjectID,
			})
			if err != nil {
				return err
			}
			_ = result // correction archive ID 记录在 run 中
		}
	case ActionNeedsReview:
		if s.candidateApplier != nil && action.TargetType == "candidate" {
			_, err := s.candidateApplier.UpdateCandidateGovernance(ctx, action.OrgID, action.TargetID, "pending", true, action.Reason, "")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// RunAutoGovernance 扫描最近活跃 scope 并自动运行治理。
func (s *Service) RunAutoGovernance(ctx context.Context) (int, error) {
	// 第一版简化实现：只对有 current units 的 scope 运行
	// 生产版需要扫描最近 24 小时有活动的 scope
	return 0, nil
}
