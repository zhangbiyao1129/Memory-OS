package retrieval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PGAccessLog struct {
	pool *pgxpool.Pool
}

func NewPGAccessLog(pool *pgxpool.Pool) *PGAccessLog {
	return &PGAccessLog{pool: pool}
}

func (l *PGAccessLog) LogRequest(request SearchRequest, rerankDegraded bool) error {
	if l == nil || l.pool == nil {
		return errors.New("retrieval postgres access log is not configured")
	}
	_, err := l.pool.Exec(context.Background(), `
INSERT INTO retrieval_requests (request_id, actor_user_id, org_id, project_id, agent_id, query_hash, rerank_degraded)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (request_id) DO UPDATE
SET rerank_degraded = retrieval_requests.rerank_degraded OR EXCLUDED.rerank_degraded`,
		request.RequestID,
		request.Actor.UserID,
		request.Actor.OrgID,
		request.Actor.ProjectID,
		request.Actor.AgentID,
		hashQuery(request.Query),
		rerankDegraded,
	)
	return err
}

func (l *PGAccessLog) LogResult(requestID string, rank int, result SearchResult) error {
	if l == nil || l.pool == nil {
		return errors.New("retrieval postgres access log is not configured")
	}
	sourceRef, err := json.Marshal(result.Source)
	if err != nil {
		return err
	}
	tx, err := l.pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck

	tag, err := tx.Exec(context.Background(), `
INSERT INTO retrieval_results (request_id, rank, score, source_kind, source_ref)
VALUES ($1,$2,$3,$4,$5::jsonb)
ON CONFLICT (request_id, rank) DO NOTHING`,
		requestID,
		rank,
		result.Score,
		string(result.Source.Kind),
		string(sourceRef),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(context.Background())
	}
	_, err = tx.Exec(context.Background(), `
INSERT INTO memory_access_logs (request_id, actor_user_id, org_id, project_id, agent_id, source_kind, source_ref)
SELECT request_id, actor_user_id, org_id, project_id, agent_id, $2, $3::jsonb
FROM retrieval_requests
WHERE request_id = $1`,
		requestID,
		string(result.Source.Kind),
		string(sourceRef),
	)
	if err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

func (l *PGAccessLog) ListRequests(filter AccessLogListFilter) ([]AccessLogRequestEntry, error) {
	if l == nil || l.pool == nil {
		return nil, errors.New("retrieval postgres access log is not configured")
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	rows, err := l.pool.Query(context.Background(), `
SELECT request_id, actor_user_id, org_id, project_id, agent_id, query_hash, rerank_degraded, created_at
FROM retrieval_requests
WHERE org_id = $1
  AND project_id = $2
  AND ($3 = '' OR actor_user_id = $3)
  AND ($4 = '' OR request_id = $4)
ORDER BY created_at DESC
LIMIT $5`, filter.OrgID, filter.ProjectID, filter.ActorUserID, filter.RequestID, filter.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []AccessLogRequestEntry{}
	for rows.Next() {
		var item AccessLogRequestEntry
		if err := rows.Scan(&item.RequestID, &item.ActorUserID, &item.OrgID, &item.ProjectID, &item.AgentID, &item.QueryHash, &item.RerankDegraded, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (l *PGAccessLog) ListResults(filter AccessLogListFilter) ([]AccessLogResultEntry, error) {
	if l == nil || l.pool == nil {
		return nil, errors.New("retrieval postgres access log is not configured")
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	rows, err := l.pool.Query(context.Background(), `
SELECT rr.request_id, rr.rank, rr.score, rr.source_kind, rr.source_ref, rr.created_at
FROM retrieval_results rr
JOIN retrieval_requests rq ON rq.request_id = rr.request_id
WHERE rq.org_id = $1
  AND rq.project_id = $2
  AND ($3 = '' OR rq.actor_user_id = $3)
  AND ($4 = '' OR rr.request_id = $4)
ORDER BY rr.created_at DESC, rr.rank ASC
LIMIT $5`, filter.OrgID, filter.ProjectID, filter.ActorUserID, filter.RequestID, filter.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []AccessLogResultEntry{}
	for rows.Next() {
		var item AccessLogResultEntry
		if err := rows.Scan(&item.RequestID, &item.Rank, &item.Score, &item.SourceKind, &item.SourceRef, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func hashQuery(query string) string {
	sum := sha256.Sum256([]byte(query))
	return hex.EncodeToString(sum[:])
}
