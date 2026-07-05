package memorystats

import "context"

type Repository interface {
	Snapshot(ctx context.Context, filter Filter) (Snapshot, error)
}
