package hotmemory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"memory-os/internal/llm"
	"memory-os/internal/secret"
)

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

func (o LLMOrganizer) Organize(ctx context.Context, memories []Memory) (OrganizeDecision, error) {
	if o.client == nil {
		return OrganizeDecision{}, errors.New("hot memory organizer not configured")
	}
	if len(memories) == 0 {
		return OrganizeDecision{}, errors.New("no hot memories to organize")
	}
	resp, err := o.client.Chat(ctx, llm.ChatRequest{
		Model: o.model,
		Messages: []llm.Message{
			{Role: "system", Content: hotMemoryOrganizerSystemPrompt()},
			{Role: "user", Content: buildHotMemorySummary(memories)},
		},
	})
	if err != nil {
		return OrganizeDecision{}, err
	}
	decision, err := parseOrganizeDecision(resp.Text)
	if err != nil {
		return OrganizeDecision{}, err
	}
	decision.Summary = secret.Sanitize(decision.Summary, nil).Text
	return decision, nil
}

type rawOrganizeDecision struct {
	DemoteIDs   []string   `json:"demote_ids"`
	KeepIDs     []string   `json:"keep_ids"`
	MergeGroups [][]string `json:"merge_groups"`
	Summary     string     `json:"summary"`
}

func parseOrganizeDecision(text string) (OrganizeDecision, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return OrganizeDecision{}, errors.New("organize response is empty")
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < 0 || end <= start {
		return OrganizeDecision{}, errors.New("organize response is not valid JSON")
	}
	var raw rawOrganizeDecision
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return OrganizeDecision{}, fmt.Errorf("parse organize response: %w", err)
	}
	return OrganizeDecision{
		DemoteIDs:   raw.DemoteIDs,
		KeepIDs:     raw.KeepIDs,
		MergeGroups: raw.MergeGroups,
		Summary:     raw.Summary,
	}, nil
}

func buildHotMemorySummary(memories []Memory) string {
	var b strings.Builder
	b.WriteString("Hot memories to organize:\n\n")
	for _, memory := range memories {
		b.WriteString(fmt.Sprintf("- ID: %s\n", memory.MemoryID))
		b.WriteString(fmt.Sprintf("  Fact: %s\n", memory.Fact))
		b.WriteString(fmt.Sprintf("  Status: %s\n", memory.Status))
		b.WriteString(fmt.Sprintf("  Confidence: %.2f\n", memory.Confidence))
		b.WriteString(fmt.Sprintf("  AccessCount: %d\n", memory.AccessCount))
		b.WriteString(fmt.Sprintf("  ReturnedCount: %d\n", memory.ReturnedCount))
		b.WriteString(fmt.Sprintf("  UsedCount: %d\n", memory.UsedCount))
		b.WriteString(fmt.Sprintf("  LastAccessedAt: %s\n", memory.LastAccessedAt.Format("2006-01-02T15:04:05Z07:00")))
		b.WriteString(fmt.Sprintf("  LastReturnedAt: %s\n", memory.LastReturnedAt.Format("2006-01-02T15:04:05Z07:00")))
		b.WriteString(fmt.Sprintf("  LastUsedAt: %s\n", memory.LastUsedAt.Format("2006-01-02T15:04:05Z07:00")))
		b.WriteString("\n")
	}
	return b.String()
}

func hotMemoryOrganizerSystemPrompt() string {
	return strings.TrimSpace(`你是 Memory OS 的热记忆整理器。分析热记忆列表,决定哪些应降权、保留或标记为重复组。

规则:
- 只输出 JSON:{"demote_ids":["..."],"keep_ids":["..."],"merge_groups":[["..."]],"summary":"..."}
- demote_ids: 应降权的热记忆 ID,适用于低价值、泛泛而谈、无真实使用信号、重复或已被 Archive 更好承载的内容
- keep_ids: 应保持活跃的热记忆 ID,适用于用户偏好、稳定事实、重要决策、仍需快速召回的内容
- merge_groups: 内容重复或高度相似的热记忆 ID 组
- 不要输出删除动作;系统只会自动降权,不会自动删除
- 所有 ID 必须来自输入列表
- pinned/promoted 记忆不会出现在输入中,不要臆造
- summary 使用简体中文一句话,不得包含 API key、token、密码明文`)
}
