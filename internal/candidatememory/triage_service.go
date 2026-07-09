package candidatememory

import (
	"context"
	"errors"
	"fmt"

	"memory-os/internal/hotmemory"
)

const (
	globalTriagePromotionThreshold = 0.82
	projectLinkPromotionThreshold  = 0.78
	defaultTriageScanLimit         = 50
)

// ProjectCatalog 为 triage 提供当前用户可见项目目录。
type ProjectCatalog interface {
	ListProjectsForTriage(userID, orgID string) ([]TriageProject, error)
}

// HotMemorySink 是 TriageService 写入热记忆所需的最小接口。
type HotMemorySink interface {
	Upsert(request hotmemory.UpsertRequest) (hotmemory.Memory, error)
}

type TriageServiceOptions struct {
	Repo           TriageRepository
	Classifier     TriageClassifier
	Fallback       TriageClassifier
	ProjectCatalog ProjectCatalog
	HotMemory      HotMemorySink
}

// TriageService 扫描候选记忆,写 triage 解释结果,并将高置信结果投递到 Hot Memory。
type TriageService struct {
	repo           TriageRepository
	classifier     TriageClassifier
	fallback       TriageClassifier
	projectCatalog ProjectCatalog
	hotMemory      HotMemorySink
}

type TriageRunResult struct {
	Processed    int
	Triaged      int
	ProjectLinks int
	Promoted     int
	Failed       int
}

func NewTriageService(options TriageServiceOptions) *TriageService {
	fallback := options.Fallback
	if fallback == nil {
		fallback = RuleTriageClassifier{}
	}
	return &TriageService{
		repo:           options.Repo,
		classifier:     options.Classifier,
		fallback:       fallback,
		projectCatalog: options.ProjectCatalog,
		hotMemory:      options.HotMemory,
	}
}

func (s *TriageService) RunAutoTriage(ctx context.Context, filter TriageScanFilter) (TriageRunResult, error) {
	if s == nil || s.repo == nil {
		return TriageRunResult{}, errors.New("triage service is not configured")
	}
	if filter.Limit <= 0 {
		filter.Limit = defaultTriageScanLimit
	}
	candidates, err := s.repo.ListCandidatesNeedingTriage(ctx, filter)
	if err != nil {
		return TriageRunResult{}, err
	}
	result := TriageRunResult{}
	for _, candidate := range candidates {
		result.Processed++
		decision, classifyErr := s.classify(ctx, candidate)
		if classifyErr != nil {
			result.Failed++
			_, _ = s.repo.UpsertTriageResult(ctx, TriageResult{
				OrgID:           candidate.OrgID,
				CandidateID:     candidate.CandidateID,
				SourceProjectID: candidate.ProjectID,
				SourceKey:       candidate.SourceKey,
				TriageScope:     TriageScopeInbox,
				Confidence:      0,
				ReviewState:     TriageReviewNeedsReview,
				Reason:          "自动整理失败",
				SourceRefs:      triageSourceRefs(candidate),
				Attempts:        1,
				LastError:       sanitizeTriageText(classifyErr.Error()),
			})
			continue
		}

		reviewState := reviewStateForDecision(candidate, decision)
		triageResult, err := s.repo.UpsertTriageResult(ctx, TriageResult{
			OrgID:           candidate.OrgID,
			CandidateID:     candidate.CandidateID,
			SourceProjectID: candidate.ProjectID,
			SourceKey:       candidate.SourceKey,
			TriageScope:     decision.Scope,
			Confidence:      decision.Confidence,
			ReviewState:     reviewState,
			Reason:          decision.Reason,
			SourceRefs:      triageSourceRefs(candidate),
			Attempts:        1,
		})
		if err != nil {
			result.Failed++
			continue
		}
		result.Triaged++

		links := normalizeDecisionLinks(candidate, decision.ProjectLinks)
		if err := s.repo.ReplaceProjectLinks(ctx, candidate.OrgID, candidate.CandidateID, links); err != nil {
			result.Failed++
			triageResult.LastError = sanitizeTriageText(err.Error())
			_, _ = s.repo.UpsertTriageResult(ctx, triageResult)
			continue
		}
		result.ProjectLinks += len(links)

		promotedIDs, promoted, promoteErr := s.promote(ctx, candidate, decision, reviewState, links)
		result.Promoted += promoted
		if promoteErr != nil {
			result.Failed++
			triageResult.LastError = sanitizeTriageText(promoteErr.Error())
			_, _ = s.repo.UpsertTriageResult(ctx, triageResult)
			continue
		}
		if len(promotedIDs) > 0 {
			_ = s.repo.UpdatePromotedHotMemoryIDs(ctx, candidate.OrgID, candidate.CandidateID, promotedIDs)
		}
	}
	return result, nil
}

func (s *TriageService) classify(ctx context.Context, candidate Candidate) (TriageDecision, error) {
	projects := []TriageProject{}
	if s.projectCatalog != nil {
		if listed, err := s.projectCatalog.ListProjectsForTriage(candidate.UserID, candidate.OrgID); err == nil {
			projects = listed
		}
	}
	input := TriageInput{Candidate: candidate, Projects: projects}
	if s.classifier != nil {
		if decision, err := s.classifier.Classify(ctx, input); err == nil {
			return decision, nil
		}
	}
	if s.fallback != nil {
		return s.fallback.Classify(ctx, input)
	}
	return TriageDecision{}, errors.New("triage classifier not configured")
}

func (s *TriageService) promote(ctx context.Context, candidate Candidate, decision TriageDecision, reviewState TriageReviewState, links []CandidateProjectLink) ([]string, int, error) {
	if s.hotMemory == nil || reviewState != TriageReviewAutoApplied || candidate.RiskLevel == RiskHigh {
		return nil, 0, nil
	}
	promotedIDs := []string{}
	promoted := 0
	if isGlobalTriageScope(decision.Scope) && decision.Confidence >= globalTriagePromotionThreshold {
		memory, err := s.hotMemory.Upsert(hotmemory.UpsertRequest{
			OrgID:            candidate.OrgID,
			ProjectID:        GlobalHotMemoryProjectID,
			UserID:           candidate.UserID,
			AgentID:          candidate.AgentID,
			Scope:            hotmemory.ScopeUser,
			Visibility:       "private",
			PermissionLabels: []string{},
			Fact:             candidate.Content,
			SourceType:       hotmemory.SourceTurnEvent,
			SourceRef:        candidateSourceRef(candidate),
			Confidence:       decision.Confidence,
		})
		if err != nil {
			return promotedIDs, promoted, err
		}
		promotedIDs = append(promotedIDs, memory.MemoryID)
		promoted++
	}
	for _, link := range links {
		if link.Confidence < projectLinkPromotionThreshold {
			continue
		}
		memory, err := s.hotMemory.Upsert(hotmemory.UpsertRequest{
			OrgID:            candidate.OrgID,
			ProjectID:        link.LinkedProjectID,
			UserID:           candidate.UserID,
			AgentID:          candidate.AgentID,
			Scope:            hotmemory.ScopeProject,
			Visibility:       "project",
			PermissionLabels: []string{"project:" + link.LinkedProjectID + ":read"},
			Fact:             candidate.Content,
			SourceType:       hotmemory.SourceTurnEvent,
			SourceRef:        candidateSourceRef(candidate),
			Confidence:       link.Confidence,
		})
		if err != nil {
			return promotedIDs, promoted, err
		}
		promotedIDs = append(promotedIDs, memory.MemoryID)
		promoted++
		_ = s.repo.UpdateProjectLinkPromotion(ctx, candidate.OrgID, candidate.CandidateID, link.LinkedProjectID, memory.MemoryID)
	}
	return promotedIDs, promoted, nil
}

func reviewStateForDecision(candidate Candidate, decision TriageDecision) TriageReviewState {
	if candidate.RiskLevel == RiskHigh {
		return TriageReviewNeedsReview
	}
	switch decision.Scope {
	case TriageScopeDiscard:
		if decision.Confidence >= globalTriagePromotionThreshold {
			return TriageReviewAutoApplied
		}
		return TriageReviewWeak
	case TriageScopeInbox:
		return TriageReviewWeak
	}
	if isGlobalTriageScope(decision.Scope) && decision.Confidence >= globalTriagePromotionThreshold {
		return TriageReviewAutoApplied
	}
	for _, link := range decision.ProjectLinks {
		if link.Confidence >= projectLinkPromotionThreshold {
			return TriageReviewAutoApplied
		}
	}
	return TriageReviewWeak
}

func normalizeDecisionLinks(candidate Candidate, links []CandidateProjectLink) []CandidateProjectLink {
	out := []CandidateProjectLink{}
	for _, link := range links {
		if link.LinkedProjectID == "" {
			continue
		}
		link.OrgID = candidate.OrgID
		link.CandidateID = candidate.CandidateID
		link.Status = "active"
		link.Evidence = sanitizeTriageText(link.Evidence)
		out = append(out, link)
	}
	return out
}

func isGlobalTriageScope(scope TriageScope) bool {
	return scope == TriageScopeGlobal || scope == TriageScopeTooling || scope == TriageScopePersonalPref
}

func triageSourceRefs(candidate Candidate) []TriageSourceRef {
	refs := []TriageSourceRef{{Kind: "candidate", ID: candidate.CandidateID}}
	for _, eventID := range candidate.SourceEventIDs {
		if eventID == "" {
			continue
		}
		refs = append(refs, TriageSourceRef{Kind: "turn_event", ID: eventID})
	}
	return refs
}

func candidateSourceRef(candidate Candidate) string {
	if len(candidate.SourceEventIDs) > 0 && candidate.SourceEventIDs[0] != "" {
		return candidate.SourceEventIDs[0]
	}
	if candidate.CandidateID != "" {
		return candidate.CandidateID
	}
	return fmt.Sprintf("%s:%s", candidate.SourceKey, candidate.ThreadID)
}
