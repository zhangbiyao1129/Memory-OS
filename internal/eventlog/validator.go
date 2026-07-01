package eventlog

import "errors"

var validTypes = map[EventType]bool{
	EventSessionStart:      true,
	EventUserMessage:       true,
	EventAssistantMessage:  true,
	EventAssistantFinal:    true,
	EventToolCallStarted:   true,
	EventToolCallCompleted: true,
	EventFileChanged:       true,
	EventCommandStarted:    true,
	EventCommandCompleted:  true,
	EventTurnCompleted:     true,
	EventTurnFailed:        true,
	EventCompact:           true,
	EventManualArchive:     true,
}

func Validate(event TurnEvent) error {
	if event.Version != "v1" {
		return errors.New("turn event version must be v1")
	}
	if event.EventID == "" || event.TurnID == "" || event.ThreadID == "" || event.SessionID == "" {
		return errors.New("turn event ids are required")
	}
	if !validTypes[event.Type] {
		return errors.New("turn event type is invalid")
	}
	if event.CreatedAt.IsZero() {
		return errors.New("turn event created_at is required")
	}
	if event.Actor.UserID == "" || event.Actor.OrgID == "" || event.Actor.ProjectID == "" || event.Actor.AgentID == "" {
		return errors.New("turn event actor context is required")
	}
	if event.Payload == nil {
		return errors.New("turn event payload is required")
	}
	return nil
}
