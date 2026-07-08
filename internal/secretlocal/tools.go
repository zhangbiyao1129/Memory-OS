package secretlocal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Tool 描述本机 MCP 暴露的 secret 工具 schema。
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ToolResult 是工具调用结果。
// Text 是模型可见文本，绝不含明文；Injected 只交给宿主执行层，绝不回传模型或日志。
type ToolResult struct {
	IsError  bool
	Text     string
	Injected string
}

type ToolHandlerConfig struct {
	KeyPath string
	Client  Client
}

// ToolHandler 处理本机 secret 工具调用：明文只在本进程内出现。
type ToolHandler struct {
	keyPath string
	client  Client
}

func NewToolHandler(config ToolHandlerConfig) *ToolHandler {
	return &ToolHandler{keyPath: config.KeyPath, client: config.Client}
}

// Handles 返回该 name 是否由本机 secret 工具处理。
func Handles(name string) bool {
	return normalizeToolName(name) != ""
}

func Tools() []Tool {
	return []Tool{
		{
			Name:        "secret_create_local",
			Description: "本机加密后创建 secret：明文只在本机加密，服务端只收密文",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":       map[string]any{"type": "string"},
					"plaintext":  map[string]any{"type": "string", "description": "secret 明文，只在本机加密，不上传"},
					"value":      map[string]any{"type": "string", "description": "plaintext 的兼容别名，只在本机加密，不上传"},
					"org_id":     map[string]any{"type": "string"},
					"project_id": map[string]any{"type": "string"},
					"env_name":   map[string]any{"type": "string"},
					"site":       map[string]any{"type": "string"},
					"purpose":    map[string]any{"type": "string"},
					"expires_at": map[string]any{"type": "string", "description": "RFC3339"},
				},
				"required": []any{"name", "org_id", "project_id"},
			},
		},
		{
			Name:        "secret_list_local",
			Description: "列出 secret 元信息（不含明文/密文）",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"org_id":     map[string]any{"type": "string"},
					"project_id": map[string]any{"type": "string"},
					"status":     map[string]any{"type": "string", "enum": []any{"active", "disabled"}},
				},
				"required": []any{"org_id", "project_id"},
			},
		},
		{
			Name:        "secret_use_local",
			Description: "在本机解密并把 secret 注入 template；返回值只包含掩码，明文不外泄",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"secret_ref": map[string]any{"type": "string"},
					"template":   map[string]any{"type": "string", "description": "含 ${secret_ref_xxx} 占位符"},
					"command":    map[string]any{"type": "string", "description": "template 的兼容别名"},
				},
				"required": []any{"secret_ref"},
			},
		},
		{
			Name:        "secret_disable_local",
			Description: "禁用一个 secret（仅 owner）",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"secret_ref": map[string]any{"type": "string"},
					"org_id":     map[string]any{"type": "string"},
					"project_id": map[string]any{"type": "string"},
				},
				"required": []any{"secret_ref"},
			},
		},
	}
}

func (h *ToolHandler) Handle(ctx context.Context, name string, args map[string]any) ToolResult {
	switch normalizeToolName(name) {
	case "secret_create_local":
		return h.handleCreate(args)
	case "secret_list_local":
		return h.handleList(args)
	case "secret_use_local":
		return h.handleUse(args)
	case "secret_disable_local":
		return h.handleDisable(args)
	default:
		return ToolResult{IsError: true, Text: "unknown local secret tool"}
	}
}

func normalizeToolName(name string) string {
	switch strings.TrimSpace(name) {
	case "secret_create_local":
		return "secret_create_local"
	case "secret_list_local", "secret_list":
		return "secret_list_local"
	case "secret_use_local", "secret_use":
		return "secret_use_local"
	case "secret_disable_local", "secret_disable":
		return "secret_disable_local"
	default:
		return ""
	}
}

func (h *ToolHandler) loadKey() (DeviceKey, error) {
	path := h.keyPath
	if strings.TrimSpace(path) == "" {
		resolved, err := DefaultKeyPath()
		if err != nil {
			return DeviceKey{}, err
		}
		path = resolved
	}
	return LoadOrCreateDeviceKey(path)
}

func (h *ToolHandler) handleCreate(args map[string]any) ToolResult {
	// 明文不做 trim，避免破坏含首尾空白/换行的 secret（如 PEM）。
	plaintext, _ := args["plaintext"].(string)
	if plaintext == "" {
		plaintext, _ = args["value"].(string)
	}
	if strings.TrimSpace(plaintext) == "" {
		return ToolResult{IsError: true, Text: "plaintext is required"}
	}
	key, err := h.loadKey()
	if err != nil {
		return ToolResult{IsError: true, Text: err.Error()}
	}
	blob, err := key.Encrypt([]byte(plaintext))
	if err != nil {
		return ToolResult{IsError: true, Text: err.Error()}
	}
	meta, err := h.client.Create(CreateParams{
		OrgID:     argString(args, "org_id"),
		ProjectID: argString(args, "project_id"),
		Name:      argString(args, "name"),
		EnvName:   argString(args, "env_name"),
		Site:      argString(args, "site"),
		Purpose:   argString(args, "purpose"),
		ExpiresAt: argString(args, "expires_at"),
	}, blob)
	if err != nil {
		return ToolResult{IsError: true, Text: err.Error()}
	}
	return jsonResult(map[string]any{
		"secret_ref":      meta.SecretRef,
		"name":            meta.Name,
		"status":          meta.Status,
		"key_fingerprint": blob.KeyFingerprint,
		"message":         "secret 已在本机加密并上传，服务端只保存密文",
	})
}

func (h *ToolHandler) handleList(args map[string]any) ToolResult {
	items, err := h.client.List(argString(args, "org_id"), argString(args, "project_id"), argString(args, "status"))
	if err != nil {
		return ToolResult{IsError: true, Text: err.Error()}
	}
	return jsonResult(map[string]any{"secrets": items})
}

func (h *ToolHandler) handleUse(args map[string]any) ToolResult {
	secretRef := argString(args, "secret_ref")
	template := argString(args, "template")
	if template == "" {
		template = argString(args, "command")
	}
	if secretRef == "" || template == "" {
		return ToolResult{IsError: true, Text: "secret_ref and template are required"}
	}
	key, err := h.loadKey()
	if err != nil {
		return ToolResult{IsError: true, Text: err.Error()}
	}
	_, blob, err := h.client.GetCiphertext(secretRef)
	if err != nil {
		return ToolResult{IsError: true, Text: err.Error()}
	}
	plaintext, err := key.Decrypt(blob)
	if err != nil {
		return ToolResult{IsError: true, Text: "本机解密失败：" + err.Error()}
	}
	// v1：本机解密成功即证明当前设备/owner 可用该 secret。
	// 明文用于本机注入宿主工具执行过程，但绝不进入模型可见的 Text 或日志——
	// Injected 字段只交给宿主执行层（stdio server 不会把它回传模型）。
	injected := strings.ReplaceAll(template, "${"+secretRef+"}", string(plaintext))
	masked := strings.ReplaceAll(template, "${"+secretRef+"}", "****")
	return ToolResult{
		Text:     fmt.Sprintf(`{"masked":%q,"injected_count":1,"message":"secret 已在本机解密，明文不会出现在返回值或日志中"}`, masked),
		Injected: injected,
	}
}

func (h *ToolHandler) handleDisable(args map[string]any) ToolResult {
	meta, err := h.client.Disable(argString(args, "secret_ref"), argString(args, "org_id"), argString(args, "project_id"))
	if err != nil {
		return ToolResult{IsError: true, Text: err.Error()}
	}
	return jsonResult(map[string]any{"secret_ref": meta.SecretRef, "status": meta.Status})
}

func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func jsonResult(payload map[string]any) ToolResult {
	body, err := json.Marshal(payload)
	if err != nil {
		return ToolResult{IsError: true, Text: err.Error()}
	}
	return ToolResult{Text: string(body)}
}
