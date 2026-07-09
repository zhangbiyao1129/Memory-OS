package memorykernel

import (
	"context"
)

// Collector 从候选、热记忆、归档和检索日志中收集治理输入。
type Collector interface {
	Collect(ctx context.Context, scope Scope) (ClassifyInput, error)
}

// CandidateSource 候选记忆数据源。
type CandidateSource interface {
	ListKernelCandidates(ctx context.Context, scope Scope, limit int) ([]CandidateInput, error)
}

// HotMemorySource 热记忆数据源。
type HotMemorySource interface {
	ListKernelHotMemories(ctx context.Context, scope Scope, limit int) ([]HotMemoryInput, error)
}

// ArchiveSource 归档摘要数据源。
type ArchiveSource interface {
	ListKernelArchives(ctx context.Context, scope Scope, limit int) ([]ArchiveInput, error)
}

// RetrievalSource 检索日志数据源。
type RetrievalSource interface {
	ListKernelRetrievals(ctx context.Context, scope Scope, limit int) ([]RetrievalInput, error)
}

// KernelCollector 从多个数据源收集治理输入的默认实现。
type KernelCollector struct {
	candidates    CandidateSource
	hotMemories   HotMemorySource
	archives      ArchiveSource
	retrievals    RetrievalSource
	existingUnits UnitLister
}

// UnitLister 已有 memory unit 的只读查询。
type UnitLister interface {
	ListUnits(ctx context.Context, filter UnitFilter) ([]MemoryUnit, error)
}

type CollectorOptions struct {
	Candidates    CandidateSource
	HotMemories   HotMemorySource
	Archives      ArchiveSource
	Retrievals    RetrievalSource
	ExistingUnits UnitLister
}

func NewCollector(opts CollectorOptions) *KernelCollector {
	return &KernelCollector{
		candidates:    opts.Candidates,
		hotMemories:   opts.HotMemories,
		archives:      opts.Archives,
		retrievals:    opts.Retrievals,
		existingUnits: opts.ExistingUnits,
	}
}

func (c *KernelCollector) Collect(ctx context.Context, scope Scope) (ClassifyInput, error) {
	input := ClassifyInput{Scope: scope}

	const limit = 50

	if c.candidates != nil {
		candidates, err := c.candidates.ListKernelCandidates(ctx, scope, limit)
		if err != nil {
			return ClassifyInput{}, err
		}
		input.Candidates = candidates
	}

	if c.hotMemories != nil {
		hotMemories, err := c.hotMemories.ListKernelHotMemories(ctx, scope, limit)
		if err != nil {
			return ClassifyInput{}, err
		}
		input.HotMemories = hotMemories
	}

	if c.archives != nil {
		archives, err := c.archives.ListKernelArchives(ctx, scope, limit)
		if err != nil {
			return ClassifyInput{}, err
		}
		input.Archives = archives
	}

	if c.retrievals != nil {
		retrievals, err := c.retrievals.ListKernelRetrievals(ctx, scope, limit)
		if err != nil {
			return ClassifyInput{}, err
		}
		input.Retrievals = retrievals
	}

	if c.existingUnits != nil {
		units, err := c.existingUnits.ListUnits(ctx, UnitFilter{
			OrgID:     scope.OrgID,
			ProjectID: scope.ProjectID,
			Status:    string(UnitCurrent),
			Limit:     limit,
		})
		if err != nil {
			return ClassifyInput{}, err
		}
		input.ExistingUnits = units
	}

	return input, nil
}
