package audit

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGRepositoryRequiresPool(t *testing.T) {
	repo := NewPGRepository(nil)

	if err := repo.Save(Log{Action: "archive.create", ResourceType: "archive", ResourceID: "archive_1", RequestID: "request_1", Result: "ok"}); err == nil {
		t.Fatal("Save() error = nil, want missing pool error")
	}
	if _, err := repo.List(ListFilter{OrgID: "org_1", ProjectID: "project_1"}); err == nil {
		t.Fatal("List() error = nil, want missing pool error")
	}
}

func TestPGRepositorySavePersistsAuditLog(t *testing.T) {
	pool := auditTestPool(t)
	userID, orgID, projectID := createAuditTenantFixtures(t, pool)
	repo := NewPGRepository(pool)
	requestID := "audit_request_" + auditSuffix()

	err := repo.Save(Log{
		ActorUserID:  userID,
		OrgID:        orgID,
		ProjectID:    projectID,
		Action:       "archive.create",
		ResourceType: "archive",
		ResourceID:   "archive_" + auditSuffix(),
		RequestID:    requestID,
		Result:       "ok",
		Metadata:     map[string]string{"source": "pg_contract", "status": "created"},
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var action string
	var metadata map[string]string
	if err := pool.QueryRow(context.Background(), `
SELECT action, metadata
FROM audit_logs
WHERE request_id = $1`, requestID).Scan(&action, &metadata); err != nil {
		t.Fatalf("select audit log: %v", err)
	}
	if action != "archive.create" {
		t.Fatalf("action = %q, want archive.create", action)
	}
	if metadata["source"] != "pg_contract" || metadata["status"] != "created" {
		t.Fatalf("metadata = %#v, want pg contract metadata", metadata)
	}

	items, err := repo.List(ListFilter{OrgID: orgID, ProjectID: projectID, ResourceType: "archive", Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].RequestID != requestID || items[0].Metadata["source"] != "pg_contract" {
		t.Fatalf("List() = %#v, want saved audit log", items)
	}
}

func auditTestPool(t *testing.T) *pgxpool.Pool {
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

func createAuditTenantFixtures(t *testing.T, pool *pgxpool.Pool) (string, string, string) {
	t.Helper()
	suffix := auditSuffix()
	var userID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO users (email, display_name) VALUES ($1, $2) RETURNING id::text`, "audit-"+suffix+"@example.com", "Audit User").Scan(&userID); err != nil {
		t.Fatalf("insert audit user: %v", err)
	}
	var orgID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id::text`, "Audit Org "+suffix, "audit-org-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert audit org: %v", err)
	}
	var projectID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO projects (org_id, name, slug) VALUES ($1::uuid, $2, $3) RETURNING id::text`, orgID, "Audit Project "+suffix, "audit-project-"+suffix).Scan(&projectID); err != nil {
		t.Fatalf("insert audit project: %v", err)
	}
	return userID, orgID, projectID
}

func auditSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
