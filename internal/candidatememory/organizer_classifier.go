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

// OrganizerAction 统一整理决策动作集。
type OrganizerAction string

const (
	OrganizerActionDiscardNoise    OrganizerAction = "discard_noise"
	OrganizerActionKeepCandidate   OrganizerAction = "keep_candidate"
	OrganizerActionArchiveMaterial OrganizerAction = "archive_material"
	OrganizerActionPromoteHot      OrganizerAction = "promote_hot"
	OrganizerActionNeedsReview     OrganizerAction = "needs_review"
	OrganizerActionDuplicateOf     OrganizerAction = "duplicate_of"
)

// OrganizerDecision 单条候选的整理决策。
type OrganizerDecision struct {
	CandidateID  string          `json:"candidate_id"`
	Action       OrganizerAction `json:"action"`
	Scope        string          `json:"scope"`
	Confidence   float64         `json:"confidence"`
	Reason       string          `json:"reason"`
	MergeTarget  string          `json:"merge_target,omitempty"`
	ProjectLinks []string        `json:"project_links,omitempty"`
}

// OrganizeResult 整理结果(脱敏后)。
type OrganizeResult struct {
	Decisions []OrganizerDecision
	Summary   string
}

// LLMOrganizer 统一 AI 整理决策器,替代 cleaner+triage 双判断。
type LLMOrganizer struct {
	client llm.ChatClient
	model  string
}

func NewLLMOrganizer(client llm.ChatClient) LLMOrganizer {
	return LLMOrganizer{client: client}
}

func (o LLMOrganizer) WithModel(model string) LLMOrganizer {
	o.model = model
	return o
}

// Organize 对候选列表做统一整理决策。projectNames 用于跨项目链接判定(可空)。
func (o LLMOrganizer) Organize(ctx context.Context, candidates []Candidate, projectNames []string) (OrganizeResult, error) {
	if o.client == nil {
		return OrganizeResult{}, errors.New("organizer not configured")
	}
	if len(candidates) == 0 {
		return OrganizeResult{}, errors.New("no candidates to organize")
	}

	summary := buildOrganizerInput(candidates, projectNames)
	resp, err := o.client.Chat(ctx, llm.ChatRequest{
		Model: o.model,
		Messages: []llm.Message{
			{Role: "system", Content: organizerSystemPrompt()},
			{Role: "user", Content: summary},
		},
	})
	if err != nil {
		return OrganizeResult{}, err
	}

	parsed, err := parseOrganizerResponse(resp.Text)
	if err != nil {
		return OrganizeResult{}, err
	}

	// 幻觉校验: 所有 candidate_id / merge_target 必须来自输入。
	idSet := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		idSet[c.CandidateID] = true
	}
	for _, d := range parsed.Decisions {
		if !idSet[d.CandidateID] {
			return OrganizeResult{}, fmt.Errorf("candidate_id %s not found in input", d.CandidateID)
		}
		if d.Action == OrganizerActionDuplicateOf && !idSet[d.MergeTarget] {
			return OrganizeResult{}, fmt.Errorf("merge_target %s not found in input", d.MergeTarget)
		}
	}

	// 高风险保护: 禁止自动 discard/promote/archive,强制降级为 needs_review。
	byID := make(map[string]Candidate, len(candidates))
	for _, c := range candidates {
		byID[c.CandidateID] = c
	}
	for i, d := range parsed.Decisions {
		if byID[d.CandidateID].RiskLevel == RiskHigh {
			if d.Action == OrganizerActionDiscardNoise || d.Action == OrganizerActionPromoteHot || d.Action == OrganizerActionArchiveMaterial {
				parsed.Decisions[i].Action = OrganizerActionNeedsReview
				parsed.Decisions[i].Reason = "高风险候选强制待确认: " + d.Reason
			}
		}
	}

	// Secret sanitize summary(不含明文密钥)。
	parsed.Summary = secret.Sanitize(parsed.Summary, nil).Text
	for i := range parsed.Decisions {
		parsed.Decisions[i].Reason = secret.Sanitize(parsed.Decisions[i].Reason, nil).Text
	}
	return parsed, nil
}

type rawOrganizeResult struct {
	Decisions []OrganizerDecision `json:"decisions"`
	Summary   string              `json:"summary"`
}

func parseOrganizerResponse(text string) (OrganizeResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return OrganizeResult{}, errors.New("organize response is empty")
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < 0 || end <= start {
		return OrganizeResult{}, errors.New("organize response is not valid JSON")
	}
	var raw rawOrganizeResult
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return OrganizeResult{}, fmt.Errorf("parse organize response: %w", err)
	}
	// 动作合法性校验。
	valid := map[OrganizerAction]bool{
		OrganizerActionDiscardNoise: true, OrganizerActionKeepCandidate: true, OrganizerActionArchiveMaterial: true,
		OrganizerActionPromoteHot: true, OrganizerActionNeedsReview: true, OrganizerActionDuplicateOf: true,
	}
	for _, d := range raw.Decisions {
		if !valid[d.Action] {
			return OrganizeResult{}, fmt.Errorf("invalid action %s for candidate %s", d.Action, d.CandidateID)
		}
	}
	return OrganizeResult{Decisions: raw.Decisions, Summary: raw.Summary}, nil
}

func buildOrganizerInput(candidates []Candidate, projectNames []string) string {
	var b strings.Builder
	b.WriteString("Candidates to organize:\n\n")
	for _, c := range candidates {
		b.WriteString(fmt.Sprintf("- ID: %s\n", c.CandidateID))
		b.WriteString(fmt.Sprintf("  Type: %s\n", c.MemoryType))
		b.WriteString(fmt.Sprintf("  Content: %s\n", c.Content))
		if c.Summary != "" {
			b.WriteString(fmt.Sprintf("  Summary: %s\n", c.Summary))
		}
		b.WriteString(fmt.Sprintf("  Risk: %s\n", c.RiskLevel))
		b.WriteString(fmt.Sprintf("  Confidence: %.2f\n", c.Confidence))
		b.WriteString(fmt.Sprintf("  CurrentStatus: %s\n", c.Status))
		b.WriteString("\n")
	}
	if len(projectNames) > 0 {
		b.WriteString("Visible projects: " + strings.Join(projectNames, ", ") + "\n")
	}
	return b.String()
}

func organizerSystemPrompt() string {
	return `你是 Memory OS 的候选记忆整理器。对每条候选输出统一去向决策。

规则:
- 只输出 JSON:{"decisions":[{"candidate_id":"...","action":"...","scope":"project|global","confidence":0.0,"reason":"...","merge_target":"可选,仅 duplicate_of","project_links":["可选跨项目"]}],"summary":"..."}
- 动作集(只能选其一):
  - discard_noise: 噪声(命令输出、测试日志、路径访问记录),应丢弃
  - keep_candidate: 高价值候选(稳定事实、偏好、决策),保留
  - archive_material: 适合沉淀为长期归档的素材(bugfix/decision/risk/follow_up)
  - promote_hot: 高频使用的短事实,提升为热记忆
  - needs_review: 无法确定,待人工确认
  - duplicate_of: 与 merge_target 指定的候选重复
- 高风险(risk=high)候选只能 needs_review,禁止 discard_noise/promote_hot/archive_material
- 所有 candidate_id 和 merge_target 必须来自输入列表
- summary 简体中文一句话说明主要整理结果
- 不得在 reason/summary 保留任何 API key、token、密码明文`
}
