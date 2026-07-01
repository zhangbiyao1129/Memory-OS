package qdrant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
