package adapter

import (
	"errors"
	"time"

	"memory-os/internal/eventlog"
)

type CodexAdapter struct {
	sdk SDK
}

type CodexTranscript struct {
	EventID   string
	TurnID    string
	ThreadID  string
	SessionID string
	Role      string
	Text      string
	CreatedAt time.Time
}

func NewCodexAdapter(config Config) CodexAdapter {
	return CodexAdapter{sdk: NewSDK(config)}
}

func (a CodexAdapter) FromTranscript(input CodexTranscript) (eventlog.TurnEvent, error) {
	switch input.Role {
	case "user":
		event, err := a.sdk.UserMessage(input.EventID, input.TurnID, input.ThreadID, input.SessionID, input.Text, input.CreatedAt)
		event.Source.Platform = "codex"
		return event, err
	case "assistant":
		event, err := a.sdk.AssistantFinal(input.EventID, input.TurnID, input.ThreadID, input.SessionID, input.Text, input.CreatedAt)
		event.Source.Platform = "codex"
		return event, err
	default:
		return eventlog.TurnEvent{}, errors.New("unsupported codex transcript role")
	}
}
