CREATE TABLE IF NOT EXISTS hot_memory_qdrant_points (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    point_id TEXT NOT NULL UNIQUE,
    memory_id TEXT NOT NULL REFERENCES hot_memories(memory_id) ON DELETE CASCADE,
    collection_name TEXT NOT NULL,
    payload JSONB NOT NULL,
    vector_status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS hot_memory_qdrant_points_memory_idx
    ON hot_memory_qdrant_points (memory_id);

CREATE INDEX IF NOT EXISTS hot_memory_qdrant_points_status_idx
    ON hot_memory_qdrant_points (vector_status, updated_at);
