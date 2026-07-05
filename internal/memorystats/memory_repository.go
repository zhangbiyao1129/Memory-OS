package memorystats

import "context"

type MemoryRepository struct {
	SnapshotValue Snapshot
	Err           error
	LastFilter    Filter
}

func NewMemoryRepository(snapshot Snapshot) *MemoryRepository {
	return &MemoryRepository{SnapshotValue: snapshot}
}

func (r *MemoryRepository) Snapshot(ctx context.Context, filter Filter) (Snapshot, error) {
	r.LastFilter = filter
	if r.Err != nil {
		return Snapshot{}, r.Err
	}
	return r.SnapshotValue, nil
}
