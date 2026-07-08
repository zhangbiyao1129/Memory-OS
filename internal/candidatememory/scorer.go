package candidatememory

// Scorer 为候选打分,分数影响分流(自动热记忆 vs 归档素材池)。
// Phase 3 可替换为结合 LLM confidence 与相似度的实现。
type Scorer interface {
	Score(c Candidate) Scores
}

// RuleScorer 基于类型/风险/confidence/长度的规则打分器。
type RuleScorer struct{}

func (RuleScorer) Score(c Candidate) Scores {
	hot := 0.0
	compose := 0.0

	// 类型基础倾向:短事实/偏好 → 热记忆;决策/修复/风险/后续 → 归档素材池
	switch c.MemoryType {
	case MemoryTypeFact, MemoryTypePreference:
		hot += 0.5
	case MemoryTypeDecision, MemoryTypeBugfix, MemoryTypeRisk, MemoryTypeFollowUp:
		compose += 0.5
	}

	// confidence 加权
	hot += c.Confidence * 0.4
	compose += c.Confidence * 0.3

	// 短内容(一句短事实)偏向热记忆
	if r := runeLen(c.Content); r > 0 && r <= 80 {
		hot += 0.1
	}

	// 风险最终调整:高风险显著压低热记忆(强制人工 review),并提升沉淀倾向
	switch c.RiskLevel {
	case RiskHigh:
		hot *= 0.3
		compose += 0.2
	case RiskMedium:
		hot *= 0.7
	}

	return Scores{
		HotMemoryScore: clamp01(hot),
		ComposeScore:   clamp01(compose),
	}
}

func runeLen(s string) int { return len([]rune(s)) }

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
