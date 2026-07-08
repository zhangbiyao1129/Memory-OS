package mcpstdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"memory-os/internal/mcp"
	"memory-os/internal/mcpproxy"
	"memory-os/internal/secretlocal"
)

type frameFormat int

const (
	frameFormatContentLength frameFormat = iota
	frameFormatJSONLine
)

type Server struct {
	proxy       mcpproxy.Proxy
	secretTools *secretlocal.ToolHandler
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func NewServer(proxy mcpproxy.Proxy) Server {
	return Server{proxy: proxy}
}

// WithSecretTools 启用本机 secret 工具（本地加解密）。
func (s Server) WithSecretTools(handler *secretlocal.ToolHandler) Server {
	s.secretTools = handler
	return s
}

func (s Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	for {
		body, format, err := ReadFrameWithFormat(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		response, ok := s.handle(ctx, body)
		if !ok {
			continue
		}
		if err := WriteFrameWithFormat(output, response, format); err != nil {
			return err
		}
	}
}

func (s Server) handle(ctx context.Context, body []byte) (rpcResponse, bool) {
	var request rpcRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}, true
	}
	if len(request.ID) == 0 && strings.HasPrefix(request.Method, "notifications/") {
		return rpcResponse{}, false
	}
	switch request.Method {
	case "initialize":
		return rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "memory-os-local", "version": "0.4.0"},
		}}, true
	case "tools/list":
		return rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{"tools": s.listTools()}}, true
	case "tools/call":
		result := s.handleToolCall(ctx, request.Params)
		return rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: result}, true
	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{}}, true
	default:
		return rpcResponse{JSONRPC: "2.0", ID: request.ID, Error: &rpcError{Code: -32601, Message: "method not found"}}, true
	}
}

func (s Server) handleToolCall(ctx context.Context, params json.RawMessage) map[string]any {
	var request struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return toolContent(true, "invalid tools/call params")
	}
	if s.secretTools != nil && secretlocal.Handles(request.Name) {
		result := s.secretTools.Handle(ctx, request.Name, request.Arguments)
		return toolContent(result.IsError, result.Text)
	}
	result, err := s.proxy.CallTool(ctx, request.Name, request.Arguments)
	if err != nil {
		return toolContent(true, err.Error())
	}
	return toolContent(result.IsError, result.Text)
}

func (s Server) listTools() []mcpTool {
	tools := convertTools(mcp.Tools())
	if s.secretTools != nil {
		for _, tool := range secretlocal.Tools() {
			tools = append(tools, mcpTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
		}
	}
	return tools
}

func toolContent(isError bool, text string) map[string]any {
	return map[string]any{
		"isError": isError,
		"content": []map[string]string{{
			"type": "text",
			"text": text,
		}},
	}
}

func convertTools(tools []mcp.Tool) []mcpTool {
	converted := make([]mcpTool, 0, len(tools))
	for _, tool := range tools {
		converted = append(converted, mcpTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	return converted
}

func ReadFrame(reader bytesLikeReader) ([]byte, error) {
	body, _, err := ReadFrameWithFormat(reader)
	return body, err
}

func ReadFrameWithFormat(reader bytesLikeReader) ([]byte, frameFormat, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, frameFormatContentLength, err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(strings.TrimSpace(line), "{") {
			return []byte(strings.TrimSpace(line)), frameFormatJSONLine, nil
		}
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, frameFormatContentLength, err
			}
			contentLength = parsed
		}
	}
	if contentLength <= 0 {
		return nil, frameFormatContentLength, errors.New("missing Content-Length")
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	return body, frameFormatContentLength, err
}

type bytesLikeReader interface {
	io.Reader
	ReadString(delim byte) (string, error)
}

func WriteFrame(writer io.Writer, payload any) error {
	return WriteFrameWithFormat(writer, payload, frameFormatContentLength)
}

func WriteFrameWithFormat(writer io.Writer, payload any, format frameFormat) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if format == frameFormatJSONLine {
		if _, err := writer.Write(body); err != nil {
			return err
		}
		_, err = writer.Write([]byte("\n"))
		return err
	}
	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = writer.Write(body)
	return err
}
