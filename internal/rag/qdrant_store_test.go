package rag

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/llm"
	"memory-os/internal/qdrant"
)

func TestQdrantStoreRequiresDependencies(t *testing.T) {
	store := NewQdrantStore(nil, nil, nil, qdrant.DefaultCollectionName)

	err := store.Upsert(ChunkPayload{ChunkID: "chunk_1"})

	if err == nil {
		t.Fatal("Upsert() error = nil, want missing dependency error")
	}
}

func TestQdrantStoreEmbedsUpsertsAndRecordsPoint(t *testing.T) {
	pool := ragTestPool(t)
	insertArchiveChunkForQdrantStore(t, pool, "chunk_qdrant_1")
	embedder := &fakeEmbeddingClient{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	qdrantClient := &fakeQdrantPointWriter{}
	store := NewQdrantStore(pool, qdrantClient, embedder, qdrant.DefaultCollectionName)
	payload := ChunkPayload{
		ChunkID: "chunk_qdrant_1", ArchiveID: "archive_qdrant_1",
		OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project",
		PermissionLabels: []string{"project:project_1:read"},
		IndexGeneration:  1,
		Content:          "deploy api",
		ContentHash:      "hash_1",
		SourceEventIDs:   []string{"event_1"},
	}

	if err := store.Upsert(payload); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if len(embedder.texts) != 1 || embedder.texts[0] != "deploy api" {
		t.Fatalf("embed texts = %#v", embedder.texts)
	}
	if qdrantClient.collection != qdrant.DefaultCollectionName || len(qdrantClient.points) != 1 {
		t.Fatalf("qdrant upsert = collection %q points %#v", qdrantClient.collection, qdrantClient.points)
	}
	point := qdrantClient.points[0]
	if point.Payload["doc_type"] != "archive_chunk" || point.Payload["chunk_id"] != "chunk_qdrant_1" || point.Payload["index_generation"] != "1" {
		t.Fatalf("point payload mismatch: %#v", point.Payload)
	}
	if point.Vector[2] < 0.299 || point.Vector[2] > 0.301 {
		t.Fatalf("point vector = %#v", point.Vector)
	}

	var storedPayload []byte
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT payload, vector_status FROM qdrant_points WHERE chunk_id = $1`, "chunk_qdrant_1").Scan(&storedPayload, &status); err != nil {
		t.Fatalf("select qdrant point: %v", err)
	}
	if status != "indexed" {
		t.Fatalf("vector_status = %q, want indexed", status)
	}
	var chunkStatus string
	if err := pool.QueryRow(context.Background(), `SELECT vector_status FROM archive_chunks WHERE chunk_id = $1`, "chunk_qdrant_1").Scan(&chunkStatus); err != nil {
		t.Fatalf("select archive chunk vector status: %v", err)
	}
	if chunkStatus != "indexed" {
		t.Fatalf("archive chunk vector_status = %q, want indexed", chunkStatus)
	}
	var decoded map[string]any
	if err := json.Unmarshal(storedPayload, &decoded); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if decoded["chunk_id"] != "chunk_qdrant_1" || decoded["doc_type"] != "archive_chunk" {
		t.Fatalf("stored payload mismatch: %#v", decoded)
	}
}

func TestQdrantStoreSearchEmbedsQueryUsesQdrantFilterAndLoadsChunk(t *testing.T) {
	pool := ragTestPool(t)
	insertArchiveChunkForQdrantStore(t, pool, "chunk_qdrant_search")
	embedder := &fakeEmbeddingClient{vectors: [][]float32{{0.4, 0.5, 0.6}}}
	qdrantClient := &fakeQdrantPointWriter{
		searchResults: []qdrant.SearchPointResult{{
			ID:    "point_1",
			Score: 0.87,
			Payload: map[string]any{
				"chunk_id": "chunk_qdrant_search",
			},
		}},
	}
	store := NewQdrantStore(pool, qdrantClient, embedder, qdrant.DefaultCollectionName)
	filter := qdrant.PayloadFilter{Must: map[string][]string{
		"doc_type":          {"archive_chunk"},
		"user_id":           {"user_1"},
		"org_id":            {"org_1"},
		"project_id":        {"project_1"},
		"visibility":        {"project"},
		"permission_labels": {"project:project_1:read"},
		"index_generation":  {"1"},
	}}

	results, err := store.Search(context.Background(), SearchRequest{Query: "deploy", Filter: filter, Limit: 3})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(embedder.texts) != 1 || embedder.texts[0] != "deploy" {
		t.Fatalf("embed texts = %#v", embedder.texts)
	}
	if qdrantClient.searchRequest.Collection != qdrant.DefaultCollectionName {
		t.Fatalf("search collection = %q", qdrantClient.searchRequest.Collection)
	}
	if qdrantClient.searchRequest.Limit != 3 {
		t.Fatalf("search limit = %d, want 3", qdrantClient.searchRequest.Limit)
	}
	if got := qdrantClient.searchRequest.Filter.Must["permission_labels"]; len(got) != 1 || got[0] != "project:project_1:read" {
		t.Fatalf("search filter permission_labels = %#v", got)
	}
	if got := qdrantClient.searchRequest.Filter.Must["index_generation"]; len(got) != 1 || got[0] != "1" {
		t.Fatalf("search filter index_generation = %#v", got)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Text != "deploy api" || results[0].Score != 0.87 || results[0].Source.ChunkID != "chunk_qdrant_search" {
		t.Fatalf("result mismatch: %#v", results[0])
	}
	if results[0].Source.ArchiveID != "archive_qdrant_1" || len(results[0].Source.SourceEventIDs) != 1 || results[0].Source.SourceEventIDs[0] != "event_1" {
		t.Fatalf("result source mismatch: %#v", results[0].Source)
	}
}

func TestQdrantStoreSearchRejectsMissingFilter(t *testing.T) {
	store := NewQdrantStore(&pgxpool.Pool{}, &fakeQdrantPointWriter{}, &fakeEmbeddingClient{vectors: [][]float32{{0.1}}}, qdrant.DefaultCollectionName)

	_, err := store.Search(context.Background(), SearchRequest{Query: "deploy"})

	if err == nil {
		t.Fatal("Search() error = nil, want missing filter rejection")
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

func ragTestPool(t *testing.T) *pgxpool.Pool {
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

func insertArchiveChunkForQdrantStore(t *testing.T, pool *pgxpool.Pool, chunkID string) {
	t.Helper()
	chunkIndex := 2
	if chunkID == "chunk_qdrant_1" {
		chunkIndex = 0
	}
	if chunkID == "chunk_qdrant_search" {
		chunkIndex = 1
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO archives (archive_id, user_id, org_id, project_id, title, file_path, status, index_generation, current_version, content_hash)
VALUES ('archive_qdrant_1','user_1','org_1','project_1','Archive','/tmp/archive.md','active',1,1,'hash_archive')
ON CONFLICT (archive_id) DO NOTHING`); err != nil {
		t.Fatalf("insert archive: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO archive_chunks (chunk_id, archive_id, org_id, project_id, user_id, visibility, permission_labels, index_generation, chunk_index, content, content_hash)
VALUES ($1,'archive_qdrant_1','org_1','project_1','user_1','project',$2,1,$3,'deploy api','hash_1')
ON CONFLICT (chunk_id) DO UPDATE SET content = EXCLUDED.content, source_event_ids = EXCLUDED.source_event_ids, stale = false`, chunkID, []string{"project:project_1:read"}, chunkIndex); err != nil {
		t.Fatalf("insert archive chunk: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE archive_chunks SET source_event_ids = $2 WHERE chunk_id = $1`, chunkID, []string{"event_1"}); err != nil {
		t.Fatalf("update archive chunk source events: %v", err)
	}
}
