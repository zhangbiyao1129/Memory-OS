package tenant

import (
	"testing"

	"memory-os/internal/workspace"
)

func TestServiceEnsureWorkspaceProjectCreatesPersonalProject(t *testing.T) {
	service := NewService(NewMemoryRepository())
	user, err := service.CreateUser("workspace-user@example.test", "Workspace User")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	ctx, err := service.EnsureWorkspaceProject(user.ID, "claude-code", workspace.Identity{
		GitRemote: "git@gitlab.example.com:team/memory-os.git",
		GitRoot:   "/work/memory-os",
		CWD:       "/work/memory-os",
		GitBranch: "main",
	})
	if err != nil {
		t.Fatalf("EnsureWorkspaceProject() error = %v", err)
	}

	if ctx.UserID != user.ID || ctx.AgentID != "claude-code" || ctx.OrgID == "" || ctx.ProjectID == "" {
		t.Fatalf("permission context ids mismatch: %#v", ctx)
	}
	if !contains(ctx.PermissionLabels, "project:"+ctx.ProjectID+":write") {
		t.Fatalf("permission labels missing project write: %#v", ctx.PermissionLabels)
	}

	projects, err := service.ListProjects(user.ID, ctx.OrgID)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %#v, want one workspace project", projects)
	}
	if projects[0].SourceType != "git" || projects[0].SourceKey != "gitlab.example.com/team/memory-os" {
		t.Fatalf("workspace project source mismatch: %#v", projects[0])
	}

	second, err := service.EnsureWorkspaceProject(user.ID, "codex", workspace.Identity{
		GitRemote: "https://gitlab.example.com/team/memory-os.git",
	})
	if err != nil {
		t.Fatalf("EnsureWorkspaceProject(second) error = %v", err)
	}
	if second.ProjectID != ctx.ProjectID || second.OrgID != ctx.OrgID {
		t.Fatalf("EnsureWorkspaceProject is not idempotent: first=%#v second=%#v", ctx, second)
	}
}

func TestServiceEnsureWorkspaceProjectRejectsMissingRemote(t *testing.T) {
	service := NewService(NewMemoryRepository())
	user, err := service.CreateUser("workspace-missing@example.test", "Workspace Missing")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	_, err = service.EnsureWorkspaceProject(user.ID, "codex", workspace.Identity{CWD: "/tmp/no-git"})

	if err == nil {
		t.Fatal("EnsureWorkspaceProject() error = nil, want missing remote rejection")
	}
}
