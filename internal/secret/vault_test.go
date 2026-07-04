package secret

import (
	"bytes"
	"testing"
)

func TestVaultCreateReturnsMetadataOnly(t *testing.T) {
	vault := NewVault(NewMemoryRepository(), testCodec(t))

	meta, err := vault.Create(CreateRequest{
		OwnerUserID: "user_1",
		OrgID:       "org_1",
		ProjectID:   "project_1",
		Name:        "api key",
		Plaintext:   "fake-secret-value",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if meta.SecretRef == "" {
		t.Fatal("secret ref is empty")
	}
	if meta.Name != "api key" {
		t.Fatalf("name = %q, want api key", meta.Name)
	}
}

func TestVaultDecryptForUse(t *testing.T) {
	vault := NewVault(NewMemoryRepository(), testCodec(t))
	meta, err := vault.Create(CreateRequest{
		OwnerUserID: "user_1",
		OrgID:       "org_1",
		ProjectID:   "project_1",
		Name:        "api key",
		Plaintext:   "fake-secret-value",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	value, err := vault.DecryptForUse(meta.SecretRef)

	if err != nil {
		t.Fatalf("DecryptForUse() error = %v", err)
	}
	if value != "fake-secret-value" {
		t.Fatalf("value = %q, want fake-secret-value", value)
	}
}

func TestVaultDisablePreventsUse(t *testing.T) {
	vault := NewVault(NewMemoryRepository(), testCodec(t))
	meta, err := vault.Create(CreateRequest{
		OwnerUserID: "user_1",
		Name:        "api key",
		Plaintext:   "fake-secret-value",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := vault.Disable(meta.SecretRef); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}

	_, err = vault.DecryptForUse(meta.SecretRef)

	if err == nil {
		t.Fatal("DecryptForUse() error = nil, want disabled rejection")
	}
}

func TestVaultListReturnsMetadataOnly(t *testing.T) {
	vault := NewVault(NewMemoryRepository(), testCodec(t))
	if _, err := vault.Create(CreateRequest{OwnerUserID: "user_1", OrgID: "org_1", ProjectID: "project_1", Name: "api key", Plaintext: "fake-secret-value"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := vault.Create(CreateRequest{OwnerUserID: "user_2", OrgID: "org_1", ProjectID: "project_1", Name: "other key", Plaintext: "other-fake-secret"}); err != nil {
		t.Fatalf("Create(other) error = %v", err)
	}

	items, err := vault.List(ListFilter{OwnerUserID: "user_1", OrgID: "org_1", ProjectID: "project_1", Status: "active"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(items) != 1 || items[0].OwnerUserID != "user_1" || items[0].Name != "api key" {
		t.Fatalf("items mismatch: %#v", items)
	}
}

func TestVaultRejectsEmptyPlaintext(t *testing.T) {
	vault := NewVault(NewMemoryRepository(), testCodec(t))

	_, err := vault.Create(CreateRequest{OwnerUserID: "user_1", Name: "empty"})

	if err == nil {
		t.Fatal("Create() error = nil, want empty plaintext rejection")
	}
}

func testCodec(t *testing.T) AESGCMCodec {
	t.Helper()

	codec, err := NewAESGCMCodec("key-1", bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatalf("NewAESGCMCodec() error = %v", err)
	}
	return codec
}
