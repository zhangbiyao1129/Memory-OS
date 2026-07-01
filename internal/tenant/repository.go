package tenant

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

type Repository interface {
	CreateUser(user User) (User, error)
	CreateOrg(org Org) (Org, error)
	CreateProject(project Project) (Project, error)
	AddMembership(membership Membership) error
	GetProject(projectID string) (Project, error)
	FindMembership(userID, orgID, projectID string) (Membership, error)
}

type MemoryRepository struct {
	mu          sync.Mutex
	users       map[string]User
	emails      map[string]string
	orgs        map[string]Org
	projects    map[string]Project
	memberships map[string]Membership
	nextID      int
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		users:       map[string]User{},
		emails:      map[string]string{},
		orgs:        map[string]Org{},
		projects:    map[string]Project{},
		memberships: map[string]Membership{},
	}
}

func (r *MemoryRepository) CreateUser(user User) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	emailKey := strings.ToLower(strings.TrimSpace(user.Email))
	if emailKey == "" {
		return User{}, errors.New("email is required")
	}
	if _, exists := r.emails[emailKey]; exists {
		return User{}, errors.New("user email already exists")
	}
	user.ID = r.newID("user")
	user.Email = emailKey
	if user.Status == "" {
		user.Status = "active"
	}
	r.users[user.ID] = user
	r.emails[emailKey] = user.ID
	return user, nil
}

func (r *MemoryRepository) CreateOrg(org Org) (Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if strings.TrimSpace(org.Slug) == "" {
		return Org{}, errors.New("org slug is required")
	}
	org.ID = r.newID("org")
	r.orgs[org.ID] = org
	return org, nil
}

func (r *MemoryRepository) CreateProject(project Project) (Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.orgs[project.OrgID]; !ok {
		return Project{}, errors.New("org not found")
	}
	project.ID = r.newID("project")
	r.projects[project.ID] = project
	return project, nil
}

func (r *MemoryRepository) AddMembership(membership Membership) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.users[membership.UserID]; !ok {
		return errors.New("user not found")
	}
	if _, ok := r.orgs[membership.OrgID]; !ok {
		return errors.New("org not found")
	}
	if membership.ProjectID != "" {
		project, ok := r.projects[membership.ProjectID]
		if !ok {
			return errors.New("project not found")
		}
		if project.OrgID != membership.OrgID {
			return errors.New("project does not belong to org")
		}
	}
	if membership.Status == "" {
		membership.Status = "active"
	}
	key := membershipKey(membership.UserID, membership.OrgID, membership.ProjectID)
	if _, exists := r.memberships[key]; exists {
		return errors.New("membership already exists")
	}
	r.memberships[key] = membership
	return nil
}

func (r *MemoryRepository) GetProject(projectID string) (Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	project, ok := r.projects[projectID]
	if !ok {
		return Project{}, errors.New("project not found")
	}
	return project, nil
}

func (r *MemoryRepository) FindMembership(userID, orgID, projectID string) (Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if membership, ok := r.memberships[membershipKey(userID, orgID, projectID)]; ok {
		return membership, nil
	}
	if membership, ok := r.memberships[membershipKey(userID, orgID, "")]; ok {
		return membership, nil
	}
	return Membership{}, errors.New("membership not found")
}

func (r *MemoryRepository) newID(prefix string) string {
	r.nextID++
	return fmt.Sprintf("%s_%d", prefix, r.nextID)
}

func membershipKey(userID, orgID, projectID string) string {
	return userID + "|" + orgID + "|" + projectID
}
