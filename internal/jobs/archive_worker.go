package jobs

import (
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
	service archive.Service
}

func NewArchiveWorker(service archive.Service) ArchiveWorker {
	return ArchiveWorker{service: service}
}

func (w ArchiveWorker) Handle(job ArchiveJob) (archive.Result, error) {
	return w.service.Create(archive.CreateRequest{
		RequestID: job.RequestID,
		ArchiveID: job.ArchiveID,
		Title:     job.Title,
		UserID:    job.UserID,
		OrgID:     job.OrgID,
		ProjectID: job.ProjectID,
		CreatedAt: job.CreatedAt,
		Events:    job.Events,
	})
}
