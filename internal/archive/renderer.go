package archive

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"memory-os/internal/eventlog"
)

type RenderRequest struct {
	ArchiveID string
	Title     string
	Events    []eventlog.TurnEvent
}

type knowledgeItem struct {
	Text    string
	EventID string
}

func RenderMarkdown(request RenderRequest) (string, error) {
	if request.ArchiveID == "" {
		return "", errors.New("archive id is required")
	}
	if request.Title == "" {
		request.Title = "Memory Archive"
	}
	if len(request.Events) == 0 {
		return "", errors.New("archive events are required")
	}

	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(escapeMarkdown(request.Title))
	b.WriteString("\n\n")
	b.WriteString("- Archive ID: `")
	b.WriteString(request.ArchiveID)
	b.WriteString("`\n")
	b.WriteString("- Event Count: ")
	b.WriteString(fmt.Sprintf("%d", len(request.Events)))
	b.WriteString("\n\n")
	b.WriteString("## Timeline\n\n")

	for _, event := range request.Events {
		b.WriteString("### ")
		b.WriteString(string(event.Type))
		b.WriteString("\n\n")
		b.WriteString("- Source ref: `")
		b.WriteString(event.EventID)
		b.WriteString("`\n")
		b.WriteString("- Turn: `")
		b.WriteString(event.TurnID)
		b.WriteString("`\n")
		b.WriteString("- Agent: `")
		b.WriteString(event.Actor.AgentID)
		b.WriteString("`\n\n")
		b.WriteString(renderPayload(event.Payload))
		b.WriteString("\n")
	}

	return b.String(), nil
}

func RenderKnowledgeMarkdown(request RenderRequest) (string, error) {
	if request.ArchiveID == "" {
		return "", errors.New("archive id is required")
	}
	if request.Title == "" {
		request.Title = "Memory Knowledge"
	}
	if len(request.Events) == 0 {
		return "", errors.New("archive events are required")
	}

	facts := make([]knowledgeItem, 0, len(request.Events))
	operations := make([]knowledgeItem, 0, len(request.Events))
	sourceRefs := make([]string, 0, len(request.Events))
	threadIDs := map[string]bool{}
	turnIDs := map[string]bool{}
	for _, event := range request.Events {
		text := summarizeEventPayload(event)
		if text == "" {
			continue
		}
		sourceRefs = append(sourceRefs, event.EventID)
		if event.ThreadID != "" {
			threadIDs[event.ThreadID] = true
		}
		if event.TurnID != "" {
			turnIDs[event.TurnID] = true
		}
		switch event.Type {
		case eventlog.EventCommandCompleted, eventlog.EventToolCallCompleted, eventlog.EventFileChanged:
			operations = append(operations, knowledgeItem{Text: text, EventID: event.EventID})
		default:
			facts = append(facts, knowledgeItem{Text: text, EventID: event.EventID})
		}
	}
	if len(facts) == 0 && len(operations) == 0 {
		return "", errors.New("archive events have no summarizable content")
	}

	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(escapeMarkdown(request.Title))
	b.WriteString("\n\n")
	b.WriteString("## 结论\n\n")
	b.WriteString("- ")
	b.WriteString(formatKnowledgeItem(firstNonEmptyItem(facts, operations)))
	b.WriteString("\n\n")
	b.WriteString("## 背景\n\n")
	writeKnowledgeList(&b, facts)
	b.WriteString("\n## 关键事实\n\n")
	writeKnowledgeList(&b, facts)
	b.WriteString("\n## 操作记录\n\n")
	writeKnowledgeList(&b, operations)
	b.WriteString("\n## 后续事项\n\n")
	b.WriteString("- 未确认。\n\n")
	b.WriteString("## 来源\n\n")
	b.WriteString("- Archive ID: `")
	b.WriteString(request.ArchiveID)
	b.WriteString("`\n")
	writeSetRefs(&b, "thread_id", threadIDs)
	writeSetRefs(&b, "turn_id", turnIDs)
	for _, eventID := range sourceRefs {
		if eventID == "" {
			continue
		}
		b.WriteString("- event_id: `")
		b.WriteString(eventID)
		b.WriteString("`\n")
	}
	return b.String(), nil
}

func renderPayload(payload map[string]any) string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		label := strings.ReplaceAll(key, "_", " ")
		b.WriteString("**")
		b.WriteString(label)
		b.WriteString(":** ")
		b.WriteString(escapeMarkdown(fmt.Sprint(payload[key])))
		b.WriteString("\n\n")
	}
	return b.String()
}

func summarizeEventPayload(event eventlog.TurnEvent) string {
	if len(event.Payload) == 0 {
		return ""
	}
	preferred := []string{"text", "message", "summary", "tool_output", "command", "path"}
	for _, key := range preferred {
		if value, ok := event.Payload[key]; ok {
			return strings.TrimSpace(fmt.Sprintf("%v", value))
		}
	}
	return strings.TrimSpace(renderPayload(event.Payload))
}

func firstNonEmptyItem(groups ...[]knowledgeItem) knowledgeItem {
	for _, group := range groups {
		for _, item := range group {
			if strings.TrimSpace(item.Text) != "" {
				return item
			}
		}
	}
	return knowledgeItem{Text: "未确认。"}
}

func writeKnowledgeList(b *strings.Builder, items []knowledgeItem) {
	if len(items) == 0 {
		b.WriteString("- 未确认。\n")
		return
	}
	for _, item := range items {
		if strings.TrimSpace(item.Text) == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(formatKnowledgeItem(item))
		b.WriteString("\n")
	}
}

func formatKnowledgeItem(item knowledgeItem) string {
	text := strings.TrimSpace(item.Text)
	if item.EventID == "" {
		return text
	}
	return text + " （event_id: `" + item.EventID + "`）"
}

func writeSetRefs(b *strings.Builder, label string, values map[string]bool) {
	refs := make([]string, 0, len(values))
	for value := range values {
		refs = append(refs, value)
	}
	sort.Strings(refs)
	for _, value := range refs {
		b.WriteString("- ")
		b.WriteString(label)
		b.WriteString(": `")
		b.WriteString(value)
		b.WriteString("`\n")
	}
}

func escapeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.TrimSpace(value)
}
