package eventlog

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

func (r *PGRepository) Save(sanitized SanitizedEvent, requestID string) (SaveResult, error) {
	if r == nil || r.pool == nil {
		return SaveResult{}, errors.New("postgres pool is not configured")
	}
	event := sanitized.Event
	if event.EventID == "" || requestID == "" {
		return SaveResult{}, errors.New("event id and request id are required")
	}
	ctx := context.Background()
	var existingEventID string
	err := r.pool.QueryRow(ctx, `SELECT event_id FROM event_ingest_requests WHERE request_id = $1`, requestID).Scan(&existingEventID)
	if err == nil {
		return SaveResult{EventID: existingEventID, Deduped: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return SaveResult{}, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SaveResult{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx, `
INSERT INTO turn_events (event_id, turn_id, thread_id, session_id, event_type, user_id, org_id, project_id, agent_id, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (event_id) DO NOTHING`,
		event.EventID, event.TurnID, event.ThreadID, event.SessionID, string(event.Type), event.Actor.UserID, event.Actor.OrgID, event.Actor.ProjectID, event.Actor.AgentID, event.CreatedAt)
	if err != nil {
		return SaveResult{}, err
	}
	deduped := tag.RowsAffected() == 0

	if _, err := tx.Exec(ctx, `
INSERT INTO turn_event_payloads (event_id, payload, safe_payload_hash, original_bytes, safe_bytes, truncated, warnings)
VALUES ($1,$2::jsonb,$3,$4,$5,$6,$7)
ON CONFLICT (event_id) DO NOTHING`,
		event.EventID, string(sanitized.SafePayload), sanitized.PayloadHash, sanitized.OriginalBytes, sanitized.SafeBytes, sanitized.Truncated, emptyWarnings(event.Warnings)); err != nil {
		return SaveResult{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO event_ingest_requests (request_id, event_id)
VALUES ($1,$2)
ON CONFLICT (request_id) DO NOTHING`,
		requestID, event.EventID); err != nil {
		return SaveResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SaveResult{}, err
	}
	return SaveResult{EventID: event.EventID, Deduped: deduped}, nil
}

func emptyWarnings(warnings []string) []string {
	if warnings == nil {
		return []string{}
	}
	return warnings
}

func (r *PGRepository) Count() int {
	if r == nil || r.pool == nil {
		return 0
	}
	var count int
	if err := r.pool.QueryRow(context.Background(), `SELECT count(*) FROM turn_events`).Scan(&count); err != nil {
		return 0
	}
	return count
}
