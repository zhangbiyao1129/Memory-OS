package memorykernel

import (
	"context"
	"strings"
	"testing"
)

func TestContextPackBuildsDeploymentPackFromProceduresRisksAndEvidence(t *testing.T) {
	repo := NewInMemoryRepository()
	_, _ = repo.UpsertUnit(context.Background(), MemoryUnit{UnitID: "unit_deploy_proc", OrgID: "org_1", ProjectID: "project_1", Type: UnitProcedure, Content: "部署前阅读 DEPLOYMENT.md", AppliesWhen: "部署 重启 上线 排障", AgentShould: "先读部署手册", Status: UnitCurrent, TrustScore: 0.95})
	_, _ = repo.UpsertUnit(context.Background(), MemoryUnit{UnitID: "unit_secret_risk", OrgID: "org_1", ProjectID: "project_1", Type: UnitRisk, Content: "Secret 明文不得入库", AppliesWhen: "任何工具和日志", AgentShould: "使用 secret_ref", Status: UnitCurrent, TrustScore: 0.9})
	builder := NewContextPackBuilder(repo)
	pack, err := builder.Build(context.Background(), ContextPackRequest{OrgID: "org_1", ProjectID: "project_1", Query: "部署 Memory OS"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !strings.Contains(pack.Context, "部署前阅读 DEPLOYMENT.md") || !strings.Contains(pack.Context, "Secret 明文不得入库") {
		t.Fatalf("pack context = %s", pack.Context)
	}
}

func TestContextPackReturnsEmptyForNoCurrentUnits(t *testing.T) {
	repo := NewInMemoryRepository()
	builder := NewContextPackBuilder(repo)
	pack, err := builder.Build(context.Background(), ContextPackRequest{OrgID: "org_1", ProjectID: "project_1", Query: "test"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if pack.Context != "" {
		t.Fatalf("expected empty context, got = %q", pack.Context)
	}
	if len(pack.Units) != 0 {
		t.Fatalf("expected 0 units, got %d", len(pack.Units))
	}
}

func TestContextPackRespectsMaxContextBytes(t *testing.T) {
	repo := NewInMemoryRepository()
	for i := 0; i < 10; i++ {
		_, _ = repo.UpsertUnit(context.Background(), MemoryUnit{
			UnitID:    "unit_" + string(rune('a'+i)),
			OrgID:     "org_1",
			ProjectID: "project_1",
			Type:      UnitFact,
			Content:   "这是一条较长的记忆内容，用于测试上下文截断功能 " + string(rune('a'+i)),
			Status:    UnitCurrent,
			TrustScore: 0.8,
		})
	}
	builder := NewContextPackBuilder(repo)
	pack, err := builder.Build(context.Background(), ContextPackRequest{OrgID: "org_1", ProjectID: "project_1", Query: "test", MaxContextBytes: 200})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(pack.Warnings) == 0 {
		t.Fatal("expected truncation warning")
	}
}

func TestContextPackPrioritizesProceduresOverFacts(t *testing.T) {
	repo := NewInMemoryRepository()
	_, _ = repo.UpsertUnit(context.Background(), MemoryUnit{UnitID: "unit_fact", OrgID: "org_1", ProjectID: "project_1", Type: UnitFact, Content: "普通事实", Status: UnitCurrent, TrustScore: 0.8})
	_, _ = repo.UpsertUnit(context.Background(), MemoryUnit{UnitID: "unit_proc", OrgID: "org_1", ProjectID: "project_1", Type: UnitProcedure, Content: "操作步骤", Status: UnitCurrent, TrustScore: 0.8})
	builder := NewContextPackBuilder(repo)
	pack, err := builder.Build(context.Background(), ContextPackRequest{OrgID: "org_1", ProjectID: "project_1", Query: "test"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(pack.Units) < 2 {
		t.Fatalf("expected at least 2 units, got %d", len(pack.Units))
	}
	// procedure 应排在 fact 前面
	if pack.Units[0].UnitID != "unit_proc" {
		t.Fatalf("expected procedure first, got %s", pack.Units[0].UnitID)
	}
}
