package rag

import (
	"context"
	"errors"
	"sync"

	"memory-os/internal/archive"
)

type Store interface {
	Upsert(payload ChunkPayload) error
	Filtered(filter map[string][]string) []ChunkPayload
}

type SearchStore interface {
	Search(ctx context.Context, request SearchRequest) ([]SearchResult, error)
}

type MemoryStore struct {
	mu     sync.Mutex
	chunks map[string]ChunkPayload
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{chunks: map[string]ChunkPayload{}}
}

func (s *MemoryStore) Upsert(payload ChunkPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if payload.ChunkID == "" {
		return errors.New("chunk id is required")
	}
	s.chunks[payload.ChunkID] = payload
	return nil
}

func (s *MemoryStore) Filtered(filter map[string][]string) []ChunkPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	results := []ChunkPayload{}
	for _, chunk := range s.chunks {
		if matchesFilter(chunk, filter) {
			results = append(results, chunk)
		}
	}
	return results
}

func payloadFromArchiveChunk(scope IndexRequest, chunk archive.Chunk) ChunkPayload {
	return ChunkPayload{ChunkID: chunk.ChunkID, ArchiveID: chunk.ArchiveID, OrgID: scope.OrgID, ProjectID: scope.ProjectID, UserID: scope.UserID, Visibility: scope.Visibility, PermissionLabels: append([]string(nil), scope.PermissionLabels...), IndexGeneration: chunk.IndexGeneration, Content: chunk.Content, ContentHash: chunk.ContentHash, SourceEventIDs: append([]string(nil), chunk.SourceEventIDs...)}
}
