package candidatememory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeArchiveCreator struct {
	last ArchiveCreateRequest
	err  error
}

func (f *fakeArchiveCreator) Create(ctx context.Context, req ArchiveCreateRequest) (ArchiveCreateResult, error) {
	if f.err != nil {
		return ArchiveCreateResult{}, f.err
	}
	f.last = req
	return ArchiveCreateResult{ArchiveID: req.ArchiveID}, nil
}

func newTestComposer() (TopicComposer, *InMemoryRepository, *fakeArchiveCreator) {
	repo := NewInMemoryRepository()
	creator := &fakeArchiveCreator{}
	return NewTopicComposer(repo, creator), repo, creator
}

type fakeArchiveSummarizer struct {
	markdown string
	err      error
}

func (f fakeArchiveSummarizer) SummarizeArchive(ctx context.Context, req ComposeRequest, candidates []Candidate) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.markdown, nil
}

func seedCandidate(t *testing.T, repo *InMemoryRepository, c Candidate) {
	t.Helper()
	c.OrgID = "o"
	c.ProjectID = "p"
	c.SourceKey = "sk"
	c.ThreadID = "th"
	if c.CandidateID == "" {
		c.CandidateID = "c-" + sanitizeForID(c.Content)
	}
	if c.Status == "" {
		c.Status = StatusInComposePool
	}
	if _, err := repo.CreateCandidate(context.Background(), c); err != nil {
		t.Fatalf("seed candidate: %v", err)
	}
}

func composeReq(force bool) ComposeRequest {
	return ComposeRequest{OrgID: "o", ProjectID: "p", SourceKey: "sk", ThreadID: "th", Force: force}
}

func TestComposerNotReadyWhenFewCandidatesAndRecentEvent(t *testing.T) {
	composer, repo, _ := newTestComposer()
	seedCandidate(t, repo, Candidate{Content: "少量事实", MemoryType: MemoryTypeFact, RiskLevel: RiskLow})
	now := time.Now()
	repo.UpsertTopicState(context.Background(), TopicState{OrgID: "o", ProjectID: "p", SourceKey: "sk", ThreadID: "th", LastEventAt: &now})

	res, err := composer.Compose(context.Background(), composeReq(false))
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if res.Ready {
		t.Fatal("少量候选且最近有事件,不应 ready")
	}
}

func TestComposerReadyWhenForce(t *testing.T) {
	composer, repo, creator := newTestComposer()
	seedCandidate(t, repo, Candidate{Content: "候选 A", MemoryType: MemoryTypeFact, RiskLevel: RiskLow})

	res, err := composer.Compose(context.Background(), composeReq(true))
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if !res.Ready || res.ArchiveID == "" {
		t.Fatalf("Force 应 ready 并生成 archive: %+v", res)
	}
	if res.Composed != 1 {
		t.Fatalf("应沉淀 1 条,得到 %d", res.Composed)
	}
	if !strings.Contains(creator.last.Markdown, "## 结论") {
		t.Fatalf("Markdown 应含固定结构: %s", creator.last.Markdown)
	}
}

func TestComposerReadyWhenCandidatesAbove10(t *testing.T) {
	composer, repo, _ := newTestComposer()
	for i := 0; i < composeMinCandidates; i++ {
		seedCandidate(t, repo, Candidate{Content: "事实" + strings.Repeat("x", i+1), MemoryType: MemoryTypeFact, RiskLevel: RiskLow})
	}
	res, _ := composer.Compose(context.Background(), composeReq(false))
	if !res.Ready {
		t.Fatal("候选 >= 10 应 ready")
	}
}

func TestComposerReadyWhenIdleOver24h(t *testing.T) {
	composer, repo, _ := newTestComposer()
	seedCandidate(t, repo, Candidate{Content: "空闲事实", MemoryType: MemoryTypeFact, RiskLevel: RiskLow})
	old := time.Now().Add(-25 * time.Hour)
	repo.UpsertTopicState(context.Background(), TopicState{OrgID: "o", ProjectID: "p", SourceKey: "sk", ThreadID: "th", LastEventAt: &old})

	res, _ := composer.Compose(context.Background(), composeReq(false))
	if !res.Ready {
		t.Fatal("24h 无新事件应 ready")
	}
}

func TestComposerReadyWhenCompletionSignal(t *testing.T) {
	composer, repo, _ := newTestComposer()
	seedCandidate(t, repo, Candidate{Content: "schema 迁移 bug 已修复", MemoryType: MemoryTypeBugfix, RiskLevel: RiskLow})

	res, _ := composer.Compose(context.Background(), composeReq(false))
	if !res.Ready {
		t.Fatal("完成信号(已修复)应 ready")
	}
}

// pending_review 高风险候选(Status=pending + RiskHigh)不得自动进入 Markdown。
func TestComposerExcludesPendingReviewHighRisk(t *testing.T) {
	composer, repo, creator := newTestComposer()
	seedCandidate(t, repo, Candidate{Content: "低风险事实", MemoryType: MemoryTypeFact, RiskLevel: RiskLow})
	seedCandidate(t, repo, Candidate{Content: "生产 schema 迁移风险", MemoryType: MemoryTypeRisk, RiskLevel: RiskHigh, Status: StatusPending})

	res, _ := composer.Compose(context.Background(), composeReq(true))
	if !res.Ready {
		t.Fatal("Force 应 ready")
	}
	if res.Composed != 1 {
		t.Fatalf("应只沉淀 1 条低风险,得到 %d(高风险 pending 应排除)", res.Composed)
	}
	if strings.Contains(creator.last.Markdown, "生产 schema 迁移风险") {
		t.Fatalf("高风险 pending 候选不应进入 Markdown: %s", creator.last.Markdown)
	}
}

func TestComposerMarksCandidatesComposedAndTopicArchiveID(t *testing.T) {
	composer, repo, _ := newTestComposer()
	seedCandidate(t, repo, Candidate{Content: "待沉淀事实", MemoryType: MemoryTypeFact, RiskLevel: RiskLow})

	res, _ := composer.Compose(context.Background(), composeReq(true))
	cands, _ := repo.ListCandidates(context.Background(), ListFilter{OrgID: "o", ProjectID: "p", SourceKey: "sk", ThreadID: "th"})
	if len(cands) != 1 || cands[0].Status != StatusComposed {
		t.Fatalf("候选应标记为 composed: %+v", cands)
	}
	ts, _ := repo.GetTopicState(context.Background(), "o", "p", "sk", "th")
	if ts.ComposedArchiveID != res.ArchiveID {
		t.Fatalf("topic state composed_archive_id 应更新: %s", ts.ComposedArchiveID)
	}
}

func TestComposerMarkdownHasFixedStructure(t *testing.T) {
	composer, repo, creator := newTestComposer()
	seedCandidate(t, repo, Candidate{Content: "决策 X", MemoryType: MemoryTypeDecision, RiskLevel: RiskLow})
	seedCandidate(t, repo, Candidate{Content: "修复 Y", MemoryType: MemoryTypeBugfix, RiskLevel: RiskLow})

	composer.Compose(context.Background(), composeReq(true))
	md := creator.last.Markdown
	required := []string{"# ", "## 结论", "## 背景", "## 现象", "## 根因", "## 修复", "## 验证", "## 遗留风险", "## 可复用经验", "## 后续事项", "## 来源"}
	for _, h := range required {
		if !strings.Contains(md, h) {
			t.Fatalf("Markdown 缺少固定 section %q:\n%s", h, md)
		}
	}
	if !strings.Contains(md, "修复 Y") || !strings.Contains(md, "决策 X") {
		t.Fatalf("Markdown 应含候选内容:\n%s", md)
	}
}

func TestComposerUsesAISummarizerWhenConfigured(t *testing.T) {
	composer, repo, creator := newTestComposer()
	composer = composer.WithSummarizer(fakeArchiveSummarizer{markdown: "# AI 归档\n\n## 结论\n- 已整理"})
	seedCandidate(t, repo, Candidate{Content: "候选 A", MemoryType: MemoryTypeFact, RiskLevel: RiskLow})

	res, err := composer.Compose(context.Background(), composeReq(true))
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if !res.Ready {
		t.Fatal("Force 应 ready")
	}
	if creator.last.Markdown != "# AI 归档\n\n## 结论\n- 已整理" {
		t.Fatalf("Markdown = %q, want AI summary", creator.last.Markdown)
	}
}

func TestComposerSummarizerFailureDoesNotCreateArchiveOrMarkComposed(t *testing.T) {
	composer, repo, creator := newTestComposer()
	composer = composer.WithSummarizer(fakeArchiveSummarizer{err: errors.New("llm down")})
	seedCandidate(t, repo, Candidate{Content: "候选 A", MemoryType: MemoryTypeFact, RiskLevel: RiskLow})

	_, err := composer.Compose(context.Background(), composeReq(true))
	if err == nil {
		t.Fatal("compose error = nil, want summarizer failure")
	}
	if creator.last.ArchiveID != "" {
		t.Fatalf("archive should not be created on summarizer failure: %+v", creator.last)
	}
	candidates, _ := repo.ListCandidates(context.Background(), ListFilter{OrgID: "o", ProjectID: "p", SourceKey: "sk", ThreadID: "th"})
	if candidates[0].Status == StatusComposed {
		t.Fatal("candidate should not be marked composed on summarizer failure")
	}
}
