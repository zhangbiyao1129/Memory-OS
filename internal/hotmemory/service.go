package hotmemory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"memory-os/internal/secret"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) Service {
	return Service{repository: repository}
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
	return s.repository.Upsert(memory)
}

func (s Service) Search(request SearchRequest) ([]SearchResult, error) {
	if strings.TrimSpace(request.Query) == "" {
		return nil, errors.New("query is required")
	}
	if len(request.Filter.Must) == 0 {
		return nil, errors.New("query-time hot memory filter is required")
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

func (s Service) MarkUsed(memoryID string) (Memory, error) {
	memory, err := s.repository.Get(memoryID)
	if err != nil {
		return Memory{}, err
	}
	memory.AccessCount++
	memory.UsedCount++
	return s.repository.Update(memory)
}

func (s Service) Delete(memoryID string) error {
	memory, err := s.repository.Get(memoryID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	memory.Status = StatusDeleted
	memory.DeletedAt = &now
	_, err = s.repository.Update(memory)
	return err
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
	base := memory.Confidence * 10
	base += float64(memory.AccessCount)
	base += float64(memory.UsedCount * 2)
	switch memory.Status {
	case StatusPromoted:
		base += 10
	case StatusDemoted:
		base -= 10
	}
	return base
}

func dedupeKey(memory Memory) string {
	return strings.Join([]string{memory.OrgID, memory.ProjectID, memory.UserID, memory.AgentID, string(memory.Scope), memory.FactHash}, "|")
}

func normalizeFact(fact string) string {
	return strings.Join(strings.Fields(strings.ToLower(fact)), " ")
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
