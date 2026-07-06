package candidatememory

import "strings"

// GateDecision 门控决策结果。
type GateDecision struct {
	Allow  bool
	Reason string
}

// ShouldExtract 判断是否应该对 ExtractionRequest 进行 LLM 提炼。
// 规则:
// 1. manual_archive_request 永远允许
// 2. assistant_final 只有命中长期价值信号才允许
// 3. 多事件中只要有一个允许就允许
func ShouldExtract(request ExtractionRequest) GateDecision {
	if len(request.Events) == 0 {
		return GateDecision{Allow: false, Reason: "empty_events"}
	}

	// 聚合所有事件的决策
	anyAllow := false
	var lastDecision GateDecision
	for _, event := range request.Events {
		decision := evaluateEvent(event)
		lastDecision = decision
		if decision.Allow {
			anyAllow = true
			break
		}
	}

	if anyAllow {
		// 如果第一个允许的事件是 manual_archive,返回 manual_archive
		for _, event := range request.Events {
			decision := evaluateEvent(event)
			if decision.Allow {
				return decision
			}
		}
		return GateDecision{Allow: true, Reason: "valuable_signal"}
	}
	return lastDecision
}

func evaluateEvent(event ExtractionEvent) GateDecision {
	switch event.Type {
	case "manual_archive_request":
		return GateDecision{Allow: true, Reason: "manual_archive"}
	case "assistant_final":
		return evaluateAssistantFinal(event)
	default:
		// 未知类型直接拒绝,不进入噪声检测
		return GateDecision{Allow: false, Reason: "unsupported_event_type"}
	}
}

func evaluateAssistantFinal(event ExtractionEvent) GateDecision {
	text := extractText(event.Payload)
	if text == "" {
		return GateDecision{Allow: false, Reason: "noise_only"}
	}

	// 长期价值信号检测
	if hasLongTermValue(text) {
		return GateDecision{Allow: true, Reason: "valuable_signal"}
	}

	// 噪声检测
	if isNoise(text) {
		return GateDecision{Allow: false, Reason: "noise_only"}
	}

	// 默认拒绝(保守策略:不确定时跳过提炼)
	return GateDecision{Allow: false, Reason: "noise_only"}
}

// extractText 从 payload JSON 中提取 text 字段。
// 简单实现:查找 "text":"..." 模式,避免引入 json 依赖。
func extractText(payload []byte) string {
	s := string(payload)
	// 查找 "text":"
	idx := strings.Index(s, `"text":"`)
	if idx < 0 {
		return ""
	}
	start := idx + len(`"text":"`)
	// 查找结尾引号(处理转义)
	end := start
	for end < len(s) {
		if s[end] == '\\' {
			end += 2 // 跳过转义字符
			continue
		}
		if s[end] == '"' {
			break
		}
		end++
	}
	if end >= len(s) {
		return ""
	}
	return s[start:end]
}

// hasLongTermValue 检测文本是否包含长期价值信号。
func hasLongTermValue(text string) bool {
	lower := strings.ToLower(text)

	// 用户偏好信号
	preferencePatterns := []string{
		"我希望", "以后都", "默认", "不要", "必须",
		"prefer", "always", "default", "must", "never",
	}
	for _, p := range preferencePatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	// 稳定事实信号
	factPatterns := []string{
		"项目架构", "长期配置", "固定端口", "固定部署",
		"使用", "采用",
	}
	for _, p := range factPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	// 明确决策信号
	decisionPatterns := []string{
		"决定采用", "最终方案", "以后按这个",
		"decided", "final decision",
	}
	for _, p := range decisionPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	// 明确待办信号
	followUpPatterns := []string{
		"需要后续", "待处理", "下次要",
		"todo", "follow up", "action item",
	}
	for _, p := range followUpPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	// 明确风险/约束信号
	riskPatterns := []string{
		"安全红线", "不能", "禁止", "高风险",
		"security", "forbidden", "high risk", "must not",
	}
	for _, p := range riskPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	return false
}

// isNoise 检测文本是否为噪声(应跳过提炼)。
func isNoise(text string) bool {
	lower := strings.ToLower(text)

	noisePatterns := []string{
		// 命令/测试/构建输出
		"命令完成", "测试通过", "构建成功", "build succeeded",
		"test passed", "command completed",
		// 文件路径访问
		"正在查看", "我会检查", "查看文件",
		"checking file", "viewing",
		// 过程描述
		"我正在", "我会", "让我",
		"i am", "let me",
		// 模型/权限/调试
		"模型名", "权限模式", "debug",
		"model name", "permission mode",
	}

	for _, p := range noisePatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	return false
}
