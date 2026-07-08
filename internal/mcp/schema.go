package mcp

import (
	"fmt"
	"strings"

	"memory-os/internal/hotmemory"
	"memory-os/internal/retrieval"
)

// Tool 描述 Phase 1 暴露的 MCP tool schema 骨架。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type ToolResponse struct {
	Code   string                    `json:"code"`
	Error  string                    `json:"error,omitempty"`
	Search *retrieval.SearchResponse `json:"search,omitempty"`
	Result any                       `json:"result,omitempty"`
}

type HandlerOptions struct {
	Retrieval retrieval.Service
	HotMemory hotmemory.Service
}

type Handler struct {
	retrieval retrieval.Service
	hotMemory hotmemory.Service
}

func NewHandler(options HandlerOptions) Handler {
	return Handler{retrieval: options.Retrieval, hotMemory: options.HotMemory}
}

func Tools() []Tool {
	return []Tool{
		{Name: "memory_search", Description: "Search unified Memory OS memories", InputSchema: memorySearchSchema()},
		{Name: "memory_archive", Description: "Archive current memory context", InputSchema: memoryArchiveSchema()},
		{Name: "memory_append_event", Description: "Append a TurnEvent v1", InputSchema: memoryAppendEventSchema()},
		{Name: "memory_get_archive", Description: "Get a Markdown archive", InputSchema: memoryGetArchiveSchema()},
		{Name: "memory_mark_used", Description: "Mark memory result as used", InputSchema: objectSchema()},
		{Name: "memory_stats", Description: "Get Memory OS statistics", InputSchema: memoryStatsSchema()},
	}
}

func HandleTool(name string, args map[string]any) ToolResponse {
	return NewHandler(HandlerOptions{}).HandleTool(name, args)
}

func (h Handler) HandleTool(name string, args map[string]any) ToolResponse {
	for _, tool := range Tools() {
		if tool.Name == name {
			if name == "memory_search" {
				if !h.retrieval.Configured() {
					return ToolResponse{Code: "retrieval_not_configured", Error: "retrieval service is not configured"}
				}
				request, err := memorySearchRequest(args)
				if err != nil {
					return ToolResponse{Code: "invalid_request", Error: err.Error()}
				}
				response, err := h.retrieval.Search(request)
				if err != nil {
					return ToolResponse{Code: "memory_search_rejected", Error: err.Error()}
				}
				return ToolResponse{Code: "ok", Search: &response}
			}
			if name == "memory_mark_used" {
				if !h.hotMemory.Configured() {
					return ToolResponse{Code: "hot_memory_not_configured", Error: "hot memory service is not configured"}
				}
				memoryID, err := stringArg(args["memory_id"])
				if err != nil {
					return ToolResponse{Code: "invalid_request", Error: err.Error()}
				}
				updated, err := h.hotMemory.MarkUsed(strings.TrimSpace(memoryID))
				if err != nil {
					return ToolResponse{Code: "hot_memory_mark_used_rejected", Error: err.Error()}
				}
				return ToolResponse{Code: "ok", Result: updated}
			}
			return ToolResponse{Code: "not_implemented", Error: "MCP tool is not implemented in phase 1"}
		}
	}
	return ToolResponse{Code: "unknown_tool", Error: "unknown MCP tool"}
}

func objectSchema() map[string]any {
	return map[string]any{"type": "object"}
}

func memoryStatsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id":     map[string]any{"type": "string"},
			"project_id": map[string]any{"type": "string"},
		},
		"additionalProperties": true,
	}
}

func memoryGetArchiveSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"archive_id": map[string]any{"type": "string"},
		},
		"required":             []any{"archive_id"},
		"additionalProperties": true,
	}
}

func memoryArchiveSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"request_id": map[string]any{"type": "string"},
			"archive_id": map[string]any{"type": "string"},
			"title":      map[string]any{"type": "string"},
			"content":    map[string]any{"type": "string"},
			"workspace":  workspaceSchema(),
			"actor":      actorSchema(),
			"created_at": map[string]any{"type": "string", "format": "date-time"},
		},
		"required":             []any{"request_id", "title", "content"},
		"additionalProperties": true,
	}
}

func memorySearchSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"request_id": map[string]any{"type": "string"},
			"query":      map[string]any{"type": "string"},
			"limit":      map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
			"workspace":  workspaceSchema(),
			"actor":      actorSchema(),
			"scope": map[string]any{
				"type": "string",
				"enum": []any{"project", "user", "org"},
			},
			"visibility": map[string]any{
				"type": "string",
				"enum": []any{"project", "private", "org"},
			},
			"archive_index_generation": map[string]any{"type": "integer", "minimum": 0},
			"max_context_bytes":        map[string]any{"type": "integer", "minimum": 1},
		},
		"required":             []any{"query"},
		"additionalProperties": true,
	}
}

func memoryAppendEventSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"request_id": map[string]any{"type": "string"},
			"workspace":  workspaceSchema(),
			"event": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"version":    map[string]any{"type": "string"},
					"event_id":   map[string]any{"type": "string"},
					"turn_id":    map[string]any{"type": "string"},
					"thread_id":  map[string]any{"type": "string"},
					"session_id": map[string]any{"type": "string"},
					"type":       map[string]any{"type": "string"},
					"created_at": map[string]any{"type": "string", "format": "date-time"},
					"actor":      actorSchema(),
					"source": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"platform": map[string]any{"type": "string"},
							"host":     map[string]any{"type": "string"},
						},
						"additionalProperties": true,
					},
					"payload": map[string]any{"type": "object", "additionalProperties": true},
				},
				"required":             []any{"version", "event_id", "turn_id", "type", "created_at", "actor", "payload"},
				"additionalProperties": true,
			},
		},
		"required":             []any{"event"},
		"additionalProperties": true,
	}
}

func workspaceSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cwd":        map[string]any{"type": "string"},
			"git_root":   map[string]any{"type": "string"},
			"git_remote": map[string]any{"type": "string"},
			"git_branch": map[string]any{"type": "string"},
			"git_commit": map[string]any{"type": "string"},
		},
		"additionalProperties": true,
	}
}

func actorSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user_id":    map[string]any{"type": "string"},
			"org_id":     map[string]any{"type": "string"},
			"project_id": map[string]any{"type": "string"},
			"agent_id":   map[string]any{"type": "string"},
		},
		"additionalProperties": true,
	}
}

func memorySearchRequest(args map[string]any) (retrieval.SearchRequest, error) {
	if args == nil {
		return retrieval.SearchRequest{}, fmt.Errorf("arguments are required")
	}
	query, _ := args["query"].(string)
	if query == "" {
		return retrieval.SearchRequest{}, fmt.Errorf("query is required")
	}
	actor, err := actorFromArgs(args["actor"])
	if err != nil {
		return retrieval.SearchRequest{}, err
	}
	labels, err := stringSlice(args["permission_labels"])
	if err != nil {
		return retrieval.SearchRequest{}, fmt.Errorf("permission_labels must be an array of strings")
	}
	scope := hotmemory.Scope(stringValue(args["scope"]))
	if scope == "" {
		scope = hotmemory.ScopeProject
	}
	visibility := stringValue(args["visibility"])
	if visibility == "" {
		visibility = "project"
	}
	return retrieval.SearchRequest{
		RequestID:              stringValue(args["request_id"]),
		Query:                  query,
		Actor:                  actor,
		Scope:                  scope,
		Visibility:             visibility,
		PermissionLabels:       labels,
		ArchiveIndexGeneration: intValue(args["archive_index_generation"]),
		MaxContextBytes:        intValue(args["max_context_bytes"]),
	}, nil
}

func actorFromArgs(value any) (retrieval.Actor, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return retrieval.Actor{}, fmt.Errorf("actor is required")
	}
	return retrieval.Actor{
		UserID:    stringValue(raw["user_id"]),
		OrgID:     stringValue(raw["org_id"]),
		ProjectID: stringValue(raw["project_id"]),
		AgentID:   stringValue(raw["agent_id"]),
	}, nil
}

func stringArg(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("memory_id is required")
	}
	return text, nil
}

func stringValue(value any) string {
	raw, _ := value.(string)
	return raw
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func stringSlice(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("non-string permission label")
			}
			values = append(values, text)
		}
		return values, nil
	default:
		return nil, fmt.Errorf("permission labels must be an array")
	}
}
