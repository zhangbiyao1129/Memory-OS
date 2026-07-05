package jobs

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/candidatememory"
)

// 编译时接口实现检查。
var _ CandidateMemoryQueue = (*PGCandidateMemoryQueue)(nil)

func TestPGCandidateMemoryQueueRequiresRepo(t *testing.T) {
	q := NewPGCandidateMemoryQueue(nil, PGCandidateMemoryQueueOptions{WorkerID: "w"})
	ctx := context.Background()

	if err := q.Enqueue(ctx, candidatememory.Job{IdempotencyKey: "k"}); err == nil {
		t.Fatal("Enqueue 应在 repo 缺失时报错")
	}
	if _, _, err := q.Lease(ctx); err == nil {
		t.Fatal("Lease 应在 repo 缺失时报错")
	}
	if err := q.Complete(ctx, candidatememory.Job{ID: 1}, CandidateMemoryJobResult{}); err == nil {
		t.Fatal("Complete 应在 repo 缺失时报错")
	}
	if err := q.Fail(ctx, candidatememory.Job{ID: 1}, errors.New("x")); err == nil {
		t.Fatal("Fail 应在 repo 缺失时报错")
	}
}

// 幂等:同 idempotency_key 重复 Enqueue 不报错且不创建重复任务。
func TestPGCandidateMemoryQueueIdempotent(t *testing.T) {
	pool := candidateQueueTestPool(t)
	repo := candidatememory.NewPGRepository(pool)
	q := NewPGCandidateMemoryQueue(repo, PGCandidateMemoryQueueOptions{WorkerID: "test-worker", LockTTL: time.Minute})
	ctx := context.Background()

	job := candidatememory.Job{
		IdempotencyKey: "cand:queue:idem:" + candidateQueueSuffix(),
		OrgID:          "o", ProjectID: "p", SourceKey: "sk", SourceEventID: "e1",
	}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("re-enqueue should be idempotent: %v", err)
	}

	leased, ok, err := q.Lease(ctx)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if !ok {
		t.Fatal("应 lease 到一个任务")
	}
	if leased.IdempotencyKey != job.IdempotencyKey {
		t.Fatalf("lease 到错误任务: %s", leased.IdempotencyKey)
	}
	// 第二次 lease 应无(已被第一次 lease 锁定)
	if _, ok, _ := q.Lease(ctx); ok {
		t.Fatal("同一任务不应被重复 lease")
	}
	if err := q.Complete(ctx, leased, CandidateMemoryJobResult{CandidateIDs: []string{"c1"}}); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

func candidateQueueTestPool(t *testing.T) *pgxpool.Pool {
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

func candidateQueueSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
