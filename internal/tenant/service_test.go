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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
