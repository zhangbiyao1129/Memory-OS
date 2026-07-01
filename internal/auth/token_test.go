package auth

import (
	"strings"
	"testing"
	"time"
)

func TestIssueTokenReturnsPlainOnceAndStoresHash(t *testing.T) {
	token, record, err := IssueToken(TokenRequest{
		SubjectID: "user_1",
		Name:      "local dev",
		Scopes:    []string{"memory:read"},
		TTL:       time.Hour,
		Prefix:    "pat",
	})
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("plain token is empty")
	}
	if record.TokenHash == "" {
		t.Fatal("token hash is empty")
	}
	if strings.Contains(record.TokenHash, token) {
		t.Fatal("token hash contains plaintext token")
	}
	if record.TokenPrefix == "" || !strings.HasPrefix(token, record.TokenPrefix) {
		t.Fatalf("token prefix mismatch token=%q record=%q", token, record.TokenPrefix)
	}
}

func TestValidateTokenRejectsWrongToken(t *testing.T) {
	token, record, err := IssueToken(TokenRequest{
		SubjectID: "user_1",
		Name:      "local dev",
		Scopes:    []string{"memory:read"},
		TTL:       time.Hour,
		Prefix:    "pat",
	})
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}
	if err := ValidateToken(token, record, time.Now()); err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	err = ValidateToken(token+"wrong", record, time.Now())

	if err == nil {
		t.Fatal("ValidateToken() error = nil, want wrong token rejection")
	}
}

func TestValidateTokenRejectsExpiredAndRevoked(t *testing.T) {
	token, record, err := IssueToken(TokenRequest{
		SubjectID: "user_1",
		Name:      "local dev",
		Scopes:    []string{"memory:read"},
		TTL:       time.Hour,
		Prefix:    "pat",
	})
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}

	if err := ValidateToken(token, record, time.Now().Add(2*time.Hour)); err == nil {
		t.Fatal("ValidateToken() error = nil, want expired rejection")
	}

	revokedAt := time.Now()
	record.RevokedAt = &revokedAt
	if err := ValidateToken(token, record, time.Now()); err == nil {
		t.Fatal("ValidateToken() error = nil, want revoked rejection")
	}
}
