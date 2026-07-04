package retrieval

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGArchiveGenerationResolverReturnsCurrentScopedGeneration(t *testing.T) {
	pool := retrievalTestPool(t)
	resolver := NewPGArchiveGenerationResolver(pool)
	suffix := retrievalSuffix()
	scope := ArchiveGenerationContext{UserID: "user_" + suffix, OrgID: "org_" + suffix, ProjectID: "project_" + suffix}

	insertArchiveGenerationForResolver(t, pool, scope, "archive_current_1_"+suffix, 1, "active")
	insertArchiveGenerationForResolver(t, pool, scope, "archive_current_3_"+suffix, 3, "active")
	insertArchiveGenerationForResolver(t, pool, ArchiveGenerationContext{UserID: "other_" + suffix, OrgID: scope.OrgID, ProjectID: scope.ProjectID}, "archive_other_user_"+suffix, 9, "active")
	insertArchiveGenerationForResolver(t, pool, scope, "archive_deleted_"+suffix, 8, "deleted")

	generation, err := resolver.CurrentGeneration(scope)
	if err != nil {
		t.Fatalf("CurrentGeneration() error = %v", err)
	}

	if generation != 3 {
		t.Fatalf("generation = %d, want 3", generation)
	}
}

func TestPGArchiveGenerationResolverReturnsZeroWhenScopeHasNoArchive(t *testing.T) {
	pool := retrievalTestPool(t)
	resolver := NewPGArchiveGenerationResolver(pool)

	generation, err := resolver.CurrentGeneration(ArchiveGenerationContext{UserID: "user_none", OrgID: "org_none", ProjectID: "project_none"})
	if err != nil {
		t.Fatalf("CurrentGeneration() error = %v", err)
	}

	if generation != 0 {
		t.Fatalf("generation = %d, want 0", generation)
	}
}

func TestPGArchiveGenerationResolverRequiresScope(t *testing.T) {
	resolver := NewPGArchiveGenerationResolver(nil)

	if _, err := resolver.CurrentGeneration(ArchiveGenerationContext{}); err == nil {
		t.Fatal("CurrentGeneration() error = nil, want missing pool/scope error")
	}
}

func insertArchiveGenerationForResolver(t *testing.T, pool *pgxpool.Pool, scope ArchiveGenerationContext, archiveID string, generation int, status string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO archives (archive_id, user_id, org_id, project_id, title, file_path, status, index_generation, current_version, content_hash, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$10,$11)
ON CONFLICT (archive_id) DO UPDATE SET
    status = EXCLUDED.status,
    index_generation = EXCLUDED.index_generation,
    updated_at = EXCLUDED.updated_at`,
		archiveID,
		scope.UserID,
		scope.OrgID,
		scope.ProjectID,
		"Archive "+archiveID,
		"/tmp/"+archiveID+".md",
		status,
		generation,
		"hash_"+archiveID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert archive generation fixture: %v", err)
	}
}
