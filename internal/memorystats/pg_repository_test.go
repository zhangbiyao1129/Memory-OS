package memorystats

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func statsPGTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func statsSuffix() string {
	replacer := strings.NewReplacer("-", "")
	return replacer.Replace(strconv.FormatInt(time.Now().UnixNano(), 10))
}

func TestPGRepositorySnapshotRejectsMissingPool(t *testing.T) {
	_, err := NewPGRepository(nil).Snapshot(context.Background(), Filter{UserID: "u", OrgID: "o", ProjectID: "p", PermissionLabels: []string{"project:p:read"}})
	if err == nil {
		t.Fatal("Snapshot() error = nil, want missing pool error")
	}
}

func TestPGRepositorySnapshotCountsLifecycleStatsWithPermissionFilter(t *testing.T) {
	pool := statsPGTestPool(t)
	ctx := context.Background()
	suffix := statsSuffix()
	orgID := "org_" + suffix
	projectID := "project_" + suffix
	userID := "user_" + suffix
	permissionLabel := "project:" + projectID + ":read"

	// 清理测试数据
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM archives WHERE org_id=$1 AND project_id=$2", orgID, projectID)
		pool.Exec(ctx, "DELETE FROM hot_memories WHERE org_id=$1 AND project_id=$2", orgID, projectID)
		pool.Exec(ctx, "DELETE FROM candidate_memories WHERE org_id=$1 AND project_id=$2", orgID, projectID)
		pool.Exec(ctx, "DELETE FROM topic_memory_states WHERE org_id=$1 AND project_id=$2", orgID, projectID)
	})

	// 插入 archives: 2 active, 1 deleted
	_, err := pool.Exec(ctx, `INSERT INTO archives (archive_id, user_id, org_id, project_id, title, status, content, index_generation, created_at, updated_at)
		VALUES ($1, $2, $3, $3, 'title1', 'active', 'content1', 1, NOW(), NOW()),
			   ($4, $2, $3, $3, 'title2', 'active', 'content2', 1, NOW(), NOW()),
			   ($5, $2, $3, $3, 'title3', 'deleted', 'content3', 1, NOW(), NOW())`,
		"arch_a_"+suffix, userID, orgID, "arch_b_"+suffix, "arch_c_"+suffix)
	if err != nil {
		t.Fatalf("insert archives: %v", err)
	}

	// 插入 hot_memories: 3 条匹配权限, 1 条不匹配
	_, err = pool.Exec(ctx, `INSERT INTO hot_memories (memory_id, user_id, org_id, project_id, agent_id, content, scope, visibility, status, permission_labels, created_at, updated_at)
		VALUES ($1, $2, $3, $3, 'agent1', 'hm1', 'project', 'project', 'active', $5, NOW(), NOW()),
			   ($4, $2, $3, $3, 'agent1', 'hm2', 'project', 'project', 'promoted', $5, NOW(), NOW()),
			   ($6, $2, $3, $3, 'agent1', 'hm3', 'project', 'project', 'demoted', $5, NOW(), NOW()),
			   ($7, $2, $3, $3, 'agent1', 'hm4', 'project', 'project', 'active', '{other_label}', NOW(), NOW())`,
		"hm_a_"+suffix, userID, orgID, "hm_b_"+suffix, "{"+permissionLabel+"}", "hm_c_"+suffix, "hm_d_"+suffix)
	if err != nil {
		t.Fatalf("insert hot_memories: %v", err)
	}

	// 插入 candidate_memories: 覆盖不同 status, risk_level, scores
	_, err = pool.Exec(ctx, `INSERT INTO candidate_memories (candidate_id, user_id, org_id, project_id, content, status, risk_level, scores, created_at, updated_at)
		VALUES ($1, $2, $3, $3, 'c1', 'pending', 'low', '{"hot_memory_score": 0.2, "compose_score": 0.3}', NOW(), NOW()),
			   ($4, $2, $3, $3, 'c2', 'composed', 'medium', '{"hot_memory_score": 0.6, "compose_score": 0.8}', NOW(), NOW()),
			   ($5, $2, $3, $3, 'c3', 'discarded', 'high', '{"hot_memory_score": 0.9, "compose_score": 0.1}', NOW(), NOW())`,
		"cand_a_"+suffix, userID, orgID, "cand_b_"+suffix, "cand_c_"+suffix)
	if err != nil {
		t.Fatalf("insert candidate_memories: %v", err)
	}

	// 插入 topic_memory_states: ready, composed, open
	_, err = pool.Exec(ctx, `INSERT INTO topic_memory_states (topic_id, user_id, org_id, project_id, source_key, status, composed_archive_id, ready_to_compose, created_at, updated_at)
		VALUES ($1, $2, $3, $3, 'topic1', 'active', '', true, NOW(), NOW()),
			   ($4, $2, $3, $3, 'topic2', 'active', 'archive_1', false, NOW(), NOW()),
			   ($5, $2, $3, $3, 'topic3', 'active', '', false, NOW(), NOW())`,
		"topic_a_"+suffix, userID, orgID, "topic_b_"+suffix, "topic_c_"+suffix)
	if err != nil {
		t.Fatalf("insert topic_memory_states: %v", err)
	}

	repo := NewPGRepository(pool)
	snapshot, err := repo.Snapshot(ctx, Filter{
		UserID:           userID,
		OrgID:            orgID,
		ProjectID:        projectID,
		PermissionLabels: []string{permissionLabel},
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	// 验证 archives
	if snapshot.Archives.Total != 2 {
		t.Fatalf("archives total = %d, want 2", snapshot.Archives.Total)
	}
	if snapshot.Archives.ByStatus["active"] != 2 {
		t.Fatalf("archives by_status[active] = %d, want 2", snapshot.Archives.ByStatus["active"])
	}
	if snapshot.Archives.ByStatus["deleted"] != 1 {
		t.Fatalf("archives by_status[deleted] = %d, want 1", snapshot.Archives.ByStatus["deleted"])
	}

	// 验证 hot_memories (只计入匹配权限的 3 条)
	if snapshot.HotMemories.Total != 3 {
		t.Fatalf("hot_memories total = %d, want 3", snapshot.HotMemories.Total)
	}
	if snapshot.HotMemories.ByStatus["active"] != 1 {
		t.Fatalf("hot_memories by_status[active] = %d, want 1", snapshot.HotMemories.ByStatus["active"])
	}
	if snapshot.HotMemories.ByStatus["promoted"] != 1 {
		t.Fatalf("hot_memories by_status[promoted] = %d, want 1", snapshot.HotMemories.ByStatus["promoted"])
	}
	if snapshot.HotMemories.ByStatus["demoted"] != 1 {
		t.Fatalf("hot_memories by_status[demoted] = %d, want 1", snapshot.HotMemories.ByStatus["demoted"])
	}

	// 验证 candidates
	if snapshot.Candidates.Total != 3 {
		t.Fatalf("candidates total = %d, want 3", snapshot.Candidates.Total)
	}
	if snapshot.Candidates.ByStatus["pending"] != 1 {
		t.Fatalf("candidates by_status[pending] = %d, want 1", snapshot.Candidates.ByStatus["pending"])
	}
	if snapshot.Candidates.ByStatus["composed"] != 1 {
		t.Fatalf("candidates by_status[composed] = %d, want 1", snapshot.Candidates.ByStatus["composed"])
	}
	if snapshot.Candidates.ByStatus["discarded"] != 1 {
		t.Fatalf("candidates by_status[discarded] = %d, want 1", snapshot.Candidates.ByStatus["discarded"])
	}
	if snapshot.Candidates.ByRisk["low"] != 1 {
		t.Fatalf("candidates by_risk[low] = %d, want 1", snapshot.Candidates.ByRisk["low"])
	}
	if snapshot.Candidates.ByRisk["medium"] != 1 {
		t.Fatalf("candidates by_risk[medium] = %d, want 1", snapshot.Candidates.ByRisk["medium"])
	}
	if snapshot.Candidates.ByRisk["high"] != 1 {
		t.Fatalf("candidates by_risk[high] = %d, want 1", snapshot.Candidates.ByRisk["high"])
	}

	// 验证 topics
	if snapshot.Topics.Total != 3 {
		t.Fatalf("topics total = %d, want 3", snapshot.Topics.Total)
	}
	if snapshot.Topics.ReadyToCompose != 1 {
		t.Fatalf("topics ready_to_compose = %d, want 1", snapshot.Topics.ReadyToCompose)
	}
	if snapshot.Topics.Composed != 1 {
		t.Fatalf("topics composed = %d, want 1", snapshot.Topics.Composed)
	}
	if snapshot.Topics.Open != 1 {
		t.Fatalf("topics open = %d, want 1", snapshot.Topics.Open)
	}
}
