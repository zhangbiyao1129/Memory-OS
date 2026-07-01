package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type TokenRequest struct {
	SubjectID string
	Name      string
	Scopes    []string
	TTL       time.Duration
	Prefix    string
}

type TokenRecord struct {
	SubjectID   string
	Name        string
	TokenPrefix string
	TokenHash   string
	Scopes      []string
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

func IssueToken(request TokenRequest) (string, TokenRecord, error) {
	if strings.TrimSpace(request.SubjectID) == "" {
		return "", TokenRecord{}, errors.New("subject id is required")
	}
	if request.TTL <= 0 {
		return "", TokenRecord{}, errors.New("token ttl must be positive")
	}
	prefix := strings.TrimSpace(request.Prefix)
	if prefix == "" {
		prefix = "tok"
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", TokenRecord{}, err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	plain := prefix + "_" + secret
	record := TokenRecord{
		SubjectID:   request.SubjectID,
		Name:        request.Name,
		TokenPrefix: prefix,
		TokenHash:   HashToken(plain),
		Scopes:      append([]string(nil), request.Scopes...),
		ExpiresAt:   time.Now().Add(request.TTL),
	}
	return plain, record, nil
}

func ValidateToken(plain string, record TokenRecord, now time.Time) error {
	if plain == "" {
		return errors.New("token is required")
	}
	if record.RevokedAt != nil {
		return errors.New("token is revoked")
	}
	if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
		return errors.New("token is expired")
	}
	if HashToken(plain) != record.TokenHash {
		return errors.New("token is invalid")
	}
	return nil
}

func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
