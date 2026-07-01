package eventlog

import (
	"errors"

	"memory-os/internal/tenant"
)

type Service struct {
	repo    Repository
	options SanitizerOptions
}

type IngestResult struct {
	EventID  string   `json:"event_id"`
	Status   string   `json:"status"`
	Deduped  bool     `json:"deduped"`
	Warnings []string `json:"warnings,omitempty"`
}

func NewService(repo Repository, options SanitizerOptions) Service {
	return Service{repo: repo, options: options}
}

func (s Service) Ingest(event TurnEvent, requestID string, permissions tenant.PermissionContext) (IngestResult, error) {
	if err := Validate(event); err != nil {
		return IngestResult{}, err
	}
	if requestID == "" {
		return IngestResult{}, errors.New("request id is required")
	}
	if event.Actor.UserID != permissions.UserID || event.Actor.OrgID != permissions.OrgID || event.Actor.ProjectID != permissions.ProjectID || event.Actor.AgentID != permissions.AgentID {
		return IngestResult{}, errors.New("turn event actor does not match permission context")
	}

	sanitized, err := Sanitize(event, s.options)
	if err != nil {
		return IngestResult{}, err
	}
	saveResult, err := s.repo.Save(sanitized.Event, sanitized.SafePayload, requestID)
	if err != nil {
		return IngestResult{}, err
	}
	return IngestResult{EventID: saveResult.EventID, Status: "accepted", Deduped: saveResult.Deduped, Warnings: sanitized.Warnings}, nil
}
