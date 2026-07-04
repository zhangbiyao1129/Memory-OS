package rag

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/llm"
	"memory-os/internal/qdrant"
)

type pointWriter interface {
	UpsertPoints(ctx context.Context, collection string, points []qdrant.Point) error
	SearchPoints(ctx context.Context, request qdrant.SearchPointsRequest) ([]qdrant.SearchPointResult, error)
}

type QdrantStore struct {
	pool       *pgxpool.Pool
	writer     pointWriter
	embedder   llm.EmbeddingClient
	collection string
}

func NewQdrantStore(pool *pgxpool.Pool, writer pointWriter, embedder llm.EmbeddingClient, collection string) *QdrantStore {
	if collection == "" {
		collection = qdrant.DefaultCollectionName
	}
	return &QdrantStore{pool: pool, writer: writer, embedder: embedder, collection: collection}
}

func (s *QdrantStore) Upsert(payload ChunkPayload) error {
	if s == nil || s.pool == nil || s.writer == nil || s.embedder == nil {
		return errors.New("qdrant rag store dependencies are required")
	}
	if payload.ChunkID == "" || payload.Content == "" {
		return errors.New("chunk id and content are required")
	}
	embedding, err := s.embedder.Embed(context.Background(), []string{payload.Content})
	if err != nil {
		return err
	}
	if len(embedding.Vectors) != 1 {
		return errors.New("embedding response must contain one vector")
	}
	pointPayload := qdrantPayload(payload)
	point := qdrant.Point{ID: pointID(payload.ChunkID), Vector: float64Vector(embedding.Vectors[0]), Payload: pointPayload}
	if err := s.writer.UpsertPoints(context.Background(), s.collection, []qdrant.Point{point}); err != nil {
		return err
	}
	encoded, err := json.Marshal(pointPayload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(context.Background(), `
INSERT INTO qdrant_points (point_id, chunk_id, collection_name, payload, vector_status)
VALUES ($1,$2,$3,$4::jsonb,'indexed')
ON CONFLICT (point_id) DO UPDATE SET
    payload = EXCLUDED.payload,
    vector_status = 'indexed',
    updated_at = now()`,
		point.ID, payload.ChunkID, s.collection, string(encoded))
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(context.Background(), `
UPDATE archive_chunks
SET vector_status = 'indexed',
    updated_at = now()
WHERE chunk_id = $1`,
		payload.ChunkID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("archive chunk %s not found for qdrant status update", payload.ChunkID)
	}
	return nil
}

func (s *QdrantStore) Filtered(filter map[string][]string) []ChunkPayload {
	return nil
}

func (s *QdrantStore) Search(ctx context.Context, request SearchRequest) ([]SearchResult, error) {
	if s == nil || s.pool == nil || s.writer == nil || s.embedder == nil {
		return nil, errors.New("qdrant rag store dependencies are required")
	}
	if request.Query == "" {
		return nil, errors.New("query is required")
	}
	if len(request.Filter.Must) == 0 {
		return nil, errors.New("query-time qdrant filter is required")
	}
	embedding, err := s.embedder.Embed(ctx, []string{request.Query})
	if err != nil {
		return nil, err
	}
	if len(embedding.Vectors) != 1 {
		return nil, errors.New("embedding response must contain one vector")
	}
	qdrantResults, err := s.writer.SearchPoints(ctx, qdrant.SearchPointsRequest{Collection: s.collection, Vector: float64Vector(embedding.Vectors[0]), Filter: request.Filter, Limit: request.Limit})
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(qdrantResults))
	for _, item := range qdrantResults {
		chunkID, _ := item.Payload["chunk_id"].(string)
		if chunkID == "" {
			continue
		}
		chunk, err := s.loadChunk(ctx, chunkID)
		if err != nil {
			return nil, err
		}
		if chunk.ChunkID == "" {
			continue
		}
		results = append(results, SearchResult{Text: chunk.Content, Score: item.Score, Source: SourceRef{ArchiveID: chunk.ArchiveID, ChunkID: chunk.ChunkID, SourceEventIDs: chunk.SourceEventIDs}})
	}
	return results, nil
}

func (s *QdrantStore) loadChunk(ctx context.Context, chunkID string) (ChunkPayload, error) {
	var chunk ChunkPayload
	err := s.pool.QueryRow(ctx, `
SELECT chunk_id, archive_id, content, source_event_ids
FROM archive_chunks
WHERE chunk_id = $1 AND stale = false`,
		chunkID).Scan(&chunk.ChunkID, &chunk.ArchiveID, &chunk.Content, &chunk.SourceEventIDs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ChunkPayload{}, nil
		}
		return ChunkPayload{}, err
	}
	return chunk, nil
}

func qdrantPayload(payload ChunkPayload) map[string]any {
	return map[string]any{
		"doc_type":          "archive_chunk",
		"chunk_id":          payload.ChunkID,
		"archive_id":        payload.ArchiveID,
		"org_id":            payload.OrgID,
		"project_id":        payload.ProjectID,
		"user_id":           payload.UserID,
		"visibility":        payload.Visibility,
		"permission_labels": payload.PermissionLabels,
		"index_generation":  fmt.Sprintf("%d", payload.IndexGeneration),
		"content_hash":      payload.ContentHash,
		"source_event_ids":  payload.SourceEventIDs,
	}
}

func pointID(chunkID string) string {
	sum := sha1.Sum([]byte(chunkID))
	value := hex.EncodeToString(sum[:])
	return value[0:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:32]
}

func float64Vector(vector []float32) []float64 {
	converted := make([]float64, 0, len(vector))
	for _, value := range vector {
		converted = append(converted, float64(value))
	}
	return converted
}
