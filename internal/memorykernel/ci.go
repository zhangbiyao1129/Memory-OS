package memorykernel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"memory-os/internal/secret"
)

// SearchService 记忆搜索接口，CI 用它验证召回。
type SearchService interface {
	Search(ctx context.Context, query string) (string, error)
}

// CIRunnerImpl 记忆 CI 执行器。
type CIRunnerImpl struct {
	repo          Repository
	searchService SearchService
}

func NewCIRunner(repo Repository, search SearchService) *CIRunnerImpl {
	return &CIRunnerImpl{repo: repo, searchService: search}
}

// RunCase 运行一个 CI 用例并返回结果。
func (r *CIRunnerImpl) RunCase(ctx context.Context, caseID string) (CIResult, error) {
	cases, err := r.repo.ListCICases(ctx, CICaseFilter{CaseID: caseID, Limit: 1})
	if err != nil {
		return CIResult{}, fmt.Errorf("list ci cases: %w", err)
	}
	if len(cases) == 0 {
		return CIResult{}, fmt.Errorf("ci case %q not found", caseID)
	}
	c := cases[0]

	// 执行搜索
	response, err := r.searchService.Search(ctx, c.Question)
	if err != nil {
		return CIResult{}, fmt.Errorf("search: %w", err)
	}

	// 截断并脱敏
	if len(response) > 2000 {
		response = response[:2000]
	}
	sanitized := secret.Sanitize(response, nil)
	response = sanitized.Text

	// 检查 must_include
	matchedInclude := []string{}
	allIncluded := true
	for _, phrase := range c.MustInclude {
		if strings.Contains(response, phrase) {
			matchedInclude = append(matchedInclude, phrase)
		} else {
			allIncluded = false
		}
	}

	// 检查 must_not_include
	matchedExclude := []string{}
	for _, phrase := range c.MustNotInclude {
		if strings.Contains(response, phrase) {
			matchedExclude = append(matchedExclude, phrase)
		}
	}

	passed := allIncluded && len(matchedExclude) == 0
	requestID := fmt.Sprintf("memory_ci_%s_%d", caseID, time.Now().UnixNano())

	return CIResult{
		ResultID:        fmt.Sprintf("ci_result_%s_%d", caseID, time.Now().UnixNano()),
		CaseID:          caseID,
		RequestID:       requestID,
		Passed:          passed,
		MatchedInclude:  matchedInclude,
		MatchedExclude:  matchedExclude,
		ResponseExcerpt: response,
	}, nil
}

// fakeSearchService 测试用的搜索服务。
type fakeSearchService struct {
	context string
}

func (f fakeSearchService) Search(_ context.Context, _ string) (string, error) {
	return f.context, nil
}
