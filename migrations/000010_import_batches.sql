CREATE TABLE IF NOT EXISTS import_batches (
    id BIGSERIAL PRIMARY KEY,
    batch_id TEXT NOT NULL UNIQUE,
    source_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    dry_run BOOLEAN NOT NULL DEFAULT false,
    item_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS import_items (
    id BIGSERIAL PRIMARY KEY,
    batch_id TEXT NOT NULL,
    source_type TEXT NOT NULL,
    external_id TEXT NOT NULL,
    item_kind TEXT NOT NULL,
    safe_text TEXT NOT NULL,
    source_ref JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS import_errors (
    id BIGSERIAL PRIMARY KEY,
    batch_id TEXT NOT NULL,
    source_type TEXT NOT NULL,
    external_id TEXT,
    error TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS external_source_refs (
    id BIGSERIAL PRIMARY KEY,
    source_type TEXT NOT NULL,
    external_id TEXT NOT NULL,
    memory_os_ref TEXT NOT NULL,
    batch_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS import_items_source_external_unique
    ON import_items (source_type, external_id);

CREATE INDEX IF NOT EXISTS import_items_batch_idx
    ON import_items (batch_id);

CREATE INDEX IF NOT EXISTS external_source_refs_source_idx
    ON external_source_refs (source_type, external_id);
