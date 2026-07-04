package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
)

func TestPGArchiveIndexQueueRequiresPool(t *testing.T) {
	queue := NewPGArchiveIndexQueue(nil, PGArchiveIndexQueueOptions{})
	job := ragIndexQueueJob("archive_missing_pool")

	if err := queue.Enqueue(context.Background(), job); err == nil {
		t.Fatal("Enqueue() error = nil, want missing pool error")
	}
	if _, _, err := queue.Lease(context.Background()); err == nil {
		t.Fatal("Lease() error = nil, want missing pool error")
	}
	if err := queue.Complete(context.Background(), job, RAGIndexResult{}); err == nil {
		t.Fatal("Complete() error = nil, want missing pool error")
	}
	if err := queue.Fail(context.Background(), job, errors.New("boom")); err == nil {
		t.Fatal("Fail() error = nil, want missing pool error")
	}
	if _, err := queue.RetryFailed(context.Background(), job.Chunks[0].ArchiveID, job.Chunks[0].IndexGeneration); err == nil {
		t.Fatal("RetryFailed() error = nil, want missing pool error")
	}
}

func TestPGArchiveIndexQueueEnqueueLeasesCompletesAndDedupes(t *testing.T) {
	pool := jobsTestPool(t)
	queue := NewPGArchiveIndexQueue(pool, PGArchiveIndexQueueOptions{WorkerID: "rag_worker_1", LeaseDuration: time.Minute})
	job := ragIndexQueueJob("archive_rag_" + jobsSuffix())
	insertArchiveForIndexJob(t, pool, job)

	if err := queue.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := queue.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue() duplicate error = %v", err)
	}
	assertArchiveIndexJobCount(t, pool, job.IdempotencyKey, 1)
	assertArchiveChunkCount(t, pool, job.Chunks[0].ArchiveID, job.Chunks[0].IndexGeneration, len(job.Chunks))

	leased, ok, err := queue.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease() error = %v", err)
	}
	if !ok {
		t.Fatal("Lease() ok = false, want true")
	}
	if leased.IdempotencyKey != job.IdempotencyKey || len(leased.Chunks) != len(job.Chunks) {
		t.Fatalf("leased job mismatch: %#v", leased)
	}
	if leased.Chunks[0].Content != "deploy api" || leased.PermissionLabels[0] != "project:"+job.ProjectID+":read" {
		t.Fatalf("leased chunks/scope mismatch: %#v", leased)
	}

	if err := queue.Complete(context.Background(), leased, RAGIndexResult{}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	assertArchiveIndexJobStatus(t, pool, job.IdempotencyKey, "completed")

	_, ok, err = queue.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease() after complete error = %v", err)
	}
	if ok {
		t.Fatal("Lease() after complete ok = true, want false")
	}
}

func TestPGArchiveIndexQueueEnqueueStoresEmptyChunkArrays(t *testing.T) {
	pool := jobsTestPool(t)
	queue := NewPGArchiveIndexQueue(pool, PGArchiveIndexQueueOptions{WorkerID: "rag_worker_1", LeaseDuration: time.Minute})
	job := ragIndexQueueJob("archive_rag_empty_arrays_" + jobsSuffix())
	job.Chunks[0].HeadingPath = nil
	job.Chunks[0].SourceEventIDs = nil
	insertArchiveForIndexJob(t, pool, job)

	if err := queue.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	assertArchiveChunkArrays(t, pool, job.Chunks[0].ChunkID, 0, 0)
	leased, ok, err := queue.Lease(context.Background())
	if err != nil || !ok {
		t.Fatalf("Lease() job=%#v ok=%v err=%v", leased, ok, err)
	}
	if leased.IdempotencyKey != job.IdempotencyKey {
		t.Fatalf("Lease() idempotency key = %q, want %q", leased.IdempotencyKey, job.IdempotencyKey)
	}
	if err := queue.Complete(context.Background(), leased, RAGIndexResult{}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestPGArchiveIndexQueueFailRequeuesUntilMaxAttempts(t *testing.T) {
	pool := jobsTestPool(t)
	queue := NewPGArchiveIndexQueue(pool, PGArchiveIndexQueueOptions{WorkerID: "rag_worker_1", LeaseDuration: time.Minute, MaxAttempts: 2})
	job := ragIndexQueueJob("archive_rag_retry_" + jobsSuffix())
	insertArchiveForIndexJob(t, pool, job)
	if err := queue.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	first, ok, err := queue.Lease(context.Background())
	if err != nil || !ok {
		t.Fatalf("first Lease() job=%#v ok=%v err=%v", first, ok, err)
	}
	if err := queue.Fail(context.Background(), first, errors.New("temporary")); err != nil {
		t.Fatalf("first Fail() error = %v", err)
	}
	assertArchiveIndexJobStatus(t, pool, job.IdempotencyKey, "pending")

	second, ok, err := queue.Lease(context.Background())
	if err != nil || !ok {
		t.Fatalf("second Lease() job=%#v ok=%v err=%v", second, ok, err)
	}
	if err := queue.Fail(context.Background(), second, errors.New("permanent")); err != nil {
		t.Fatalf("second Fail() error = %v", err)
	}
	assertArchiveIndexJobStatus(t, pool, job.IdempotencyKey, "failed")
}

func TestPGArchiveIndexQueueRetryFailedResetsCurrentGeneration(t *testing.T) {
	pool := jobsTestPool(t)
	queue := NewPGArchiveIndexQueue(pool, PGArchiveIndexQueueOptions{WorkerID: "rag_worker_1", LeaseDuration: time.Minute, MaxAttempts: 1})
	job := ragIndexQueueJob("archive_rag_retry_failed_" + jobsSuffix())
	insertArchiveForIndexJob(t, pool, job)
	if err := queue.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	leased, ok, err := queue.Lease(context.Background())
	if err != nil || !ok {
		t.Fatalf("Lease() job=%#v ok=%v err=%v", leased, ok, err)
	}
	if err := queue.Fail(context.Background(), leased, errors.New("embedding provider down")); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	assertArchiveIndexJobStatus(t, pool, job.IdempotencyKey, "failed")
	markArchiveIndexVectorFailed(t, pool, job.Chunks[0].ArchiveID, job.Chunks[0].IndexGeneration, job.Chunks[0].ChunkID)

	retried, err := queue.RetryFailed(context.Background(), job.Chunks[0].ArchiveID, job.Chunks[0].IndexGeneration)
	if err != nil {
		t.Fatalf("RetryFailed() error = %v", err)
	}
	if retried != 1 {
		t.Fatalf("RetryFailed() retried = %d, want 1", retried)
	}
	assertArchiveIndexJobRetryReady(t, pool, job.IdempotencyKey)
	assertArchiveIndexVectorStatus(t, pool, job.Chunks[0].ArchiveID, job.Chunks[0].IndexGeneration, job.Chunks[0].ChunkID, "pending")

	retriedAgain, err := queue.RetryFailed(context.Background(), job.Chunks[0].ArchiveID, job.Chunks[0].IndexGeneration)
	if err != nil {
		t.Fatalf("RetryFailed() second call error = %v", err)
	}
	if retriedAgain != 0 {
		t.Fatalf("RetryFailed() second call retried = %d, want 0", retriedAgain)
	}

	released, ok, err := queue.Lease(context.Background())
	if err != nil || !ok {
		t.Fatalf("Lease() after retry job=%#v ok=%v err=%v", released, ok, err)
	}
	if released.IdempotencyKey != job.IdempotencyKey {
		t.Fatalf("leased retry job idempotency key = %q, want %q", released.IdempotencyKey, job.IdempotencyKey)
	}
}

func ragIndexQueueJob(archiveID string) RAGIndexJob {
	projectID := "project_" + jobsSuffix()
	return RAGIndexJob{
		IdempotencyKey: "rag_" + archiveID + "_g1",
		OrgID:          "org_" + jobsSuffix(),
		ProjectID:      projectID,
		UserID:         "user_" + jobsSuffix(),
		Visibility:     "project",
		PermissionLabels: []string{
			"project:" + projectID + ":read",
		},
		Chunks: []archive.Chunk{{
			ChunkID:         archiveID + "_g1_c0",
			ArchiveID:       archiveID,
			IndexGeneration: 1,
			ChunkIndex:      0,
			HeadingPath:     []string{"Deploy"},
			SourceEventIDs:  []string{"event_" + jobsSuffix()},
			Content:         "deploy api",
			ContentHash:     "hash_" + jobsSuffix(),
		}},
	}
}

func insertArchiveForIndexJob(t *testing.T, pool *pgxpool.Pool, job RAGIndexJob) {
	t.Helper()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
INSERT INTO archives (archive_id, user_id, org_id, project_id, title, file_path, status, index_generation, current_version, content_hash, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,'active',$7,1,$8,$9,$9)
ON CONFLICT (archive_id) DO NOTHING`,
		job.Chunks[0].ArchiveID, job.UserID, job.OrgID, job.ProjectID, "Archive "+job.Chunks[0].ArchiveID, "/tmp/"+job.Chunks[0].ArchiveID+".md", job.Chunks[0].IndexGeneration, "hash_"+jobsSuffix(), now)
	if err != nil {
		t.Fatalf("insert archive: %v", err)
	}
}

func assertArchiveIndexJobCount(t *testing.T, pool *pgxpool.Pool, idempotencyKey string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM archive_index_jobs WHERE idempotency_key = $1`, idempotencyKey).Scan(&count); err != nil {
		t.Fatalf("count archive_index_jobs: %v", err)
	}
	if count != want {
		t.Fatalf("archive_index_jobs count = %d, want %d", count, want)
	}
}

func assertArchiveChunkCount(t *testing.T, pool *pgxpool.Pool, archiveID string, generation int, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM archive_chunks WHERE archive_id = $1 AND index_generation = $2`, archiveID, generation).Scan(&count); err != nil {
		t.Fatalf("count archive_chunks: %v", err)
	}
	if count != want {
		t.Fatalf("archive_chunks count = %d, want %d", count, want)
	}
}

func assertArchiveChunkArrays(t *testing.T, pool *pgxpool.Pool, chunkID string, wantHeadingLen, wantSourceLen int) {
	t.Helper()
	var headingLen, sourceLen int
	if err := pool.QueryRow(context.Background(), `
SELECT COALESCE(array_length(heading_path, 1), 0), COALESCE(array_length(source_event_ids, 1), 0)
FROM archive_chunks
WHERE chunk_id = $1`, chunkID).Scan(&headingLen, &sourceLen); err != nil {
		t.Fatalf("select archive chunk arrays: %v", err)
	}
	if headingLen != wantHeadingLen || sourceLen != wantSourceLen {
		t.Fatalf("archive chunk array lengths heading=%d source=%d, want heading=%d source=%d", headingLen, sourceLen, wantHeadingLen, wantSourceLen)
	}
}

func assertArchiveIndexJobStatus(t *testing.T, pool *pgxpool.Pool, idempotencyKey, want string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM archive_index_jobs WHERE idempotency_key = $1`, idempotencyKey).Scan(&status); err != nil {
		t.Fatalf("select archive index job status: %v", err)
	}
	if status != want {
		t.Fatalf("archive index job status = %q, want %q", status, want)
	}
}

func markArchiveIndexVectorFailed(t *testing.T, pool *pgxpool.Pool, archiveID string, generation int, chunkID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE archive_chunks SET vector_status = 'failed' WHERE archive_id = $1 AND index_generation = $2`, archiveID, generation); err != nil {
		t.Fatalf("mark archive chunk failed: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO qdrant_points (point_id, chunk_id, collection_name, payload, vector_status)
VALUES ($1,$2,'memory_archive_chunks','{}','failed')
ON CONFLICT (point_id) DO UPDATE SET vector_status = 'failed', updated_at = now()`,
		"point_"+chunkID, chunkID); err != nil {
		t.Fatalf("mark qdrant point failed: %v", err)
	}
}

func assertArchiveIndexJobRetryReady(t *testing.T, pool *pgxpool.Pool, idempotencyKey string) {
	t.Helper()
	var status, errorMessage string
	var attempts int
	var lockedBy *string
	var lockedUntil *time.Time
	var completedAt *time.Time
	err := pool.QueryRow(context.Background(), `
SELECT status, error_message, attempts, locked_by, locked_until, completed_at
FROM archive_index_jobs
WHERE idempotency_key = $1`, idempotencyKey).Scan(&status, &errorMessage, &attempts, &lockedBy, &lockedUntil, &completedAt)
	if err != nil {
		t.Fatalf("select retry-ready archive index job: %v", err)
	}
	if status != "pending" || errorMessage != "" || attempts != 0 || lockedBy != nil || lockedUntil != nil || completedAt != nil {
		t.Fatalf("archive index job retry state mismatch: status=%q error=%q attempts=%d locked_by=%v locked_until=%v completed_at=%v", status, errorMessage, attempts, lockedBy, lockedUntil, completedAt)
	}
}

func assertArchiveIndexVectorStatus(t *testing.T, pool *pgxpool.Pool, archiveID string, generation int, chunkID string, want string) {
	t.Helper()
	var chunkStatus, pointStatus string
	if err := pool.QueryRow(context.Background(), `SELECT vector_status FROM archive_chunks WHERE archive_id = $1 AND index_generation = $2 AND chunk_id = $3`, archiveID, generation, chunkID).Scan(&chunkStatus); err != nil {
		t.Fatalf("select archive chunk vector status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT vector_status FROM qdrant_points WHERE chunk_id = $1`, chunkID).Scan(&pointStatus); err != nil {
		t.Fatalf("select qdrant point vector status: %v", err)
	}
	if chunkStatus != want || pointStatus != want {
		t.Fatalf("vector status chunk=%q point=%q, want %q", chunkStatus, pointStatus, want)
	}
}
