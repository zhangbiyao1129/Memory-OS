package jobs

import (
	"context"
	"os"
	"strconv"
	"time"

	"memory-os/internal/archive"
	"memory-os/internal/eventlog"
)

type ArchiveJob struct {
	RequestID string
	ArchiveID string
	Title     string
	UserID    string
	OrgID     string
	ProjectID string
	CreatedAt time.Time
	Events    []eventlog.TurnEvent
}

type ArchiveWorker struct {
	service       archive.Service
	indexEnqueuer ragIndexEnqueuer
	handle        func(ArchiveJob) (archive.Result, error)
}

type ragIndexEnqueuer interface {
	Enqueue(ctx context.Context, job RAGIndexJob) error
}

func NewArchiveWorker(service archive.Service) ArchiveWorker {
	return ArchiveWorker{service: service}
}

func NewArchiveWorkerWithIndexQueue(service archive.Service, queue ragIndexEnqueuer) ArchiveWorker {
	return ArchiveWorker{service: service, indexEnqueuer: queue}
}

func (w ArchiveWorker) Handle(job ArchiveJob) (archive.Result, error) {
	if w.handle != nil {
		return w.handle(job)
	}
	result, err := w.service.Create(archive.CreateRequest{
		RequestID: job.RequestID,
		ArchiveID: job.ArchiveID,
		Title:     job.Title,
		UserID:    job.UserID,
		OrgID:     job.OrgID,
		ProjectID: job.ProjectID,
		CreatedAt: job.CreatedAt,
		Events:    job.Events,
	})
	if err != nil || result.Deduped || w.indexEnqueuer == nil {
		return result, err
	}
	return result, w.enqueueRAGIndex(job, result.Metadata)
}

func (w ArchiveWorker) enqueueRAGIndex(job ArchiveJob, metadata archive.Metadata) error {
	content, err := os.ReadFile(metadata.FilePath)
	if err != nil {
		return err
	}
	chunks, err := archive.ChunkMarkdown(archive.ChunkRequest{ArchiveID: metadata.ArchiveID, IndexGeneration: metadata.IndexGeneration, Content: string(content)})
	if err != nil {
		return err
	}
	return w.indexEnqueuer.Enqueue(context.Background(), RAGIndexJob{
		IdempotencyKey:   ragIndexIdempotencyKey(metadata.ArchiveID, metadata.IndexGeneration),
		OrgID:            metadata.OrgID,
		ProjectID:        metadata.ProjectID,
		UserID:           metadata.UserID,
		Visibility:       "project",
		PermissionLabels: []string{"project:" + metadata.ProjectID + ":read"},
		Chunks:           chunks,
	})
}

func ragIndexIdempotencyKey(archiveID string, generation int) string {
	return "rag_" + archiveID + "_g" + strconv.Itoa(generation)
}
