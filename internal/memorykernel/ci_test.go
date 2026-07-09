package memorykernel

import (
	"context"
	"testing"
)

func TestCIRunnerPassesWhenAllPhrasesMatch(t *testing.T) {
	repo := NewInMemoryRepository()
	_, _ = repo.UpsertCICase(context.Background(), CICase{
		CaseID: "ci_pass", OrgID: "org_1", ProjectID: "project_1",
		Question:       "memory_archive 实现了吗？",
		MustInclude:    []string{"已实现", "已部署"},
		MustNotInclude: []string{"尚未实现"},
		Status:         "active",
	})
	runner := NewCIRunner(repo, fakeSearchService{context: "memory_archive 已实现，已部署到生产环境"})
	result, err := runner.RunCase(context.Background(), "ci_pass")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if !result.Passed {
		t.Fatalf("result should pass: %#v", result)
	}
	if len(result.MatchedInclude) != 2 {
		t.Fatalf("matched include = %v, want 2", result.MatchedInclude)
	}
}

func TestCIRunnerFailsWhenForbiddenPhraseAppears(t *testing.T) {
	repo := NewInMemoryRepository()
	_, _ = repo.UpsertCICase(context.Background(), CICase{
		CaseID: "ci_memory_archive", OrgID: "org_1", ProjectID: "project_1",
		Question:       "memory_archive 现在实现了吗？",
		MustInclude:    []string{"已实现"},
		MustNotInclude: []string{"尚未实现"},
		Status:         "active",
	})
	runner := NewCIRunner(repo, fakeSearchService{context: "memory_archive 尚未实现"})
	result, err := runner.RunCase(context.Background(), "ci_memory_archive")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("result should fail: %#v", result)
	}
	if len(result.MatchedExclude) != 1 {
		t.Fatalf("matched exclude = %#v", result.MatchedExclude)
	}
}

func TestCIRunnerFailsWhenMustIncludeMissing(t *testing.T) {
	repo := NewInMemoryRepository()
	_, _ = repo.UpsertCICase(context.Background(), CICase{
		CaseID: "ci_missing", OrgID: "org_1", ProjectID: "project_1",
		Question:    "功能状态？",
		MustInclude: []string{"已上线"},
		Status:      "active",
	})
	runner := NewCIRunner(repo, fakeSearchService{context: "功能还在开发中"})
	result, err := runner.RunCase(context.Background(), "ci_missing")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("result should fail when must_include missing")
	}
}
