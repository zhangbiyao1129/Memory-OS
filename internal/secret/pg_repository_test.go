package secret

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

	if err := repo.Save(Metadata{SecretRef: "secret_ref_test"}, Version{}); err == nil {
		t.Fatal("Save() error = nil, want missing pool error")
	}
	if _, err := repo.GetMetadata("secret_ref_test"); err == nil {
		t.Fatal("GetMetadata() error = nil, want missing pool error")
	}
	if _, err := repo.List(ListFilter{OwnerUserID: "user_1"}); err == nil {
		t.Fatal("List() error = nil, want missing pool error")
	}
}

func TestPGRepositoryVaultLifecycle(t *testing.T) {
	pool := secretTestPool(t)
	userID, orgID, projectID := createSecretTenantFixtures(t, pool)
	vault := NewVault(NewPGRepository(pool), testCodec(t))
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)

	meta, err := vault.Create(CreateRequest{
		OwnerUserID: userID,
		OrgID:       orgID,
		ProjectID:   projectID,
		Name:        "api-key-" + suffix,
		Plaintext:   "fake-secret-value-" + suffix,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if meta.SecretRef == "" || meta.CurrentVersion != 1 || meta.Status != "active" {
		t.Fatalf("metadata mismatch: %#v", meta)
	}

	storedMeta, err := NewPGRepository(pool).GetMetadata(meta.SecretRef)
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if storedMeta.Name != meta.Name || storedMeta.OwnerUserID != userID || storedMeta.ProjectID != projectID {
		t.Fatalf("stored metadata mismatch: %#v", storedMeta)
	}
	listed, err := NewPGRepository(pool).List(ListFilter{OwnerUserID: userID, OrgID: orgID, ProjectID: projectID, Status: "active"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].SecretRef != meta.SecretRef {
		t.Fatalf("listed metadata mismatch: %#v", listed)
	}

	value, err := vault.DecryptForUse(meta.SecretRef)
	if err != nil {
		t.Fatalf("DecryptForUse() error = %v", err)
	}
	if value != "fake-secret-value-"+suffix {
		t.Fatalf("decrypted value = %q, want fake secret value", value)
	}

	if err := vault.Disable(meta.SecretRef); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	disabledMeta, err := NewPGRepository(pool).GetMetadata(meta.SecretRef)
	if err != nil {
		t.Fatalf("GetMetadata(disabled) error = %v", err)
	}
	if disabledMeta.Status != "disabled" {
		t.Fatalf("disabled status = %q, want disabled", disabledMeta.Status)
	}
	if _, err := vault.DecryptForUse(meta.SecretRef); err == nil {
		t.Fatal("DecryptForUse() error = nil, want disabled rejection")
	}
}

func secretTestPool(t *testing.T) *pgxpool.Pool {
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

func createSecretTenantFixtures(t *testing.T, pool *pgxpool.Pool) (string, string, string) {
	t.Helper()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	var userID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO users (email, display_name) VALUES ($1, $2) RETURNING id::text`, "secret-"+suffix+"@example.com", "Secret User").Scan(&userID); err != nil {
		t.Fatalf("insert secret user: %v", err)
	}
	var orgID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id::text`, "Secret Org "+suffix, "secret-org-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert secret org: %v", err)
	}
	var projectID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO projects (org_id, name, slug) VALUES ($1::uuid, $2, $3) RETURNING id::text`, orgID, "Secret Project "+suffix, "secret-project-"+suffix).Scan(&projectID); err != nil {
		t.Fatalf("insert secret project: %v", err)
	}
	return userID, orgID, projectID
}
