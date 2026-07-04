package tenant

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

type Repository interface {
	CreateUser(user User) (User, error)
	FindUserByEmail(email string) (User, error)
	ListUsers(status string) ([]User, error)
	UpdateUserStatus(userID, status string) (User, error)
	CreateOrg(org Org) (Org, error)
	CreateProject(project Project) (Project, error)
	AddMembership(membership Membership) error
	EnsurePersonalOrg(userID string) (Org, error)
	EnsureProjectForSource(project Project) (Project, error)
	EnsureMembership(membership Membership) error
	GetProject(projectID string) (Project, error)
	UpdateOrg(org Org) (Org, error)
	UpdateProject(project Project) (Project, error)
	DeleteOrg(orgID string) (Org, error)
	DeleteProject(orgID, projectID string) (Project, error)
	UpdateMembershipRole(membership Membership) (Membership, error)
	RemoveMembership(userID, orgID, projectID string) (Membership, error)
	FindMembership(userID, orgID, projectID string) (Membership, error)
	ListOrgs(userID string) ([]Org, error)
	ListProjects(userID, orgID string) ([]Project, error)
	ListMemberships(orgID, projectID string) ([]Membership, error)
	ListRoleDefinitions() ([]RoleDefinition, error)
	UpsertRoleDefinition(role RoleDefinition) (RoleDefinition, error)
}

type MemoryRepository struct {
	mu             sync.Mutex
	users          map[string]User
	userOrder      []string
	emails         map[string]string
	orgs           map[string]Org
	projects       map[string]Project
	projectSources map[string]string
	memberships    map[string]Membership
	roles          map[string]RoleDefinition
	nextIDs        map[string]int
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		users:          map[string]User{},
		userOrder:      []string{},
		emails:         map[string]string{},
		orgs:           map[string]Org{},
		projects:       map[string]Project{},
		projectSources: map[string]string{},
		memberships:    map[string]Membership{},
		roles:          map[string]RoleDefinition{},
		nextIDs:        map[string]int{},
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
	r.userOrder = append(r.userOrder, user.ID)
	r.emails[emailKey] = user.ID
	return user, nil
}

func (r *MemoryRepository) FindUserByEmail(email string) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	userID, ok := r.emails[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return User{}, errors.New("user not found")
	}
	user, ok := r.users[userID]
	if !ok {
		return User{}, errors.New("user not found")
	}
	return user, nil
}

func (r *MemoryRepository) ListUsers(status string) ([]User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []User{}
	for _, id := range r.userOrder {
		user, ok := r.users[id]
		if !ok {
			continue
		}
		if status != "" && user.Status != status {
			continue
		}
		items = append(items, user)
	}
	return items, nil
}

func (r *MemoryRepository) UpdateUserStatus(userID, status string) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, ok := r.users[userID]
	if !ok {
		return User{}, errors.New("user not found")
	}
	user.Status = status
	r.users[userID] = user
	return user, nil
}

func (r *MemoryRepository) CreateOrg(org Org) (Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if strings.TrimSpace(org.Slug) == "" {
		return Org{}, errors.New("org slug is required")
	}
	org.ID = r.newID("org")
	if org.Status == "" {
		org.Status = "active"
	}
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
	if project.Status == "" {
		project.Status = "active"
	}
	r.projects[project.ID] = project
	if sourceKey := projectSourceKey(project.SourceType, project.SourceKey); sourceKey != "" {
		r.projectSources[sourceKey] = project.ID
	}
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

func (r *MemoryRepository) EnsurePersonalOrg(userID string) (Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.users[userID]; !ok {
		return Org{}, errors.New("user not found")
	}
	slug := personalOrgSlug(userID)
	for _, org := range r.orgs {
		if strings.EqualFold(org.Slug, slug) && org.Status != "deleted" {
			return org, nil
		}
	}
	org := Org{ID: r.newID("org"), Name: "个人工作区", Slug: slug, Status: "active"}
	r.orgs[org.ID] = org
	return org, nil
}

func (r *MemoryRepository) EnsureProjectForSource(project Project) (Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.orgs[project.OrgID]; !ok {
		return Project{}, errors.New("org not found")
	}
	sourceKey := projectSourceKey(project.SourceType, project.SourceKey)
	if sourceKey == "" {
		return Project{}, errors.New("project source is required")
	}
	if projectID, ok := r.projectSources[sourceKey]; ok {
		existing := r.projects[projectID]
		if existing.Status != "deleted" {
			return existing, nil
		}
	}
	if project.Status == "" {
		project.Status = "active"
	}
	project.ID = r.newID("project")
	r.projects[project.ID] = project
	r.projectSources[sourceKey] = project.ID
	return project, nil
}

func (r *MemoryRepository) EnsureMembership(membership Membership) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if membership.Status == "" {
		membership.Status = "active"
	}
	if membership.Role == "" {
		membership.Role = RoleMember
	}
	key := membershipKey(membership.UserID, membership.OrgID, membership.ProjectID)
	if existing, ok := r.memberships[key]; ok {
		if existing.Status == membership.Status && existing.Role == membership.Role {
			return nil
		}
		existing.Status = membership.Status
		existing.Role = membership.Role
		r.memberships[key] = existing
		return nil
	}
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
	r.memberships[key] = membership
	return nil
}

func (r *MemoryRepository) GetProject(projectID string) (Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	project, ok := r.projects[projectID]
	if !ok || project.Status == "deleted" {
		return Project{}, errors.New("project not found")
	}
	return project, nil
}

func (r *MemoryRepository) UpdateOrg(org Org) (Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.orgs[org.ID]
	if !ok || existing.Status == "deleted" {
		return Org{}, errors.New("org not found")
	}
	if strings.TrimSpace(org.Name) == "" {
		return Org{}, errors.New("org name is required")
	}
	if strings.TrimSpace(org.Slug) == "" {
		return Org{}, errors.New("org slug is required")
	}
	existing.Name = org.Name
	existing.Slug = strings.TrimSpace(org.Slug)
	r.orgs[org.ID] = existing
	return existing, nil
}

func (r *MemoryRepository) UpdateProject(project Project) (Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	org, ok := r.orgs[project.OrgID]
	if !ok || org.Status == "deleted" {
		return Project{}, errors.New("org not found")
	}
	existing, ok := r.projects[project.ID]
	if !ok || existing.OrgID != project.OrgID || existing.Status == "deleted" {
		return Project{}, errors.New("project not found")
	}
	if strings.TrimSpace(project.Name) == "" {
		return Project{}, errors.New("project name is required")
	}
	if strings.TrimSpace(project.Slug) == "" {
		return Project{}, errors.New("project slug is required")
	}
	existing.Name = project.Name
	existing.Slug = strings.TrimSpace(project.Slug)
	r.projects[project.ID] = existing
	return existing, nil
}

func (r *MemoryRepository) DeleteOrg(orgID string) (Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	org, ok := r.orgs[orgID]
	if !ok || org.Status == "deleted" {
		return Org{}, errors.New("org not found")
	}
	org.Status = "deleted"
	r.orgs[orgID] = org
	return org, nil
}

func (r *MemoryRepository) DeleteProject(orgID, projectID string) (Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	project, ok := r.projects[projectID]
	if !ok || project.OrgID != orgID || project.Status == "deleted" {
		return Project{}, errors.New("project not found")
	}
	project.Status = "deleted"
	r.projects[projectID] = project
	return project, nil
}

func (r *MemoryRepository) UpdateMembershipRole(membership Membership) (Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := membershipKey(membership.UserID, membership.OrgID, membership.ProjectID)
	existing, ok := r.memberships[key]
	if !ok || existing.Status != "active" {
		return Membership{}, errors.New("membership not found")
	}
	existing.Role = membership.Role
	r.memberships[key] = existing
	return existing, nil
}

func (r *MemoryRepository) RemoveMembership(userID, orgID, projectID string) (Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := membershipKey(userID, orgID, projectID)
	existing, ok := r.memberships[key]
	if !ok || existing.Status == "disabled" {
		return Membership{}, errors.New("membership not found")
	}
	existing.Status = "disabled"
	r.memberships[key] = existing
	return existing, nil
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

func (r *MemoryRepository) ListOrgs(userID string) ([]Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []Org{}
	seen := map[string]bool{}
	for _, membership := range r.memberships {
		if membership.UserID != userID || membership.Status != "active" {
			continue
		}
		org, ok := r.orgs[membership.OrgID]
		if !ok || org.Status == "deleted" || seen[org.ID] {
			continue
		}
		items = append(items, org)
		seen[org.ID] = true
	}
	return items, nil
}

func (r *MemoryRepository) ListProjects(userID, orgID string) ([]Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	orgMembership, hasOrgMembership := r.memberships[membershipKey(userID, orgID, "")]
	items := []Project{}
	for _, project := range r.projects {
		if project.OrgID != orgID || project.Status == "deleted" {
			continue
		}
		projectMembership, hasProjectMembership := r.memberships[membershipKey(userID, orgID, project.ID)]
		if hasProjectMembership && projectMembership.Status == "active" {
			items = append(items, project)
			continue
		}
		if hasOrgMembership && orgMembership.Status == "active" {
			items = append(items, project)
		}
	}
	return items, nil
}

func (r *MemoryRepository) ListMemberships(orgID, projectID string) ([]Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []Membership{}
	for _, membership := range r.memberships {
		if membership.OrgID != orgID {
			continue
		}
		if projectID != "" && membership.ProjectID != projectID {
			continue
		}
		items = append(items, membership)
	}
	return items, nil
}

func (r *MemoryRepository) ListRoleDefinitions() ([]RoleDefinition, error) {
	result := tenantRoleDefinitions()
	for _, role := range r.roles {
		result = append(result, role)
	}
	return result, nil
}

func (r *MemoryRepository) UpsertRoleDefinition(role RoleDefinition) (RoleDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := strings.ToLower(strings.TrimSpace(role.Role))
	if name == "" {
		return RoleDefinition{}, errors.New("role name is required")
	}
	permissionLabels := make([]string, 0, len(role.PermissionLabels))
	for _, label := range role.PermissionLabels {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			permissionLabels = append(permissionLabels, trimmed)
		}
	}
	if len(permissionLabels) == 0 {
		return RoleDefinition{}, errors.New("role permission labels are required")
	}

	result := RoleDefinition{
		Role:             name,
		DisplayName:      strings.TrimSpace(role.DisplayName),
		Description:      strings.TrimSpace(role.Description),
		PermissionLabels: permissionLabels,
	}
	r.roles[name] = result
	return result, nil
}

func (r *MemoryRepository) newID(prefix string) string {
	r.nextIDs[prefix]++
	return fmt.Sprintf("%s_%d", prefix, r.nextIDs[prefix])
}

func personalOrgSlug(userID string) string {
	return "personal-" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(userID), "_", "-"))
}

func projectSourceKey(sourceType, sourceKey string) string {
	sourceType = strings.ToLower(strings.TrimSpace(sourceType))
	sourceKey = strings.ToLower(strings.TrimSpace(sourceKey))
	if sourceType == "" || sourceKey == "" {
		return ""
	}
	return sourceType + ":" + sourceKey
}

func membershipKey(userID, orgID, projectID string) string {
	return userID + "|" + orgID + "|" + projectID
}
