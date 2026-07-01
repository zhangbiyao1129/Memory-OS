package retrieval

import "sync"

type AccessLog interface {
	LogRequest(SearchRequest, bool) error
	LogResult(string, int, SearchResult) error
}

type MemoryAccessLog struct {
	mu       sync.Mutex
	requests []SearchRequest
	results  []SearchResult
}

func NewMemoryAccessLog() *MemoryAccessLog {
	return &MemoryAccessLog{}
}

func (l *MemoryAccessLog) LogRequest(request SearchRequest, rerankDegraded bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, request)
	return nil
}

func (l *MemoryAccessLog) LogResult(requestID string, rank int, result SearchResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.results = append(l.results, result)
	return nil
}

func (l *MemoryAccessLog) Requests() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.requests)
}

func (l *MemoryAccessLog) Results() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.results)
}
