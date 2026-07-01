package archive

import (
	"strings"
	"testing"
)

func TestChunkMarkdownKeepsHeadingAndSourceRefs(t *testing.T) {
	markdown := "# Deploy Notes\n\n### user_message\n\n- Source ref: `event_1`\n\n**text:** deploy api\n\n### tool_call_completed\n\n- Source ref: `event_2`\n\n**tool output:** ok\n"

	chunks, err := ChunkMarkdown(ChunkRequest{
		ArchiveID:       "archive_1",
		IndexGeneration: 1,
		Content:         markdown,
		MaxBytes:        128,
	})

	if err != nil {
		t.Fatalf("ChunkMarkdown() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("chunks len = 0")
	}
	if chunks[0].ArchiveID != "archive_1" || chunks[0].IndexGeneration != 1 {
		t.Fatalf("chunk metadata mismatch: %#v", chunks[0])
	}
	if !containsString(chunks[0].HeadingPath, "Deploy Notes") {
		t.Fatalf("heading path missing title: %#v", chunks[0].HeadingPath)
	}
	if !containsString(chunks[0].SourceEventIDs, "event_1") {
		t.Fatalf("source refs missing event_1: %#v", chunks[0].SourceEventIDs)
	}
}

func TestChunkMarkdownDoesNotLeakFakeSecret(t *testing.T) {
	markdown := "# Safe\n\ntext sk-test-redacted-example\n"

	chunks, err := ChunkMarkdown(ChunkRequest{ArchiveID: "archive_1", IndexGeneration: 1, Content: markdown, MaxBytes: 256})

	if err != nil {
		t.Fatalf("ChunkMarkdown() error = %v", err)
	}
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "sk-test-redacted-example") {
			t.Fatal("chunk leaked fake secret")
		}
	}
}

func TestChunkMarkdownRejectsEmptyContent(t *testing.T) {
	_, err := ChunkMarkdown(ChunkRequest{ArchiveID: "archive_1", IndexGeneration: 1})
	if err == nil {
		t.Fatal("ChunkMarkdown() error = nil, want empty content rejection")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
