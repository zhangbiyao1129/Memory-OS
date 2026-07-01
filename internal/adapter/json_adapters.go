package adapter

import (
	"encoding/json"
	"errors"
	"time"

	"memory-os/internal/eventlog"
)

type ClaudeCodeAdapter struct {
	sdk SDK
}

type OpenCodeAdapter struct {
	sdk SDK
}

type HermesAdapter struct {
	sdk SDK
}

func NewClaudeCodeAdapter(config Config) ClaudeCodeAdapter {
	return ClaudeCodeAdapter{sdk: NewSDK(config)}
}

func NewOpenCodeAdapter(config Config) OpenCodeAdapter {
	return OpenCodeAdapter{sdk: NewSDK(config)}
}

func NewHermesAdapter(config Config) HermesAdapter {
	return HermesAdapter{sdk: NewSDK(config)}
}

type claudePayload struct {
	SessionID string `json:"session_id"`
	ThreadID  string `json:"thread_id"`
	TurnID    string `json:"turn_id"`
	Events    []struct {
		ID        string `json:"id"`
		Role      string `json:"role"`
		Text      string `json:"text"`
		CreatedAt string `json:"created_at"`
	} `json:"events"`
}

func (a ClaudeCodeAdapter) Convert(input BatchInput) ([]eventlog.TurnEvent, error) {
	var payload claudePayload
	if err := json.Unmarshal(input.Content, &payload); err != nil {
		return nil, err
	}
	events := []eventlog.TurnEvent{}
	for _, item := range payload.Events {
		createdAt, err := parseTime(item.CreatedAt)
		if err != nil {
			return nil, err
		}
		var event eventlog.TurnEvent
		switch item.Role {
		case "user":
			event, err = a.sdk.UserMessage(item.ID, payload.TurnID, payload.ThreadID, payload.SessionID, item.Text, createdAt)
		case "assistant":
			event, err = a.sdk.AssistantFinal(item.ID, payload.TurnID, payload.ThreadID, payload.SessionID, item.Text, createdAt)
		default:
			err = errors.New("unsupported claude code role")
		}
		if err != nil {
			return nil, err
		}
		event.Source.Platform = "claude-code"
		events = append(events, event)
	}
	return events, nil
}

type openCodePayload struct {
	Session string `json:"session"`
	Thread  string `json:"thread"`
	Turn    string `json:"turn"`
	Items   []struct {
		EventID string `json:"event_id"`
		Kind    string `json:"kind"`
		Role    string `json:"role"`
		Tool    string `json:"tool"`
		Content string `json:"content"`
		Time    string `json:"time"`
	} `json:"items"`
}

func (a OpenCodeAdapter) Convert(input BatchInput) ([]eventlog.TurnEvent, error) {
	var payload openCodePayload
	if err := json.Unmarshal(input.Content, &payload); err != nil {
		return nil, err
	}
	events := []eventlog.TurnEvent{}
	for _, item := range payload.Items {
		createdAt, err := parseTime(item.Time)
		if err != nil {
			return nil, err
		}
		var event eventlog.TurnEvent
		switch {
		case item.Kind == "message" && item.Role == "user":
			event, err = a.sdk.UserMessage(item.EventID, payload.Turn, payload.Thread, payload.Session, item.Content, createdAt)
		case item.Kind == "message" && item.Role == "assistant":
			event, err = a.sdk.AssistantFinal(item.EventID, payload.Turn, payload.Thread, payload.Session, item.Content, createdAt)
		case item.Kind == "tool_result":
			platform := "opencode"
			if item.Tool != "" {
				platform = "opencode:" + item.Tool
			}
			event, err = a.sdk.ToolOutput(item.EventID, payload.Turn, payload.Thread, payload.Session, item.Content, createdAt, platform)
		default:
			err = errors.New("unsupported opencode item")
		}
		if err != nil {
			return nil, err
		}
		event.Source.Platform = "opencode"
		events = append(events, event)
	}
	return events, nil
}

type hermesPayload struct {
	ConversationID string `json:"conversation_id"`
	SessionID      string `json:"session_id"`
	TurnID         string `json:"turn_id"`
	Records        []struct {
		UUID      string `json:"uuid"`
		Type      string `json:"type"`
		Body      string `json:"body"`
		Timestamp string `json:"timestamp"`
	} `json:"records"`
}

func (a HermesAdapter) Convert(input BatchInput) ([]eventlog.TurnEvent, error) {
	var payload hermesPayload
	if err := json.Unmarshal(input.Content, &payload); err != nil {
		return nil, err
	}
	events := []eventlog.TurnEvent{}
	for _, item := range payload.Records {
		createdAt, err := parseTime(item.Timestamp)
		if err != nil {
			return nil, err
		}
		var event eventlog.TurnEvent
		switch item.Type {
		case "human":
			event, err = a.sdk.UserMessage(item.UUID, payload.TurnID, payload.ConversationID, payload.SessionID, item.Body, createdAt)
		case "assistant_final":
			event, err = a.sdk.AssistantFinal(item.UUID, payload.TurnID, payload.ConversationID, payload.SessionID, item.Body, createdAt)
		default:
			err = errors.New("unsupported hermes record")
		}
		if err != nil {
			return nil, err
		}
		event.Source.Platform = "hermes"
		events = append(events, event)
	}
	return events, nil
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Now().UTC(), nil
	}
	return time.Parse(time.RFC3339, value)
}
