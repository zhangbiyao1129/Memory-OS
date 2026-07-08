-- 000025_candidate_triage.sql
-- 候选记忆自动整理解释层。
-- 不改变 candidate_memories 的项目归属,只记录自动判断、跨项目链接和 Hot Memory 投递结果。

CREATE TABLE IF NOT EXISTS candidate_triage_results (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    candidate_id TEXT NOT NULL,
    source_project_id TEXT NOT NULL,
    source_key TEXT NOT NULL,
    triage_scope TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    review_state TEXT NOT NULL DEFAULT 'weak',
    reason TEXT NOT NULL DEFAULT '',
    source_refs JSONB NOT NULL DEFAULT '[]',
    promoted_hot_memory_ids TEXT[] NOT NULL DEFAULT '{}',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS candidate_triage_results_candidate_unique
    ON candidate_triage_results (org_id, candidate_id);

CREATE INDEX IF NOT EXISTS candidate_triage_results_scope_idx
    ON candidate_triage_results (org_id, triage_scope, review_state, confidence DESC);

CREATE TABLE IF NOT EXISTS candidate_project_links (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    candidate_id TEXT NOT NULL,
    linked_project_id TEXT NOT NULL,
    linked_source_key TEXT NOT NULL DEFAULT '',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    evidence TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    promoted_hot_memory_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS candidate_project_links_unique
    ON candidate_project_links (org_id, candidate_id, linked_project_id);

CREATE INDEX IF NOT EXISTS candidate_project_links_project_idx
    ON candidate_project_links (org_id, linked_project_id, status, confidence DESC);
