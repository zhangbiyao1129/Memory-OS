package candidatememory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"memory-os/internal/llm"
	"memory-os/internal/secret"
)

// TriageProject 是分类器可见的项目目录项。
type TriageProject struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	SourceKey string `json:"source_key"`
}

// TriageInput 是单条候选记忆的自动整理输入。
type TriageInput struct {
	Candidate Candidate
	Projects  []TriageProject
}

// TriageDecision 是分类器输出的整理决策。
type TriageDecision struct {
	Scope        TriageScope
	Confidence   float64
	Reason       string
	ProjectLinks []CandidateProjectLink
}

// TriageClassifier 判断候选记忆应该进入项目、全局、工具、偏好、收件箱或丢弃。
type TriageClassifier interface {
	Classify(ctx context.Context, input TriageInput) (TriageDecision, error)
}

// LLMTriageClassifier 使用 LLM 输出 JSON 决策。
type LLMTriageClassifier struct {
	client llm.ChatClient
	model  string
}

func NewLLMTriageClassifier(client llm.ChatClient) LLMTriageClassifier {
	return LLMTriageClassifier{client: client}
}

func (c LLMTriageClassifier) WithModel(model string) LLMTriageClassifier {
	c.model = model
	return c
}

func (c LLMTriageClassifier) Classify(ctx context.Context, input TriageInput) (TriageDecision, error) {
	if c.client == nil {
		return TriageDecision{}, errors.New("triage classifier not configured")
	}
	resp, err := c.client.Chat(ctx, llm.ChatRequest{
		Model: c.model,
		Messages: []llm.Message{
			{Role: "system", Content: triageSystemPrompt()},
			{Role: "user", Content: buildTriagePrompt(input)},
		},
	})
	if err != nil {
		return TriageDecision{}, err
	}
	return parseTriageResponse(resp.Text)
}

type rawTriageDecision struct {
	Scope        string                 `json:"scope"`
	Confidence   float64                `json:"confidence"`
	Reason       string                 `json:"reason"`
	ProjectLinks []rawTriageProjectLink `json:"project_links"`
}

type rawTriageProjectLink struct {
	ProjectID  string  `json:"project_id"`
	SourceKey  string  `json:"source_key"`
	Confidence float64 `json:"confidence"`
	Evidence   string  `json:"evidence"`
}

func parseTriageResponse(text string) (TriageDecision, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return TriageDecision{}, errors.New("triage response is empty")
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < 0 || end <= start {
		return TriageDecision{}, errors.New("triage response is not valid JSON")
	}
	var raw rawTriageDecision
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return TriageDecision{}, fmt.Errorf("parse triage response: %w", err)
	}
	scope := TriageScope(strings.TrimSpace(raw.Scope))
	if !scope.Valid() {
		return TriageDecision{}, fmt.Errorf("invalid triage scope %q", raw.Scope)
	}
	decision := TriageDecision{
		Scope:      scope,
		Confidence: clampConfidence(raw.Confidence),
		Reason:     sanitizeTriageText(raw.Reason),
	}
	for _, link := range raw.ProjectLinks {
		if strings.TrimSpace(link.ProjectID) == "" {
			continue
		}
		decision.ProjectLinks = append(decision.ProjectLinks, CandidateProjectLink{
			LinkedProjectID: link.ProjectID,
			LinkedSourceKey: link.SourceKey,
			Confidence:      clampConfidence(link.Confidence),
			Evidence:        sanitizeTriageText(link.Evidence),
			Status:          "active",
		})
	}
	return decision, nil
}

// RuleTriageClassifier 是 LLM 不可用时的保守 fallback。
type RuleTriageClassifier struct{}

func (RuleTriageClassifier) Classify(ctx context.Context, input TriageInput) (TriageDecision, error) {
	c := input.Candidate
	if c.RiskLevel == RiskHigh {
		return TriageDecision{Scope: TriageScopeInbox, Confidence: 0.5, Reason: "高风险候选需要复核"}, nil
	}
	if c.MemoryType == MemoryTypePreference {
		return TriageDecision{Scope: TriageScopePersonalPref, Confidence: 0.76, Reason: "用户偏好候选"}, nil
	}
	content := strings.ToLower(c.Content + " " + c.SourceKey)
	if strings.HasPrefix(c.SourceKey, "local/") && containsAny(content, []string{"codex", "mcp", "hook", "配置", "代理", "模型"}) {
		return TriageDecision{Scope: TriageScopeTooling, Confidence: 0.72, Reason: "本地工具配置经验"}, nil
	}
	return TriageDecision{Scope: TriageScopeProject, Confidence: 0.6, Reason: "项目上下文候选"}, nil
}

func buildTriagePrompt(input TriageInput) string {
	c := input.Candidate
	projects, _ := json.Marshal(input.Projects)
	var b strings.Builder
	b.WriteString("请整理以下候选记忆。\n")
	b.WriteString(fmt.Sprintf("candidate_id: %s\n", c.CandidateID))
	b.WriteString(fmt.Sprintf("memory_type: %s\n", c.MemoryType))
	b.WriteString(fmt.Sprintf("risk_level: %s\n", c.RiskLevel))
	b.WriteString(fmt.Sprintf("confidence: %.2f\n", c.Confidence))
	b.WriteString(fmt.Sprintf("source_key: %s\n", c.SourceKey))
	b.WriteString(fmt.Sprintf("content: %s\n", secret.Sanitize(c.Content, nil).Text))
	b.WriteString("project_catalog_json:\n")
	b.Write(projects)
	return b.String()
}

func triageSystemPrompt() string {
	return strings.TrimSpace(`你是 Memory OS 的候选记忆自动整理器。只输出 JSON。
schema: {"scope":"project|global|tooling|personal_pref|inbox|discard","confidence":0.0,"reason":"简短中文理由","project_links":[{"project_id":"project_x","source_key":"github.com/acme/repo","confidence":0.0,"evidence":"命中依据"}]}
规则:
- project: 只属于当前项目或能关联到具体项目。
- global/tooling/personal_pref: 对用户跨项目复用有稳定价值。
- inbox: 不确定或需要复核。
- discard: 明显噪声,但不要输出任何敏感信息。
- 不得在 reason/evidence 中保留 API key、token、密码或私钥明文。`)
}

func sanitizeTriageText(text string) string {
	return strings.TrimSpace(secret.Sanitize(text, nil).Text)
}

func clampConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
