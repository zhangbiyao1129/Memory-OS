package memorystats

import (
	"context"
	"errors"
	"testing"
)

type fakeRepository struct {
	filter Filter
	err    error
}

func (r *fakeRepository) Snapshot(ctx context.Context, filter Filter) (Snapshot, error) {
	r.filter = filter
	if r.err != nil {
		return Snapshot{}, r.err
	}
	return Snapshot{Archives: AssetStats{Total: 1, ByStatus: map[string]int64{"active": 1}}}, nil
}

func TestServiceRequiresConfiguredRepository(t *testing.T) {
	_, err := NewService(nil).Snapshot(context.Background(), Filter{UserID: "u", OrgID: "o", ProjectID: "p", PermissionLabels: []string{"project:p:read"}})
	if err == nil {
		t.Fatal("Snapshot() error = nil, want missing repository error")
	}
}

func TestServiceValidatesPermissionContext(t *testing.T) {
	svc := NewService(&fakeRepository{})
	cases := []Filter{
		{OrgID: "o", ProjectID: "p", PermissionLabels: []string{"project:p:read"}},
		{UserID: "u", ProjectID: "p", PermissionLabels: []string{"project:p:read"}},
		{UserID: "u", OrgID: "o", PermissionLabels: []string{"project:p:read"}},
		{UserID: "u", OrgID: "o", ProjectID: "p"},
	}
	for _, filter := range cases {
		if _, err := svc.Snapshot(context.Background(), filter); err == nil {
			t.Fatalf("Snapshot(%#v) error = nil, want validation error", filter)
		}
	}
}

func TestServicePassesFilterToRepository(t *testing.T) {
	repo := &fakeRepository{}
	filter := Filter{UserID: "u", OrgID: "o", ProjectID: "p", PermissionLabels: []string{"project:p:read"}}
	got, err := NewService(repo).Snapshot(context.Background(), filter)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got.Archives.Total != 1 {
		t.Fatalf("Snapshot() archives total = %d, want 1", got.Archives.Total)
	}
	if repo.filter.UserID != filter.UserID || repo.filter.ProjectID != filter.ProjectID {
		t.Fatalf("repository filter = %#v, want %#v", repo.filter, filter)
	}
}

func TestServiceReturnsRepositoryError(t *testing.T) {
	want := errors.New("boom")
	_, err := NewService(&fakeRepository{err: want}).Snapshot(context.Background(), Filter{UserID: "u", OrgID: "o", ProjectID: "p", PermissionLabels: []string{"project:p:read"}})
	if !errors.Is(err, want) {
		t.Fatalf("Snapshot() error = %v, want %v", err, want)
	}
}
