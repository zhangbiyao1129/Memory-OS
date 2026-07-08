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
	triageResultColumns      = "id, org_id, candidate_id, source_project_id, source_key, triage_scope, confidence, review_state, reason, source_refs, promoted_hot_memory_ids, attempts, last_error, created_at, updated_at"
	triageProjectLinkColumns = "id, org_id, candidate_id, linked_project_id, linked_source_key, confidence, evidence, status, promoted_hot_memory_id, created_at, updated_at"
)

// PGTriageRepository 基于 PostgreSQL 的 TriageRepository 实现。
type PGTriageRepository struct {
	pool *pgxpool.Pool
}

func NewPGTriageRepository(pool *pgxpool.Pool) *PGTriageRepository {
	return &PGTriageRepository{pool: pool}
}

func (r *PGTriageRepository) check() error {
	if r == nil || r.pool == nil {
		return errors.New("triage repository is not configured")
	}
	return nil
}

var _ TriageRepository = (*PGTriageRepository)(nil)

func (r *PGTriageRepository) ListCandidatesNeedingTriage(ctx context.Context, filter TriageScanFilter) ([]Candidate, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(filter.OrgID) == "" {
		return nil, errors.New("org_id is required")
	}
	limit := triageClampLimit(filter.Limit)

	where := []string{"cm.org_id = $1", "cm.status <> 'discarded'", "ctr.candidate_id IS NULL"}
	args := []any{filter.OrgID}
	if filter.MinConfidence > 0 {
		args = append(args, filter.MinConfidence)
		where = append(where, fmt.Sprintf("cm.confidence >= $%d", len(args)))
	}
	args = append(args, limit)
	query := `SELECT cm.candidate_id, cm.org_id, cm.project_id, cm.source_key, cm.user_id, cm.agent_id, cm.thread_id, cm.session_id,
	cm.source_event_ids, cm.memory_type, cm.content, cm.summary, cm.risk_level, cm.confidence, cm.status,
	cm.similar_refs, cm.scores, cm.created_at, cm.updated_at
FROM candidate_memories cm
LEFT JOIN candidate_triage_results ctr
  ON ctr.org_id = cm.org_id AND ctr.candidate_id = cm.candidate_id
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY cm.created_at ASC
LIMIT $` + fmt.Sprintf("%d", len(args))

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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > limit {
		return out[:limit], nil
	}
	return out, nil
}

func (r *PGTriageRepository) ListTriageResults(ctx context.Context, filter TriageListFilter) ([]TriageResult, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(filter.OrgID) == "" {
		return nil, errors.New("org_id is required")
	}
	where := []string{"org_id = $1"}
	args := []any{filter.OrgID}
	if filter.SourceProjectID != "" {
		args = append(args, filter.SourceProjectID)
		where = append(where, fmt.Sprintf("source_project_id = $%d", len(args)))
	}
	if filter.SourceKey != "" {
		args = append(args, filter.SourceKey)
		where = append(where, fmt.Sprintf("source_key = $%d", len(args)))
	}
	if filter.ReviewState != "" {
		args = append(args, string(filter.ReviewState))
		where = append(where, fmt.Sprintf("review_state = $%d", len(args)))
	}
	limit := triageClampLimit(filter.Limit)
	args = append(args, limit)
	limitPlaceholder := len(args)
	args = append(args, filter.Offset)
	offsetPlaceholder := len(args)
	query := "SELECT " + triageResultColumns + " FROM candidate_triage_results WHERE " + strings.Join(where, " AND ") +
		fmt.Sprintf(" ORDER BY updated_at DESC, candidate_id ASC LIMIT $%d OFFSET $%d", limitPlaceholder, offsetPlaceholder)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TriageResult{}
	for rows.Next() {
		result, err := scanTriageResult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PGTriageRepository) UpsertTriageResult(ctx context.Context, result TriageResult) (TriageResult, error) {
	if err := r.check(); err != nil {
		return TriageResult{}, err
	}
	if strings.TrimSpace(result.OrgID) == "" || strings.TrimSpace(result.CandidateID) == "" {
		return TriageResult{}, errors.New("org_id and candidate_id are required")
	}
	if !result.TriageScope.Valid() {
		return TriageResult{}, errors.New("invalid triage scope")
	}
	if !result.ReviewState.Valid() {
		result.ReviewState = TriageReviewWeak
	}
	sourceRefs, err := json.Marshal(result.SourceRefs)
	if err != nil {
		return TriageResult{}, err
	}
	query := `INSERT INTO candidate_triage_results (
		org_id, candidate_id, source_project_id, source_key, triage_scope, confidence,
		review_state, reason, source_refs, promoted_hot_memory_ids, attempts, last_error
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	ON CONFLICT (org_id, candidate_id) DO UPDATE SET
		source_project_id = EXCLUDED.source_project_id,
		source_key = EXCLUDED.source_key,
		triage_scope = EXCLUDED.triage_scope,
		confidence = EXCLUDED.confidence,
		review_state = EXCLUDED.review_state,
		reason = EXCLUDED.reason,
		source_refs = EXCLUDED.source_refs,
		promoted_hot_memory_ids = EXCLUDED.promoted_hot_memory_ids,
		attempts = EXCLUDED.attempts,
		last_error = EXCLUDED.last_error,
		updated_at = now()
	RETURNING ` + triageResultColumns
	row := r.pool.QueryRow(ctx,
		query,
		result.OrgID,
		result.CandidateID,
		result.SourceProjectID,
		result.SourceKey,
		string(result.TriageScope),
		result.Confidence,
		string(result.ReviewState),
		result.Reason,
		string(sourceRefs),
		result.PromotedHotMemoryIDs,
		result.Attempts,
		result.LastError,
	)
	return scanTriageResult(row)
}

func (r *PGTriageRepository) GetTriageResult(ctx context.Context, orgID, candidateID string) (TriageResult, error) {
	if err := r.check(); err != nil {
		return TriageResult{}, err
	}
	row := r.pool.QueryRow(ctx, `SELECT `+triageResultColumns+` FROM candidate_triage_results WHERE org_id=$1 AND candidate_id=$2`, orgID, candidateID)
	return scanTriageResult(row)
}

func (r *PGTriageRepository) ReplaceProjectLinks(ctx context.Context, orgID, candidateID string, links []CandidateProjectLink) error {
	if err := r.check(); err != nil {
		return err
	}
	if len(links) == 0 {
		_, err := r.pool.Exec(ctx, `DELETE FROM candidate_project_links WHERE org_id=$1 AND candidate_id=$2`, orgID, candidateID)
		return err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err = tx.Exec(ctx, `DELETE FROM candidate_project_links WHERE org_id=$1 AND candidate_id=$2`, orgID, candidateID); err != nil {
		return err
	}
	for _, link := range links {
		if strings.TrimSpace(link.LinkedProjectID) == "" {
			continue
		}
		createdAt := link.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO candidate_project_links (org_id, candidate_id, linked_project_id, linked_source_key, confidence, evidence, status, promoted_hot_memory_id, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			orgID,
			candidateID,
			link.LinkedProjectID,
			link.LinkedSourceKey,
			link.Confidence,
			link.Evidence,
			link.Status,
			link.PromotedHotMemoryID,
			createdAt,
			createdAt,
		)
		if err != nil {
			return err
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (r *PGTriageRepository) ListProjectLinks(ctx context.Context, filter CandidateProjectLinksFilter) ([]CandidateProjectLink, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	where := []string{}
	args := []any{}
	idx := 1
	if filter.OrgID != "" {
		where = append(where, fmt.Sprintf("org_id = $%d", idx))
		args = append(args, filter.OrgID)
		idx++
	}
	if filter.CandidateID != "" {
		where = append(where, fmt.Sprintf("candidate_id = $%d", idx))
		args = append(args, filter.CandidateID)
		idx++
	}
	if filter.LinkedProjectID != "" {
		where = append(where, fmt.Sprintf("linked_project_id = $%d", idx))
		args = append(args, filter.LinkedProjectID)
		idx++
	}
	if filter.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, filter.Status)
		idx++
	}
	if filter.MinConfidence > 0 {
		where = append(where, fmt.Sprintf("confidence >= $%d", idx))
		args = append(args, filter.MinConfidence)
		idx++
	}
	query := "SELECT " + triageProjectLinkColumns + " FROM candidate_project_links"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY confidence DESC, updated_at DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", triageClampLimit(filter.Limit))
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CandidateProjectLink{}
	for rows.Next() {
		link, err := scanCandidateProjectLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PGTriageRepository) UpdatePromotedHotMemoryIDs(ctx context.Context, orgID, candidateID string, ids []string) error {
	if err := r.check(); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE candidate_triage_results
		SET promoted_hot_memory_ids=$1, updated_at=now()
		WHERE org_id=$2 AND candidate_id=$3`,
		ids, orgID, candidateID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGTriageRepository) UpdateProjectLinkPromotion(ctx context.Context, orgID, candidateID, linkedProjectID, memoryID string) error {
	if err := r.check(); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE candidate_project_links
		SET promoted_hot_memory_id=$1, updated_at=now()
		WHERE org_id=$2 AND candidate_id=$3 AND linked_project_id=$4`,
		memoryID, orgID, candidateID, linkedProjectID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}

func scanTriageResult(row pgx.Row) (TriageResult, error) {
	var r TriageResult
	var triageScope, reviewState string
	var sourceRefs []byte
	if err := row.Scan(
		&r.ID, &r.OrgID, &r.CandidateID, &r.SourceProjectID, &r.SourceKey, &triageScope,
		&r.Confidence, &reviewState, &r.Reason, &sourceRefs, &r.PromotedHotMemoryIDs, &r.Attempts, &r.LastError, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TriageResult{}, ErrNotFound
		}
		return TriageResult{}, err
	}
	r.TriageScope = TriageScope(triageScope)
	r.ReviewState = normalizeReviewState(TriageReviewState(reviewState))
	if len(sourceRefs) > 0 {
		if err := json.Unmarshal(sourceRefs, &r.SourceRefs); err != nil {
			return TriageResult{}, err
		}
	}
	return r, nil
}

func scanCandidateProjectLink(row pgx.Row) (CandidateProjectLink, error) {
	var l CandidateProjectLink
	if err := row.Scan(
		&l.ID, &l.OrgID, &l.CandidateID, &l.LinkedProjectID, &l.LinkedSourceKey,
		&l.Confidence, &l.Evidence, &l.Status, &l.PromotedHotMemoryID, &l.CreatedAt, &l.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CandidateProjectLink{}, ErrNotFound
		}
		return CandidateProjectLink{}, err
	}
	return l, nil
}
