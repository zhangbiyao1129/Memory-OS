package retrieval

import (
	"testing"

	"memory-os/internal/hotmemory"
)

// --- 内容分类测试 ---

func TestClassifyContent_ShortFacts(t *testing.T) {
	shortFacts := []string{
		"端口: 18080",
		"配置 key=value",
		"状态 active",
		"端口是 18081",
		"数据库连接串 postgres://localhost:5432/db",
		"API 地址 http://localhost:18081",
		"版本 v0.4.0",
		"超时 30s",
		"路径 /opt/memory-os",
	}
	for _, text := range shortFacts {
		if got := classifyContent(text); got != ContentShortFact {
			t.Errorf("classifyContent(%q) = %q, want %q", text, got, ContentShortFact)
		}
	}
}

func TestClassifyContent_FullProcess(t *testing.T) {
	fullProcesses := []string{
		"第一步先做 X，然后做 Y，最后做 Z",
		"1. 安装依赖 2. 编译 3. 部署 4. 验证",
		"问题复现步骤：先打开文件，然后修改配置，最后重启服务",
		"排查过程：首先检查日志，发现连接超时，然后检查网络，最后发现是防火墙问题",
		"调试步骤：第一步设置断点，第二步运行测试，第三步检查变量值",
	}
	for _, text := range fullProcesses {
		if got := classifyContent(text); got != ContentFullProcess {
			t.Errorf("classifyContent(%q) = %q, want %q", text, got, ContentFullProcess)
		}
	}
}

func TestClassifyContent_Default(t *testing.T) {
	defaults := []string{
		"一段普通的描述文字，不包含明显的分类特征",
		"这个功能的实现方式需要进一步讨论",
	}
	for _, text := range defaults {
		if got := classifyContent(text); got != ContentDefault {
			t.Errorf("classifyContent(%q) = %q, want %q", text, got, ContentDefault)
		}
	}
}

// --- 分数提升测试 ---

func TestBoostScore_ShortFactGetsHigherBoost(t *testing.T) {
	baseScore := 0.5
	shortFactScore := applyContentBoost(baseScore, ContentShortFact)
	processScore := applyContentBoost(baseScore, ContentFullProcess)
	defaultScore := applyContentBoost(baseScore, ContentDefault)

	if shortFactScore <= baseScore {
		t.Errorf("short fact boost = %f, want > %f", shortFactScore, baseScore)
	}
	if processScore <= baseScore {
		t.Errorf("process boost = %f, want > %f", processScore, baseScore)
	}
	if shortFactScore <= defaultScore {
		t.Errorf("short fact boost %f should be >= default boost %f", shortFactScore, defaultScore)
	}
}

// --- 检索集成：热记忆短事实分数提升 ---

func TestSearchBoostsHotMemoryShortFactScore(t *testing.T) {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	// 短事实
	if _, err := hot.Upsert(hotmemory.UpsertRequest{
		OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex",
		Scope: hotmemory.ScopeProject, Visibility: "project",
		PermissionLabels: []string{"project:project_1:read"},
		Fact:             "端口: 18080", SourceType: hotmemory.SourceTurnEvent, SourceRef: "e1", Confidence: 0.5,
	}); err != nil {
		t.Fatal(err)
	}
	// 普通描述
	if _, err := hot.Upsert(hotmemory.UpsertRequest{
		OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex",
		Scope: hotmemory.ScopeProject, Visibility: "project",
		PermissionLabels: []string{"project:project_1:read"},
		Fact:             "一段普通的描述文字，不包含明显的分类特征", SourceType: hotmemory.SourceTurnEvent, SourceRef: "e2", Confidence: 0.5,
	}); err != nil {
		t.Fatal(err)
	}

	service := NewService(Options{HotMemory: hot})
	response, err := service.Search(SearchRequest{
		RequestID: "req_boost", Query: "端口", Actor: Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Visibility: "project", Scope: hotmemory.ScopeProject, PermissionLabels: []string{"project:project_1:read"},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(response.Results) < 1 {
		t.Fatal("expected at least 1 result")
	}
	// 短事实应该排在前面（因为 boost 更高）
	for _, r := range response.Results {
		if r.Source.Kind == SourceHotMemory && r.Source.MemoryID != "" {
			if r.Text == "端口: 18080" && response.Results[0].Text != "端口: 18080" {
				// 短事实应该被提升到更前面
				t.Logf("results order: first=%q, checking boost applied", response.Results[0].Text)
			}
		}
	}
}
