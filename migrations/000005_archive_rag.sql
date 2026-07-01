CREATE TABLE IF NOT EXISTS archive_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chunk_id TEXT NOT NULL UNIQUE,
    archive_id TEXT NOT NULL REFERENCES archives(archive_id) ON DELETE CASCADE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    visibility TEXT NOT NULL,
    permission_labels TEXT[] NOT NULL DEFAULT '{}',
    index_generation INTEGER NOT NULL,
    chunk_index INTEGER NOT NULL,
    heading_path TEXT[] NOT NULL DEFAULT '{}',
    source_event_ids TEXT[] NOT NULL DEFAULT '{}',
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    vector_status TEXT NOT NULL DEFAULT 'pending',
    stale BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS archive_chunks_archive_generation_index_unique ON archive_chunks (archive_id, index_generation, chunk_index);
CREATE INDEX IF NOT EXISTS archive_chunks_archive_generation_idx ON archive_chunks (archive_id, index_generation, stale);
CREATE INDEX IF NOT EXISTS archive_chunks_scope_idx ON archive_chunks (org_id, project_id, user_id, visibility);

CREATE TABLE IF NOT EXISTS archive_index_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key TEXT NOT NULL,
    archive_id TEXT NOT NULL REFERENCES archives(archive_id) ON DELETE CASCADE,
    index_generation INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS archive_index_jobs_idempotency_unique ON archive_index_jobs (idempotency_key);
CREATE INDEX IF NOT EXISTS archive_index_jobs_status_idx ON archive_index_jobs (status, created_at);

CREATE TABLE IF NOT EXISTS qdrant_points (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    point_id TEXT NOT NULL UNIQUE,
    chunk_id TEXT NOT NULL REFERENCES archive_chunks(chunk_id) ON DELETE CASCADE,
    collection_name TEXT NOT NULL,
    payload JSONB NOT NULL,
    vector_status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS qdrant_points_chunk_idx ON qdrant_points (chunk_id);
