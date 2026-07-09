CREATE TABLE IF NOT EXISTS memory_units (
    id BIGSERIAL PRIMARY KEY,
    unit_id TEXT NOT NULL UNIQUE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_key TEXT NOT NULL DEFAULT '',
    thread_id TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL,
    agent_id TEXT NOT NULL DEFAULT '',
    unit_type TEXT NOT NULL,
    content TEXT NOT NULL,
    applies_when TEXT NOT NULL DEFAULT '',
    agent_should TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'current',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_level TEXT NOT NULL DEFAULT 'low',
    source_refs JSONB NOT NULL DEFAULT '[]',
    superseded_by TEXT NOT NULL DEFAULT '',
    valid_from TIMESTAMPTZ,
    valid_to TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_units_scope_status_idx
    ON memory_units (org_id, project_id, source_key, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS memory_units_type_status_idx
    ON memory_units (unit_type, status, trust_score DESC);

CREATE TABLE IF NOT EXISTS memory_claims (
    id BIGSERIAL PRIMARY KEY,
    claim_id TEXT NOT NULL UNIQUE,
    unit_id TEXT NOT NULL REFERENCES memory_units(unit_id) ON DELETE CASCADE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    predicate TEXT NOT NULL,
    value TEXT NOT NULL,
    polarity TEXT NOT NULL DEFAULT 'positive',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    evidence_refs JSONB NOT NULL DEFAULT '[]',
    observed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS memory_claims_unit_subject_predicate_unique
    ON memory_claims (unit_id, subject, predicate);

CREATE INDEX IF NOT EXISTS memory_claims_scope_lookup_idx
    ON memory_claims (org_id, project_id, subject, predicate, created_at DESC);

CREATE TABLE IF NOT EXISTS memory_governance_runs (
    id BIGSERIAL PRIMARY KEY,
    run_id TEXT NOT NULL UNIQUE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_key TEXT NOT NULL DEFAULT '',
    thread_id TEXT NOT NULL DEFAULT '',
    trigger_type TEXT NOT NULL DEFAULT 'manual',
    status TEXT NOT NULL DEFAULT 'running',
    processed_candidates INTEGER NOT NULL DEFAULT 0,
    processed_hot_memories INTEGER NOT NULL DEFAULT 0,
    processed_archives INTEGER NOT NULL DEFAULT 0,
    created_units INTEGER NOT NULL DEFAULT 0,
    superseded_units INTEGER NOT NULL DEFAULT 0,
    stale_candidates INTEGER NOT NULL DEFAULT 0,
    demoted_hot_memories INTEGER NOT NULL DEFAULT 0,
    ci_cases_created INTEGER NOT NULL DEFAULT 0,
    ci_cases_passed INTEGER NOT NULL DEFAULT 0,
    correction_archive_id TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_governance_runs_scope_idx
    ON memory_governance_runs (org_id, project_id, source_key, thread_id, started_at DESC);

CREATE INDEX IF NOT EXISTS memory_governance_runs_status_idx
    ON memory_governance_runs (status, updated_at DESC);

CREATE TABLE IF NOT EXISTS memory_governance_actions (
    id BIGSERIAL PRIMARY KEY,
    action_id TEXT NOT NULL UNIQUE,
    run_id TEXT NOT NULL REFERENCES memory_governance_runs(run_id) ON DELETE CASCADE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    action TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    evidence_refs JSONB NOT NULL DEFAULT '[]',
    applied BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_governance_actions_target_idx
    ON memory_governance_actions (org_id, project_id, target_type, target_id, created_at DESC);

CREATE TABLE IF NOT EXISTS memory_ci_cases (
    id BIGSERIAL PRIMARY KEY,
    case_id TEXT NOT NULL UNIQUE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_key TEXT NOT NULL DEFAULT '',
    question TEXT NOT NULL,
    must_include TEXT[] NOT NULL DEFAULT '{}',
    must_not_include TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'active',
    source_run_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_ci_cases_scope_status_idx
    ON memory_ci_cases (org_id, project_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS memory_ci_results (
    id BIGSERIAL PRIMARY KEY,
    result_id TEXT NOT NULL UNIQUE,
    case_id TEXT NOT NULL REFERENCES memory_ci_cases(case_id) ON DELETE CASCADE,
    run_id TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    passed BOOLEAN NOT NULL DEFAULT false,
    matched_include TEXT[] NOT NULL DEFAULT '{}',
    matched_exclude TEXT[] NOT NULL DEFAULT '{}',
    response_excerpt TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_ci_results_case_idx
    ON memory_ci_results (case_id, created_at DESC);

-- candidate_memories 扩展字段，支持 Memory Kernel 治理状态
ALTER TABLE candidate_memories
    ADD COLUMN IF NOT EXISTS governance_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS superseded_by TEXT NOT NULL DEFAULT '';
