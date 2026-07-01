package tenant

import (
	"errors"
	"strings"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return Service{repo: repo}
}

func (s Service) CreateUser(email, displayName string) (User, error) {
	if strings.TrimSpace(displayName) == "" {
		return User{}, errors.New("display name is required")
	}
	return s.repo.CreateUser(User{Email: email, DisplayName: displayName, Status: "active"})
}

func (s Service) CreateOrg(name, slug string) (Org, error) {
	if strings.TrimSpace(name) == "" {
		return Org{}, errors.New("org name is required")
	}
	return s.repo.CreateOrg(Org{Name: name, Slug: slug})
}

func (s Service) CreateProject(orgID, name, slug string) (Project, error) {
	if strings.TrimSpace(name) == "" {
		return Project{}, errors.New("project name is required")
	}
	return s.repo.CreateProject(Project{OrgID: orgID, Name: name, Slug: slug})
}

func (s Service) AddMembership(userID, orgID, projectID, role string) error {
	if role == "" {
		role = RoleMember
	}
	return s.repo.AddMembership(Membership{UserID: userID, OrgID: orgID, ProjectID: projectID, Role: role, Status: "active"})
}

func (s Service) PermissionContext(userID, orgID, projectID, agentID string) (PermissionContext, error) {
	project, err := s.repo.GetProject(projectID)
	if err != nil {
		return PermissionContext{}, err
	}
	if project.OrgID != orgID {
		return PermissionContext{}, errors.New("project does not belong to org")
	}

	membership, err := s.repo.FindMembership(userID, orgID, projectID)
	if err != nil {
		return PermissionContext{}, err
	}
	if membership.Status != "active" {
		return PermissionContext{}, errors.New("membership is not active")
	}

	labels := []string{
		"user:" + userID + ":read",
		"org:" + orgID + ":member",
		"project:" + projectID + ":read",
	}
	if membership.Role == RoleOwner || membership.Role == RoleAdmin {
		labels = append(labels, "project:"+projectID+":write", "secret:"+projectID+":use")
	}

	return PermissionContext{UserID: userID, OrgID: orgID, ProjectID: projectID, AgentID: agentID, PermissionLabels: labels}, nil
}
