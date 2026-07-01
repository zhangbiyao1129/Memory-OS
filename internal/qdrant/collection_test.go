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

func TestEnsureCollectionRejectsInvalidConfig(t *testing.T) {
	client, err := NewClient("http://localhost:18083")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if err := client.EnsureCollection(t.Context(), CollectionConfig{}); err == nil {
		t.Fatal("EnsureCollection() error = nil, want invalid config error")
	}
}
