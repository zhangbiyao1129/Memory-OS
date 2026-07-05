package candidatememory

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 编译时接口实现检查:任一 Repository 实现缺方法或签名不匹配,本地 go test 即编译失败。
var (
	_ Repository = (*InMemoryRepository)(nil)
	_ Repository = (*PGRepository)(nil)
)

func TestPGRepositoryRequiresPool(t *testing.T) {
	repo := NewPGRepository(nil)
	ctx := context.Background()

	if _, err := repo.CreateCandidate(ctx, Candidate{CandidateID: "x", OrgID: "o"}); err == nil {
		t.Fatal("CreateCandidate 应在 pool 缺失时报错")
	}
	if _, err := repo.GetCandidate(ctx, "o", "x"); err == nil {
		t.Fatal("GetCandidate 应在 pool 缺失时报错")
	}
	if _, err := repo.ListCandidates(ctx, ListFilter{OrgID: "o"}); err == nil {
		t.Fatal("ListCandidates 应在 pool 缺失时报错")
	}
	if _, err := repo.UpdateCandidateStatus(ctx, "o", "x", StatusAccepted, Scores{}); err == nil {
		t.Fatal("UpdateCandidateStatus 应在 pool 缺失时报错")
	}
	if _, err := repo.UpsertJob(ctx, Job{IdempotencyKey: "k"}); err == nil {
		t.Fatal("UpsertJob 应在 pool 缺失时报错")
	}
	if _, err := repo.LeaseJob(ctx, time.Now(), "w", time.Minute); err == nil {
		t.Fatal("LeaseJob 应在 pool 缺失时报错")
	}
	if err := repo.CompleteJob(ctx, 1, nil); err == nil {
		t.Fatal("CompleteJob 应在 pool 缺失时报错")
	}
	if err := repo.FailJob(ctx, 1, "err"); err == nil {
		t.Fatal("FailJob 应在 pool 缺失时报错")
	}
	if _, err := repo.UpsertTopicState(ctx, TopicState{OrgID: "o", ProjectID: "p", SourceKey: "s", ThreadID: "t"}); err == nil {
		t.Fatal("UpsertTopicState 应在 pool 缺失时报错")
	}
	if _, err := repo.GetTopicState(ctx, "o", "p", "s", "t"); err == nil {
		t.Fatal("GetTopicState 应在 pool 缺失时报错")
	}
}

// candidate_memories 的 org_id/project_id 是 TEXT 无外键,可直接用任意值,无需 tenant fixture。
func TestPGRepositoryCandidateLifecycle(t *testing.T) {
	pool := candidatePGTestPool(t)
	repo := NewPGRepository(pool)
	ctx := context.Background()
	suffix := candidateSuffix()
	orgID := "cand-org-" + suffix
	projectID := "cand-proj-" + suffix
	sourceKey := "github.com/acme/web-" + suffix

	created, err := repo.CreateCandidate(ctx, Candidate{
		CandidateID:    "cand-" + suffix,
		OrgID:          orgID,
		ProjectID:      projectID,
		SourceKey:      sourceKey,
		UserID:         "u",
		AgentID:        "claude",
		ThreadID:       "thread-1",
		SourceEventIDs: []string{"evt-1"},
		MemoryType:     MemoryTypeFact,
		Content:        "事实内容",
		Summary:        "短摘要",
		RiskLevel:      RiskLow,
		Confidence:     0.8,
		Status:         StatusPending,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("created_at 应持久化")
	}

	got, err := repo.GetCandidate(ctx, orgID, "cand-"+suffix)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Content != "事实内容" || len(got.SourceEventIDs) != 1 {
		t.Fatalf("字段未正确持久化: %+v", got)
	}

	// 同 candidate_id 重复创建 → ErrConflict(对应 ON CONFLICT DO NOTHING)
	if _, err := repo.CreateCandidate(ctx, Candidate{
		CandidateID: "cand-" + suffix, OrgID: orgID, ProjectID: projectID, SourceKey: sourceKey,
		UserID: "u", AgentID: "a", ThreadID: "t",
		MemoryType: MemoryTypeFact, Content: "x", Status: StatusPending,
	}); err != ErrConflict {
		t.Fatalf("重复创建应返回 ErrConflict,得到 %v", err)
	}

	// List 按 source_key 隔离同名 project(硬规则 4)
	listed, err := repo.ListCandidates(ctx, ListFilter{OrgID: orgID, ProjectID: projectID, SourceKey: sourceKey})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("期望 1 条,得到 %d", len(listed))
	}

	updated, err := repo.UpdateCandidateStatus(ctx, orgID, "cand-"+suffix, StatusAccepted, Scores{HotMemoryScore: 0.5})
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if updated.Status != StatusAccepted || updated.Scores.HotMemoryScore != 0.5 {
		t.Fatalf("status/scores 未持久化: %+v", updated)
	}
}

func TestPGRepositoryJobLeaseComplete(t *testing.T) {
	pool := candidatePGTestPool(t)
	repo := NewPGRepository(pool)
	ctx := context.Background()
	suffix := candidateSuffix()

	job := Job{
		IdempotencyKey: "cand:job:" + suffix,
		OrgID:          "o-" + suffix,
		ProjectID:      "p-" + suffix,
		SourceKey:      "sk-" + suffix,
		SourceEventID:  "evt-1",
	}
	if _, err := repo.UpsertJob(ctx, job); err != nil {
		t.Fatalf("upsert job: %v", err)
	}
	again, err := repo.UpsertJob(ctx, job)
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if again.Attempts != 0 {
		t.Fatalf("幂等 upsert 不应改变 attempts,得到 %d", again.Attempts)
	}

	leased, err := repo.LeaseJob(ctx, time.Now().UTC(), "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if leased == nil || leased.IdempotencyKey != "cand:job:"+suffix {
		t.Fatalf("lease 未拿到正确 job: %+v", leased)
	}
	if leased.Attempts != 1 {
		t.Fatalf("lease 后 attempts 应为 1,得到 %d", leased.Attempts)
	}

	if err := repo.CompleteJob(ctx, leased.ID, []string{"cand-a"}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if none, _ := repo.LeaseJob(ctx, time.Now().UTC(), "worker-2", time.Minute); none != nil {
		t.Fatalf("completed job 不应再被 lease,得到 %+v", none)
	}
}

func TestPGRepositoryTopicStateUpsert(t *testing.T) {
	pool := candidatePGTestPool(t)
	repo := NewPGRepository(pool)
	ctx := context.Background()
	suffix := candidateSuffix()

	first, err := repo.UpsertTopicState(ctx, TopicState{
		OrgID: "to-" + suffix, ProjectID: "tp-" + suffix, SourceKey: "tsk-" + suffix, ThreadID: "tt-" + suffix,
		CandidateCount: 1,
	})
	if err != nil {
		t.Fatalf("upsert topic: %v", err)
	}
	second, err := repo.UpsertTopicState(ctx, TopicState{
		OrgID: "to-" + suffix, ProjectID: "tp-" + suffix, SourceKey: "tsk-" + suffix, ThreadID: "tt-" + suffix,
		CandidateCount: 5, ReadyToCompose: true,
	})
	if err != nil {
		t.Fatalf("re-upsert topic: %v", err)
	}
	if second.ID != first.ID {
		t.Fatal("topic re-upsert 应更新同一行")
	}
	if second.CandidateCount != 5 || !second.ReadyToCompose {
		t.Fatalf("topic 字段未更新: %+v", second)
	}
}

func candidatePGTestPool(t *testing.T) *pgxpool.Pool {
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

func candidateSuffix() string {
	repl := strings.NewReplacer("-", "")
	return repl.Replace(strconv.FormatInt(time.Now().UnixNano(), 10))
}
