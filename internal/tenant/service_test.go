package tenant

import "testing"

func TestServiceCreatesUserOrgProjectMembership(t *testing.T) {
	service := NewService(NewMemoryRepository())

	user, err := service.CreateUser("alice@example.com", "Alice")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	org, err := service.CreateOrg("Org Alpha", "org-alpha")
	if err != nil {
		t.Fatalf("CreateOrg() error = %v", err)
	}
	project, err := service.CreateProject(org.ID, "Project Alpha", "project-alpha")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if err := service.AddMembership(user.ID, org.ID, project.ID, RoleOwner); err != nil {
		t.Fatalf("AddMembership() error = %v", err)
	}

	ctx, err := service.PermissionContext(user.ID, org.ID, project.ID, "codex")
	if err != nil {
		t.Fatalf("PermissionContext() error = %v", err)
	}
	if ctx.UserID != user.ID || ctx.OrgID != org.ID || ctx.ProjectID != project.ID {
		t.Fatalf("permission context ids mismatch: %#v", ctx)
	}
	if !contains(ctx.PermissionLabels, "project:"+project.ID+":read") {
		t.Fatalf("permission labels missing project read: %#v", ctx.PermissionLabels)
	}
}

func TestPermissionContextRejectsCrossOrg(t *testing.T) {
	service := NewService(NewMemoryRepository())
	user, _ := service.CreateUser("alice@example.com", "Alice")
	orgAlpha, _ := service.CreateOrg("Org Alpha", "org-alpha")
	orgBeta, _ := service.CreateOrg("Org Beta", "org-beta")
	projectAlpha, _ := service.CreateProject(orgAlpha.ID, "Project Alpha", "project-alpha")
	if err := service.AddMembership(user.ID, orgAlpha.ID, projectAlpha.ID, RoleMember); err != nil {
		t.Fatalf("AddMembership() error = %v", err)
	}

	_, err := service.PermissionContext(user.ID, orgBeta.ID, projectAlpha.ID, "codex")

	if err == nil {
		t.Fatal("PermissionContext() error = nil, want cross org rejection")
	}
}

func TestPermissionContextRejectsCrossProject(t *testing.T) {
	service := NewService(NewMemoryRepository())
	user, _ := service.CreateUser("alice@example.com", "Alice")
	org, _ := service.CreateOrg("Org Alpha", "org-alpha")
	projectAlpha, _ := service.CreateProject(org.ID, "Project Alpha", "project-alpha")
	projectBeta, _ := service.CreateProject(org.ID, "Project Beta", "project-beta")
	if err := service.AddMembership(user.ID, org.ID, projectAlpha.ID, RoleMember); err != nil {
		t.Fatalf("AddMembership() error = %v", err)
	}

	_, err := service.PermissionContext(user.ID, org.ID, projectBeta.ID, "codex")

	if err == nil {
		t.Fatal("PermissionContext() error = nil, want cross project rejection")
	}
}

func TestPermissionContextRejectsMissingMembership(t *testing.T) {
	service := NewService(NewMemoryRepository())
	user, _ := service.CreateUser("alice@example.com", "Alice")
	org, _ := service.CreateOrg("Org Alpha", "org-alpha")
	project, _ := service.CreateProject(org.ID, "Project Alpha", "project-alpha")

	_, err := service.PermissionContext(user.ID, org.ID, project.ID, "codex")

	if err == nil {
		t.Fatal("PermissionContext() error = nil, want missing membership rejection")
	}
}

func TestDuplicateEmailRejected(t *testing.T) {
	service := NewService(NewMemoryRepository())
	if _, err := service.CreateUser("alice@example.com", "Alice"); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	_, err := service.CreateUser("ALICE@example.com", "Alice Again")

	if err == nil {
		t.Fatal("CreateUser() error = nil, want duplicate email rejection")
	}
}

func TestServiceListsUsers(t *testing.T) {
	service := NewService(NewMemoryRepository())
	alice, err := service.CreateUser("ALICE@example.com", "Alice")
	if err != nil {
		t.Fatalf("CreateUser(alice) error = %v", err)
	}
	bob, err := service.CreateUser("bob@example.com", "Bob")
	if err != nil {
		t.Fatalf("CreateUser(bob) error = %v", err)
	}

	users, err := service.ListUsers("")
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ListUsers() len = %d, want 2: %#v", len(users), users)
	}
	if users[0].ID != alice.ID || users[0].Email != "alice@example.com" || users[1].ID != bob.ID {
		t.Fatalf("ListUsers() = %#v, want creation order with normalized email", users)
	}
}

func TestServiceFindsUserByEmail(t *testing.T) {
	service := NewService(NewMemoryRepository())
	user, err := service.CreateUser("ALICE@example.com", "Alice")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	found, err := service.FindUserByEmail(" alice@example.com ")
	if err != nil {
		t.Fatalf("FindUserByEmail() error = %v", err)
	}
	if found.ID != user.ID || found.Email != "alice@example.com" {
		t.Fatalf("FindUserByEmail() = %#v, want created normalized user %#v", found, user)
	}

	if _, err := service.FindUserByEmail("missing@example.com"); err == nil {
		t.Fatal("FindUserByEmail(missing) error = nil, want not found")
	}
}

func TestServiceUpdatesUserStatus(t *testing.T) {
	service := NewService(NewMemoryRepository())
	user, err := service.CreateUser("status-user@example.com", "Status User")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	disabled, err := service.UpdateUserStatus(user.ID, "disabled")
	if err != nil {
		t.Fatalf("UpdateUserStatus(disabled) error = %v", err)
	}
	if disabled.Status != "disabled" {
		t.Fatalf("UpdateUserStatus(disabled) status = %q, want disabled", disabled.Status)
	}
	disabledUsers, err := service.ListUsers("disabled")
	if err != nil {
		t.Fatalf("ListUsers(disabled) error = %v", err)
	}
	if len(disabledUsers) != 1 || disabledUsers[0].ID != user.ID {
		t.Fatalf("ListUsers(disabled) = %#v, want disabled user", disabledUsers)
	}

	active, err := service.UpdateUserStatus(user.ID, "active")
	if err != nil {
		t.Fatalf("UpdateUserStatus(active) error = %v", err)
	}
	if active.Status != "active" {
		t.Fatalf("UpdateUserStatus(active) status = %q, want active", active.Status)
	}
	if _, err := service.UpdateUserStatus(user.ID, "deleted"); err == nil {
		t.Fatal("UpdateUserStatus(deleted) error = nil, want unsupported status rejection")
	}
	if _, err := service.UpdateUserStatus("", "disabled"); err == nil {
		t.Fatal("UpdateUserStatus(empty user) error = nil, want validation rejection")
	}
}

func TestServiceListsRoleDefinitions(t *testing.T) {
	service := NewService(NewMemoryRepository())

	roles, err := service.ListRoles("project_roles")
	if err != nil {
		t.Fatalf("ListRoles() error = %v", err)
	}
	if len(roles) != 3 {
		t.Fatalf("ListRoles() len = %d, want 3: %#v", len(roles), roles)
	}
	if roles[0].Role != RoleOwner || roles[1].Role != RoleAdmin || roles[2].Role != RoleMember {
		t.Fatalf("ListRoles() order = %#v, want owner/admin/member", roles)
	}
	if !contains(roles[0].PermissionLabels, "project:project_roles:write") || !contains(roles[0].PermissionLabels, "secret:project_roles:use") {
		t.Fatalf("owner role labels = %#v, want write and secret use", roles[0].PermissionLabels)
	}
	if !contains(roles[2].PermissionLabels, "project:project_roles:read") || contains(roles[2].PermissionLabels, "project:project_roles:write") {
		t.Fatalf("member role labels = %#v, want read only project permission", roles[2].PermissionLabels)
	}
	if _, err := service.ListRoles(""); err == nil {
		t.Fatal("ListRoles(empty project) error = nil, want validation error")
	}
}

func TestServiceAcceptsCustomRoleInRoleDefinitions(t *testing.T) {
	repo := &customRoleRepository{
		MemoryRepository: NewMemoryRepository(),
		roleDefinitions: []RoleDefinition{
			{Role: "reviewer", DisplayName: "Reviewer", PermissionLabels: []string{"project:{project_id}:read"}},
		},
	}
	service := NewService(repo)

	user, err := service.CreateUser("reviewer@example.com", "Reviewer")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	org, err := service.CreateOrg("Role Org", "role-org")
	if err != nil {
		t.Fatalf("CreateOrg() error = %v", err)
	}
	project, err := service.CreateProject(org.ID, "Role Project", "role-project")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if err := service.AddMembership(user.ID, org.ID, project.ID, "reviewer"); err != nil {
		t.Fatalf("AddMembership(custom role) error = %v", err)
	}
	memberships, err := service.ListMemberships(org.ID, project.ID)
	if err != nil {
		t.Fatalf("ListMemberships() error = %v", err)
	}
	if len(memberships) != 1 || memberships[0].Role != "reviewer" || memberships[0].UserID != user.ID {
		t.Fatalf("ListMemberships() = %#v, want reviewer member", memberships)
	}
}

func TestServiceRejectsUnknownRole(t *testing.T) {
	service := NewService(NewMemoryRepository())
	user, err := service.CreateUser("reject@example.com", "Reject")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	org, err := service.CreateOrg("Reject Org", "reject-org")
	if err != nil {
		t.Fatalf("CreateOrg() error = %v", err)
	}
	project, err := service.CreateProject(org.ID, "Reject Project", "reject-project")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if err := service.AddMembership(user.ID, org.ID, project.ID, "ghost-role"); err == nil {
		t.Fatal("AddMembership(ghost role) error = nil, want membership role is invalid")
	}
}

func TestServiceUpsertRoleDefinitionUpdatesList(t *testing.T) {
	service := NewService(NewMemoryRepository())

	upserted, err := service.UpsertRoleDefinition(RoleDefinition{
		Role:             "Reviewer",
		DisplayName:      "Reviewer",
		Description:      "Code review role",
		PermissionLabels: []string{"project:{project_id}:read", "project:{project_id}:write"},
	})
	if err != nil {
		t.Fatalf("UpsertRoleDefinition() error = %v", err)
	}
	if upserted.Role != "reviewer" {
		t.Fatalf("upserted role = %q, want reviewer", upserted.Role)
	}

	roles, err := service.ListRoles("project_roles")
	if err != nil {
		t.Fatalf("ListRoles() error = %v", err)
	}
	found := false
	for _, role := range roles {
		if role.Role == "reviewer" {
			found = true
			if !contains(role.PermissionLabels, "project:project_roles:write") {
				t.Fatalf("custom role labels = %#v, want expanded project write", role.PermissionLabels)
			}
		}
	}
	if !found {
		t.Fatal("ListRoles() missing custom reviewer role")
	}
}

func TestServiceUpsertRoleDefinitionValidatesRoleAndLabels(t *testing.T) {
	service := NewService(NewMemoryRepository())

	if _, err := service.UpsertRoleDefinition(RoleDefinition{}); err == nil {
		t.Fatal("UpsertRoleDefinition(empty role) error = nil, want validation error")
	}
	if _, err := service.UpsertRoleDefinition(RoleDefinition{Role: "empty-label"}); err == nil {
		t.Fatal("UpsertRoleDefinition(empty labels) error = nil, want validation error")
	}
}

type customRoleRepository struct {
	*MemoryRepository
	roleDefinitions []RoleDefinition
}

func (r *customRoleRepository) ListRoleDefinitions() ([]RoleDefinition, error) {
	return append([]RoleDefinition(nil), r.roleDefinitions...), nil
}

func TestServiceListsTenantResourcesAndOrgWritePermission(t *testing.T) {
	service := NewService(NewMemoryRepository())
	owner, _ := service.CreateUser("owner@example.com", "Owner")
	member, _ := service.CreateUser("member@example.com", "Member")
	org, _ := service.CreateOrg("Org Alpha", "org-alpha")
	project, _ := service.CreateProject(org.ID, "Project Alpha", "project-alpha")
	if err := service.AddMembership(owner.ID, org.ID, "", RoleOwner); err != nil {
		t.Fatalf("AddMembership(owner org) error = %v", err)
	}
	if err := service.AddMembership(member.ID, org.ID, project.ID, RoleMember); err != nil {
		t.Fatalf("AddMembership(member project) error = %v", err)
	}

	orgs, err := service.ListOrgs(owner.ID)
	if err != nil {
		t.Fatalf("ListOrgs() error = %v", err)
	}
	if len(orgs) != 1 || orgs[0].ID != org.ID {
		t.Fatalf("ListOrgs() = %#v, want created org", orgs)
	}
	projects, err := service.ListProjects(owner.ID, org.ID)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 1 || projects[0].ID != project.ID {
		t.Fatalf("ListProjects() = %#v, want created project", projects)
	}
	memberships, err := service.ListMemberships(org.ID, project.ID)
	if err != nil {
		t.Fatalf("ListMemberships() error = %v", err)
	}
	if len(memberships) != 1 || memberships[0].UserID != member.ID {
		t.Fatalf("ListMemberships() = %#v, want project member", memberships)
	}
	if err := service.RequireOrgWrite(owner.ID, org.ID); err != nil {
		t.Fatalf("RequireOrgWrite(owner) error = %v", err)
	}
	if err := service.RequireOrgWrite(member.ID, org.ID); err == nil {
		t.Fatal("RequireOrgWrite(member) error = nil, want forbidden")
	}
}

func TestServiceSoftDeletesTenantResources(t *testing.T) {
	service := NewService(NewMemoryRepository())
	owner, _ := service.CreateUser("owner-delete@example.com", "Owner Delete")
	member, _ := service.CreateUser("member-delete@example.com", "Member Delete")
	org, _ := service.CreateOrg("Org Delete", "org-delete")
	project, _ := service.CreateProject(org.ID, "Project Delete", "project-delete")
	if err := service.AddMembership(owner.ID, org.ID, "", RoleOwner); err != nil {
		t.Fatalf("AddMembership(owner org) error = %v", err)
	}
	if err := service.AddMembership(owner.ID, org.ID, project.ID, RoleOwner); err != nil {
		t.Fatalf("AddMembership(owner project) error = %v", err)
	}
	if err := service.AddMembership(member.ID, org.ID, project.ID, RoleMember); err != nil {
		t.Fatalf("AddMembership(member project) error = %v", err)
	}

	if _, err := service.DeleteProject(member.ID, org.ID, project.ID); err == nil {
		t.Fatal("DeleteProject(member) error = nil, want forbidden")
	}
	deletedProject, err := service.DeleteProject(owner.ID, org.ID, project.ID)
	if err != nil {
		t.Fatalf("DeleteProject(owner) error = %v", err)
	}
	if deletedProject.Status != "deleted" {
		t.Fatalf("deleted project status = %q, want deleted", deletedProject.Status)
	}
	projects, err := service.ListProjects(owner.ID, org.ID)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("ListProjects() after delete = %#v, want empty", projects)
	}
	if _, err := service.PermissionContext(owner.ID, org.ID, project.ID, "codex"); err == nil {
		t.Fatal("PermissionContext() error = nil, want deleted project rejection")
	}

	deletedOrg, err := service.DeleteOrg(owner.ID, org.ID)
	if err != nil {
		t.Fatalf("DeleteOrg(owner) error = %v", err)
	}
	if deletedOrg.Status != "deleted" {
		t.Fatalf("deleted org status = %q, want deleted", deletedOrg.Status)
	}
	orgs, err := service.ListOrgs(owner.ID)
	if err != nil {
		t.Fatalf("ListOrgs() error = %v", err)
	}
	if len(orgs) != 0 {
		t.Fatalf("ListOrgs() after delete = %#v, want empty", orgs)
	}
}

func TestServiceUpdatesTenantResources(t *testing.T) {
	service := NewService(NewMemoryRepository())
	owner, _ := service.CreateUser("owner-update@example.com", "Owner Update")
	member, _ := service.CreateUser("member-update@example.com", "Member Update")
	org, _ := service.CreateOrg("Org Update", "org-update")
	project, _ := service.CreateProject(org.ID, "Project Update", "project-update")
	if err := service.AddMembership(owner.ID, org.ID, "", RoleOwner); err != nil {
		t.Fatalf("AddMembership(owner org) error = %v", err)
	}
	if err := service.AddMembership(member.ID, org.ID, project.ID, RoleMember); err != nil {
		t.Fatalf("AddMembership(member project) error = %v", err)
	}

	if _, err := service.UpdateOrg(member.ID, org.ID, "Member Rename", "member-rename"); err == nil {
		t.Fatal("UpdateOrg(member) error = nil, want forbidden")
	}
	updatedOrg, err := service.UpdateOrg(owner.ID, org.ID, "Org Renamed", "org-renamed")
	if err != nil {
		t.Fatalf("UpdateOrg(owner) error = %v", err)
	}
	if updatedOrg.ID != org.ID || updatedOrg.Name != "Org Renamed" || updatedOrg.Slug != "org-renamed" || updatedOrg.Status != "active" {
		t.Fatalf("UpdateOrg() = %#v, want renamed active org", updatedOrg)
	}
	orgs, err := service.ListOrgs(owner.ID)
	if err != nil {
		t.Fatalf("ListOrgs() error = %v", err)
	}
	if len(orgs) != 1 || orgs[0].Name != "Org Renamed" || orgs[0].Slug != "org-renamed" {
		t.Fatalf("ListOrgs() after update = %#v, want renamed org", orgs)
	}

	if _, err := service.UpdateProject(member.ID, org.ID, project.ID, "Member Project Rename", "member-project-rename"); err == nil {
		t.Fatal("UpdateProject(member) error = nil, want forbidden")
	}
	updatedProject, err := service.UpdateProject(owner.ID, org.ID, project.ID, "Project Renamed", "project-renamed")
	if err != nil {
		t.Fatalf("UpdateProject(owner) error = %v", err)
	}
	if updatedProject.ID != project.ID || updatedProject.OrgID != org.ID || updatedProject.Name != "Project Renamed" || updatedProject.Slug != "project-renamed" || updatedProject.Status != "active" {
		t.Fatalf("UpdateProject() = %#v, want renamed active project", updatedProject)
	}
	projects, err := service.ListProjects(owner.ID, org.ID)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "Project Renamed" || projects[0].Slug != "project-renamed" {
		t.Fatalf("ListProjects() after update = %#v, want renamed project", projects)
	}

	if _, err := service.DeleteProject(owner.ID, org.ID, project.ID); err != nil {
		t.Fatalf("DeleteProject(owner) error = %v", err)
	}
	if _, err := service.UpdateProject(owner.ID, org.ID, project.ID, "Deleted Project", "deleted-project"); err == nil {
		t.Fatal("UpdateProject(deleted) error = nil, want not found")
	}
	if _, err := service.DeleteOrg(owner.ID, org.ID); err != nil {
		t.Fatalf("DeleteOrg(owner) error = %v", err)
	}
	if _, err := service.UpdateOrg(owner.ID, org.ID, "Deleted Org", "deleted-org"); err == nil {
		t.Fatal("UpdateOrg(deleted) error = nil, want not found")
	}
}

func TestServiceManagesMembershipRoleAndRemoval(t *testing.T) {
	service := NewService(NewMemoryRepository())
	owner, _ := service.CreateUser("owner-membership@example.com", "Owner Membership")
	member, _ := service.CreateUser("member-membership@example.com", "Member Membership")
	outsider, _ := service.CreateUser("outsider-membership@example.com", "Outsider Membership")
	org, _ := service.CreateOrg("Org Membership", "org-membership")
	project, _ := service.CreateProject(org.ID, "Project Membership", "project-membership")
	if err := service.AddMembership(owner.ID, org.ID, "", RoleOwner); err != nil {
		t.Fatalf("AddMembership(owner org) error = %v", err)
	}
	if err := service.AddMembership(member.ID, org.ID, project.ID, RoleMember); err != nil {
		t.Fatalf("AddMembership(member project) error = %v", err)
	}

	if _, err := service.UpdateMembershipRole(member.ID, member.ID, org.ID, project.ID, RoleAdmin); err == nil {
		t.Fatal("UpdateMembershipRole(member actor) error = nil, want forbidden")
	}
	updated, err := service.UpdateMembershipRole(owner.ID, member.ID, org.ID, project.ID, RoleAdmin)
	if err != nil {
		t.Fatalf("UpdateMembershipRole(owner) error = %v", err)
	}
	if updated.UserID != member.ID || updated.Role != RoleAdmin || updated.Status != "active" {
		t.Fatalf("UpdateMembershipRole() = %#v, want active admin membership", updated)
	}
	memberships, err := service.ListMemberships(org.ID, project.ID)
	if err != nil {
		t.Fatalf("ListMemberships() error = %v", err)
	}
	if len(memberships) != 1 || memberships[0].Role != RoleAdmin || memberships[0].Status != "active" {
		t.Fatalf("ListMemberships() after role update = %#v, want active admin", memberships)
	}

	if _, err := service.RemoveMembership(outsider.ID, member.ID, org.ID, project.ID); err == nil {
		t.Fatal("RemoveMembership(outsider actor) error = nil, want forbidden")
	}
	removed, err := service.RemoveMembership(owner.ID, member.ID, org.ID, project.ID)
	if err != nil {
		t.Fatalf("RemoveMembership(owner) error = %v", err)
	}
	if removed.UserID != member.ID || removed.Status != "disabled" || removed.Role != RoleAdmin {
		t.Fatalf("RemoveMembership() = %#v, want disabled admin membership", removed)
	}
	if _, err := service.PermissionContext(member.ID, org.ID, project.ID, "codex"); err == nil {
		t.Fatal("PermissionContext(removed member) error = nil, want disabled membership rejection")
	}
}

func TestServiceProjectOwnerCanManageProjectMembershipWithoutOrgMembership(t *testing.T) {
	service := NewService(NewMemoryRepository())
	projectOwner, _ := service.CreateUser("project-owner-membership@example.com", "Project Owner Membership")
	member, _ := service.CreateUser("project-member-membership@example.com", "Project Member Membership")
	org, _ := service.CreateOrg("Project Membership Scope", "project-membership-scope")
	project, _ := service.CreateProject(org.ID, "Project Membership Scope Project", "project-membership-scope-project")
	if err := service.AddMembership(projectOwner.ID, org.ID, project.ID, RoleOwner); err != nil {
		t.Fatalf("AddMembership(project owner) error = %v", err)
	}
	if err := service.AddMembership(member.ID, org.ID, project.ID, RoleMember); err != nil {
		t.Fatalf("AddMembership(member project) error = %v", err)
	}

	updated, err := service.UpdateMembershipRole(projectOwner.ID, member.ID, org.ID, project.ID, RoleAdmin)
	if err != nil {
		t.Fatalf("UpdateMembershipRole(project owner) error = %v", err)
	}
	if updated.Role != RoleAdmin || updated.Status != "active" {
		t.Fatalf("UpdateMembershipRole(project owner) = %#v, want active admin membership", updated)
	}

	removed, err := service.RemoveMembership(projectOwner.ID, member.ID, org.ID, project.ID)
	if err != nil {
		t.Fatalf("RemoveMembership(project owner) error = %v", err)
	}
	if removed.Role != RoleAdmin || removed.Status != "disabled" {
		t.Fatalf("RemoveMembership(project owner) = %#v, want disabled admin membership", removed)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
