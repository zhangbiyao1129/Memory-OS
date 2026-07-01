CREATE TABLE IF NOT EXISTS archives (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    archive_id TEXT NOT NULL UNIQUE,
    user_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    title TEXT NOT NULL,
    file_path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    index_generation INTEGER NOT NULL DEFAULT 1,
    current_version INTEGER NOT NULL DEFAULT 1,
    content_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS archives_project_user_idx ON archives (org_id, project_id, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS archives_index_generation_idx ON archives (archive_id, index_generation);
CREATE INDEX IF NOT EXISTS archives_status_idx ON archives (status);

CREATE TABLE IF NOT EXISTS archive_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    archive_id TEXT NOT NULL REFERENCES archives(archive_id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    file_path TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    editor_user_id TEXT NOT NULL,
    edit_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS archive_versions_unique_version ON archive_versions (archive_id, version);

CREATE TABLE IF NOT EXISTS archive_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    archive_id TEXT NOT NULL REFERENCES archives(archive_id) ON DELETE CASCADE,
    event_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS archive_events_unique_event ON archive_events (archive_id, event_id);

CREATE TABLE IF NOT EXISTS archive_edit_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    archive_id TEXT NOT NULL REFERENCES archives(archive_id) ON DELETE CASCADE,
    actor_user_id TEXT NOT NULL,
    old_version INTEGER NOT NULL,
    new_version INTEGER NOT NULL,
    old_content_hash TEXT NOT NULL,
    new_content_hash TEXT NOT NULL,
    request_id TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS archive_edit_audit_logs_archive_idx ON archive_edit_audit_logs (archive_id, created_at DESC);

CREATE TABLE IF NOT EXISTS archive_index_generations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    archive_id TEXT NOT NULL REFERENCES archives(archive_id) ON DELETE CASCADE,
    index_generation INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS archive_index_generations_unique_generation ON archive_index_generations (archive_id, index_generation);
