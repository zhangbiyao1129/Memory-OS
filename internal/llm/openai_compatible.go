package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("model_provider_not_configured")

func IsNotConfigured(err error) bool {
	return errors.Is(err, ErrNotConfigured)
}

type OpenAICompatibleConfig struct {
	BaseURL        string
	APIKey         string
	LLMModel       string
	EmbeddingModel string
	RerankModel    string
}

type OpenAICompatibleClient struct {
	cfg        OpenAICompatibleConfig
	httpClient *http.Client
}

func NewOpenAICompatible(cfg OpenAICompatibleConfig) (*OpenAICompatibleClient, error) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		return nil, errors.New("llm base url is required")
	}
	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return nil, fmt.Errorf("llm base url invalid: %w", err)
	}
	return &OpenAICompatibleClient{cfg: cfg, httpClient: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (c *OpenAICompatibleClient) Chat(ctx context.Context, request ChatRequest) (ChatResponse, error) {
	if err := c.ensureConfigured(c.cfg.LLMModel); err != nil {
		return ChatResponse{}, err
	}
	return ChatResponse{}, errors.New("chat client transport is not implemented in phase 1")
}

func (c *OpenAICompatibleClient) Embed(ctx context.Context, texts []string) (EmbeddingResponse, error) {
	if err := c.ensureConfigured(c.cfg.EmbeddingModel); err != nil {
		return EmbeddingResponse{}, err
	}
	if len(texts) == 0 {
		return EmbeddingResponse{}, errors.New("embedding texts are required")
	}
	var decoded struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := c.postJSON(ctx, "/v1/embeddings", map[string]any{"model": c.cfg.EmbeddingModel, "input": texts}, &decoded); err != nil {
		return EmbeddingResponse{}, err
	}
	vectors := make([][]float32, 0, len(decoded.Data))
	dim := 0
	for _, item := range decoded.Data {
		if dim == 0 {
			dim = len(item.Embedding)
		}
		vectors = append(vectors, item.Embedding)
	}
	if len(vectors) == 0 || dim == 0 {
		return EmbeddingResponse{}, errors.New("embedding response is empty")
	}
	return EmbeddingResponse{Vectors: vectors, Dim: dim}, nil
}

func (c *OpenAICompatibleClient) Rerank(ctx context.Context, query string, documents []string) (RerankResponse, error) {
	if err := c.ensureConfigured(c.cfg.RerankModel); err != nil {
		return RerankResponse{}, err
	}
	if strings.TrimSpace(query) == "" {
		return RerankResponse{}, errors.New("rerank query is required")
	}
	if len(documents) == 0 {
		return RerankResponse{}, errors.New("rerank documents are required")
	}
	var decoded struct {
		Results []struct {
			Index          int      `json:"index"`
			Score          *float64 `json:"score"`
			RelevanceScore *float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := c.postJSON(ctx, "/v1/rerank", map[string]any{"model": c.cfg.RerankModel, "query": query, "documents": documents}, &decoded); err != nil {
		return RerankResponse{}, err
	}
	results := make([]RerankResult, 0, len(decoded.Results))
	for _, item := range decoded.Results {
		score := 0.0
		if item.RelevanceScore != nil {
			score = *item.RelevanceScore
		} else if item.Score != nil {
			score = *item.Score
		}
		results = append(results, RerankResult{Index: item.Index, Score: score})
	}
	return RerankResponse{Results: results}, nil
}

func (c *OpenAICompatibleClient) ensureConfigured(model string) error {
	if c == nil || c.cfg.BaseURL == "" || c.cfg.APIKey == "" {
		return ErrNotConfigured
	}
	if model == "" {
		return errors.New("model name is required")
	}
	return nil
}

func (c *OpenAICompatibleClient) postJSON(ctx context.Context, path string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("model request invalid: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("model request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("model provider status: %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("model response decode failed: %w", err)
	}
	return nil
}
