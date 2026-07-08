package mcpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"memory-os/internal/workspace"
)

type WorkspaceDetector interface {
	Detect(ctx context.Context, cwd string) (workspace.Identity, error)
}

type Config struct {
	MCPURL   string
	Token    string
	AgentID  string
	CWD      string
	Client   *http.Client
	Detector WorkspaceDetector
}

type Proxy struct {
	mcpURL   string
	token    string
	agentID  string
	cwd      string
	client   *http.Client
	detector WorkspaceDetector
}

type ToolResult struct {
	IsError bool
	Text    string
}

func New(config Config) Proxy {
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	detector := config.Detector
	if detector == nil {
		detector = workspace.NewGitDetector(workspace.ExecCommandRunner{})
	}
	agentID := strings.TrimSpace(config.AgentID)
	if agentID == "" {
		agentID = "mcp"
	}
	return Proxy{
		mcpURL:   strings.TrimRight(strings.TrimSpace(config.MCPURL), "/"),
		token:    strings.TrimSpace(config.Token),
		agentID:  agentID,
		cwd:      config.CWD,
		client:   client,
		detector: detector,
	}
}

func (p Proxy) CallTool(ctx context.Context, name string, args map[string]any) (ToolResult, error) {
	if strings.TrimSpace(p.mcpURL) == "" {
		return ToolResult{}, errors.New("MEMORY_OS_MCP_URL is required")
	}
	if strings.TrimSpace(p.token) == "" {
		return ToolResult{}, errors.New("MEMORY_OS_TOKEN is required")
	}
	if args == nil {
		args = map[string]any{}
	}
	if name == "memory_search" {
		if err := p.ensureSearchContext(ctx, args); err != nil {
			return ToolResult{IsError: true, Text: err.Error()}, nil
		}
	}
	if name == "memory_append_event" {
		if err := p.ensureAppendEventContext(ctx, args); err != nil {
			return ToolResult{IsError: true, Text: err.Error()}, nil
		}
	}
	payload := map[string]any{"name": name, "arguments": args}
	body, err := json.Marshal(payload)
	if err != nil {
		return ToolResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.mcpURL+"/tools/call", bytes.NewReader(body))
	if err != nil {
		return ToolResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.token)
	response, err := p.client.Do(request)
	if err != nil {
		return ToolResult{}, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return ToolResult{}, err
	}
	result := formatRemoteResponse(responseBody)
	if response.StatusCode >= 400 {
		result.IsError = true
	}
	return result, nil
}

func (p Proxy) ensureSearchContext(ctx context.Context, args map[string]any) error {
	actor := mapArg(args["actor"])
	if actor == nil {
		actor = map[string]any{}
		args["actor"] = actor
	}
	if strings.TrimSpace(stringArg(actor["agent_id"])) == "" {
		actor["agent_id"] = p.agentID
	}
	if strings.TrimSpace(stringArg(args["scope"])) == "" {
		args["scope"] = "project"
	}
	if strings.TrimSpace(stringArg(args["visibility"])) == "" {
		args["visibility"] = "project"
	}
	if strings.TrimSpace(stringArg(actor["project_id"])) != "" {
		return nil
	}
	if _, ok := args["workspace"].(map[string]any); ok {
		return nil
	}
	detectCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	identity, err := p.detector.Detect(detectCtx, p.cwd)
	if err != nil {
		return fmt.Errorf("无法自动识别当前 Git 项目：%w", err)
	}
	args["workspace"] = workspaceToArgs(identity)
	return nil
}

func (p Proxy) ensureAppendEventContext(ctx context.Context, args map[string]any) error {
	event := mapArg(args["event"])
	if event == nil {
		return errors.New("event is required")
	}
	actor := mapArg(event["actor"])
	if actor == nil {
		actor = map[string]any{}
		event["actor"] = actor
	}
	if strings.TrimSpace(stringArg(actor["agent_id"])) == "" {
		actor["agent_id"] = p.agentID
	}
	if _, ok := args["workspace"].(map[string]any); ok {
		return nil
	}
	detectCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	identity, err := p.detector.Detect(detectCtx, p.cwd)
	if err != nil {
		return fmt.Errorf("无法自动识别当前工作区：%w", err)
	}
	args["workspace"] = workspaceToArgs(identity)
	return nil
}

func formatRemoteResponse(body []byte) ToolResult {
	var response struct {
		Code   string          `json:"code"`
		Error  string          `json:"error"`
		Search json.RawMessage `json:"search"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return ToolResult{IsError: true, Text: string(body)}
	}
	if response.Code != "" && response.Code != "ok" {
		if response.Error != "" {
			return ToolResult{IsError: true, Text: response.Error}
		}
		return ToolResult{IsError: true, Text: response.Code}
	}
	if len(response.Search) > 0 {
		return ToolResult{Text: string(response.Search)}
	}
	return ToolResult{Text: string(body)}
}

func mapArg(value any) map[string]any {
	raw, _ := value.(map[string]any)
	return raw
}

func stringArg(value any) string {
	raw, _ := value.(string)
	return raw
}

func workspaceToArgs(identity workspace.Identity) map[string]any {
	return map[string]any{
		"cwd":        identity.CWD,
		"git_root":   identity.GitRoot,
		"git_remote": identity.GitRemote,
		"git_branch": identity.GitBranch,
		"git_commit": identity.GitCommit,
	}
}
