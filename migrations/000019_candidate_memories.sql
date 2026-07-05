-- 000019_candidate_memories.sql
-- 记忆生命周期改造 Phase 1:候选记忆领域。
-- 事件全量保存 → candidate_memories → 热记忆/待确认/主题沉淀 → Markdown Archive。
-- 三张表:
--   candidate_memories    候选记忆(提炼产物,等待分流)
--   candidate_memory_jobs 候选提炼任务(archive_jobs 风格,Redis 队列幂等重试)
--   topic_memory_states   主题沉淀状态(org+project+source_key+thread 维度)
-- source_key 源自 workspace identity(000018),用于避免同名 project 串数据。

CREATE TABLE IF NOT EXISTS candidate_memories (
    id BIGSERIAL PRIMARY KEY,
    candidate_id TEXT NOT NULL UNIQUE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_key TEXT NOT NULL,
    user_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    source_event_ids TEXT[] NOT NULL DEFAULT '{}',
    memory_type TEXT NOT NULL,
    content TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    risk_level TEXT NOT NULL DEFAULT 'low',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    similar_refs JSONB NOT NULL DEFAULT '[]',
    scores JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS candidate_memories_scope_idx
    ON candidate_memories (org_id, project_id, source_key);

CREATE INDEX IF NOT EXISTS candidate_memories_status_idx
    ON candidate_memories (status, created_at DESC);

CREATE INDEX IF NOT EXISTS candidate_memories_thread_idx
    ON candidate_memories (org_id, project_id, thread_id);

CREATE TABLE IF NOT EXISTS candidate_memory_jobs (
    id BIGSERIAL PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_key TEXT NOT NULL,
    source_event_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    locked_by TEXT,
    locked_until TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    candidate_ids TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS candidate_memory_jobs_ready_idx
    ON candidate_memory_jobs (status, locked_until, created_at);

CREATE TABLE IF NOT EXISTS topic_memory_states (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_key TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    candidate_count INTEGER NOT NULL DEFAULT 0,
    completion_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_event_at TIMESTAMPTZ,
    ready_to_compose BOOLEAN NOT NULL DEFAULT false,
    composed_archive_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS topic_memory_states_topic_unique
    ON topic_memory_states (org_id, project_id, source_key, thread_id);
