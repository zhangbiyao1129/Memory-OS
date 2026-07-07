package secret

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"
)

// CreateEncryptedRequest 由本机 MCP 上传：只含 metadata，密文单独通过 EncryptedBlob 传入。
type CreateEncryptedRequest struct {
	OwnerUserID string
	OrgID       string
	ProjectID   string
	Name        string
	EnvName     string
	Site        string
	Purpose     string
	ExpiresAt   *time.Time
}

// Store 是服务端零明文的 secret 存储：只落库/回传密文，永不加解密。
type Store struct {
	repo Repository
}

func NewStore(repo Repository) Store {
	return Store{repo: repo}
}

func (s Store) Configured() bool {
	return s.repo != nil
}

func (s Store) CreateEncrypted(request CreateEncryptedRequest, blob EncryptedBlob) (Metadata, error) {
	if request.OwnerUserID == "" {
		return Metadata{}, errors.New("owner user id is required")
	}
	if request.Name == "" {
		return Metadata{}, errors.New("secret name is required")
	}
	if len(blob.Ciphertext) == 0 {
		return Metadata{}, errors.New("ciphertext is required")
	}
	if len(blob.Nonce) == 0 {
		return Metadata{}, errors.New("nonce is required")
	}
	if blob.Algorithm == "" {
		return Metadata{}, errors.New("algorithm is required")
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
		EnvName:        request.EnvName,
		Site:           request.Site,
		Purpose:        request.Purpose,
		ExpiresAt:      request.ExpiresAt,
		Status:         "active",
		CurrentVersion: 1,
	}
	version := Version{SecretRef: ref, Version: 1, Blob: blob}
	if err := s.repo.Save(meta, version); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

// GetCiphertext 只允许 owner 取回自己的密文，服务端不解密。
func (s Store) GetCiphertext(secretRef, ownerUserID string) (Metadata, EncryptedBlob, error) {
	if secretRef == "" {
		return Metadata{}, EncryptedBlob{}, errors.New("secret ref is required")
	}
	if ownerUserID == "" {
		return Metadata{}, EncryptedBlob{}, errors.New("owner user id is required")
	}
	meta, err := s.repo.GetMetadata(secretRef)
	if err != nil {
		return Metadata{}, EncryptedBlob{}, err
	}
	if meta.OwnerUserID != ownerUserID {
		return Metadata{}, EncryptedBlob{}, ErrForbidden
	}
	if meta.Status != "active" {
		return Metadata{}, EncryptedBlob{}, errors.New("secret is not active")
	}
	version, err := s.repo.GetCurrentVersion(secretRef)
	if err != nil {
		return Metadata{}, EncryptedBlob{}, err
	}
	return meta, version.Blob, nil
}

func (s Store) List(filter ListFilter) ([]Metadata, error) {
	if filter.OwnerUserID == "" {
		return nil, errors.New("owner user id is required")
	}
	if filter.Status == "" {
		filter.Status = "active"
	}
	return s.repo.List(filter)
}

func (s Store) Metadata(secretRef string) (Metadata, error) {
	if secretRef == "" {
		return Metadata{}, errors.New("secret ref is required")
	}
	return s.repo.GetMetadata(secretRef)
}

func (s Store) Disable(secretRef string) error {
	return s.repo.Disable(secretRef)
}

func newSecretRef() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "secret_ref_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
