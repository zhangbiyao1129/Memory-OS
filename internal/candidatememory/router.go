package candidatememory

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

// RoutingDecision 分流决策结果。
type RoutingDecision struct {
	Target   string
	Promoted bool // 是否已提升为热记忆
}

// Router 候选分流器:决定候选进入 pending_review / hot_memory / compose_pool。
type Router struct{}

func NewRouter(_ any) Router {
	return Router{}
}

// ApplyRouting 根据候选属性决定目标状态。
// 分流规则(Phase A):
//   - 高风险 → pending_review(强制人工确认,不自动入热记忆)
//   - 低风险短事实/偏好 → pending(阶段 A 禁止自动提升)
//   - bugfix/decision/risk/follow_up → compose pool(作为归档素材)
//   - 其余 → pending
func (r Router) ApplyRouting(c Candidate) (Candidate, RoutingDecision, error) {
	if c.RiskLevel == RiskHigh {
		c.Status = StatusPending
		return c, RoutingDecision{Target: RoutingTargetPendingReview}, nil
	}

	if c.RiskLevel == RiskLow && isShortFact(c) && c.Confidence >= hotMemoryPromoteConfidence {
		c.Status = StatusPending
		return c, RoutingDecision{Target: RoutingTargetPending}, nil
	}

	if isComposeType(c.MemoryType) {
		c.Status = StatusInComposePool
		return c, RoutingDecision{Target: RoutingTargetComposePool}, nil
	}

	c.Status = StatusPending
	return c, RoutingDecision{Target: RoutingTargetPending}, nil
}

// isShortFact 低风险短事实/偏好:类型为 fact/preference 且内容较短。
func isShortFact(c Candidate) bool {
	if c.MemoryType != MemoryTypeFact && c.MemoryType != MemoryTypePreference {
		return false
	}
	return len([]rune(c.Content)) <= shortFactMaxRunes
}

// isComposeType 进入归档素材池的类型。
func isComposeType(mt MemoryType) bool {
	switch mt {
	case MemoryTypeBugfix, MemoryTypeDecision, MemoryTypeRisk, MemoryTypeFollowUp:
		return true
	}
	return false
}
