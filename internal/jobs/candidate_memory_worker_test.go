package jobs

import (
	"context"
	"errors"
	"testing"

	"memory-os/internal/candidatememory"
)

type fakeExtractor struct {
	candidates []candidatememory.Candidate
	err        error
}

func (f fakeExtractor) Extract(ctx context.Context, req candidatememory.ExtractionRequest) (candidatememory.ExtractionResult, error) {
	if f.err != nil {
		return candidatememory.ExtractionResult{}, f.err
	}
	return candidatememory.ExtractionResult{Candidates: f.candidates}, nil
}

type fakeEventLoader struct {
	req candidatememory.ExtractionRequest
	err error
}

func (f fakeEventLoader) LoadExtractionRequest(ctx context.Context, job candidatememory.Job) (candidatememory.ExtractionRequest, error) {
	if f.err != nil {
		return candidatememory.ExtractionRequest{}, f.err
	}
	return f.req, nil
}

func newTestCandidateWorker(t *testing.T, extractor candidatememory.Extractor) (CandidateMemoryWorker, *candidatememory.InMemoryRepository, candidatememory.ExtractionRequest) {
	t.Helper()
	repo := candidatememory.NewInMemoryRepository()
	service := candidatememory.NewService(repo, candidatememory.RuleScorer{})
	router := candidatememory.NewRouter(nil) // 不提升热记忆,候选降级 pending 仍持久化
	req := candidatememory.ExtractionRequest{
		OrgID: "org-1", ProjectID: "proj-1", SourceKey: "github.com/acme/web",
		UserID: "u", AgentID: "a", ThreadID: "thread-1", SessionID: "s",
		Events: []candidatememory.ExtractionEvent{
			{EventID: "e1", Type: "manual_archive_request", Payload: []byte(`{"topic":"test"}`)},
		},
	}
	loader := fakeEventLoader{req: req}
	return NewCandidateMemoryWorker(extractor, router, service, repo, loader), repo, req
}

// 默认 Handle 全链路:提炼 → 分流 → 持久化候选 → 更新 topic state。
func TestCandidateMemoryWorkerPersistsCandidatesAndTopicState(t *testing.T) {
	worker, repo, _ := newTestCandidateWorker(t, fakeExtractor{candidates: []candidatememory.Candidate{
		{CandidateID: "cand-a", MemoryType: candidatememory.MemoryTypeFact, Content: "短事实", Confidence: 0.9, RiskLevel: candidatememory.RiskLow},
		{CandidateID: "cand-b", MemoryType: candidatememory.MemoryTypeBugfix, Content: "修复 bug", Confidence: 0.8, RiskLevel: candidatememory.RiskLow},
	}})

	result, err := worker.Handle(candidatememory.Job{OrgID: "org-1", ProjectID: "proj-1", SourceKey: "github.com/acme/web", SourceEventID: "e1"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(result.CandidateIDs) != 2 {
		t.Fatalf("期望 2 个候选 ID,得到 %d", len(result.CandidateIDs))
	}

	cands, _ := repo.ListCandidates(context.Background(), candidatememory.ListFilter{OrgID: "org-1"})
	if len(cands) != 2 {
		t.Fatalf("候选未持久化: %d", len(cands))
	}
	ts, err := repo.GetTopicState(context.Background(), "org-1", "proj-1", "github.com/acme/web", "thread-1")
	if err != nil {
		t.Fatalf("topic state: %v", err)
	}
	if ts.CandidateCount != 2 {
		t.Fatalf("topic candidate_count 应为 2: %d", ts.CandidateCount)
	}
	if ts.ReadyToCompose {
		t.Fatal("候选数未达到阈值时 ready_to_compose 应为 false")
	}
}

func TestCandidateMemoryWorkerMarksTopicReadyAtComposeThreshold(t *testing.T) {
	extracted := make([]candidatememory.Candidate, 0, candidatememory.ComposeMinCandidates)
	for i := 0; i < candidatememory.ComposeMinCandidates; i++ {
		extracted = append(extracted, candidatememory.Candidate{
			CandidateID: "cand-ready-" + string(rune('a'+i)),
			MemoryType:  candidatememory.MemoryTypeFact,
			Content:     "可沉淀事实",
			Confidence:  0.9,
			RiskLevel:   candidatememory.RiskLow,
		})
	}
	worker, repo, _ := newTestCandidateWorker(t, fakeExtractor{candidates: extracted})

	if _, err := worker.Handle(candidatememory.Job{OrgID: "org-1", ProjectID: "proj-1", SourceKey: "github.com/acme/web", SourceEventID: "e1"}); err != nil {
		t.Fatalf("handle: %v", err)
	}

	ts, err := repo.GetTopicState(context.Background(), "org-1", "proj-1", "github.com/acme/web", "thread-1")
	if err != nil {
		t.Fatalf("topic state: %v", err)
	}
	if !ts.ReadyToCompose {
		t.Fatalf("ready_to_compose = false, want true when candidate_count reaches %d", candidatememory.ComposeMinCandidates)
	}
}

// 重复候选(ErrConflict)跳过,不阻塞其余候选。
func TestCandidateMemoryWorkerSkipsDuplicateCandidates(t *testing.T) {
	worker, repo, _ := newTestCandidateWorker(t, fakeExtractor{candidates: []candidatememory.Candidate{
		{CandidateID: "cand-dup", MemoryType: candidatememory.MemoryTypeFact, Content: "dup", Confidence: 0.9, RiskLevel: candidatememory.RiskLow},
		{CandidateID: "cand-new", MemoryType: candidatememory.MemoryTypeFact, Content: "new", Confidence: 0.9, RiskLevel: candidatememory.RiskLow},
	}})
	// 预先占用 cand-dup
	repo.CreateCandidate(context.Background(), candidatememory.Candidate{CandidateID: "cand-dup", OrgID: "org-1", MemoryType: candidatememory.MemoryTypeFact, Content: "dup", Status: candidatememory.StatusPending})

	result, err := worker.Handle(candidatememory.Job{OrgID: "org-1", ProjectID: "proj-1", SourceKey: "github.com/acme/web", SourceEventID: "e1"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(result.CandidateIDs) != 1 || result.CandidateIDs[0] != "cand-new" {
		t.Fatalf("应只产出 cand-new(重复跳过): %v", result.CandidateIDs)
	}
}

// 提炼失败(Handle 返回 error)→ worker 返回 error,供 runner 触发 Fail/重试。
func TestCandidateMemoryWorkerExtractorFailureReturnsError(t *testing.T) {
	worker, _, _ := newTestCandidateWorker(t, fakeExtractor{err: errors.New("llm timeout")})
	if _, err := worker.Handle(candidatememory.Job{OrgID: "org-1", ProjectID: "proj-1", SourceKey: "sk", SourceEventID: "e1"}); err == nil {
		t.Fatal("提炼失败应返回 error")
	}
}

// 事件加载失败 → 返回 error。
func TestCandidateMemoryWorkerEventLoaderFailureReturnsError(t *testing.T) {
	repo := candidatememory.NewInMemoryRepository()
	service := candidatememory.NewService(repo, candidatememory.RuleScorer{})
	worker := NewCandidateMemoryWorker(fakeExtractor{}, candidatememory.NewRouter(nil), service, repo, fakeEventLoader{err: errors.New("event not found")})
	if _, err := worker.Handle(candidatememory.Job{OrgID: "o", SourceEventID: "e1"}); err == nil {
		t.Fatal("事件加载失败应返回 error")
	}
}

// 门控:低价值 assistant_final 时 extractor 不应被调用,返回空 candidate ids。
func TestCandidateMemoryWorkerGateSkipsLowValueEvents(t *testing.T) {
	extractorCalled := false
	extractor := &trackingExtractor{
		candidates: []candidatememory.Candidate{},
		called:     &extractorCalled,
	}
	loader := fakeEventLoader{
		req: candidatememory.ExtractionRequest{
			OrgID: "org-1", ProjectID: "proj-1", SourceKey: "sk",
			Events: []candidatememory.ExtractionEvent{
				{EventID: "e1", Type: "assistant_final", Payload: []byte(`{"text":"命令完成，测试通过"}`)},
			},
		},
	}
	worker := NewCandidateMemoryWorker(extractor, candidatememory.NewRouter(nil), candidatememory.NewService(candidatememory.NewInMemoryRepository(), candidatememory.RuleScorer{}), candidatememory.NewInMemoryRepository(), loader)

	result, err := worker.Handle(candidatememory.Job{OrgID: "org-1", ProjectID: "proj-1", SourceEventID: "e1"})
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if len(result.CandidateIDs) != 0 {
		t.Fatalf("低价值事件应返回空 candidate ids,得到 %v", result.CandidateIDs)
	}
	if extractorCalled {
		t.Fatal("低价值事件不应调用 extractor")
	}
}

// 门控:manual_archive_request 时 extractor 必须被调用。
func TestCandidateMemoryWorkerGateAllowsManualArchive(t *testing.T) {
	extractorCalled := false
	extractor := &trackingExtractor{
		candidates: []candidatememory.Candidate{
			{CandidateID: "cand-m", MemoryType: candidatememory.MemoryTypeFact, Content: "manual", Confidence: 0.9, RiskLevel: candidatememory.RiskLow},
		},
		called: &extractorCalled,
	}
	loader := fakeEventLoader{
		req: candidatememory.ExtractionRequest{
			OrgID: "org-1", ProjectID: "proj-1", SourceKey: "sk",
			Events: []candidatememory.ExtractionEvent{
				{EventID: "e1", Type: "manual_archive_request", Payload: []byte(`{"topic":"short"}`)},
			},
		},
	}
	worker := NewCandidateMemoryWorker(extractor, candidatememory.NewRouter(nil), candidatememory.NewService(candidatememory.NewInMemoryRepository(), candidatememory.RuleScorer{}), candidatememory.NewInMemoryRepository(), loader)

	result, err := worker.Handle(candidatememory.Job{OrgID: "org-1", ProjectID: "proj-1", SourceEventID: "e1"})
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if len(result.CandidateIDs) != 1 {
		t.Fatalf("manual archive 应产出 1 个候选,得到 %d", len(result.CandidateIDs))
	}
	if !extractorCalled {
		t.Fatal("manual archive 必须调用 extractor")
	}
}

// 门控:高价值 assistant_final 时 extractor 必须被调用。
func TestCandidateMemoryWorkerGateAllowsHighValueEvents(t *testing.T) {
	extractorCalled := false
	extractor := &trackingExtractor{
		candidates: []candidatememory.Candidate{
			{CandidateID: "cand-h", MemoryType: candidatememory.MemoryTypePreference, Content: "偏好", Confidence: 0.9, RiskLevel: candidatememory.RiskLow},
		},
		called: &extractorCalled,
	}
	loader := fakeEventLoader{
		req: candidatememory.ExtractionRequest{
			OrgID: "org-1", ProjectID: "proj-1", SourceKey: "sk",
			Events: []candidatememory.ExtractionEvent{
				{EventID: "e1", Type: "assistant_final", Payload: []byte(`{"text":"我希望以后都用 Go"}`)},
			},
		},
	}
	worker := NewCandidateMemoryWorker(extractor, candidatememory.NewRouter(nil), candidatememory.NewService(candidatememory.NewInMemoryRepository(), candidatememory.RuleScorer{}), candidatememory.NewInMemoryRepository(), loader)

	result, err := worker.Handle(candidatememory.Job{OrgID: "org-1", ProjectID: "proj-1", SourceEventID: "e1"})
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if len(result.CandidateIDs) != 1 {
		t.Fatalf("高价值事件应产出 1 个候选,得到 %d", len(result.CandidateIDs))
	}
	if !extractorCalled {
		t.Fatal("高价值事件必须调用 extractor")
	}
}

// extractor 失败仍返回 error,让 queue Fail 逻辑保持原语义。
func TestCandidateMemoryWorkerExtractorFailureReturnsErrorWithGate(t *testing.T) {
	loader := fakeEventLoader{
		req: candidatememory.ExtractionRequest{
			OrgID: "org-1", ProjectID: "proj-1", SourceKey: "sk",
			Events: []candidatememory.ExtractionEvent{
				{EventID: "e1", Type: "manual_archive_request", Payload: []byte(`{"topic":"t"}`)},
			},
		},
	}
	worker := NewCandidateMemoryWorker(fakeExtractor{err: errors.New("llm timeout")}, candidatememory.NewRouter(nil), candidatememory.NewService(candidatememory.NewInMemoryRepository(), candidatememory.RuleScorer{}), candidatememory.NewInMemoryRepository(), loader)
	if _, err := worker.Handle(candidatememory.Job{OrgID: "org-1", ProjectID: "proj-1", SourceEventID: "e1"}); err == nil {
		t.Fatal("提炼失败应返回 error")
	}
}

// trackingExtractor 追踪是否被调用。
type trackingExtractor struct {
	candidates []candidatememory.Candidate
	err        error
	called     *bool
}

func (e *trackingExtractor) Extract(ctx context.Context, req candidatememory.ExtractionRequest) (candidatememory.ExtractionResult, error) {
	if e.called != nil {
		*e.called = true
	}
	if e.err != nil {
		return candidatememory.ExtractionResult{}, e.err
	}
	return candidatememory.ExtractionResult{Candidates: e.candidates}, nil
}
