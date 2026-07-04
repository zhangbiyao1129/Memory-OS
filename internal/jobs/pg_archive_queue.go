package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
	"memory-os/internal/eventlog"
)

const (
	defaultArchiveQueueWorkerID      = "memory-worker"
	defaultArchiveQueueLeaseDuration = 5 * time.Minute
	defaultArchiveQueueMaxAttempts   = 3
)

type PGArchiveQueueOptions struct {
	WorkerID      string
	LeaseDuration time.Duration
	MaxAttempts   int
}

type PGArchiveQueue struct {
	pool          *pgxpool.Pool
	workerID      string
	leaseDuration time.Duration
	maxAttempts   int
}

func NewPGArchiveQueue(pool *pgxpool.Pool, options PGArchiveQueueOptions) *PGArchiveQueue {
	if options.WorkerID == "" {
		options.WorkerID = defaultArchiveQueueWorkerID
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = defaultArchiveQueueLeaseDuration
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = defaultArchiveQueueMaxAttempts
	}
	return &PGArchiveQueue{pool: pool, workerID: options.WorkerID, leaseDuration: options.LeaseDuration, maxAttempts: options.MaxAttempts}
}

func (q *PGArchiveQueue) Enqueue(ctx context.Context, job ArchiveJob) error {
	if q == nil || q.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	if job.RequestID == "" || job.ArchiveID == "" || job.UserID == "" || job.OrgID == "" || job.ProjectID == "" {
		return errors.New("archive job ids are required")
	}
	eventIDs := make([]string, 0, len(job.Events))
	for _, event := range job.Events {
		if event.EventID == "" {
			return errors.New("archive job event ids are required")
		}
		eventIDs = append(eventIDs, event.EventID)
	}
	if len(eventIDs) == 0 {
		return errors.New("archive job events are required")
	}
	_, err := q.pool.Exec(ctx, `
INSERT INTO archive_jobs (request_id, archive_id, title, user_id, org_id, project_id, event_ids, max_attempts, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)
ON CONFLICT (request_id) DO NOTHING`,
		job.RequestID, job.ArchiveID, job.Title, job.UserID, job.OrgID, job.ProjectID, eventIDs, q.maxAttempts, job.CreatedAt)
	return err
}

func (q *PGArchiveQueue) Lease(ctx context.Context) (ArchiveJob, bool, error) {
	if q == nil || q.pool == nil {
		return ArchiveJob{}, false, errors.New("postgres pool is not configured")
	}
	tx, err := q.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ArchiveJob{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var job ArchiveJob
	var eventIDs []string
	err = tx.QueryRow(ctx, `
WITH next_job AS (
    SELECT id
    FROM archive_jobs
    WHERE status IN ('pending', 'leased')
      AND attempts < max_attempts
      AND (status = 'pending' OR locked_until < now())
    ORDER BY created_at ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE archive_jobs
SET status = 'leased',
    attempts = attempts + 1,
    locked_by = $1,
    locked_until = now() + $2::interval,
    updated_at = now()
WHERE id = (SELECT id FROM next_job)
RETURNING request_id, archive_id, title, user_id, org_id, project_id, event_ids, created_at`,
		q.workerID, q.leaseDuration.String()).Scan(&job.RequestID, &job.ArchiveID, &job.Title, &job.UserID, &job.OrgID, &job.ProjectID, &eventIDs, &job.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ArchiveJob{}, false, nil
	}
	if err != nil {
		return ArchiveJob{}, false, err
	}

	events, err := loadArchiveJobEvents(ctx, tx, eventIDs)
	if err != nil {
		return ArchiveJob{}, false, err
	}
	job.Events = events
	if err := tx.Commit(ctx); err != nil {
		return ArchiveJob{}, false, err
	}
	return job, true, nil
}

func (q *PGArchiveQueue) Complete(ctx context.Context, job ArchiveJob, result archive.Result) error {
	if q == nil || q.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	_, err := q.pool.Exec(ctx, `
UPDATE archive_jobs
SET status = 'completed',
    locked_by = NULL,
    locked_until = NULL,
    last_error = '',
    completed_at = now(),
    updated_at = now()
WHERE request_id = $1 AND archive_id = $2`,
		job.RequestID, result.Metadata.ArchiveID)
	return err
}

func (q *PGArchiveQueue) Fail(ctx context.Context, job ArchiveJob, jobErr error) error {
	if q == nil || q.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	message := ""
	if jobErr != nil {
		message = jobErr.Error()
	}
	_, err := q.pool.Exec(ctx, `
UPDATE archive_jobs
SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
    locked_by = NULL,
    locked_until = NULL,
    last_error = $2,
    updated_at = now()
WHERE request_id = $1`,
		job.RequestID, message)
	return err
}

func loadArchiveJobEvents(ctx context.Context, tx pgx.Tx, eventIDs []string) ([]eventlog.TurnEvent, error) {
	rows, err := tx.Query(ctx, `
SELECT e.event_id, e.turn_id, e.thread_id, e.session_id, e.event_type, e.user_id, e.org_id, e.project_id, e.agent_id, e.created_at, p.payload, p.warnings
FROM unnest($1::text[]) WITH ORDINALITY AS wanted(event_id, ord)
JOIN turn_events e ON e.event_id = wanted.event_id
JOIN turn_event_payloads p ON p.event_id = e.event_id
ORDER BY wanted.ord`,
		eventIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []eventlog.TurnEvent{}
	for rows.Next() {
		var event eventlog.TurnEvent
		var payload []byte
		if err := rows.Scan(&event.EventID, &event.TurnID, &event.ThreadID, &event.SessionID, &event.Type, &event.Actor.UserID, &event.Actor.OrgID, &event.Actor.ProjectID, &event.Actor.AgentID, &event.CreatedAt, &payload, &event.Warnings); err != nil {
			return nil, err
		}
		event.Version = "v1"
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(events) != len(eventIDs) {
		return nil, errors.New("archive job references missing turn events")
	}
	return events, nil
}
