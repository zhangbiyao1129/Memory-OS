package qdrant

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

const (
	DefaultCollectionName = "memory_os"
	DefaultVectorSize     = 1024
	DefaultDistance       = "Cosine"
)

type CollectionConfig struct {
	Name       string
	VectorSize int
	Distance   string
}

// Client 是 Qdrant HTTP API 的最小 wrapper。
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("qdrant url is required")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("qdrant url invalid: %w", err)
	}
	return &Client{baseURL: baseURL, httpClient: &http.Client{Timeout: 5 * time.Second}}, nil
}

func (c *Client) Health(ctx context.Context) error {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return errors.New("qdrant client is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("qdrant health request invalid: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant health failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant health status: %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) EnsureCollection(ctx context.Context, config CollectionConfig) error {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return errors.New("qdrant client is not configured")
	}
	if strings.TrimSpace(config.Name) == "" {
		return errors.New("qdrant collection name is required")
	}
	if config.VectorSize <= 0 {
		return errors.New("qdrant vector size is required")
	}
	if strings.TrimSpace(config.Distance) == "" {
		return errors.New("qdrant distance is required")
	}
	body, err := json.Marshal(map[string]any{
		"vectors": map[string]any{
			"size":     config.VectorSize,
			"distance": config.Distance,
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/collections/"+config.Name, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("qdrant ensure collection request invalid: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant ensure collection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant ensure collection status: %d", resp.StatusCode)
	}
	return nil
}

// Checker 用于 health service 检查 Qdrant。
type Checker struct {
	Client *Client
}

func (c Checker) Check(ctx context.Context) error {
	if c.Client == nil {
		return errors.New("qdrant client is not configured")
	}
	return c.Client.Health(ctx)
}
