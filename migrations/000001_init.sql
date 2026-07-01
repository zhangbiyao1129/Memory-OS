CREATE TABLE IF NOT EXISTS schema_migrations (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO schema_migrations (version)
VALUES (1)
ON CONFLICT (version) DO NOTHING;

CREATE TABLE IF NOT EXISTS health_checks (
    id BIGSERIAL PRIMARY KEY,
    component TEXT NOT NULL UNIQUE,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
