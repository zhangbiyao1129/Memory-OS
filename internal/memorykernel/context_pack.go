package memorykernel

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ContextPackService 上下文包构建服务接口。
type ContextPackService interface {
	Build(ctx context.Context, request ContextPackRequest) (ContextPack, error)
}

// ContextPackRequest 构建上下文包的请求。
type ContextPackRequest struct {
	OrgID           string
	ProjectID       string
	Query           string
	MaxContextBytes int
}

// ContextPack 构建好的上下文包。
type ContextPack struct {
	RequestID string
	Intent    string
	Context   string
	Units     []MemoryUnit
	Warnings  []string
}

// typePriority 单位类型优先级，越高越优先。
var typePriority = map[UnitType]int{
	UnitProcedure:   8,
	UnitRisk:        7,
	UnitPreference:  6,
	UnitDecision:    5,
	UnitFact:        4,
	UnitEnvironment: 3,
	UnitTask:        2,
	UnitEvidence:    1,
}

// ContextPackBuilder 从 Repository 构建上下文包。
type ContextPackBuilder struct {
	repo Repository
}

func NewContextPackBuilder(repo Repository) *ContextPackBuilder {
	return &ContextPackBuilder{repo: repo}
}

func (b *ContextPackBuilder) Build(ctx context.Context, req ContextPackRequest) (ContextPack, error) {
	units, err := b.repo.ListUnits(ctx, UnitFilter{
		OrgID:     req.OrgID,
		ProjectID: req.ProjectID,
		Status:    string(UnitCurrent),
		Limit:     100,
	})
	if err != nil {
		return ContextPack{}, fmt.Errorf("list units: %w", err)
	}

	// 按优先级排序
	scored := make([]scoredUnit, 0, len(units))
	for _, u := range units {
		su := scoredUnit{unit: u, score: computeUnitScore(u, req.Query)}
		scored = append(scored, su)
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].unit.UpdatedAt.After(scored[j].unit.UpdatedAt)
	})

	// 构建 context 文本
	var sb strings.Builder
	warnings := []string{}
	maxBytes := req.MaxContextBytes
	if maxBytes <= 0 {
		maxBytes = 8192
	}

	var selectedUnits []MemoryUnit
	for _, su := range scored {
		text := formatUnit(su.unit)
		if sb.Len()+len(text) > maxBytes {
			warnings = append(warnings, fmt.Sprintf("上下文已截断，共 %d 条可信事实", len(scored)))
			break
		}
		sb.WriteString(text)
		sb.WriteString("\n\n")
		selectedUnits = append(selectedUnits, su.unit)
	}

	requestID := fmt.Sprintf("ctx_%d", time.Now().UnixNano())
	return ContextPack{
		RequestID: requestID,
		Intent:    req.Query,
		Context:   sb.String(),
		Units:     selectedUnits,
		Warnings:  warnings,
	}, nil
}

type scoredUnit struct {
	unit  MemoryUnit
	score float64
}

func computeUnitScore(u MemoryUnit, query string) float64 {
	score := u.TrustScore

	// applies_when 命中 query token 加分
	if u.AppliesWhen != "" && query != "" {
		queryTokens := strings.Fields(query)
		for _, token := range queryTokens {
			if strings.Contains(u.AppliesWhen, token) {
				score += 0.5
				break
			}
		}
	}

	// type 优先级加成
	score += float64(typePriority[u.Type]) * 0.1

	return score
}

func formatUnit(u MemoryUnit) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] %s", u.Type, u.Content))
	if u.AppliesWhen != "" {
		sb.WriteString(fmt.Sprintf("\n适用场景: %s", u.AppliesWhen))
	}
	if u.AgentShould != "" {
		sb.WriteString(fmt.Sprintf("\nAgent 行为: %s", u.AgentShould))
	}
	return sb.String()
}
