ALTER TABLE archive_index_jobs ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE archive_index_jobs ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 3;
ALTER TABLE archive_index_jobs ADD COLUMN IF NOT EXISTS locked_by TEXT;
ALTER TABLE archive_index_jobs ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
ALTER TABLE archive_index_jobs ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS archive_index_jobs_ready_idx ON archive_index_jobs (status, locked_until, created_at);
