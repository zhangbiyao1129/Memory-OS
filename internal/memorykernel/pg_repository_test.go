package memorykernel

import (
	"context"
	"os"
	"testing"

	"memory-os/internal/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

func memoryKernelPGTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.RunEmbeddedMigrations(context.Background(), pool); err != nil {
		t.Fatalf("RunEmbeddedMigrations() error = %v", err)
	}
	return pool
}

func TestPGRepositoryUpsertsUnitClaimAndGovernanceAction(t *testing.T) {
	ctx := context.Background()
	pool := memoryKernelPGTestPool(t)
	repo := NewPGRepository(pool)

	run, err := repo.CreateRun(ctx, GovernanceRun{
		RunID: "gov_run_repo_1", OrgID: "org_1", ProjectID: "project_1", TriggerType: "manual",
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if run.RunID != "gov_run_repo_1" || run.Status != RunRunning {
		t.Fatalf("run = %#v", run)
	}

	unit, err := repo.UpsertUnit(ctx, MemoryUnit{
		UnitID: "unit_repo_1", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1",
		Type: UnitFact, Content: "memory_archive 已实现并部署", Status: UnitCurrent,
		Confidence: 0.95, TrustScore: 0.9,
		SourceRefs: []SourceRef{{Kind: "candidate", ID: "cand_new"}},
	})
	if err != nil {
		t.Fatalf("UpsertUnit() error = %v", err)
	}
	if unit.Status != UnitCurrent || unit.Content == "" {
		t.Fatalf("unit = %#v", unit)
	}

	claim, err := repo.UpsertClaim(ctx, MemoryClaim{
		ClaimID: "claim_repo_1", UnitID: "unit_repo_1", OrgID: "org_1", ProjectID: "project_1",
		Subject: "memory_archive", Predicate: "implementation_status", Value: "implemented_and_deployed",
		Polarity: "positive", Confidence: 0.95,
	})
	if err != nil {
		t.Fatalf("UpsertClaim() error = %v", err)
	}
	if claim.Value != "implemented_and_deployed" {
		t.Fatalf("claim = %#v", claim)
	}

	action, err := repo.RecordAction(ctx, GovernanceAction{
		ActionID: "gov_action_repo_1", RunID: "gov_run_repo_1", OrgID: "org_1", ProjectID: "project_1",
		TargetType: "candidate", TargetID: "cand_old", Action: ActionDiscardStale,
		Reason: "旧事实已被新事实覆盖", Applied: true,
	})
	if err != nil {
		t.Fatalf("RecordAction() error = %v", err)
	}
	if !action.Applied {
		t.Fatalf("action = %#v", action)
	}
}
