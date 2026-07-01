package jobs

import (
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
