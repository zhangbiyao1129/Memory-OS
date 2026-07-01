package eventlog

import (
	"testing"
	"time"
)

func TestValidateAcceptsMinimalUserMessage(t *testing.T) {
	event := TurnEvent{
		Version:   "v1",
		EventID:   "event_1",
		TurnID:    "turn_1",
		ThreadID:  "thread_1",
		SessionID: "session_1",
		Type:      EventUserMessage,
		CreatedAt: time.Now().UTC(),
		Actor: Actor{
			UserID:    "user_1",
			OrgID:     "org_1",
			ProjectID: "project_1",
			AgentID:   "codex",
		},
		Payload: map[string]any{"text": "hello"},
	}

	if err := Validate(event); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsMissingRequiredFields(t *testing.T) {
	event := TurnEvent{Version: "v1", Type: EventUserMessage}

	err := Validate(event)

	if err == nil {
		t.Fatal("Validate() error = nil, want required field error")
	}
}

func TestValidateRejectsInvalidType(t *testing.T) {
	event := validEvent()
	event.Type = "unknown"

	err := Validate(event)

	if err == nil {
		t.Fatal("Validate() error = nil, want invalid type error")
	}
}

func TestValidateRejectsZeroCreatedAt(t *testing.T) {
	event := validEvent()
	event.CreatedAt = time.Time{}

	err := Validate(event)

	if err == nil {
		t.Fatal("Validate() error = nil, want created_at error")
	}
}

func validEvent() TurnEvent {
	return TurnEvent{
		Version:   "v1",
		EventID:   "event_1",
		TurnID:    "turn_1",
		ThreadID:  "thread_1",
		SessionID: "session_1",
		Type:      EventUserMessage,
		CreatedAt: time.Now().UTC(),
		Actor: Actor{
			UserID:    "user_1",
			OrgID:     "org_1",
			ProjectID: "project_1",
			AgentID:   "codex",
		},
		Payload: map[string]any{"text": "hello"},
	}
}
