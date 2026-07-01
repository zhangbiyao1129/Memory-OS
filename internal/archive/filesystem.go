package archive

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

type PathContext struct {
	OrgID     string
	ProjectID string
	UserID    string
	ArchiveID string
	CreatedAt time.Time
}

var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func BuildPath(root string, ctx PathContext) (string, error) {
	if root == "" {
		return "", errors.New("archive root is required")
	}
	for name, value := range map[string]string{"org_id": ctx.OrgID, "project_id": ctx.ProjectID, "user_id": ctx.UserID, "archive_id": ctx.ArchiveID} {
		if !safeIDPattern.MatchString(value) {
			return "", fmt.Errorf("%s is invalid", name)
		}
	}
	if ctx.CreatedAt.IsZero() {
		ctx.CreatedAt = time.Now().UTC()
	}

	path := filepath.Join(root, "archives", "org_"+ctx.OrgID, "project_"+ctx.ProjectID, "user_"+ctx.UserID, ctx.CreatedAt.Format("2006"), ctx.CreatedAt.Format("01"), "archive_"+ctx.ArchiveID+".md")
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(cleanRoot, cleanPath); err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == "../" {
		return "", errors.New("archive path escapes root")
	}
	return cleanPath, nil
}
