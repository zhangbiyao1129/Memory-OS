package tenant

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

func (r *PGRepository) CreateUser(user User) (User, error) {
	if r == nil || r.pool == nil {
		return User{}, errors.New("tenant postgres repository is not configured")
	}
	email := strings.ToLower(strings.TrimSpace(user.Email))
	if email == "" {
		return User{}, errors.New("email is required")
	}
	if user.Status == "" {
		user.Status = "active"
	}
	row := r.pool.QueryRow(context.Background(), `
INSERT INTO users (email, display_name, status)
VALUES ($1, $2, $3)
RETURNING id::text, email, display_name, status`, email, user.DisplayName, user.Status)
	return scanUser(row)
}

func (r *PGRepository) FindUserByEmail(email string) (User, error) {
	if r == nil || r.pool == nil {
		return User{}, errors.New("tenant postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
SELECT id::text, email, display_name, status
FROM users
WHERE lower(email) = lower($1)`, strings.TrimSpace(email))
	user, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, errors.New("user not found")
	}
	return user, err
}

func (r *PGRepository) ListUsers(status string) ([]User, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("tenant postgres repository is not configured")
	}
	rows, err := r.pool.Query(context.Background(), `
SELECT id::text, email, display_name, status
FROM users
WHERE ($1::text = '' OR status = $1)
ORDER BY created_at, id`, strings.TrimSpace(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, user)
	}
	return items, rows.Err()
}

func (r *PGRepository) UpdateUserStatus(userID, status string) (User, error) {
	if r == nil || r.pool == nil {
		return User{}, errors.New("tenant postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
UPDATE users
SET status = $2,
    updated_at = now()
WHERE id = $1::uuid
RETURNING id::text, email, display_name, status`, userID, status)
	user, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, errors.New("user not found")
	}
	return user, err
}

func (r *PGRepository) CreateOrg(org Org) (Org, error) {
	if r == nil || r.pool == nil {
		return Org{}, errors.New("tenant postgres repository is not configured")
	}
	slug := strings.TrimSpace(org.Slug)
	if slug == "" {
		return Org{}, errors.New("org slug is required")
	}
	if org.Status == "" {
		org.Status = "active"
	}
	row := r.pool.QueryRow(context.Background(), `
INSERT INTO orgs (name, slug, status)
VALUES ($1, $2, $3)
RETURNING id::text, name, slug, status`, org.Name, slug, org.Status)
	return scanOrg(row)
}

func (r *PGRepository) CreateProject(project Project) (Project, error) {
	if r == nil || r.pool == nil {
		return Project{}, errors.New("tenant postgres repository is not configured")
	}
	slug := strings.TrimSpace(project.Slug)
	if slug == "" {
		return Project{}, errors.New("project slug is required")
	}
	if project.Status == "" {
		project.Status = "active"
	}
	row := r.pool.QueryRow(context.Background(), `
INSERT INTO projects (org_id, name, slug, status)
VALUES ($1::uuid, $2, $3, $4)
RETURNING id::text, org_id::text, name, slug, status, COALESCE(source_type, ''), COALESCE(source_key, '')`, project.OrgID, project.Name, slug, project.Status)
	return scanProject(row)
}

func (r *PGRepository) AddMembership(membership Membership) error {
	if r == nil || r.pool == nil {
		return errors.New("tenant postgres repository is not configured")
	}
	if membership.Status == "" {
		membership.Status = "active"
	}
	if membership.Role == "" {
		membership.Role = RoleMember
	}
	if membership.ProjectID != "" {
		project, err := r.GetProject(membership.ProjectID)
		if err != nil {
			return err
		}
		if project.OrgID != membership.OrgID {
			return errors.New("project does not belong to org")
		}
	}
	roleID, err := r.ensureRole(membership.Role)
	if err != nil {
		return err
	}
	var projectID any
	if membership.ProjectID != "" {
		projectID = membership.ProjectID
	}
	_, err = r.pool.Exec(context.Background(), `
INSERT INTO memberships (user_id, org_id, project_id, role_id, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5)`,
		membership.UserID, membership.OrgID, projectID, roleID, membership.Status)
	return err
}

func (r *PGRepository) EnsurePersonalOrg(userID string) (Org, error) {
	if r == nil || r.pool == nil {
		return Org{}, errors.New("tenant postgres repository is not configured")
	}
	if strings.TrimSpace(userID) == "" {
		return Org{}, errors.New("user id is required")
	}
	slug := personalOrgSlug(userID)
	row := r.pool.QueryRow(context.Background(), `
INSERT INTO orgs (name, slug, status)
VALUES ('个人工作区', $1, 'active')
ON CONFLICT (lower(slug)) DO UPDATE SET updated_at = now()
RETURNING id::text, name, slug, status`, slug)
	return scanOrg(row)
}

func (r *PGRepository) EnsureProjectForSource(project Project) (Project, error) {
	if r == nil || r.pool == nil {
		return Project{}, errors.New("tenant postgres repository is not configured")
	}
	sourceType := strings.ToLower(strings.TrimSpace(project.SourceType))
	sourceKey := strings.ToLower(strings.TrimSpace(project.SourceKey))
	slug := strings.TrimSpace(project.Slug)
	if sourceType == "" || sourceKey == "" {
		return Project{}, errors.New("project source is required")
	}
	if slug == "" {
		return Project{}, errors.New("project slug is required")
	}
	if project.Status == "" {
		project.Status = "active"
	}
	row := r.pool.QueryRow(context.Background(), `
INSERT INTO projects (org_id, name, slug, status, source_type, source_key)
VALUES ($1::uuid, $2, $3, $4, $5, $6)
ON CONFLICT (source_type, source_key) WHERE source_type IS NOT NULL AND source_key IS NOT NULL
DO UPDATE SET updated_at = now()
RETURNING id::text, org_id::text, name, slug, status, COALESCE(source_type, ''), COALESCE(source_key, '')`,
		project.OrgID, project.Name, slug, project.Status, sourceType, sourceKey)
	return scanProject(row)
}

func (r *PGRepository) EnsureMembership(membership Membership) error {
	if r == nil || r.pool == nil {
		return errors.New("tenant postgres repository is not configured")
	}
	if membership.Status == "" {
		membership.Status = "active"
	}
	if membership.Role == "" {
		membership.Role = RoleMember
	}
	roleID, err := r.ensureRole(membership.Role)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(context.Background(), `
INSERT INTO memberships (user_id, org_id, project_id, role_id, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5)
ON CONFLICT (user_id, org_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid))
DO UPDATE SET role_id = EXCLUDED.role_id, status = EXCLUDED.status, updated_at = now()`,
		membership.UserID, membership.OrgID, emptyToNil(membership.ProjectID), roleID, membership.Status)
	return err
}

func (r *PGRepository) GetProject(projectID string) (Project, error) {
	if r == nil || r.pool == nil {
		return Project{}, errors.New("tenant postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
SELECT id::text, org_id::text, name, slug, status, COALESCE(source_type, ''), COALESCE(source_key, '')
FROM projects
WHERE id = $1::uuid AND status = 'active'`, projectID)
	project, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, errors.New("project not found")
	}
	return project, err
}

func (r *PGRepository) UpdateOrg(org Org) (Org, error) {
	if r == nil || r.pool == nil {
		return Org{}, errors.New("tenant postgres repository is not configured")
	}
	slug := strings.TrimSpace(org.Slug)
	if strings.TrimSpace(org.Name) == "" {
		return Org{}, errors.New("org name is required")
	}
	if slug == "" {
		return Org{}, errors.New("org slug is required")
	}
	row := r.pool.QueryRow(context.Background(), `
UPDATE orgs
SET name = $2,
    slug = $3,
    updated_at = now()
WHERE id = $1::uuid AND status = 'active'
RETURNING id::text, name, slug, status`, org.ID, org.Name, slug)
	updated, err := scanOrg(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, errors.New("org not found")
	}
	return updated, err
}

func (r *PGRepository) UpdateProject(project Project) (Project, error) {
	if r == nil || r.pool == nil {
		return Project{}, errors.New("tenant postgres repository is not configured")
	}
	slug := strings.TrimSpace(project.Slug)
	if strings.TrimSpace(project.Name) == "" {
		return Project{}, errors.New("project name is required")
	}
	if slug == "" {
		return Project{}, errors.New("project slug is required")
	}
	row := r.pool.QueryRow(context.Background(), `
UPDATE projects
SET name = $3,
    slug = $4,
    updated_at = now()
WHERE id = $1::uuid
  AND org_id = $2::uuid
  AND status = 'active'
  AND EXISTS (
      SELECT 1
      FROM orgs
      WHERE orgs.id = projects.org_id
        AND orgs.status = 'active'
  )
RETURNING id::text, org_id::text, name, slug, status, COALESCE(source_type, ''), COALESCE(source_key, '')`, project.ID, project.OrgID, project.Name, slug)
	updated, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, errors.New("project not found")
	}
	return updated, err
}

func (r *PGRepository) DeleteOrg(orgID string) (Org, error) {
	if r == nil || r.pool == nil {
		return Org{}, errors.New("tenant postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
UPDATE orgs
SET status = 'deleted',
    updated_at = now()
WHERE id = $1::uuid AND status <> 'deleted'
RETURNING id::text, name, slug, status`, orgID)
	org, err := scanOrg(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, errors.New("org not found")
	}
	return org, err
}

func (r *PGRepository) DeleteProject(orgID, projectID string) (Project, error) {
	if r == nil || r.pool == nil {
		return Project{}, errors.New("tenant postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
UPDATE projects
SET status = 'deleted',
    updated_at = now()
WHERE id = $1::uuid
  AND org_id = $2::uuid
  AND status <> 'deleted'
RETURNING id::text, org_id::text, name, slug, status, COALESCE(source_type, ''), COALESCE(source_key, '')`, projectID, orgID)
	project, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, errors.New("project not found")
	}
	return project, err
}

func (r *PGRepository) UpdateMembershipRole(membership Membership) (Membership, error) {
	if r == nil || r.pool == nil {
		return Membership{}, errors.New("tenant postgres repository is not configured")
	}
	roleID, err := r.ensureRole(membership.Role)
	if err != nil {
		return Membership{}, err
	}
	row := r.pool.QueryRow(context.Background(), `
UPDATE memberships
SET role_id = $4::uuid,
    updated_at = now()
WHERE user_id = $1::uuid
  AND org_id = $2::uuid
  AND (($3::uuid IS NULL AND project_id IS NULL) OR project_id = $3::uuid)
  AND status = 'active'
RETURNING user_id::text, org_id::text, COALESCE(project_id::text, ''), $5::text, status`,
		membership.UserID, membership.OrgID, emptyToNil(membership.ProjectID), roleID, membership.Role)
	updated, err := scanMembership(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, errors.New("membership not found")
	}
	return updated, err
}

func (r *PGRepository) RemoveMembership(userID, orgID, projectID string) (Membership, error) {
	if r == nil || r.pool == nil {
		return Membership{}, errors.New("tenant postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
UPDATE memberships
SET status = 'disabled',
    updated_at = now()
WHERE user_id = $1::uuid
  AND org_id = $2::uuid
  AND (($3::uuid IS NULL AND project_id IS NULL) OR project_id = $3::uuid)
  AND status <> 'disabled'
RETURNING user_id::text, org_id::text, COALESCE(project_id::text, ''), (
    SELECT roles.name FROM roles WHERE roles.id = memberships.role_id
), status`, userID, orgID, emptyToNil(projectID))
	removed, err := scanMembership(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, errors.New("membership not found")
	}
	return removed, err
}

func (r *PGRepository) FindMembership(userID, orgID, projectID string) (Membership, error) {
	if r == nil || r.pool == nil {
		return Membership{}, errors.New("tenant postgres repository is not configured")
	}
	membership, err := r.findMembership(userID, orgID, projectID)
	if err == nil {
		return membership, nil
	}
	if projectID != "" && errors.Is(err, pgx.ErrNoRows) {
		return r.findMembership(userID, orgID, "")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, errors.New("membership not found")
	}
	return Membership{}, err
}

func (r *PGRepository) ListOrgs(userID string) ([]Org, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("tenant postgres repository is not configured")
	}
	rows, err := r.pool.Query(context.Background(), `
SELECT DISTINCT o.id::text, o.name, o.slug, o.status
FROM orgs o
JOIN memberships m ON m.org_id = o.id
WHERE m.user_id = $1::uuid AND m.status = 'active' AND o.status = 'active'
ORDER BY o.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Org{}
	for rows.Next() {
		org, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, org)
	}
	return items, rows.Err()
}

func (r *PGRepository) ListProjects(userID, orgID string) ([]Project, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("tenant postgres repository is not configured")
	}
	rows, err := r.pool.Query(context.Background(), `
SELECT DISTINCT p.id::text, p.org_id::text, p.name, p.slug, p.status, COALESCE(p.source_type, ''), COALESCE(p.source_key, '')
FROM projects p
JOIN memberships m ON m.org_id = p.org_id AND (m.project_id = p.id OR m.project_id IS NULL)
JOIN orgs o ON o.id = p.org_id
WHERE m.user_id = $1::uuid AND p.org_id = $2::uuid AND m.status = 'active' AND p.status = 'active' AND o.status = 'active'
ORDER BY p.name`, userID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Project{}
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, project)
	}
	return items, rows.Err()
}

func (r *PGRepository) ListMemberships(orgID, projectID string) ([]Membership, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("tenant postgres repository is not configured")
	}
	rows, err := r.pool.Query(context.Background(), `
SELECT m.user_id::text, m.org_id::text, COALESCE(m.project_id::text, ''), roles.name, m.status
FROM memberships m
JOIN roles ON roles.id = m.role_id
WHERE m.org_id = $1::uuid
  AND ($2::uuid IS NULL OR m.project_id = $2::uuid)
ORDER BY m.created_at DESC`, orgID, emptyToNil(projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Membership{}
	for rows.Next() {
		membership, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, membership)
	}
	return items, rows.Err()
}

func (r *PGRepository) ListRoleDefinitions() ([]RoleDefinition, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("tenant postgres repository is not configured")
	}
	rows, err := r.pool.Query(context.Background(), `
SELECT name, permission_labels
FROM roles
ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []RoleDefinition{}
	for rows.Next() {
		definition, err := scanRoleDefinition(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, definition)
	}
	return items, rows.Err()
}

func (r *PGRepository) UpsertRoleDefinition(role RoleDefinition) (RoleDefinition, error) {
	if r == nil || r.pool == nil {
		return RoleDefinition{}, errors.New("tenant postgres repository is not configured")
	}
	name := strings.TrimSpace(strings.ToLower(role.Role))
	if name == "" {
		return RoleDefinition{}, errors.New("role name is required")
	}
	permissionLabels := make([]string, 0, len(role.PermissionLabels))
	for _, label := range role.PermissionLabels {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			permissionLabels = append(permissionLabels, trimmed)
		}
	}
	if len(permissionLabels) == 0 {
		return RoleDefinition{}, errors.New("role permission labels are required")
	}

	var definition RoleDefinition
	row := r.pool.QueryRow(context.Background(), `
INSERT INTO roles (name, permission_labels)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET permission_labels = EXCLUDED.permission_labels
RETURNING name, permission_labels`, name, permissionLabels)
	err := row.Scan(&definition.Role, &definition.PermissionLabels)
	if err != nil {
		return RoleDefinition{}, err
	}
	return definition, nil
}

func (r *PGRepository) findMembership(userID, orgID, projectID string) (Membership, error) {
	query := `
SELECT m.user_id::text, m.org_id::text, COALESCE(m.project_id::text, ''), roles.name, m.status
FROM memberships m
JOIN roles ON roles.id = m.role_id
WHERE m.user_id = $1::uuid AND m.org_id = $2::uuid AND `
	args := []any{userID, orgID}
	if projectID == "" {
		query += "m.project_id IS NULL"
	} else {
		query += "m.project_id = $3::uuid"
		args = append(args, projectID)
	}
	row := r.pool.QueryRow(context.Background(), query, args...)
	var membership Membership
	err := row.Scan(&membership.UserID, &membership.OrgID, &membership.ProjectID, &membership.Role, &membership.Status)
	return membership, err
}

func scanMembership(row userScanner) (Membership, error) {
	var membership Membership
	err := row.Scan(&membership.UserID, &membership.OrgID, &membership.ProjectID, &membership.Role, &membership.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, pgx.ErrNoRows
	}
	return membership, err
}

func (r *PGRepository) ensureRole(name string) (string, error) {
	var id string
	err := r.pool.QueryRow(context.Background(), `
INSERT INTO roles (name, permission_labels)
VALUES ($1, '{}')
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id::text`, name).Scan(&id)
	return id, err
}

type userScanner interface {
	Scan(dest ...any) error
}

func scanUser(row userScanner) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &user.Status)
	return user, err
}

func scanOrg(row userScanner) (Org, error) {
	var org Org
	err := row.Scan(&org.ID, &org.Name, &org.Slug, &org.Status)
	return org, err
}

func scanProject(row userScanner) (Project, error) {
	var project Project
	err := row.Scan(&project.ID, &project.OrgID, &project.Name, &project.Slug, &project.Status, &project.SourceType, &project.SourceKey)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, pgx.ErrNoRows
	}
	return project, err
}

func scanRoleDefinition(row userScanner) (RoleDefinition, error) {
	var definition RoleDefinition
	err := row.Scan(&definition.Role, &definition.PermissionLabels)
	return definition, err
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}
