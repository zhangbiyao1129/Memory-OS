package archive

import (
	"strings"
	"testing"
	"time"

	"memory-os/internal/eventlog"
)

func TestRenderMarkdownIncludesEventsAndRefs(t *testing.T) {
	markdown, err := RenderMarkdown(RenderRequest{
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		Events: []eventlog.TurnEvent{
			{
				Version:   "v1",
				EventID:   "event_1",
				TurnID:    "turn_1",
				ThreadID:  "thread_1",
				SessionID: "session_1",
				Type:      eventlog.EventUserMessage,
				CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
				Payload:   map[string]any{"text": "deploy api"},
			},
			{
				Version:   "v1",
				EventID:   "event_2",
				TurnID:    "turn_1",
				ThreadID:  "thread_1",
				SessionID: "session_1",
				Type:      eventlog.EventToolCallCompleted,
				CreatedAt: time.Date(2026, 7, 1, 0, 1, 0, 0, time.UTC),
				Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
				Payload:   map[string]any{"tool_output": "ok"},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderMarkdown() error = %v", err)
	}

	for _, want := range []string{"# Deploy Notes", "archive_1", "event_1", "deploy api", "tool output", "ok"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

func TestRenderMarkdownDoesNotLeakFakeSecret(t *testing.T) {
	event := eventlog.TurnEvent{
		Version:   "v1",
		EventID:   "event_1",
		TurnID:    "turn_1",
		ThreadID:  "thread_1",
		SessionID: "session_1",
		Type:      eventlog.EventUserMessage,
		CreatedAt: time.Now().UTC(),
		Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Payload:   map[string]any{"text": "token sk-test-redacted-example"},
	}
	sanitized, err := eventlog.Sanitize(event, eventlog.SanitizerOptions{})
	if err != nil {
		t.Fatalf("Sanitize() error = %v", err)
	}

	markdown, err := RenderMarkdown(RenderRequest{ArchiveID: "archive_1", Title: "Safe Notes", Events: []eventlog.TurnEvent{sanitized.Event}})

	if err != nil {
		t.Fatalf("RenderMarkdown() error = %v", err)
	}
	if strings.Contains(markdown, "sk-test-redacted-example") {
		t.Fatal("markdown leaked fake secret")
	}
	if !strings.Contains(markdown, "secret_ref:") {
		t.Fatalf("markdown missing secret_ref: %s", markdown)
	}
}

func TestRenderKnowledgeMarkdownSummarizesConversationForRAG(t *testing.T) {
	markdown, err := RenderKnowledgeMarkdown(RenderRequest{
		ArchiveID: "archive_summary_1",
		Title:     "前端 API 地址修复",
		Events: []eventlog.TurnEvent{
			{
				Version:   "v1",
				EventID:   "event_user_1",
				TurnID:    "turn_1",
				ThreadID:  "thread_1",
				SessionID: "session_1",
				Type:      eventlog.EventUserMessage,
				CreatedAt: time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC),
				Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
				Payload:   map[string]any{"text": "前端页面请求 http://your-server:18081 失败，需要修复部署配置"},
			},
			{
				Version:   "v1",
				EventID:   "event_assistant_1",
				TurnID:    "turn_1",
				ThreadID:  "thread_1",
				SessionID: "session_1",
				Type:      eventlog.EventAssistantFinal,
				CreatedAt: time.Date(2026, 7, 4, 10, 5, 0, 0, time.UTC),
				Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
				Payload: map[string]any{
					"text": "根因是 docker-compose.t480.yml 把 NUXT_PUBLIC_API_BASE 默认写成 your-server。修复为默认空值，并让 useApi 按当前 hostname 拼接 :18081。验证 go test ./internal/webdeploy 和 npm run build 通过。",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderKnowledgeMarkdown() error = %v", err)
	}

	for _, want := range []string{
		"# 前端 API 地址修复",
		"## 结论",
		"## 背景",
		"## 关键事实",
		"## 操作记录",
		"## 后续事项",
		"## 来源",
		"your-server",
		"useApi",
		"go test ./internal/webdeploy",
		"event_user_1",
		"event_assistant_1",
		"thread_1",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("knowledge markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "## Timeline") {
		t.Fatalf("knowledge markdown must not be the raw timeline renderer:\n%s", markdown)
	}
}

func TestRenderKnowledgeMarkdownKeepsSourceRefsNearKnowledgeItems(t *testing.T) {
	markdown, err := RenderKnowledgeMarkdown(RenderRequest{
		ArchiveID: "archive_summary_1",
		Title:     "知识来源测试",
		Events: []eventlog.TurnEvent{
			{
				Version:   "v1",
				EventID:   "event_user_1",
				TurnID:    "turn_1",
				ThreadID:  "thread_1",
				SessionID: "session_1",
				Type:      eventlog.EventUserMessage,
				CreatedAt: time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC),
				Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
				Payload:   map[string]any{"text": strings.Repeat("前端 API 地址需要按当前 hostname 生成。", 8)},
			},
			{
				Version:   "v1",
				EventID:   "event_tool_1",
				TurnID:    "turn_1",
				ThreadID:  "thread_1",
				SessionID: "session_1",
				Type:      eventlog.EventToolCallCompleted,
				CreatedAt: time.Date(2026, 7, 4, 10, 1, 0, 0, time.UTC),
				Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
				Payload:   map[string]any{"tool_output": strings.Repeat("go test ./internal/webdeploy 通过。", 8)},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderKnowledgeMarkdown() error = %v", err)
	}

	chunks, err := ChunkMarkdown(ChunkRequest{ArchiveID: "archive_summary_1", IndexGeneration: 1, Content: markdown, MaxBytes: 220})
	if err != nil {
		t.Fatalf("ChunkMarkdown() error = %v", err)
	}
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "hostname") && !containsString(chunk.SourceEventIDs, "event_user_1") {
			t.Fatalf("hostname chunk missing user source refs: %#v\n%s", chunk.SourceEventIDs, chunk.Content)
		}
		if strings.Contains(chunk.Content, "go test") && !containsString(chunk.SourceEventIDs, "event_tool_1") {
			t.Fatalf("go test chunk missing tool source refs: %#v\n%s", chunk.SourceEventIDs, chunk.Content)
		}
	}
}
