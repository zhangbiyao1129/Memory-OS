package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGRepositoryRequiresPool(t *testing.T) {
	repo := NewPGRepository(nil)

	if err := repo.SetPasswordHash("user_1", "hash"); err == nil {
		t.Fatal("SetPasswordHash() error = nil, want missing pool error")
	}
	if _, err := repo.GetPasswordHash("user_1"); err == nil {
		t.Fatal("GetPasswordHash() error = nil, want missing pool error")
	}
	if _, err := repo.GetPAT("pat_1"); err == nil {
		t.Fatal("GetPAT() error = nil, want missing pool error")
	}
	if _, err := repo.ListPATs(TokenListFilter{UserID: "user_1"}); err == nil {
		t.Fatal("ListPATs() error = nil, want missing pool error")
	}
	if _, err := repo.GetAdapterToken("adapter_1"); err == nil {
		t.Fatal("GetAdapterToken() error = nil, want missing pool error")
	}
	if _, err := repo.ListAdapterTokens(AdapterTokenListFilter{UserID: "user_1"}); err == nil {
		t.Fatal("ListAdapterTokens() error = nil, want missing pool error")
	}
}

func TestPGRepositoryPasswordAndTokens(t *testing.T) {
	pool := authTestPool(t)
	repo := NewPGRepository(pool)
	userID := createAuthTestUser(t, pool, "alice-auth-pg@example.com")
	orgID := createAuthTestOrg(t, pool, "Auth PG Org", "auth-pg-org")
	projectID := createAuthTestProject(t, pool, orgID, "Auth PG Project", "auth-pg-project")

	if err := repo.SetPasswordHash(userID, "hash-1"); err != nil {
		t.Fatalf("SetPasswordHash() error = %v", err)
	}
	hash, err := repo.GetPasswordHash(userID)
	if err != nil {
		t.Fatalf("GetPasswordHash() error = %v", err)
	}
	if hash != "hash-1" {
		t.Fatalf("password hash = %q, want hash-1", hash)
	}

	pat := PATRecord{SubjectID: userID, Name: "dev", TokenPrefix: "pat", TokenHash: "hash_pat_1", Scopes: []string{"memory:read"}, ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.SavePAT(pat); err != nil {
		t.Fatalf("SavePAT() error = %v", err)
	}
	savedPAT, err := repo.FindPATByHash(pat.TokenHash)
	if err != nil {
		t.Fatalf("FindPATByHash() error = %v", err)
	}
	if savedPAT.ID == "" || savedPAT.SubjectID != userID || savedPAT.Scopes[0] != "memory:read" {
		t.Fatalf("saved PAT mismatch: %#v", savedPAT)
	}
	gotPAT, err := repo.GetPAT(savedPAT.ID)
	if err != nil {
		t.Fatalf("GetPAT() error = %v", err)
	}
	if gotPAT.TokenHash == "" || gotPAT.SubjectID != userID {
		t.Fatalf("GetPAT() mismatch: %#v", gotPAT)
	}
	pats, err := repo.ListPATs(TokenListFilter{UserID: userID, Status: "active"})
	if err != nil {
		t.Fatalf("ListPATs() error = %v", err)
	}
	if len(pats) != 1 || pats[0].ID != savedPAT.ID {
		t.Fatalf("ListPATs() = %#v, want saved PAT only", pats)
	}
	if err := repo.RevokePAT(savedPAT.ID, time.Now()); err != nil {
		t.Fatalf("RevokePAT() error = %v", err)
	}
	revokedPAT, err := repo.FindPATByHash(pat.TokenHash)
	if err != nil {
		t.Fatalf("FindPATByHash(revoked) error = %v", err)
	}
	if revokedPAT.RevokedAt == nil {
		t.Fatal("revoked PAT RevokedAt = nil")
	}
	activePATs, err := repo.ListPATs(TokenListFilter{UserID: userID, Status: "active"})
	if err != nil {
		t.Fatalf("ListPATs(active after revoke) error = %v", err)
	}
	if len(activePATs) != 0 {
		t.Fatalf("active PATs after revoke = %#v, want none", activePATs)
	}

	adapter := AdapterTokenRecord{UserID: userID, OrgID: orgID, ProjectID: projectID, AgentID: "codex", TokenPrefix: "adapter", TokenHash: "hash_adapter_1", Scopes: []string{"turn_event:write"}, ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.SaveAdapterToken(adapter); err != nil {
		t.Fatalf("SaveAdapterToken() error = %v", err)
	}
	savedAdapter, err := repo.FindAdapterTokenByHash(adapter.TokenHash)
	if err != nil {
		t.Fatalf("FindAdapterTokenByHash() error = %v", err)
	}
	if savedAdapter.ID == "" || savedAdapter.ProjectID != projectID || savedAdapter.AgentID != "codex" {
		t.Fatalf("saved adapter token mismatch: %#v", savedAdapter)
	}
	gotAdapter, err := repo.GetAdapterToken(savedAdapter.ID)
	if err != nil {
		t.Fatalf("GetAdapterToken() error = %v", err)
	}
	if gotAdapter.TokenHash == "" || gotAdapter.UserID != userID || gotAdapter.ProjectID != projectID {
		t.Fatalf("GetAdapterToken() mismatch: %#v", gotAdapter)
	}
	adapters, err := repo.ListAdapterTokens(AdapterTokenListFilter{UserID: userID, OrgID: orgID, ProjectID: projectID, Status: "active"})
	if err != nil {
		t.Fatalf("ListAdapterTokens() error = %v", err)
	}
	if len(adapters) != 1 || adapters[0].ID != savedAdapter.ID {
		t.Fatalf("ListAdapterTokens() = %#v, want saved adapter token only", adapters)
	}
	if err := repo.RevokeAdapterToken(savedAdapter.ID, time.Now()); err != nil {
		t.Fatalf("RevokeAdapterToken() error = %v", err)
	}
	revokedAdapter, err := repo.FindAdapterTokenByHash(adapter.TokenHash)
	if err != nil {
		t.Fatalf("FindAdapterTokenByHash(revoked) error = %v", err)
	}
	if revokedAdapter.RevokedAt == nil {
		t.Fatal("revoked adapter token RevokedAt = nil")
	}
	activeAdapters, err := repo.ListAdapterTokens(AdapterTokenListFilter{UserID: userID, OrgID: orgID, ProjectID: projectID, Status: "active"})
	if err != nil {
		t.Fatalf("ListAdapterTokens(active after revoke) error = %v", err)
	}
	if len(activeAdapters) != 0 {
		t.Fatalf("active adapter tokens after revoke = %#v, want none", activeAdapters)
	}
}

func authTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createAuthTestUser(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `INSERT INTO users (email, display_name) VALUES ($1, $2) ON CONFLICT (lower(email)) DO UPDATE SET display_name = EXCLUDED.display_name RETURNING id::text`, email, "Auth PG User").Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func createAuthTestOrg(t *testing.T, pool *pgxpool.Pool, name, slug string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `INSERT INTO orgs (name, slug) VALUES ($1, $2) ON CONFLICT (lower(slug)) DO UPDATE SET name = EXCLUDED.name RETURNING id::text`, name, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insert org: %v", err)
	}
	return id
}

func createAuthTestProject(t *testing.T, pool *pgxpool.Pool, orgID, name, slug string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `INSERT INTO projects (org_id, name, slug) VALUES ($1::uuid, $2, $3) ON CONFLICT (org_id, lower(slug)) DO UPDATE SET name = EXCLUDED.name RETURNING id::text`, orgID, name, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return id
}
