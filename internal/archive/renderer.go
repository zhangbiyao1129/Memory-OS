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

func escapeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.TrimSpace(value)
}
