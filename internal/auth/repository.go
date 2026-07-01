package auth

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type PATRecord struct {
	ID          string
	SubjectID   string
	Name        string
	TokenPrefix string
	TokenHash   string
	Scopes      []string
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

type AdapterTokenRecord struct {
	ID          string
	UserID      string
	OrgID       string
	ProjectID   string
	AgentID     string
	TokenPrefix string
	TokenHash   string
	Scopes      []string
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

type Repository interface {
	SetPasswordHash(userID, passwordHash string) error
	GetPasswordHash(userID string) (string, error)
	SavePAT(record PATRecord) error
	FindPATByHash(tokenHash string) (PATRecord, error)
	RevokePAT(id string, revokedAt time.Time) error
	SaveAdapterToken(record AdapterTokenRecord) error
	FindAdapterTokenByHash(tokenHash string) (AdapterTokenRecord, error)
}

type MemoryRepository struct {
	mu            sync.Mutex
	passwords     map[string]string
	pats          map[string]PATRecord
	patsByHash    map[string]string
	adapterTokens map[string]AdapterTokenRecord
	adapterByHash map[string]string
	nextID        int
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		passwords:     map[string]string{},
		pats:          map[string]PATRecord{},
		patsByHash:    map[string]string{},
		adapterTokens: map[string]AdapterTokenRecord{},
		adapterByHash: map[string]string{},
	}
}

func (r *MemoryRepository) SetPasswordHash(userID, passwordHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if userID == "" || passwordHash == "" {
		return errors.New("user id and password hash are required")
	}
	r.passwords[userID] = passwordHash
	return nil
}

func (r *MemoryRepository) GetPasswordHash(userID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hash, ok := r.passwords[userID]
	if !ok {
		return "", errors.New("password credential not found")
	}
	return hash, nil
}

func (r *MemoryRepository) SavePAT(record PATRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if record.TokenHash == "" {
		return errors.New("token hash is required")
	}
	if _, exists := r.patsByHash[record.TokenHash]; exists {
		return errors.New("token hash already exists")
	}
	record.ID = r.newID("pat")
	r.pats[record.ID] = record
	r.patsByHash[record.TokenHash] = record.ID
	return nil
}

func (r *MemoryRepository) FindPATByHash(tokenHash string) (PATRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.patsByHash[tokenHash]
	if !ok {
		return PATRecord{}, errors.New("pat not found")
	}
	return r.pats[id], nil
}

func (r *MemoryRepository) RevokePAT(id string, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.pats[id]
	if !ok {
		return errors.New("pat not found")
	}
	record.RevokedAt = &revokedAt
	r.pats[id] = record
	return nil
}

func (r *MemoryRepository) SaveAdapterToken(record AdapterTokenRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if record.TokenHash == "" {
		return errors.New("token hash is required")
	}
	if _, exists := r.adapterByHash[record.TokenHash]; exists {
		return errors.New("adapter token hash already exists")
	}
	record.ID = r.newID("adapter_token")
	r.adapterTokens[record.ID] = record
	r.adapterByHash[record.TokenHash] = record.ID
	return nil
}

func (r *MemoryRepository) FindAdapterTokenByHash(tokenHash string) (AdapterTokenRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.adapterByHash[tokenHash]
	if !ok {
		return AdapterTokenRecord{}, errors.New("adapter token not found")
	}
	return r.adapterTokens[id], nil
}

func (r *MemoryRepository) newID(prefix string) string {
	r.nextID++
	return fmt.Sprintf("%s_%d", prefix, r.nextID)
}
