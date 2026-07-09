package memorykernel

import (
	"context"
	"testing"
)

func TestServiceRunAutoGovernanceRunsEachActiveScopeOnce(t *testing.T) {
	repo := NewInMemoryRepository()
	if _, err := repo.UpsertUnit(context.Background(), MemoryUnit{
		UnitID:    "unit_a",
		OrgID:     "org_1",
		ProjectID: "project_1",
		SourceKey: "source_a",
		UserID:    "user_1",
		Type:      UnitFact,
		Content:   "memory_archive 已实现",
		Status:    UnitCurrent,
	}); err != nil {
		t.Fatalf("UpsertUnit(unit_a) error = %v", err)
	}
	if _, err := repo.UpsertUnit(context.Background(), MemoryUnit{
		UnitID:    "unit_b",
		OrgID:     "org_1",
		ProjectID: "project_1",
		SourceKey: "source_b",
		UserID:    "user_1",
		Type:      UnitFact,
		Content:   "memory_kernel 已接入",
		Status:    UnitCurrent,
	}); err != nil {
		t.Fatalf("UpsertUnit(unit_b) error = %v", err)
	}

	service := NewService(ServiceOptions{
		Repository: repo,
		Collector:  fakeCollector{},
		Classifier: fakeClassifier{result: ClassifyResult{Summary: "auto"}},
	})

	count, err := service.RunAutoGovernance(context.Background())
	if err != nil {
		t.Fatalf("RunAutoGovernance() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("RunAutoGovernance() = %d, want 2", count)
	}

	runs, err := repo.ListRuns(context.Background(), RunFilter{OrgID: "org_1"})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("ListRuns() = %d runs, want 2", len(runs))
	}
}
