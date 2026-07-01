package adapter

import (
	"testing"
	"time"

	"memory-os/internal/eventlog"
)

func TestCodexTranscriptToTurnEvent(t *testing.T) {
	adapter := NewCodexAdapter(Config{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"})

	event, err := adapter.FromTranscript(CodexTranscript{
		EventID:   "event_1",
		TurnID:    "turn_1",
		ThreadID:  "thread_1",
		SessionID: "session_1",
		Role:      "user",
		Text:      "please inspect repo",
		CreatedAt: time.Now().UTC(),
	})

	if err != nil {
		t.Fatalf("FromTranscript() error = %v", err)
	}
	if event.Type != eventlog.EventUserMessage {
		t.Fatalf("type = %q, want user_message", event.Type)
	}
	if event.Source.Platform != "codex" {
		t.Fatalf("platform = %q, want codex", event.Source.Platform)
	}
}
