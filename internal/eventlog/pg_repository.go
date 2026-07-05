package eventlog

import (
	"context"
	"encoding/json"
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

// GetEvent 按 event_id 读取已保存事件(join turn_events + turn_event_payloads),返回已脱敏事件。
func (r *PGRepository) GetEvent(eventID string) (TurnEvent, error) {
	if r == nil || r.pool == nil {
		return TurnEvent{}, errors.New("postgres pool is not configured")
	}
	var event TurnEvent
	var eventType string
	var payload []byte
	err := r.pool.QueryRow(context.Background(), `
SELECT e.event_id, e.turn_id, e.thread_id, e.session_id, e.event_type,
       e.user_id, e.org_id, e.project_id, e.agent_id, e.created_at, p.payload
FROM turn_events e
LEFT JOIN turn_event_payloads p ON p.event_id = e.event_id
WHERE e.event_id = $1`, eventID).Scan(
		&event.EventID, &event.TurnID, &event.ThreadID, &event.SessionID, &eventType,
		&event.Actor.UserID, &event.Actor.OrgID, &event.Actor.ProjectID, &event.Actor.AgentID, &event.CreatedAt, &payload,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TurnEvent{}, ErrEventNotFound
		}
		return TurnEvent{}, err
	}
	event.Type = EventType(eventType)
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return TurnEvent{}, err
		}
	}
	return event, nil
}
