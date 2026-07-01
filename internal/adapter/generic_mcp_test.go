package adapter

import (
	"testing"
	"time"

	"memory-os/internal/eventlog"
)

func TestGenericMCPToolOutputToTurnEvent(t *testing.T) {
	adapter := NewGenericMCPAdapter(Config{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "generic-mcp"})

	event, err := adapter.FromInput(GenericMCPInput{
		EventID:   "event_1",
		TurnID:    "turn_1",
		ThreadID:  "thread_1",
		SessionID: "session_1",
		Kind:      "tool_output",
		Text:      "tool result",
		CreatedAt: time.Now().UTC(),
	})

	if err != nil {
		t.Fatalf("FromInput() error = %v", err)
	}
	if event.Type != eventlog.EventToolCallCompleted {
		t.Fatalf("type = %q, want tool_call_completed", event.Type)
	}
	if event.Payload["tool_output"] != "tool result" {
		t.Fatalf("tool output = %v", event.Payload["tool_output"])
	}
}
