-- 000020_candidate_maintenance_runs.sql
-- 候选记忆清洗整合审计表。
-- 记录所有手动/自动 AI 清洗整合操作,用于审计和健康监控。

CREATE TABLE IF NOT EXISTS candidate_maintenance_runs (
    id BIGSERIAL PRIMARY KEY,
    run_id TEXT NOT NULL UNIQUE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_key TEXT NOT NULL DEFAULT '',
    thread_id TEXT NOT NULL DEFAULT '',
    trigger_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    processed INTEGER NOT NULL DEFAULT 0,
    discarded INTEGER NOT NULL DEFAULT 0,
    kept INTEGER NOT NULL DEFAULT 0,
    composed INTEGER NOT NULL DEFAULT 0,
    archive_id TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    locked_by TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS candidate_maintenance_runs_scope_idx
    ON candidate_maintenance_runs (org_id, project_id, source_key, thread_id, created_at DESC);

CREATE INDEX IF NOT EXISTS candidate_maintenance_runs_status_idx
    ON candidate_maintenance_runs (status, updated_at DESC);

-- 同项目同时只能有一个 running 的清洗任务(防重入)
CREATE UNIQUE INDEX IF NOT EXISTS candidate_maintenance_runs_running_project_unique
    ON candidate_maintenance_runs (org_id, project_id)
    WHERE status = 'running';
