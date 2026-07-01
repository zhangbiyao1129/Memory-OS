package adapter

import (
	"time"

	"memory-os/internal/eventlog"
)

type BatchInput struct {
	SourceName string
	Content    []byte
}

type BatchAdapter interface {
	Convert(input BatchInput) ([]eventlog.TurnEvent, error)
}

func (a CodexAdapter) Convert(input BatchInput) ([]eventlog.TurnEvent, error) {
	event, err := a.FromTranscript(CodexTranscript{
		EventID:   "codex_event_1",
		TurnID:    "codex_turn_1",
		ThreadID:  "codex_thread_1",
		SessionID: "codex_session_1",
		Role:      "user",
		Text:      string(input.Content),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return []eventlog.TurnEvent{event}, nil
}

func (a GenericMCPAdapter) Convert(input BatchInput) ([]eventlog.TurnEvent, error) {
	event, err := a.FromInput(GenericMCPInput{
		EventID:   "generic_mcp_event_1",
		TurnID:    "generic_mcp_turn_1",
		ThreadID:  "generic_mcp_thread_1",
		SessionID: "generic_mcp_session_1",
		Kind:      "user_message",
		Text:      string(input.Content),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	event.Source.Platform = "generic-mcp"
	return []eventlog.TurnEvent{event}, nil
}
