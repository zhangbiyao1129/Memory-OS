package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"memory-os/internal/candidatememory"
	"memory-os/internal/secret"
)

// CandidateMemoryJobResult 候选提炼任务完成结果。
type CandidateMemoryJobResult struct {
	CandidateIDs []string
}

// CandidateEventLoader 从事件源加载并构造候选提炼请求。
// 生产由 eventlog 适配器实现(查 TurnEvent → 填 ExtractionRequest),测试可 mock。
type CandidateEventLoader interface {
	LoadExtractionRequest(ctx context.Context, job candidatememory.Job) (candidatememory.ExtractionRequest, error)
}

// CandidateMemoryWorker 候选提炼 worker:Lease job → 加载事件 → 提炼 → 分流 → 持久化 → Complete。
type CandidateMemoryWorker struct {
	extractor   candidatememory.Extractor
	router      candidatememory.Router
	service     *candidatememory.Service
	repo        candidatememory.Repository
	eventLoader CandidateEventLoader
	handle      func(candidatememory.Job) (CandidateMemoryJobResult, error)
}

func NewCandidateMemoryWorker(extractor candidatememory.Extractor, router candidatememory.Router, service *candidatememory.Service, repo candidatememory.Repository, eventLoader CandidateEventLoader) CandidateMemoryWorker {
	return CandidateMemoryWorker{
		extractor:   extractor,
		router:      router,
		service:     service,
		repo:        repo,
		eventLoader: eventLoader,
	}
}

// WithHandle 注入自定义 handle(测试用),覆盖默认实现。
func (w CandidateMemoryWorker) WithHandle(handle func(candidatememory.Job) (CandidateMemoryJobResult, error)) CandidateMemoryWorker {
	w.handle = handle
	return w
}

func (w CandidateMemoryWorker) Handle(job candidatememory.Job) (CandidateMemoryJobResult, error) {
	if w.handle != nil {
		return w.handle(job)
	}
	return w.defaultHandle(job)
}

// defaultHandle 默认提炼链路:加载事件 → 门控 → LLM 提炼(脱敏)→ 分流 → 持久化候选 → 更新 topic state。
// 重复候选(ErrConflict)跳过(去重由 hotmemory + candidate_id 哈希保证)。
func (w CandidateMemoryWorker) defaultHandle(job candidatememory.Job) (CandidateMemoryJobResult, error) {
	ctx := context.Background()
	request, err := w.eventLoader.LoadExtractionRequest(ctx, job)
	if err != nil {
		return CandidateMemoryJobResult{}, err
	}

	// 门控:低价值事件不触发 LLM 提炼
	decision := candidatememory.ShouldExtract(request)
	if !decision.Allow {
		return CandidateMemoryJobResult{CandidateIDs: nil}, nil
	}

	extracted, err := w.extractor.Extract(ctx, request)
	if err != nil {
		return CandidateMemoryJobResult{}, err
	}
	candidates := extracted.Candidates
	if len(candidates) == 0 && decision.Reason == "manual_archive" {
		if fallback, ok := fallbackManualArchiveCandidate(request); ok {
			candidates = []candidatememory.Candidate{fallback}
		}
	}
	candidateIDs := make([]string, 0, len(candidates))
	for i := range candidates {
		c := candidates[i]
		fillOwnership(&c, request)
		routed, _, err := w.router.ApplyRouting(c)
		if err != nil {
			return CandidateMemoryJobResult{}, err
		}
		saved, err := w.service.CreateAndScore(ctx, routed)
		if err != nil {
			if errors.Is(err, candidatememory.ErrConflict) {
				continue
			}
			return CandidateMemoryJobResult{}, err
		}
		candidateIDs = append(candidateIDs, saved.CandidateID)
	}
	if err := w.updateTopicState(ctx, job, request.ThreadID, len(candidateIDs)); err != nil {
		return CandidateMemoryJobResult{}, err
	}
	return CandidateMemoryJobResult{CandidateIDs: candidateIDs}, nil
}

// updateTopicState 累加候选计数并刷新 last_event_at(topic 沉淀 ready 判断用)。
func (w CandidateMemoryWorker) updateTopicState(ctx context.Context, job candidatememory.Job, threadID string, added int) error {
	if w.repo == nil || threadID == "" {
		return nil
	}
	now := time.Now().UTC()
	newCount := added
	existing, err := w.repo.GetTopicState(ctx, job.OrgID, job.ProjectID, job.SourceKey, threadID)
	if err == nil {
		newCount = existing.CandidateCount + added
	} else if !errors.Is(err, candidatememory.ErrNotFound) {
		return err
	}
	_, err = w.repo.UpsertTopicState(ctx, candidatememory.TopicState{
		OrgID:          job.OrgID,
		ProjectID:      job.ProjectID,
		SourceKey:      job.SourceKey,
		ThreadID:       threadID,
		CandidateCount: newCount,
		ReadyToCompose: newCount >= candidatememory.ComposeMinCandidates,
		LastEventAt:    &now,
	})
	return err
}

// fillOwnership 防御性补全候选归属。LLMExtractor 已填,此处理保底(硬规则 4:候选必须有完整归属)。
func fillOwnership(c *candidatememory.Candidate, request candidatememory.ExtractionRequest) {
	if c.OrgID == "" {
		c.OrgID = request.OrgID
	}
	if c.ProjectID == "" {
		c.ProjectID = request.ProjectID
	}
	if c.SourceKey == "" {
		c.SourceKey = request.SourceKey
	}
	if c.UserID == "" {
		c.UserID = request.UserID
	}
	if c.AgentID == "" {
		c.AgentID = request.AgentID
	}
	if c.ThreadID == "" {
		c.ThreadID = request.ThreadID
	}
	if c.SessionID == "" {
		c.SessionID = request.SessionID
	}
}

func fallbackManualArchiveCandidate(request candidatememory.ExtractionRequest) (candidatememory.Candidate, bool) {
	content := manualArchiveText(request.Events)
	content = strings.TrimSpace(secret.Sanitize(content, nil).Text)
	if content == "" {
		return candidatememory.Candidate{}, false
	}
	c := candidatememory.Candidate{
		MemoryType:     candidatememory.MemoryTypeFact,
		Content:        content,
		Summary:        summarizeFallbackContent(content),
		RiskLevel:      candidatememory.AssessRisk(content, candidatememory.RiskLow),
		Confidence:     0.6,
		Status:         candidatememory.StatusPending,
		SourceEventIDs: eventIDsFromExtractionRequest(request.Events),
	}
	c.CandidateID = candidatememory.NewCandidateID(request.SourceKey, &c)
	return c, true
}

func manualArchiveText(events []candidatememory.ExtractionEvent) string {
	fields := []string{"text", "content", "note", "summary", "topic"}
	for _, event := range events {
		if event.Type != "manual_archive_request" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			text := strings.TrimSpace(string(event.Payload))
			if text != "" {
				return text
			}
			continue
		}
		for _, field := range fields {
			if value, ok := payload[field].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func summarizeFallbackContent(content string) string {
	const maxRunes = 80
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes])
}

func eventIDsFromExtractionRequest(events []candidatememory.ExtractionEvent) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		if event.EventID != "" {
			ids = append(ids, event.EventID)
		}
	}
	return ids
}
