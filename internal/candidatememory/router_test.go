package candidatememory

import (
	"testing"

	"memory-os/internal/hotmemory"
)

type fakeHotMemory struct {
	upserts []hotmemory.UpsertRequest
	err     error
}

func (f *fakeHotMemory) Upsert(req hotmemory.UpsertRequest) (hotmemory.Memory, error) {
	if f.err != nil {
		return hotmemory.Memory{}, f.err
	}
	f.upserts = append(f.upserts, req)
	return hotmemory.Memory{MemoryID: "hm-" + req.Fact}, nil
}

// 高风险 → pending_review,不自动进热记忆或 Archive。
func TestRouterHighRiskGoesPendingReview(t *testing.T) {
	fake := &fakeHotMemory{}
	r := NewRouter(fake)
	c := Candidate{MemoryType: MemoryTypeRisk, RiskLevel: RiskHigh, Content: "生产 schema 迁移", Confidence: 0.9}

	got, decision, err := r.ApplyRouting(c)
	if err != nil {
		t.Fatalf("apply routing: %v", err)
	}
	if got.Status != StatusPending {
		t.Fatalf("高风险应进入 pending_review(status=pending): %s", got.Status)
	}
	if decision.Target != RoutingTargetPendingReview {
		t.Fatalf("decision.Target 应为 pending_review: %s", decision.Target)
	}
	if len(fake.upserts) != 0 {
		t.Fatalf("高风险不应进入热记忆: %d", len(fake.upserts))
	}
}

// 低风险短事实 + confidence>=0.7 → 自动 hotmemory.Upsert + StatusPromotedToHot。
func TestRouterLowRiskShortFactPromotesToHotMemory(t *testing.T) {
	fake := &fakeHotMemory{}
	r := NewRouter(fake)
	c := Candidate{
		MemoryType: MemoryTypeFact, RiskLevel: RiskLow,
		Content:    "项目使用 PostgreSQL 作为权威元数据源",
		Confidence: 0.85,
		OrgID:      "o", ProjectID: "p", UserID: "u", AgentID: "a", SourceKey: "github.com/acme/web",
	}

	got, decision, err := r.ApplyRouting(c)
	if err != nil {
		t.Fatalf("apply routing: %v", err)
	}
	if got.Status != StatusPromotedToHot {
		t.Fatalf("低风险短事实应提升为热记忆: %s", got.Status)
	}
	if !decision.Promoted || len(fake.upserts) != 1 {
		t.Fatalf("应触发一次 hotmemory.Upsert: promoted=%v upserts=%d", decision.Promoted, len(fake.upserts))
	}
	if fake.upserts[0].Fact != c.Content || fake.upserts[0].SourceRef != c.SourceKey {
		t.Fatalf("upsert 字段不正确: %+v", fake.upserts[0])
	}
}

// 低 confidence → 不自动提升,保持 pending(待用户确认/更多证据)。
func TestRouterLowConfidenceStaysPending(t *testing.T) {
	fake := &fakeHotMemory{}
	r := NewRouter(fake)
	c := Candidate{MemoryType: MemoryTypeFact, RiskLevel: RiskLow, Content: "短事实但不确定", Confidence: 0.5}

	got, _, err := r.ApplyRouting(c)
	if err != nil {
		t.Fatalf("apply routing: %v", err)
	}
	if got.Status != StatusPending {
		t.Fatalf("低 confidence 应保持 pending: %s", got.Status)
	}
	if len(fake.upserts) != 0 {
		t.Fatalf("低 confidence 不应进热记忆: %d", len(fake.upserts))
	}
}

// bugfix/decision/risk/follow_up → compose pool,不进热记忆。
func TestRouterComposeTypesGoToComposePool(t *testing.T) {
	fake := &fakeHotMemory{}
	r := NewRouter(fake)
	for _, mt := range []MemoryType{MemoryTypeBugfix, MemoryTypeDecision, MemoryTypeFollowUp} {
		c := Candidate{MemoryType: mt, RiskLevel: RiskLow, Content: "compose 候选", Confidence: 0.9}
		got, decision, err := r.ApplyRouting(c)
		if err != nil {
			t.Fatalf("apply routing %s: %v", mt, err)
		}
		if got.Status != StatusInComposePool {
			t.Fatalf("%s 应进入 compose pool: %s", mt, got.Status)
		}
		if decision.Target != RoutingTargetComposePool {
			t.Fatalf("%s decision.Target 应为 compose_pool: %s", mt, decision.Target)
		}
	}
	if len(fake.upserts) != 0 {
		t.Fatalf("compose 类型不应进热记忆: %d", len(fake.upserts))
	}
}

// 无热记忆依赖时,低风险短事实也不应 panic,降级为 pending。
func TestRouterWithoutHotMemoryDegradesToPending(t *testing.T) {
	r := NewRouter(nil)
	c := Candidate{MemoryType: MemoryTypeFact, RiskLevel: RiskLow, Content: "短事实", Confidence: 0.9}
	got, _, err := r.ApplyRouting(c)
	if err != nil {
		t.Fatalf("apply routing: %v", err)
	}
	if got.Status != StatusPending {
		t.Fatalf("无 hotMemory 依赖应降级 pending: %s", got.Status)
	}
}
