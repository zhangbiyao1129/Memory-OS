package secretlocal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newToolTestServer(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	store := map[string]storedSecret{}
	captured := map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/memory/secrets/create":
			captured["create"] = body
			enc, _ := body["encrypted"].(map[string]any)
			ref := "secret_ref_1"
			store[ref] = storedSecret{name: str(body["name"]), enc: enc, status: "active"}
			_ = json.NewEncoder(w).Encode(map[string]any{"secret_ref": ref, "name": body["name"], "status": "active"})
		case "/memory/secrets/list":
			items := []map[string]any{}
			for ref, s := range store {
				items = append(items, map[string]any{"secret_ref": ref, "name": s.name, "status": s.status})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": items})
		case "/memory/secrets/ciphertext":
			ref := str(body["secret_ref"])
			s, ok := store[ref]
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"secret not found"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"secret_ref": ref, "encrypted": s.enc})
		case "/memory/secrets/disable":
			ref := str(body["secret_ref"])
			s := store[ref]
			s.status = "disabled"
			store[ref] = s
			_ = json.NewEncoder(w).Encode(map[string]any{"secret_ref": ref, "status": "disabled"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return server, &captured
}

type storedSecret struct {
	name   string
	enc    map[string]any
	status string
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func newTestHandler(t *testing.T, serverURL string) *ToolHandler {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "memory-os", "secret-device-key.json")
	return NewToolHandler(ToolHandlerConfig{
		KeyPath: keyPath,
		Client:  NewClient(serverURL, "test-token", nil),
	})
}

func TestToolHandlerCreateThenUseReturnsMaskedOutput(t *testing.T) {
	server, captured := newToolTestServer(t)
	defer server.Close()
	handler := newTestHandler(t, server.URL)

	createResult := handler.Handle(context.Background(), "secret_create_local", map[string]any{
		"name":       "api-key",
		"org_id":     "org_1",
		"project_id": "project_1",
		"plaintext":  "fake-secret-value",
	})
	if createResult.IsError {
		t.Fatalf("secret_create_local error: %s", createResult.Text)
	}
	if strings.Contains(createResult.Text, "fake-secret-value") {
		t.Fatalf("create result leaked plaintext: %s", createResult.Text)
	}
	// 服务端收到的请求不含明文
	createBody := (*captured)["create"].(map[string]any)
	if _, ok := createBody["plaintext"]; ok {
		t.Fatal("server received plaintext field")
	}

	useResult := handler.Handle(context.Background(), "secret_use", map[string]any{
		"secret_ref": "secret_ref_1",
		"template":   "TOKEN=${secret_ref_1}",
	})
	if useResult.IsError {
		t.Fatalf("secret_use error: %s", useResult.Text)
	}
	if strings.Contains(useResult.Text, "fake-secret-value") {
		t.Fatalf("secret_use leaked plaintext: %s", useResult.Text)
	}
	if !strings.Contains(useResult.Text, "****") {
		t.Fatalf("secret_use output not masked: %s", useResult.Text)
	}
	// Injected 字段应含真实明文（供宿主执行层），但绝不进入模型可见 Text。
	if useResult.Injected != "TOKEN=fake-secret-value" {
		t.Fatalf("secret_use Injected = %q, want TOKEN=fake-secret-value", useResult.Injected)
	}
}

func TestToolHandlerListAndDisable(t *testing.T) {
	server, _ := newToolTestServer(t)
	defer server.Close()
	handler := newTestHandler(t, server.URL)

	handler.Handle(context.Background(), "secret_create_local", map[string]any{
		"name": "api-key", "org_id": "org_1", "project_id": "project_1", "plaintext": "v",
	})

	listResult := handler.Handle(context.Background(), "secret_list", map[string]any{"org_id": "org_1", "project_id": "project_1"})
	if listResult.IsError || !strings.Contains(listResult.Text, "secret_ref_1") {
		t.Fatalf("secret_list unexpected: %s", listResult.Text)
	}

	disableResult := handler.Handle(context.Background(), "secret_disable", map[string]any{"secret_ref": "secret_ref_1", "org_id": "org_1", "project_id": "project_1"})
	if disableResult.IsError || !strings.Contains(disableResult.Text, "disabled") {
		t.Fatalf("secret_disable unexpected: %s", disableResult.Text)
	}
}

func TestToolHandlerRejectsInsecureKeyFile(t *testing.T) {
	server, _ := newToolTestServer(t)
	defer server.Close()
	keyPath := filepath.Join(t.TempDir(), "memory-os", "secret-device-key.json")
	handler := NewToolHandler(ToolHandlerConfig{KeyPath: keyPath, Client: NewClient(server.URL, "test-token", nil)})

	// 先创建一次生成 key 文件
	if r := handler.Handle(context.Background(), "secret_create_local", map[string]any{"name": "k", "org_id": "o", "project_id": "p", "plaintext": "v"}); r.IsError {
		t.Fatalf("initial create error: %s", r.Text)
	}
	// 破坏文件权限
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	result := handler.Handle(context.Background(), "secret_use", map[string]any{"secret_ref": "secret_ref_1", "template": "T=${secret_ref_1}"})
	if !result.IsError {
		t.Fatal("secret_use with insecure key file should error")
	}
}

func TestToolHandlerTools(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range Tools() {
		names[tool.Name] = true
	}
	for _, want := range []string{"secret_create_local", "secret_list", "secret_use", "secret_disable"} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}
}
