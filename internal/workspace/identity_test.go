package workspace

import "testing"

func TestResolveNormalizesGitRemoteIdentity(t *testing.T) {
	identity, err := Resolve(Identity{
		CWD:       "/Users/kanyun/Memory OS",
		GitRoot:   "/Users/kanyun/Memory OS",
		GitRemote: "git@gitlab.example.com:Team/Memory-OS.git",
		GitBranch: "main",
		GitCommit: "abc123",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if identity.SourceType != "git" {
		t.Fatalf("SourceType = %q, want git", identity.SourceType)
	}
	if identity.SourceKey != "gitlab.example.com/team/memory-os" {
		t.Fatalf("SourceKey = %q, want normalized git key", identity.SourceKey)
	}
	if identity.ProjectName != "Memory OS" {
		t.Fatalf("ProjectName = %q, want Memory OS", identity.ProjectName)
	}
	if identity.ProjectSlug != "gitlab-example-com-team-memory-os" {
		t.Fatalf("ProjectSlug = %q, want gitlab-example-com-team-memory-os", identity.ProjectSlug)
	}
}

func TestResolveDropsGitRemoteCredentials(t *testing.T) {
	identity, err := Resolve(Identity{
		GitRemote: "https://user:secret-token@gitlab.example.com/team/memory-os.git",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if identity.SourceKey != "gitlab.example.com/team/memory-os" {
		t.Fatalf("SourceKey = %q, want credential-free git key", identity.SourceKey)
	}
}

func TestResolveFallsBackToLocalWorkspaceWhenGitRemoteMissing(t *testing.T) {
	identity, err := Resolve(Identity{CWD: "/tmp/no-git"})

	if err != nil {
		t.Fatalf("Resolve() error = %v, want local fallback", err)
	}
	if identity.SourceType != "local" || identity.SourceKey == "" {
		t.Fatalf("identity = %#v, want local source", identity)
	}
}

func TestResolveFallsBackToInboxWhenWorkspaceContextMissing(t *testing.T) {
	identity, err := Resolve(Identity{})

	if err != nil {
		t.Fatalf("Resolve() error = %v, want inbox fallback", err)
	}
	if identity.SourceType != "inbox" || identity.SourceKey != "inbox/general" {
		t.Fatalf("identity = %#v, want inbox source", identity)
	}
}
