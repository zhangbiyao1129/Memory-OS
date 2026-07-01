package mcp

// Tool 描述 Phase 1 暴露的 MCP tool schema 骨架。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type ToolResponse struct {
	Code  string `json:"code"`
	Error string `json:"error,omitempty"`
}

func Tools() []Tool {
	return []Tool{
		{Name: "memory_search", Description: "Search unified Memory OS memories", InputSchema: objectSchema()},
		{Name: "memory_archive", Description: "Archive current memory context", InputSchema: objectSchema()},
		{Name: "memory_append_event", Description: "Append a TurnEvent v1", InputSchema: objectSchema()},
		{Name: "memory_get_archive", Description: "Get a Markdown archive", InputSchema: objectSchema()},
		{Name: "memory_mark_used", Description: "Mark memory result as used", InputSchema: objectSchema()},
		{Name: "memory_stats", Description: "Get Memory OS statistics", InputSchema: objectSchema()},
	}
}

func HandleTool(name string, args map[string]any) ToolResponse {
	for _, tool := range Tools() {
		if tool.Name == name {
			if name == "memory_search" {
				query, _ := args["query"].(string)
				if query == "" {
					return ToolResponse{Code: "invalid_request", Error: "query is required"}
				}
				return ToolResponse{Code: "ok"}
			}
			return ToolResponse{Code: "not_implemented", Error: "MCP tool is not implemented in phase 1"}
		}
	}
	return ToolResponse{Code: "unknown_tool", Error: "unknown MCP tool"}
}

func objectSchema() map[string]any {
	return map[string]any{"type": "object"}
}
