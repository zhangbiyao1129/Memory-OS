package hotmemory

import (
	"context"
	"strings"
	"testing"

	"memory-os/internal/llm"
)

type fakeHotMemoryChatClient struct {
	text string
}

func (f fakeHotMemoryChatClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Text: f.text}, nil
}

func TestLLMOrganizerParsesDecisionAndSanitizesSummary(t *testing.T) {
	organizer := NewLLMOrganizer(fakeHotMemoryChatClient{text: `{
		"demote_ids":["hm_noise"],
		"keep_ids":["hm_keep"],
		"merge_groups":[["hm_dup_a","hm_dup_b"]],
		"summary":"降权 sk-test-redacted-example 低信号热记忆"
	}`})

	result, err := organizer.Organize(context.Background(), []Memory{
		{MemoryID: "hm_noise", Fact: "噪声", Status: StatusActive},
		{MemoryID: "hm_keep", Fact: "稳定事实", Status: StatusActive},
	})
	if err != nil {
		t.Fatalf("Organize() error = %v", err)
	}
	if len(result.DemoteIDs) != 1 || result.DemoteIDs[0] != "hm_noise" {
		t.Fatalf("demote ids = %#v, want hm_noise", result.DemoteIDs)
	}
	if len(result.MergeGroups) != 1 || len(result.MergeGroups[0]) != 2 {
		t.Fatalf("merge groups = %#v, want one duplicate group", result.MergeGroups)
	}
	if strings.Contains(result.Summary, "sk-test-redacted-example") {
		t.Fatalf("summary leaked secret-like value: %s", result.Summary)
	}
}
