package jobs

import (
	"errors"
	"sync"

	"memory-os/internal/archive"
	"memory-os/internal/rag"
)

type RAGIndexJob struct {
	IdempotencyKey   string
	OrgID            string
	ProjectID        string
	UserID           string
	Visibility       string
	PermissionLabels []string
	Chunks           []archive.Chunk
}

type RAGIndexResult struct {
	Deduped bool
}

type RAGIndexWorker struct {
	mu      sync.Mutex
	seen    map[string]bool
	service rag.Service
	handle  func(RAGIndexJob) (RAGIndexResult, error)
}

func NewRAGIndexWorker(service rag.Service) *RAGIndexWorker {
	return &RAGIndexWorker{service: service, seen: map[string]bool{}}
}

func (w *RAGIndexWorker) Handle(job RAGIndexJob) (RAGIndexResult, error) {
	if w.handle != nil {
		return w.handle(job)
	}
	if job.IdempotencyKey == "" {
		return RAGIndexResult{}, errors.New("rag index job idempotency key is required")
	}
	w.mu.Lock()
	if w.seen[job.IdempotencyKey] {
		w.mu.Unlock()
		return RAGIndexResult{Deduped: true}, nil
	}
	w.seen[job.IdempotencyKey] = true
	w.mu.Unlock()

	if err := w.service.Index(rag.IndexRequest{OrgID: job.OrgID, ProjectID: job.ProjectID, UserID: job.UserID, Visibility: job.Visibility, PermissionLabels: job.PermissionLabels, Chunks: job.Chunks}); err != nil {
		return RAGIndexResult{}, err
	}
	return RAGIndexResult{Deduped: false}, nil
}
