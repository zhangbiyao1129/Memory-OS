ALTER TABLE orgs ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE projects ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';

CREATE INDEX IF NOT EXISTS orgs_status_idx ON orgs (status);
CREATE INDEX IF NOT EXISTS projects_org_status_idx ON projects (org_id, status);
