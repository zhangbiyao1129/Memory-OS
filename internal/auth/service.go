package auth

import (
	"errors"
	"time"
)

type Service struct {
	repo Repository
}

type Session struct {
	UserID string
}

type AdapterTokenRequest struct {
	UserID    string
	OrgID     string
	ProjectID string
	AgentID   string
	Scopes    []string
	TTL       time.Duration
}

type AdapterTokenBinding struct {
	OrgID     string
	ProjectID string
	AgentID   string
}

func NewService(repo Repository) Service {
	return Service{repo: repo}
}

func (s Service) Configured() bool {
	return s.repo != nil
}

func (s Service) SetPassword(userID, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.repo.SetPasswordHash(userID, hash)
}

func (s Service) LoginPassword(userID, password string) (Session, error) {
	hash, err := s.repo.GetPasswordHash(userID)
	if err != nil {
		return Session{}, err
	}
	if !VerifyPassword(hash, password) {
		return Session{}, errors.New("invalid credentials")
	}
	return Session{UserID: userID}, nil
}

func (s Service) CreatePAT(userID, name string, scopes []string, ttl time.Duration) (string, PATRecord, error) {
	plain, tokenRecord, err := IssueToken(TokenRequest{SubjectID: userID, Name: name, Scopes: scopes, TTL: ttl, Prefix: "pat"})
	if err != nil {
		return "", PATRecord{}, err
	}
	record := PATRecord{
		SubjectID:   tokenRecord.SubjectID,
		Name:        tokenRecord.Name,
		TokenPrefix: tokenRecord.TokenPrefix,
		TokenHash:   tokenRecord.TokenHash,
		Scopes:      tokenRecord.Scopes,
		ExpiresAt:   tokenRecord.ExpiresAt,
		RevokedAt:   tokenRecord.RevokedAt,
	}
	if err := s.repo.SavePAT(record); err != nil {
		return "", PATRecord{}, err
	}
	saved, err := s.repo.FindPATByHash(record.TokenHash)
	if err != nil {
		return "", PATRecord{}, err
	}
	return plain, saved, nil
}

func (s Service) ValidatePAT(plain string, now time.Time) (PATRecord, error) {
	record, err := s.repo.FindPATByHash(HashToken(plain))
	if err != nil {
		return PATRecord{}, err
	}
	if err := ValidateToken(plain, TokenRecord{
		SubjectID:   record.SubjectID,
		Name:        record.Name,
		TokenPrefix: record.TokenPrefix,
		TokenHash:   record.TokenHash,
		Scopes:      record.Scopes,
		ExpiresAt:   record.ExpiresAt,
		RevokedAt:   record.RevokedAt,
	}, now); err != nil {
		return PATRecord{}, err
	}
	return record, nil
}

func (s Service) GetPAT(id string) (PATRecord, error) {
	return s.repo.GetPAT(id)
}

func (s Service) ListPATs(filter TokenListFilter) ([]PATRecord, error) {
	return s.repo.ListPATs(filter)
}

func (s Service) RevokePAT(id string) error {
	return s.repo.RevokePAT(id, time.Now())
}

func (s Service) CreateAdapterToken(request AdapterTokenRequest) (string, AdapterTokenRecord, error) {
	if request.OrgID == "" || request.ProjectID == "" || request.AgentID == "" {
		return "", AdapterTokenRecord{}, errors.New("adapter token binding is required")
	}
	plain, tokenRecord, err := IssueToken(TokenRequest{SubjectID: request.UserID, Scopes: request.Scopes, TTL: request.TTL, Prefix: "adapter"})
	if err != nil {
		return "", AdapterTokenRecord{}, err
	}
	record := AdapterTokenRecord{
		UserID:      request.UserID,
		OrgID:       request.OrgID,
		ProjectID:   request.ProjectID,
		AgentID:     request.AgentID,
		TokenPrefix: tokenRecord.TokenPrefix,
		TokenHash:   tokenRecord.TokenHash,
		Scopes:      tokenRecord.Scopes,
		ExpiresAt:   tokenRecord.ExpiresAt,
	}
	if err := s.repo.SaveAdapterToken(record); err != nil {
		return "", AdapterTokenRecord{}, err
	}
	saved, err := s.repo.FindAdapterTokenByHash(record.TokenHash)
	if err != nil {
		return "", AdapterTokenRecord{}, err
	}
	return plain, saved, nil
}

func (s Service) ValidateAdapterToken(plain string, binding AdapterTokenBinding, now time.Time) (AdapterTokenRecord, error) {
	record, err := s.repo.FindAdapterTokenByHash(HashToken(plain))
	if err != nil {
		return AdapterTokenRecord{}, err
	}
	if record.OrgID != binding.OrgID || record.ProjectID != binding.ProjectID || record.AgentID != binding.AgentID {
		return AdapterTokenRecord{}, errors.New("adapter token binding mismatch")
	}
	if err := ValidateToken(plain, TokenRecord{
		SubjectID:   record.UserID,
		TokenPrefix: record.TokenPrefix,
		TokenHash:   record.TokenHash,
		Scopes:      record.Scopes,
		ExpiresAt:   record.ExpiresAt,
		RevokedAt:   record.RevokedAt,
	}, now); err != nil {
		return AdapterTokenRecord{}, err
	}
	return record, nil
}

func (s Service) GetAdapterToken(id string) (AdapterTokenRecord, error) {
	return s.repo.GetAdapterToken(id)
}

func (s Service) ListAdapterTokens(filter AdapterTokenListFilter) ([]AdapterTokenRecord, error) {
	return s.repo.ListAdapterTokens(filter)
}

func (s Service) RevokeAdapterToken(id string) error {
	return s.repo.RevokeAdapterToken(id, time.Now())
}
