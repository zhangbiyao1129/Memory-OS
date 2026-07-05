package candidatememory

import "memory-os/internal/hotmemory"

// 分流目标常量。
const (
	RoutingTargetPendingReview = "pending_review"
	RoutingTargetHotMemory     = "hot_memory"
	RoutingTargetComposePool   = "compose_pool"
	RoutingTargetPending       = "pending"
)

// hotMemoryPromoteConfidence 自动提升热记忆的最低 confidence。
const hotMemoryPromoteConfidence = 0.7

// shortFactMaxRunes 短事实的最大字符数(一句短事实)。
const shortFactMaxRunes = 120

// HotMemoryUpsertter 热记忆 upsert 能力,由 hotmemory.Service 实现。
type HotMemoryUpsertter interface {
	Upsert(request hotmemory.UpsertRequest) (hotmemory.Memory, error)
}

// RoutingDecision 分流决策结果。
type RoutingDecision struct {
	Target   string
	Promoted bool // 是否已提升为热记忆
}

// Router 候选分流器:决定候选进入 pending_review / hot_memory / compose_pool。
type Router struct {
	hotMemory HotMemoryUpsertter
}

func NewRouter(hotMemory HotMemoryUpsertter) Router {
	return Router{hotMemory: hotMemory}
}

// ApplyRouting 根据候选属性决定目标状态,可能触发 hotmemory.Upsert。
// 分流规则(Phase 3):
//   - 高风险 → pending_review(强制人工确认,不自动入 hot/archive)
//   - 低风险短事实/偏好 + confidence>=0.7 → 自动 hotmemory.Upsert
//   - bugfix/decision/risk/follow_up → compose pool(等主题沉淀)
//   - 其余 → pending
func (r Router) ApplyRouting(c Candidate) (Candidate, RoutingDecision, error) {
	if c.RiskLevel == RiskHigh {
		c.Status = StatusPending
		return c, RoutingDecision{Target: RoutingTargetPendingReview}, nil
	}

	if c.RiskLevel == RiskLow && isShortFact(c) && c.Confidence >= hotMemoryPromoteConfidence {
		if r.hotMemory == nil {
			// 无热记忆依赖时降级为 pending,不阻塞提炼链路
			c.Status = StatusPending
			return c, RoutingDecision{Target: RoutingTargetPending}, nil
		}
		if _, err := r.upsertHotMemory(c); err != nil {
			return c, RoutingDecision{}, err
		}
		c.Status = StatusPromotedToHot
		return c, RoutingDecision{Target: RoutingTargetHotMemory, Promoted: true}, nil
	}

	if isComposeType(c.MemoryType) {
		c.Status = StatusInComposePool
		return c, RoutingDecision{Target: RoutingTargetComposePool}, nil
	}

	c.Status = StatusPending
	return c, RoutingDecision{Target: RoutingTargetPending}, nil
}

func (r Router) upsertHotMemory(c Candidate) (hotmemory.Memory, error) {
	return r.hotMemory.Upsert(hotmemory.UpsertRequest{
		OrgID:            c.OrgID,
		ProjectID:        c.ProjectID,
		UserID:           c.UserID,
		AgentID:          c.AgentID,
		Scope:            hotmemory.ScopeProject,
		Visibility:       "project",
		PermissionLabels: []string{"project:" + c.ProjectID + ":read", "project:" + c.ProjectID + ":write"},
		Fact:             c.Content,
		SourceType:       hotmemory.SourceTurnEvent,
		SourceRef:        c.SourceKey,
		Confidence:       c.Confidence,
	})
}

// isShortFact 低风险短事实/偏好:类型为 fact/preference 且内容较短。
func isShortFact(c Candidate) bool {
	if c.MemoryType != MemoryTypeFact && c.MemoryType != MemoryTypePreference {
		return false
	}
	return len([]rune(c.Content)) <= shortFactMaxRunes
}

// isComposeType 进入主题沉淀池的类型。
func isComposeType(mt MemoryType) bool {
	switch mt {
	case MemoryTypeBugfix, MemoryTypeDecision, MemoryTypeRisk, MemoryTypeFollowUp:
		return true
	}
	return false
}
