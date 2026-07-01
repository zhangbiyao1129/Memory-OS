CREATE TABLE IF NOT EXISTS memory_access_logs (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    actor_user_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    source_ref JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS retrieval_requests (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE,
    actor_user_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    query_hash TEXT NOT NULL,
    rerank_degraded BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS retrieval_results (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    rank INTEGER NOT NULL,
    score DOUBLE PRECISION NOT NULL DEFAULT 0,
    source_kind TEXT NOT NULL,
    source_ref JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_access_logs_actor_idx
    ON memory_access_logs (actor_user_id, org_id, project_id);

CREATE INDEX IF NOT EXISTS retrieval_results_request_idx
    ON retrieval_results (request_id, rank);
