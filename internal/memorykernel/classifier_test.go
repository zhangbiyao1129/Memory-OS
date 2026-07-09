package memorykernel

import (
	"context"
	"strings"
	"testing"

	"memory-os/internal/llm"
)

type stubChatClient struct {
	text string
}

func (s stubChatClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Text: s.text}, nil
}

func TestLLMClassifierParsesUnitsClaimsActionsAndSanitizes(t *testing.T) {
	resp := `{
	  "units":[{"unit_id":"unit_1","type":"fact","content":"memory_archive 已实现并部署","applies_when":"询问 MCP 当前能力","agent_should":"回答当前能力并引用部署证据","status":"current","confidence":0.95,"trust_score":0.9,"risk_level":"low","source_refs":[{"kind":"candidate","id":"cand_new"}]}],
	  "claims":[{"claim_id":"claim_1","unit_id":"unit_1","subject":"memory_archive","predicate":"implementation_status","value":"implemented_and_deployed","polarity":"positive","confidence":0.95,"evidence_refs":[{"kind":"candidate","id":"cand_new"}]}],
	  "actions":[{"action_id":"act_1","target_type":"candidate","target_id":"cand_old","action":"discard_stale","reason":"旧事实被 cand_new 覆盖 sk-test-redacted-example","evidence_refs":[{"kind":"candidate","id":"cand_new"}]}],
	  "ci_cases":[{"case_id":"ci_1","question":"memory_archive 现在实现了吗？","must_include":["已实现","已部署"],"must_not_include":["尚未实现"]}],
	  "summary":"修复旧事实 sk-test-redacted-example"
	}`
	classifier := NewLLMClassifier(stubChatClient{text: resp}).WithModel("test-model")
	result, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope:      Scope{OrgID: "org_1", ProjectID: "project_1"},
		Candidates: []CandidateInput{{ID: "cand_old", RiskLevel: "low"}, {ID: "cand_new", RiskLevel: "low"}},
	})
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if result.Units[0].Content != "memory_archive 已实现并部署" {
		t.Fatalf("unit content = %q", result.Units[0].Content)
	}
	if strings.Contains(result.Actions[0].Reason, "sk-test") || strings.Contains(result.Summary, "sk-test") {
		t.Fatalf("classifier leaked secret-shaped text: %#v", result)
	}
}

func TestLLMClassifierRejectsAutoDiscardForHighRiskCandidate(t *testing.T) {
	resp := `{"units":[],"claims":[],"actions":[{"action_id":"act_1","target_type":"candidate","target_id":"cand_secret","action":"discard_stale","reason":"丢弃"}],"ci_cases":[],"summary":"x"}`
	classifier := NewLLMClassifier(stubChatClient{text: resp})
	_, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope:      Scope{OrgID: "org_1", ProjectID: "project_1"},
		Candidates: []CandidateInput{{ID: "cand_secret", RiskLevel: "high"}},
	})
	if err == nil || !strings.Contains(err.Error(), "high risk") {
		t.Fatalf("Classify() error = %v, want high risk rejection", err)
	}
}

func TestLLMClassifierRejectsUnknownAction(t *testing.T) {
	resp := `{"units":[],"claims":[],"actions":[{"action_id":"act_1","target_type":"candidate","target_id":"cand_old","action":"delete_permanently","reason":"删除"}],"ci_cases":[],"summary":"x"}`
	classifier := NewLLMClassifier(stubChatClient{text: resp})
	_, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope:      Scope{OrgID: "org_1", ProjectID: "project_1"},
		Candidates: []CandidateInput{{ID: "cand_old", RiskLevel: "low"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("Classify() error = %v, want unknown action rejection", err)
	}
}

func TestLLMClassifierRejectsInvalidTargetID(t *testing.T) {
	resp := `{"units":[],"claims":[],"actions":[{"action_id":"act_1","target_type":"candidate","target_id":"nonexistent","action":"discard_stale","reason":"x"}],"ci_cases":[],"summary":"x"}`
	classifier := NewLLMClassifier(stubChatClient{text: resp})
	_, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope:      Scope{OrgID: "org_1", ProjectID: "project_1"},
		Candidates: []CandidateInput{{ID: "cand_old", RiskLevel: "low"}},
	})
	if err == nil || !strings.Contains(err.Error(), "not found in input") {
		t.Fatalf("Classify() error = %v, want invalid target rejection", err)
	}
}

func TestLLMClassifierRejectsEmptyUnitContent(t *testing.T) {
	resp := `{"units":[{"unit_id":"u1","type":"fact","content":"","status":"current"}],"claims":[],"actions":[],"ci_cases":[],"summary":"x"}`
	classifier := NewLLMClassifier(stubChatClient{text: resp})
	_, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope: Scope{OrgID: "org_1", ProjectID: "project_1"},
	})
	if err == nil || !strings.Contains(err.Error(), "content") {
		t.Fatalf("Classify() error = %v, want empty content rejection", err)
	}
}
