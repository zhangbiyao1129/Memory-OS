package qdrant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClientRejectsEmptyBaseURL(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatal("NewClient() error = nil, want missing url error")
	}
}

func TestNewClientAcceptsBaseURL(t *testing.T) {
	client, err := NewClient("http://localhost:18083")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewClient() returned nil client")
	}
}

func TestCheckerReturnsErrorWhenClientIsNil(t *testing.T) {
	checker := Checker{}

	err := checker.Check(context.Background())

	if err == nil {
		t.Fatal("Checker.Check() error = nil, want missing client error")
	}
}

func TestCollectionInfoReadsQdrantCollectionStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/collections/memory_os" {
			t.Fatalf("path = %q, want /collections/memory_os", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"status":                "green",
				"vectors_count":         7,
				"indexed_vectors_count": 6,
				"points_count":          5,
				"segments_count":        1,
				"config": map[string]any{
					"params": map[string]any{
						"vectors": map[string]any{"size": 1024, "distance": "Cosine"},
					},
				},
				"payload_schema": map[string]any{
					"user_id": map[string]any{"data_type": "keyword"},
				},
			},
		})
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	info, err := client.CollectionInfo(context.Background(), DefaultCollectionName)
	if err != nil {
		t.Fatalf("CollectionInfo() error = %v", err)
	}

	if info.Name != DefaultCollectionName || info.Status != "green" {
		t.Fatalf("collection identity = %#v, want memory_os green", info)
	}
	if info.PointsCount != 5 || info.VectorsCount != 7 || info.IndexedVectorsCount != 6 || info.SegmentsCount != 1 {
		t.Fatalf("collection counters = %#v", info)
	}
	if info.VectorSize != 1024 || info.Distance != "Cosine" {
		t.Fatalf("vector config = %#v", info)
	}
	if !info.PayloadSchema["user_id"] {
		t.Fatalf("payload schema missing user_id: %#v", info.PayloadSchema)
	}
}
