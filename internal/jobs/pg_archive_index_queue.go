package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
)

const (
	defaultRAGIndexWorkerID      = "memory-rag-index-worker"
	defaultRAGIndexLeaseDuration = 5 * time.Minute
	defaultRAGIndexMaxAttempts   = 3
)

type PGArchiveIndexQueueOptions struct {
	WorkerID      string
	LeaseDuration time.Duration
	MaxAttempts   int
}

type PGArchiveIndexQueue struct {
	pool          *pgxpool.Pool
	workerID      string
	leaseDuration time.Duration
	maxAttempts   int
}

func NewPGArchiveIndexQueue(pool *pgxpool.Pool, options PGArchiveIndexQueueOptions) *PGArchiveIndexQueue {
	if options.WorkerID == "" {
		options.WorkerID = defaultRAGIndexWorkerID
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = defaultRAGIndexLeaseDuration
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = defaultRAGIndexMaxAttempts
	}
	return &PGArchiveIndexQueue{pool: pool, workerID: options.WorkerID, leaseDuration: options.LeaseDuration, maxAttempts: options.MaxAttempts}
}

func (q *PGArchiveIndexQueue) Enqueue(ctx context.Context, job RAGIndexJob) error {
	if q == nil || q.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	if job.IdempotencyKey == "" || job.OrgID == "" || job.ProjectID == "" || job.UserID == "" || job.Visibility == "" || len(job.PermissionLabels) == 0 || len(job.Chunks) == 0 {
		return errors.New("rag index job scope, permissions and chunks are required")
	}
	archiveID := job.Chunks[0].ArchiveID
	generation := job.Chunks[0].IndexGeneration
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `UPDATE archive_chunks SET stale = true, updated_at = now() WHERE archive_id = $1 AND index_generation <> $2`, archiveID, generation); err != nil {
		return err
	}
	for _, chunk := range job.Chunks {
		if err := insertArchiveChunk(ctx, tx, job, chunk); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO archive_index_jobs (idempotency_key, archive_id, index_generation, status, max_attempts)
VALUES ($1,$2,$3,'pending',$4)
ON CONFLICT (idempotency_key) DO NOTHING`,
		job.IdempotencyKey, archiveID, generation, q.maxAttempts); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (q *PGArchiveIndexQueue) Lease(ctx context.Context) (RAGIndexJob, bool, error) {
	if q == nil || q.pool == nil {
		return RAGIndexJob{}, false, errors.New("postgres pool is not configured")
	}
	tx, err := q.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RAGIndexJob{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var job RAGIndexJob
	var archiveID string
	var generation int
	err = tx.QueryRow(ctx, `
WITH next_job AS (
    SELECT id
    FROM archive_index_jobs
    WHERE status IN ('pending', 'leased')
      AND attempts < max_attempts
      AND (status = 'pending' OR locked_until < now())
    ORDER BY created_at ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE archive_index_jobs
SET status = 'leased',
    attempts = attempts + 1,
    locked_by = $1,
    locked_until = now() + $2::interval,
    updated_at = now()
WHERE id = (SELECT id FROM next_job)
RETURNING idempotency_key, archive_id, index_generation`,
		q.workerID, q.leaseDuration.String()).Scan(&job.IdempotencyKey, &archiveID, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return RAGIndexJob{}, false, nil
	}
	if err != nil {
		return RAGIndexJob{}, false, err
	}
	metadata, err := loadArchiveIndexScope(ctx, tx, archiveID)
	if err != nil {
		return RAGIndexJob{}, false, err
	}
	chunks, permissionLabels, err := loadArchiveIndexChunks(ctx, tx, archiveID, generation)
	if err != nil {
		return RAGIndexJob{}, false, err
	}
	job.OrgID = metadata.OrgID
	job.ProjectID = metadata.ProjectID
	job.UserID = metadata.UserID
	job.Visibility = "project"
	job.PermissionLabels = permissionLabels
	job.Chunks = chunks
	if err := tx.Commit(ctx); err != nil {
		return RAGIndexJob{}, false, err
	}
	return job, true, nil
}

func (q *PGArchiveIndexQueue) Complete(ctx context.Context, job RAGIndexJob, result RAGIndexResult) error {
	if q == nil || q.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	_, err := q.pool.Exec(ctx, `
UPDATE archive_index_jobs
SET status = 'completed',
    error_message = '',
    locked_by = NULL,
    locked_until = NULL,
    completed_at = now(),
    updated_at = now()
WHERE idempotency_key = $1`,
		job.IdempotencyKey)
	return err
}

func (q *PGArchiveIndexQueue) Fail(ctx context.Context, job RAGIndexJob, jobErr error) error {
	if q == nil || q.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	message := ""
	if jobErr != nil {
		message = jobErr.Error()
	}
	_, err := q.pool.Exec(ctx, `
UPDATE archive_index_jobs
SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
    error_message = $2,
    locked_by = NULL,
    locked_until = NULL,
    updated_at = now()
WHERE idempotency_key = $1`,
		job.IdempotencyKey, message)
	return err
}

func (q *PGArchiveIndexQueue) RetryFailed(ctx context.Context, archiveID string, generation int) (int64, error) {
	if q == nil || q.pool == nil {
		return 0, errors.New("postgres pool is not configured")
	}
	if archiveID == "" || generation <= 0 {
		return 0, errors.New("archive id and index generation are required")
	}
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx, `
UPDATE archive_index_jobs
SET status = 'pending',
    error_message = '',
    attempts = 0,
    locked_by = NULL,
    locked_until = NULL,
    completed_at = NULL,
    updated_at = now()
WHERE archive_id = $1
  AND index_generation = $2
  AND status = 'failed'`, archiveID, generation)
	if err != nil {
		return 0, err
	}
	retried := tag.RowsAffected()
	if retried > 0 {
		if _, err := tx.Exec(ctx, `
UPDATE archive_chunks
SET vector_status = 'pending',
    updated_at = now()
WHERE archive_id = $1
  AND index_generation = $2
  AND stale = false`, archiveID, generation); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
UPDATE qdrant_points qp
SET vector_status = 'pending',
    updated_at = now()
FROM archive_chunks ac
WHERE qp.chunk_id = ac.chunk_id
  AND ac.archive_id = $1
  AND ac.index_generation = $2
  AND ac.stale = false`, archiveID, generation); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return retried, nil
}

func insertArchiveChunk(ctx context.Context, tx pgx.Tx, job RAGIndexJob, chunk archive.Chunk) error {
	headingPath := emptyStringSlice(chunk.HeadingPath)
	sourceEventIDs := emptyStringSlice(chunk.SourceEventIDs)
	_, err := tx.Exec(ctx, `
INSERT INTO archive_chunks (chunk_id, archive_id, org_id, project_id, user_id, visibility, permission_labels, index_generation, chunk_index, heading_path, source_event_ids, content, content_hash, vector_status, stale)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'pending',false)
ON CONFLICT (chunk_id) DO UPDATE SET
    permission_labels = EXCLUDED.permission_labels,
    content = EXCLUDED.content,
    content_hash = EXCLUDED.content_hash,
    vector_status = 'pending',
    stale = false,
    updated_at = now()`,
		chunk.ChunkID, chunk.ArchiveID, job.OrgID, job.ProjectID, job.UserID, job.Visibility, job.PermissionLabels, chunk.IndexGeneration, chunk.ChunkIndex, headingPath, sourceEventIDs, chunk.Content, chunk.ContentHash)
	return err
}

func emptyStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func loadArchiveIndexScope(ctx context.Context, tx pgx.Tx, archiveID string) (archive.Metadata, error) {
	var metadata archive.Metadata
	err := tx.QueryRow(ctx, `SELECT archive_id, user_id, org_id, project_id FROM archives WHERE archive_id = $1`, archiveID).Scan(&metadata.ArchiveID, &metadata.UserID, &metadata.OrgID, &metadata.ProjectID)
	return metadata, err
}

func loadArchiveIndexChunks(ctx context.Context, tx pgx.Tx, archiveID string, generation int) ([]archive.Chunk, []string, error) {
	rows, err := tx.Query(ctx, `
SELECT chunk_id, archive_id, index_generation, chunk_index, heading_path, source_event_ids, content, content_hash, permission_labels
FROM archive_chunks
WHERE archive_id = $1 AND index_generation = $2 AND stale = false
ORDER BY chunk_index`, archiveID, generation)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	chunks := []archive.Chunk{}
	permissionLabels := []string{}
	for rows.Next() {
		var chunk archive.Chunk
		var labels []string
		if err := rows.Scan(&chunk.ChunkID, &chunk.ArchiveID, &chunk.IndexGeneration, &chunk.ChunkIndex, &chunk.HeadingPath, &chunk.SourceEventIDs, &chunk.Content, &chunk.ContentHash, &labels); err != nil {
			return nil, nil, err
		}
		if len(permissionLabels) == 0 {
			permissionLabels = labels
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(chunks) == 0 {
		return nil, nil, errors.New("archive index job has no chunks")
	}
	return chunks, permissionLabels, nil
}
