package adapter

import (
	"time"

	"memory-os/internal/eventlog"
	"memory-os/internal/secret"
)

type Config struct {
	UserID    string
	OrgID     string
	ProjectID string
	AgentID   string
}

type SDK struct {
	config Config
}

func NewSDK(config Config) SDK {
	return SDK{config: config}
}

func (s SDK) UserMessage(eventID, turnID, threadID, sessionID, text string, createdAt time.Time) (eventlog.TurnEvent, error) {
	return s.textEvent(eventID, turnID, threadID, sessionID, eventlog.EventUserMessage, "text", text, createdAt, "adapter-sdk")
}

func (s SDK) AssistantFinal(eventID, turnID, threadID, sessionID, text string, createdAt time.Time) (eventlog.TurnEvent, error) {
	return s.textEvent(eventID, turnID, threadID, sessionID, eventlog.EventAssistantFinal, "text", text, createdAt, "adapter-sdk")
}

func (s SDK) ToolOutput(eventID, turnID, threadID, sessionID, text string, createdAt time.Time, platform string) (eventlog.TurnEvent, error) {
	return s.textEvent(eventID, turnID, threadID, sessionID, eventlog.EventToolCallCompleted, "tool_output", text, createdAt, platform)
}

func (s SDK) textEvent(eventID, turnID, threadID, sessionID string, eventType eventlog.EventType, payloadKey, text string, createdAt time.Time, platform string) (eventlog.TurnEvent, error) {
	sanitized := secret.Sanitize(text, func(index int, match string) string { return "secret_ref_adapter" })
	event := eventlog.TurnEvent{
		Version:   "v1",
		EventID:   eventID,
		TurnID:    turnID,
		ThreadID:  threadID,
		SessionID: sessionID,
		Type:      eventType,
		CreatedAt: createdAt,
		Actor: eventlog.Actor{
			UserID:    s.config.UserID,
			OrgID:     s.config.OrgID,
			ProjectID: s.config.ProjectID,
			AgentID:   s.config.AgentID,
		},
		Source:  eventlog.Source{Platform: platform},
		Payload: map[string]any{payloadKey: sanitized.Text},
	}
	return event, eventlog.Validate(event)
}
