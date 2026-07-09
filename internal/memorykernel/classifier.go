package memorykernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"memory-os/internal/llm"
	"memory-os/internal/secret"
)

// Classifier LLM 分类器接口。
type Classifier interface {
	Classify(ctx context.Context, input ClassifyInput) (ClassifyResult, error)
}

// allowedActions 治理动作白名单。
var allowedActions = map[GovernanceActionName]bool{
	ActionKeep:             true,
	ActionDiscardNoise:     true,
	ActionDiscardStale:     true,
	ActionMarkSuperseded:   true,
	ActionDemoteHotMemory:  true,
	ActionCreateUnit:       true,
	ActionSupersedeUnit:    true,
	ActionCreateCorrection: true,
	ActionCreateCICase:     true,
	ActionNeedsReview:      true,
}

// highRiskAllowedActions 高风险 candidate 只允许这些动作。
var highRiskAllowedActions = map[GovernanceActionName]bool{
	ActionKeep:        true,
	ActionNeedsReview: true,
	ActionCreateUnit:  true,
}

type llmClassifyResponse struct {
	Units   []MemoryUnitJSON       `json:"units"`
	Claims  []MemoryClaimJSON      `json:"claims"`
	Actions []GovernanceActionJSON `json:"actions"`
	CICases []CICaseJSON           `json:"ci_cases"`
	Summary string                 `json:"summary"`
}

type MemoryUnitJSON struct {
	UnitID      string      `json:"unit_id"`
	Type        string      `json:"type"`
	Content     string      `json:"content"`
	AppliesWhen string      `json:"applies_when"`
	AgentShould string      `json:"agent_should"`
	Status      string      `json:"status"`
	Confidence  float64     `json:"confidence"`
	TrustScore  float64     `json:"trust_score"`
	RiskLevel   string      `json:"risk_level"`
	SourceRefs  []SourceRef `json:"source_refs"`
}

type MemoryClaimJSON struct {
	ClaimID      string      `json:"claim_id"`
	UnitID       string      `json:"unit_id"`
	Subject      string      `json:"subject"`
	Predicate    string      `json:"predicate"`
	Value        string      `json:"value"`
	Polarity     string      `json:"polarity"`
	Confidence   float64     `json:"confidence"`
	EvidenceRefs []SourceRef `json:"evidence_refs"`
}

type GovernanceActionJSON struct {
	ActionID     string      `json:"action_id"`
	TargetType   string      `json:"target_type"`
	TargetID     string      `json:"target_id"`
	Action       string      `json:"action"`
	Reason       string      `json:"reason"`
	EvidenceRefs []SourceRef `json:"evidence_refs"`
}

type CICaseJSON struct {
	CaseID         string   `json:"case_id"`
	Question       string   `json:"question"`
	MustInclude    []string `json:"must_include"`
	MustNotInclude []string `json:"must_not_include"`
}

// LLMClassifier 基于 LLM 的记忆治理分类器。
type LLMClassifier struct {
	client llm.ChatClient
	model  string
}

func NewLLMClassifier(client llm.ChatClient) *LLMClassifier {
	return &LLMClassifier{client: client}
}

func (c *LLMClassifier) WithModel(model string) *LLMClassifier {
	c.model = model
	return c
}

func (c *LLMClassifier) Classify(ctx context.Context, input ClassifyInput) (ClassifyResult, error) {
	prompt := c.buildPrompt(input)
	resp, err := c.client.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: prompt}},
		Model:    c.model,
	})
	if err != nil {
		return ClassifyResult{}, fmt.Errorf("llm chat: %w", err)
	}
	return c.parseResponse(resp.Text, input)
}

func (c *LLMClassifier) buildPrompt(input ClassifyInput) string {
	var sb strings.Builder
	sb.WriteString("你是 Memory Kernel 治理分类器。根据输入的记忆上下文，输出严格 JSON。")
	sb.WriteString("\n\n## 输入上下文\n")
	sb.WriteString(fmt.Sprintf("Scope: org=%s project=%s\n", input.Scope.OrgID, input.Scope.ProjectID))

	if len(input.Candidates) > 0 {
		sb.WriteString("\n### 候选记忆\n")
		for _, cand := range input.Candidates {
			sb.WriteString(fmt.Sprintf("- id=%s type=%s risk=%s status=%s confidence=%.2f content=%s\n",
				cand.ID, cand.Type, cand.RiskLevel, cand.Status, cand.Confidence, cand.Content))
		}
	}
	if len(input.HotMemories) > 0 {
		sb.WriteString("\n### 热记忆\n")
		for _, hm := range input.HotMemories {
			sb.WriteString(fmt.Sprintf("- id=%s status=%s access=%d returned=%d used=%d pinned=%v fact=%s\n",
				hm.ID, hm.Status, hm.AccessCount, hm.ReturnedCount, hm.UsedCount, hm.Pinned, hm.Fact))
		}
	}
	if len(input.Archives) > 0 {
		sb.WriteString("\n### 归档\n")
		for _, a := range input.Archives {
			sb.WriteString(fmt.Sprintf("- id=%s title=%s excerpt=%s\n", a.ID, a.Title, a.Excerpt))
		}
	}
	if len(input.Retrievals) > 0 {
		sb.WriteString("\n### 检索日志\n")
		for _, r := range input.Retrievals {
			sb.WriteString(fmt.Sprintf("- request_id=%s source_kind=%s source_id=%s\n", r.RequestID, r.SourceKind, r.SourceID))
		}
	}
	if len(input.ExistingUnits) > 0 {
		sb.WriteString("\n### 已有 memory units\n")
		for _, u := range input.ExistingUnits {
			sb.WriteString(fmt.Sprintf("- id=%s type=%s status=%s trust=%.2f content=%s\n",
				u.UnitID, u.Type, u.Status, u.TrustScore, u.Content))
		}
	}

	sb.WriteString("\n## 输出要求\n")
	sb.WriteString("输出严格 JSON，包含 units、claims、actions、ci_cases、summary 字段。\n")
	sb.WriteString("规则：\n")
	sb.WriteString("1. 所有 unit_id 必须非空。\n")
	sb.WriteString("2. 所有 content 必须非空。\n")
	sb.WriteString("3. 所有 target_id 必须来自输入 ID 集合。\n")
	sb.WriteString("4. 高风险 candidate 只能 needs_review、keep 或 create_memory_unit(且 status=needs_review)。\n")
	sb.WriteString("5. 发现旧事实被新事实覆盖时，对旧候选用 discard_stale 或 mark_superseded。\n")
	sb.WriteString("6. summary、reason、content、agent_should 中如含 secret 形式文本，使用 secret_ref 替代。\n")
	sb.WriteString("7. 识别过期/冲突/重复候选，生成 CI case 验证召回正确性。\n")
	return sb.String()
}

func (c *LLMClassifier) parseResponse(text string, input ClassifyInput) (ClassifyResult, error) {
	text = strings.TrimSpace(text)
	// 提取 JSON 块
	if idx := strings.Index(text, "{"); idx >= 0 {
		end := strings.LastIndex(text, "}")
		if end > idx {
			text = text[idx : end+1]
		}
	}

	var resp llmClassifyResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return ClassifyResult{}, fmt.Errorf("parse llm response: %w", err)
	}

	// 构建输入 ID 集合
	validIDs := buildValidIDSet(input)

	// 校验和转换
	result := ClassifyResult{Summary: resp.Summary}

	// 脱敏 summary
	result.Summary = sanitizeText(result.Summary)

	for _, uj := range resp.Units {
		if uj.UnitID == "" || uj.Content == "" {
			return ClassifyResult{}, errors.New("unit_id and content are required")
		}
		if err := validateSourceRefs("unit source refs", uj.SourceRefs, validIDs); err != nil {
			return ClassifyResult{}, err
		}
		unit := MemoryUnit{
			UnitID:      uj.UnitID,
			OrgID:       input.Scope.OrgID,
			ProjectID:   input.Scope.ProjectID,
			UserID:      input.Scope.UserID,
			Type:        UnitType(uj.Type),
			Content:     sanitizeText(uj.Content),
			AppliesWhen: sanitizeText(uj.AppliesWhen),
			AgentShould: sanitizeText(uj.AgentShould),
			Status:      UnitStatus(uj.Status),
			Confidence:  uj.Confidence,
			TrustScore:  uj.TrustScore,
			RiskLevel:   uj.RiskLevel,
			SourceRefs:  uj.SourceRefs,
		}
		result.Units = append(result.Units, unit)
	}

	validUnitIDs := buildValidUnitIDSet(input, result.Units)
	for _, cj := range resp.Claims {
		if cj.ClaimID == "" || cj.UnitID == "" || cj.Subject == "" || cj.Predicate == "" {
			return ClassifyResult{}, errors.New("claim_id, unit_id, subject, predicate are required")
		}
		if !validUnitIDs[cj.UnitID] {
			return ClassifyResult{}, fmt.Errorf("claim unit_id %q not found in input or generated units", cj.UnitID)
		}
		if err := validateSourceRefs("claim evidence refs", cj.EvidenceRefs, validIDs); err != nil {
			return ClassifyResult{}, err
		}
		claim := MemoryClaim{
			ClaimID:      cj.ClaimID,
			UnitID:       cj.UnitID,
			OrgID:        input.Scope.OrgID,
			ProjectID:    input.Scope.ProjectID,
			Subject:      cj.Subject,
			Predicate:    cj.Predicate,
			Value:        cj.Value,
			Polarity:     cj.Polarity,
			Confidence:   cj.Confidence,
			EvidenceRefs: cj.EvidenceRefs,
		}
		result.Claims = append(result.Claims, claim)
	}

	for _, aj := range resp.Actions {
		if aj.ActionID == "" || aj.TargetType == "" || aj.TargetID == "" || aj.Action == "" {
			return ClassifyResult{}, errors.New("action_id, target_type, target_id, action are required")
		}
		actionName := GovernanceActionName(aj.Action)
		if !allowedActions[actionName] {
			return ClassifyResult{}, fmt.Errorf("unknown action: %s", aj.Action)
		}
		// 校验 target_id 来自输入
		if !validIDs[aj.TargetID] {
			return ClassifyResult{}, fmt.Errorf("target_id %q not found in input", aj.TargetID)
		}
		// 高风险 candidate 校验
		if isHighRiskCandidate(aj.TargetID, input) && !highRiskAllowedActions[actionName] {
			return ClassifyResult{}, fmt.Errorf("action %s not allowed for high risk candidate %q", aj.Action, aj.TargetID)
		}
		if err := validateSourceRefs("action evidence refs", aj.EvidenceRefs, validIDs); err != nil {
			return ClassifyResult{}, err
		}
		if actionName == ActionCreateUnit && isHighRiskCandidate(aj.TargetID, input) && !allUnitsNeedReview(result.Units) {
			return ClassifyResult{}, fmt.Errorf("high risk candidate %q create_memory_unit requires generated units status=needs_review", aj.TargetID)
		}
		action := GovernanceAction{
			ActionID:     aj.ActionID,
			RunID:        "", // 由 service 填充
			OrgID:        input.Scope.OrgID,
			ProjectID:    input.Scope.ProjectID,
			TargetType:   aj.TargetType,
			TargetID:     aj.TargetID,
			Action:       actionName,
			Reason:       sanitizeText(aj.Reason),
			EvidenceRefs: aj.EvidenceRefs,
		}
		result.Actions = append(result.Actions, action)
	}

	for _, cij := range resp.CICases {
		if cij.CaseID == "" || cij.Question == "" {
			return ClassifyResult{}, errors.New("case_id and question are required")
		}
		ciCase := CICase{
			CaseID:         cij.CaseID,
			OrgID:          input.Scope.OrgID,
			ProjectID:      input.Scope.ProjectID,
			Question:       cij.Question,
			MustInclude:    cij.MustInclude,
			MustNotInclude: cij.MustNotInclude,
			Status:         "active",
		}
		result.CICases = append(result.CICases, ciCase)
	}

	return result, nil
}

func buildValidIDSet(input ClassifyInput) map[string]bool {
	ids := map[string]bool{}
	for _, c := range input.Candidates {
		ids[c.ID] = true
	}
	for _, h := range input.HotMemories {
		ids[h.ID] = true
	}
	for _, a := range input.Archives {
		ids[a.ID] = true
	}
	for _, u := range input.ExistingUnits {
		ids[u.UnitID] = true
	}
	for _, r := range input.Retrievals {
		ids[r.RequestID] = true
		if r.SourceID != "" {
			ids[r.SourceID] = true
		}
	}
	return ids
}

func buildValidUnitIDSet(input ClassifyInput, generated []MemoryUnit) map[string]bool {
	ids := map[string]bool{}
	for _, u := range input.ExistingUnits {
		ids[u.UnitID] = true
	}
	for _, u := range generated {
		ids[u.UnitID] = true
	}
	return ids
}

func validateSourceRefs(label string, refs []SourceRef, validIDs map[string]bool) error {
	for _, ref := range refs {
		id := strings.TrimSpace(ref.ID)
		if id == "" || !validIDs[id] {
			return fmt.Errorf("%s contain id %q not found in input", label, ref.ID)
		}
	}
	return nil
}

func allUnitsNeedReview(units []MemoryUnit) bool {
	if len(units) == 0 {
		return false
	}
	for _, unit := range units {
		if unit.Status != UnitNeedsReview {
			return false
		}
	}
	return true
}

func isHighRiskCandidate(targetID string, input ClassifyInput) bool {
	for _, c := range input.Candidates {
		if c.ID == targetID && c.RiskLevel == "high" {
			return true
		}
	}
	return false
}

func sanitizeText(text string) string {
	result := secret.Sanitize(text, nil)
	return result.Text
}
