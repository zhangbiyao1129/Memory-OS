package memorystats

import (
	"context"
	"errors"

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
	topics, err := r.topicStats(ctx, filter)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Archives: archives, HotMemories: hot, Candidates: candidates, Topics: topics}, nil
}

func (r *PGRepository) archiveStats(ctx context.Context, filter Filter) (AssetStats, error) {
	stats := AssetStats{ByStatus: make(map[string]int64)}
	rows, err := r.pool.Query(ctx, `SELECT status, count(*) FROM archives WHERE user_id=$1 AND org_id=$2 AND project_id=$3 GROUP BY status`, filter.UserID, filter.OrgID, filter.ProjectID)
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
	rows, err := r.pool.Query(ctx, `SELECT status, count(*) FROM hot_memories WHERE user_id=$1 AND org_id=$2 AND project_id=$3 AND scope='project' AND visibility='project' AND deleted_at IS NULL AND status <> 'deleted' AND permission_labels && $4::text[] GROUP BY status`, filter.UserID, filter.OrgID, filter.ProjectID, filter.PermissionLabels)
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
	rows, err := r.pool.Query(ctx, `SELECT status, count(*) FROM candidate_memories WHERE org_id=$1 AND project_id=$2 GROUP BY status`, filter.OrgID, filter.ProjectID)
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
	rows, err = r.pool.Query(ctx, `SELECT risk_level, count(*) FROM candidate_memories WHERE org_id=$1 AND project_id=$2 GROUP BY risk_level`, filter.OrgID, filter.ProjectID)
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

	return stats, nil
}

func (r *PGRepository) scoreBuckets(ctx context.Context, filter Filter, scoreKey string, buckets *[]ScoreBucket) error {
	query := `SELECT
	  count(*) FILTER (WHERE COALESCE((scores->>'` + scoreKey + `')::double precision, 0) >= 0 AND COALESCE((scores->>'` + scoreKey + `')::double precision, 0) < 0.25),
	  count(*) FILTER (WHERE COALESCE((scores->>'` + scoreKey + `')::double precision, 0) >= 0.25 AND COALESCE((scores->>'` + scoreKey + `')::double precision, 0) < 0.5),
	  count(*) FILTER (WHERE COALESCE((scores->>'` + scoreKey + `')::double precision, 0) >= 0.5 AND COALESCE((scores->>'` + scoreKey + `')::double precision, 0) < 0.75),
	  count(*) FILTER (WHERE COALESCE((scores->>'` + scoreKey + `')::double precision, 0) >= 0.75)
	FROM candidate_memories
	WHERE org_id=$1 AND project_id=$2`

	var c1, c2, c3, c4 int64
	err := r.pool.QueryRow(ctx, query, filter.OrgID, filter.ProjectID).Scan(&c1, &c2, &c3, &c4)
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

func (r *PGRepository) topicStats(ctx context.Context, filter Filter) (TopicStats, error) {
	var stats TopicStats
	err := r.pool.QueryRow(ctx, `SELECT
	  count(*),
	  count(*) FILTER (WHERE composed_archive_id = '' AND ready_to_compose),
	  count(*) FILTER (WHERE composed_archive_id <> ''),
	  count(*) FILTER (WHERE composed_archive_id = '' AND NOT ready_to_compose)
	FROM topic_memory_states
	WHERE org_id=$1 AND project_id=$2`, filter.OrgID, filter.ProjectID).Scan(&stats.Total, &stats.ReadyToCompose, &stats.Composed, &stats.Open)
	if err != nil {
		return stats, err
	}
	return stats, nil
}
