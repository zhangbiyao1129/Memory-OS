-- 000028_candidate_maintenance_run_metrics.sql
-- 候选整理运行记录细分统计: 保留历史字段,新增非破坏性审计列。
ALTER TABLE candidate_maintenance_runs ADD COLUMN IF NOT EXISTS archive_material INTEGER NOT NULL DEFAULT 0;
ALTER TABLE candidate_maintenance_runs ADD COLUMN IF NOT EXISTS promoted_hot INTEGER NOT NULL DEFAULT 0;
ALTER TABLE candidate_maintenance_runs ADD COLUMN IF NOT EXISTS needs_review INTEGER NOT NULL DEFAULT 0;
ALTER TABLE candidate_maintenance_runs ADD COLUMN IF NOT EXISTS hot_memory_demoted INTEGER NOT NULL DEFAULT 0;
