package candidatememory

import (
	"context"
	"testing"
)

func TestServiceCreateAndScoreAssignsScoresWhenMissing(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewInMemoryRepository(), RuleScorer{})

	c, err := svc.CreateAndScore(ctx, testCandidate())
	if err != nil {
		t.Fatalf("create and score: %v", err)
	}
	if c.Scores.HotMemoryScore == 0 && c.Scores.ComposeScore == 0 {
		t.Fatal("scorer 应在创建时填充 scores")
	}
}

func TestServiceCreateAndScoreKeepsExplicitScores(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewInMemoryRepository(), RuleScorer{})

	in := testCandidate()
	in.Scores = Scores{HotMemoryScore: 0.42, ComposeScore: 0.17}
	c, err := svc.CreateAndScore(ctx, in)
	if err != nil {
		t.Fatalf("create and score: %v", err)
	}
	if c.Scores.HotMemoryScore != 0.42 {
		t.Fatalf("显式 scores 不应被覆盖: %+v", c.Scores)
	}
}

func TestServiceAcceptUpdatesStatus(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewInMemoryRepository(), RuleScorer{})
	svc.CreateAndScore(ctx, testCandidate())

	updated, err := svc.Accept(ctx, "org-1", "cand-1")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if updated.Status != StatusAccepted {
		t.Fatalf("状态应为 accepted: %s", updated.Status)
	}
}

func TestServiceDiscardUpdatesStatus(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewInMemoryRepository(), RuleScorer{})
	svc.CreateAndScore(ctx, testCandidate())

	updated, err := svc.Discard(ctx, "org-1", "cand-1")
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	if updated.Status != StatusDiscarded {
		t.Fatalf("状态应为 discarded: %s", updated.Status)
	}
}

func TestServiceAcceptMissingReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewInMemoryRepository(), RuleScorer{})
	if _, err := svc.Accept(ctx, "org-1", "missing"); err != ErrNotFound {
		t.Fatalf("期望 ErrNotFound,得到 %v", err)
	}
}

func TestServiceListByScope(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewInMemoryRepository(), RuleScorer{})
	svc.CreateAndScore(ctx, testCandidate())

	got, err := svc.List(ctx, ListFilter{OrgID: "org-1", ProjectID: "proj-1", SourceKey: "github.com/acme/web"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("期望 1 条候选,得到 %d", len(got))
	}
}
