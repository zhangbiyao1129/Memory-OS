package adapter

import (
	"strings"
	"testing"
	"time"

	"memory-os/internal/eventlog"
)

func TestSDKBuildUserMessageSanitizesSecret(t *testing.T) {
	sdk := NewSDK(Config{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"})

	event, err := sdk.UserMessage("event_1", "turn_1", "thread_1", "session_1", "hello sk-test-redacted-example", time.Now().UTC())

	if err != nil {
		t.Fatalf("UserMessage() error = %v", err)
	}
	if event.Type != eventlog.EventUserMessage {
		t.Fatalf("type = %q, want user_message", event.Type)
	}
	if strings.Contains(event.Payload["text"].(string), "sk-test-redacted-example") {
		t.Fatal("adapter event leaked secret")
	}
}
