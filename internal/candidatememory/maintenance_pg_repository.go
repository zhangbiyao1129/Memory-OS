package candidatememory

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maintenanceRunColumns = "id, run_id, org_id, project_id, source_key, thread_id, trigger_type, status, stage, total_candidates, processed, discarded, kept, composed, archive_id, summary, last_error, locked_by, started_at, completed_at, created_at, updated_at"

// PGMaintenanceRepository 基于 pgx 的 MaintenanceRepository 实现。
type PGMaintenanceRepository struct {
	pool *pgxpool.Pool
}

func NewPGMaintenanceRepository(pool *pgxpool.Pool) *PGMaintenanceRepository {
	return &PGMaintenanceRepository{pool: pool}
}

func (r *PGMaintenanceRepository) CreateRun(ctx context.Context, run MaintenanceRun) (MaintenanceRun, error) {
	if r == nil || r.pool == nil {
		return MaintenanceRun{}, errors.New("maintenance repository is not configured")
	}
	query := `INSERT INTO candidate_maintenance_runs (
		run_id, org_id, project_id, source_key, thread_id, trigger_type, status, stage, total_candidates, started_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	RETURNING ` + maintenanceRunColumns
	row := r.pool.QueryRow(ctx, query,
		run.RunID, run.OrgID, run.ProjectID, run.SourceKey, run.ThreadID,
		string(run.TriggerType), string(run.Status), string(run.Stage), run.TotalCandidates, run.StartedAt,
	)
	return scanMaintenanceRun(row)
}

func (r *PGMaintenanceRepository) GetRun(ctx context.Context, runID string) (MaintenanceRun, error) {
	if r == nil || r.pool == nil {
		return MaintenanceRun{}, errors.New("maintenance repository is not configured")
	}
	query := "SELECT " + maintenanceRunColumns + " FROM candidate_maintenance_runs WHERE run_id=$1"
	row := r.pool.QueryRow(ctx, query, runID)
	return scanMaintenanceRun(row)
}

func (r *PGMaintenanceRepository) UpdateRun(ctx context.Context, runID string, status MaintenanceRunStatus, update MaintenanceRunUpdate) error {
	if r == nil || r.pool == nil {
		return errors.New("maintenance repository is not configured")
	}
	_, err := r.pool.Exec(ctx, `UPDATE candidate_maintenance_runs
		SET status=$1, processed=$2, discarded=$3, kept=$4, composed=$5,
		    archive_id=$6, summary=$7, last_error=$8, completed_at=$9, updated_at=now()
		WHERE run_id=$10`,
		string(status), update.Processed, update.Discarded, update.Kept, update.Composed,
		update.ArchiveID, update.Summary, update.LastError, update.CompletedAt, runID,
	)
	return err
}

func (r *PGMaintenanceRepository) GetRunningRun(ctx context.Context, orgID, projectID string) (*MaintenanceRun, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("maintenance repository is not configured")
	}
	query := "SELECT " + maintenanceRunColumns + " FROM candidate_maintenance_runs WHERE org_id=$1 AND project_id=$2 AND status='running' LIMIT 1"
	row := r.pool.QueryRow(ctx, query, orgID, projectID)
	run, err := scanMaintenanceRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

func (r *PGMaintenanceRepository) UpdateStage(ctx context.Context, runID string, stage MaintenanceRunStage, totalCandidates int) error {
	if r == nil || r.pool == nil {
		return errors.New("maintenance repository is not configured")
	}
	_, err := r.pool.Exec(ctx, `UPDATE candidate_maintenance_runs
		SET stage=$1, total_candidates=$2, updated_at=now()
		WHERE run_id=$3`,
		string(stage), totalCandidates, runID,
	)
	return err
}

func (r *PGMaintenanceRepository) MarkStaleRunningAsFailed(ctx context.Context, before time.Time) (int, error) {
	if r == nil || r.pool == nil {
		return 0, errors.New("maintenance repository is not configured")
	}
	tag, err := r.pool.Exec(ctx, `UPDATE candidate_maintenance_runs
		SET status='failed', stage='failed', last_error='stale: exceeded timeout', completed_at=now(), updated_at=now()
		WHERE status='running' AND started_at < $1`,
		before,
	)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func scanMaintenanceRun(row rowScanner) (MaintenanceRun, error) {
	var run MaintenanceRun
	var triggerType, status, stage string
	var completedAt *time.Time
	if err := row.Scan(
		&run.ID, &run.RunID, &run.OrgID, &run.ProjectID, &run.SourceKey, &run.ThreadID,
		&triggerType, &status, &stage, &run.TotalCandidates,
		&run.Processed, &run.Discarded, &run.Kept, &run.Composed,
		&run.ArchiveID, &run.Summary, &run.LastError, &run.LockedBy,
		&run.StartedAt, &completedAt, &run.CreatedAt, &run.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MaintenanceRun{}, ErrMaintenanceNotFound
		}
		return MaintenanceRun{}, err
	}
	run.TriggerType = MaintenanceTriggerType(triggerType)
	run.Status = MaintenanceRunStatus(status)
	run.Stage = MaintenanceRunStage(stage)
	run.CompletedAt = completedAt
	return run, nil
}
