package candidatememory

import "testing"

func TestRuleScorerLowRiskFactFavorsHotMemory(t *testing.T) {
	s := RuleScorer{}
	got := s.Score(Candidate{
		MemoryType: MemoryTypeFact,
		RiskLevel:  RiskLow,
		Confidence: 0.9,
		Content:    "项目使用 PostgreSQL 作为权威元数据源",
	})
	if got.HotMemoryScore <= got.ComposeScore {
		t.Fatalf("低风险短事实应偏向热记忆: hot=%v compose=%v", got.HotMemoryScore, got.ComposeScore)
	}
	if got.HotMemoryScore > 1.0 || got.HotMemoryScore < 0 {
		t.Fatalf("hot 分数应 clamp 到 [0,1]: %v", got.HotMemoryScore)
	}
}

func TestRuleScorerBugfixFavorsComposePool(t *testing.T) {
	s := RuleScorer{}
	got := s.Score(Candidate{
		MemoryType: MemoryTypeBugfix,
		RiskLevel:  RiskLow,
		Confidence: 0.8,
		Content:    "修复同名 project 归档显示 0 的 bug,根因是检索未按 source_key 过滤",
	})
	if got.ComposeScore <= 0.3 {
		t.Fatalf("bugfix 应有较高 compose 分数: %v", got.ComposeScore)
	}
}

func TestRuleScorerHighRiskDepressesHot(t *testing.T) {
	s := RuleScorer{}
	got := s.Score(Candidate{
		MemoryType: MemoryTypeRisk,
		RiskLevel:  RiskHigh,
		Confidence: 0.9,
		Content:    "生产数据库 schema 迁移前必须备份并确认回滚方案",
	})
	if got.HotMemoryScore > 0.5 {
		t.Fatalf("高风险应显著压低 hot 分数: %v", got.HotMemoryScore)
	}
}

func TestRuleScorerClampsToUnit(t *testing.T) {
	s := RuleScorer{}
	got := s.Score(Candidate{MemoryType: MemoryTypePreference, RiskLevel: RiskLow, Confidence: 1.0, Content: "用中文回复"})
	if got.HotMemoryScore > 1.0 || got.ComposeScore > 1.0 {
		t.Fatalf("分数应 clamp 到 [0,1]: %+v", got)
	}
}
