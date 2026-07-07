package secret

import (
	"errors"
	"testing"
	"time"
)

func sampleBlob() EncryptedBlob {
	return EncryptedBlob{
		Algorithm:      "AES-256-GCM",
		DeviceKeyID:    "device_1",
		KeyFingerprint: "fp_abc123",
		Nonce:          []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		Ciphertext:     []byte{9, 8, 7, 6, 5, 4, 3, 2, 1},
	}
}

func TestStoreCreateEncryptedReturnsMetadataOnly(t *testing.T) {
	store := NewStore(NewMemoryRepository())

	expires := time.Now().Add(24 * time.Hour).UTC()
	meta, err := store.CreateEncrypted(CreateEncryptedRequest{
		OwnerUserID: "user_1",
		OrgID:       "org_1",
		ProjectID:   "project_1",
		Name:        "api key",
		EnvName:     "PROD",
		Site:        "binance",
		Purpose:     "trading",
		ExpiresAt:   &expires,
	}, sampleBlob())
	if err != nil {
		t.Fatalf("CreateEncrypted() error = %v", err)
	}
	if meta.SecretRef == "" {
		t.Fatal("secret ref is empty")
	}
	if meta.Name != "api key" || meta.EnvName != "PROD" || meta.Site != "binance" || meta.Purpose != "trading" {
		t.Fatalf("metadata mismatch: %#v", meta)
	}
	if meta.ExpiresAt == nil || !meta.ExpiresAt.Equal(expires) {
		t.Fatalf("expires_at mismatch: %#v", meta.ExpiresAt)
	}
	if meta.Status != "active" || meta.CurrentVersion != 1 {
		t.Fatalf("status/version mismatch: %#v", meta)
	}
}

func TestStoreCreateEncryptedRejectsEmptyBlob(t *testing.T) {
	store := NewStore(NewMemoryRepository())

	_, err := store.CreateEncrypted(CreateEncryptedRequest{OwnerUserID: "user_1", Name: "empty"}, EncryptedBlob{})

	if err == nil {
		t.Fatal("CreateEncrypted() error = nil, want empty ciphertext rejection")
	}
}

func TestStoreGetCiphertextOwnerOnly(t *testing.T) {
	store := NewStore(NewMemoryRepository())
	meta, err := store.CreateEncrypted(CreateEncryptedRequest{OwnerUserID: "user_1", Name: "api key"}, sampleBlob())
	if err != nil {
		t.Fatalf("CreateEncrypted() error = %v", err)
	}

	gotMeta, blob, err := store.GetCiphertext(meta.SecretRef, "user_1")
	if err != nil {
		t.Fatalf("GetCiphertext(owner) error = %v", err)
	}
	if gotMeta.SecretRef != meta.SecretRef {
		t.Fatalf("metadata mismatch: %#v", gotMeta)
	}
	if string(blob.Ciphertext) != string(sampleBlob().Ciphertext) {
		t.Fatalf("ciphertext mismatch: %v", blob.Ciphertext)
	}

	if _, _, err := store.GetCiphertext(meta.SecretRef, "user_2"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("GetCiphertext(non-owner) error = %v, want ErrForbidden", err)
	}
}

func TestStoreGetCiphertextRejectsDisabled(t *testing.T) {
	store := NewStore(NewMemoryRepository())
	meta, err := store.CreateEncrypted(CreateEncryptedRequest{OwnerUserID: "user_1", Name: "api key"}, sampleBlob())
	if err != nil {
		t.Fatalf("CreateEncrypted() error = %v", err)
	}
	if err := store.Disable(meta.SecretRef); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}

	if _, _, err := store.GetCiphertext(meta.SecretRef, "user_1"); err == nil {
		t.Fatal("GetCiphertext(disabled) error = nil, want disabled rejection")
	}
}

func TestStoreListReturnsMetadataOnly(t *testing.T) {
	store := NewStore(NewMemoryRepository())
	if _, err := store.CreateEncrypted(CreateEncryptedRequest{OwnerUserID: "user_1", OrgID: "org_1", ProjectID: "project_1", Name: "api key"}, sampleBlob()); err != nil {
		t.Fatalf("CreateEncrypted() error = %v", err)
	}
	if _, err := store.CreateEncrypted(CreateEncryptedRequest{OwnerUserID: "user_2", OrgID: "org_1", ProjectID: "project_1", Name: "other key"}, sampleBlob()); err != nil {
		t.Fatalf("CreateEncrypted(other) error = %v", err)
	}

	items, err := store.List(ListFilter{OwnerUserID: "user_1", OrgID: "org_1", ProjectID: "project_1", Status: "active"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].OwnerUserID != "user_1" || items[0].Name != "api key" {
		t.Fatalf("items mismatch: %#v", items)
	}
}

func TestStoreListRequiresOwner(t *testing.T) {
	store := NewStore(NewMemoryRepository())

	if _, err := store.List(ListFilter{}); err == nil {
		t.Fatal("List() error = nil, want owner required")
	}
}
