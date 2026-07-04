package tenant

import (
	"errors"
	"strings"
	"unicode"

	"memory-os/internal/workspace"
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

func (s Service) CreateUser(email, displayName string) (User, error) {
	if strings.TrimSpace(displayName) == "" {
		return User{}, errors.New("display name is required")
	}
	return s.repo.CreateUser(User{Email: email, DisplayName: displayName, Status: "active"})
}

func (s Service) FindUserByEmail(email string) (User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return User{}, errors.New("email is required")
	}
	return s.repo.FindUserByEmail(email)
}

func (s Service) ListUsers(status string) ([]User, error) {
	status = strings.TrimSpace(status)
	if status != "" && status != "active" && status != "disabled" && status != "deleted" {
		return nil, errors.New("user status is invalid")
	}
	return s.repo.ListUsers(status)
}

func (s Service) UpdateUserStatus(userID, status string) (User, error) {
	userID = strings.TrimSpace(userID)
	status = strings.TrimSpace(status)
	if userID == "" {
		return User{}, errors.New("user id is required")
	}
	if status != "active" && status != "disabled" {
		return User{}, errors.New("user status is invalid")
	}
	return s.repo.UpdateUserStatus(userID, status)
}

func (s Service) CreateOrg(name, slug string) (Org, error) {
	if strings.TrimSpace(name) == "" {
		return Org{}, errors.New("org name is required")
	}
	return s.repo.CreateOrg(Org{Name: name, Slug: slug, Status: "active"})
}

func (s Service) CreateProject(orgID, name, slug string) (Project, error) {
	if strings.TrimSpace(name) == "" {
		return Project{}, errors.New("project name is required")
	}
	return s.repo.CreateProject(Project{OrgID: orgID, Name: name, Slug: slug, Status: "active"})
}

func (s Service) AddMembership(userID, orgID, projectID, role string) error {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = RoleMember
	}
	if err := s.validateRole(role); err != nil {
		return errors.New("membership role is invalid")
	}
	return s.repo.AddMembership(Membership{UserID: userID, OrgID: orgID, ProjectID: projectID, Role: role, Status: "active"})
}

func (s Service) EnsureWorkspaceProject(userID, agentID string, identity workspace.Identity) (PermissionContext, error) {
	userID = strings.TrimSpace(userID)
	agentID = strings.TrimSpace(agentID)
	if userID == "" {
		return PermissionContext{}, errors.New("user id is required")
	}
	if agentID == "" {
		agentID = "mcp"
	}
	resolved, err := workspace.Resolve(identity)
	if err != nil {
		return PermissionContext{}, err
	}
	org, err := s.repo.EnsurePersonalOrg(userID)
	if err != nil {
		return PermissionContext{}, err
	}
	project, err := s.repo.EnsureProjectForSource(Project{
		OrgID:      org.ID,
		Name:       resolved.ProjectName,
		Slug:       resolved.ProjectSlug,
		Status:     "active",
		SourceType: resolved.SourceType,
		SourceKey:  resolved.SourceKey,
	})
	if err != nil {
		return PermissionContext{}, err
	}
	if project.OrgID != org.ID {
		if _, err := s.repo.FindMembership(userID, project.OrgID, project.ID); err != nil {
			return PermissionContext{}, errors.New("workspace project membership approval required")
		}
		return s.PermissionContext(userID, project.OrgID, project.ID, agentID)
	}
	if err := s.repo.EnsureMembership(Membership{UserID: userID, OrgID: org.ID, Role: RoleOwner, Status: "active"}); err != nil {
		return PermissionContext{}, err
	}
	if err := s.repo.EnsureMembership(Membership{UserID: userID, OrgID: org.ID, ProjectID: project.ID, Role: RoleOwner, Status: "active"}); err != nil {
		return PermissionContext{}, err
	}
	return s.PermissionContext(userID, org.ID, project.ID, agentID)
}

func (s Service) ListOrgs(userID string) ([]Org, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, errors.New("user id is required")
	}
	return s.repo.ListOrgs(userID)
}

func (s Service) ListProjects(userID, orgID string) ([]Project, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(orgID) == "" {
		return nil, errors.New("user id and org id are required")
	}
	return s.repo.ListProjects(userID, orgID)
}

func (s Service) ListMemberships(orgID, projectID string) ([]Membership, error) {
	if strings.TrimSpace(orgID) == "" {
		return nil, errors.New("org id is required")
	}
	return s.repo.ListMemberships(orgID, projectID)
}

func (s Service) UpdateOrg(actorUserID, orgID, name, slug string) (Org, error) {
	if strings.TrimSpace(actorUserID) == "" || strings.TrimSpace(orgID) == "" {
		return Org{}, errors.New("actor user id and org id are required")
	}
	if strings.TrimSpace(name) == "" {
		return Org{}, errors.New("org name is required")
	}
	if strings.TrimSpace(slug) == "" {
		return Org{}, errors.New("org slug is required")
	}
	if err := s.RequireOrgWrite(actorUserID, orgID); err != nil {
		return Org{}, err
	}
	return s.repo.UpdateOrg(Org{ID: orgID, Name: name, Slug: strings.TrimSpace(slug), Status: "active"})
}

func (s Service) UpdateProject(actorUserID, orgID, projectID, name, slug string) (Project, error) {
	if strings.TrimSpace(actorUserID) == "" || strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" {
		return Project{}, errors.New("actor user id, org id and project id are required")
	}
	if strings.TrimSpace(name) == "" {
		return Project{}, errors.New("project name is required")
	}
	if strings.TrimSpace(slug) == "" {
		return Project{}, errors.New("project slug is required")
	}
	if err := s.RequireOrgWrite(actorUserID, orgID); err != nil {
		return Project{}, err
	}
	return s.repo.UpdateProject(Project{ID: projectID, OrgID: orgID, Name: name, Slug: strings.TrimSpace(slug), Status: "active"})
}

func (s Service) DeleteOrg(actorUserID, orgID string) (Org, error) {
	if strings.TrimSpace(actorUserID) == "" || strings.TrimSpace(orgID) == "" {
		return Org{}, errors.New("actor user id and org id are required")
	}
	if err := s.RequireOrgWrite(actorUserID, orgID); err != nil {
		return Org{}, err
	}
	return s.repo.DeleteOrg(orgID)
}

func (s Service) DeleteProject(actorUserID, orgID, projectID string) (Project, error) {
	if strings.TrimSpace(actorUserID) == "" || strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" {
		return Project{}, errors.New("actor user id, org id and project id are required")
	}
	if err := s.RequireOrgWrite(actorUserID, orgID); err != nil {
		return Project{}, err
	}
	return s.repo.DeleteProject(orgID, projectID)
}

func (s Service) UpdateMembershipRole(actorUserID, targetUserID, orgID, projectID, role string) (Membership, error) {
	if strings.TrimSpace(actorUserID) == "" || strings.TrimSpace(targetUserID) == "" || strings.TrimSpace(orgID) == "" {
		return Membership{}, errors.New("actor user id, target user id and org id are required")
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		return Membership{}, errors.New("membership role is required")
	}
	if err := s.validateRole(role); err != nil {
		return Membership{}, errors.New("membership role is invalid")
	}
	if strings.TrimSpace(projectID) != "" {
		if err := s.RequireProjectWrite(actorUserID, orgID, projectID); err != nil {
			return Membership{}, err
		}
	} else {
		if err := s.RequireOrgWrite(actorUserID, orgID); err != nil {
			return Membership{}, err
		}
	}
	return s.repo.UpdateMembershipRole(Membership{UserID: targetUserID, OrgID: orgID, ProjectID: projectID, Role: role, Status: "active"})
}

func (s Service) RemoveMembership(actorUserID, targetUserID, orgID, projectID string) (Membership, error) {
	if strings.TrimSpace(actorUserID) == "" || strings.TrimSpace(targetUserID) == "" || strings.TrimSpace(orgID) == "" {
		return Membership{}, errors.New("actor user id, target user id and org id are required")
	}
	if strings.TrimSpace(projectID) != "" {
		if err := s.RequireProjectWrite(actorUserID, orgID, projectID); err != nil {
			return Membership{}, err
		}
	} else {
		if err := s.RequireOrgWrite(actorUserID, orgID); err != nil {
			return Membership{}, err
		}
	}
	return s.repo.RemoveMembership(targetUserID, orgID, projectID)
}

func (s Service) ListRoles(projectID string) ([]RoleDefinition, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("project id is required")
	}
	definitions, err := s.repo.ListRoleDefinitions()
	if err != nil {
		return nil, err
	}

	projectRoles := tenantRoleDefinitions()
	projectRoleByName := make(map[string]RoleDefinition, len(projectRoles))
	for _, role := range projectRoles {
		projectRoleByName[role.Role] = role
	}

	storedRoleByName := make(map[string]RoleDefinition, len(definitions))
	for _, definition := range definitions {
		storedRoleByName[definition.Role] = definition
	}

	result := make([]RoleDefinition, 0, len(projectRoleByName)+len(definitions))
	for _, roleName := range []string{RoleOwner, RoleAdmin, RoleMember} {
		definition := projectRoleByName[roleName]
		if stored, ok := storedRoleByName[roleName]; ok {
			definition = mergeRoleDefinition(definition, stored)
		}
		result = append(result, expandRoleDefinitionForProject(definition, projectID))
		delete(storedRoleByName, roleName)
	}

	for _, definition := range definitions {
		if definition.Role == "" {
			continue
		}
		if _, isProjectBuiltin := projectRoleByName[definition.Role]; isProjectBuiltin {
			continue
		}
		result = append(result, expandRoleDefinitionForProject(mergeRoleDefinition(customRoleTemplate(definition.Role), definition), projectID))
	}

	return result, nil
}

func (s Service) UpsertRoleDefinition(role RoleDefinition) (RoleDefinition, error) {
	role.Role = strings.ToLower(strings.TrimSpace(role.Role))
	if role.Role == "" {
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

	return s.repo.UpsertRoleDefinition(RoleDefinition{
		Role:             role.Role,
		DisplayName:      strings.TrimSpace(role.DisplayName),
		Description:      strings.TrimSpace(role.Description),
		PermissionLabels: permissionLabels,
	})
}

func (s Service) RequireOrgWrite(userID, orgID string) error {
	membership, err := s.repo.FindMembership(userID, orgID, "")
	if err != nil {
		return err
	}
	if membership.Status != "active" {
		return errors.New("membership is not active")
	}
	if membership.Role != RoleOwner && membership.Role != RoleAdmin {
		return errors.New("org write permission denied")
	}
	return nil
}

func (s Service) RequireProjectWrite(userID, orgID, projectID string) error {
	membership, err := s.repo.FindMembership(userID, orgID, projectID)
	if err != nil {
		return err
	}
	if membership.Status != "active" {
		return errors.New("membership is not active")
	}
	if membership.Role != RoleOwner && membership.Role != RoleAdmin {
		return errors.New("project write permission denied")
	}
	return nil
}

func tenantRoleDefinitions() []RoleDefinition {
	return []RoleDefinition{
		{
			Role:        RoleOwner,
			DisplayName: "Owner",
			Description: "拥有项目治理、成员管理、Secret 使用和写入权限。",
			PermissionLabels: []string{
				"project:{project_id}:read",
				"project:{project_id}:write",
				"secret:{project_id}:use",
			},
		},
		{
			Role:        RoleAdmin,
			DisplayName: "Admin",
			Description: "拥有项目写入、成员协作和 Secret 使用权限。",
			PermissionLabels: []string{
				"project:{project_id}:read",
				"project:{project_id}:write",
				"secret:{project_id}:use",
			},
		},
		{
			Role:        RoleMember,
			DisplayName: "Member",
			Description: "拥有项目读取权限，可参与检索和普通记忆使用。",
			PermissionLabels: []string{
				"project:{project_id}:read",
			},
		},
	}
}

func customRoleTemplate(role string) RoleDefinition {
	label := strings.TrimSpace(strings.ToLower(role))
	if label == "" {
		label = "custom"
	}
	runes := []rune(label)
	runes[0] = unicode.ToUpper(runes[0])
	return RoleDefinition{
		Role:             role,
		DisplayName:      string(runes),
		Description:      "自定义角色。",
		PermissionLabels: []string{"project:{project_id}:read"},
	}
}

func (s Service) validateRole(role string) error {
	role = strings.ToLower(strings.TrimSpace(role))
	definitions, err := s.repo.ListRoleDefinitions()
	if err != nil {
		return err
	}
	for _, definition := range definitions {
		if strings.ToLower(strings.TrimSpace(definition.Role)) == role {
			return nil
		}
	}
	return errors.New("membership role is invalid")
}

func mergeRoleDefinition(base RoleDefinition, override RoleDefinition) RoleDefinition {
	if strings.TrimSpace(override.Role) != "" {
		base.Role = strings.TrimSpace(override.Role)
	}
	if strings.TrimSpace(override.DisplayName) != "" {
		base.DisplayName = strings.TrimSpace(override.DisplayName)
	}
	if strings.TrimSpace(override.Description) != "" {
		base.Description = strings.TrimSpace(override.Description)
	}
	if len(override.PermissionLabels) > 0 {
		base.PermissionLabels = append([]string(nil), override.PermissionLabels...)
	}
	return base
}

func expandRoleDefinitionForProject(role RoleDefinition, projectID string) RoleDefinition {
	if strings.TrimSpace(projectID) == "" {
		return role
	}
	if len(role.PermissionLabels) == 0 {
		if role.Role == RoleOwner {
			role.PermissionLabels = tenantRoleDefinitionsForProject(projectID)[0].PermissionLabels
		} else if role.Role == RoleAdmin {
			role.PermissionLabels = tenantRoleDefinitionsForProject(projectID)[1].PermissionLabels
		} else if role.Role == RoleMember {
			role.PermissionLabels = tenantRoleDefinitionsForProject(projectID)[2].PermissionLabels
		} else {
			role.PermissionLabels = []string{"project:" + projectID + ":read"}
		}
	} else {
		expanded := make([]string, 0, len(role.PermissionLabels))
		for _, label := range role.PermissionLabels {
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			expanded = append(expanded, strings.ReplaceAll(label, "{project_id}", projectID))
		}
		role.PermissionLabels = expanded
	}
	return role
}

func tenantRoleDefinitionsForProject(projectID string) []RoleDefinition {
	result := make([]RoleDefinition, 0, 3)
	for _, definition := range tenantRoleDefinitions() {
		result = append(result, expandRoleDefinitionForProject(definition, projectID))
	}
	return result
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
