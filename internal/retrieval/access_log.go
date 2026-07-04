package retrieval

import (
	"sync"
	"time"
)

type AccessLog interface {
	LogRequest(SearchRequest, bool) error
	LogResult(string, int, SearchResult) error
}

type MemoryAccessLog struct {
	mu       sync.Mutex
	requests []AccessLogRequestEntry
	results  []AccessLogResultEntry
}

type AccessLogListFilter struct {
	OrgID       string
	ProjectID   string
	ActorUserID string
	RequestID   string
	Limit       int
}

type AccessLogRequestEntry struct {
	RequestID      string
	ActorUserID    string
	OrgID          string
	ProjectID      string
	AgentID        string
	QueryHash      string
	RerankDegraded bool
	CreatedAt      time.Time
}

type AccessLogResultEntry struct {
	RequestID  string
	Rank       int
	Score      float64
	SourceKind string
	SourceRef  map[string]any
	CreatedAt  time.Time
}

type AccessLogReader interface {
	ListRequests(filter AccessLogListFilter) ([]AccessLogRequestEntry, error)
	ListResults(filter AccessLogListFilter) ([]AccessLogResultEntry, error)
}

func NewMemoryAccessLog() *MemoryAccessLog {
	return &MemoryAccessLog{}
}

func (l *MemoryAccessLog) LogRequest(request SearchRequest, rerankDegraded bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, AccessLogRequestEntry{
		RequestID:      request.RequestID,
		ActorUserID:    request.Actor.UserID,
		OrgID:          request.Actor.OrgID,
		ProjectID:      request.Actor.ProjectID,
		AgentID:        request.Actor.AgentID,
		QueryHash:      hashQuery(request.Query),
		RerankDegraded: rerankDegraded,
		CreatedAt:      time.Now(),
	})
	return nil
}

func (l *MemoryAccessLog) LogResult(requestID string, rank int, result SearchResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.results = append(l.results, AccessLogResultEntry{RequestID: requestID, Rank: rank, Score: result.Score, SourceKind: string(result.Source.Kind), SourceRef: sourceRefMap(result.Source), CreatedAt: time.Now()})
	return nil
}

func (l *MemoryAccessLog) Requests() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.requests)
}

func (l *MemoryAccessLog) ListRequests(filter AccessLogListFilter) ([]AccessLogRequestEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	items := []AccessLogRequestEntry{}
	for i := len(l.requests) - 1; i >= 0; i-- {
		request := l.requests[i]
		if filter.OrgID != "" && request.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && request.ProjectID != filter.ProjectID {
			continue
		}
		if filter.ActorUserID != "" && request.ActorUserID != filter.ActorUserID {
			continue
		}
		if filter.RequestID != "" && request.RequestID != filter.RequestID {
			continue
		}
		items = append(items, request)
		if len(items) >= filter.Limit {
			break
		}
	}
	return items, nil
}

func (l *MemoryAccessLog) ListResults(filter AccessLogListFilter) ([]AccessLogResultEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	allowedRequests := map[string]bool{}
	for _, request := range l.requests {
		if filter.OrgID != "" && request.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && request.ProjectID != filter.ProjectID {
			continue
		}
		if filter.ActorUserID != "" && request.ActorUserID != filter.ActorUserID {
			continue
		}
		if filter.RequestID != "" && request.RequestID != filter.RequestID {
			continue
		}
		allowedRequests[request.RequestID] = true
	}
	items := []AccessLogResultEntry{}
	for i := len(l.results) - 1; i >= 0; i-- {
		result := l.results[i]
		if !allowedRequests[result.RequestID] {
			continue
		}
		items = append(items, result)
		if len(items) >= filter.Limit {
			break
		}
	}
	return items, nil
}

func sourceRefMap(source SourceRef) map[string]any {
	return map[string]any{
		"kind":             source.Kind,
		"memory_id":        source.MemoryID,
		"archive_id":       source.ArchiveID,
		"chunk_id":         source.ChunkID,
		"source_event_ids": source.SourceEventIDs,
	}
}

func (l *MemoryAccessLog) Results() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.results)
}
