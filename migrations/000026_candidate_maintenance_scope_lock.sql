-- 000026_candidate_maintenance_scope_lock.sql
-- 将候选清洗整合 running 防重入从项目级收窄到 source/thread 级。
-- 自动整理允许同一项目不同来源或线程并行排队,但同一 scope 仍保持幂等。

DROP INDEX IF EXISTS candidate_maintenance_runs_running_project_unique;

CREATE UNIQUE INDEX IF NOT EXISTS candidate_maintenance_runs_running_scope_unique
    ON candidate_maintenance_runs (org_id, project_id, source_key, thread_id)
    WHERE status = 'running';
