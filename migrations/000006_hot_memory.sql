CREATE TABLE IF NOT EXISTS hot_memories (
    id BIGSERIAL PRIMARY KEY,
    memory_id TEXT NOT NULL UNIQUE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    scope TEXT NOT NULL,
    visibility TEXT NOT NULL,
    permission_labels TEXT[] NOT NULL DEFAULT '{}',
    fact TEXT NOT NULL,
    fact_hash TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    access_count INTEGER NOT NULL DEFAULT 0,
    used_count INTEGER NOT NULL DEFAULT 0,
    hot_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS hot_memory_sources (
    id BIGSERIAL PRIMARY KEY,
    memory_id TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_ref TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS hot_memory_events (
    id BIGSERIAL PRIMARY KEY,
    memory_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS hot_memory_index_jobs (
    id BIGSERIAL PRIMARY KEY,
    job_id TEXT NOT NULL UNIQUE,
    memory_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS hot_memories_scope_fact_unique
    ON hot_memories (org_id, project_id, user_id, scope, fact_hash)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS hot_memories_scope_idx
    ON hot_memories (org_id, project_id, user_id, scope);

CREATE INDEX IF NOT EXISTS hot_memories_status_score_idx
    ON hot_memories (status, hot_score DESC);

CREATE INDEX IF NOT EXISTS hot_memory_sources_memory_idx
    ON hot_memory_sources (memory_id);

CREATE UNIQUE INDEX IF NOT EXISTS hot_memory_sources_memory_source_unique
    ON hot_memory_sources (memory_id, source_type, source_ref);

CREATE UNIQUE INDEX IF NOT EXISTS hot_memory_index_jobs_idempotency_unique
    ON hot_memory_index_jobs (idempotency_key);
