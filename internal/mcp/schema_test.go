package mcp

import "testing"

func TestToolsContainRequiredMemoryTools(t *testing.T) {
	tools := Tools()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}

	required := []string{
		"memory_search",
		"memory_archive",
		"memory_append_event",
		"memory_get_archive",
		"memory_mark_used",
		"memory_stats",
	}
	for _, name := range required {
		if !names[name] {
			t.Fatalf("missing MCP tool %q", name)
		}
	}
}

func TestHandleToolRunsMemorySearch(t *testing.T) {
	response := HandleTool("memory_search", map[string]any{"query": "hello"})

	if response.Error != "" {
		t.Fatalf("response error = %q, want empty", response.Error)
	}
	if response.Code != "ok" {
		t.Fatalf("code = %q, want ok", response.Code)
	}
}

func TestHandleToolRejectsUnknownTool(t *testing.T) {
	response := HandleTool("unknown", nil)

	if response.Code != "unknown_tool" {
		t.Fatalf("code = %q, want unknown_tool", response.Code)
	}
}
