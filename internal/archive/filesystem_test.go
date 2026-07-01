package archive

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildPathUsesExpectedLayout(t *testing.T) {
	path, err := BuildPath("/tmp/archive-root", PathContext{
		OrgID:     "org_alpha",
		ProjectID: "project_alpha",
		UserID:    "user_alice",
		ArchiveID: "archive_1",
		CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildPath() error = %v", err)
	}

	wantSuffix := filepath.Join("archives", "org_org_alpha", "project_project_alpha", "user_user_alice", "2026", "07", "archive_archive_1.md")
	if !strings.HasSuffix(path, wantSuffix) {
		t.Fatalf("path = %q, want suffix %q", path, wantSuffix)
	}
}

func TestBuildPathRejectsTraversal(t *testing.T) {
	_, err := BuildPath("/tmp/archive-root", PathContext{
		OrgID:     "../org",
		ProjectID: "project_alpha",
		UserID:    "user_alice",
		ArchiveID: "archive_1",
		CreatedAt: time.Now(),
	})

	if err == nil {
		t.Fatal("BuildPath() error = nil, want traversal rejection")
	}
}

func TestBuildPathRejectsEmptyID(t *testing.T) {
	_, err := BuildPath("/tmp/archive-root", PathContext{
		OrgID:     "org_alpha",
		ProjectID: "",
		UserID:    "user_alice",
		ArchiveID: "archive_1",
		CreatedAt: time.Now(),
	})

	if err == nil {
		t.Fatal("BuildPath() error = nil, want empty project id rejection")
	}
}
