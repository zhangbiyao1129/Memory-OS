package jobs

import (
	"context"
	"strings"
	"testing"
	"time"

	"memory-os/internal/archive"
	"memory-os/internal/eventlog"
)

func TestArchiveWorkerDedupesJob(t *testing.T) {
	service := archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	worker := NewArchiveWorker(service)
	job := ArchiveJob{
		RequestID: "job_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Now().UTC(),
		Events: []eventlog.TurnEvent{{
			Version: "v1", EventID: "event_1", TurnID: "turn_1", ThreadID: "thread_1", SessionID: "session_1",
			Type: eventlog.EventUserMessage, CreatedAt: time.Now().UTC(),
			Actor:   eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
			Payload: map[string]any{"text": "deploy api"},
		}},
	}

	first, err := worker.Handle(job)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	second, err := worker.Handle(job)
	if err != nil {
		t.Fatalf("Handle() duplicate error = %v", err)
	}
	if first.Deduped {
		t.Fatal("first result deduped = true, want false")
	}
	if !second.Deduped {
		t.Fatal("second result deduped = false, want true")
	}
}

func TestArchiveWorkerEnqueuesRAGIndexJobAfterCreate(t *testing.T) {
	service := archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	queue := &fakeRAGIndexQueue{}
	worker := NewArchiveWorkerWithIndexQueue(service, queue)
	job := ArchiveJob{
		RequestID: "job_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Now().UTC(),
		Events: []eventlog.TurnEvent{{
			Version: "v1", EventID: "event_1", TurnID: "turn_1", ThreadID: "thread_1", SessionID: "session_1",
			Type: eventlog.EventUserMessage, CreatedAt: time.Now().UTC(),
			Actor:   eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
			Payload: map[string]any{"text": "deploy api"},
		}},
	}

	result, err := worker.Handle(job)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Deduped {
		t.Fatal("Handle() deduped = true, want false")
	}
	if len(queue.jobs) != 1 {
		t.Fatalf("rag index jobs = %d, want 1", len(queue.jobs))
	}
	indexJob := queue.jobs[0]
	if indexJob.IdempotencyKey != "rag_archive_1_g1" || indexJob.OrgID != "org_1" || indexJob.ProjectID != "project_1" || indexJob.UserID != "user_1" {
		t.Fatalf("rag index job scope mismatch: %#v", indexJob)
	}
	if len(indexJob.Chunks) == 0 || indexJob.Chunks[0].ArchiveID != "archive_1" || indexJob.Chunks[0].IndexGeneration != 1 {
		t.Fatalf("rag index chunks mismatch: %#v", indexJob.Chunks)
	}
	if indexJob.Chunks[0].Content == "" || !strings.Contains(indexJob.Chunks[0].Content, "## 结论") {
		t.Fatalf("rag index chunk should contain knowledge markdown: %#v", indexJob.Chunks[0])
	}

	if _, err := worker.Handle(job); err != nil {
		t.Fatalf("Handle() duplicate error = %v", err)
	}
	if len(queue.jobs) != 1 {
		t.Fatalf("rag index jobs after duplicate = %d, want 1", len(queue.jobs))
	}
}

type fakeRAGIndexQueue struct {
	jobs []RAGIndexJob
}

func (q *fakeRAGIndexQueue) Enqueue(ctx context.Context, job RAGIndexJob) error {
	q.jobs = append(q.jobs, job)
	return nil
}
