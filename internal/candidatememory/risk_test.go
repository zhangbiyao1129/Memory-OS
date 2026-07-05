package candidatememory

import "testing"

func TestAssessRiskKeepsExplicitHigh(t *testing.T) {
	if got := AssessRisk("普通内容", RiskHigh); got != RiskHigh {
		t.Fatalf("显式 high 应保持: %v", got)
	}
}

func TestAssessRiskKeywordPromotesToHigh(t *testing.T) {
	cases := []string{
		"修改了生产数据库 schema",
		"线上部署需要鉴权",
		"删除旧迁移脚本",
		"this rotates the secret key",
		"用户密码重置流程",
		"调整权限策略",
		"刷新访问 token",
	}
	for _, content := range cases {
		if got := AssessRisk(content, RiskLow); got != RiskHigh {
			t.Fatalf("高风险关键词应提升为 high,内容=%q 得到 %v", content, got)
		}
	}
}

func TestAssessRiskStaysLowWithoutKeyword(t *testing.T) {
	if got := AssessRisk("项目使用 Go/Hertz 技术栈", RiskLow); got != RiskLow {
		t.Fatalf("无关键词应保持 low: %v", got)
	}
}
