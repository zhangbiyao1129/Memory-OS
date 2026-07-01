package eventlog

import (
	"strings"
	"testing"
)

func TestSanitizeReplacesFakeSecret(t *testing.T) {
	event := validEvent()
	event.Payload = map[string]any{"text": "token sk-test-redacted-example"}

	result, err := Sanitize(event, SanitizerOptions{MaxTurnEventBytes: 256 * 1024, MaxToolOutputBytes: 64 * 1024})

	if err != nil {
		t.Fatalf("Sanitize() error = %v", err)
	}
	if strings.Contains(string(result.SafePayload), "sk-test-redacted-example") {
		t.Fatal("safe payload leaked fake secret")
	}
	if !strings.Contains(string(result.SafePayload), "secret_ref:") {
		t.Fatalf("safe payload missing secret_ref: %s", result.SafePayload)
	}
}

func TestSanitizeTruncatesToolOutputAndKeepsHash(t *testing.T) {
	event := validEvent()
	event.Type = EventToolCallCompleted
	event.Payload = map[string]any{"tool_output": strings.Repeat("x", 128)}

	result, err := Sanitize(event, SanitizerOptions{MaxTurnEventBytes: 1024, MaxToolOutputBytes: 16})

	if err != nil {
		t.Fatalf("Sanitize() error = %v", err)
	}
	if !result.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if result.OriginalBytes == 0 || result.SafeBytes == 0 {
		t.Fatalf("original bytes = %d safe bytes = %d, want both recorded", result.OriginalBytes, result.SafeBytes)
	}
	if result.PayloadHash == "" {
		t.Fatal("payload hash is empty")
	}
	if strings.Contains(string(result.SafePayload), strings.Repeat("x", 64)) {
		t.Fatal("safe payload contains untruncated output")
	}
}

func TestSanitizeRejectsOversizedTurnEvent(t *testing.T) {
	event := validEvent()
	event.Payload = map[string]any{"text": strings.Repeat("x", 2048)}

	_, err := Sanitize(event, SanitizerOptions{MaxTurnEventBytes: 128, MaxToolOutputBytes: 64})

	if err == nil {
		t.Fatal("Sanitize() error = nil, want oversized event rejection")
	}
}
