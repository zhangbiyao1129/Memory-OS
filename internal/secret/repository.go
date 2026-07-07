package secret

import (
	"errors"
	"sync"
	"time"
)

// ErrForbidden 表示调用者不是该 secret 的 owner。
var ErrForbidden = errors.New("secret forbidden")

// Metadata 只包含服务端可见的元信息，绝不含明文。
type Metadata struct {
	SecretRef      string
	OwnerUserID    string
	OrgID          string
	ProjectID      string
	Name           string
	EnvName        string
	Site           string
	Purpose        string
	ExpiresAt      *time.Time
	Status         string
	CurrentVersion int
}

type ListFilter struct {
	OwnerUserID string
	OrgID       string
	ProjectID   string
	Status      string
	Limit       int
}

// EncryptedBlob 是本机 MCP 加密后上传的密文与算法元信息。
// 服务端只存储和回传，不持有任何解密材料。
type EncryptedBlob struct {
	Algorithm      string
	DeviceKeyID    string
	KeyFingerprint string
	Nonce          []byte
	Ciphertext     []byte
}

type Version struct {
	SecretRef string
	Version   int
	Blob      EncryptedBlob
}

type Repository interface {
	Save(meta Metadata, version Version) error
	GetMetadata(secretRef string) (Metadata, error)
	List(filter ListFilter) ([]Metadata, error)
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

func (r *MemoryRepository) List(filter ListFilter) ([]Metadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []Metadata{}
	for _, meta := range r.metadata {
		if filter.OwnerUserID != "" && meta.OwnerUserID != filter.OwnerUserID {
			continue
		}
		if filter.OrgID != "" && meta.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && meta.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Status != "" && meta.Status != filter.Status {
			continue
		}
		items = append(items, meta)
	}
	return items, nil
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
