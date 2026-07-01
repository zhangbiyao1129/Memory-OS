package audit

import (
	"strings"
	"testing"
)

func TestRecordDoesNotAllowSecretPlaintext(t *testing.T) {
	service := NewService(NewMemoryRepository())

	err := service.Record(Log{
		ActorUserID:  "user_1",
		Action:       "secret.inject",
		ResourceType: "secret",
		ResourceID:   "secret_ref_1",
		RequestID:    "req_1",
		Result:       "ok",
		Metadata:     map[string]string{"tool": "deploy", "value": "fake-secret-value"},
	})

	if err == nil {
		t.Fatal("Record() error = nil, want plaintext secret rejection")
	}
}

func TestRecordStoresAuditMetadata(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo)

	err := service.Record(Log{
		ActorUserID:  "user_1",
		Action:       "secret.inject",
		ResourceType: "secret",
		ResourceID:   "secret_ref_1",
		RequestID:    "req_1",
		Result:       "ok",
		Metadata:     map[string]string{"tool": "deploy", "purpose": "integration-test"},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	logs := repo.All()
	if len(logs) != 1 {
		t.Fatalf("logs len = %d, want 1", len(logs))
	}
	if strings.Contains(logs[0].Metadata["tool"], "fake-secret-value") {
		t.Fatal("audit metadata leaked secret")
	}
}
