package tenant

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGRepositoryRequiresPool(t *testing.T) {
	repo := NewPGRepository(nil)

	if _, err := repo.CreateUser(User{Email: "alice@example.com", DisplayName: "Alice"}); err == nil {
		t.Fatal("CreateUser() error = nil, want missing pool error")
	}
	if _, err := repo.ListUsers(""); err == nil {
		t.Fatal("ListUsers() error = nil, want missing pool error")
	}
	if _, err := repo.FindUserByEmail("alice@example.com"); err == nil {
		t.Fatal("FindUserByEmail() error = nil, want missing pool error")
	}
	if _, err := repo.GetProject("project_1"); err == nil {
		t.Fatal("GetProject() error = nil, want missing pool error")
	}
	if _, err := repo.ListOrgs("user_1"); err == nil {
		t.Fatal("ListOrgs() error = nil, want missing pool error")
	}
	if _, err := repo.ListProjects("user_1", "org_1"); err == nil {
		t.Fatal("ListProjects() error = nil, want missing pool error")
	}
	if _, err := repo.ListMemberships("org_1", "project_1"); err == nil {
		t.Fatal("ListMemberships() error = nil, want missing pool error")
	}
	if _, err := repo.DeleteProject("org_1", "project_1"); err == nil {
		t.Fatal("DeleteProject() error = nil, want missing pool error")
	}
	if _, err := repo.DeleteOrg("org_1"); err == nil {
		t.Fatal("DeleteOrg() error = nil, want missing pool error")
	}
	if _, err := repo.UpdateOrg(Org{ID: "org_1", Name: "Org", Slug: "org"}); err == nil {
		t.Fatal("UpdateOrg() error = nil, want missing pool error")
	}
	if _, err := repo.UpdateProject(Project{ID: "project_1", OrgID: "org_1", Name: "Project", Slug: "project"}); err == nil {
		t.Fatal("UpdateProject() error = nil, want missing pool error")
	}
	if _, err := repo.UpdateMembershipRole(Membership{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", Role: RoleAdmin}); err == nil {
		t.Fatal("UpdateMembershipRole() error = nil, want missing pool error")
	}
	if _, err := repo.RemoveMembership("user_1", "org_1", "project_1"); err == nil {
		t.Fatal("RemoveMembership() error = nil, want missing pool error")
	}
}

func TestPGRepositoryListsRoleDefinitions(t *testing.T) {
	pool := tenantTestPool(t)
	repo := NewPGRepository(pool)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	roleName := "reviewer-" + suffix

	_, err := pool.Exec(context.Background(), `
INSERT INTO roles (name, permission_labels)
VALUES ($1, ARRAY['project:{project_id}:read', 'project:{project_id}:write'])
ON CONFLICT (name) DO UPDATE SET permission_labels = EXCLUDED.permission_labels`, roleName)
	if err != nil {
		t.Fatalf("insert custom role failed: %v", err)
	}

	roles, err := repo.ListRoleDefinitions()
	if err != nil {
		t.Fatalf("ListRoleDefinitions() error = %v", err)
	}
	foundCustom := false
	for _, role := range roles {
		if role.Role == roleName {
			foundCustom = true
			if len(role.PermissionLabels) != 2 {
				t.Fatalf("custom role labels = %#v, want 2 labels", role.PermissionLabels)
			}
			if role.PermissionLabels[0] != "project:{project_id}:read" || role.PermissionLabels[1] != "project:{project_id}:write" {
				t.Fatalf("custom role labels = %#v, want keep stored labels", role.PermissionLabels)
			}
			break
		}
	}
	if !foundCustom {
		t.Fatalf("ListRoleDefinitions() roles = %#v, want custom role %s", roles, roleName)
	}
}

func TestPGRepositoryUpsertRoleDefinitionOverridesPermissionLabels(t *testing.T) {
	pool := tenantTestPool(t)
	repo := NewPGRepository(pool)
	roleName := "reviewer-upsert-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	inserted, err := repo.UpsertRoleDefinition(RoleDefinition{
		Role:             roleName,
		PermissionLabels: []string{"project:{project_id}:read"},
	})
	if err != nil {
		t.Fatalf("UpsertRoleDefinition(insert) error = %v", err)
	}
	if inserted.Role != roleName || len(inserted.PermissionLabels) != 1 {
		t.Fatalf("inserted role = %#v, want role_name and single label", inserted)
	}

	updated, err := repo.UpsertRoleDefinition(RoleDefinition{
		Role:             roleName,
		PermissionLabels: []string{"project:{project_id}:read", "project:{project_id}:write"},
	})
	if err != nil {
		t.Fatalf("UpsertRoleDefinition(update) error = %v", err)
	}
	if updated.Role != roleName || len(updated.PermissionLabels) != 2 {
		t.Fatalf("updated role = %#v, want role_name and two labels", updated)
	}

	row := pool.QueryRow(context.Background(), "SELECT permission_labels FROM roles WHERE name = $1", roleName)
	var labels []string
	if err := row.Scan(&labels); err != nil {
		t.Fatalf("scan role labels: %v", err)
	}
	if len(labels) != 2 || labels[0] != "project:{project_id}:read" || labels[1] != "project:{project_id}:write" {
		t.Fatalf("stored permission labels = %#v, want read+write in order", labels)
	}
}

func TestPGRepositoryCreatesTenantGraph(t *testing.T) {
	pool := tenantTestPool(t)
	service := NewService(NewPGRepository(pool))
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)

	user, err := service.CreateUser("alice-tenant-"+suffix+"@example.com", "Alice Tenant")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	org, err := service.CreateOrg("Tenant PG Org "+suffix, "tenant-pg-org-"+suffix)
	if err != nil {
		t.Fatalf("CreateOrg() error = %v", err)
	}
	project, err := service.CreateProject(org.ID, "Tenant PG Project "+suffix, "tenant-pg-project-"+suffix)
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if err := service.AddMembership(user.ID, org.ID, project.ID, RoleOwner); err != nil {
		t.Fatalf("AddMembership() error = %v", err)
	}
	if err := service.AddMembership(user.ID, org.ID, "", RoleOwner); err != nil {
		t.Fatalf("AddMembership(org owner) error = %v", err)
	}

	ctx, err := service.PermissionContext(user.ID, org.ID, project.ID, "codex")
	if err != nil {
		t.Fatalf("PermissionContext() error = %v", err)
	}
	if ctx.UserID != user.ID || ctx.OrgID != org.ID || ctx.ProjectID != project.ID {
		t.Fatalf("permission context ids mismatch: %#v", ctx)
	}
	if !contains(ctx.PermissionLabels, "project:"+project.ID+":write") {
		t.Fatalf("owner labels missing project write: %#v", ctx.PermissionLabels)
	}

	if _, err := service.CreateUser("ALICE-tenant-"+suffix+"@example.com", "Duplicate"); err == nil {
		t.Fatal("CreateUser() error = nil, want duplicate email rejection")
	}

	users, err := service.ListUsers("")
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) == 0 || users[len(users)-1].ID != user.ID {
		t.Fatalf("ListUsers() = %#v, want created user in result", users)
	}

	orgs, err := service.ListOrgs(user.ID)
	if err != nil {
		t.Fatalf("ListOrgs() error = %v", err)
	}
	if len(orgs) == 0 || orgs[0].ID != org.ID {
		t.Fatalf("ListOrgs() = %#v, want created org", orgs)
	}
	projects, err := service.ListProjects(user.ID, org.ID)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) == 0 || projects[0].ID != project.ID {
		t.Fatalf("ListProjects() = %#v, want created project", projects)
	}
	memberships, err := service.ListMemberships(org.ID, project.ID)
	if err != nil {
		t.Fatalf("ListMemberships() error = %v", err)
	}
	if len(memberships) == 0 || memberships[0].UserID != user.ID {
		t.Fatalf("ListMemberships() = %#v, want created membership", memberships)
	}
	if err := service.RequireOrgWrite(user.ID, org.ID); err != nil {
		t.Fatalf("RequireOrgWrite() error = %v", err)
	}
}

func TestPGRepositoryUpdatesUserStatus(t *testing.T) {
	pool := tenantTestPool(t)
	service := NewService(NewPGRepository(pool))
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)

	user, err := service.CreateUser("status-tenant-"+suffix+"@example.com", "Status Tenant")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	disabled, err := service.UpdateUserStatus(user.ID, "disabled")
	if err != nil {
		t.Fatalf("UpdateUserStatus(disabled) error = %v", err)
	}
	if disabled.ID != user.ID || disabled.Status != "disabled" {
		t.Fatalf("UpdateUserStatus(disabled) = %#v, want disabled same user", disabled)
	}
	disabledUsers, err := service.ListUsers("disabled")
	if err != nil {
		t.Fatalf("ListUsers(disabled) error = %v", err)
	}
	foundDisabled := false
	for _, item := range disabledUsers {
		if item.ID == user.ID {
			foundDisabled = true
			break
		}
	}
	if !foundDisabled {
		t.Fatalf("ListUsers(disabled) missing updated user %#v in %#v", user, disabledUsers)
	}
	active, err := service.UpdateUserStatus(user.ID, "active")
	if err != nil {
		t.Fatalf("UpdateUserStatus(active) error = %v", err)
	}
	if active.ID != user.ID || active.Status != "active" {
		t.Fatalf("UpdateUserStatus(active) = %#v, want active same user", active)
	}
}

func TestPGRepositoryFindsUserByEmail(t *testing.T) {
	pool := tenantTestPool(t)
	service := NewService(NewPGRepository(pool))
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)

	user, err := service.CreateUser("lookup-tenant-"+suffix+"@example.com", "Lookup Tenant")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	found, err := service.FindUserByEmail(" LOOKUP-tenant-" + suffix + "@example.com ")
	if err != nil {
		t.Fatalf("FindUserByEmail() error = %v", err)
	}
	if found.ID != user.ID || found.Email != user.Email {
		t.Fatalf("FindUserByEmail() = %#v, want %#v", found, user)
	}

	if _, err := service.FindUserByEmail("missing-tenant-" + suffix + "@example.com"); err == nil {
		t.Fatal("FindUserByEmail(missing) error = nil, want not found")
	}
}

func TestPGRepositorySoftDeletesTenantResources(t *testing.T) {
	pool := tenantTestPool(t)
	service := NewService(NewPGRepository(pool))
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)

	user, _ := service.CreateUser("delete-tenant-"+suffix+"@example.com", "Delete Tenant")
	org, _ := service.CreateOrg("Tenant Delete Org "+suffix, "tenant-delete-org-"+suffix)
	project, _ := service.CreateProject(org.ID, "Tenant Delete Project "+suffix, "tenant-delete-project-"+suffix)
	if err := service.AddMembership(user.ID, org.ID, "", RoleOwner); err != nil {
		t.Fatalf("AddMembership(org owner) error = %v", err)
	}
	if err := service.AddMembership(user.ID, org.ID, project.ID, RoleOwner); err != nil {
		t.Fatalf("AddMembership(project owner) error = %v", err)
	}

	deletedProject, err := service.DeleteProject(user.ID, org.ID, project.ID)
	if err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}
	if deletedProject.Status != "deleted" {
		t.Fatalf("deleted project status = %q, want deleted", deletedProject.Status)
	}
	projects, err := service.ListProjects(user.ID, org.ID)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("ListProjects() after project delete = %#v, want empty", projects)
	}
	assertTenantMembershipCount(t, pool, org.ID, project.ID, 1)

	deletedOrg, err := service.DeleteOrg(user.ID, org.ID)
	if err != nil {
		t.Fatalf("DeleteOrg() error = %v", err)
	}
	if deletedOrg.Status != "deleted" {
		t.Fatalf("deleted org status = %q, want deleted", deletedOrg.Status)
	}
	orgs, err := service.ListOrgs(user.ID)
	if err != nil {
		t.Fatalf("ListOrgs() error = %v", err)
	}
	if len(orgs) != 0 {
		t.Fatalf("ListOrgs() after org delete = %#v, want empty", orgs)
	}
	assertTenantMembershipCount(t, pool, org.ID, project.ID, 1)
}

func TestPGRepositoryUpdatesTenantResources(t *testing.T) {
	pool := tenantTestPool(t)
	service := NewService(NewPGRepository(pool))
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)

	user, _ := service.CreateUser("update-tenant-"+suffix+"@example.com", "Update Tenant")
	org, _ := service.CreateOrg("Tenant Update Org "+suffix, "tenant-update-org-"+suffix)
	project, _ := service.CreateProject(org.ID, "Tenant Update Project "+suffix, "tenant-update-project-"+suffix)
	if err := service.AddMembership(user.ID, org.ID, "", RoleOwner); err != nil {
		t.Fatalf("AddMembership(org owner) error = %v", err)
	}
	if err := service.AddMembership(user.ID, org.ID, project.ID, RoleOwner); err != nil {
		t.Fatalf("AddMembership(project owner) error = %v", err)
	}

	updatedOrg, err := service.UpdateOrg(user.ID, org.ID, "Tenant Updated Org "+suffix, "tenant-updated-org-"+suffix)
	if err != nil {
		t.Fatalf("UpdateOrg() error = %v", err)
	}
	if updatedOrg.ID != org.ID || updatedOrg.Name != "Tenant Updated Org "+suffix || updatedOrg.Slug != "tenant-updated-org-"+suffix || updatedOrg.Status != "active" {
		t.Fatalf("UpdateOrg() = %#v, want renamed active org", updatedOrg)
	}
	updatedProject, err := service.UpdateProject(user.ID, org.ID, project.ID, "Tenant Updated Project "+suffix, "tenant-updated-project-"+suffix)
	if err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}
	if updatedProject.ID != project.ID || updatedProject.OrgID != org.ID || updatedProject.Name != "Tenant Updated Project "+suffix || updatedProject.Slug != "tenant-updated-project-"+suffix || updatedProject.Status != "active" {
		t.Fatalf("UpdateProject() = %#v, want renamed active project", updatedProject)
	}

	if _, err := service.DeleteProject(user.ID, org.ID, project.ID); err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}
	if _, err := service.UpdateProject(user.ID, org.ID, project.ID, "Deleted Project "+suffix, "deleted-project-"+suffix); err == nil {
		t.Fatal("UpdateProject(deleted) error = nil, want not found")
	}
	if _, err := service.DeleteOrg(user.ID, org.ID); err != nil {
		t.Fatalf("DeleteOrg() error = %v", err)
	}
	if _, err := service.UpdateOrg(user.ID, org.ID, "Deleted Org "+suffix, "deleted-org-"+suffix); err == nil {
		t.Fatal("UpdateOrg(deleted) error = nil, want not found")
	}
}

func TestPGRepositoryManagesMembershipRoleAndRemoval(t *testing.T) {
	pool := tenantTestPool(t)
	service := NewService(NewPGRepository(pool))
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)

	owner, _ := service.CreateUser("owner-membership-"+suffix+"@example.com", "Owner Membership")
	member, _ := service.CreateUser("member-membership-"+suffix+"@example.com", "Member Membership")
	org, _ := service.CreateOrg("Tenant Membership Org "+suffix, "tenant-membership-org-"+suffix)
	project, _ := service.CreateProject(org.ID, "Tenant Membership Project "+suffix, "tenant-membership-project-"+suffix)
	if err := service.AddMembership(owner.ID, org.ID, "", RoleOwner); err != nil {
		t.Fatalf("AddMembership(owner org) error = %v", err)
	}
	if err := service.AddMembership(member.ID, org.ID, project.ID, RoleMember); err != nil {
		t.Fatalf("AddMembership(member project) error = %v", err)
	}

	updated, err := service.UpdateMembershipRole(owner.ID, member.ID, org.ID, project.ID, RoleAdmin)
	if err != nil {
		t.Fatalf("UpdateMembershipRole() error = %v", err)
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

	removed, err := service.RemoveMembership(owner.ID, member.ID, org.ID, project.ID)
	if err != nil {
		t.Fatalf("RemoveMembership() error = %v", err)
	}
	if removed.UserID != member.ID || removed.Status != "disabled" || removed.Role != RoleAdmin {
		t.Fatalf("RemoveMembership() = %#v, want disabled admin membership", removed)
	}
	if _, err := service.PermissionContext(member.ID, org.ID, project.ID, "codex"); err == nil {
		t.Fatal("PermissionContext(removed member) error = nil, want disabled membership rejection")
	}
	assertTenantMembershipCount(t, pool, org.ID, project.ID, 1)
}

func TestPGRepositoryRejectsCrossProjectMembershipLookup(t *testing.T) {
	pool := tenantTestPool(t)
	service := NewService(NewPGRepository(pool))
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	user, _ := service.CreateUser("bob-tenant-"+suffix+"@example.com", "Bob Tenant")
	org, _ := service.CreateOrg("Tenant PG Org B "+suffix, "tenant-pg-org-b-"+suffix)
	projectAlpha, _ := service.CreateProject(org.ID, "Tenant PG Alpha "+suffix, "tenant-pg-alpha-"+suffix)
	projectBeta, _ := service.CreateProject(org.ID, "Tenant PG Beta "+suffix, "tenant-pg-beta-"+suffix)
	if err := service.AddMembership(user.ID, org.ID, projectAlpha.ID, RoleMember); err != nil {
		t.Fatalf("AddMembership() error = %v", err)
	}

	_, err := service.PermissionContext(user.ID, org.ID, projectBeta.ID, "codex")

	if err == nil {
		t.Fatal("PermissionContext() error = nil, want cross project rejection")
	}
}

func assertTenantMembershipCount(t *testing.T, pool *pgxpool.Pool, orgID, projectID string, want int) {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(), `
SELECT count(*)
FROM memberships
WHERE org_id = $1::uuid AND project_id = $2::uuid`, orgID, projectID).Scan(&count)
	if err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if count != want {
		t.Fatalf("membership count = %d, want %d", count, want)
	}
}

func tenantTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
