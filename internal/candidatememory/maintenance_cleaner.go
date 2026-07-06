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

// LLMMaintenanceCleaner 基于 LLM 的候选清洗器。
type LLMMaintenanceCleaner struct {
	client llm.ChatClient
	model  string
}

func NewLLMMaintenanceCleaner(client llm.ChatClient) LLMMaintenanceCleaner {
	return LLMMaintenanceCleaner{client: client}
}

func (c LLMMaintenanceCleaner) WithModel(model string) LLMMaintenanceCleaner {
	c.model = model
	return c
}

func (c LLMMaintenanceCleaner) Clean(ctx context.Context, candidates []Candidate) (CleanResult, error) {
	if c.client == nil {
		return CleanResult{}, errors.New("maintenance cleaner not configured")
	}
	if len(candidates) == 0 {
		return CleanResult{}, errors.New("no candidates to clean")
	}

	// 构造候选摘要(脱敏后)
	summary := buildCandidateSummary(candidates)

	resp, err := c.client.Chat(ctx, llm.ChatRequest{
		Model: c.model,
		Messages: []llm.Message{
			{Role: "system", Content: maintenanceCleanerSystemPrompt()},
			{Role: "user", Content: summary},
		},
	})
	if err != nil {
		return CleanResult{}, err
	}

	result, err := parseMaintenanceCleanResponse(resp.Text)
	if err != nil {
		return CleanResult{}, err
	}

	// Secret sanitize
	result.Summary = secret.Sanitize(result.Summary, nil).Text

	return result, nil
}

type rawCleanResult struct {
	DiscardIDs  []string   `json:"discard_ids"`
	KeepIDs     []string   `json:"keep_ids"`
	MergeGroups [][]string `json:"merge_groups"`
	Summary     string     `json:"summary"`
}

func parseMaintenanceCleanResponse(text string) (CleanResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return CleanResult{}, errors.New("clean response is empty")
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < 0 || end <= start {
		return CleanResult{}, errors.New("clean response is not valid JSON")
	}
	var raw rawCleanResult
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return CleanResult{}, fmt.Errorf("parse clean response: %w", err)
	}
	return CleanResult{
		DiscardIDs:  raw.DiscardIDs,
		KeepIDs:     raw.KeepIDs,
		MergeGroups: raw.MergeGroups,
		Summary:     raw.Summary,
	}, nil
}

func buildCandidateSummary(candidates []Candidate) string {
	var b strings.Builder
	b.WriteString("Candidates to clean:\n\n")
	for _, c := range candidates {
		b.WriteString(fmt.Sprintf("- ID: %s\n", c.CandidateID))
		b.WriteString(fmt.Sprintf("  Type: %s\n", c.MemoryType))
		b.WriteString(fmt.Sprintf("  Content: %s\n", c.Content))
		if c.Summary != "" {
			b.WriteString(fmt.Sprintf("  Summary: %s\n", c.Summary))
		}
		b.WriteString(fmt.Sprintf("  Risk: %s\n", c.RiskLevel))
		b.WriteString(fmt.Sprintf("  Confidence: %.2f\n", c.Confidence))
		b.WriteString("\n")
	}
	return b.String()
}

func maintenanceCleanerSystemPrompt() string {
	return strings.TrimSpace(`你是 Memory OS 的候选记忆清洗器。分析候选记忆列表,决定丢弃噪声、保留高价值候选。

规则:
- 只输出 JSON:{"discard_ids":["..."],"keep_ids":["..."],"merge_groups":[["..."]],"summary":"..."}
- discard_ids: 应丢弃的候选 ID(噪声、命令输出、测试日志、路径访问记录等)
- keep_ids: 应保留的候选 ID(用户偏好、稳定事实、决策、风险、待办等)
- merge_groups: 应合并的候选 ID 组(内容重复或高度相似的候选)
- summary: 清洗摘要(简体中文,一句话说明主要操作)
- 高风险(risk_level=high)候选不能放入 discard_ids
- 所有候选 ID 必须来自输入列表
- 不得在 summary 中保留任何 API key、token、密码明文`)
}
