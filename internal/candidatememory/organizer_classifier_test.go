package candidatememory

import (
	"context"
	"strings"
	"testing"

	"memory-os/internal/llm"
)

// stubChatClient 桩 LLM,返回预设文本。
type stubChatClient struct {
	text string
	err  error
}

func (s stubChatClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	if s.err != nil {
		return llm.ChatResponse{}, s.err
	}
	return llm.ChatResponse{Text: s.text}, nil
}

func sampleCandidates() []Candidate {
	return []Candidate{
		{CandidateID: "c1", OrgID: "o1", ProjectID: "p1", MemoryType: MemoryTypeFact, Content: "用户偏好 Vue", RiskLevel: RiskLow, Confidence: 0.9, Status: StatusPending},
		{CandidateID: "c2", OrgID: "o1", ProjectID: "p1", MemoryType: MemoryTypeBugfix, Content: "修复了空指针", RiskLevel: RiskLow, Confidence: 0.8, Status: StatusInComposePool},
		{CandidateID: "c3", OrgID: "o1", ProjectID: "p1", MemoryType: MemoryTypeRisk, Content: "生产 API key 泄露", RiskLevel: RiskHigh, Confidence: 0.7, Status: StatusPending},
	}
}

func TestOrganizer_ParseLegalActions(t *testing.T) {
	resp := `{"decisions":[{"candidate_id":"c1","action":"keep_candidate","scope":"project","confidence":0.9,"reason":"稳定偏好"},{"candidate_id":"c2","action":"archive_material","scope":"project","confidence":0.8,"reason":"bugfix"},{"candidate_id":"c3","action":"needs_review","scope":"project","confidence":0.6,"reason":"高风险"}],"summary":"整理完成"}`
	o := NewLLMOrganizer(stubChatClient{text: resp})
	out, err := o.Organize(context.Background(), sampleCandidates(), nil)
	if err != nil {
		t.Fatalf("organize: %v", err)
	}
	if len(out.Decisions) != 3 {
		t.Fatalf("want 3 decisions, got %d", len(out.Decisions))
	}
	if out.Decisions[2].Action != OrganizerActionNeedsReview {
		t.Error("high risk must be needs_review")
	}
	if out.Summary == "" {
		t.Error("summary should not be empty")
	}
}

func TestOrganizer_HighRiskRejectsAutoActions(t *testing.T) {
	// LLM 错误地对高风险候选返回 promote_hot,决策器必须降级为 needs_review。
	resp := `{"decisions":[{"candidate_id":"c3","action":"promote_hot","scope":"global","confidence":0.9,"reason":"提升"}]}`
	o := NewLLMOrganizer(stubChatClient{text: resp})
	out, err := o.Organize(context.Background(), sampleCandidates(), nil)
	if err != nil {
		t.Fatalf("organize: %v", err)
	}
	if out.Decisions[0].Action != OrganizerActionNeedsReview {
		t.Fatalf("high risk promote_hot must be downgraded to needs_review, got %s", out.Decisions[0].Action)
	}
}

func TestOrganizer_HallucinatedCandidateIDRejected(t *testing.T) {
	resp := `{"decisions":[{"candidate_id":"cX-not-exist","action":"discard_noise","scope":"project","confidence":0.9,"reason":"噪声"}]}`
	o := NewLLMOrganizer(stubChatClient{text: resp})
	_, err := o.Organize(context.Background(), sampleCandidates(), nil)
	if err == nil {
		t.Fatal("hallucinated candidate_id must be rejected")
	}
	if !strings.Contains(err.Error(), "candidate_id") {
		t.Fatalf("error should mention candidate_id, got %v", err)
	}
}

func TestOrganizer_DuplicateOfCarriesTarget(t *testing.T) {
	resp := `{"decisions":[{"candidate_id":"c1","action":"duplicate_of","scope":"project","confidence":0.9,"reason":"重复","merge_target":"c2"}]}`
	o := NewLLMOrganizer(stubChatClient{text: resp})
	out, err := o.Organize(context.Background(), sampleCandidates(), nil)
	if err != nil {
		t.Fatalf("organize: %v", err)
	}
	if out.Decisions[0].Action != OrganizerActionDuplicateOf {
		t.Fatal("want duplicate_of")
	}
	if out.Decisions[0].MergeTarget != "c2" {
		t.Fatalf("merge target = %s, want c2", out.Decisions[0].MergeTarget)
	}
}

func TestOrganizer_BadJSONZeroWrite(t *testing.T) {
	o := NewLLMOrganizer(stubChatClient{text: "not json"})
	_, err := o.Organize(context.Background(), sampleCandidates(), nil)
	if err == nil {
		t.Fatal("bad json must error, zero write")
	}
}

func TestOrganizer_EmptyInputErrors(t *testing.T) {
	o := NewLLMOrganizer(stubChatClient{})
	if _, err := o.Organize(context.Background(), nil, nil); err == nil {
		t.Fatal("empty input must error")
	}
}
