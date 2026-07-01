package main

import (
	"strings"
	"testing"
)

func TestRunDryRunTranscriptOutputsTurnEvent(t *testing.T) {
	out, err := run([]string{"--adapter", "transcript", "--dry-run", "--input", "../../internal/adapter/fixtures/transcript_sample.md"})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(out, `"version":"v1"`) || !strings.Contains(out, `"source":{"platform":"transcript"`) {
		t.Fatalf("dry-run output missing TurnEvent v1: %s", out)
	}
	if strings.Contains(out, "sk-test-redacted-example") {
		t.Fatalf("dry-run leaked fake secret: %s", out)
	}
}
