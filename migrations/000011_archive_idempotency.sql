CREATE TABLE IF NOT EXISTS archive_request_idempotency (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    archive_id TEXT NOT NULL REFERENCES archives(archive_id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS archive_request_idempotency_request_unique
    ON archive_request_idempotency (request_id);
