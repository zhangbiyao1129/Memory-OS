package audit

import (
	"errors"
	"regexp"
)

var forbiddenSecretPattern = regexp.MustCompile(`(?i)(fake-secret-value|sk[-_][a-z0-9_-]{8,}|password-test-redacted|BEGIN [A-Z ]*PRIVATE KEY)`)

type Service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return Service{repo: repo}
}

func (s Service) Record(log Log) error {
	if log.Action == "" || log.ResourceType == "" || log.ResourceID == "" || log.RequestID == "" {
		return errors.New("audit action, resource and request id are required")
	}
	for _, value := range log.Metadata {
		if forbiddenSecretPattern.MatchString(value) {
			return errors.New("audit metadata contains secret-like plaintext")
		}
	}
	return s.repo.Save(log)
}
