package memorystats

import (
	"context"
	"errors"
	"strings"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return Service{repo: repo}
}

func (s Service) Configured() bool {
	return s.repo != nil
}

func (s Service) Snapshot(ctx context.Context, filter Filter) (Snapshot, error) {
	if !s.Configured() {
		return Snapshot{}, errors.New("memory stats service is not configured")
	}
	if strings.TrimSpace(filter.UserID) == "" {
		return Snapshot{}, errors.New("user_id is required")
	}
	if strings.TrimSpace(filter.OrgID) == "" || strings.TrimSpace(filter.ProjectID) == "" {
		return Snapshot{}, errors.New("org_id and project_id are required")
	}
	if len(filter.PermissionLabels) == 0 {
		return Snapshot{}, errors.New("permission labels are required")
	}
	return s.repo.Snapshot(ctx, filter)
}
