package candidatememory

import (
	"context"
	"testing"
	"time"
)

func testCandidate() Candidate {
	return Candidate{
		CandidateID:    "cand-1",
		OrgID:          "org-1",
		ProjectID:      "proj-1",
		SourceKey:      "github.com/acme/web",
		UserID:         "user-1",
		AgentID:        "claude",
		ThreadID:       "thread-1",
		SessionID:      "session-1",
		SourceEventIDs: []string{"evt-1", "evt-2"},
		MemoryType:     MemoryTypeFact,
		Content:        "项目使用 Go/Hertz 栈",
		Summary:        "技术栈为 Go/Hertz",
		RiskLevel:      RiskLow,
		Confidence:     0.85,
		Status:         StatusPending,
	}
}

func TestInMemoryRepositoryCreateAndGetCandidate(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryRepository()

	created, err := repo.CreateCandidate(ctx, testCandidate())
	if err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("created_at should be set")
	}

	got, err := repo.GetCandidate(ctx, "org-1", "cand-1")
	if err != nil {
		t.Fatalf("get candidate: %v", err)
	}
	if got.Content != "项目使用 Go/Hertz 栈" {
		t.Fatalf("unexpected content: %s", got.Content)
	}
	if len(got.SourceEventIDs) != 2 {
		t.Fatalf("source event ids not preserved: %v", got.SourceEventIDs)
	}
}

func TestInMemoryRepositoryGetCandidateMissing(t *testing.T) {
	repo := NewInMemoryRepository()
	if _, err := repo.GetCandidate(context.Background(), "org-1", "missing"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestInMemoryRepositoryCreateDuplicateCandidateConflicts(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryRepository()
	if _, err := repo.CreateCandidate(ctx, testCandidate()); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := repo.CreateCandidate(ctx, testCandidate()); err != ErrConflict {
		t.Fatalf("duplicate create should return ErrConflict, got %v", err)
	}
}

// 同名 project(proj-1)但不同 source_key 必须可区分,这是硬规则 4 的核心。
func TestInMemoryRepositoryListIsolatesSameNameProjectBySourceKey(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryRepository()

	c := testCandidate()
	repo.CreateCandidate(ctx, c)

	shell := c
	shell.CandidateID = "cand-2"
	shell.SourceKey = "gitlab.com/acme/shell"
	repo.CreateCandidate(ctx, shell)

	got, err := repo.ListCandidates(ctx, ListFilter{OrgID: "org-1", ProjectID: "proj-1", SourceKey: "github.com/acme/web"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].CandidateID != "cand-1" {
		t.Fatalf("expected only cand-1 for github source_key, got %v", got)
	}

	promoted, _ := repo.ListCandidates(ctx, ListFilter{OrgID: "org-1", Status: StatusPromotedToHot})
	if len(promoted) != 0 {
		t.Fatalf("expected no promoted, got %v", promoted)
	}
}

func TestInMemoryRepositoryUpdateCandidateStatus(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryRepository()
	repo.CreateCandidate(ctx, testCandidate())

	scores := Scores{HotMemoryScore: 0.9, ComposeScore: 0.1}
	updated, err := repo.UpdateCandidateStatus(ctx, "org-1", "cand-1", StatusAccepted, scores)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if updated.Status != StatusAccepted {
		t.Fatalf("status not updated: %s", updated.Status)
	}
	if updated.Scores.HotMemoryScore != 0.9 {
		t.Fatalf("scores not updated: %+v", updated.Scores)
	}

	got, _ := repo.GetCandidate(ctx, "org-1", "cand-1")
	if got.Status != StatusAccepted {
		t.Fatalf("status not persisted: %s", got.Status)
	}
}

func TestInMemoryRepositoryJobIdempotency(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryRepository()
	job := Job{
		IdempotencyKey: "candidate:proj-1:github.com/acme/web:evt-1:extract",
		OrgID:          "org-1",
		ProjectID:      "proj-1",
		SourceKey:      "github.com/acme/web",
		SourceEventID:  "evt-1",
	}
	first, err := repo.UpsertJob(ctx, job)
	if err != nil {
		t.Fatalf("upsert job: %v", err)
	}
	if first.ID == 0 {
		t.Fatal("job should get an ID")
	}
	again, err := repo.UpsertJob(ctx, job)
	if err != nil {
		t.Fatalf("re-upsert job: %v", err)
	}
	if again.ID != first.ID {
		t.Fatal("idempotent re-upsert should return same job, not create a duplicate")
	}
}

func TestInMemoryRepositoryLeaseJobExclusive(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryRepository()
	repo.UpsertJob(ctx, Job{
		IdempotencyKey: "candidate:proj-1:github.com/acme/web:evt-1:extract",
		OrgID:          "org-1",
		ProjectID:      "proj-1",
		SourceKey:      "github.com/acme/web",
		SourceEventID:  "evt-1",
	})

	now := time.Now().UTC()
	leased, err := repo.LeaseJob(ctx, now, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if leased == nil {
		t.Fatal("expected a leased job")
	}
	if leased.LockedBy != "worker-1" {
		t.Fatalf("locked_by not set: %s", leased.LockedBy)
	}

	if again, _ := repo.LeaseJob(ctx, now, "worker-2", time.Minute); again != nil {
		t.Fatal("should not lease the same job again before expiry")
	}
}

func TestInMemoryRepositoryCompleteAndFailJob(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryRepository()
	repo.UpsertJob(ctx, Job{
		IdempotencyKey: "candidate:proj-1:github.com/acme/web:evt-1:extract",
		OrgID:          "org-1",
		ProjectID:      "proj-1",
		SourceKey:      "github.com/acme/web",
		SourceEventID:  "evt-1",
	})
	now := time.Now().UTC()
	leased, _ := repo.LeaseJob(ctx, now, "worker-1", time.Minute)

	if err := repo.CompleteJob(ctx, leased.ID, []string{"cand-a"}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	repo.UpsertJob(ctx, Job{
		IdempotencyKey: "candidate:proj-1:github.com/acme/web:evt-2:extract",
		OrgID:          "org-1",
		ProjectID:      "proj-1",
		SourceKey:      "github.com/acme/web",
		SourceEventID:  "evt-2",
	})
	second, _ := repo.LeaseJob(ctx, now, "worker-1", time.Minute)
	if err := repo.FailJob(ctx, second.ID, "llm timeout"); err != nil {
		t.Fatalf("fail: %v", err)
	}
}

func TestInMemoryRepositoryTopicStateUpsertSameRow(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryRepository()
	ts := TopicState{
		OrgID:          "org-1",
		ProjectID:      "proj-1",
		SourceKey:      "github.com/acme/web",
		ThreadID:       "thread-1",
		CandidateCount: 1,
	}
	first, err := repo.UpsertTopicState(ctx, ts)
	if err != nil {
		t.Fatalf("upsert topic state: %v", err)
	}

	ts.CandidateCount = 2
	ts.ReadyToCompose = true
	again, err := repo.UpsertTopicState(ctx, ts)
	if err != nil {
		t.Fatalf("re-upsert topic state: %v", err)
	}
	if again.ID != first.ID {
		t.Fatal("topic state re-upsert should update the same row, not insert")
	}
	if again.CandidateCount != 2 || !again.ReadyToCompose {
		t.Fatalf("topic state not updated: %+v", again)
	}
}

func TestInMemoryRepository_UpdateCandidateStatus_PreservesNeedsReview(t *testing.T) {
	repo := NewInMemoryRepository()
	cand := Candidate{CandidateID: "c1", OrgID: "o1", ProjectID: "p1", Status: StatusPending, NeedsReview: true, Scores: Scores{}}
	_, _ = repo.CreateCandidate(context.Background(), cand)

	// 状态变更不应丢失 NeedsReview 标记。
	updated, err := repo.UpdateCandidateStatus(context.Background(), "o1", "c1", StatusAccepted, cand.Scores)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if !updated.NeedsReview {
		t.Fatal("NeedsReview should be preserved across UpdateCandidateStatus")
	}
}
