package secret

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
)

type CreateRequest struct {
	OwnerUserID string
	OrgID       string
	ProjectID   string
	Name        string
	Plaintext   string
}

type Vault struct {
	repo  Repository
	codec AESGCMCodec
}

func NewVault(repo Repository, codec AESGCMCodec) Vault {
	return Vault{repo: repo, codec: codec}
}

func (v Vault) Create(request CreateRequest) (Metadata, error) {
	if request.OwnerUserID == "" {
		return Metadata{}, errors.New("owner user id is required")
	}
	if request.Name == "" {
		return Metadata{}, errors.New("secret name is required")
	}
	if request.Plaintext == "" {
		return Metadata{}, errors.New("secret plaintext is required")
	}
	encrypted, err := v.codec.Encrypt([]byte(request.Plaintext))
	if err != nil {
		return Metadata{}, err
	}
	ref, err := newSecretRef()
	if err != nil {
		return Metadata{}, err
	}
	meta := Metadata{
		SecretRef:      ref,
		OwnerUserID:    request.OwnerUserID,
		OrgID:          request.OrgID,
		ProjectID:      request.ProjectID,
		Name:           request.Name,
		Status:         "active",
		CurrentVersion: 1,
	}
	version := Version{SecretRef: ref, Version: 1, Value: encrypted}
	if err := v.repo.Save(meta, version); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

func (v Vault) DecryptForUse(secretRef string) (string, error) {
	meta, err := v.repo.GetMetadata(secretRef)
	if err != nil {
		return "", err
	}
	if meta.Status != "active" {
		return "", errors.New("secret is not active")
	}
	version, err := v.repo.GetCurrentVersion(secretRef)
	if err != nil {
		return "", err
	}
	plaintext, err := v.codec.Decrypt(version.Value)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (v Vault) Disable(secretRef string) error {
	return v.repo.Disable(secretRef)
}

func newSecretRef() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "secret_ref_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
