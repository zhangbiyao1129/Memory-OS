package adapter

import (
	"fmt"
	"strings"
	"time"

	"memory-os/internal/eventlog"
)

type TranscriptImporter struct {
	sdk SDK
}

func NewTranscriptImporter(config Config) TranscriptImporter {
	return TranscriptImporter{sdk: NewSDK(config)}
}

func (a TranscriptImporter) Convert(input BatchInput) ([]eventlog.TurnEvent, error) {
	lines := strings.Split(string(input.Content), "\n")
	events := []eventlog.TurnEvent{}
	turnID := "transcript_turn_1"
	threadID := "transcript_thread_1"
	sessionID := "transcript_session_1"
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		index := len(events) + 1
		eventID := fmt.Sprintf("transcript_event_%d", index)
		createdAt := time.Date(2026, 7, 1, 0, 0, index, 0, time.UTC)
		var event eventlog.TurnEvent
		var err error
		switch {
		case strings.HasPrefix(trimmed, "User:"):
			event, err = a.sdk.UserMessage(eventID, turnID, threadID, sessionID, strings.TrimSpace(strings.TrimPrefix(trimmed, "User:")), createdAt)
		case strings.HasPrefix(trimmed, "Assistant:"):
			event, err = a.sdk.AssistantFinal(eventID, turnID, threadID, sessionID, strings.TrimSpace(strings.TrimPrefix(trimmed, "Assistant:")), createdAt)
		case strings.HasPrefix(trimmed, "Tool:"):
			event, err = a.sdk.ToolOutput(eventID, turnID, threadID, sessionID, strings.TrimSpace(strings.TrimPrefix(trimmed, "Tool:")), createdAt, "transcript")
		default:
			event, err = a.sdk.UserMessage(eventID, turnID, threadID, sessionID, trimmed, createdAt)
		}
		if err != nil {
			return nil, err
		}
		event.Source.Platform = "transcript"
		events = append(events, event)
	}
	return events, nil
}
