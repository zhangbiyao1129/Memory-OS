package rag

import (
	"memory-os/internal/archive"
	"memory-os/internal/qdrant"
)

type ChunkPayload struct {
	ChunkID          string
	ArchiveID        string
	OrgID            string
	ProjectID        string
	UserID           string
	Visibility       string
	PermissionLabels []string
	IndexGeneration  int
	Content          string
	ContentHash      string
	SourceEventIDs   []string
}

type IndexRequest struct {
	OrgID            string
	ProjectID        string
	UserID           string
	Visibility       string
	PermissionLabels []string
	Chunks           []archive.Chunk
}

type SearchRequest struct {
	Query  string
	Filter qdrant.PayloadFilter
	Limit  int
}

type SearchResult struct {
	Text   string
	Score  float64
	Source SourceRef
}

type SourceRef struct {
	ArchiveID      string
	ChunkID        string
	SourceEventIDs []string
}
