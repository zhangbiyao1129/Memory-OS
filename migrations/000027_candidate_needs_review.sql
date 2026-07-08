-- 000027_candidate_needs_review.sql
-- 候选记忆 needs_review 标记: AI 整理判定为待人工确认的候选置 true。
-- 仅新增列,不删除/修改旧字段(硬规则: 不做破坏性清理)。
ALTER TABLE candidate_memories
    ADD COLUMN IF NOT EXISTS needs_review BOOLEAN NOT NULL DEFAULT FALSE;

-- 待确认候选查询索引。
CREATE INDEX IF NOT EXISTS idx_candidate_memories_needs_review
    ON candidate_memories (org_id, project_id, needs_review)
    WHERE needs_review;
