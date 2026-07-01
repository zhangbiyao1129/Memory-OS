package adapter

import (
	"os"
	"strings"
	"testing"

	"memory-os/internal/eventlog"
)

func TestAdaptersImplementBatchInterface(t *testing.T) {
	var _ BatchAdapter = NewCodexAdapter(testConfig("codex"))
	var _ BatchAdapter = NewGenericMCPAdapter(testConfig("generic-mcp"))
	var _ BatchAdapter = NewClaudeCodeAdapter(testConfig("claude-code"))
	var _ BatchAdapter = NewOpenCodeAdapter(testConfig("opencode"))
	var _ BatchAdapter = NewHermesAdapter(testConfig("hermes"))
	var _ BatchAdapter = NewTranscriptImporter(testConfig("transcript"))
}

func TestClaudeCodeAdapterFixtureOutputsValidSanitizedTurnEvents(t *testing.T) {
	events := convertFixture(t, NewClaudeCodeAdapter(testConfig("claude-code")), "fixtures/claude_code_sample.json")
	assertValidBatch(t, events, "claude-code", 2)
}

func TestOpenCodeAdapterFixtureOutputsValidSanitizedTurnEvents(t *testing.T) {
	events := convertFixture(t, NewOpenCodeAdapter(testConfig("opencode")), "fixtures/opencode_sample.json")
	assertValidBatch(t, events, "opencode", 2)
	if events[1].Type != eventlog.EventToolCallCompleted {
		t.Fatalf("second event type = %q, want tool_call_completed", events[1].Type)
	}
}

func TestHermesAdapterFixtureOutputsValidSanitizedTurnEvents(t *testing.T) {
	events := convertFixture(t, NewHermesAdapter(testConfig("hermes")), "fixtures/hermes_sample.json")
	assertValidBatch(t, events, "hermes", 2)
}

func TestTranscriptImporterOutputsValidSanitizedTurnEvents(t *testing.T) {
	events := convertFixture(t, NewTranscriptImporter(testConfig("transcript")), "fixtures/transcript_sample.md")
	assertValidBatch(t, events, "transcript", 3)
	if events[2].Type != eventlog.EventToolCallCompleted {
		t.Fatalf("third event type = %q, want tool_call_completed", events[2].Type)
	}
}

func convertFixture(t *testing.T, adapter BatchAdapter, path string) []eventlog.TurnEvent {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	events, err := adapter.Convert(BatchInput{SourceName: path, Content: content})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	return events
}

func assertValidBatch(t *testing.T, events []eventlog.TurnEvent, platform string, want int) {
	t.Helper()
	if len(events) != want {
		t.Fatalf("events len = %d, want %d", len(events), want)
	}
	for _, event := range events {
		if err := eventlog.Validate(event); err != nil {
			t.Fatalf("invalid event %#v: %v", event, err)
		}
		if event.Source.Platform != platform {
			t.Fatalf("platform = %q, want %q", event.Source.Platform, platform)
		}
		for _, value := range event.Payload {
			if text, ok := value.(string); ok && strings.Contains(text, "sk-test-redacted-example") {
				t.Fatalf("event leaked fake secret: %#v", event)
			}
		}
	}
}

func testConfig(agent string) Config {
	return Config{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: agent}
}
