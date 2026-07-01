package rag

import (
	"errors"
	"fmt"
	"strings"
)

type Service struct {
	store Store
}

func NewService(store Store) Service {
	return Service{store: store}
}

func (s Service) Index(request IndexRequest) error {
	if request.OrgID == "" || request.ProjectID == "" || request.UserID == "" || request.Visibility == "" || len(request.PermissionLabels) == 0 {
		return errors.New("index scope and permissions are required")
	}
	for _, item := range request.Chunks {
		if err := s.store.Upsert(payloadFromArchiveChunk(request, item)); err != nil {
			return err
		}
	}
	return nil
}

func (s Service) Search(request SearchRequest) ([]SearchResult, error) {
	if request.Query == "" {
		return nil, errors.New("query is required")
	}
	if len(request.Filter.Must) == 0 {
		return nil, errors.New("query-time qdrant filter is required")
	}
	candidates := s.store.Filtered(request.Filter.Must)
	results := []SearchResult{}
	for _, candidate := range candidates {
		if strings.Contains(strings.ToLower(candidate.Content), strings.ToLower(request.Query)) {
			results = append(results, SearchResult{Text: candidate.Content, Score: 1, Source: SourceRef{ArchiveID: candidate.ArchiveID, ChunkID: candidate.ChunkID, SourceEventIDs: candidate.SourceEventIDs}})
		}
	}
	return results, nil
}

func matchesFilter(chunk ChunkPayload, filter map[string][]string) bool {
	for key, allowed := range filter {
		if key == "permission_labels" {
			if !hasAny(chunk.PermissionLabels, allowed) {
				return false
			}
			continue
		}
		value := payloadValue(chunk, key)
		if value == "" {
			return false
		}
		if !contains(allowed, value) {
			return false
		}
	}
	return true
}

func payloadValue(chunk ChunkPayload, key string) string {
	switch key {
	case "doc_type":
		return "archive_chunk"
	case "org_id":
		return chunk.OrgID
	case "project_id":
		return chunk.ProjectID
	case "user_id":
		return chunk.UserID
	case "visibility":
		return chunk.Visibility
	case "index_generation":
		return fmt.Sprintf("%d", chunk.IndexGeneration)
	default:
		return ""
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasAny(values []string, targets []string) bool {
	for _, target := range targets {
		if contains(values, target) {
			return true
		}
	}
	return false
}
