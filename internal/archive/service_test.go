package archive

import (
	"os"
	"strings"
	"testing"
	"time"

	"memory-os/internal/eventlog"
)

func TestServiceCreateArchiveWritesFileAndMetadata(t *testing.T) {
	root := t.TempDir()
	service := NewService(NewMemoryRepository(), root)

	result, err := service.Create(CreateRequest{
		RequestID: "request_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Events:    []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Metadata.CurrentVersion != 1 || result.Metadata.IndexGeneration != 1 {
		t.Fatalf("metadata version/index = %d/%d, want 1/1", result.Metadata.CurrentVersion, result.Metadata.IndexGeneration)
	}
	content, err := os.ReadFile(result.Metadata.FilePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(content), "Deploy Notes") {
		t.Fatalf("archive content missing title: %s", content)
	}
}

func TestServiceCreateDedupesRequestID(t *testing.T) {
	service := NewService(NewMemoryRepository(), t.TempDir())
	request := CreateRequest{
		RequestID: "request_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Now().UTC(),
		Events:    []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")},
	}
	first, err := service.Create(request)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	second, err := service.Create(request)
	if err != nil {
		t.Fatalf("Create() duplicate error = %v", err)
	}
	if !second.Deduped {
		t.Fatal("duplicate create deduped = false, want true")
	}
	if first.Metadata.ArchiveID != second.Metadata.ArchiveID {
		t.Fatal("duplicate create returned different archive")
	}
}

func TestServiceEditArchiveIncrementsVersionAndIndexGeneration(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, t.TempDir())
	created, err := service.Create(CreateRequest{
		RequestID: "request_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Now().UTC(),
		Events:    []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	edited, err := service.Edit(EditRequest{
		RequestID:   "edit_1",
		ArchiveID:   created.Metadata.ArchiveID,
		ActorUserID: "user_1",
		Reason:      "manual correction",
		Content:     "# Edited\n\nnew content",
	})
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}

	if edited.Metadata.CurrentVersion != 2 {
		t.Fatalf("version = %d, want 2", edited.Metadata.CurrentVersion)
	}
	if edited.Metadata.IndexGeneration != 2 {
		t.Fatalf("index_generation = %d, want 2", edited.Metadata.IndexGeneration)
	}
	if len(repo.Versions(created.Metadata.ArchiveID)) != 2 {
		t.Fatalf("versions len = %d, want 2", len(repo.Versions(created.Metadata.ArchiveID)))
	}
	if len(repo.AuditLogs(created.Metadata.ArchiveID)) != 1 {
		t.Fatalf("audit logs len = %d, want 1", len(repo.AuditLogs(created.Metadata.ArchiveID)))
	}
}

func archiveEvent(eventID, text string) eventlog.TurnEvent {
	return eventlog.TurnEvent{
		Version:   "v1",
		EventID:   eventID,
		TurnID:    "turn_1",
		ThreadID:  "thread_1",
		SessionID: "session_1",
		Type:      eventlog.EventUserMessage,
		CreatedAt: time.Now().UTC(),
		Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Payload:   map[string]any{"text": text},
	}
}
