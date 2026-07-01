package eventlog

import (
	"testing"

	"memory-os/internal/tenant"
)

func TestServiceIngestsValidEvent(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, SanitizerOptions{MaxTurnEventBytes: 256 * 1024, MaxToolOutputBytes: 64 * 1024})
	event := validEvent()

	result, err := service.Ingest(event, "request_1", permissionContextFor(event))

	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if result.EventID != event.EventID || result.Deduped {
		t.Fatalf("result = %#v, want event id and not deduped", result)
	}
	if repo.Count() != 1 {
		t.Fatalf("repo count = %d, want 1", repo.Count())
	}
}

func TestServiceDedupesDuplicateEvent(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, SanitizerOptions{})
	event := validEvent()
	permissions := permissionContextFor(event)
	if _, err := service.Ingest(event, "request_1", permissions); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	result, err := service.Ingest(event, "request_2", permissions)

	if err != nil {
		t.Fatalf("Ingest() duplicate error = %v", err)
	}
	if !result.Deduped {
		t.Fatal("duplicate result deduped = false, want true")
	}
}

func TestServiceRejectsPermissionMismatch(t *testing.T) {
	service := NewService(NewMemoryRepository(), SanitizerOptions{})
	event := validEvent()
	permissions := permissionContextFor(event)
	permissions.ProjectID = "project_other"

	_, err := service.Ingest(event, "request_1", permissions)

	if err == nil {
		t.Fatal("Ingest() error = nil, want permission mismatch")
	}
}

func TestServiceRejectsInvalidEvent(t *testing.T) {
	service := NewService(NewMemoryRepository(), SanitizerOptions{})
	event := validEvent()
	event.EventID = ""

	_, err := service.Ingest(event, "request_1", permissionContextFor(validEvent()))

	if err == nil {
		t.Fatal("Ingest() error = nil, want invalid event")
	}
}

func permissionContextFor(event TurnEvent) tenant.PermissionContext {
	return tenant.PermissionContext{
		UserID:           event.Actor.UserID,
		OrgID:            event.Actor.OrgID,
		ProjectID:        event.Actor.ProjectID,
		AgentID:          event.Actor.AgentID,
		PermissionLabels: []string{"project:" + event.Actor.ProjectID + ":write"},
	}
}
