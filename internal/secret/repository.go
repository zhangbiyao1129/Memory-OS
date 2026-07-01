package secret

import (
	"errors"
	"sync"
)

type Metadata struct {
	SecretRef      string
	OwnerUserID    string
	OrgID          string
	ProjectID      string
	Name           string
	Status         string
	CurrentVersion int
}

type Version struct {
	SecretRef string
	Version   int
	Value     EncryptedValue
}

type Repository interface {
	Save(meta Metadata, version Version) error
	GetMetadata(secretRef string) (Metadata, error)
	GetCurrentVersion(secretRef string) (Version, error)
	Disable(secretRef string) error
}

type MemoryRepository struct {
	mu       sync.Mutex
	metadata map[string]Metadata
	versions map[string]Version
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{metadata: map[string]Metadata{}, versions: map[string]Version{}}
}

func (r *MemoryRepository) Save(meta Metadata, version Version) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if meta.SecretRef == "" {
		return errors.New("secret ref is required")
	}
	if _, exists := r.metadata[meta.SecretRef]; exists {
		return errors.New("secret ref already exists")
	}
	r.metadata[meta.SecretRef] = meta
	r.versions[meta.SecretRef] = version
	return nil
}

func (r *MemoryRepository) GetMetadata(secretRef string) (Metadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta, ok := r.metadata[secretRef]
	if !ok {
		return Metadata{}, errors.New("secret not found")
	}
	return meta, nil
}

func (r *MemoryRepository) GetCurrentVersion(secretRef string) (Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	version, ok := r.versions[secretRef]
	if !ok {
		return Version{}, errors.New("secret version not found")
	}
	return version, nil
}

func (r *MemoryRepository) Disable(secretRef string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta, ok := r.metadata[secretRef]
	if !ok {
		return errors.New("secret not found")
	}
	meta.Status = "disabled"
	r.metadata[secretRef] = meta
	return nil
}
