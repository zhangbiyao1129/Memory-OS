package archive

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

func (r *PGRepository) SaveCreate(metadata Metadata, version Version, eventIDs []string, requestID string) (Metadata, bool, error) {
	if r == nil || r.pool == nil {
		return Metadata{}, false, errors.New("archive postgres repository is not configured")
	}
	if metadata.ArchiveID == "" || requestID == "" {
		return Metadata{}, false, errors.New("archive id and request id are required")
	}
	tx, err := r.pool.Begin(context.Background())
	if err != nil {
		return Metadata{}, false, err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck

	if existing, ok, err := getIdempotentArchive(context.Background(), tx, requestID); err != nil {
		return Metadata{}, false, err
	} else if ok {
		return existing, true, nil
	}

	if _, err := tx.Exec(context.Background(), `
INSERT INTO archives (archive_id, user_id, org_id, project_id, title, file_path, status, index_generation, current_version, content_hash, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		metadata.ArchiveID,
		metadata.UserID,
		metadata.OrgID,
		metadata.ProjectID,
		metadata.Title,
		metadata.FilePath,
		metadata.Status,
		metadata.IndexGeneration,
		metadata.CurrentVersion,
		metadata.ContentHash,
		metadata.CreatedAt,
		metadata.UpdatedAt,
	); err != nil {
		return Metadata{}, false, err
	}
	if err := insertVersion(context.Background(), tx, version); err != nil {
		return Metadata{}, false, err
	}
	for _, eventID := range eventIDs {
		if eventID == "" {
			continue
		}
		if _, err := tx.Exec(context.Background(), `
INSERT INTO archive_events (archive_id, event_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING`, metadata.ArchiveID, eventID); err != nil {
			return Metadata{}, false, err
		}
	}
	if err := insertIndexGeneration(context.Background(), tx, metadata.ArchiveID, metadata.IndexGeneration, "initial archive"); err != nil {
		return Metadata{}, false, err
	}
	if err := insertIdempotency(context.Background(), tx, requestID, "create", metadata.ArchiveID); err != nil {
		return Metadata{}, false, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return Metadata{}, false, err
	}
	return metadata, false, nil
}

func (r *PGRepository) Get(archiveID string) (Metadata, error) {
	if r == nil || r.pool == nil {
		return Metadata{}, errors.New("archive postgres repository is not configured")
	}
	return scanMetadata(r.pool.QueryRow(context.Background(), selectArchiveMetadataSQL()+" WHERE archive_id = $1", archiveID))
}

func (r *PGRepository) List(filter ListFilter) ([]Metadata, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("archive postgres repository is not configured")
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	rows, err := r.pool.Query(context.Background(), selectArchiveMetadataSQL()+`
WHERE user_id = $1
  AND org_id = $2
  AND project_id = $3
  AND ($4 = '' OR status = $4)
ORDER BY created_at DESC
LIMIT $5`, filter.UserID, filter.OrgID, filter.ProjectID, filter.Status, filter.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	archives := []Metadata{}
	for rows.Next() {
		metadata, err := scanMetadata(rows)
		if err != nil {
			return nil, err
		}
		archives = append(archives, metadata)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return archives, nil
}

func (r *PGRepository) SaveEdit(metadata Metadata, version Version, audit EditAuditLog, requestID string) (Metadata, bool, error) {
	if r == nil || r.pool == nil {
		return Metadata{}, false, errors.New("archive postgres repository is not configured")
	}
	if metadata.ArchiveID == "" || requestID == "" {
		return Metadata{}, false, errors.New("archive id and request id are required")
	}
	tx, err := r.pool.Begin(context.Background())
	if err != nil {
		return Metadata{}, false, err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck

	if existing, ok, err := getIdempotentArchive(context.Background(), tx, requestID); err != nil {
		return Metadata{}, false, err
	} else if ok {
		return existing, true, nil
	}

	saved, err := scanMetadata(tx.QueryRow(context.Background(), `
UPDATE archives
SET title = $2,
    file_path = $3,
    status = $4,
    index_generation = $5,
    current_version = $6,
    content_hash = $7,
    updated_at = $8
WHERE archive_id = $1
RETURNING archive_id, user_id, org_id, project_id, title, file_path, status, index_generation, current_version, content_hash, created_at, updated_at`,
		metadata.ArchiveID,
		metadata.Title,
		metadata.FilePath,
		metadata.Status,
		metadata.IndexGeneration,
		metadata.CurrentVersion,
		metadata.ContentHash,
		metadata.UpdatedAt,
	))
	if err != nil {
		return Metadata{}, false, err
	}
	if err := insertVersion(context.Background(), tx, version); err != nil {
		return Metadata{}, false, err
	}
	if _, err := tx.Exec(context.Background(), `
INSERT INTO archive_edit_audit_logs (archive_id, actor_user_id, old_version, new_version, old_content_hash, new_content_hash, request_id, reason, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		audit.ArchiveID,
		audit.ActorUserID,
		audit.OldVersion,
		audit.NewVersion,
		audit.OldContentHash,
		audit.NewContentHash,
		audit.RequestID,
		audit.Reason,
		audit.CreatedAt,
	); err != nil {
		return Metadata{}, false, err
	}
	if err := insertIndexGeneration(context.Background(), tx, metadata.ArchiveID, metadata.IndexGeneration, audit.Reason); err != nil {
		return Metadata{}, false, err
	}
	if err := insertIdempotency(context.Background(), tx, requestID, "edit", metadata.ArchiveID); err != nil {
		return Metadata{}, false, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return Metadata{}, false, err
	}
	return saved, false, nil
}

func (r *PGRepository) SoftDelete(metadata Metadata, audit EditAuditLog, requestID string) (Metadata, bool, error) {
	if r == nil || r.pool == nil {
		return Metadata{}, false, errors.New("archive postgres repository is not configured")
	}
	if metadata.ArchiveID == "" || requestID == "" {
		return Metadata{}, false, errors.New("archive id and request id are required")
	}
	tx, err := r.pool.Begin(context.Background())
	if err != nil {
		return Metadata{}, false, err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck

	if existing, ok, err := getIdempotentArchive(context.Background(), tx, requestID); err != nil {
		return Metadata{}, false, err
	} else if ok {
		return existing, true, nil
	}

	saved, err := scanMetadata(tx.QueryRow(context.Background(), `
UPDATE archives
SET status = $2,
    updated_at = $3
WHERE archive_id = $1
RETURNING archive_id, user_id, org_id, project_id, title, file_path, status, index_generation, current_version, content_hash, created_at, updated_at`,
		metadata.ArchiveID,
		metadata.Status,
		metadata.UpdatedAt,
	))
	if err != nil {
		return Metadata{}, false, err
	}
	if _, err := tx.Exec(context.Background(), `
INSERT INTO archive_edit_audit_logs (archive_id, actor_user_id, old_version, new_version, old_content_hash, new_content_hash, request_id, reason, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		audit.ArchiveID,
		audit.ActorUserID,
		audit.OldVersion,
		audit.NewVersion,
		audit.OldContentHash,
		audit.NewContentHash,
		audit.RequestID,
		audit.Reason,
		audit.CreatedAt,
	); err != nil {
		return Metadata{}, false, err
	}
	if err := insertIdempotency(context.Background(), tx, requestID, "delete", metadata.ArchiveID); err != nil {
		return Metadata{}, false, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return Metadata{}, false, err
	}
	return saved, false, nil
}

func (r *PGRepository) MarkReindex(metadata Metadata, requestID string, reason string) (Metadata, bool, error) {
	if r == nil || r.pool == nil {
		return Metadata{}, false, errors.New("archive postgres repository is not configured")
	}
	if metadata.ArchiveID == "" || requestID == "" {
		return Metadata{}, false, errors.New("archive id and request id are required")
	}
	tx, err := r.pool.Begin(context.Background())
	if err != nil {
		return Metadata{}, false, err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck

	if existing, ok, err := getIdempotentArchive(context.Background(), tx, requestID); err != nil {
		return Metadata{}, false, err
	} else if ok {
		return existing, true, nil
	}

	saved, err := scanMetadata(tx.QueryRow(context.Background(), `
UPDATE archives
SET index_generation = $2,
    updated_at = $3
WHERE archive_id = $1
RETURNING archive_id, user_id, org_id, project_id, title, file_path, status, index_generation, current_version, content_hash, created_at, updated_at`,
		metadata.ArchiveID,
		metadata.IndexGeneration,
		metadata.UpdatedAt,
	))
	if err != nil {
		return Metadata{}, false, err
	}
	if err := insertIndexGeneration(context.Background(), tx, metadata.ArchiveID, metadata.IndexGeneration, reason); err != nil {
		return Metadata{}, false, err
	}
	if err := insertIdempotency(context.Background(), tx, requestID, "reindex", metadata.ArchiveID); err != nil {
		return Metadata{}, false, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return Metadata{}, false, err
	}
	return saved, false, nil
}

func (r *PGRepository) Versions(archiveID string) ([]Version, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("archive postgres repository is not configured")
	}
	rows, err := r.pool.Query(context.Background(), `
SELECT archive_id, version, file_path, content_hash, editor_user_id, edit_reason, created_at
FROM archive_versions
WHERE archive_id = $1
ORDER BY version ASC`, archiveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []Version{}
	for rows.Next() {
		var version Version
		if err := rows.Scan(&version.ArchiveID, &version.Version, &version.FilePath, &version.ContentHash, &version.EditorUserID, &version.Reason, &version.CreatedAt); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, nil
}

func selectArchiveMetadataSQL() string {
	return "SELECT archive_id, user_id, org_id, project_id, title, file_path, status, index_generation, current_version, content_hash, created_at, updated_at FROM archives"
}

type metadataScanner interface {
	Scan(dest ...any) error
}

func scanMetadata(row metadataScanner) (Metadata, error) {
	var metadata Metadata
	err := row.Scan(
		&metadata.ArchiveID,
		&metadata.UserID,
		&metadata.OrgID,
		&metadata.ProjectID,
		&metadata.Title,
		&metadata.FilePath,
		&metadata.Status,
		&metadata.IndexGeneration,
		&metadata.CurrentVersion,
		&metadata.ContentHash,
		&metadata.CreatedAt,
		&metadata.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Metadata{}, errors.New("archive not found")
	}
	return metadata, err
}

type archiveQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type archiveExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func getIdempotentArchive(ctx context.Context, tx archiveQuerier, requestID string) (Metadata, bool, error) {
	var archiveID string
	err := tx.QueryRow(ctx, `SELECT archive_id FROM archive_request_idempotency WHERE request_id = $1`, requestID).Scan(&archiveID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Metadata{}, false, nil
	}
	if err != nil {
		return Metadata{}, false, err
	}
	metadata, err := scanMetadata(tx.QueryRow(ctx, selectArchiveMetadataSQL()+" WHERE archive_id = $1", archiveID))
	return metadata, true, err
}

func insertVersion(ctx context.Context, tx archiveExecutor, version Version) error {
	_, err := tx.Exec(ctx, `
INSERT INTO archive_versions (archive_id, version, file_path, content_hash, editor_user_id, edit_reason, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		version.ArchiveID,
		version.Version,
		version.FilePath,
		version.ContentHash,
		version.EditorUserID,
		version.Reason,
		version.CreatedAt,
	)
	return err
}

func insertIndexGeneration(ctx context.Context, tx archiveExecutor, archiveID string, generation int, reason string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO archive_index_generations (archive_id, index_generation, status, reason)
VALUES ($1,$2,'pending',$3)
ON CONFLICT DO NOTHING`, archiveID, generation, reason)
	return err
}

func insertIdempotency(ctx context.Context, tx archiveExecutor, requestID, operation, archiveID string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO archive_request_idempotency (request_id, operation, archive_id)
VALUES ($1,$2,$3)`, requestID, operation, archiveID)
	return err
}
