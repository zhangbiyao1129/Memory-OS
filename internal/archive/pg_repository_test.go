package archive

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
	metadata := archivePGMetadata("archive_missing_pool")
	version := archivePGVersion(metadata)

	if _, _, err := repo.SaveCreate(metadata, version, []string{"event_1"}, "request_missing_pool"); err == nil {
		t.Fatal("SaveCreate() error = nil, want missing pool error")
	}
	if _, err := repo.Get(metadata.ArchiveID); err == nil {
		t.Fatal("Get() error = nil, want missing pool error")
	}
	if _, err := repo.List(ListFilter{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1"}); err == nil {
		t.Fatal("List() error = nil, want missing pool error")
	}
	if _, _, err := repo.SaveEdit(metadata, version, EditAuditLog{}, "edit_missing_pool"); err == nil {
		t.Fatal("SaveEdit() error = nil, want missing pool error")
	}
	if _, _, err := repo.SoftDelete(metadata, EditAuditLog{}, "delete_missing_pool"); err == nil {
		t.Fatal("SoftDelete() error = nil, want missing pool error")
	}
	if _, _, err := repo.MarkReindex(metadata, "reindex_missing_pool", "manual rebuild"); err == nil {
		t.Fatal("MarkReindex() error = nil, want missing pool error")
	}
	if _, err := repo.Versions(metadata.ArchiveID); err == nil {
		t.Fatal("Versions() error = nil, want missing pool error")
	}
}

func TestPGRepositorySaveCreatePersistsArchiveGraphAndDedupesRequestID(t *testing.T) {
	pool := archiveTestPool(t)
	repo := NewPGRepository(pool)
	metadata := archivePGMetadata("archive_create_" + archiveSuffix())
	version := archivePGVersion(metadata)
	eventIDs := []string{"event_a_" + archiveSuffix(), "event_b_" + archiveSuffix()}

	saved, deduped, err := repo.SaveCreate(metadata, version, eventIDs, "create_"+metadata.ArchiveID)
	if err != nil {
		t.Fatalf("SaveCreate() error = %v", err)
	}
	if deduped {
		t.Fatal("SaveCreate() deduped = true, want false")
	}
	if saved.ArchiveID != metadata.ArchiveID || saved.CurrentVersion != 1 || saved.IndexGeneration != 1 {
		t.Fatalf("saved metadata mismatch: %#v", saved)
	}

	duplicate, deduped, err := repo.SaveCreate(metadata, version, eventIDs, "create_"+metadata.ArchiveID)
	if err != nil {
		t.Fatalf("SaveCreate() duplicate error = %v", err)
	}
	if !deduped || duplicate.ArchiveID != metadata.ArchiveID {
		t.Fatalf("duplicate = %#v deduped=%v, want same archive deduped", duplicate, deduped)
	}

	stored, err := repo.Get(metadata.ArchiveID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ContentHash != metadata.ContentHash || stored.FilePath != metadata.FilePath {
		t.Fatalf("stored metadata mismatch: %#v", stored)
	}
	assertArchiveRowCount(t, pool, "archive_versions", metadata.ArchiveID, 1)
	assertArchiveRowCount(t, pool, "archive_events", metadata.ArchiveID, len(eventIDs))
	assertArchiveRowCount(t, pool, "archive_index_generations", metadata.ArchiveID, 1)
}

func TestPGRepositoryListSoftDeleteAndMarkReindex(t *testing.T) {
	pool := archiveTestPool(t)
	repo := NewPGRepository(pool)
	metadata := archivePGMetadata("archive_manage_" + archiveSuffix())
	version := archivePGVersion(metadata)
	if _, _, err := repo.SaveCreate(metadata, version, []string{"event_" + archiveSuffix()}, "create_"+metadata.ArchiveID); err != nil {
		t.Fatalf("SaveCreate() error = %v", err)
	}

	listed, err := repo.List(ListFilter{OrgID: metadata.OrgID, ProjectID: metadata.ProjectID, UserID: metadata.UserID, Status: "active"})
	if err != nil {
		t.Fatalf("List(active) error = %v", err)
	}
	if len(listed) != 1 || listed[0].ArchiveID != metadata.ArchiveID {
		t.Fatalf("listed mismatch: %#v", listed)
	}

	reindexed := metadata
	reindexed.IndexGeneration = 2
	reindexed.UpdatedAt = time.Now().UTC()
	savedReindex, deduped, err := repo.MarkReindex(reindexed, "reindex_"+metadata.ArchiveID, "manual rebuild")
	if err != nil {
		t.Fatalf("MarkReindex() error = %v", err)
	}
	if deduped || savedReindex.IndexGeneration != 2 {
		t.Fatalf("saved reindex = %#v deduped=%v", savedReindex, deduped)
	}
	duplicateReindex, deduped, err := repo.MarkReindex(reindexed, "reindex_"+metadata.ArchiveID, "manual rebuild")
	if err != nil {
		t.Fatalf("MarkReindex() duplicate error = %v", err)
	}
	if !deduped || duplicateReindex.IndexGeneration != 2 {
		t.Fatalf("duplicate reindex = %#v deduped=%v", duplicateReindex, deduped)
	}
	assertArchiveRowCount(t, pool, "archive_index_generations", metadata.ArchiveID, 2)

	deleted := savedReindex
	deleted.Status = "deleted"
	deleted.UpdatedAt = time.Now().UTC()
	audit := EditAuditLog{ArchiveID: deleted.ArchiveID, ActorUserID: deleted.UserID, OldVersion: deleted.CurrentVersion, NewVersion: deleted.CurrentVersion, OldContentHash: deleted.ContentHash, NewContentHash: deleted.ContentHash, RequestID: "delete_" + deleted.ArchiveID, Reason: "cleanup", CreatedAt: deleted.UpdatedAt}
	savedDelete, deduped, err := repo.SoftDelete(deleted, audit, audit.RequestID)
	if err != nil {
		t.Fatalf("SoftDelete() error = %v", err)
	}
	if deduped || savedDelete.Status != "deleted" {
		t.Fatalf("saved delete = %#v deduped=%v", savedDelete, deduped)
	}
	active, err := repo.List(ListFilter{OrgID: metadata.OrgID, ProjectID: metadata.ProjectID, UserID: metadata.UserID, Status: "active"})
	if err != nil {
		t.Fatalf("List(active after delete) error = %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active archives after delete = %#v, want none", active)
	}
	assertArchiveRowCount(t, pool, "archive_versions", metadata.ArchiveID, 1)
	assertArchiveRowCount(t, pool, "archive_edit_audit_logs", metadata.ArchiveID, 1)
}

func TestPGRepositorySaveEditCreatesVersionAuditAndIndexGeneration(t *testing.T) {
	pool := archiveTestPool(t)
	repo := NewPGRepository(pool)
	metadata := archivePGMetadata("archive_edit_" + archiveSuffix())
	version := archivePGVersion(metadata)
	if _, _, err := repo.SaveCreate(metadata, version, []string{"event_" + archiveSuffix()}, "create_"+metadata.ArchiveID); err != nil {
		t.Fatalf("SaveCreate() error = %v", err)
	}

	edited := metadata
	edited.CurrentVersion = 2
	edited.IndexGeneration = 2
	edited.ContentHash = "hash_edited_" + archiveSuffix()
	edited.UpdatedAt = time.Now().UTC()
	editVersion := Version{ArchiveID: edited.ArchiveID, Version: 2, FilePath: edited.FilePath, ContentHash: edited.ContentHash, EditorUserID: edited.UserID, Reason: "manual correction", CreatedAt: edited.UpdatedAt}
	audit := EditAuditLog{ArchiveID: edited.ArchiveID, ActorUserID: edited.UserID, OldVersion: 1, NewVersion: 2, OldContentHash: metadata.ContentHash, NewContentHash: edited.ContentHash, RequestID: "edit_" + edited.ArchiveID, Reason: "manual correction", CreatedAt: edited.UpdatedAt}

	saved, deduped, err := repo.SaveEdit(edited, editVersion, audit, audit.RequestID)
	if err != nil {
		t.Fatalf("SaveEdit() error = %v", err)
	}
	if deduped {
		t.Fatal("SaveEdit() deduped = true, want false")
	}
	if saved.CurrentVersion != 2 || saved.IndexGeneration != 2 || saved.ContentHash != edited.ContentHash {
		t.Fatalf("edited metadata mismatch: %#v", saved)
	}

	duplicate, deduped, err := repo.SaveEdit(edited, editVersion, audit, audit.RequestID)
	if err != nil {
		t.Fatalf("SaveEdit() duplicate error = %v", err)
	}
	if !deduped || duplicate.CurrentVersion != 2 {
		t.Fatalf("duplicate = %#v deduped=%v, want current archive deduped", duplicate, deduped)
	}
	assertArchiveRowCount(t, pool, "archive_versions", edited.ArchiveID, 2)
	assertArchiveRowCount(t, pool, "archive_edit_audit_logs", edited.ArchiveID, 1)
	assertArchiveRowCount(t, pool, "archive_index_generations", edited.ArchiveID, 2)
	versions, err := repo.Versions(edited.ArchiveID)
	if err != nil {
		t.Fatalf("Versions() error = %v", err)
	}
	if len(versions) != 2 || versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatalf("versions mismatch: %#v", versions)
	}
}

func archiveTestPool(t *testing.T) *pgxpool.Pool {
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

func archivePGMetadata(archiveID string) Metadata {
	now := time.Now().UTC()
	return Metadata{
		ArchiveID:       archiveID,
		UserID:          "user_" + archiveSuffix(),
		OrgID:           "org_" + archiveSuffix(),
		ProjectID:       "project_" + archiveSuffix(),
		Title:           "Archive " + archiveID,
		FilePath:        "/tmp/memory-os/" + archiveID + ".md",
		Status:          "active",
		IndexGeneration: 1,
		CurrentVersion:  1,
		ContentHash:     "hash_" + archiveSuffix(),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func archivePGVersion(metadata Metadata) Version {
	return Version{ArchiveID: metadata.ArchiveID, Version: metadata.CurrentVersion, FilePath: metadata.FilePath, ContentHash: metadata.ContentHash, EditorUserID: metadata.UserID, Reason: "initial archive", CreatedAt: metadata.CreatedAt}
}

func assertArchiveRowCount(t *testing.T, pool *pgxpool.Pool, tableName, archiveID string, want int) {
	t.Helper()
	var count int
	query := "SELECT count(*) FROM " + tableName + " WHERE archive_id = $1"
	if err := pool.QueryRow(context.Background(), query, archiveID).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", tableName, err)
	}
	if count != want {
		t.Fatalf("%s rows = %d, want %d", tableName, count, want)
	}
}

func archiveSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
