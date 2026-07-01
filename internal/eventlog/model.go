package eventlog

import "time"

type EventType string

const (
	EventSessionStart      EventType = "session_start"
	EventUserMessage       EventType = "user_message"
	EventAssistantMessage  EventType = "assistant_message"
	EventAssistantFinal    EventType = "assistant_final"
	EventToolCallStarted   EventType = "tool_call_started"
	EventToolCallCompleted EventType = "tool_call_completed"
	EventFileChanged       EventType = "file_changed"
	EventCommandStarted    EventType = "command_started"
	EventCommandCompleted  EventType = "command_completed"
	EventTurnCompleted     EventType = "turn_completed"
	EventTurnFailed        EventType = "turn_failed"
	EventCompact           EventType = "compact"
	EventManualArchive     EventType = "manual_archive_request"
)

type Actor struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
}

type Source struct {
	Platform string `json:"platform"`
	Host     string `json:"host,omitempty"`
}

type TurnEvent struct {
	Version   string         `json:"version"`
	EventID   string         `json:"event_id"`
	TurnID    string         `json:"turn_id"`
	ThreadID  string         `json:"thread_id"`
	SessionID string         `json:"session_id"`
	Type      EventType      `json:"type"`
	CreatedAt time.Time      `json:"created_at"`
	Actor     Actor          `json:"actor"`
	Source    Source         `json:"source,omitempty"`
	Payload   map[string]any `json:"payload"`
	Warnings  []string       `json:"warnings,omitempty"`
}
