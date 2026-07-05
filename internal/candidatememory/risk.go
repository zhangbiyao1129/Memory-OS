package candidatememory

import "strings"

// highRiskKeywords 默认高风险关键词。命中任一即把候选提升为 RiskHigh,进入 pending_review。
var highRiskKeywords = []string{
	"secret", "token", "密码", "权限", "删除", "部署", "生产", "线上", "schema", "迁移", "数据库",
}

// AssessRisk 根据内容关键词评估风险等级。显式 high 保持;关键词命中提升为 high;否则保持 base。
func AssessRisk(content string, base RiskLevel) RiskLevel {
	if base == RiskHigh {
		return RiskHigh
	}
	lowered := strings.ToLower(content)
	for _, kw := range highRiskKeywords {
		if strings.Contains(lowered, strings.ToLower(kw)) {
			return RiskHigh
		}
	}
	return base
}
