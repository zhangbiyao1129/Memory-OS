CREATE TABLE IF NOT EXISTS turn_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id TEXT NOT NULL UNIQUE,
    turn_id TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    user_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS turn_events_turn_id_idx ON turn_events (turn_id, created_at);
CREATE INDEX IF NOT EXISTS turn_events_thread_id_idx ON turn_events (thread_id, created_at);
CREATE INDEX IF NOT EXISTS turn_events_session_id_idx ON turn_events (session_id, created_at);
CREATE INDEX IF NOT EXISTS turn_events_actor_idx ON turn_events (user_id, org_id, project_id, agent_id);

CREATE TABLE IF NOT EXISTS turn_event_payloads (
    event_id TEXT PRIMARY KEY REFERENCES turn_events(event_id) ON DELETE CASCADE,
    payload JSONB NOT NULL,
    safe_payload_hash TEXT NOT NULL,
    original_bytes INTEGER NOT NULL DEFAULT 0,
    safe_bytes INTEGER NOT NULL DEFAULT 0,
    truncated BOOLEAN NOT NULL DEFAULT false,
    warnings TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS event_ingest_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id TEXT NOT NULL,
    event_id TEXT NOT NULL REFERENCES turn_events(event_id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS event_ingest_requests_request_id_unique ON event_ingest_requests (request_id);

CREATE TABLE IF NOT EXISTS adapter_ingest_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id TEXT NOT NULL,
    adapter_name TEXT NOT NULL,
    event_id TEXT NOT NULL,
    result TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS adapter_ingest_logs_event_idx ON adapter_ingest_logs (event_id, created_at DESC);
