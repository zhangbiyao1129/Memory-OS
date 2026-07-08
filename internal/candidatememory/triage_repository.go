package candidatememory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// TriageRepository 管理 triage 结果和候选跨项目链接。
type TriageRepository interface {
	ListCandidatesNeedingTriage(ctx context.Context, filter TriageScanFilter) ([]Candidate, error)
	ListTriageResults(ctx context.Context, filter TriageListFilter) ([]TriageResult, error)
	UpsertTriageResult(ctx context.Context, result TriageResult) (TriageResult, error)
	GetTriageResult(ctx context.Context, orgID, candidateID string) (TriageResult, error)
	ReplaceProjectLinks(ctx context.Context, orgID, candidateID string, links []CandidateProjectLink) error
	ListProjectLinks(ctx context.Context, filter CandidateProjectLinksFilter) ([]CandidateProjectLink, error)
	UpdatePromotedHotMemoryIDs(ctx context.Context, orgID, candidateID string, ids []string) error
	UpdateProjectLinkPromotion(ctx context.Context, orgID, candidateID, linkedProjectID, memoryID string) error
}

// InMemoryTriageRepository 为内存测试提供 triage 持久化实现。
type InMemoryTriageRepository struct {
	mu            sync.Mutex
	candidateRepo *InMemoryRepository
	results       map[string]TriageResult
	links         map[string][]CandidateProjectLink
}

func NewInMemoryTriageRepository(candidateRepo *InMemoryRepository) *InMemoryTriageRepository {
	return &InMemoryTriageRepository{
		candidateRepo: candidateRepo,
		results:       map[string]TriageResult{},
		links:         map[string][]CandidateProjectLink{},
	}
}

var _ TriageRepository = (*InMemoryTriageRepository)(nil)

func triageResultKey(orgID, candidateID string) string {
	return orgID + "/" + candidateID
}

func triageLinksKey(orgID, candidateID string) string {
	return orgID + "/" + candidateID
}

func (r *InMemoryTriageRepository) ListCandidatesNeedingTriage(ctx context.Context, filter TriageScanFilter) ([]Candidate, error) {
	if strings.TrimSpace(filter.OrgID) == "" {
		return nil, errors.New("org_id is required")
	}
	filter.Limit = triageClampLimit(filter.Limit)

	candidates, err := r.candidateRepo.ListCandidates(ctx, ListFilter{
		OrgID: filter.OrgID,
		Limit: filter.Limit,
	})
	if err != nil {
		return nil, err
	}

	out := []Candidate{}
	for _, candidate := range candidates {
		if candidate.Status == StatusDiscarded {
			continue
		}
		if _, exists := r.results[triageResultKey(candidate.OrgID, candidate.CandidateID)]; exists {
			continue
		}
		if filter.MinConfidence > 0 && candidate.Confidence < filter.MinConfidence {
			continue
		}
		out = append(out, candidate)
	}
	if len(out) <= filter.Limit {
		return out, nil
	}
	return out[:filter.Limit], nil
}

func (r *InMemoryTriageRepository) ListTriageResults(ctx context.Context, filter TriageListFilter) ([]TriageResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []TriageResult{}
	for _, result := range r.results {
		if filter.OrgID != "" && result.OrgID != filter.OrgID {
			continue
		}
		if filter.SourceProjectID != "" && result.SourceProjectID != filter.SourceProjectID {
			continue
		}
		if filter.SourceKey != "" && result.SourceKey != filter.SourceKey {
			continue
		}
		if filter.ReviewState != "" && result.ReviewState != filter.ReviewState {
			continue
		}
		out = append(out, cloneTriageResult(result))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].CandidateID < out[j].CandidateID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.Offset > 0 && filter.Offset < len(out) {
		out = out[filter.Offset:]
	} else if filter.Offset >= len(out) {
		return []TriageResult{}, nil
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:triageClampLimit(filter.Limit)]
	}
	return out, nil
}

func (r *InMemoryTriageRepository) UpsertTriageResult(ctx context.Context, result TriageResult) (TriageResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(result.OrgID) == "" || strings.TrimSpace(result.CandidateID) == "" {
		return TriageResult{}, errors.New("org_id and candidate_id are required")
	}
	if !result.TriageScope.Valid() {
		return TriageResult{}, errors.New("invalid triage scope")
	}
	if !result.ReviewState.Valid() {
		result.ReviewState = TriageReviewWeak
	}
	now := time.Now().UTC()
	key := triageResultKey(result.OrgID, result.CandidateID)
	if existing, ok := r.results[key]; ok {
		existing.OrgID = result.OrgID
		existing.CandidateID = result.CandidateID
		existing.SourceProjectID = result.SourceProjectID
		existing.SourceKey = result.SourceKey
		existing.TriageScope = result.TriageScope
		existing.Confidence = result.Confidence
		existing.ReviewState = normalizeReviewState(result.ReviewState)
		existing.Reason = result.Reason
		existing.SourceRefs = append([]TriageSourceRef(nil), result.SourceRefs...)
		existing.PromotedHotMemoryIDs = append([]string(nil), result.PromotedHotMemoryIDs...)
		existing.Attempts = result.Attempts
		existing.LastError = result.LastError
		existing.UpdatedAt = now
		r.results[key] = existing
		return existing, nil
	}

	if result.CreatedAt.IsZero() {
		result.CreatedAt = now
	}
	result.UpdatedAt = now
	if !result.ReviewState.Valid() {
		result.ReviewState = TriageReviewWeak
	}
	result.SourceRefs = append([]TriageSourceRef(nil), result.SourceRefs...)
	result.PromotedHotMemoryIDs = append([]string(nil), result.PromotedHotMemoryIDs...)
	r.results[key] = result
	return result, nil
}

func (r *InMemoryTriageRepository) GetTriageResult(ctx context.Context, orgID, candidateID string) (TriageResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result, ok := r.results[triageResultKey(orgID, candidateID)]
	if !ok {
		return TriageResult{}, ErrNotFound
	}
	return cloneTriageResult(result), nil
}

func (r *InMemoryTriageRepository) ReplaceProjectLinks(ctx context.Context, orgID, candidateID string, links []CandidateProjectLink) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := triageLinksKey(orgID, candidateID)
	if len(links) == 0 {
		r.links[key] = nil
		return nil
	}
	out := make([]CandidateProjectLink, 0, len(links))
	for _, link := range links {
		if strings.TrimSpace(link.LinkedProjectID) == "" {
			continue
		}
		link.OrgID = orgID
		link.CandidateID = candidateID
		if link.CreatedAt.IsZero() {
			link.CreatedAt = time.Now().UTC()
		}
		link.UpdatedAt = link.CreatedAt
		out = append(out, link)
	}
	if len(out) == 0 {
		r.links[key] = nil
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence == out[j].Confidence {
			return out[i].LinkedProjectID < out[j].LinkedProjectID
		}
		return out[i].Confidence > out[j].Confidence
	})
	r.links[key] = out
	return nil
}

func (r *InMemoryTriageRepository) ListProjectLinks(ctx context.Context, filter CandidateProjectLinksFilter) ([]CandidateProjectLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []CandidateProjectLink{}
	for _, links := range r.links {
		for _, link := range links {
			if filter.OrgID != "" && filter.OrgID != link.OrgID {
				continue
			}
			if filter.CandidateID != "" && filter.CandidateID != link.CandidateID {
				continue
			}
			if filter.LinkedProjectID != "" && filter.LinkedProjectID != link.LinkedProjectID {
				continue
			}
			if filter.Status != "" && filter.Status != link.Status {
				continue
			}
			if filter.MinConfidence > 0 && link.Confidence < filter.MinConfidence {
				continue
			}
			out = append(out, cloneCandidateProjectLink(link))
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence == out[j].Confidence {
			if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
				return out[i].LinkedProjectID < out[j].LinkedProjectID
			}
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Confidence > out[j].Confidence
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (r *InMemoryTriageRepository) UpdatePromotedHotMemoryIDs(ctx context.Context, orgID, candidateID string, ids []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := triageResultKey(orgID, candidateID)
	result, ok := r.results[key]
	if !ok {
		return ErrNotFound
	}
	result.PromotedHotMemoryIDs = append([]string(nil), ids...)
	result.UpdatedAt = time.Now().UTC()
	r.results[key] = result
	return nil
}

func (r *InMemoryTriageRepository) UpdateProjectLinkPromotion(ctx context.Context, orgID, candidateID, linkedProjectID, memoryID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := triageLinksKey(orgID, candidateID)
	links := r.links[key]
	updated := false
	for i := range links {
		if links[i].LinkedProjectID == linkedProjectID {
			links[i].PromotedHotMemoryID = memoryID
			links[i].UpdatedAt = time.Now().UTC()
			updated = true
		}
	}
	if !updated {
		return nil
	}
	r.links[key] = links
	return nil
}

func cloneCandidateProjectLink(link CandidateProjectLink) CandidateProjectLink {
	return link
}

func cloneTriageResult(result TriageResult) TriageResult {
	result.SourceRefs = append([]TriageSourceRef(nil), result.SourceRefs...)
	result.PromotedHotMemoryIDs = append([]string(nil), result.PromotedHotMemoryIDs...)
	return result
}

func triageClampLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}
