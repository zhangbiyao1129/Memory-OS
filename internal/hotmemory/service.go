package hotmemory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"memory-os/internal/secret"
)

type Service struct {
	repository  Repository
	vectorIndex VectorIndex
	organizer   Organizer
}

func NewService(repository Repository) Service {
	return Service{repository: repository}
}

func NewServiceWithVectorIndex(repository Repository, vectorIndex VectorIndex) Service {
	return Service{repository: repository, vectorIndex: vectorIndex}
}

func (s Service) WithOrganizer(organizer Organizer) Service {
	s.organizer = organizer
	return s
}

func (s Service) Configured() bool {
	return s.repository != nil
}

func (s Service) Upsert(request UpsertRequest) (Memory, error) {
	if err := validateUpsert(request); err != nil {
		return Memory{}, err
	}
	sanitized := secret.Sanitize(request.Fact, func(index int, match string) string { return fmt.Sprintf("secret_ref_hot_memory_%d", index) })
	fact := strings.TrimSpace(sanitized.Text)
	factHash := hash(normalizeFact(fact))
	memory := Memory{
		MemoryID:         "hm_" + factHash[:16],
		OrgID:            request.OrgID,
		ProjectID:        request.ProjectID,
		UserID:           request.UserID,
		AgentID:          request.AgentID,
		Scope:            request.Scope,
		Visibility:       request.Visibility,
		PermissionLabels: append([]string(nil), request.PermissionLabels...),
		Fact:             fact,
		FactHash:         factHash,
		Sources:          []Source{{SourceType: request.SourceType, SourceRef: request.SourceRef, Confidence: request.Confidence}},
		Confidence:       request.Confidence,
		Status:           StatusActive,
	}
	memory.HotScore = score(memory)
	saved, err := s.repository.Upsert(memory)
	if err != nil {
		return Memory{}, err
	}
	if s.vectorIndex != nil {
		if err := s.vectorIndex.Index(saved); err != nil {
			return Memory{}, err
		}
	}
	return saved, nil
}

func (s Service) Get(memoryID string) (Memory, error) {
	if !s.Configured() {
		return Memory{}, errors.New("hot memory service is not configured")
	}
	return s.repository.Get(memoryID)
}

func (s Service) List(filter map[string][]string) ([]Memory, error) {
	if !s.Configured() {
		return nil, errors.New("hot memory service is not configured")
	}
	if len(filter) == 0 {
		return nil, errors.New("hot memory filter is required")
	}
	return s.repository.Search(filter), nil
}

func (s Service) Search(request SearchRequest) ([]SearchResult, error) {
	if strings.TrimSpace(request.Query) == "" {
		return nil, errors.New("query is required")
	}
	if len(request.Filter.Must) == 0 {
		return nil, errors.New("query-time hot memory filter is required")
	}
	if s.vectorIndex != nil {
		return s.vectorIndex.Search(request)
	}
	candidates := s.repository.Search(request.Filter.Must)
	results := []SearchResult{}
	query := strings.ToLower(request.Query)
	for _, candidate := range candidates {
		if strings.Contains(strings.ToLower(candidate.Fact), query) {
			results = append(results, SearchResult{Memory: candidate, Score: candidate.HotScore})
		}
	}
	return results, nil
}

func (s Service) Promote(memoryID string) (Memory, error) {
	memory, err := s.repository.Get(memoryID)
	if err != nil {
		return Memory{}, err
	}
	memory.Status = StatusPromoted
	return s.repository.Update(memory)
}

func (s Service) Demote(memoryID string) (Memory, error) {
	memory, err := s.repository.Get(memoryID)
	if err != nil {
		return Memory{}, err
	}
	memory.Status = StatusDemoted
	return s.repository.Update(memory)
}

func (s Service) MarkAccessed(memoryID string) (Memory, error) {
	return s.repository.IncrementUsageSignal(memoryID, SignalAccessed)
}

func (s Service) MarkReturned(memoryID string) (Memory, error) {
	return s.repository.IncrementUsageSignal(memoryID, SignalReturned)
}

func (s Service) MarkUsed(memoryID string) (Memory, error) {
	return s.repository.IncrementUsageSignal(memoryID, SignalUsed)
}

// SetPinned 是人工 override 入口：pin 后热度获得固定加成且不因降级淘汰，
// unpin 恢复常规热度计算。pin 是幂等设值，无并发自增语义。
func (s Service) SetPinned(memoryID string, pinned bool) (Memory, error) {
	memory, err := s.repository.Get(memoryID)
	if err != nil {
		return Memory{}, err
	}
	memory.Pinned = pinned
	return s.repository.Update(memory)
}

func (s Service) Edit(request EditRequest) (Memory, error) {
	if !s.Configured() {
		return Memory{}, errors.New("hot memory service is not configured")
	}
	if strings.TrimSpace(request.MemoryID) == "" {
		return Memory{}, errors.New("memory id is required")
	}
	sanitized := secret.Sanitize(request.Fact, func(index int, match string) string { return fmt.Sprintf("secret_ref_hot_memory_%d", index) })
	fact := strings.TrimSpace(sanitized.Text)
	if fact == "" {
		return Memory{}, errors.New("fact is required")
	}
	memory, err := s.repository.Get(request.MemoryID)
	if err != nil {
		return Memory{}, err
	}
	if memory.Status == StatusDeleted || memory.DeletedAt != nil {
		return Memory{}, errors.New("deleted memory cannot be edited")
	}
	memory.Fact = fact
	memory.FactHash = hash(normalizeFact(fact))
	if request.Confidence > 0 {
		memory.Confidence = request.Confidence
	}
	updated, err := s.repository.Update(memory)
	if err != nil {
		return Memory{}, err
	}
	if s.vectorIndex != nil {
		if err := s.vectorIndex.Index(updated); err != nil {
			return Memory{}, err
		}
	}
	return updated, nil
}

func (s Service) Delete(memoryID string) error {
	memory, err := s.repository.Get(memoryID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	memory.Status = StatusDeleted
	memory.DeletedAt = &now
	updated, err := s.repository.Update(memory)
	if err != nil {
		return err
	}
	if s.vectorIndex != nil {
		return s.vectorIndex.Delete(updated)
	}
	return nil
}

type Organizer interface {
	Organize(ctx context.Context, memories []Memory) (OrganizeDecision, error)
}

type OrganizeRequest struct {
	Filter map[string][]string
	Limit  int
}

type OrganizeDecision struct {
	DemoteIDs   []string
	KeepIDs     []string
	MergeGroups [][]string
	Summary     string
}

type OrganizeResult struct {
	Processed int
	Demoted   int
	Kept      int
	Summary   string
}

func (s Service) Organize(ctx context.Context, request OrganizeRequest) (OrganizeResult, error) {
	if !s.Configured() {
		return OrganizeResult{}, errors.New("hot memory service is not configured")
	}
	if s.organizer == nil {
		return OrganizeResult{}, errors.New("hot memory organizer is not configured")
	}
	if len(request.Filter) == 0 {
		return OrganizeResult{}, errors.New("hot memory filter is required")
	}
	memories := s.repository.Search(request.Filter)
	candidates := organizeCandidates(memories, request.Limit)
	if len(candidates) == 0 {
		return OrganizeResult{Summary: "没有需要整理的热记忆。"}, nil
	}
	decision, err := s.organizer.Organize(ctx, candidates)
	if err != nil {
		return OrganizeResult{}, err
	}
	candidateByID := make(map[string]Memory, len(candidates))
	for _, memory := range candidates {
		candidateByID[memory.MemoryID] = memory
	}
	demoteSet := make(map[string]bool, len(decision.DemoteIDs))
	for _, id := range decision.DemoteIDs {
		if _, ok := candidateByID[id]; !ok {
			return OrganizeResult{}, fmt.Errorf("memory_id %s not found in organize candidates", id)
		}
		demoteSet[id] = true
	}
	demoted := 0
	for id := range demoteSet {
		memory := candidateByID[id]
		memory.Status = StatusDemoted
		if _, err := s.repository.Update(memory); err != nil {
			return OrganizeResult{}, err
		}
		demoted++
	}
	return OrganizeResult{
		Processed: len(candidates),
		Demoted:   demoted,
		Kept:      len(candidates) - demoted,
		Summary:   decision.Summary,
	}, nil
}

func organizeCandidates(memories []Memory, limit int) []Memory {
	candidates := make([]Memory, 0, len(memories))
	for _, memory := range memories {
		if memory.Status != StatusActive || memory.Pinned {
			continue
		}
		candidates = append(candidates, memory)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].UsedCount != candidates[j].UsedCount {
			return candidates[i].UsedCount < candidates[j].UsedCount
		}
		if candidates[i].ReturnedCount != candidates[j].ReturnedCount {
			return candidates[i].ReturnedCount < candidates[j].ReturnedCount
		}
		if candidates[i].AccessCount != candidates[j].AccessCount {
			return candidates[i].AccessCount < candidates[j].AccessCount
		}
		return candidates[i].UpdatedAt.Before(candidates[j].UpdatedAt)
	})
	if limit > 0 && len(candidates) > limit {
		return candidates[:limit]
	}
	return candidates
}

func validateUpsert(request UpsertRequest) error {
	if request.OrgID == "" || request.ProjectID == "" || request.UserID == "" || request.AgentID == "" {
		return errors.New("scope ids are required")
	}
	if request.Scope == "" || request.Visibility == "" || strings.TrimSpace(request.Fact) == "" {
		return errors.New("scope, visibility, and fact are required")
	}
	if request.Visibility != "private" && len(request.PermissionLabels) == 0 {
		return errors.New("permission labels are required")
	}
	if request.SourceType == "" || request.SourceRef == "" {
		return errors.New("source is required")
	}
	return nil
}

func score(memory Memory) float64 {
	base := 1.0
	base += float64(memory.AccessCount) * 0.5
	base += float64(memory.ReturnedCount) * 2
	base += float64(memory.UsedCount) * 5
	if memory.Pinned {
		base += 20
	}
	switch memory.Status {
	case StatusPromoted:
		base += 10
	case StatusDemoted:
		base -= 10
	}
	if bonus := recencyBonus(memory.LastAccessedAt, memory.LastReturnedAt, memory.LastUsedAt); bonus > 0 {
		base += bonus
	}
	return base
}

func recencyBonus(values ...time.Time) float64 {
	var latest time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if value.After(latest) {
			latest = value
		}
	}
	if latest.IsZero() {
		return 0
	}
	age := time.Since(latest)
	switch {
	case age <= time.Minute:
		return 3
	case age <= 10*time.Minute:
		return 2
	case age <= time.Hour:
		return 1
	default:
		return 0
	}
}

func dedupeKey(memory Memory) string {
	return strings.Join([]string{memory.OrgID, memory.ProjectID, memory.UserID, string(memory.Scope), memory.FactHash}, "|")
}

func normalizeFact(fact string) string {
	return strings.Join(strings.Fields(strings.ToLower(fact)), " ")
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
