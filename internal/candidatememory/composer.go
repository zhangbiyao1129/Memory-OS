package candidatememory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"memory-os/internal/llm"
	"memory-os/internal/secret"
)

// 完成信号关键词(候选 content/summary 命中即视为任务完成信号)。
var completionSignalKeywords = []string{
	"已修复", "已验证", "验证通过", "已部署", "已完成", "完成", "沉淀", "总结", "已解决",
}

// 候选沉淀阈值。
const (
	ComposeMinCandidates    = 10
	composeMinCandidates    = ComposeMinCandidates
	composeIdleThreshold    = 24 * time.Hour
	composeHotMemoryUsedMin = 3
)

// ArchiveCreator 抽象 archive.Create(生产由 archive.Service + ChunkMarkdown + RAGIndexQueue 适配实现)。
type ArchiveCreator interface {
	Create(ctx context.Context, req ArchiveCreateRequest) (ArchiveCreateResult, error)
}

type ArchiveSummarizer interface {
	SummarizeArchive(ctx context.Context, req ComposeRequest, candidates []Candidate) (string, error)
}

type ArchiveCreateRequest struct {
	ArchiveID string
	Title     string
	Markdown  string
	OrgID     string
	ProjectID string
	UserID    string
	SourceKey string
}

type ArchiveCreateResult struct {
	ArchiveID string
}

// TopicComposer 把一个 topic 的候选沉淀为 Markdown Archive。
// ready 判定只用规则(信号/数量/空闲/手动),不允许仅靠 LLM 判定任务完成。
type TopicComposer struct {
	repo           Repository
	archiveCreator ArchiveCreator
	summarizer     ArchiveSummarizer
}

func NewTopicComposer(repo Repository, archiveCreator ArchiveCreator) TopicComposer {
	return TopicComposer{repo: repo, archiveCreator: archiveCreator}
}

func (c TopicComposer) WithSummarizer(summarizer ArchiveSummarizer) TopicComposer {
	c.summarizer = summarizer
	return c
}

func (c TopicComposer) SummarizerConfigured() bool {
	return c.summarizer != nil
}

type LLMArchiveSummarizer struct {
	client llm.ChatClient
	model  string
}

func NewLLMArchiveSummarizer(client llm.ChatClient) LLMArchiveSummarizer {
	return LLMArchiveSummarizer{client: client}
}

func (s LLMArchiveSummarizer) WithModel(model string) LLMArchiveSummarizer {
	s.model = model
	return s
}

func (s LLMArchiveSummarizer) SummarizeArchive(ctx context.Context, req ComposeRequest, candidates []Candidate) (string, error) {
	if s.client == nil {
		return "", errors.New("archive summarizer not configured")
	}
	input := buildArchiveSummarizerInput(req, candidates)
	resp, err := s.client.Chat(ctx, llm.ChatRequest{
		Model: s.model,
		Messages: []llm.Message{
			{Role: "system", Content: archiveSummarizerSystemPrompt()},
			{Role: "user", Content: input},
		},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Text), nil
}

type ComposeRequest struct {
	OrgID     string
	ProjectID string
	SourceKey string
	ThreadID  string
	Force     bool
}

type ComposeResult struct {
	Ready     bool
	ArchiveID string
	Composed  int
}

// Compose 判断 ready → 生成 Markdown → 创建 Archive → 标记候选 Composed + topic state。
func (c TopicComposer) Compose(ctx context.Context, req ComposeRequest) (ComposeResult, error) {
	candidates, err := c.repo.ListCandidates(ctx, ListFilter{
		OrgID: req.OrgID, ProjectID: req.ProjectID, SourceKey: req.SourceKey, ThreadID: req.ThreadID,
	})
	if err != nil {
		return ComposeResult{}, err
	}
	eligible := filterComposable(candidates)
	if len(eligible) == 0 {
		return ComposeResult{Ready: false}, nil
	}

	ts, _ := c.repo.GetTopicState(ctx, req.OrgID, req.ProjectID, req.SourceKey, req.ThreadID)
	if !req.Force && !isReadyToCompose(ts, eligible) {
		return ComposeResult{Ready: false}, nil
	}

	archiveID := newArchiveID(req)
	markdown, err := c.composeArchiveMarkdown(ctx, req, eligible)
	if err != nil {
		return ComposeResult{}, err
	}
	if c.archiveCreator != nil {
		if _, err := c.archiveCreator.Create(ctx, ArchiveCreateRequest{
			ArchiveID: archiveID,
			Title:     composeTitle(req),
			Markdown:  markdown,
			OrgID:     req.OrgID,
			ProjectID: req.ProjectID,
			UserID:    firstUserID(eligible),
			SourceKey: req.SourceKey,
		}); err != nil {
			return ComposeResult{}, err
		}
	}

	for _, cand := range eligible {
		if _, err := c.repo.UpdateCandidateStatus(ctx, req.OrgID, cand.CandidateID, StatusComposed, cand.Scores, false); err != nil {
			return ComposeResult{}, err
		}
	}
	c.repo.UpsertTopicState(ctx, TopicState{
		OrgID: req.OrgID, ProjectID: req.ProjectID, SourceKey: req.SourceKey, ThreadID: req.ThreadID,
		ComposedArchiveID: archiveID,
	})
	return ComposeResult{Ready: true, ArchiveID: archiveID, Composed: len(eligible)}, nil
}

func (c TopicComposer) composeArchiveMarkdown(ctx context.Context, req ComposeRequest, candidates []Candidate) (string, error) {
	if c.summarizer == nil {
		return composeMarkdown(req, candidates), nil
	}
	markdown, err := c.summarizer.SummarizeArchive(ctx, req, candidates)
	if err != nil {
		return "", err
	}
	markdown = strings.TrimSpace(secret.Sanitize(markdown, nil).Text)
	if markdown == "" {
		return "", errors.New("archive summarizer returned empty markdown")
	}
	return markdown, nil
}

// filterComposable 排除已丢弃/已归档 + 未确认的高风险(pending_review 不得自动入 Markdown)。
func filterComposable(candidates []Candidate) []Candidate {
	out := []Candidate{}
	for _, c := range candidates {
		if c.Status == StatusDiscarded || c.Status == StatusComposed {
			continue
		}
		if c.RiskLevel == RiskHigh && c.Status == StatusPending {
			continue
		}
		out = append(out, c)
	}
	return out
}

// isReadyToCompose 规则判定(不调 LLM):
// 候选>=10 / 完成信号 / 24h 无新事件。
func isReadyToCompose(ts TopicState, candidates []Candidate) bool {
	if len(candidates) >= composeMinCandidates {
		return true
	}
	if containsCompletionSignal(candidates) {
		return true
	}
	if ts.LastEventAt != nil && time.Since(*ts.LastEventAt) >= composeIdleThreshold {
		return true
	}
	return false
}

func containsCompletionSignal(candidates []Candidate) bool {
	for _, c := range candidates {
		text := c.Content + " " + c.Summary
		for _, kw := range completionSignalKeywords {
			if strings.Contains(text, kw) {
				return true
			}
		}
	}
	return false
}

func newArchiveID(req ComposeRequest) string {
	return fmt.Sprintf("archive_topic_%s_%s_%d", sanitizeForID(req.SourceKey), req.ThreadID, time.Now().UnixNano())
}

func sanitizeForID(s string) string {
	r := strings.NewReplacer("/", "_", ":", "_", ".", "_")
	return r.Replace(s)
}

func composeTitle(req ComposeRequest) string {
	return "记忆归档: " + req.ThreadID
}

func firstUserID(candidates []Candidate) string {
	for _, c := range candidates {
		if c.UserID != "" {
			return c.UserID
		}
	}
	return ""
}

func buildArchiveSummarizerInput(req ComposeRequest, candidates []Candidate) string {
	var b strings.Builder
	b.WriteString("请把以下候选记忆整理为长期归档 Markdown。\n")
	b.WriteString("- source_key: `" + req.SourceKey + "`\n")
	b.WriteString("- thread_id: `" + req.ThreadID + "`\n\n")
	for _, c := range candidates {
		b.WriteString("候选 ID: `" + c.CandidateID + "`\n")
		b.WriteString("类型: " + string(c.MemoryType) + "\n")
		b.WriteString("风险: " + string(c.RiskLevel) + "\n")
		b.WriteString("内容: " + c.Content + "\n")
		if c.Summary != "" {
			b.WriteString("摘要: " + c.Summary + "\n")
		}
		if len(c.SourceEventIDs) > 0 {
			b.WriteString("事件: `" + strings.Join(c.SourceEventIDs, "`, `") + "`\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func archiveSummarizerSystemPrompt() string {
	return strings.TrimSpace(`你是 Memory OS 的长期归档整理器。把同一 source_key/thread_id 下的一组候选记忆整理成一份可复用的 Markdown 归档。

要求:
- 只输出 Markdown,不要输出 JSON 或解释
- 必须包含章节:# 标题、## 结论、## 背景、## 关键决策、## 修复/配置、## 验证证据、## 可复用经验、## 后续事项、## 来源
- 归纳总结,不要逐条机械复制候选
- 保留可追溯来源: source_key、thread_id、candidate_id、event_id
- 不得保留 API key、token、密码、私钥、cookie 等明文敏感信息
- 不确定的信息写入后续事项或遗留风险,不要编造事实`)
}

// composeMarkdown 固定结构:标题/结论/背景/现象/根因/修复/验证/遗留风险/可复用经验/后续事项/来源。
// 所有 section header 固定写出(内容可为空),便于检索与人工审阅。
func composeMarkdown(req ComposeRequest, candidates []Candidate) string {
	var b strings.Builder
	b.WriteString("# " + composeTitle(req) + "\n\n")

	writeSection := func(title string, matcher func(Candidate) bool) {
		b.WriteString("## " + title + "\n")
		for _, c := range candidates {
			if matcher(c) {
				b.WriteString("- " + c.Content + "\n")
			}
		}
		b.WriteString("\n")
	}

	writeSection("结论", func(c Candidate) bool { return c.MemoryType == MemoryTypeDecision })
	writeSection("背景", func(c Candidate) bool { return c.MemoryType == MemoryTypeFact })
	writeSection("现象", func(c Candidate) bool { return c.MemoryType == MemoryTypeFact })
	writeSection("根因", func(c Candidate) bool { return c.MemoryType == MemoryTypeRisk || c.MemoryType == MemoryTypeBugfix })
	writeSection("修复", func(c Candidate) bool { return c.MemoryType == MemoryTypeBugfix })
	writeSection("验证", func(c Candidate) bool { return c.MemoryType == MemoryTypeDecision })
	writeSection("遗留风险", func(c Candidate) bool { return c.RiskLevel == RiskHigh || c.RiskLevel == RiskMedium })
	writeSection("可复用经验", func(c Candidate) bool { return c.MemoryType == MemoryTypePreference })
	writeSection("后续事项", func(c Candidate) bool { return c.MemoryType == MemoryTypeFollowUp })

	b.WriteString("## 来源\n")
	b.WriteString("- source_key: `" + req.SourceKey + "`\n")
	b.WriteString("- thread_id: `" + req.ThreadID + "`\n")
	for _, c := range candidates {
		for _, eid := range c.SourceEventIDs {
			b.WriteString("- event_id: `" + eid + "`\n")
		}
	}
	b.WriteString("\n")
	return b.String()
}
