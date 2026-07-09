package memorykernel

import (
	"context"
	"strings"
	"testing"
)

func TestLLMClassifierRejectsUnitSourceRefsOutsideInput(t *testing.T) {
	resp := `{
	  "units":[{"unit_id":"unit_1","type":"fact","content":"ok","status":"current","source_refs":[{"kind":"candidate","id":"cand_missing"}]}],
	  "claims":[],
	  "actions":[],
	  "ci_cases":[],
	  "summary":"x"
	}`
	classifier := NewLLMClassifier(stubChatClient{text: resp})
	_, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope:      Scope{OrgID: "org_1", ProjectID: "project_1"},
		Candidates: []CandidateInput{{ID: "cand_1", RiskLevel: "low"}},
	})
	if err == nil || !strings.Contains(err.Error(), "source refs") {
		t.Fatalf("Classify() error = %v, want source ref rejection", err)
	}
}

func TestLLMClassifierRejectsClaimEvidenceRefsOutsideInput(t *testing.T) {
	resp := `{
	  "units":[{"unit_id":"unit_1","type":"fact","content":"ok","status":"current"}],
	  "claims":[{"claim_id":"claim_1","unit_id":"unit_1","subject":"memory_archive","predicate":"implementation_status","value":"implemented","evidence_refs":[{"kind":"candidate","id":"cand_missing"}]}],
	  "actions":[],
	  "ci_cases":[],
	  "summary":"x"
	}`
	classifier := NewLLMClassifier(stubChatClient{text: resp})
	_, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope:      Scope{OrgID: "org_1", ProjectID: "project_1"},
		Candidates: []CandidateInput{{ID: "cand_1", RiskLevel: "low"}},
	})
	if err == nil || !strings.Contains(err.Error(), "evidence refs") {
		t.Fatalf("Classify() error = %v, want evidence ref rejection", err)
	}
}

func TestLLMClassifierRejectsActionEvidenceRefsOutsideInput(t *testing.T) {
	resp := `{
	  "units":[],
	  "claims":[],
	  "actions":[{"action_id":"act_1","target_type":"candidate","target_id":"cand_1","action":"discard_stale","reason":"x","evidence_refs":[{"kind":"candidate","id":"cand_missing"}]}],
	  "ci_cases":[],
	  "summary":"x"
	}`
	classifier := NewLLMClassifier(stubChatClient{text: resp})
	_, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope:      Scope{OrgID: "org_1", ProjectID: "project_1"},
		Candidates: []CandidateInput{{ID: "cand_1", RiskLevel: "low"}},
	})
	if err == nil || !strings.Contains(err.Error(), "evidence refs") {
		t.Fatalf("Classify() error = %v, want evidence ref rejection", err)
	}
}

func TestLLMClassifierRejectsHighRiskCreateMemoryUnitWithoutNeedsReview(t *testing.T) {
	resp := `{
	  "units":[{"unit_id":"unit_1","type":"fact","content":"new fact","status":"current"}],
	  "claims":[],
	  "actions":[{"action_id":"act_1","target_type":"candidate","target_id":"cand_secret","action":"create_memory_unit","reason":"x"}],
	  "ci_cases":[],
	  "summary":"x"
	}`
	classifier := NewLLMClassifier(stubChatClient{text: resp})
	_, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope:      Scope{OrgID: "org_1", ProjectID: "project_1"},
		Candidates: []CandidateInput{{ID: "cand_secret", RiskLevel: "high"}},
	})
	if err == nil || !strings.Contains(err.Error(), "needs_review") {
		t.Fatalf("Classify() error = %v, want needs_review rejection", err)
	}
}
