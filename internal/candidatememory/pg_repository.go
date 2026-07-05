package candidatememory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	candidateColumns = "candidate_id, org_id, project_id, source_key, user_id, agent_id, thread_id, session_id, source_event_ids, memory_type, content, summary, risk_level, confidence, status, similar_refs, scores, created_at, updated_at"
	jobColumns       = "id, idempotency_key, org_id, project_id, source_key, source_event_id, status, attempts, max_attempts, locked_by, locked_until, last_error, candidate_ids, created_at, updated_at, completed_at"
	topicColumns     = "id, org_id, project_id, source_key, thread_id, candidate_count, completion_score, last_event_at, ready_to_compose, composed_archive_id, created_at, updated_at"
)

// PGRepository 基于 pgx 的 PostgreSQL 实现。所有写操作以 org_id 限定,避免跨租户。
type PGRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

func (r *PGRepository) check() error {
	if r == nil || r.pool == nil {
		return errors.New("candidate memory postgres repository is not configured")
	}
	return nil
}

func (r *PGRepository) CreateCandidate(ctx context.Context, c Candidate) (Candidate, error) {
	if err := r.check(); err != nil {
		return Candidate{}, err
	}
	similarRefs, scoresBytes, err := encodeCandidateJSONB(c)
	if err != nil {
		return Candidate{}, err
	}
	query := `INSERT INTO candidate_memories (
		candidate_id, org_id, project_id, source_key, user_id, agent_id, thread_id, session_id,
		source_event_ids, memory_type, content, summary, risk_level, confidence, status, similar_refs, scores
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
	ON CONFLICT (candidate_id) DO NOTHING
	RETURNING ` + candidateColumns
	row := r.pool.QueryRow(ctx, query,
		c.CandidateID, c.OrgID, c.ProjectID, c.SourceKey, c.UserID, c.AgentID, c.ThreadID, c.SessionID,
		c.SourceEventIDs, string(c.MemoryType), c.Content, c.Summary, string(c.RiskLevel), c.Confidence, string(c.Status),
		similarRefs, scoresBytes,
	)
	cand, err := scanCandidate(row)
	if err != nil {
		// ON CONFLICT DO NOTHING 命中冲突时 RETURNING 无行 → ErrNotFound → 转为 ErrConflict
		if errors.Is(err, ErrNotFound) {
			return Candidate{}, ErrConflict
		}
		return Candidate{}, err
	}
	return cand, nil
}

func (r *PGRepository) GetCandidate(ctx context.Context, orgID, candidateID string) (Candidate, error) {
	if err := r.check(); err != nil {
		return Candidate{}, err
	}
	query := "SELECT " + candidateColumns + " FROM candidate_memories WHERE org_id=$1 AND candidate_id=$2"
	row := r.pool.QueryRow(ctx, query, orgID, candidateID)
	return scanCandidate(row)
}

func (r *PGRepository) ListCandidates(ctx context.Context, filter ListFilter) ([]Candidate, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	where := []string{}
	args := []any{}
	if filter.OrgID != "" {
		args = append(args, filter.OrgID)
		where = append(where, fmt.Sprintf("org_id = $%d", len(args)))
	}
	if filter.ProjectID != "" {
		args = append(args, filter.ProjectID)
		where = append(where, fmt.Sprintf("project_id = $%d", len(args)))
	}
	if filter.SourceKey != "" {
		args = append(args, filter.SourceKey)
		where = append(where, fmt.Sprintf("source_key = $%d", len(args)))
	}
	if filter.ThreadID != "" {
		args = append(args, filter.ThreadID)
		where = append(where, fmt.Sprintf("thread_id = $%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, string(filter.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.RiskLevel != "" {
		args = append(args, string(filter.RiskLevel))
		where = append(where, fmt.Sprintf("risk_level = $%d", len(args)))
	}
	query := "SELECT " + candidateColumns + " FROM candidate_memories"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PGRepository) UpdateCandidateStatus(ctx context.Context, orgID, candidateID string, status Status, scores Scores) (Candidate, error) {
	if err := r.check(); err != nil {
		return Candidate{}, err
	}
	scoresBytes, err := json.Marshal(scores)
	if err != nil {
		return Candidate{}, err
	}
	query := `UPDATE candidate_memories SET status=$1, scores=$2, updated_at=now()
	WHERE org_id=$3 AND candidate_id=$4 RETURNING ` + candidateColumns
	row := r.pool.QueryRow(ctx, query, string(status), scoresBytes, orgID, candidateID)
	return scanCandidate(row)
}

// UpsertJob 幂等:同 idempotency_key 返回已存在任务,不新建。
// DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key 是 noop,仅为触发 RETURNING 返回行。
func (r *PGRepository) UpsertJob(ctx context.Context, job Job) (Job, error) {
	if err := r.check(); err != nil {
		return Job{}, err
	}
	maxAttempts := job.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	status := string(job.Status)
	if status == "" {
		status = string(JobPending)
	}
	query := `INSERT INTO candidate_memory_jobs (
		idempotency_key, org_id, project_id, source_key, source_event_id, status, max_attempts
	) VALUES ($1,$2,$3,$4,$5,$6,$7)
	ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
	RETURNING ` + jobColumns
	row := r.pool.QueryRow(ctx, query,
		job.IdempotencyKey, job.OrgID, job.ProjectID, job.SourceKey, job.SourceEventID, status, maxAttempts,
	)
	return scanJob(row)
}

// LeaseJob 抢占一个 pending 或运行锁过期的任务(FOR UPDATE SKIP LOCKED,与 archive_jobs 风格一致)。
// 无可抢占任务时返回 (nil, nil)。
func (r *PGRepository) LeaseJob(ctx context.Context, now time.Time, lockedBy string, lockTTL time.Duration) (*Job, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	until := now.Add(lockTTL)
	query := `UPDATE candidate_memory_jobs
		SET status='running', locked_by=$1, locked_until=$2, attempts=attempts+1, updated_at=now()
		WHERE id = (
			SELECT id FROM candidate_memory_jobs
			WHERE status='pending' OR (status='running' AND locked_until < now())
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING ` + jobColumns
	row := r.pool.QueryRow(ctx, query, lockedBy, until)
	j, err := scanJob(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &j, nil
}

func (r *PGRepository) CompleteJob(ctx context.Context, id int64, candidateIDs []string) error {
	if err := r.check(); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `UPDATE candidate_memory_jobs
		SET status='done', candidate_ids=$1, completed_at=now(), last_error='', updated_at=now()
		WHERE id=$2`, candidateIDs, id)
	return err
}

func (r *PGRepository) FailJob(ctx context.Context, id int64, lastError string) error {
	if err := r.check(); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `UPDATE candidate_memory_jobs
		SET last_error=$1, locked_by=NULL, locked_until=NULL,
			status=CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
			updated_at=now()
		WHERE id=$2`, lastError, id)
	return err
}

func (r *PGRepository) UpsertTopicState(ctx context.Context, ts TopicState) (TopicState, error) {
	if err := r.check(); err != nil {
		return TopicState{}, err
	}
	query := `INSERT INTO topic_memory_states (
		org_id, project_id, source_key, thread_id, candidate_count, completion_score, last_event_at, ready_to_compose, composed_archive_id
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	ON CONFLICT (org_id, project_id, source_key, thread_id) DO UPDATE SET
		candidate_count=EXCLUDED.candidate_count,
		completion_score=EXCLUDED.completion_score,
		last_event_at=EXCLUDED.last_event_at,
		ready_to_compose=EXCLUDED.ready_to_compose,
		composed_archive_id=EXCLUDED.composed_archive_id,
		updated_at=now()
	RETURNING ` + topicColumns
	row := r.pool.QueryRow(ctx, query,
		ts.OrgID, ts.ProjectID, ts.SourceKey, ts.ThreadID, ts.CandidateCount, ts.CompletionScore,
		ts.LastEventAt, ts.ReadyToCompose, ts.ComposedArchiveID,
	)
	return scanTopicState(row)
}

func (r *PGRepository) GetTopicState(ctx context.Context, orgID, projectID, sourceKey, threadID string) (TopicState, error) {
	if err := r.check(); err != nil {
		return TopicState{}, err
	}
	query := "SELECT " + topicColumns + " FROM topic_memory_states WHERE org_id=$1 AND project_id=$2 AND source_key=$3 AND thread_id=$4"
	row := r.pool.QueryRow(ctx, query, orgID, projectID, sourceKey, threadID)
	return scanTopicState(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCandidate(row rowScanner) (Candidate, error) {
	var c Candidate
	var memoryType, riskLevel, status string
	var similarRefs, scores []byte
	if err := row.Scan(
		&c.CandidateID, &c.OrgID, &c.ProjectID, &c.SourceKey, &c.UserID, &c.AgentID, &c.ThreadID, &c.SessionID,
		&c.SourceEventIDs, &memoryType, &c.Content, &c.Summary, &riskLevel, &c.Confidence, &status,
		&similarRefs, &scores, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Candidate{}, ErrNotFound
		}
		return Candidate{}, err
	}
	c.MemoryType = MemoryType(memoryType)
	c.RiskLevel = RiskLevel(riskLevel)
	c.Status = Status(status)
	if err := decodeCandidateJSONB(similarRefs, scores, &c); err != nil {
		return Candidate{}, err
	}
	return c, nil
}

func scanJob(row rowScanner) (Job, error) {
	var j Job
	var status string
	var lockedBy *string
	var lockedUntil, completedAt *time.Time
	if err := row.Scan(
		&j.ID, &j.IdempotencyKey, &j.OrgID, &j.ProjectID, &j.SourceKey, &j.SourceEventID,
		&status, &j.Attempts, &j.MaxAttempts, &lockedBy, &lockedUntil, &j.LastError, &j.CandidateIDs,
		&j.CreatedAt, &j.UpdatedAt, &completedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}
	j.Status = JobStatus(status)
	if lockedBy != nil {
		j.LockedBy = *lockedBy
	}
	j.LockedUntil = lockedUntil
	j.CompletedAt = completedAt
	return j, nil
}

func scanTopicState(row rowScanner) (TopicState, error) {
	var ts TopicState
	var lastEventAt *time.Time
	if err := row.Scan(
		&ts.ID, &ts.OrgID, &ts.ProjectID, &ts.SourceKey, &ts.ThreadID, &ts.CandidateCount, &ts.CompletionScore,
		&lastEventAt, &ts.ReadyToCompose, &ts.ComposedArchiveID, &ts.CreatedAt, &ts.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TopicState{}, ErrNotFound
		}
		return TopicState{}, err
	}
	ts.LastEventAt = lastEventAt
	return ts, nil
}

func encodeCandidateJSONB(c Candidate) (similarRefs, scores []byte, err error) {
	similarRefs, err = json.Marshal(c.SimilarRefs)
	if err != nil {
		return nil, nil, err
	}
	if len(c.SimilarRefs) == 0 {
		similarRefs = []byte("[]")
	}
	scores, err = json.Marshal(c.Scores)
	if err != nil {
		return nil, nil, err
	}
	return similarRefs, scores, nil
}

func decodeCandidateJSONB(similarRefs, scores []byte, c *Candidate) error {
	if len(similarRefs) > 0 {
		if err := json.Unmarshal(similarRefs, &c.SimilarRefs); err != nil {
			return err
		}
	}
	if len(scores) > 0 {
		if err := json.Unmarshal(scores, &c.Scores); err != nil {
			return err
		}
	}
	return nil
}
