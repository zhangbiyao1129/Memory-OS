package retrieval

import "memory-os/internal/hotmemory"

type Actor struct {
	UserID    string
	OrgID     string
	ProjectID string
	AgentID   string
}

type SearchRequest struct {
	RequestID              string
	Query                  string
	Actor                  Actor
	Scope                  hotmemory.Scope
	Visibility             string
	PermissionLabels       []string
	ArchiveIndexGeneration int
	MaxContextBytes        int
}

type ArchiveGenerationContext struct {
	UserID    string
	OrgID     string
	ProjectID string
}

type SearchResponse struct {
	RequestID       string         `json:"request_id"`
	Context         string         `json:"context"`
	Results         []SearchResult `json:"results"`
	RerankDegraded  bool           `json:"rerank_degraded"`
	AccessLogCount  int            `json:"access_log_count"`
	MarkedUsedCount int            `json:"marked_used_count"`
}

type SearchResult struct {
	Text   string    `json:"text"`
	Score  float64   `json:"score"`
	Source SourceRef `json:"source"`
}

type SourceKind string

const (
	SourceHotMemory    SourceKind = "hot_memory"
	SourceArchiveChunk SourceKind = "archive_chunk"
)

type SourceRef struct {
	Kind           SourceKind `json:"kind"`
	MemoryID       string     `json:"memory_id,omitempty"`
	ArchiveID      string     `json:"archive_id,omitempty"`
	ChunkID        string     `json:"chunk_id,omitempty"`
	SourceEventIDs []string   `json:"source_event_ids,omitempty"`
	Scope          string     `json:"scope,omitempty"`
	ProjectID      string     `json:"project_id,omitempty"`
}

type candidate struct {
	id     string
	text   string
	score  float64
	source SourceRef
}
