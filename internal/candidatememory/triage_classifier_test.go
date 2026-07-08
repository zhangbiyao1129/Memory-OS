package candidatememory

import (
	"context"
	"strings"
	"testing"

	"memory-os/internal/llm"
)

type fakeTriageChatClient struct {
	text string
	err  error
}

func (f fakeTriageChatClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	if f.err != nil {
		return llm.ChatResponse{}, f.err
	}
	return llm.ChatResponse{Text: f.text}, nil
}

func TestParseTriageResponseAllowsJSONInsideText(t *testing.T) {
	decision, err := parseTriageResponse(`result:
{"scope":"tooling","confidence":0.91,"reason":"Codex hook setup","project_links":[{"project_id":"project_memory_os","source_key":"github.com/acme/memory-os","confidence":0.86,"evidence":"mentions Memory OS"}]}`)
	if err != nil {
		t.Fatalf("parseTriageResponse: %v", err)
	}
	if decision.Scope != TriageScopeTooling {
		t.Fatalf("scope = %s, want tooling", decision.Scope)
	}
	if len(decision.ProjectLinks) != 1 || decision.ProjectLinks[0].LinkedProjectID != "project_memory_os" {
		t.Fatalf("project links = %#v", decision.ProjectLinks)
	}
}

func TestParseTriageResponseRejectsUnknownScope(t *testing.T) {
	_, err := parseTriageResponse(`{"scope":"everything","confidence":0.91,"reason":"bad","project_links":[]}`)
	if err == nil {
		t.Fatal("parseTriageResponse error = nil, want invalid scope")
	}
}

func TestLLMTriageClassifierSanitizesReason(t *testing.T) {
	classifier := NewLLMTriageClassifier(fakeTriageChatClient{text: `{"scope":"global","confidence":0.93,"reason":"use token sk-[REDACTED]","project_links":[]}`})
	decision, err := classifier.Classify(context.Background(), TriageInput{
		Candidate: Candidate{CandidateID: "cand-1", OrgID: "org-1", ProjectID: "project-1", SourceKey: "local/a", Content: "tooling rule", MemoryType: MemoryTypeFact, Confidence: 0.9},
		Projects:  []TriageProject{{ProjectID: "project-1", Name: "Memory OS", SourceKey: "github.com/acme/memory-os"}},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if strings.Contains(decision.Reason, "abcdef") {
		t.Fatalf("reason was not sanitized: %q", decision.Reason)
	}
}

func TestRuleTriageClassifierClassifiesLocalToolingConservatively(t *testing.T) {
	decision, err := RuleTriageClassifier{}.Classify(context.Background(), TriageInput{
		Candidate: Candidate{SourceKey: "local/tmp", Content: "Codex MCP 配置需要复用", RiskLevel: RiskLow, MemoryType: MemoryTypeFact},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if decision.Scope != TriageScopeTooling || decision.Confidence >= 0.82 {
		t.Fatalf("decision = %#v, want tooling below auto-promotion threshold", decision)
	}
}
