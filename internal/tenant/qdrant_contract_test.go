package tenant

import (
	"testing"

	"memory-os/internal/qdrant"
)

func TestPermissionContextBuildsQdrantFilter(t *testing.T) {
	service := NewService(NewMemoryRepository())
	user, _ := service.CreateUser("alice@example.com", "Alice")
	org, _ := service.CreateOrg("Org Alpha", "org-alpha")
	project, _ := service.CreateProject(org.ID, "Project Alpha", "project-alpha")
	if err := service.AddMembership(user.ID, org.ID, project.ID, RoleMember); err != nil {
		t.Fatalf("AddMembership() error = %v", err)
	}
	permissions, err := service.PermissionContext(user.ID, org.ID, project.ID, "codex")
	if err != nil {
		t.Fatalf("PermissionContext() error = %v", err)
	}

	filter, err := qdrant.BuildPayloadFilter(qdrant.FilterContext{
		UserID:           permissions.UserID,
		OrgID:            permissions.OrgID,
		ProjectID:        permissions.ProjectID,
		Visibility:       "project",
		PermissionLabels: permissions.PermissionLabels,
		DocType:          "archive_chunk",
	})

	if err != nil {
		t.Fatalf("BuildPayloadFilter() error = %v", err)
	}
	if len(filter.Must["permission_labels"]) == 0 {
		t.Fatal("qdrant filter permission labels are empty")
	}
}
