-- 000021_candidate_maintenance_progress.sql
-- 给 candidate_maintenance_runs 增加 stage 和 total_candidates 字段,
-- 支持清洗进度持久化追踪。

ALTER TABLE candidate_maintenance_runs
    ADD COLUMN IF NOT EXISTS stage TEXT NOT NULL DEFAULT 'queued',
    ADD COLUMN IF NOT EXISTS total_candidates INTEGER NOT NULL DEFAULT 0;
