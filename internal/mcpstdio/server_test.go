package mcpstdio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"memory-os/internal/mcpproxy"
	"memory-os/internal/workspace"
)

type stdioFakeDetector struct{}

func (stdioFakeDetector) Detect(_ context.Context, _ string) (workspace.Identity, error) {
	return workspace.Identity{CWD: "/work/memory-os", GitRoot: "/work/memory-os", GitRemote: "gitlab.example.com/team/memory-os", GitBranch: "main"}, nil
}

func TestServerHandlesInitializeToolsListAndToolCall(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"ok","search":{"request_id":"stdio_1","context":"stdio memory result"}}`))
	}))
	defer httpServer.Close()

	var input bytes.Buffer
	writeTestFrame(t, &input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
	writeTestFrame(t, &input, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	writeTestFrame(t, &input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_search","arguments":{"query":"deploy"}}}`)
	output := bytes.Buffer{}
	server := NewServer(mcpproxy.New(mcpproxy.Config{MCPURL: httpServer.URL, Token: "test-token", AgentID: "codex", Detector: stdioFakeDetector{}}))

	if err := server.Serve(context.Background(), &input, &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	responses := readTestFrames(t, &output)
	if len(responses) != 3 {
		t.Fatalf("responses len = %d, want 3: %s", len(responses), output.String())
	}
	if !strings.Contains(responses[0], `"serverInfo"`) {
		t.Fatalf("initialize response = %s, want serverInfo", responses[0])
	}
	if !strings.Contains(responses[1], `"memory_search"`) || !strings.Contains(responses[1], `"inputSchema"`) {
		t.Fatalf("tools/list response = %s, want MCP tools schema", responses[1])
	}
	if !strings.Contains(responses[2], "stdio memory result") || strings.Contains(responses[2], `"isError":true`) {
		t.Fatalf("tools/call response = %s, want successful content", responses[2])
	}
}

func writeTestFrame(t *testing.T, w io.Writer, body string) {
	t.Helper()
	if _, err := w.Write([]byte("Content-Length: " + itoa(len(body)) + "\r\n\r\n" + body)); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func readTestFrames(t *testing.T, r io.Reader) []string {
	t.Helper()
	reader := bufio.NewReader(bytes.NewBuffer(mustReadAll(t, r)))
	var frames []string
	for {
		body, err := ReadFrame(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadFrame() error = %v", err)
		}
		frames = append(frames, string(body))
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("invalid json response: %v", err)
		}
	}
	return frames
}

func mustReadAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return body
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}
