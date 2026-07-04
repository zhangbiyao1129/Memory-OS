package eventlog

import (
	"testing"
)

func TestMemoryRepositoryDedupesEventID(t *testing.T) {
	repo := NewMemoryRepository()
	event := validEvent()
	sanitized, err := Sanitize(event, SanitizerOptions{})
	if err != nil {
		t.Fatalf("Sanitize() error = %v", err)
	}

	result, err := repo.Save(sanitized, "request_1")
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if result.Deduped {
		t.Fatal("first save deduped = true, want false")
	}

	result, err = repo.Save(sanitized, "request_2")
	if err != nil {
		t.Fatalf("Save() duplicate error = %v", err)
	}
	if !result.Deduped {
		t.Fatal("duplicate save deduped = false, want true")
	}
	if repo.Count() != 1 {
		t.Fatalf("repo count = %d, want 1", repo.Count())
	}
}

func TestMemoryRepositoryDedupesRequestID(t *testing.T) {
	repo := NewMemoryRepository()
	event := validEvent()
	sanitized, err := Sanitize(event, SanitizerOptions{})
	if err != nil {
		t.Fatalf("Sanitize() error = %v", err)
	}

	if _, err := repo.Save(sanitized, "request_1"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	sanitized.Event.EventID = "event_2"

	result, err := repo.Save(sanitized, "request_1")

	if err != nil {
		t.Fatalf("Save() duplicate request error = %v", err)
	}
	if !result.Deduped {
		t.Fatal("duplicate request deduped = false, want true")
	}
	if repo.Count() != 1 {
		t.Fatalf("repo count = %d, want 1", repo.Count())
	}
}
