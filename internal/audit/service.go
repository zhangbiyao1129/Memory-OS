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

func (s Service) Configured() bool {
	return s.repo != nil
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

func (s Service) List(filter ListFilter) ([]Log, error) {
	if filter.OrgID == "" || filter.ProjectID == "" {
		return nil, errors.New("audit org id and project id are required")
	}
	return s.repo.List(filter)
}

func (s Service) ListSecurity(filter ListFilter) ([]Log, error) {
	if filter.ActorUserID == "" {
		return nil, errors.New("audit actor user id is required")
	}
	return s.repo.List(filter)
}
