package candidatememory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	"memory-os/internal/llm"
	"memory-os/internal/secret"
)

// ErrExtractorNotConfigured 提炼器未配置 LLM client。
var ErrExtractorNotConfigured = errors.New("candidate extractor not configured")

// ExtractionEvent 提炼所需的事件最小投影。
// 用本地结构而非直接引用 eventlog.TurnEvent,避免循环依赖;Phase 4 由 adapter 转换。
type ExtractionEvent struct {
	EventID string
	Type    string
	Payload []byte
}

// ExtractionRequest 候选提炼输入,携带完整归属信息(硬规则 4)。
type ExtractionRequest struct {
	OrgID     string
	ProjectID string
	SourceKey string
	UserID    string
	AgentID   string
	ThreadID  string
	SessionID string
	Events    []ExtractionEvent
}

// ExtractionResult 提炼产出。
type ExtractionResult struct {
	Candidates []Candidate
}

// Extractor 从事件提炼结构化候选(fact/decision/bugfix/preference/risk/follow_up)。
// LLM 返回必须经过 Secret sanitize;失败时返回 error 供 worker 重试,不影响 eventlog。
type Extractor interface {
	Extract(ctx context.Context, request ExtractionRequest) (ExtractionResult, error)
}

const (
	maxExtractionEventBytes    = 48 * 1024
	maxCandidatesPerExtraction = 3
)

// LLMExtractor 基于 LLM 的候选提炼器。
type LLMExtractor struct {
	client llm.ChatClient
	model  string
}

func NewLLMExtractor(client llm.ChatClient) LLMExtractor {
	return LLMExtractor{client: client}
}

// WithModel 设置 Chat 模型名(生产从 cfg.LLMModel 注入)。
func (e LLMExtractor) WithModel(model string) LLMExtractor { e.model = model; return e }

func (e LLMExtractor) Extract(ctx context.Context, request ExtractionRequest) (ExtractionResult, error) {
	if e.client == nil {
		return ExtractionResult{}, ErrExtractorNotConfigured
	}
	if len(request.Events) == 0 {
		return ExtractionResult{}, errors.New("extraction requires at least one event")
	}
	payload, err := marshalExtractionEvents(request.Events)
	if err != nil {
		return ExtractionResult{}, err
	}
	resp, err := e.client.Chat(ctx, llm.ChatRequest{
		Model: e.model,
		Messages: []llm.Message{
			{Role: "system", Content: extractionSystemPrompt()},
			{Role: "user", Content: extractionUserPrompt(payload)},
		},
	})
	if err != nil {
		return ExtractionResult{}, err
	}
	raw, err := parseExtractionResponse(resp.Text)
	if err != nil {
		return ExtractionResult{}, err
	}
	eventIDs := eventIDsFrom(request.Events)
	if len(raw) > maxCandidatesPerExtraction {
		raw = raw[:maxCandidatesPerExtraction]
	}
	for i := range raw {
		c := &raw[i]
		c.OrgID = request.OrgID
		c.ProjectID = request.ProjectID
		c.SourceKey = request.SourceKey
		c.UserID = request.UserID
		c.AgentID = request.AgentID
		c.ThreadID = request.ThreadID
		c.SessionID = request.SessionID
		c.SourceEventIDs = eventIDs
		if c.CandidateID == "" {
			c.CandidateID = NewCandidateID(request.SourceKey, c)
		}
		if c.Status == "" {
			c.Status = StatusPending
		}
		// Secret sanitize:明文不进 candidate(硬规则)
		c.Content = secret.Sanitize(c.Content, nil).Text
		c.Summary = secret.Sanitize(c.Summary, nil).Text
		// 风险评估:高风险关键词命中提升为 high
		c.RiskLevel = AssessRisk(c.Content, c.RiskLevel)
	}
	return ExtractionResult{Candidates: raw}, nil
}

type rawCandidate struct {
	MemoryType string  `json:"memory_type"`
	Content    string  `json:"content"`
	Summary    string  `json:"summary"`
	Confidence float64 `json:"confidence"`
}

type extractionEnvelope struct {
	Candidates []rawCandidate `json:"candidates"`
}

func parseExtractionResponse(text string) ([]Candidate, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("extraction returned empty response")
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < 0 || end <= start {
		return nil, errors.New("extraction response is not valid JSON")
	}
	var envelope extractionEnvelope
	if err := json.Unmarshal([]byte(text[start:end+1]), &envelope); err != nil {
		return nil, fmt.Errorf("parse extraction response: %w", err)
	}
	out := make([]Candidate, 0, len(envelope.Candidates))
	for _, item := range envelope.Candidates {
		out = append(out, Candidate{
			MemoryType: MemoryType(item.MemoryType),
			Content:    item.Content,
			Summary:    item.Summary,
			Confidence: item.Confidence,
			RiskLevel:  RiskLow,
		})
	}
	return out, nil
}

func marshalExtractionEvents(events []ExtractionEvent) (string, error) {
	body, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return "", err
	}
	text := string(body)
	if len(text) > maxExtractionEventBytes {
		return text[:maxExtractionEventBytes] + "\n...[truncated]", nil
	}
	return text, nil
}

func eventIDsFrom(events []ExtractionEvent) []string {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		if e.EventID != "" {
			ids = append(ids, e.EventID)
		}
	}
	return ids
}

// NewCandidateID 基于来源、内容和类型生成稳定候选 ID,用于提炼器和兜底候选保持同一幂等规则。
func NewCandidateID(sourceKey string, c *Candidate) string {
	h := fnv.New32a()
	h.Write([]byte(sourceKey))
	h.Write([]byte(c.Content))
	h.Write([]byte(string(c.MemoryType)))
	return fmt.Sprintf("cand-%x", h.Sum32())
}

func extractionSystemPrompt() string {
	return strings.TrimSpace(`你是 Memory OS 的候选记忆提炼器。从对话事件中提炼结构化候选记忆,输出简体中文 JSON。

要求:
- 只输出 JSON:{"candidates":[{"memory_type":"...","content":"...","summary":"...","confidence":0.0}]}
- memory_type 必须是其中之一:fact、decision、bugfix、preference、risk、follow_up
- content 为简体中文一句话事实;summary 为更短摘要(可空)
- confidence ∈ [0,1]:事实确定程度
- 每次最多输出 3 条候选;只保留长期有用的用户偏好、稳定事实、关键决策、明确风险或待办
- 不得在 content/summary 中保留任何 API key、token、密码、私钥、cookie 明文
- 忽略测试 marker、临时验证词、重复日志、文件路径访问记录、命令输出流水账、模型名、权限模式、内部状态和调试过程噪声`)
}

func extractionUserPrompt(payload string) string {
	return "Events JSON:\n```json\n" + payload + "\n```"
}
