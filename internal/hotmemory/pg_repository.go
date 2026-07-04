package hotmemory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

func (r *PGRepository) Upsert(memory Memory) (Memory, error) {
	if r == nil || r.pool == nil {
		return Memory{}, errors.New("hot memory postgres repository is not configured")
	}
	query, args := buildUpsertSQL(memory)
	row := r.pool.QueryRow(context.Background(), query, args...)
	return scanMemory(row)
}

func (r *PGRepository) Get(memoryID string) (Memory, error) {
	if r == nil || r.pool == nil {
		return Memory{}, errors.New("hot memory postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), selectMemoryColumns()+" WHERE memory_id = $1", memoryID)
	return scanMemory(row)
}

func (r *PGRepository) Search(filter map[string][]string) []Memory {
	if r == nil || r.pool == nil {
		return nil
	}
	query, args, err := buildSearchSQL(filter)
	if err != nil {
		return nil
	}
	rows, err := r.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	memories := []Memory{}
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil
		}
		memories = append(memories, memory)
	}
	return memories
}

func (r *PGRepository) Update(memory Memory) (Memory, error) {
	if r == nil || r.pool == nil {
		return Memory{}, errors.New("hot memory postgres repository is not configured")
	}
	query, args := buildUpdateSQL(memory)
	row := r.pool.QueryRow(context.Background(), query, args...)
	return scanMemory(row)
}

func buildUpsertSQL(memory Memory) (string, []any) {
	source := Source{}
	if len(memory.Sources) > 0 {
		source = memory.Sources[0]
	}
	query := `
WITH upserted AS (
    INSERT INTO hot_memories (
        memory_id, org_id, project_id, user_id, agent_id, scope, visibility,
        permission_labels, fact, fact_hash, confidence, access_count, used_count, hot_score, status
    ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
    ON CONFLICT (org_id, project_id, user_id, agent_id, scope, fact_hash) WHERE deleted_at IS NULL
    DO UPDATE SET
        confidence = GREATEST(hot_memories.confidence, EXCLUDED.confidence),
        hot_score = EXCLUDED.hot_score,
        updated_at = now()
    RETURNING memory_id, org_id, project_id, user_id, agent_id, scope, visibility, permission_labels, fact, fact_hash, confidence, access_count, used_count, hot_score, status, created_at, updated_at, deleted_at
)
, source_upsert AS (
    INSERT INTO hot_memory_sources (memory_id, source_type, source_ref, confidence)
    SELECT memory_id, $16, $17, $18 FROM upserted
    ON CONFLICT DO NOTHING
)
SELECT memory_id, org_id, project_id, user_id, agent_id, scope, visibility, permission_labels, fact, fact_hash, confidence, access_count, used_count, hot_score, status, created_at, updated_at, deleted_at
FROM upserted`
	args := []any{
		memory.MemoryID,
		memory.OrgID,
		memory.ProjectID,
		memory.UserID,
		memory.AgentID,
		string(memory.Scope),
		memory.Visibility,
		memory.PermissionLabels,
		memory.Fact,
		memory.FactHash,
		memory.Confidence,
		memory.AccessCount,
		memory.UsedCount,
		memory.HotScore,
		string(memory.Status),
		string(source.SourceType),
		source.SourceRef,
		source.Confidence,
	}
	return query, args
}

func buildSearchSQL(filter map[string][]string) (string, []any, error) {
	if len(filter) == 0 {
		return "", nil, errors.New("hot memory search filter is required")
	}
	keys := make([]string, 0, len(filter))
	for key := range filter {
		if key == "doc_type" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	where := []string{"deleted_at IS NULL", "status <> 'deleted'"}
	args := []any{}
	for _, key := range keys {
		values := filter[key]
		if len(values) == 0 {
			continue
		}
		args = append(args, values)
		placeholder := fmt.Sprintf("$%d", len(args))
		if key == "permission_labels" {
			where = append(where, "permission_labels && "+placeholder+"::text[]")
			continue
		}
		where = append(where, key+" = ANY("+placeholder+"::text[])")
	}
	query := selectMemoryColumns() + " WHERE " + strings.Join(where, " AND ") + " ORDER BY hot_score DESC"
	return query, args, nil
}

func buildUpdateSQL(memory Memory) (string, []any) {
	query := `UPDATE hot_memories
SET fact = $1,
    fact_hash = $2,
    confidence = $3,
    access_count = $4,
    used_count = $5,
    hot_score = $6,
    status = $7,
    updated_at = now(),
    deleted_at = $8
WHERE memory_id = $9
RETURNING memory_id, org_id, project_id, user_id, agent_id, scope, visibility, permission_labels, fact, fact_hash, confidence, access_count, used_count, hot_score, status, created_at, updated_at, deleted_at`
	return query, []any{memory.Fact, memory.FactHash, memory.Confidence, memory.AccessCount, memory.UsedCount, memory.HotScore, string(memory.Status), memory.DeletedAt, memory.MemoryID}
}

func selectMemoryColumns() string {
	return "SELECT memory_id, org_id, project_id, user_id, agent_id, scope, visibility, permission_labels, fact, fact_hash, confidence, access_count, used_count, hot_score, status, created_at, updated_at, deleted_at FROM hot_memories"
}

type memoryScanner interface {
	Scan(dest ...any) error
}

func scanMemory(row memoryScanner) (Memory, error) {
	var memory Memory
	var scope string
	var status string
	var deletedAt *time.Time
	if err := row.Scan(
		&memory.MemoryID,
		&memory.OrgID,
		&memory.ProjectID,
		&memory.UserID,
		&memory.AgentID,
		&scope,
		&memory.Visibility,
		&memory.PermissionLabels,
		&memory.Fact,
		&memory.FactHash,
		&memory.Confidence,
		&memory.AccessCount,
		&memory.UsedCount,
		&memory.HotScore,
		&status,
		&memory.CreatedAt,
		&memory.UpdatedAt,
		&deletedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, errors.New("memory not found")
		}
		return Memory{}, err
	}
	memory.Scope = Scope(scope)
	memory.Status = Status(status)
	memory.DeletedAt = deletedAt
	return memory, nil
}
