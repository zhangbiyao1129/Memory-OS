package auth

import (
	"testing"
	"time"
)

func TestPasswordLogin(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.SetPassword("user_1", "correct-password"); err != nil {
		t.Fatalf("SetPassword() error = %v", err)
	}

	session, err := service.LoginPassword("user_1", "correct-password")

	if err != nil {
		t.Fatalf("LoginPassword() error = %v", err)
	}
	if session.UserID != "user_1" {
		t.Fatalf("session user id = %q, want user_1", session.UserID)
	}
}

func TestPasswordLoginRejectsWrongPassword(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if err := service.SetPassword("user_1", "correct-password"); err != nil {
		t.Fatalf("SetPassword() error = %v", err)
	}

	_, err := service.LoginPassword("user_1", "wrong-password")

	if err == nil {
		t.Fatal("LoginPassword() error = nil, want wrong password rejection")
	}
}

func TestPATCreateValidateRevoke(t *testing.T) {
	service := NewService(NewMemoryRepository())

	plain, record, err := service.CreatePAT("user_1", "dev", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	if record.TokenHash == "" || plain == "" {
		t.Fatal("token or hash is empty")
	}
	if _, err := service.ValidatePAT(plain, time.Now()); err != nil {
		t.Fatalf("ValidatePAT() error = %v", err)
	}
	if err := service.RevokePAT(record.ID); err != nil {
		t.Fatalf("RevokePAT() error = %v", err)
	}
	if _, err := service.ValidatePAT(plain, time.Now()); err == nil {
		t.Fatal("ValidatePAT() error = nil, want revoked rejection")
	}
}

func TestAdapterTokenBinding(t *testing.T) {
	service := NewService(NewMemoryRepository())

	plain, record, err := service.CreateAdapterToken(AdapterTokenRequest{
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		AgentID:   "codex",
		Scopes:    []string{"turn_event:write"},
		TTL:       time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateAdapterToken() error = %v", err)
	}
	if record.ProjectID != "project_1" {
		t.Fatalf("project id = %q, want project_1", record.ProjectID)
	}
	if _, err := service.ValidateAdapterToken(plain, AdapterTokenBinding{OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"}, time.Now()); err != nil {
		t.Fatalf("ValidateAdapterToken() error = %v", err)
	}
	if _, err := service.ValidateAdapterToken(plain, AdapterTokenBinding{OrgID: "org_1", ProjectID: "project_2", AgentID: "codex"}, time.Now()); err == nil {
		t.Fatal("ValidateAdapterToken() error = nil, want project mismatch rejection")
	}
}
