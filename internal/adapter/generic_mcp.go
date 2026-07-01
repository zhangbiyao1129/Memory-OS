package adapter

import (
	"errors"
	"time"

	"memory-os/internal/eventlog"
)

type GenericMCPAdapter struct {
	sdk SDK
}

type GenericMCPInput struct {
	EventID   string
	TurnID    string
	ThreadID  string
	SessionID string
	Kind      string
	Text      string
	CreatedAt time.Time
}

func NewGenericMCPAdapter(config Config) GenericMCPAdapter {
	return GenericMCPAdapter{sdk: NewSDK(config)}
}

func (a GenericMCPAdapter) FromInput(input GenericMCPInput) (eventlog.TurnEvent, error) {
	switch input.Kind {
	case "user_message":
		return a.sdk.UserMessage(input.EventID, input.TurnID, input.ThreadID, input.SessionID, input.Text, input.CreatedAt)
	case "assistant_final":
		return a.sdk.AssistantFinal(input.EventID, input.TurnID, input.ThreadID, input.SessionID, input.Text, input.CreatedAt)
	case "tool_output":
		return a.sdk.ToolOutput(input.EventID, input.TurnID, input.ThreadID, input.SessionID, input.Text, input.CreatedAt, "generic-mcp")
	default:
		return eventlog.TurnEvent{}, errors.New("unsupported generic mcp input kind")
	}
}
