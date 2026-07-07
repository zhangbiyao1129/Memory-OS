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
	hasOrg := strings.TrimSpace(filter.OrgID) != ""
	hasProject := strings.TrimSpace(filter.ProjectID) != ""
	if hasOrg != hasProject {
		return Snapshot{}, errors.New("org_id and project_id must be provided together")
	}
	if hasProject && len(filter.PermissionLabels) == 0 {
		return Snapshot{}, errors.New("permission labels are required")
	}
	return s.repo.Snapshot(ctx, filter)
}
