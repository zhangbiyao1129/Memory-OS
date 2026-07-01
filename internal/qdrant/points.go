package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type Point struct {
	ID      string         `json:"id"`
	Vector  []float64      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

type SearchPointsRequest struct {
	Collection string
	Vector     []float64
	Filter     PayloadFilter
	Limit      int
}

type SearchPointResult struct {
	ID      string         `json:"id"`
	Score   float64        `json:"score"`
	Payload map[string]any `json:"payload"`
}

var uuidPointIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (c *Client) UpsertPoints(ctx context.Context, collection string, points []Point) error {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return errors.New("qdrant client is not configured")
	}
	if strings.TrimSpace(collection) == "" {
		return errors.New("qdrant collection is required")
	}
	if len(points) == 0 {
		return errors.New("qdrant points are required")
	}
	for _, point := range points {
		if !validPointID(point.ID) {
			return fmt.Errorf("qdrant point id %q is invalid: use unsigned integer or UUID", point.ID)
		}
		if len(point.Vector) == 0 {
			return fmt.Errorf("qdrant point %q vector is required", point.ID)
		}
	}
	body, err := json.Marshal(map[string]any{"points": points})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/collections/"+collection+"/points?wait=true", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("qdrant upsert points request invalid: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant upsert points failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant upsert points status: %d", resp.StatusCode)
	}
	return nil
}

func validPointID(id string) bool {
	if id == "" {
		return false
	}
	if _, err := strconv.ParseUint(id, 10, 64); err == nil {
		return true
	}
	return uuidPointIDPattern.MatchString(id)
}

func (c *Client) SearchPoints(ctx context.Context, request SearchPointsRequest) ([]SearchPointResult, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return nil, errors.New("qdrant client is not configured")
	}
	if strings.TrimSpace(request.Collection) == "" {
		return nil, errors.New("qdrant collection is required")
	}
	if len(request.Vector) == 0 {
		return nil, errors.New("qdrant search vector is required")
	}
	if len(request.Filter.Must) == 0 {
		return nil, errors.New("qdrant query-time filter is required")
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 10
	}
	body, err := json.Marshal(map[string]any{
		"vector":       request.Vector,
		"filter":       toQdrantFilter(request.Filter),
		"limit":        limit,
		"with_payload": true,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/collections/"+request.Collection+"/points/search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("qdrant search points request invalid: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qdrant search points failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("qdrant search points status: %d", resp.StatusCode)
	}
	var decoded struct {
		Result []SearchPointResult `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded.Result, nil
}

func toQdrantFilter(filter PayloadFilter) map[string]any {
	must := []map[string]any{}
	for key, values := range filter.Must {
		if len(values) == 1 {
			must = append(must, map[string]any{"key": key, "match": map[string]any{"value": values[0]}})
			continue
		}
		anyValues := make([]any, 0, len(values))
		for _, value := range values {
			anyValues = append(anyValues, value)
		}
		must = append(must, map[string]any{"key": key, "match": map[string]any{"any": anyValues}})
	}
	return map[string]any{"must": must}
}
