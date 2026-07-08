package candidatememory

import "context"

// Service 候选记忆领域服务。
// Phase 1 提供 CRUD + 打分 + 用户确认(accept/discard/markComposed);
// 自动提炼(extractor)与分流路由在 Phase 3 接入。
type Service struct {
	repo   Repository
	scorer Scorer
}

func NewService(repo Repository, scorer Scorer) *Service {
	if scorer == nil {
		scorer = RuleScorer{}
	}
	return &Service{repo: repo, scorer: scorer}
}

// CreateAndScore 创建候选;若调用方未显式提供 scores,则用 Scorer 计算。
func (s *Service) CreateAndScore(ctx context.Context, c Candidate) (Candidate, error) {
	if c.Scores.HotMemoryScore == 0 && c.Scores.ComposeScore == 0 {
		c.Scores = s.scorer.Score(c)
	}
	return s.repo.CreateCandidate(ctx, c)
}

func (s *Service) Get(ctx context.Context, orgID, candidateID string) (Candidate, error) {
	return s.repo.GetCandidate(ctx, orgID, candidateID)
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]Candidate, error) {
	return s.repo.ListCandidates(ctx, filter)
}

// Accept 用户确认接受候选。
func (s *Service) Accept(ctx context.Context, orgID, candidateID string) (Candidate, error) {
	return s.updateStatus(ctx, orgID, candidateID, StatusAccepted)
}

// Discard 用户丢弃候选。
func (s *Service) Discard(ctx context.Context, orgID, candidateID string) (Candidate, error) {
	return s.updateStatus(ctx, orgID, candidateID, StatusDiscarded)
}

// MarkComposed 标记候选已归档进 Archive(Phase 5 Composer 调用)。
func (s *Service) MarkComposed(ctx context.Context, orgID, candidateID string) (Candidate, error) {
	return s.updateStatus(ctx, orgID, candidateID, StatusComposed)
}

func (s *Service) updateStatus(ctx context.Context, orgID, candidateID string, status Status) (Candidate, error) {
	existing, err := s.repo.GetCandidate(ctx, orgID, candidateID)
	if err != nil {
		return Candidate{}, err
	}
	return s.repo.UpdateCandidateStatus(ctx, orgID, candidateID, status, existing.Scores, existing.NeedsReview)
}
