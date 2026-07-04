package qdrant

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type StatusClient interface {
	Health(ctx context.Context) error
	CollectionInfo(ctx context.Context, collection string) (CollectionInfo, error)
}

type StatusStore interface {
	IndexStats(ctx context.Context, collection string) (IndexStats, error)
	ArchiveIndexStats(ctx context.Context, collection, archiveID string, indexGeneration int) (ArchiveIndexStats, error)
}

type IndexStats struct {
	PointsByStatus          map[string]int64 `json:"points_by_status"`
	ArchivePointsByStatus   map[string]int64 `json:"archive_points_by_status"`
	HotMemoryPointsByStatus map[string]int64 `json:"hot_memory_points_by_status"`
	JobsByStatus            map[string]int64 `json:"jobs_by_status"`
	LatestPointAt           *time.Time       `json:"latest_point_at,omitempty"`
}

type ArchiveIndexStats struct {
	ArchiveID       string                  `json:"archive_id"`
	IndexGeneration int                     `json:"index_generation"`
	JobsByStatus    map[string]int64        `json:"jobs_by_status"`
	ChunksByStatus  map[string]int64        `json:"chunks_by_status"`
	PointsByStatus  map[string]int64        `json:"points_by_status"`
	IndexJobs       []ArchiveIndexJobStatus `json:"index_jobs"`
	ArchiveChunks   []ArchiveChunkStatus    `json:"archive_chunks"`
	LatestPointAt   *time.Time              `json:"latest_point_at,omitempty"`
}

type ArchiveIndexJobStatus struct {
	IdempotencyKey string     `json:"idempotency_key"`
	Status         string     `json:"status"`
	ErrorMessage   string     `json:"error_message"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	LockedBy       string     `json:"locked_by,omitempty"`
	LockedUntil    *time.Time `json:"locked_until,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type ArchiveChunkStatus struct {
	ChunkID            string     `json:"chunk_id"`
	ChunkIndex         int        `json:"chunk_index"`
	VectorStatus       string     `json:"vector_status"`
	ContentHash        string     `json:"content_hash"`
	HeadingPath        []string   `json:"heading_path"`
	SourceEventIDs     []string   `json:"source_event_ids"`
	QdrantPointID      string     `json:"qdrant_point_id,omitempty"`
	QdrantVectorStatus string     `json:"qdrant_vector_status,omitempty"`
	QdrantUpdatedAt    *time.Time `json:"qdrant_updated_at,omitempty"`
}

type StatusSnapshot struct {
	Collection                   CollectionInfo   `json:"collection"`
	PointsByStatus               map[string]int64 `json:"points_by_status"`
	ArchivePointsByStatus        map[string]int64 `json:"archive_points_by_status"`
	HotMemoryPointsByStatus      map[string]int64 `json:"hot_memory_points_by_status"`
	JobsByStatus                 map[string]int64 `json:"jobs_by_status"`
	QueryTimeFilterEnforced      bool             `json:"query_time_filter_enforced"`
	RequiredPayloadFields        []string         `json:"required_payload_fields"`
	MissingRequiredPayloadFields []string         `json:"missing_required_payload_fields"`
	LatestPointAt                *time.Time       `json:"latest_point_at,omitempty"`
}

var requiredPayloadFields = []string{"doc_type", "user_id", "org_id", "project_id", "visibility", "permission_labels", "index_generation"}

type StatusOptions struct {
	Client         StatusClient
	Store          StatusStore
	CollectionName string
}

type StatusService struct {
	client         StatusClient
	store          StatusStore
	collectionName string
}

func NewStatusService(options StatusOptions) StatusService {
	if options.CollectionName == "" {
		options.CollectionName = DefaultCollectionName
	}
	return StatusService{client: options.Client, store: options.Store, collectionName: options.CollectionName}
}

func (s StatusService) Configured() bool {
	return s.client != nil
}

func (s StatusService) Snapshot(ctx context.Context) (StatusSnapshot, error) {
	if !s.Configured() {
		return StatusSnapshot{}, errors.New("qdrant status service is not configured")
	}
	if err := s.client.Health(ctx); err != nil {
		return StatusSnapshot{}, err
	}
	info, err := s.client.CollectionInfo(ctx, s.collectionName)
	if err != nil {
		return StatusSnapshot{}, err
	}
	stats := IndexStats{PointsByStatus: map[string]int64{}, ArchivePointsByStatus: map[string]int64{}, HotMemoryPointsByStatus: map[string]int64{}, JobsByStatus: map[string]int64{}}
	if s.store != nil {
		stats, err = s.store.IndexStats(ctx, s.collectionName)
		if err != nil {
			return StatusSnapshot{}, err
		}
	}
	if stats.PointsByStatus == nil {
		stats.PointsByStatus = map[string]int64{}
	}
	if stats.ArchivePointsByStatus == nil {
		stats.ArchivePointsByStatus = map[string]int64{}
	}
	if stats.HotMemoryPointsByStatus == nil {
		stats.HotMemoryPointsByStatus = map[string]int64{}
	}
	if stats.JobsByStatus == nil {
		stats.JobsByStatus = map[string]int64{}
	}
	missingRequiredFields := missingRequiredPayloadFields(info.PayloadSchema)
	return StatusSnapshot{
		Collection:                   info,
		PointsByStatus:               stats.PointsByStatus,
		ArchivePointsByStatus:        stats.ArchivePointsByStatus,
		HotMemoryPointsByStatus:      stats.HotMemoryPointsByStatus,
		JobsByStatus:                 stats.JobsByStatus,
		QueryTimeFilterEnforced:      len(missingRequiredFields) == 0,
		RequiredPayloadFields:        append([]string(nil), requiredPayloadFields...),
		MissingRequiredPayloadFields: missingRequiredFields,
		LatestPointAt:                stats.LatestPointAt,
	}, nil
}

func (s StatusService) ArchiveIndexStats(ctx context.Context, archiveID string, indexGeneration int) (ArchiveIndexStats, error) {
	if s.store == nil {
		return ArchiveIndexStats{}, errors.New("qdrant status postgres store is not configured")
	}
	stats, err := s.store.ArchiveIndexStats(ctx, s.collectionName, archiveID, indexGeneration)
	if err != nil {
		return ArchiveIndexStats{}, err
	}
	if stats.JobsByStatus == nil {
		stats.JobsByStatus = map[string]int64{}
	}
	if stats.ChunksByStatus == nil {
		stats.ChunksByStatus = map[string]int64{}
	}
	if stats.PointsByStatus == nil {
		stats.PointsByStatus = map[string]int64{}
	}
	if stats.IndexJobs == nil {
		stats.IndexJobs = []ArchiveIndexJobStatus{}
	}
	if stats.ArchiveChunks == nil {
		stats.ArchiveChunks = []ArchiveChunkStatus{}
	}
	return stats, nil
}

type PGStatusStore struct {
	pool *pgxpool.Pool
}

func NewPGStatusStore(pool *pgxpool.Pool) *PGStatusStore {
	return &PGStatusStore{pool: pool}
}

func (s *PGStatusStore) IndexStats(ctx context.Context, collection string) (IndexStats, error) {
	if s == nil || s.pool == nil {
		return IndexStats{}, errors.New("qdrant status postgres store is not configured")
	}
	stats := IndexStats{
		PointsByStatus:          map[string]int64{},
		ArchivePointsByStatus:   map[string]int64{},
		HotMemoryPointsByStatus: map[string]int64{},
		JobsByStatus:            map[string]int64{},
	}
	rows, err := s.pool.Query(ctx, `
SELECT vector_status, count(*), max(updated_at)
FROM qdrant_points
WHERE collection_name = $1
GROUP BY vector_status`, collection)
	if err != nil {
		return IndexStats{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		var latest *time.Time
		if err := rows.Scan(&status, &count, &latest); err != nil {
			return IndexStats{}, err
		}
		stats.ArchivePointsByStatus[status] = count
		addPointStatus(stats.PointsByStatus, status, count)
		updateLatestPoint(&stats.LatestPointAt, latest)
	}
	if err := rows.Err(); err != nil {
		return IndexStats{}, err
	}
	hotRows, err := s.pool.Query(ctx, `
SELECT vector_status, count(*), max(updated_at)
FROM hot_memory_qdrant_points
WHERE collection_name = $1
GROUP BY vector_status`, collection)
	if err != nil {
		return IndexStats{}, err
	}
	defer hotRows.Close()
	for hotRows.Next() {
		var status string
		var count int64
		var latest *time.Time
		if err := hotRows.Scan(&status, &count, &latest); err != nil {
			return IndexStats{}, err
		}
		stats.HotMemoryPointsByStatus[status] = count
		addPointStatus(stats.PointsByStatus, status, count)
		updateLatestPoint(&stats.LatestPointAt, latest)
	}
	if err := hotRows.Err(); err != nil {
		return IndexStats{}, err
	}
	jobRows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM archive_index_jobs GROUP BY status`)
	if err != nil {
		return IndexStats{}, err
	}
	defer jobRows.Close()
	for jobRows.Next() {
		var status string
		var count int64
		if err := jobRows.Scan(&status, &count); err != nil {
			return IndexStats{}, err
		}
		stats.JobsByStatus[status] = count
	}
	return stats, jobRows.Err()
}

func addPointStatus(target map[string]int64, status string, count int64) {
	target[status] += count
}

func updateLatestPoint(current **time.Time, candidate *time.Time) {
	if candidate != nil && (*current == nil || candidate.After(**current)) {
		*current = candidate
	}
}

func missingRequiredPayloadFields(schema map[string]bool) []string {
	missing := make([]string, 0, len(requiredPayloadFields))
	for _, field := range requiredPayloadFields {
		if !schema[field] {
			missing = append(missing, field)
		}
	}
	sort.Strings(missing)
	return missing
}

func (s *PGStatusStore) ArchiveIndexStats(ctx context.Context, collection, archiveID string, indexGeneration int) (ArchiveIndexStats, error) {
	if s == nil || s.pool == nil {
		return ArchiveIndexStats{}, errors.New("qdrant status postgres store is not configured")
	}
	stats := ArchiveIndexStats{
		ArchiveID:       archiveID,
		IndexGeneration: indexGeneration,
		JobsByStatus:    map[string]int64{},
		ChunksByStatus:  map[string]int64{},
		PointsByStatus:  map[string]int64{},
	}
	jobRows, err := s.pool.Query(ctx, `
SELECT status, count(*)
FROM archive_index_jobs
WHERE archive_id = $1 AND index_generation = $2
GROUP BY status`, archiveID, indexGeneration)
	if err != nil {
		return ArchiveIndexStats{}, err
	}
	defer jobRows.Close()
	for jobRows.Next() {
		var status string
		var count int64
		if err := jobRows.Scan(&status, &count); err != nil {
			return ArchiveIndexStats{}, err
		}
		stats.JobsByStatus[status] = count
	}
	if err := jobRows.Err(); err != nil {
		return ArchiveIndexStats{}, err
	}
	jobDetailRows, err := s.pool.Query(ctx, `
SELECT idempotency_key, status, error_message, attempts, max_attempts, coalesce(locked_by, ''), locked_until, completed_at, created_at, updated_at
FROM archive_index_jobs
WHERE archive_id = $1 AND index_generation = $2
ORDER BY updated_at DESC, created_at DESC
LIMIT 20`, archiveID, indexGeneration)
	if err != nil {
		return ArchiveIndexStats{}, err
	}
	defer jobDetailRows.Close()
	for jobDetailRows.Next() {
		var job ArchiveIndexJobStatus
		if err := jobDetailRows.Scan(&job.IdempotencyKey, &job.Status, &job.ErrorMessage, &job.Attempts, &job.MaxAttempts, &job.LockedBy, &job.LockedUntil, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return ArchiveIndexStats{}, err
		}
		stats.IndexJobs = append(stats.IndexJobs, job)
	}
	if err := jobDetailRows.Err(); err != nil {
		return ArchiveIndexStats{}, err
	}

	chunkRows, err := s.pool.Query(ctx, `
SELECT vector_status, count(*)
FROM archive_chunks
WHERE archive_id = $1 AND index_generation = $2 AND stale = false
GROUP BY vector_status`, archiveID, indexGeneration)
	if err != nil {
		return ArchiveIndexStats{}, err
	}
	defer chunkRows.Close()
	for chunkRows.Next() {
		var status string
		var count int64
		if err := chunkRows.Scan(&status, &count); err != nil {
			return ArchiveIndexStats{}, err
		}
		stats.ChunksByStatus[status] = count
	}
	if err := chunkRows.Err(); err != nil {
		return ArchiveIndexStats{}, err
	}
	chunkDetailRows, err := s.pool.Query(ctx, `
SELECT ac.chunk_id, ac.chunk_index, ac.vector_status, ac.content_hash, ac.heading_path, ac.source_event_ids,
       coalesce(qp.point_id, ''), coalesce(qp.vector_status, ''), qp.updated_at
FROM archive_chunks ac
LEFT JOIN qdrant_points qp ON qp.chunk_id = ac.chunk_id AND qp.collection_name = $1
WHERE ac.archive_id = $2 AND ac.index_generation = $3 AND ac.stale = false
ORDER BY ac.chunk_index ASC
LIMIT 100`, collection, archiveID, indexGeneration)
	if err != nil {
		return ArchiveIndexStats{}, err
	}
	defer chunkDetailRows.Close()
	for chunkDetailRows.Next() {
		var chunk ArchiveChunkStatus
		if err := chunkDetailRows.Scan(&chunk.ChunkID, &chunk.ChunkIndex, &chunk.VectorStatus, &chunk.ContentHash, &chunk.HeadingPath, &chunk.SourceEventIDs, &chunk.QdrantPointID, &chunk.QdrantVectorStatus, &chunk.QdrantUpdatedAt); err != nil {
			return ArchiveIndexStats{}, err
		}
		stats.ArchiveChunks = append(stats.ArchiveChunks, chunk)
	}
	if err := chunkDetailRows.Err(); err != nil {
		return ArchiveIndexStats{}, err
	}

	pointRows, err := s.pool.Query(ctx, `
SELECT qp.vector_status, count(*), max(qp.updated_at)
FROM qdrant_points qp
JOIN archive_chunks ac ON ac.chunk_id = qp.chunk_id
WHERE qp.collection_name = $1
  AND ac.archive_id = $2
  AND ac.index_generation = $3
  AND ac.stale = false
GROUP BY qp.vector_status`, collection, archiveID, indexGeneration)
	if err != nil {
		return ArchiveIndexStats{}, err
	}
	defer pointRows.Close()
	for pointRows.Next() {
		var status string
		var count int64
		var latest *time.Time
		if err := pointRows.Scan(&status, &count, &latest); err != nil {
			return ArchiveIndexStats{}, err
		}
		stats.PointsByStatus[status] = count
		if latest != nil && (stats.LatestPointAt == nil || latest.After(*stats.LatestPointAt)) {
			stats.LatestPointAt = latest
		}
	}
	return stats, pointRows.Err()
}
