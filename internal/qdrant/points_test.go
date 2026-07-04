package qdrant

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUpsertPointsUsesMemoryCollectionAndPayload(t *testing.T) {
	var path string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.String()
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	err = client.UpsertPoints(t.Context(), DefaultCollectionName, []Point{{ID: "11111111-1111-4111-8111-111111111111", Vector: []float64{0.1, 0.2}, Payload: map[string]any{"doc_type": "archive_chunk", "user_id": "user_1"}}})
	if err != nil {
		t.Fatalf("UpsertPoints() error = %v", err)
	}
	if path != "/collections/memory_os/points?wait=true" {
		t.Fatalf("path = %q", path)
	}
	points := payload["points"].([]any)
	first := points[0].(map[string]any)
	if first["id"] != "11111111-1111-4111-8111-111111111111" || first["payload"].(map[string]any)["doc_type"] != "archive_chunk" {
		t.Fatalf("point payload mismatch: %#v", first)
	}
}

func TestUpsertPointsRejectsInvalidPointIDBeforeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for invalid point id")
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	err = client.UpsertPoints(t.Context(), DefaultCollectionName, []Point{{ID: "point_1", Vector: []float64{0.1}, Payload: map[string]any{"doc_type": "archive_chunk"}}})
	if err == nil {
		t.Fatal("UpsertPoints() error = nil, want invalid point id error")
	}
}

func TestSearchPointsRequiresAndSerializesQueryTimeFilter(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/memory_os/points/search" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		encoded, _ := json.Marshal(payload)
		body = string(encoded)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"id":"point_1","score":0.99,"payload":{"doc_type":"archive_chunk","chunk_id":"chunk_1"}}]}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	filter, err := BuildPayloadFilter(FilterContext{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, DocType: "archive_chunk", IndexGeneration: 2})
	if err != nil {
		t.Fatalf("BuildPayloadFilter() error = %v", err)
	}

	results, err := client.SearchPoints(t.Context(), SearchPointsRequest{Collection: DefaultCollectionName, Vector: []float64{0.1, 0.2}, Filter: filter, Limit: 3})
	if err != nil {
		t.Fatalf("SearchPoints() error = %v", err)
	}
	for _, required := range []string{"\"key\":\"user_id\"", "\"key\":\"permission_labels\"", "\"key\":\"index_generation\"", "\"limit\":3"} {
		if !strings.Contains(body, required) {
			t.Fatalf("search body missing %s: %s", required, body)
		}
	}
	if len(results) != 1 || results[0].ID != "point_1" || results[0].Payload["chunk_id"] != "chunk_1" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSearchPointsRejectsMissingFilter(t *testing.T) {
	client, err := NewClient("http://localhost:18083")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.SearchPoints(t.Context(), SearchPointsRequest{Collection: DefaultCollectionName, Vector: []float64{0.1}, Limit: 1})
	if err == nil {
		t.Fatal("SearchPoints() error = nil, want missing filter error")
	}
}

func TestSearchPointsRealQdrantAppliesQueryTimeFilter(t *testing.T) {
	baseURL := os.Getenv("QDRANT_TEST_URL")
	if baseURL == "" {
		t.Skip("QDRANT_TEST_URL is not set")
	}
	client, err := NewClient(baseURL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	collection := fmt.Sprintf("memory_os_filter_test_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, client.baseURL+"/collections/"+collection, nil)
		if err == nil {
			resp, err := client.httpClient.Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
		}
	})
	if err := client.EnsureCollection(t.Context(), CollectionConfig{Name: collection, VectorSize: 2, Distance: DefaultDistance}); err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}
	points := []Point{
		{
			ID:     "11111111-1111-4111-8111-111111111111",
			Vector: []float64{0.1, 0.2},
			Payload: map[string]any{
				"doc_type":          "archive_chunk",
				"chunk_id":          "chunk_allowed",
				"user_id":           "user_allowed",
				"org_id":            "org_allowed",
				"project_id":        "project_allowed",
				"visibility":        "project",
				"permission_labels": []string{"project:project_allowed:read"},
				"index_generation":  "2",
			},
		},
		{
			ID:     "22222222-2222-4222-8222-222222222222",
			Vector: []float64{0.1, 0.2},
			Payload: map[string]any{
				"doc_type":          "archive_chunk",
				"chunk_id":          "chunk_denied",
				"user_id":           "user_denied",
				"org_id":            "org_allowed",
				"project_id":        "project_allowed",
				"visibility":        "project",
				"permission_labels": []string{"project:project_allowed:read"},
				"index_generation":  "2",
			},
		},
		{
			ID:     "33333333-3333-4333-8333-333333333333",
			Vector: []float64{0.1, 0.2},
			Payload: map[string]any{
				"doc_type":          "archive_chunk",
				"chunk_id":          "chunk_old_generation",
				"user_id":           "user_allowed",
				"org_id":            "org_allowed",
				"project_id":        "project_allowed",
				"visibility":        "project",
				"permission_labels": []string{"project:project_allowed:read"},
				"index_generation":  "1",
			},
		},
	}
	if err := client.UpsertPoints(t.Context(), collection, points); err != nil {
		t.Fatalf("UpsertPoints() error = %v", err)
	}
	filter, err := BuildPayloadFilter(FilterContext{UserID: "user_allowed", OrgID: "org_allowed", ProjectID: "project_allowed", Visibility: "project", PermissionLabels: []string{"project:project_allowed:read"}, DocType: "archive_chunk", IndexGeneration: 2})
	if err != nil {
		t.Fatalf("BuildPayloadFilter() error = %v", err)
	}

	results, err := client.SearchPoints(t.Context(), SearchPointsRequest{Collection: collection, Vector: []float64{0.1, 0.2}, Filter: filter, Limit: 10})
	if err != nil {
		t.Fatalf("SearchPoints() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1: %#v", len(results), results)
	}
	if results[0].Payload["chunk_id"] != "chunk_allowed" {
		t.Fatalf("result chunk_id = %#v, want chunk_allowed", results[0].Payload["chunk_id"])
	}
}
