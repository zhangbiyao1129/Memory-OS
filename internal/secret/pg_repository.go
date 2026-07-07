package secret

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

func (r *PGRepository) Save(meta Metadata, version Version) error {
	if r == nil || r.pool == nil {
		return errors.New("secret postgres repository is not configured")
	}
	if meta.SecretRef == "" {
		return errors.New("secret ref is required")
	}
	tx, err := r.pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck

	var secretID string
	err = tx.QueryRow(context.Background(), `
INSERT INTO secrets (secret_ref, owner_user_id, org_id, project_id, name, env_name, site, purpose, expires_at, status, current_version)
VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, $9, $10, $11)
RETURNING id::text`,
		meta.SecretRef,
		meta.OwnerUserID,
		emptyToNil(meta.OrgID),
		emptyToNil(meta.ProjectID),
		meta.Name,
		meta.EnvName,
		meta.Site,
		meta.Purpose,
		meta.ExpiresAt,
		meta.Status,
		meta.CurrentVersion,
	).Scan(&secretID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(context.Background(), `
INSERT INTO secret_versions (secret_id, version, key_id, algorithm, device_key_id, key_fingerprint, nonce, ciphertext)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)`,
		secretID,
		version.Version,
		version.Blob.DeviceKeyID,
		version.Blob.Algorithm,
		version.Blob.DeviceKeyID,
		version.Blob.KeyFingerprint,
		version.Blob.Nonce,
		version.Blob.Ciphertext,
	)
	if err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

func (r *PGRepository) GetMetadata(secretRef string) (Metadata, error) {
	if r == nil || r.pool == nil {
		return Metadata{}, errors.New("secret postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
SELECT secret_ref, owner_user_id::text, COALESCE(org_id::text, ''), COALESCE(project_id::text, ''), name, COALESCE(env_name, ''), COALESCE(site, ''), COALESCE(purpose, ''), expires_at, status, current_version
FROM secrets
WHERE secret_ref = $1`, secretRef)
	meta, err := scanMetadata(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Metadata{}, errors.New("secret not found")
	}
	return meta, err
}

func (r *PGRepository) List(filter ListFilter) ([]Metadata, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("secret postgres repository is not configured")
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	rows, err := r.pool.Query(context.Background(), `
SELECT secret_ref, owner_user_id::text, COALESCE(org_id::text, ''), COALESCE(project_id::text, ''), name, COALESCE(env_name, ''), COALESCE(site, ''), COALESCE(purpose, ''), expires_at, status, current_version
FROM secrets
WHERE owner_user_id = $1::uuid
  AND ($2::uuid IS NULL OR org_id = $2::uuid)
  AND ($3::uuid IS NULL OR project_id = $3::uuid)
  AND ($4 = '' OR status = $4)
ORDER BY created_at DESC
LIMIT $5`, filter.OwnerUserID, emptyToNil(filter.OrgID), emptyToNil(filter.ProjectID), filter.Status, filter.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Metadata{}
	for rows.Next() {
		meta, err := scanMetadata(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *PGRepository) GetCurrentVersion(secretRef string) (Version, error) {
	if r == nil || r.pool == nil {
		return Version{}, errors.New("secret postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
SELECT s.secret_ref, v.version, v.algorithm, v.device_key_id, v.key_fingerprint, v.nonce, v.ciphertext
FROM secrets s
JOIN secret_versions v ON v.secret_id = s.id AND v.version = s.current_version
WHERE s.secret_ref = $1`, secretRef)
	var version Version
	err := row.Scan(&version.SecretRef, &version.Version, &version.Blob.Algorithm, &version.Blob.DeviceKeyID, &version.Blob.KeyFingerprint, &version.Blob.Nonce, &version.Blob.Ciphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, errors.New("secret version not found")
	}
	return version, err
}

func (r *PGRepository) Disable(secretRef string) error {
	if r == nil || r.pool == nil {
		return errors.New("secret postgres repository is not configured")
	}
	tag, err := r.pool.Exec(context.Background(), `UPDATE secrets SET status = 'disabled', updated_at = now() WHERE secret_ref = $1`, secretRef)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("secret not found")
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMetadata(row rowScanner) (Metadata, error) {
	var meta Metadata
	var expiresAt *time.Time
	err := row.Scan(&meta.SecretRef, &meta.OwnerUserID, &meta.OrgID, &meta.ProjectID, &meta.Name, &meta.EnvName, &meta.Site, &meta.Purpose, &expiresAt, &meta.Status, &meta.CurrentVersion)
	if err != nil {
		return Metadata{}, err
	}
	meta.ExpiresAt = expiresAt
	return meta, nil
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}
