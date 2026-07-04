package auth

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

func (r *PGRepository) SetPasswordHash(userID, passwordHash string) error {
	if r == nil || r.pool == nil {
		return errors.New("auth postgres repository is not configured")
	}
	if userID == "" || passwordHash == "" {
		return errors.New("user id and password hash are required")
	}
	_, err := r.pool.Exec(context.Background(), `
INSERT INTO password_credentials (user_id, password_hash, updated_at)
VALUES ($1::uuid, $2, now())
ON CONFLICT (user_id)
DO UPDATE SET password_hash = EXCLUDED.password_hash, updated_at = now()`, userID, passwordHash)
	return err
}

func (r *PGRepository) GetPasswordHash(userID string) (string, error) {
	if r == nil || r.pool == nil {
		return "", errors.New("auth postgres repository is not configured")
	}
	var hash string
	err := r.pool.QueryRow(context.Background(), `SELECT password_hash FROM password_credentials WHERE user_id = $1::uuid`, userID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("password credential not found")
	}
	return hash, err
}

func (r *PGRepository) SavePAT(record PATRecord) error {
	if r == nil || r.pool == nil {
		return errors.New("auth postgres repository is not configured")
	}
	if record.TokenHash == "" {
		return errors.New("token hash is required")
	}
	_, err := r.pool.Exec(context.Background(), `
INSERT INTO personal_access_tokens (user_id, name, token_prefix, token_hash, scopes, expires_at, revoked_at)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)`,
		record.SubjectID, record.Name, record.TokenPrefix, record.TokenHash, record.Scopes, record.ExpiresAt, record.RevokedAt)
	return err
}

func (r *PGRepository) FindPATByHash(tokenHash string) (PATRecord, error) {
	if r == nil || r.pool == nil {
		return PATRecord{}, errors.New("auth postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
SELECT id::text, user_id::text, name, token_prefix, token_hash, scopes, expires_at, revoked_at
FROM personal_access_tokens
WHERE token_hash = $1`, tokenHash)
	var record PATRecord
	err := row.Scan(&record.ID, &record.SubjectID, &record.Name, &record.TokenPrefix, &record.TokenHash, &record.Scopes, &record.ExpiresAt, &record.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PATRecord{}, errors.New("pat not found")
	}
	return record, err
}

func (r *PGRepository) GetPAT(id string) (PATRecord, error) {
	if r == nil || r.pool == nil {
		return PATRecord{}, errors.New("auth postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
SELECT id::text, user_id::text, name, token_prefix, token_hash, scopes, expires_at, revoked_at
FROM personal_access_tokens
WHERE id = $1::uuid`, id)
	var record PATRecord
	err := row.Scan(&record.ID, &record.SubjectID, &record.Name, &record.TokenPrefix, &record.TokenHash, &record.Scopes, &record.ExpiresAt, &record.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PATRecord{}, errors.New("pat not found")
	}
	return record, err
}

func (r *PGRepository) ListPATs(filter TokenListFilter) ([]PATRecord, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("auth postgres repository is not configured")
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	rows, err := r.pool.Query(context.Background(), `
SELECT id::text, user_id::text, name, token_prefix, token_hash, scopes, expires_at, revoked_at
FROM personal_access_tokens
WHERE user_id = $1::uuid
  AND ($2 = '' OR ($2 = 'active' AND revoked_at IS NULL) OR ($2 = 'revoked' AND revoked_at IS NOT NULL))
ORDER BY created_at DESC
LIMIT $3`, filter.UserID, filter.Status, filter.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []PATRecord{}
	for rows.Next() {
		var record PATRecord
		if err := rows.Scan(&record.ID, &record.SubjectID, &record.Name, &record.TokenPrefix, &record.TokenHash, &record.Scopes, &record.ExpiresAt, &record.RevokedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *PGRepository) RevokePAT(id string, revokedAt time.Time) error {
	if r == nil || r.pool == nil {
		return errors.New("auth postgres repository is not configured")
	}
	tag, err := r.pool.Exec(context.Background(), `UPDATE personal_access_tokens SET revoked_at = $2 WHERE id = $1::uuid`, id, revokedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("pat not found")
	}
	return nil
}

func (r *PGRepository) SaveAdapterToken(record AdapterTokenRecord) error {
	if r == nil || r.pool == nil {
		return errors.New("auth postgres repository is not configured")
	}
	if record.TokenHash == "" {
		return errors.New("token hash is required")
	}
	_, err := r.pool.Exec(context.Background(), `
INSERT INTO adapter_tokens (user_id, org_id, project_id, agent_id, token_prefix, token_hash, scopes, expires_at, revoked_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9)`,
		record.UserID, record.OrgID, record.ProjectID, record.AgentID, record.TokenPrefix, record.TokenHash, record.Scopes, record.ExpiresAt, record.RevokedAt)
	return err
}

func (r *PGRepository) FindAdapterTokenByHash(tokenHash string) (AdapterTokenRecord, error) {
	if r == nil || r.pool == nil {
		return AdapterTokenRecord{}, errors.New("auth postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
SELECT id::text, user_id::text, org_id::text, project_id::text, agent_id, token_prefix, token_hash, scopes, expires_at, revoked_at
FROM adapter_tokens
WHERE token_hash = $1`, tokenHash)
	var record AdapterTokenRecord
	err := row.Scan(&record.ID, &record.UserID, &record.OrgID, &record.ProjectID, &record.AgentID, &record.TokenPrefix, &record.TokenHash, &record.Scopes, &record.ExpiresAt, &record.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdapterTokenRecord{}, errors.New("adapter token not found")
	}
	return record, err
}

func (r *PGRepository) GetAdapterToken(id string) (AdapterTokenRecord, error) {
	if r == nil || r.pool == nil {
		return AdapterTokenRecord{}, errors.New("auth postgres repository is not configured")
	}
	row := r.pool.QueryRow(context.Background(), `
SELECT id::text, user_id::text, org_id::text, project_id::text, agent_id, token_prefix, token_hash, scopes, expires_at, revoked_at
FROM adapter_tokens
WHERE id = $1::uuid`, id)
	var record AdapterTokenRecord
	err := row.Scan(&record.ID, &record.UserID, &record.OrgID, &record.ProjectID, &record.AgentID, &record.TokenPrefix, &record.TokenHash, &record.Scopes, &record.ExpiresAt, &record.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdapterTokenRecord{}, errors.New("adapter token not found")
	}
	return record, err
}

func (r *PGRepository) ListAdapterTokens(filter AdapterTokenListFilter) ([]AdapterTokenRecord, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("auth postgres repository is not configured")
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	rows, err := r.pool.Query(context.Background(), `
SELECT id::text, user_id::text, org_id::text, project_id::text, agent_id, token_prefix, token_hash, scopes, expires_at, revoked_at
FROM adapter_tokens
WHERE user_id = $1::uuid
  AND ($2::uuid IS NULL OR org_id = $2::uuid)
  AND ($3::uuid IS NULL OR project_id = $3::uuid)
  AND ($4 = '' OR agent_id = $4)
  AND ($5 = '' OR ($5 = 'active' AND revoked_at IS NULL) OR ($5 = 'revoked' AND revoked_at IS NOT NULL))
ORDER BY created_at DESC
LIMIT $6`, filter.UserID, emptyToNil(filter.OrgID), emptyToNil(filter.ProjectID), filter.AgentID, filter.Status, filter.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []AdapterTokenRecord{}
	for rows.Next() {
		var record AdapterTokenRecord
		if err := rows.Scan(&record.ID, &record.UserID, &record.OrgID, &record.ProjectID, &record.AgentID, &record.TokenPrefix, &record.TokenHash, &record.Scopes, &record.ExpiresAt, &record.RevokedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *PGRepository) RevokeAdapterToken(id string, revokedAt time.Time) error {
	if r == nil || r.pool == nil {
		return errors.New("auth postgres repository is not configured")
	}
	tag, err := r.pool.Exec(context.Background(), `UPDATE adapter_tokens SET revoked_at = $2 WHERE id = $1::uuid`, id, revokedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("adapter token not found")
	}
	return nil
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}
