ALTER TABLE hot_memories
    ADD COLUMN IF NOT EXISTS returned_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_accessed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_returned_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS hot_memories_pinned_score_idx
    ON hot_memories (pinned DESC, hot_score DESC);

CREATE INDEX IF NOT EXISTS hot_memories_accessed_idx
    ON hot_memories (last_accessed_at DESC);

CREATE INDEX IF NOT EXISTS hot_memories_returned_idx
    ON hot_memories (last_returned_at DESC);

CREATE INDEX IF NOT EXISTS hot_memories_used_idx
    ON hot_memories (last_used_at DESC);
