package retrieval

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PGArchiveGenerationResolver struct {
	pool *pgxpool.Pool
}

func NewPGArchiveGenerationResolver(pool *pgxpool.Pool) *PGArchiveGenerationResolver {
	return &PGArchiveGenerationResolver{pool: pool}
}

func (r *PGArchiveGenerationResolver) CurrentGeneration(scope ArchiveGenerationContext) (int, error) {
	if r == nil || r.pool == nil {
		return 0, errors.New("archive generation resolver is not configured")
	}
	if scope.UserID == "" || scope.OrgID == "" || scope.ProjectID == "" {
		return 0, errors.New("archive generation scope is required")
	}
	var generation int
	err := r.pool.QueryRow(context.Background(), `
SELECT COALESCE(MAX(index_generation), 0)
FROM archives
WHERE user_id = $1
  AND org_id = $2
  AND project_id = $3
  AND status = 'active'`,
		scope.UserID, scope.OrgID, scope.ProjectID).Scan(&generation)
	return generation, err
}
