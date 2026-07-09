package memorystats

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

func (r *PGRepository) Snapshot(ctx context.Context, filter Filter) (Snapshot, error) {
	if r == nil || r.pool == nil {
		return Snapshot{}, errors.New("memory stats postgres repository is not configured")
	}
	archives, err := r.archiveStats(ctx, filter)
	if err != nil {
		return Snapshot{}, err
	}
	hot, err := r.hotMemoryStats(ctx, filter)
	if err != nil {
		return Snapshot{}, err
	}
	candidates, err := r.candidateStats(ctx, filter)
	if err != nil {
		return Snapshot{}, err
	}
	candidateJobs, err := r.candidateJobStats(ctx, filter)
	if err != nil {
		return Snapshot{}, err
	}
	topics, err := r.topicStats(ctx, filter)
	if err != nil {
		return Snapshot{}, err
	}
	kernel, err := r.memoryKernelStats(ctx, filter)
	if err != nil {
		// memory kernel 表可能不存在（迁移前），返回零值
		kernel = MemoryKernelStats{}
	}
	return Snapshot{Archives: archives, HotMemories: hot, Candidates: candidates, CandidateJobs: candidateJobs, Topics: topics, MemoryKernel: kernel}, nil
}

func (r *PGRepository) archiveStats(ctx context.Context, filter Filter) (AssetStats, error) {
	stats := AssetStats{ByStatus: make(map[string]int64)}
	query := `SELECT status, count(*) FROM archives WHERE user_id=$1`
	args := []any{filter.UserID}
	if projectScoped(filter) {
		query += ` AND org_id=$2 AND project_id=$3`
		args = append(args, filter.OrgID, filter.ProjectID)
	}
	query += ` GROUP BY status`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return stats, err
		}
		stats.ByStatus[status] = count
	}
	stats.Total = stats.ByStatus["active"]
	return stats, rows.Err()
}

func (r *PGRepository) hotMemoryStats(ctx context.Context, filter Filter) (HotMemoryStats, error) {
	stats := HotMemoryStats{ByStatus: make(map[string]int64)}
	query := `SELECT status, count(*) FROM hot_memories WHERE user_id=$1 AND deleted_at IS NULL AND status <> 'deleted'`
	args := []any{filter.UserID}
	if projectScoped(filter) {
		query += ` AND org_id=$2 AND project_id=$3 AND permission_labels && $4::text[]`
		args = append(args, filter.OrgID, filter.ProjectID, filter.PermissionLabels)
	}
	query += ` GROUP BY status`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return stats, err
		}
		stats.ByStatus[status] = count
	}
	stats.Total = stats.ByStatus["active"] + stats.ByStatus["promoted"] + stats.ByStatus["demoted"]
	return stats, rows.Err()
}

func (r *PGRepository) candidateStats(ctx context.Context, filter Filter) (CandidateStats, error) {
	stats := CandidateStats{ByStatus: make(map[string]int64), ByRisk: make(map[string]int64)}

	// 按状态统计
	rows, err := r.pool.Query(ctx, scopedCandidateQuery(filter, `SELECT status, count(*) FROM candidate_memories`, `GROUP BY status`), scopedCandidateArgs(filter)...)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return stats, err
		}
		stats.ByStatus[status] = count
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}

	// 按风险等级统计
	rows, err = r.pool.Query(ctx, scopedCandidateQuery(filter, `SELECT risk_level, count(*) FROM candidate_memories`, `GROUP BY risk_level`), scopedCandidateArgs(filter)...)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var risk string
		var count int64
		if err := rows.Scan(&risk, &count); err != nil {
			return stats, err
		}
		stats.ByRisk[risk] = count
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}

	// hot_memory_score 分桶
	err = r.scoreBuckets(ctx, filter, "hot_memory_score", &stats.HotScoreBuckets)
	if err != nil {
		return stats, err
	}

	// compose_score 分桶
	err = r.scoreBuckets(ctx, filter, "compose_score", &stats.ComposeScoreBuckets)
	if err != nil {
		return stats, err
	}

	// 计算总数
	for _, count := range stats.ByStatus {
		stats.Total += count
	}

	query := `SELECT
		count(*) FILTER (WHERE status='pending' AND COALESCE(needs_review, false) = false),
		count(*) FILTER (WHERE status='in_compose_pool'),
		count(*) FILTER (WHERE status='pending' AND COALESCE(needs_review, false) = true)
	FROM candidate_memories
	WHERE ` + candidateWhereClause(filter)
	err = r.pool.QueryRow(ctx, query, scopedCandidateArgs(filter)...).Scan(&stats.PendingOrganizeTotal, &stats.ArchiveMaterialTotal, &stats.NeedsReviewTotal)
	if err != nil {
		return stats, err
	}

	// actionable_total 只表示需要人工确认的候选,归档素材单独统计。
	stats.ActionableTotal = stats.NeedsReviewTotal

	return stats, nil
}

func (r *PGRepository) scoreBuckets(ctx context.Context, filter Filter, scoreKey string, buckets *[]ScoreBucket) error {
	query := `SELECT
	  count(*) FILTER (WHERE COALESCE((scores->>'` + scoreKey + `')::double precision, 0) >= 0 AND COALESCE((scores->>'` + scoreKey + `')::double precision, 0) < 0.25),
	  count(*) FILTER (WHERE COALESCE((scores->>'` + scoreKey + `')::double precision, 0) >= 0.25 AND COALESCE((scores->>'` + scoreKey + `')::double precision, 0) < 0.5),
	  count(*) FILTER (WHERE COALESCE((scores->>'` + scoreKey + `')::double precision, 0) >= 0.5 AND COALESCE((scores->>'` + scoreKey + `')::double precision, 0) < 0.75),
	  count(*) FILTER (WHERE COALESCE((scores->>'` + scoreKey + `')::double precision, 0) >= 0.75)
	FROM candidate_memories
	WHERE ` + candidateWhereClause(filter)

	var c1, c2, c3, c4 int64
	err := r.pool.QueryRow(ctx, query, scopedCandidateArgs(filter)...).Scan(&c1, &c2, &c3, &c4)
	if err != nil {
		return err
	}
	*buckets = []ScoreBucket{
		{Label: "0-0.25", Count: c1},
		{Label: "0.25-0.5", Count: c2},
		{Label: "0.5-0.75", Count: c3},
		{Label: "0.75-1", Count: c4},
	}
	return nil
}

// candidateJobStats 候选提炼任务健康统计。
func (r *PGRepository) candidateJobStats(ctx context.Context, filter Filter) (CandidateJobStats, error) {
	stats := CandidateJobStats{ByStatus: make(map[string]int64)}

	// 按状态统计
	rows, err := r.pool.Query(ctx, scopedCandidateJobQuery(filter, `SELECT status, count(*) FROM candidate_memory_jobs`, `GROUP BY status`), scopedCandidateJobArgs(filter)...)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return stats, err
		}
		stats.ByStatus[status] = count
		stats.Total += count
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}

	// 派生各状态计数
	stats.Pending = stats.ByStatus["pending"]
	stats.Running = stats.ByStatus["running"]
	stats.Failed = stats.ByStatus["failed"]
	stats.Done = stats.ByStatus["done"]

	// 最近错误(截断到500字符,不含 secret)
	var latestError string
	err = r.pool.QueryRow(ctx, scopedCandidateJobQuery(filter, `SELECT last_error FROM candidate_memory_jobs`, `AND last_error <> '' ORDER BY updated_at DESC LIMIT 1`), scopedCandidateJobArgs(filter)...).Scan(&latestError)
	if err == nil {
		// 截断到 500 字符
		if len(latestError) > 500 {
			latestError = latestError[:500]
		}
		stats.LatestError = latestError
	} else if err != pgx.ErrNoRows {
		return stats, err
	}

	// 最早 pending 时间
	var oldestPendingAt interface{}
	err = r.pool.QueryRow(ctx, scopedCandidateJobQuery(filter, `SELECT MIN(created_at) FROM candidate_memory_jobs`, `AND status='pending'`), scopedCandidateJobArgs(filter)...).Scan(&oldestPendingAt)
	if err == nil && oldestPendingAt != nil {
		if t, ok := oldestPendingAt.(interface{ Format(string) string }); ok {
			stats.OldestPendingAt = t.Format("2006-01-02T15:04:05Z07:00")
		}
	} else if err != nil && err != pgx.ErrNoRows {
		return stats, err
	}

	// 最近完成时间
	var lastCompletedAt interface{}
	err = r.pool.QueryRow(ctx, scopedCandidateJobQuery(filter, `SELECT MAX(completed_at) FROM candidate_memory_jobs`, `AND status='done'`), scopedCandidateJobArgs(filter)...).Scan(&lastCompletedAt)
	if err == nil && lastCompletedAt != nil {
		if t, ok := lastCompletedAt.(interface{ Format(string) string }); ok {
			stats.LastCompletedAt = t.Format("2006-01-02T15:04:05Z07:00")
		}
	} else if err != nil && err != pgx.ErrNoRows {
		return stats, err
	}

	return stats, nil
}

func (r *PGRepository) topicStats(ctx context.Context, filter Filter) (TopicStats, error) {
	var stats TopicStats
	query := `SELECT
	  count(*),
	  count(*) FILTER (WHERE composed_archive_id = '' AND ready_to_compose),
	  count(*) FILTER (WHERE composed_archive_id <> ''),
	  count(*) FILTER (WHERE composed_archive_id = '' AND NOT ready_to_compose)
	FROM topic_memory_states
	WHERE ` + topicWhereClause(filter)
	err := r.pool.QueryRow(ctx, query, topicArgs(filter)...).Scan(&stats.Total, &stats.ReadyToCompose, &stats.Composed, &stats.Open)
	if err != nil {
		return stats, err
	}
	return stats, nil
}

func projectScoped(filter Filter) bool {
	return strings.TrimSpace(filter.OrgID) != "" && strings.TrimSpace(filter.ProjectID) != ""
}

func candidateWhereClause(filter Filter) string {
	if projectScoped(filter) {
		return "org_id=$1 AND project_id=$2"
	}
	return "user_id=$1"
}

func scopedCandidateArgs(filter Filter) []any {
	if projectScoped(filter) {
		return []any{filter.OrgID, filter.ProjectID}
	}
	return []any{filter.UserID}
}

func scopedCandidateQuery(filter Filter, prefix, suffix string) string {
	return prefix + " WHERE " + candidateWhereClause(filter) + " " + suffix
}

func candidateJobWhereClause(filter Filter) string {
	if projectScoped(filter) {
		return "org_id=$1 AND project_id=$2"
	}
	return `EXISTS (
		SELECT 1 FROM turn_events
		WHERE turn_events.event_id = candidate_memory_jobs.source_event_id
		  AND turn_events.user_id = $1
	)`
}

func scopedCandidateJobArgs(filter Filter) []any {
	if projectScoped(filter) {
		return []any{filter.OrgID, filter.ProjectID}
	}
	return []any{filter.UserID}
}

func scopedCandidateJobQuery(filter Filter, prefix, suffix string) string {
	return prefix + " WHERE " + candidateJobWhereClause(filter) + " " + suffix
}

func topicWhereClause(filter Filter) string {
	if projectScoped(filter) {
		return "org_id=$1 AND project_id=$2"
	}
	return `EXISTS (
		SELECT 1 FROM candidate_memories
		WHERE candidate_memories.org_id = topic_memory_states.org_id
		  AND candidate_memories.project_id = topic_memory_states.project_id
		  AND candidate_memories.source_key = topic_memory_states.source_key
		  AND candidate_memories.thread_id = topic_memory_states.thread_id
		  AND candidate_memories.user_id = $1
	)`
}

func topicArgs(filter Filter) []any {
	if projectScoped(filter) {
		return []any{filter.OrgID, filter.ProjectID}
	}
	return []any{filter.UserID}
}

func (r *PGRepository) memoryKernelStats(ctx context.Context, filter Filter) (MemoryKernelStats, error) {
	var stats MemoryKernelStats

	// units total
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM memory_units WHERE org_id=$1 AND project_id=$2`, filter.OrgID, filter.ProjectID).Scan(&stats.UnitsTotal)
	if err != nil {
		return stats, err
	}
	// units current
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM memory_units WHERE org_id=$1 AND project_id=$2 AND status='current'`, filter.OrgID, filter.ProjectID).Scan(&stats.UnitsCurrent)
	// units needs_review
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM memory_units WHERE org_id=$1 AND project_id=$2 AND status='needs_review'`, filter.OrgID, filter.ProjectID).Scan(&stats.UnitsNeedsReview)
	// governance runs
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM memory_governance_runs WHERE org_id=$1 AND project_id=$2`, filter.OrgID, filter.ProjectID).Scan(&stats.GovernanceRunsTotal)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM memory_governance_runs WHERE org_id=$1 AND project_id=$2 AND status='failed'`, filter.OrgID, filter.ProjectID).Scan(&stats.GovernanceRunsFailed)
	// ci cases
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM memory_ci_cases WHERE org_id=$1 AND project_id=$2 AND status='active'`, filter.OrgID, filter.ProjectID).Scan(&stats.CICasesActive)
	// last run
	_ = r.pool.QueryRow(ctx, `SELECT started_at, summary FROM memory_governance_runs WHERE org_id=$1 AND project_id=$2 ORDER BY started_at DESC LIMIT 1`, filter.OrgID, filter.ProjectID).Scan(&stats.LastRunAt, &stats.LastRunSummary)
	return stats, nil
}
