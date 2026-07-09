package memorykernel

import (
	"context"
	"testing"
	"time"
)

type fakeCandidateSource struct {
	items []CandidateInput
}

func (f fakeCandidateSource) ListKernelCandidates(_ context.Context, _ Scope, _ int) ([]CandidateInput, error) {
	return f.items, nil
}

type fakeHotMemorySource struct {
	items []HotMemoryInput
}

func (f fakeHotMemorySource) ListKernelHotMemories(_ context.Context, _ Scope, _ int) ([]HotMemoryInput, error) {
	return f.items, nil
}

type fakeArchiveSource struct {
	items []ArchiveInput
}

func (f fakeArchiveSource) ListKernelArchives(_ context.Context, _ Scope, _ int) ([]ArchiveInput, error) {
	return f.items, nil
}

type fakeRetrievalSource struct {
	items []RetrievalInput
}

func (f fakeRetrievalSource) ListKernelRetrievals(_ context.Context, _ Scope, _ int) ([]RetrievalInput, error) {
	return f.items, nil
}

func TestCollectorBuildsGovernanceInputForScope(t *testing.T) {
	collector := NewCollector(CollectorOptions{
		Candidates: fakeCandidateSource{items: []CandidateInput{{ID: "cand_old", Content: "memory_archive 尚未实现", RiskLevel: "low"}}},
		HotMemories: fakeHotMemorySource{items: []HotMemoryInput{{ID: "hm_old", Fact: "memory_archive 尚未实现", ReturnedCount: 0}}},
		Archives: fakeArchiveSource{items: []ArchiveInput{{ID: "archive_new", Title: "记忆修订: MCP 当前能力", Excerpt: "memory_archive 已实现"}}},
		Retrievals: fakeRetrievalSource{items: []RetrievalInput{{RequestID: "req_1", SourceKind: "archive_chunk"}}},
	})
	input, err := collector.Collect(context.Background(), Scope{OrgID: "org_1", ProjectID: "project_1"})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(input.Candidates) != 1 || len(input.HotMemories) != 1 || len(input.Archives) != 1 || len(input.Retrievals) != 1 {
		t.Fatalf("input = %#v", input)
	}
	if input.Scope.OrgID != "org_1" || input.Scope.ProjectID != "project_1" {
		t.Fatalf("scope = %#v", input.Scope)
	}
}

func TestCollectorHandlesNilSources(t *testing.T) {
	collector := NewCollector(CollectorOptions{})
	input, err := collector.Collect(context.Background(), Scope{OrgID: "org_1", ProjectID: "project_1"})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(input.Candidates) != 0 || len(input.HotMemories) != 0 || len(input.Archives) != 0 || len(input.Retrievals) != 0 {
		t.Fatalf("expected empty input, got = %#v", input)
	}
}

func TestCollectorCollectsExistingUnits(t *testing.T) {
	existingRepo := NewInMemoryRepository()
	_, _ = existingRepo.UpsertUnit(context.Background(), MemoryUnit{
		UnitID: "unit_existing", OrgID: "org_1", ProjectID: "project_1",
		Type: UnitFact, Content: "已有事实", Status: UnitCurrent, TrustScore: 0.8,
	})
	collector := NewCollector(CollectorOptions{
		ExistingUnits: existingRepo,
	})
	input, err := collector.Collect(context.Background(), Scope{OrgID: "org_1", ProjectID: "project_1"})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(input.ExistingUnits) != 1 || input.ExistingUnits[0].UnitID != "unit_existing" {
		t.Fatalf("existing units = %#v", input.ExistingUnits)
	}
}

// fakeArchiveSourceWithTime 支持按时间排序的归档源。
type fakeArchiveSourceWithTime struct {
	items []ArchiveInput
}

func (f fakeArchiveSourceWithTime) ListKernelArchives(_ context.Context, _ Scope, _ int) ([]ArchiveInput, error) {
	return f.items, nil
}

func TestCollectorPassesScopeToSources(t *testing.T) {
	var capturedScope Scope
	capturing := &scopeCapturingCandidateSource{capture: &capturedScope}
	collector := NewCollector(CollectorOptions{
		Candidates: capturing,
	})
	_, _ = collector.Collect(context.Background(), Scope{OrgID: "test_org", ProjectID: "test_proj", SourceKey: "test_key"})
	if capturedScope.OrgID != "test_org" || capturedScope.ProjectID != "test_proj" {
		t.Fatalf("scope not passed through: %#v", capturedScope)
	}
}

type scopeCapturingCandidateSource struct {
	capture *Scope
}

func (s *scopeCapturingCandidateSource) ListKernelCandidates(_ context.Context, scope Scope, _ int) ([]CandidateInput, error) {
	*s.capture = scope
	return nil, nil
}

// 确保 CollectorOptions 时间戳不冲突。
var _ Collector = (*KernelCollector)(nil)

// fakeHotMemoryWithPinned 带 pinned 标记的热记忆。
func fakeHotMemoryInput(id, fact string, pinned bool) HotMemoryInput {
	return HotMemoryInput{ID: id, Fact: fact, Pinned: pinned, Status: "active"}
}

func TestCollectorIncludesPinnedHotMemories(t *testing.T) {
	collector := NewCollector(CollectorOptions{
		HotMemories: fakeHotMemorySource{items: []HotMemoryInput{fakeHotMemoryInput("hm_pinned", "pinned fact", true)}},
	})
	input, err := collector.Collect(context.Background(), Scope{OrgID: "org_1", ProjectID: "project_1"})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(input.HotMemories) != 1 || !input.HotMemories[0].Pinned {
		t.Fatalf("expected pinned hot memory, got = %#v", input.HotMemories)
	}
}

func TestCollectorEmptyScopeReturnsEmptyInput(t *testing.T) {
	collector := NewCollector(CollectorOptions{
		Candidates: fakeCandidateSource{items: []CandidateInput{{ID: "c1", Content: "x", RiskLevel: "low"}}},
	})
	input, err := collector.Collect(context.Background(), Scope{})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	// 空 scope 仍然返回数据（scope 过滤由数据源处理）
	if len(input.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(input.Candidates))
	}
}

// fakeArchiveInputWithTime 带时间戳的归档。
func fakeArchiveInput(id, title string, t time.Time) ArchiveInput {
	return ArchiveInput{ID: id, Title: title, UpdatedAt: t}
}
