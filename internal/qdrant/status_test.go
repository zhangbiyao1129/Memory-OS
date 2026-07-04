package qdrant

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStatusServiceSnapshotMarksQueryTimeFilterUnenforcedWhenRequiredFieldsMissing(t *testing.T) {
	service := NewStatusService(StatusOptions{
		Client: fakeStatusClient{
			info: CollectionInfo{
				Name:          DefaultCollectionName,
				Status:        "green",
				PayloadSchema: map[string]bool{"user_id": true, "org_id": true, "project_id": true},
			},
		},
		CollectionName: DefaultCollectionName,
	})

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.QueryTimeFilterEnforced {
		t.Fatal("QueryTimeFilterEnforced = true, want false when payload schema is incomplete")
	}
	wantMissing := []string{"doc_type", "index_generation", "permission_labels", "visibility"}
	if len(snapshot.MissingRequiredPayloadFields) != len(wantMissing) {
		t.Fatalf("MissingRequiredPayloadFields len = %d, want %d (%v)", len(snapshot.MissingRequiredPayloadFields), len(wantMissing), snapshot.MissingRequiredPayloadFields)
	}
	for index, field := range wantMissing {
		if snapshot.MissingRequiredPayloadFields[index] != field {
			t.Fatalf("MissingRequiredPayloadFields[%d] = %q, want %q", index, snapshot.MissingRequiredPayloadFields[index], field)
		}
	}
}

func TestStatusServiceSnapshotMarksQueryTimeFilterEnforcedWhenRequiredFieldsPresent(t *testing.T) {
	service := NewStatusService(StatusOptions{
		Client: fakeStatusClient{
			info: CollectionInfo{
				Name:   DefaultCollectionName,
				Status: "green",
				PayloadSchema: map[string]bool{
					"doc_type":          true,
					"user_id":           true,
					"org_id":            true,
					"project_id":        true,
					"visibility":        true,
					"permission_labels": true,
					"index_generation":  true,
				},
			},
		},
		CollectionName: DefaultCollectionName,
	})

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if !snapshot.QueryTimeFilterEnforced {
		t.Fatal("QueryTimeFilterEnforced = false, want true when payload schema is complete")
	}
	if len(snapshot.MissingRequiredPayloadFields) != 0 {
		t.Fatalf("MissingRequiredPayloadFields = %v, want empty", snapshot.MissingRequiredPayloadFields)
	}
}

func TestPGStatusStoreArchiveIndexStatsReturnsJobAndChunkDetails(t *testing.T) {
	pool := qdrantTestPool(t)
	store := NewPGStatusStore(pool)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	archiveID := "archive_status_pg_" + suffix
	chunkID := archiveID + "_g1_c0"
	pointID := "point_" + suffix
	now := time.Now().UTC()

	if _, err := pool.Exec(context.Background(), `
INSERT INTO archives (archive_id, user_id, org_id, project_id, title, file_path, status, index_generation, current_version, content_hash, created_at, updated_at)
VALUES ($1,'user_pg','org_pg','project_pg','PG Status','/tmp/pg-status.md','active',1,1,'archive_hash',$2,$2)`,
		archiveID, now); err != nil {
		t.Fatalf("insert archive: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO archive_index_jobs (idempotency_key, archive_id, index_generation, status, error_message, attempts, max_attempts, created_at, updated_at)
VALUES ($1,$2,1,'failed','embedding timeout',2,3,$3,$3)`,
		"rag_"+archiveID+"_g1", archiveID, now); err != nil {
		t.Fatalf("insert archive index job: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO archive_chunks (chunk_id, archive_id, org_id, project_id, user_id, visibility, permission_labels, index_generation, chunk_index, heading_path, source_event_ids, content, content_hash, vector_status, stale, created_at, updated_at)
VALUES ($1,$2,'org_pg','project_pg','user_pg','project',$3,1,0,$4,$5,'safe chunk body','chunk_hash','pending',false,$6,$6)`,
		chunkID, archiveID, []string{"project:project_pg:read"}, []string{"Root"}, []string{"event_pg"}, now); err != nil {
		t.Fatalf("insert archive chunk: %v", err)
	}
	payload, err := json.Marshal(map[string]any{"archive_id": archiveID, "chunk_id": chunkID})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO qdrant_points (point_id, chunk_id, collection_name, payload, vector_status, created_at, updated_at)
VALUES ($1,$2,$3,$4::jsonb,'indexed',$5,$5)`,
		pointID, chunkID, DefaultCollectionName, string(payload), now); err != nil {
		t.Fatalf("insert qdrant point: %v", err)
	}

	stats, err := store.ArchiveIndexStats(context.Background(), DefaultCollectionName, archiveID, 1)
	if err != nil {
		t.Fatalf("ArchiveIndexStats() error = %v", err)
	}
	if stats.JobsByStatus["failed"] != 1 || stats.ChunksByStatus["pending"] != 1 || stats.PointsByStatus["indexed"] != 1 {
		t.Fatalf("status counts mismatch: %#v", stats)
	}
	if len(stats.IndexJobs) != 1 || stats.IndexJobs[0].ErrorMessage != "embedding timeout" || stats.IndexJobs[0].Attempts != 2 {
		t.Fatalf("index job details mismatch: %#v", stats.IndexJobs)
	}
	if len(stats.ArchiveChunks) != 1 || stats.ArchiveChunks[0].ContentHash != "chunk_hash" || stats.ArchiveChunks[0].QdrantPointID != pointID {
		t.Fatalf("archive chunk details mismatch: %#v", stats.ArchiveChunks)
	}
}

func TestPGStatusStoreIndexStatsSeparatesArchiveAndHotMemoryPoints(t *testing.T) {
	pool := qdrantTestPool(t)
	store := NewPGStatusStore(pool)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	archiveID := "archive_index_stats_" + suffix
	chunkID := archiveID + "_g1_c0"
	memoryID := "hm_index_stats_" + suffix
	collectionName := "memory_index_stats_test_" + suffix
	fact := "safe fact " + suffix
	factHash := "fact_hash_" + suffix
	now := time.Now().UTC()

	if _, err := pool.Exec(context.Background(), `
INSERT INTO archives (archive_id, user_id, org_id, project_id, title, file_path, status, index_generation, current_version, content_hash, created_at, updated_at)
VALUES ($1,'user_pg','org_pg','project_pg','PG Status','/tmp/pg-status.md','active',1,1,'archive_hash',$2,$2)`,
		archiveID, now); err != nil {
		t.Fatalf("insert archive: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO archive_chunks (chunk_id, archive_id, org_id, project_id, user_id, visibility, permission_labels, index_generation, chunk_index, heading_path, source_event_ids, content, content_hash, vector_status, stale, created_at, updated_at)
VALUES ($1,$2,'org_pg','project_pg','user_pg','project',$3,1,0,$4,$5,'safe chunk body','chunk_hash','indexed',false,$6,$6)`,
		chunkID, archiveID, []string{"project:project_pg:read"}, []string{"Root"}, []string{"event_pg"}, now); err != nil {
		t.Fatalf("insert archive chunk: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO qdrant_points (point_id, chunk_id, collection_name, payload, vector_status, created_at, updated_at)
VALUES ($1,$2,$3,$4::jsonb,'indexed',$5,$5)`,
		"point_archive_"+suffix, chunkID, collectionName, `{"doc_type":"archive_chunk"}`, now); err != nil {
		t.Fatalf("insert archive qdrant point: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO hot_memories (memory_id, org_id, project_id, user_id, agent_id, scope, visibility, permission_labels, fact, fact_hash, confidence, status, created_at, updated_at)
VALUES ($1,'org_pg','project_pg','user_pg','codex','project','project',$2,$3,$4,0.9,'active',$5,$5)`,
		memoryID, []string{"project:project_pg:read"}, fact, factHash, now); err != nil {
		t.Fatalf("insert hot memory: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO hot_memory_qdrant_points (point_id, memory_id, collection_name, payload, vector_status, created_at, updated_at)
VALUES ($1,$2,$3,$4::jsonb,'indexed',$5,$5)`,
		"point_hot_"+suffix, memoryID, collectionName, `{"doc_type":"hot_memory"}`, now.Add(time.Second)); err != nil {
		t.Fatalf("insert hot memory qdrant point: %v", err)
	}

	stats, err := store.IndexStats(context.Background(), collectionName)
	if err != nil {
		t.Fatalf("IndexStats() error = %v", err)
	}
	if stats.ArchivePointsByStatus["indexed"] != 1 {
		t.Fatalf("archive point counts mismatch: %#v", stats.ArchivePointsByStatus)
	}
	if stats.HotMemoryPointsByStatus["indexed"] != 1 {
		t.Fatalf("hot memory point counts mismatch: %#v", stats.HotMemoryPointsByStatus)
	}
	if stats.PointsByStatus["indexed"] < 2 {
		t.Fatalf("combined point counts should include archive and hot memory points: %#v", stats.PointsByStatus)
	}
}

func qdrantTestPool(t *testing.T) *pgxpool.Pool {
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

type fakeStatusClient struct {
	info CollectionInfo
	err  error
}

func (c fakeStatusClient) Health(ctx context.Context) error {
	return c.err
}

func (c fakeStatusClient) CollectionInfo(ctx context.Context, collection string) (CollectionInfo, error) {
	if c.err != nil {
		return CollectionInfo{}, c.err
	}
	if c.info.Name == "" {
		return CollectionInfo{}, errors.New("collection info missing")
	}
	return c.info, nil
}
