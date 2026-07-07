WITH ranked AS (
    SELECT
        memory_id,
        first_value(memory_id) OVER (
            PARTITION BY org_id, project_id, user_id, scope, fact_hash
            ORDER BY created_at ASC, id ASC
        ) AS keep_memory_id,
        row_number() OVER (
            PARTITION BY org_id, project_id, user_id, scope, fact_hash
            ORDER BY created_at ASC, id ASC
        ) AS rn
    FROM hot_memories
    WHERE deleted_at IS NULL
),
duplicate_totals AS (
    SELECT
        ranked.keep_memory_id,
        max(hot_memories.confidence) AS max_confidence,
        max(hot_memories.hot_score) AS max_hot_score,
        sum(hot_memories.access_count) AS access_count,
        sum(hot_memories.returned_count) AS returned_count,
        sum(hot_memories.used_count) AS used_count,
        max(hot_memories.last_accessed_at) AS last_accessed_at,
        max(hot_memories.last_returned_at) AS last_returned_at,
        max(hot_memories.last_used_at) AS last_used_at
    FROM ranked
    JOIN hot_memories ON hot_memories.memory_id = ranked.memory_id
    WHERE ranked.rn > 1
    GROUP BY ranked.keep_memory_id
),
merged_winners AS (
    UPDATE hot_memories AS keep
    SET confidence = GREATEST(keep.confidence, duplicate_totals.max_confidence),
        hot_score = GREATEST(keep.hot_score, duplicate_totals.max_hot_score),
        access_count = keep.access_count + duplicate_totals.access_count,
        returned_count = keep.returned_count + duplicate_totals.returned_count,
        used_count = keep.used_count + duplicate_totals.used_count,
        last_accessed_at = GREATEST(keep.last_accessed_at, duplicate_totals.last_accessed_at),
        last_returned_at = GREATEST(keep.last_returned_at, duplicate_totals.last_returned_at),
        last_used_at = GREATEST(keep.last_used_at, duplicate_totals.last_used_at),
        updated_at = now()
    FROM duplicate_totals
    WHERE keep.memory_id = duplicate_totals.keep_memory_id
    RETURNING keep.memory_id
)
UPDATE hot_memories
SET status = 'deleted',
    deleted_at = now(),
    updated_at = now()
FROM ranked
WHERE hot_memories.memory_id = ranked.memory_id
  AND ranked.rn > 1;

DROP INDEX IF EXISTS hot_memories_scope_fact_unique;

CREATE UNIQUE INDEX IF NOT EXISTS hot_memories_scope_fact_unique
    ON hot_memories (org_id, project_id, user_id, scope, fact_hash)
    WHERE deleted_at IS NULL;
