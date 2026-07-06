package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewOpenAICompatibleRejectsMissingBaseURL(t *testing.T) {
	_, err := NewOpenAICompatible(OpenAICompatibleConfig{APIKey: "secret"})
	if err == nil {
		t.Fatal("NewOpenAICompatible() error = nil, want missing base url error")
	}
}

func TestNewOpenAICompatibleUsesConfiguredTimeout(t *testing.T) {
	client, err := NewOpenAICompatible(OpenAICompatibleConfig{
		BaseURL: "http://example.local:8000",
		APIKey:  "secret-key",
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}
	if client.httpClient.Timeout != 2*time.Minute {
		t.Fatalf("timeout = %s, want 2m0s", client.httpClient.Timeout)
	}
}

func TestEmbeddingReturnsNotConfiguredWithoutKey(t *testing.T) {
	client, err := NewOpenAICompatible(OpenAICompatibleConfig{
		BaseURL:        "http://example.local:8000",
		EmbeddingModel: "bge-m3",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	_, err = client.Embed(context.Background(), []string{"hello"})

	if err == nil {
		t.Fatal("Embed() error = nil, want not configured error")
	}
	if !IsNotConfigured(err) {
		t.Fatalf("Embed() error = %v, want not configured", err)
	}
}

func TestErrorDoesNotLeakAPIKey(t *testing.T) {
	client, err := NewOpenAICompatible(OpenAICompatibleConfig{
		BaseURL:        "http://example.local:8000",
		APIKey:         "sk-test-redacted-example",
		EmbeddingModel: "",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	_, err = client.Embed(context.Background(), []string{"hello"})

	if err == nil {
		t.Fatal("Embed() error = nil, want model error")
	}
	if strings.Contains(err.Error(), "sk-test-redacted-example") {
		t.Fatalf("error leaked api key: %v", err)
	}
}

func TestEmbedCallsOpenAICompatibleEndpoint(t *testing.T) {
	var path string
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["model"] != "bge-m3" {
			t.Fatalf("model = %v", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer server.Close()
	client, err := NewOpenAICompatible(OpenAICompatibleConfig{BaseURL: server.URL, APIKey: "secret-key", EmbeddingModel: "bge-m3"})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	response, err := client.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if path != "/v1/embeddings" || auth != "Bearer secret-key" {
		t.Fatalf("request path/auth = %s/%s", path, auth)
	}
	if response.Dim != 3 || len(response.Vectors) != 1 || response.Vectors[0][2] != 0.3 {
		t.Fatalf("embedding response = %#v", response)
	}
}

func TestRerankCallsOpenAICompatibleEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["model"] != "bge-reranker-v2-m3" || payload["query"] != "deploy" {
			t.Fatalf("payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.91},{"index":0,"score":0.7}]}`))
	}))
	defer server.Close()
	client, err := NewOpenAICompatible(OpenAICompatibleConfig{BaseURL: server.URL, APIKey: "secret-key", RerankModel: "bge-reranker-v2-m3"})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	response, err := client.Rerank(context.Background(), "deploy", []string{"a", "b"})
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if len(response.Results) != 2 || response.Results[0].Index != 1 || response.Results[0].Score != 0.91 {
		t.Fatalf("rerank response = %#v", response)
	}
}

func TestChatReturnsNotConfiguredWithoutKey(t *testing.T) {
	client, err := NewOpenAICompatible(OpenAICompatibleConfig{
		BaseURL:  "http://example.local:8000",
		LLMModel: "gpt-4o",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})

	if err == nil {
		t.Fatal("Chat() error = nil, want not configured error")
	}
	if !IsNotConfigured(err) {
		t.Fatalf("Chat() error = %v, want not configured", err)
	}
}

func TestChatReturnsModelRequiredWithoutModel(t *testing.T) {
	client, err := NewOpenAICompatible(OpenAICompatibleConfig{
		BaseURL: "http://example.local:8000",
		APIKey:  "secret",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})

	if err == nil {
		t.Fatal("Chat() error = nil, want model required error")
	}
	if !strings.Contains(err.Error(), "model name is required") {
		t.Fatalf("Chat() error = %v, want model name required", err)
	}
}

func TestChatPrefersRequestModelOverConfig(t *testing.T) {
	var receivedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		receivedModel = payload["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"candidates\":[]}"}}]}`))
	}))
	defer server.Close()

	client, err := NewOpenAICompatible(OpenAICompatibleConfig{
		BaseURL:  server.URL,
		APIKey:   "secret-key",
		LLMModel: "config-model",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:    "request-model",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if receivedModel != "request-model" {
		t.Fatalf("model = %s, want request-model", receivedModel)
	}
	if resp.Text != `{"candidates":[]}` {
		t.Fatalf("response text = %q", resp.Text)
	}
}

func TestChatFallsBackToConfigModel(t *testing.T) {
	var receivedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		receivedModel = payload["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"result text"}}]}`))
	}))
	defer server.Close()

	client, err := NewOpenAICompatible(OpenAICompatibleConfig{
		BaseURL:  server.URL,
		APIKey:   "secret-key",
		LLMModel: "fallback-model",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if receivedModel != "fallback-model" {
		t.Fatalf("model = %s, want fallback-model", receivedModel)
	}
	if resp.Text != "result text" {
		t.Fatalf("response text = %q, want result text", resp.Text)
	}
}

func TestChatReturnsErrorOnNon2xxStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewOpenAICompatible(OpenAICompatibleConfig{
		BaseURL:  server.URL,
		APIKey:   "secret-key",
		LLMModel: "test-model",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("Chat() error = nil, want upstream error")
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("error leaked api key: %v", err)
	}
}

func TestChatReturnsErrorOnEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()

	client, err := NewOpenAICompatible(OpenAICompatibleConfig{
		BaseURL:  server.URL,
		APIKey:   "secret-key",
		LLMModel: "test-model",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("Chat() error = nil, want empty choices error")
	}
}

func TestTransportErrorDoesNotLeakAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad upstream", http.StatusBadGateway)
	}))
	defer server.Close()
	client, err := NewOpenAICompatible(OpenAICompatibleConfig{BaseURL: server.URL, APIKey: "sk-test-redacted-example", EmbeddingModel: "bge-m3"})
	if err != nil {
		t.Fatalf("NewOpenAICompatible() error = %v", err)
	}

	_, err = client.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("Embed() error = nil, want upstream error")
	}
	if strings.Contains(err.Error(), "sk-test-redacted-example") {
		t.Fatalf("error leaked api key: %v", err)
	}
}
