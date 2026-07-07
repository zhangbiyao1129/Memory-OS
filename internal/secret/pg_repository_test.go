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
	if _, err := repo.GetCurrentVersion("secret_ref_test"); err == nil {
		t.Fatal("GetCurrentVersion() error = nil, want missing pool error")
	}
	if _, err := repo.GetMetadata("secret_ref_test"); err == nil {
		t.Fatal("GetMetadata() error = nil, want missing pool error")
	}
	if _, err := repo.List(ListFilter{OwnerUserID: "user_1"}); err == nil {
		t.Fatal("List() error = nil, want missing pool error")
	}
}

func TestPGRepositoryStoreLifecycle(t *testing.T) {
	pool := secretTestPool(t)
	userID, orgID, projectID := createSecretTenantFixtures(t, pool)
	store := NewStore(NewPGRepository(pool))
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)

	expires := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Microsecond)
	meta, err := store.CreateEncrypted(CreateEncryptedRequest{
		OwnerUserID: userID,
		OrgID:       orgID,
		ProjectID:   projectID,
		Name:        "api-key-" + suffix,
		EnvName:     "PROD",
		Site:        "binance",
		Purpose:     "trading",
		ExpiresAt:   &expires,
	}, EncryptedBlob{
		Algorithm:      "AES-256-GCM",
		DeviceKeyID:    "device-" + suffix,
		KeyFingerprint: "fp-" + suffix,
		Nonce:          []byte("nonce-" + suffix),
		Ciphertext:     []byte("cipher-" + suffix),
	})
	if err != nil {
		t.Fatalf("CreateEncrypted() error = %v", err)
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
	if storedMeta.EnvName != "PROD" || storedMeta.Site != "binance" || storedMeta.Purpose != "trading" {
		t.Fatalf("stored metadata extra fields mismatch: %#v", storedMeta)
	}
	if storedMeta.ExpiresAt == nil {
		t.Fatal("stored expires_at is nil")
	}
	listed, err := NewPGRepository(pool).List(ListFilter{OwnerUserID: userID, OrgID: orgID, ProjectID: projectID, Status: "active"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].SecretRef != meta.SecretRef {
		t.Fatalf("listed metadata mismatch: %#v", listed)
	}

	gotMeta, blob, err := store.GetCiphertext(meta.SecretRef, userID)
	if err != nil {
		t.Fatalf("GetCiphertext(owner) error = %v", err)
	}
	if gotMeta.SecretRef != meta.SecretRef {
		t.Fatalf("ciphertext metadata mismatch: %#v", gotMeta)
	}
	if string(blob.Ciphertext) != "cipher-"+suffix || string(blob.Nonce) != "nonce-"+suffix {
		t.Fatalf("ciphertext blob mismatch: %#v", blob)
	}
	if blob.Algorithm != "AES-256-GCM" || blob.KeyFingerprint != "fp-"+suffix {
		t.Fatalf("ciphertext algorithm/fingerprint mismatch: %#v", blob)
	}

	if _, _, err := store.GetCiphertext(meta.SecretRef, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("GetCiphertext(non-owner) error = nil, want forbidden")
	}

	if err := store.Disable(meta.SecretRef); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	disabledMeta, err := NewPGRepository(pool).GetMetadata(meta.SecretRef)
	if err != nil {
		t.Fatalf("GetMetadata(disabled) error = %v", err)
	}
	if disabledMeta.Status != "disabled" {
		t.Fatalf("disabled status = %q, want disabled", disabledMeta.Status)
	}
	if _, _, err := store.GetCiphertext(meta.SecretRef, userID); err == nil {
		t.Fatal("GetCiphertext(disabled) error = nil, want disabled rejection")
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
