ALTER TABLE projects ADD COLUMN IF NOT EXISTS source_type TEXT;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS source_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS projects_source_unique
ON projects (source_type, source_key)
WHERE source_type IS NOT NULL AND source_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS projects_source_idx
ON projects (source_type, source_key);
