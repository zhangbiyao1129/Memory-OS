package qdrant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnsureCollectionCreatesSingleMemoryOSCollection(t *testing.T) {
	var path string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
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

	if err := client.EnsureCollection(t.Context(), CollectionConfig{Name: DefaultCollectionName, VectorSize: DefaultVectorSize, Distance: DefaultDistance}); err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}
	if path != "/collections/memory_os" {
		t.Fatalf("path = %q, want /collections/memory_os", path)
	}
	vectors, ok := payload["vectors"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing vectors: %#v", payload)
	}
	if vectors["size"].(float64) != DefaultVectorSize || vectors["distance"] != DefaultDistance {
		t.Fatalf("vectors = %#v", vectors)
	}
}

func TestEnsureCollectionSchemaCreatesCollectionAndPayloadIndexes(t *testing.T) {
	requests := []struct {
		path    string
		method  string
		payload map[string]any
	}{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/collections/memory_os" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"status":"green","points_count":0,"vectors_count":0,"indexed_vectors_count":0,"segments_count":1,"config":{"params":{"vectors":{"size":1024,"distance":"Cosine"}}},"payload_schema":{}}}`))
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		requests = append(requests, struct {
			path    string
			method  string
			payload map[string]any
		}{path: r.URL.Path, method: r.Method, payload: payload})
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if err := client.EnsureCollectionSchema(t.Context(), CollectionConfig{Name: DefaultCollectionName, VectorSize: DefaultVectorSize, Distance: DefaultDistance}, DefaultPayloadIndexConfigs()); err != nil {
		t.Fatalf("EnsureCollectionSchema() error = %v", err)
	}
	if len(requests) != 1+len(DefaultPayloadIndexConfigs()) {
		t.Fatalf("request count = %d, want %d", len(requests), 1+len(DefaultPayloadIndexConfigs()))
	}
	if requests[0].path != "/collections/memory_os" || requests[0].method != http.MethodPut {
		t.Fatalf("collection request = %#v, want PUT /collections/memory_os", requests[0])
	}
	for index, cfg := range DefaultPayloadIndexConfigs() {
		request := requests[index+1]
		if request.path != "/collections/memory_os/index" || request.method != http.MethodPut {
			t.Fatalf("payload index request[%d] = %#v, want PUT /collections/memory_os/index", index, request)
		}
		if request.payload["field_name"] != cfg.FieldName {
			t.Fatalf("payload index field_name[%d] = %v, want %q", index, request.payload["field_name"], cfg.FieldName)
		}
		if request.payload["field_schema"] != cfg.FieldSchema {
			t.Fatalf("payload index field_schema[%d] = %v, want %q", index, request.payload["field_schema"], cfg.FieldSchema)
		}
	}
}

func TestEnsurePayloadIndexesSkipsExistingSchemas(t *testing.T) {
	indexRequests := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/memory_os":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"status":"green","points_count":0,"vectors_count":0,"indexed_vectors_count":0,"segments_count":1,"config":{"params":{"vectors":{"size":1024,"distance":"Cosine"}}},"payload_schema":{"doc_type":{},"user_id":{}}}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/memory_os/index":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			indexRequests = append(indexRequests, payload)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	err = client.EnsurePayloadIndexes(t.Context(), DefaultCollectionName, []PayloadIndexConfig{
		{FieldName: "doc_type", FieldSchema: PayloadSchemaKeyword},
		{FieldName: "user_id", FieldSchema: PayloadSchemaKeyword},
		{FieldName: "status", FieldSchema: PayloadSchemaKeyword},
	})
	if err != nil {
		t.Fatalf("EnsurePayloadIndexes() error = %v", err)
	}
	if len(indexRequests) != 1 {
		t.Fatalf("index request count = %d, want 1", len(indexRequests))
	}
	if indexRequests[0]["field_name"] != "status" {
		t.Fatalf("created field = %v, want status", indexRequests[0]["field_name"])
	}
}

func TestEnsureCollectionRejectsInvalidConfig(t *testing.T) {
	client, err := NewClient("http://localhost:18083")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if err := client.EnsureCollection(t.Context(), CollectionConfig{}); err == nil {
		t.Fatal("EnsureCollection() error = nil, want invalid config error")
	}
}
