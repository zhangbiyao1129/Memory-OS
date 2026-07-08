package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type PayloadIndexConfig struct {
	FieldName   string
	FieldSchema string
}

const (
	PayloadSchemaKeyword = "keyword"
)

var defaultPayloadIndexConfigs = []PayloadIndexConfig{
	{FieldName: "doc_type", FieldSchema: PayloadSchemaKeyword},
	{FieldName: "user_id", FieldSchema: PayloadSchemaKeyword},
	{FieldName: "org_id", FieldSchema: PayloadSchemaKeyword},
	{FieldName: "project_id", FieldSchema: PayloadSchemaKeyword},
	{FieldName: "visibility", FieldSchema: PayloadSchemaKeyword},
	{FieldName: "permission_labels", FieldSchema: PayloadSchemaKeyword},
	{FieldName: "index_generation", FieldSchema: PayloadSchemaKeyword},
	{FieldName: "agent_id", FieldSchema: PayloadSchemaKeyword},
	{FieldName: "scope", FieldSchema: PayloadSchemaKeyword},
	{FieldName: "status", FieldSchema: PayloadSchemaKeyword},
}

func DefaultPayloadIndexConfigs() []PayloadIndexConfig {
	return append([]PayloadIndexConfig(nil), defaultPayloadIndexConfigs...)
}

type CollectionInfo struct {
	Name                string          `json:"collection_name"`
	Status              string          `json:"collection_status"`
	PointsCount         int64           `json:"points_count"`
	VectorsCount        int64           `json:"vectors_count"`
	IndexedVectorsCount int64           `json:"indexed_vectors_count"`
	SegmentsCount       int64           `json:"segments_count"`
	VectorSize          int             `json:"vector_size"`
	Distance            string          `json:"distance"`
	PayloadSchema       map[string]bool `json:"payload_schema"`
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

func (c *Client) EnsurePayloadIndexes(ctx context.Context, collection string, indexes []PayloadIndexConfig) error {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return errors.New("qdrant client is not configured")
	}
	collection = strings.TrimSpace(collection)
	if collection == "" {
		return errors.New("qdrant collection name is required")
	}
	for _, index := range indexes {
		if strings.TrimSpace(index.FieldName) == "" {
			return errors.New("qdrant payload index field name is required")
		}
		if strings.TrimSpace(index.FieldSchema) == "" {
			return errors.New("qdrant payload index field schema is required")
		}
	}
	info, err := c.CollectionInfo(ctx, collection)
	if err != nil {
		return fmt.Errorf("qdrant payload index schema lookup failed: %w", err)
	}
	for _, index := range indexes {
		if info.PayloadSchema[index.FieldName] {
			continue
		}
		body, err := json.Marshal(map[string]any{
			"field_name":   index.FieldName,
			"field_schema": index.FieldSchema,
		})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/collections/"+collection+"/index", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("qdrant ensure payload index request invalid: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("qdrant ensure payload index failed: %w", err)
		}
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode == http.StatusConflict {
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("qdrant ensure payload index %q status: %d body: %s", index.FieldName, resp.StatusCode, strings.TrimSpace(string(responseBody)))
		}
	}
	return nil
}

func (c *Client) EnsureCollectionSchema(ctx context.Context, config CollectionConfig, indexes []PayloadIndexConfig) error {
	if err := c.EnsureCollection(ctx, config); err != nil {
		return err
	}
	if len(indexes) == 0 {
		return nil
	}
	return c.EnsurePayloadIndexes(ctx, config.Name, indexes)
}

func (c *Client) CollectionInfo(ctx context.Context, collection string) (CollectionInfo, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return CollectionInfo{}, errors.New("qdrant client is not configured")
	}
	collection = strings.TrimSpace(collection)
	if collection == "" {
		return CollectionInfo{}, errors.New("qdrant collection name is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/collections/"+collection, nil)
	if err != nil {
		return CollectionInfo{}, fmt.Errorf("qdrant collection info request invalid: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return CollectionInfo{}, fmt.Errorf("qdrant collection info failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CollectionInfo{}, fmt.Errorf("qdrant collection info status: %d", resp.StatusCode)
	}
	var decoded struct {
		Result struct {
			Status              string         `json:"status"`
			PointsCount         int64          `json:"points_count"`
			VectorsCount        int64          `json:"vectors_count"`
			IndexedVectorsCount int64          `json:"indexed_vectors_count"`
			SegmentsCount       int64          `json:"segments_count"`
			Config              collectionMeta `json:"config"`
			PayloadSchema       map[string]any `json:"payload_schema"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return CollectionInfo{}, err
	}
	info := CollectionInfo{
		Name:                collection,
		Status:              decoded.Result.Status,
		PointsCount:         decoded.Result.PointsCount,
		VectorsCount:        decoded.Result.VectorsCount,
		IndexedVectorsCount: decoded.Result.IndexedVectorsCount,
		SegmentsCount:       decoded.Result.SegmentsCount,
		VectorSize:          decoded.Result.Config.Params.Vectors.Size,
		Distance:            decoded.Result.Config.Params.Vectors.Distance,
		PayloadSchema:       map[string]bool{},
	}
	for field := range decoded.Result.PayloadSchema {
		info.PayloadSchema[field] = true
	}
	return info, nil
}

type collectionMeta struct {
	Params struct {
		Vectors struct {
			Size     int    `json:"size"`
			Distance string `json:"distance"`
		} `json:"vectors"`
	} `json:"params"`
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
