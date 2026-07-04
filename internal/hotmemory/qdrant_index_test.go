package hotmemory

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/llm"
	"memory-os/internal/qdrant"
)

func TestQdrantIndexRequiresDependencies(t *testing.T) {
	index := NewQdrantIndex(nil, nil, nil, qdrant.DefaultCollectionName)

	err := index.Index(Memory{MemoryID: "hm_1", Fact: "fact"})

	if err == nil {
		t.Fatal("Index() error = nil, want missing dependency error")
	}
}

func TestQdrantIndexEmbedsUpsertsAndRecordsPoint(t *testing.T) {
	pool := hotMemoryTestPool(t)
	insertHotMemoryForQdrantIndex(t, pool, "hm_qdrant_1")
	embedder := &fakeEmbeddingClient{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	qdrantClient := &fakeQdrantPointWriter{}
	index := NewQdrantIndex(pool, qdrantClient, embedder, qdrant.DefaultCollectionName)
	memory := Memory{
		MemoryID:         "hm_qdrant_1",
		OrgID:            "org_1",
		ProjectID:        "project_1",
		UserID:           "user_1",
		AgentID:          "codex",
		Scope:            ScopeProject,
		Visibility:       "project",
		PermissionLabels: []string{"project:project_1:read"},
		Fact:             "deploy api hot memory",
		FactHash:         "hash_hot_1",
		Status:           StatusActive,
	}

	if err := index.Index(memory); err != nil {
		t.Fatalf("Index() error = %v", err)
	}

	if len(embedder.texts) != 1 || embedder.texts[0] != "deploy api hot memory" {
		t.Fatalf("embed texts = %#v", embedder.texts)
	}
	if qdrantClient.collection != qdrant.DefaultCollectionName || len(qdrantClient.points) != 1 {
		t.Fatalf("qdrant upsert = collection %q points %#v", qdrantClient.collection, qdrantClient.points)
	}
	point := qdrantClient.points[0]
	if point.Payload["doc_type"] != "hot_memory" || point.Payload["memory_id"] != "hm_qdrant_1" || point.Payload["status"] != "active" {
		t.Fatalf("point payload mismatch: %#v", point.Payload)
	}
	if point.Vector[2] < 0.299 || point.Vector[2] > 0.301 {
		t.Fatalf("point vector = %#v", point.Vector)
	}

	var storedPayload []byte
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT payload, vector_status FROM hot_memory_qdrant_points WHERE memory_id = $1`, "hm_qdrant_1").Scan(&storedPayload, &status); err != nil {
		t.Fatalf("select hot memory qdrant point: %v", err)
	}
	if status != "indexed" {
		t.Fatalf("vector_status = %q, want indexed", status)
	}
	var decoded map[string]any
	if err := json.Unmarshal(storedPayload, &decoded); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if decoded["memory_id"] != "hm_qdrant_1" || decoded["doc_type"] != "hot_memory" {
		t.Fatalf("stored payload mismatch: %#v", decoded)
	}
}

func TestQdrantIndexSearchEmbedsQueryUsesFilterAndLoadsMemory(t *testing.T) {
	pool := hotMemoryTestPool(t)
	insertHotMemoryForQdrantIndex(t, pool, "hm_qdrant_search")
	embedder := &fakeEmbeddingClient{vectors: [][]float32{{0.4, 0.5, 0.6}}}
	qdrantClient := &fakeQdrantPointWriter{
		searchResults: []qdrant.SearchPointResult{{
			ID:    "point_1",
			Score: 0.88,
			Payload: map[string]any{
				"memory_id": "hm_qdrant_search",
			},
		}},
	}
	index := NewQdrantIndex(pool, qdrantClient, embedder, qdrant.DefaultCollectionName)
	filter := PayloadFilter{Must: map[string][]string{
		"doc_type":          {"hot_memory"},
		"user_id":           {"user_1"},
		"org_id":            {"org_1"},
		"project_id":        {"project_1"},
		"scope":             {"project"},
		"visibility":        {"project"},
		"status":            {"active", "promoted", "demoted"},
		"permission_labels": {"project:project_1:read"},
	}}

	results, err := index.Search(SearchRequest{Query: "deploy", Filter: filter})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(embedder.texts) != 1 || embedder.texts[0] != "deploy" {
		t.Fatalf("embed texts = %#v", embedder.texts)
	}
	if qdrantClient.searchRequest.Collection != qdrant.DefaultCollectionName {
		t.Fatalf("search collection = %q", qdrantClient.searchRequest.Collection)
	}
	if got := qdrantClient.searchRequest.Filter.Must["permission_labels"]; len(got) != 1 || got[0] != "project:project_1:read" {
		t.Fatalf("search filter permission_labels = %#v", got)
	}
	if got := qdrantClient.searchRequest.Filter.Must["status"]; len(got) != 3 || got[0] != "active" {
		t.Fatalf("search filter status = %#v", got)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Memory.MemoryID != "hm_qdrant_search" || results[0].Memory.Fact != "deploy api hot memory" || results[0].Score != 0.88 {
		t.Fatalf("result mismatch: %#v", results[0])
	}
}

func TestQdrantIndexSearchRejectsMissingStatusFilter(t *testing.T) {
	index := NewQdrantIndex(&pgxpool.Pool{}, &fakeQdrantPointWriter{}, &fakeEmbeddingClient{vectors: [][]float32{{0.1}}}, qdrant.DefaultCollectionName)

	_, err := index.Search(SearchRequest{Query: "deploy", Filter: PayloadFilter{Must: map[string][]string{"doc_type": {"hot_memory"}}}})

	if err == nil {
		t.Fatal("Search() error = nil, want missing status filter rejection")
	}
}

type fakeEmbeddingClient struct {
	texts   []string
	vectors [][]float32
}

func (c *fakeEmbeddingClient) Embed(ctx context.Context, texts []string) (llm.EmbeddingResponse, error) {
	c.texts = append([]string(nil), texts...)
	return llm.EmbeddingResponse{Vectors: c.vectors, Dim: len(c.vectors[0])}, nil
}

type fakeQdrantPointWriter struct {
	collection    string
	points        []qdrant.Point
	searchRequest qdrant.SearchPointsRequest
	searchResults []qdrant.SearchPointResult
}

func (w *fakeQdrantPointWriter) UpsertPoints(ctx context.Context, collection string, points []qdrant.Point) error {
	w.collection = collection
	w.points = append([]qdrant.Point(nil), points...)
	return nil
}

func (w *fakeQdrantPointWriter) SearchPoints(ctx context.Context, request qdrant.SearchPointsRequest) ([]qdrant.SearchPointResult, error) {
	w.searchRequest = request
	return append([]qdrant.SearchPointResult(nil), w.searchResults...), nil
}

func hotMemoryTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertHotMemoryForQdrantIndex(t *testing.T, pool *pgxpool.Pool, memoryID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO hot_memories (
    memory_id, org_id, project_id, user_id, agent_id, scope, visibility,
    permission_labels, fact, fact_hash, confidence, hot_score, status
) VALUES ($1,'org_1','project_1','user_1','codex','project','project',$2,'deploy api hot memory','hash_hot_1',0.8,8,'active')
ON CONFLICT (memory_id) DO UPDATE SET
    fact = EXCLUDED.fact,
    status = EXCLUDED.status,
    deleted_at = NULL`, memoryID, []string{"project:project_1:read"}); err != nil {
		t.Fatalf("insert hot memory: %v", err)
	}
}
