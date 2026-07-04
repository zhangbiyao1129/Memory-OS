package audit

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

func (r *PGRepository) Save(log Log) error {
	if r == nil || r.pool == nil {
		return errors.New("audit postgres repository is not configured")
	}
	metadata := log.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(context.Background(), `
INSERT INTO audit_logs (actor_user_id, org_id, project_id, action, resource_type, resource_id, request_id, result, metadata)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9::jsonb)`,
		emptyUUID(log.ActorUserID),
		emptyUUID(log.OrgID),
		emptyUUID(log.ProjectID),
		log.Action,
		log.ResourceType,
		log.ResourceID,
		log.RequestID,
		log.Result,
		string(metadataJSON),
	)
	return err
}

func (r *PGRepository) List(filter ListFilter) ([]Log, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("audit postgres repository is not configured")
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	rows, err := r.pool.Query(context.Background(), `
SELECT id::text, COALESCE(actor_user_id::text, ''), COALESCE(org_id::text, ''), COALESCE(project_id::text, ''),
       action, resource_type, resource_id, request_id, result, metadata, created_at
FROM audit_logs
WHERE ($1::uuid IS NULL OR org_id = $1::uuid)
  AND ($2::uuid IS NULL OR project_id = $2::uuid)
  AND ($3::uuid IS NULL OR actor_user_id = $3::uuid)
  AND ($4 = '' OR resource_type = $4)
  AND ($5 = '' OR resource_id = $5)
ORDER BY created_at DESC
LIMIT $6`, emptyUUID(filter.OrgID), emptyUUID(filter.ProjectID), emptyUUID(filter.ActorUserID), filter.ResourceType, filter.ResourceID, filter.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Log{}
	for rows.Next() {
		var log Log
		if err := scanLog(rows, &log); err != nil {
			return nil, err
		}
		items = append(items, log)
	}
	return items, rows.Err()
}

func scanLog(row pgx.Row, log *Log) error {
	return row.Scan(&log.ID, &log.ActorUserID, &log.OrgID, &log.ProjectID, &log.Action, &log.ResourceType, &log.ResourceID, &log.RequestID, &log.Result, &log.Metadata, &log.CreatedAt)
}

func emptyUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}
