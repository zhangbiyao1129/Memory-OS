package archive

import (
	"strings"
	"testing"
	"time"

	"memory-os/internal/eventlog"
)

func TestRenderMarkdownIncludesEventsAndRefs(t *testing.T) {
	markdown, err := RenderMarkdown(RenderRequest{
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		Events: []eventlog.TurnEvent{
			{
				Version:   "v1",
				EventID:   "event_1",
				TurnID:    "turn_1",
				ThreadID:  "thread_1",
				SessionID: "session_1",
				Type:      eventlog.EventUserMessage,
				CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
				Payload:   map[string]any{"text": "deploy api"},
			},
			{
				Version:   "v1",
				EventID:   "event_2",
				TurnID:    "turn_1",
				ThreadID:  "thread_1",
				SessionID: "session_1",
				Type:      eventlog.EventToolCallCompleted,
				CreatedAt: time.Date(2026, 7, 1, 0, 1, 0, 0, time.UTC),
				Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
				Payload:   map[string]any{"tool_output": "ok"},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderMarkdown() error = %v", err)
	}

	for _, want := range []string{"# Deploy Notes", "archive_1", "event_1", "deploy api", "tool output", "ok"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

func TestRenderMarkdownDoesNotLeakFakeSecret(t *testing.T) {
	event := eventlog.TurnEvent{
		Version:   "v1",
		EventID:   "event_1",
		TurnID:    "turn_1",
		ThreadID:  "thread_1",
		SessionID: "session_1",
		Type:      eventlog.EventUserMessage,
		CreatedAt: time.Now().UTC(),
		Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Payload:   map[string]any{"text": "token sk-test-redacted-example"},
	}
	sanitized, err := eventlog.Sanitize(event, eventlog.SanitizerOptions{})
	if err != nil {
		t.Fatalf("Sanitize() error = %v", err)
	}

	markdown, err := RenderMarkdown(RenderRequest{ArchiveID: "archive_1", Title: "Safe Notes", Events: []eventlog.TurnEvent{sanitized.Event}})

	if err != nil {
		t.Fatalf("RenderMarkdown() error = %v", err)
	}
	if strings.Contains(markdown, "sk-test-redacted-example") {
		t.Fatal("markdown leaked fake secret")
	}
	if !strings.Contains(markdown, "secret_ref:") {
		t.Fatalf("markdown missing secret_ref: %s", markdown)
	}
}
