package memorykernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"memory-os/internal/secret"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	unitColumns   = "id, unit_id, org_id, project_id, source_key, thread_id, user_id, agent_id, unit_type, content, applies_when, agent_should, status, confidence, trust_score, risk_level, source_refs, superseded_by, valid_from, valid_to, created_at, updated_at"
	claimColumns  = "id, claim_id, unit_id, org_id, project_id, subject, predicate, value, polarity, confidence, evidence_refs, observed_at, created_at"
	runColumns    = "id, run_id, org_id, project_id, source_key, thread_id, trigger_type, status, processed_candidates, processed_hot_memories, processed_archives, created_units, superseded_units, stale_candidates, demoted_hot_memories, ci_cases_created, ci_cases_passed, correction_archive_id, summary, last_error, started_at, completed_at, updated_at"
	actionColumns = "id, action_id, run_id, org_id, project_id, target_type, target_id, action, reason, evidence_refs, applied, created_at"
	ciCaseColumns = "id, case_id, org_id, project_id, source_key, question, must_include, must_not_include, status, source_run_id, created_at, updated_at"
	ciResultColumns = "id, result_id, case_id, run_id, request_id, passed, matched_include, matched_exclude, response_excerpt, created_at"
)

// PGRepository PostgreSQL 实现，所有写操作以 org_id 限定。
type PGRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

func (r *PGRepository) check() error {
	if r == nil || r.pool == nil {
		return errors.New("memory kernel postgres repository is not configured")
	}
	return nil
}

func (r *PGRepository) CreateRun(ctx context.Context, run GovernanceRun) (GovernanceRun, error) {
	if err := r.check(); err != nil {
		return GovernanceRun{}, err
	}
	status := string(run.Status)
	if status == "" {
		status = string(RunRunning)
	}
	triggerType := run.TriggerType
	if triggerType == "" {
		triggerType = "manual"
	}
	query := `INSERT INTO memory_governance_runs (
		run_id, org_id, project_id, source_key, thread_id, trigger_type, status
	) VALUES ($1,$2,$3,$4,$5,$6,$7)
	ON CONFLICT (run_id) DO UPDATE SET run_id=EXCLUDED.run_id
	RETURNING ` + runColumns
	row := r.pool.QueryRow(ctx, query,
		run.RunID, run.OrgID, run.ProjectID, run.SourceKey, run.ThreadID, triggerType, status,
	)
	return scanRun(row)
}

func (r *PGRepository) CompleteRun(ctx context.Context, runID string, update GovernanceRunUpdate) error {
	if err := r.check(); err != nil {
		return err
	}
	query := `UPDATE memory_governance_runs SET
		status=$1, processed_candidates=$2, processed_hot_memories=$3, processed_archives=$4,
		created_units=$5, superseded_units=$6, stale_candidates=$7, demoted_hot_memories=$8,
		ci_cases_created=$9, ci_cases_passed=$10, correction_archive_id=$11, summary=$12,
		completed_at=now(), updated_at=now()
	WHERE run_id=$13`
	tag, err := r.pool.Exec(ctx, query,
		string(update.Status), update.ProcessedCandidates, update.ProcessedHotMemories,
		update.ProcessedArchives, update.CreatedUnits, update.SupersededUnits,
		update.StaleCandidates, update.DemotedHotMemories, update.CICasesCreated,
		update.CICasesPassed, update.CorrectionArchiveID, update.Summary, runID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUnitNotFound
	}
	return nil
}

func (r *PGRepository) FailRun(ctx context.Context, runID string, runErr error) error {
	if err := r.check(); err != nil {
		return err
	}
	sanitized := secret.Sanitize(runErr.Error(), nil)
	lastErr := sanitized.Text
	if len(lastErr) > 1000 {
		lastErr = lastErr[:1000]
	}
	query := `UPDATE memory_governance_runs SET status='failed', last_error=$1, completed_at=now(), updated_at=now()
	WHERE run_id=$2`
	tag, err := r.pool.Exec(ctx, query, lastErr, runID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUnitNotFound
	}
	return nil
}

func (r *PGRepository) GetRun(ctx context.Context, runID string) (GovernanceRun, error) {
	if err := r.check(); err != nil {
		return GovernanceRun{}, err
	}
	query := "SELECT " + runColumns + " FROM memory_governance_runs WHERE run_id=$1"
	row := r.pool.QueryRow(ctx, query, runID)
	return scanRun(row)
}

func (r *PGRepository) ListRuns(ctx context.Context, filter RunFilter) ([]GovernanceRun, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	where, args := buildRunFilter(filter)
	query := "SELECT " + runColumns + " FROM memory_governance_runs"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY started_at DESC"
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GovernanceRun
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *PGRepository) UpsertUnit(ctx context.Context, unit MemoryUnit) (MemoryUnit, error) {
	if err := r.check(); err != nil {
		return MemoryUnit{}, err
	}
	sourceRefs, err := json.Marshal(unit.SourceRefs)
	if err != nil {
		return MemoryUnit{}, err
	}
	if len(sourceRefs) == 0 {
		sourceRefs = []byte("[]")
	}
	query := `INSERT INTO memory_units (
		unit_id, org_id, project_id, source_key, thread_id, user_id, agent_id,
		unit_type, content, applies_when, agent_should, status, confidence, trust_score,
		risk_level, source_refs, superseded_by, valid_from, valid_to
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
	ON CONFLICT (unit_id) DO UPDATE SET
		status=EXCLUDED.status, content=EXCLUDED.content, applies_when=EXCLUDED.applies_when,
		agent_should=EXCLUDED.agent_should, source_refs=EXCLUDED.source_refs,
		trust_score=GREATEST(memory_units.trust_score, EXCLUDED.trust_score),
		updated_at=now()
	RETURNING ` + unitColumns
	row := r.pool.QueryRow(ctx, query,
		unit.UnitID, unit.OrgID, unit.ProjectID, unit.SourceKey, unit.ThreadID, unit.UserID, unit.AgentID,
		string(unit.Type), unit.Content, unit.AppliesWhen, unit.AgentShould, string(unit.Status),
		unit.Confidence, unit.TrustScore, unit.RiskLevel, sourceRefs, unit.SupersededBy,
		unit.ValidFrom, unit.ValidTo,
	)
	return scanUnit(row)
}

func (r *PGRepository) UpsertClaim(ctx context.Context, claim MemoryClaim) (MemoryClaim, error) {
	if err := r.check(); err != nil {
		return MemoryClaim{}, err
	}
	evidenceRefs, err := json.Marshal(claim.EvidenceRefs)
	if err != nil {
		return MemoryClaim{}, err
	}
	if len(evidenceRefs) == 0 {
		evidenceRefs = []byte("[]")
	}
	query := `INSERT INTO memory_claims (
		claim_id, unit_id, org_id, project_id, subject, predicate, value, polarity, confidence, evidence_refs, observed_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	ON CONFLICT (unit_id, subject, predicate) DO UPDATE SET
		value=EXCLUDED.value, polarity=EXCLUDED.polarity, confidence=EXCLUDED.confidence, evidence_refs=EXCLUDED.evidence_refs
	RETURNING ` + claimColumns
	row := r.pool.QueryRow(ctx, query,
		claim.ClaimID, claim.UnitID, claim.OrgID, claim.ProjectID,
		claim.Subject, claim.Predicate, claim.Value, claim.Polarity,
		claim.Confidence, evidenceRefs, claim.ObservedAt,
	)
	return scanClaim(row)
}

func (r *PGRepository) ListUnits(ctx context.Context, filter UnitFilter) ([]MemoryUnit, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	where, args := buildUnitFilter(filter)
	query := "SELECT " + unitColumns + " FROM memory_units"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryUnit
	for rows.Next() {
		u, err := scanUnit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *PGRepository) ListClaims(ctx context.Context, filter ClaimFilter) ([]MemoryClaim, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	where, args := buildClaimFilter(filter)
	query := "SELECT " + claimColumns + " FROM memory_claims"
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
	var out []MemoryClaim
	for rows.Next() {
		c, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PGRepository) MarkUnitSuperseded(ctx context.Context, orgID, unitID, supersededBy, reason string) (MemoryUnit, error) {
	if err := r.check(); err != nil {
		return MemoryUnit{}, err
	}
	sanitized := secret.Sanitize(reason, nil)
	query := `UPDATE memory_units SET status='superseded', superseded_by=$1, updated_at=now()
	WHERE org_id=$2 AND unit_id=$3 RETURNING ` + unitColumns
	row := r.pool.QueryRow(ctx, query, supersededBy, orgID, unitID)
	_ = sanitized.Text // reason 已脱敏但不存入 memory_units，仅审计
	return scanUnit(row)
}

func (r *PGRepository) RecordAction(ctx context.Context, action GovernanceAction) (GovernanceAction, error) {
	if err := r.check(); err != nil {
		return GovernanceAction{}, err
	}
	evidenceRefs, err := json.Marshal(action.EvidenceRefs)
	if err != nil {
		return GovernanceAction{}, err
	}
	if len(evidenceRefs) == 0 {
		evidenceRefs = []byte("[]")
	}
	query := `INSERT INTO memory_governance_actions (
		action_id, run_id, org_id, project_id, target_type, target_id, action, reason, evidence_refs, applied
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	ON CONFLICT (action_id) DO UPDATE SET action_id=EXCLUDED.action_id
	RETURNING ` + actionColumns
	row := r.pool.QueryRow(ctx, query,
		action.ActionID, action.RunID, action.OrgID, action.ProjectID,
		action.TargetType, action.TargetID, string(action.Action), action.Reason,
		evidenceRefs, action.Applied,
	)
	return scanAction(row)
}

func (r *PGRepository) ListActions(ctx context.Context, filter ActionFilter) ([]GovernanceAction, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	where, args := buildActionFilter(filter)
	query := "SELECT " + actionColumns + " FROM memory_governance_actions"
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
	var out []GovernanceAction
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PGRepository) UpsertCICase(ctx context.Context, c CICase) (CICase, error) {
	if err := r.check(); err != nil {
		return CICase{}, err
	}
	query := `INSERT INTO memory_ci_cases (
		case_id, org_id, project_id, source_key, question, must_include, must_not_include, status, source_run_id
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	ON CONFLICT (case_id) DO UPDATE SET
		must_include=EXCLUDED.must_include, must_not_include=EXCLUDED.must_not_include,
		status=EXCLUDED.status, updated_at=now()
	RETURNING ` + ciCaseColumns
	row := r.pool.QueryRow(ctx, query,
		c.CaseID, c.OrgID, c.ProjectID, c.SourceKey, c.Question,
		c.MustInclude, c.MustNotInclude, c.Status, c.SourceRunID,
	)
	return scanCICase(row)
}

func (r *PGRepository) RecordCIResult(ctx context.Context, result CIResult) (CIResult, error) {
	if err := r.check(); err != nil {
		return CIResult{}, err
	}
	query := `INSERT INTO memory_ci_results (
		result_id, case_id, run_id, request_id, passed, matched_include, matched_exclude, response_excerpt
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	ON CONFLICT (result_id) DO UPDATE SET result_id=EXCLUDED.result_id
	RETURNING ` + ciResultColumns
	row := r.pool.QueryRow(ctx, query,
		result.ResultID, result.CaseID, result.RunID, result.RequestID,
		result.Passed, result.MatchedInclude, result.MatchedExclude, result.ResponseExcerpt,
	)
	return scanCIResult(row)
}

func (r *PGRepository) ListCICases(ctx context.Context, filter CICaseFilter) ([]CICase, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	where, args := buildCICaseFilter(filter)
	query := "SELECT " + ciCaseColumns + " FROM memory_ci_cases"
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
	var out []CICase
	for rows.Next() {
		c, err := scanCICase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PGRepository) ListCIResults(ctx context.Context, filter CIResultFilter) ([]CIResult, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	where, args := buildCIResultFilter(filter)
	query := "SELECT " + ciResultColumns + " FROM memory_ci_results"
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
	var out []CIResult
	for rows.Next() {
		res, err := scanCIResult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

// --- scan helpers ---

type pgRowScanner interface {
	Scan(dest ...any) error
}

func scanUnit(row pgRowScanner) (MemoryUnit, error) {
	var u MemoryUnit
	var unitType, status string
	var sourceRefs []byte
	var validFrom, validTo *time.Time
	if err := row.Scan(
		&u.ID, &u.UnitID, &u.OrgID, &u.ProjectID, &u.SourceKey, &u.ThreadID, &u.UserID, &u.AgentID,
		&unitType, &u.Content, &u.AppliesWhen, &u.AgentShould, &status, &u.Confidence, &u.TrustScore,
		&u.RiskLevel, &sourceRefs, &u.SupersededBy, &validFrom, &validTo, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MemoryUnit{}, ErrUnitNotFound
		}
		return MemoryUnit{}, err
	}
	u.Type = UnitType(unitType)
	u.Status = UnitStatus(status)
	u.ValidFrom = validFrom
	u.ValidTo = validTo
	if len(sourceRefs) > 0 {
		_ = json.Unmarshal(sourceRefs, &u.SourceRefs)
	}
	return u, nil
}

func scanClaim(row pgRowScanner) (MemoryClaim, error) {
	var c MemoryClaim
	var evidenceRefs []byte
	var observedAt *time.Time
	if err := row.Scan(
		&c.ID, &c.ClaimID, &c.UnitID, &c.OrgID, &c.ProjectID,
		&c.Subject, &c.Predicate, &c.Value, &c.Polarity, &c.Confidence,
		&evidenceRefs, &observedAt, &c.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MemoryClaim{}, ErrClaimNotFound
		}
		return MemoryClaim{}, err
	}
	c.ObservedAt = observedAt
	if len(evidenceRefs) > 0 {
		_ = json.Unmarshal(evidenceRefs, &c.EvidenceRefs)
	}
	return c, nil
}

func scanRun(row pgRowScanner) (GovernanceRun, error) {
	var run GovernanceRun
	var status string
	var completedAt *time.Time
	if err := row.Scan(
		&run.ID, &run.RunID, &run.OrgID, &run.ProjectID, &run.SourceKey, &run.ThreadID,
		&run.TriggerType, &status, &run.ProcessedCandidates, &run.ProcessedHotMemories,
		&run.ProcessedArchives, &run.CreatedUnits, &run.SupersededUnits, &run.StaleCandidates,
		&run.DemotedHotMemories, &run.CICasesCreated, &run.CICasesPassed,
		&run.CorrectionArchiveID, &run.Summary, &run.LastError,
		&run.StartedAt, &completedAt, &run.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GovernanceRun{}, ErrUnitNotFound
		}
		return GovernanceRun{}, err
	}
	run.Status = RunStatus(status)
	run.CompletedAt = completedAt
	return run, nil
}

func scanAction(row pgRowScanner) (GovernanceAction, error) {
	var a GovernanceAction
	var action string
	var evidenceRefs []byte
	if err := row.Scan(
		&a.ID, &a.ActionID, &a.RunID, &a.OrgID, &a.ProjectID,
		&a.TargetType, &a.TargetID, &action, &a.Reason, &evidenceRefs, &a.Applied, &a.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GovernanceAction{}, ErrUnitNotFound
		}
		return GovernanceAction{}, err
	}
	a.Action = GovernanceActionName(action)
	if len(evidenceRefs) > 0 {
		_ = json.Unmarshal(evidenceRefs, &a.EvidenceRefs)
	}
	return a, nil
}

func scanCICase(row pgRowScanner) (CICase, error) {
	var c CICase
	if err := row.Scan(
		&c.ID, &c.CaseID, &c.OrgID, &c.ProjectID, &c.SourceKey, &c.Question,
		&c.MustInclude, &c.MustNotInclude, &c.Status, &c.SourceRunID, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CICase{}, ErrUnitNotFound
		}
		return CICase{}, err
	}
	return c, nil
}

func scanCIResult(row pgRowScanner) (CIResult, error) {
	var res CIResult
	if err := row.Scan(
		&res.ID, &res.ResultID, &res.CaseID, &res.RunID, &res.RequestID,
		&res.Passed, &res.MatchedInclude, &res.MatchedExclude, &res.ResponseExcerpt, &res.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CIResult{}, ErrUnitNotFound
		}
		return CIResult{}, err
	}
	return res, nil
}

// --- filter builders ---

func buildRunFilter(f RunFilter) ([]string, []any) {
	var where []string
	var args []any
	if f.OrgID != "" {
		args = append(args, f.OrgID)
		where = append(where, fmt.Sprintf("org_id=$%d", len(args)))
	}
	if f.ProjectID != "" {
		args = append(args, f.ProjectID)
		where = append(where, fmt.Sprintf("project_id=$%d", len(args)))
	}
	if f.Status != "" {
		args = append(args, string(f.Status))
		where = append(where, fmt.Sprintf("status=$%d", len(args)))
	}
	return where, args
}

func buildUnitFilter(f UnitFilter) ([]string, []any) {
	var where []string
	var args []any
	if f.OrgID != "" {
		args = append(args, f.OrgID)
		where = append(where, fmt.Sprintf("org_id=$%d", len(args)))
	}
	if f.ProjectID != "" {
		args = append(args, f.ProjectID)
		where = append(where, fmt.Sprintf("project_id=$%d", len(args)))
	}
	if f.SourceKey != "" {
		args = append(args, f.SourceKey)
		where = append(where, fmt.Sprintf("source_key=$%d", len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf("status=$%d", len(args)))
	}
	return where, args
}

func buildClaimFilter(f ClaimFilter) ([]string, []any) {
	var where []string
	var args []any
	if f.OrgID != "" {
		args = append(args, f.OrgID)
		where = append(where, fmt.Sprintf("org_id=$%d", len(args)))
	}
	if f.ProjectID != "" {
		args = append(args, f.ProjectID)
		where = append(where, fmt.Sprintf("project_id=$%d", len(args)))
	}
	if f.UnitID != "" {
		args = append(args, f.UnitID)
		where = append(where, fmt.Sprintf("unit_id=$%d", len(args)))
	}
	return where, args
}

func buildActionFilter(f ActionFilter) ([]string, []any) {
	var where []string
	var args []any
	if f.OrgID != "" {
		args = append(args, f.OrgID)
		where = append(where, fmt.Sprintf("org_id=$%d", len(args)))
	}
	if f.ProjectID != "" {
		args = append(args, f.ProjectID)
		where = append(where, fmt.Sprintf("project_id=$%d", len(args)))
	}
	if f.RunID != "" {
		args = append(args, f.RunID)
		where = append(where, fmt.Sprintf("run_id=$%d", len(args)))
	}
	if f.TargetType != "" {
		args = append(args, f.TargetType)
		where = append(where, fmt.Sprintf("target_type=$%d", len(args)))
	}
	if f.TargetID != "" {
		args = append(args, f.TargetID)
		where = append(where, fmt.Sprintf("target_id=$%d", len(args)))
	}
	return where, args
}

func buildCICaseFilter(f CICaseFilter) ([]string, []any) {
	var where []string
	var args []any
	if f.OrgID != "" {
		args = append(args, f.OrgID)
		where = append(where, fmt.Sprintf("org_id=$%d", len(args)))
	}
	if f.ProjectID != "" {
		args = append(args, f.ProjectID)
		where = append(where, fmt.Sprintf("project_id=$%d", len(args)))
	}
	if f.CaseID != "" {
		args = append(args, f.CaseID)
		where = append(where, fmt.Sprintf("case_id=$%d", len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf("status=$%d", len(args)))
	}
	return where, args
}

func buildCIResultFilter(f CIResultFilter) ([]string, []any) {
	var where []string
	var args []any
	if f.CaseID != "" {
		args = append(args, f.CaseID)
		where = append(where, fmt.Sprintf("case_id=$%d", len(args)))
	}
	if f.RunID != "" {
		args = append(args, f.RunID)
		where = append(where, fmt.Sprintf("run_id=$%d", len(args)))
	}
	return where, args
}
