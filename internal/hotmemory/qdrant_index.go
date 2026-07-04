package hotmemory

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

type QdrantIndex struct {
	pool       *pgxpool.Pool
	writer     pointWriter
	embedder   llm.EmbeddingClient
	collection string
}

func NewQdrantIndex(pool *pgxpool.Pool, writer pointWriter, embedder llm.EmbeddingClient, collection string) *QdrantIndex {
	if collection == "" {
		collection = qdrant.DefaultCollectionName
	}
	return &QdrantIndex{pool: pool, writer: writer, embedder: embedder, collection: collection}
}

func (i *QdrantIndex) Index(memory Memory) error {
	if i == nil || i.pool == nil || i.writer == nil || i.embedder == nil {
		return errors.New("hot memory qdrant index dependencies are required")
	}
	if memory.MemoryID == "" || memory.Fact == "" {
		return errors.New("memory id and fact are required")
	}
	embedding, err := i.embedder.Embed(context.Background(), []string{memory.Fact})
	if err != nil {
		return err
	}
	if len(embedding.Vectors) != 1 {
		return errors.New("embedding response must contain one vector")
	}
	payload := hotMemoryQdrantPayload(memory)
	point := qdrant.Point{ID: hotMemoryPointID(memory.MemoryID), Vector: float64Vector(embedding.Vectors[0]), Payload: payload}
	if err := i.writer.UpsertPoints(context.Background(), i.collection, []qdrant.Point{point}); err != nil {
		return err
	}
	return i.recordPoint(point.ID, memory.MemoryID, payload)
}

func (i *QdrantIndex) Delete(memory Memory) error {
	memory.Status = StatusDeleted
	return i.Index(memory)
}

func (i *QdrantIndex) Search(request SearchRequest) ([]SearchResult, error) {
	if i == nil || i.pool == nil || i.writer == nil || i.embedder == nil {
		return nil, errors.New("hot memory qdrant index dependencies are required")
	}
	if request.Query == "" {
		return nil, errors.New("query is required")
	}
	if len(request.Filter.Must) == 0 {
		return nil, errors.New("query-time hot memory filter is required")
	}
	if err := statusFilterError(request.Filter); err != nil {
		return nil, err
	}
	embedding, err := i.embedder.Embed(context.Background(), []string{request.Query})
	if err != nil {
		return nil, err
	}
	if len(embedding.Vectors) != 1 {
		return nil, errors.New("embedding response must contain one vector")
	}
	qdrantResults, err := i.writer.SearchPoints(context.Background(), qdrant.SearchPointsRequest{Collection: i.collection, Vector: float64Vector(embedding.Vectors[0]), Filter: request.Filter, Limit: 10})
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(qdrantResults))
	for _, item := range qdrantResults {
		memoryID, _ := item.Payload["memory_id"].(string)
		if memoryID == "" {
			continue
		}
		memory, err := i.loadMemory(context.Background(), memoryID)
		if err != nil {
			return nil, err
		}
		if memory.MemoryID == "" || memory.Status == StatusDeleted || memory.DeletedAt != nil {
			continue
		}
		results = append(results, SearchResult{Memory: memory, Score: item.Score})
	}
	return results, nil
}

func (i *QdrantIndex) recordPoint(pointID, memoryID string, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = i.pool.Exec(context.Background(), `
INSERT INTO hot_memory_qdrant_points (point_id, memory_id, collection_name, payload, vector_status)
VALUES ($1,$2,$3,$4::jsonb,'indexed')
ON CONFLICT (point_id) DO UPDATE SET
    payload = EXCLUDED.payload,
    vector_status = 'indexed',
    updated_at = now()`,
		pointID, memoryID, i.collection, string(encoded))
	return err
}

func (i *QdrantIndex) loadMemory(ctx context.Context, memoryID string) (Memory, error) {
	row := i.pool.QueryRow(ctx, selectMemoryColumns()+" WHERE memory_id = $1", memoryID)
	memory, err := scanMemory(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, nil
		}
		return Memory{}, err
	}
	return memory, nil
}

func hotMemoryQdrantPayload(memory Memory) map[string]any {
	return map[string]any{
		"doc_type":          "hot_memory",
		"memory_id":         memory.MemoryID,
		"org_id":            memory.OrgID,
		"project_id":        memory.ProjectID,
		"user_id":           memory.UserID,
		"agent_id":          memory.AgentID,
		"scope":             string(memory.Scope),
		"visibility":        memory.Visibility,
		"permission_labels": memory.PermissionLabels,
		"status":            string(memory.Status),
		"fact_hash":         memory.FactHash,
	}
}

func hotMemoryPointID(memoryID string) string {
	sum := sha1.Sum([]byte("hot_memory:" + memoryID))
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

func statusFilterError(filter PayloadFilter) error {
	if len(filter.Must["status"]) == 0 {
		return fmt.Errorf("hot memory qdrant status filter is required")
	}
	return nil
}
