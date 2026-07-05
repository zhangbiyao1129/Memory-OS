package retrieval

import (
	"regexp"
	"strings"
)

// ContentKind 内容分类结果。
type ContentKind string

const (
	ContentShortFact   ContentKind = "short_fact"   // 端口/配置/状态/版本等短事实
	ContentFullProcess ContentKind = "full_process" // 步骤/排查/调试等完整过程
	ContentDefault     ContentKind = "default"      // 默认/未分类
)

// 短事实特征：包含端口/配置/状态/地址/版本/路径/超时/key=value 等。
var shortFactPatterns = regexp.MustCompile(`(?i)(端口|port|配置|config|状态|status|地址|address|版本|version|路径|path|超时|timeout|连接|url|http|postgres|redis|key\s*[=:])`)

// 完整过程特征：步骤/排查/调试/复现/首先…然后…最后…/第N步 等。
var fullProcessPatterns = regexp.MustCompile(`(?i)(第[一二三四五六七八九十\d]+步|步骤|排查过程|调试步骤|复现步骤|首先.*然后|1\.\s|2\.\s)`)

// classifyContent 根据文本特征分类内容类型。
// 完整过程优先于短事实（因为过程文本常包含"配置""超时"等词）。
func classifyContent(text string) ContentKind {
	if fullProcessPatterns.MatchString(text) {
		return ContentFullProcess
	}
	if shortFactPatterns.MatchString(text) {
		return ContentShortFact
	}
	return ContentDefault
}

// applyContentBoost 根据内容类型对分数施加 boost。
// 短事实 +20%（热记忆偏好：简短可直接注入上下文）；完整过程 +10%（Archive 偏好：过程可追溯）。
func applyContentBoost(score float64, kind ContentKind) float64 {
	switch kind {
	case ContentShortFact:
		return score * 1.2
	case ContentFullProcess:
		return score * 1.1
	default:
		return score
	}
}

// boostCandidate 根据候选文本内容分类并提升分数。
func boostCandidate(c *candidate) {
	kind := classifyContent(c.text)
	c.score = applyContentBoost(c.score, kind)
}

// normalizeFact 归一化文本用于去重（与 hotmemory 包同名但独立实现，仅用于 retrieval 层）。
func normalizeForDedupe(text string) string {
	return strings.TrimSpace(strings.ToLower(text))
}
