package memorykernel

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	// ErrUnitNotFound memory unit 不存在。
	ErrUnitNotFound = errors.New("memory unit not found")
	// ErrClaimNotFound memory claim 不存在。
	ErrClaimNotFound = errors.New("memory claim not found")
)

// Repository Memory Kernel 持久化接口。PG 实现见 pg_repository.go，内存实现用于测试。
type Repository interface {
	CreateRun(ctx context.Context, run GovernanceRun) (GovernanceRun, error)
	CompleteRun(ctx context.Context, runID string, update GovernanceRunUpdate) error
	FailRun(ctx context.Context, runID string, err error) error
	GetRun(ctx context.Context, runID string) (GovernanceRun, error)
	ListRuns(ctx context.Context, filter RunFilter) ([]GovernanceRun, error)
	UpsertUnit(ctx context.Context, unit MemoryUnit) (MemoryUnit, error)
	UpsertClaim(ctx context.Context, claim MemoryClaim) (MemoryClaim, error)
	ListUnits(ctx context.Context, filter UnitFilter) ([]MemoryUnit, error)
	ListClaims(ctx context.Context, filter ClaimFilter) ([]MemoryClaim, error)
	MarkUnitSuperseded(ctx context.Context, orgID, unitID, supersededBy, reason string) (MemoryUnit, error)
	RecordAction(ctx context.Context, action GovernanceAction) (GovernanceAction, error)
	ListActions(ctx context.Context, filter ActionFilter) ([]GovernanceAction, error)
	UpsertCICase(ctx context.Context, c CICase) (CICase, error)
	RecordCIResult(ctx context.Context, result CIResult) (CIResult, error)
	ListCICases(ctx context.Context, filter CICaseFilter) ([]CICase, error)
	ListCIResults(ctx context.Context, filter CIResultFilter) ([]CIResult, error)
}

// RunFilter 查询 governance runs 的过滤器。
type RunFilter struct {
	OrgID     string
	ProjectID string
	Status    RunStatus
	Limit     int
}

// InMemoryRepository 内存版 Repository，用于单元测试。
type InMemoryRepository struct {
	mu         sync.Mutex
	runs       map[string]GovernanceRun
	units      map[string]MemoryUnit
	claims     map[string]MemoryClaim
	actions    map[string]GovernanceAction
	ciCases    map[string]CICase
	ciResults  map[string]CIResult
	nextID     int64
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		runs:      map[string]GovernanceRun{},
		units:     map[string]MemoryUnit{},
		claims:    map[string]MemoryClaim{},
		actions:   map[string]GovernanceAction{},
		ciCases:   map[string]CICase{},
		ciResults: map[string]CIResult{},
	}
}

func (r *InMemoryRepository) nextIDVal() int64 {
	r.nextID++
	return r.nextID
}

func (r *InMemoryRepository) CreateRun(_ context.Context, run GovernanceRun) (GovernanceRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run.ID = r.nextIDVal()
	now := time.Now().UTC()
	run.StartedAt = now
	run.UpdatedAt = now
	run.Status = RunRunning
	r.runs[run.RunID] = run
	return run, nil
}

func (r *InMemoryRepository) CompleteRun(_ context.Context, runID string, update GovernanceRunUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return ErrUnitNotFound
	}
	run.Status = update.Status
	run.ProcessedCandidates = update.ProcessedCandidates
	run.ProcessedHotMemories = update.ProcessedHotMemories
	run.ProcessedArchives = update.ProcessedArchives
	run.CreatedUnits = update.CreatedUnits
	run.SupersededUnits = update.SupersededUnits
	run.StaleCandidates = update.StaleCandidates
	run.DemotedHotMemories = update.DemotedHotMemories
	run.CICasesCreated = update.CICasesCreated
	run.CICasesPassed = update.CICasesPassed
	run.CorrectionArchiveID = update.CorrectionArchiveID
	run.Summary = update.Summary
	now := time.Now().UTC()
	completed := now
	run.CompletedAt = &completed
	run.UpdatedAt = now
	r.runs[runID] = run
	return nil
}

func (r *InMemoryRepository) FailRun(_ context.Context, runID string, err error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return ErrUnitNotFound
	}
	run.Status = RunFailed
	run.LastError = err.Error()
	now := time.Now().UTC()
	completed := now
	run.CompletedAt = &completed
	run.UpdatedAt = now
	r.runs[runID] = run
	return nil
}

func (r *InMemoryRepository) GetRun(_ context.Context, runID string) (GovernanceRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return GovernanceRun{}, ErrUnitNotFound
	}
	return run, nil
}

func (r *InMemoryRepository) ListRuns(_ context.Context, filter RunFilter) ([]GovernanceRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []GovernanceRun
	for _, run := range r.runs {
		if filter.OrgID != "" && run.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && run.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Status != "" && run.Status != filter.Status {
			continue
		}
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (r *InMemoryRepository) UpsertUnit(_ context.Context, unit MemoryUnit) (MemoryUnit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := r.units[unit.UnitID]; ok {
		if unit.TrustScore < existing.TrustScore {
			return existing, nil
		}
		unit.ID = existing.ID
		unit.CreatedAt = existing.CreatedAt
	} else {
		unit.ID = r.nextIDVal()
		unit.CreatedAt = now
	}
	unit.UpdatedAt = now
	r.units[unit.UnitID] = unit
	return unit, nil
}

func (r *InMemoryRepository) UpsertClaim(_ context.Context, claim MemoryClaim) (MemoryClaim, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := claim.UnitID + "/" + claim.Subject + "/" + claim.Predicate
	if existing, ok := r.claims[key]; ok {
		claim.ID = existing.ID
		claim.CreatedAt = existing.CreatedAt
	} else {
		claim.ID = r.nextIDVal()
		claim.CreatedAt = time.Now().UTC()
	}
	r.claims[key] = claim
	return claim, nil
}

func (r *InMemoryRepository) ListUnits(_ context.Context, filter UnitFilter) ([]MemoryUnit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []MemoryUnit
	for _, u := range r.units {
		if filter.OrgID != "" && u.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && u.ProjectID != filter.ProjectID {
			continue
		}
		if filter.SourceKey != "" && u.SourceKey != filter.SourceKey {
			continue
		}
		if filter.Status != "" && string(u.Status) != filter.Status {
			continue
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (r *InMemoryRepository) ListClaims(_ context.Context, filter ClaimFilter) ([]MemoryClaim, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []MemoryClaim
	for _, c := range r.claims {
		if filter.OrgID != "" && c.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && c.ProjectID != filter.ProjectID {
			continue
		}
		if filter.UnitID != "" && c.UnitID != filter.UnitID {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (r *InMemoryRepository) MarkUnitSuperseded(_ context.Context, orgID, unitID, supersededBy, _ string) (MemoryUnit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.units[unitID]
	if !ok || u.OrgID != orgID {
		return MemoryUnit{}, ErrUnitNotFound
	}
	u.Status = UnitSuperseded
	u.SupersededBy = supersededBy
	u.UpdatedAt = time.Now().UTC()
	r.units[unitID] = u
	return u, nil
}

func (r *InMemoryRepository) RecordAction(_ context.Context, action GovernanceAction) (GovernanceAction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	action.ID = r.nextIDVal()
	action.CreatedAt = time.Now().UTC()
	r.actions[action.ActionID] = action
	return action, nil
}

func (r *InMemoryRepository) ListActions(_ context.Context, filter ActionFilter) ([]GovernanceAction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []GovernanceAction
	for _, a := range r.actions {
		if filter.OrgID != "" && a.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && a.ProjectID != filter.ProjectID {
			continue
		}
		if filter.RunID != "" && a.RunID != filter.RunID {
			continue
		}
		if filter.TargetType != "" && a.TargetType != filter.TargetType {
			continue
		}
		if filter.TargetID != "" && a.TargetID != filter.TargetID {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (r *InMemoryRepository) UpsertCICase(_ context.Context, c CICase) (CICase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := r.ciCases[c.CaseID]; ok {
		c.ID = existing.ID
		c.CreatedAt = existing.CreatedAt
	} else {
		c.ID = r.nextIDVal()
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	r.ciCases[c.CaseID] = c
	return c, nil
}

func (r *InMemoryRepository) RecordCIResult(_ context.Context, result CIResult) (CIResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result.ID = r.nextIDVal()
	result.CreatedAt = time.Now().UTC()
	r.ciResults[result.ResultID] = result
	return result, nil
}

func (r *InMemoryRepository) ListCICases(_ context.Context, filter CICaseFilter) ([]CICase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []CICase
	for _, c := range r.ciCases {
		if filter.OrgID != "" && c.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && c.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Status != "" && c.Status != filter.Status {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (r *InMemoryRepository) ListCIResults(_ context.Context, filter CIResultFilter) ([]CIResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []CIResult
	for _, res := range r.ciResults {
		if filter.CaseID != "" && res.CaseID != filter.CaseID {
			continue
		}
		if filter.RunID != "" && res.RunID != filter.RunID {
			continue
		}
		out = append(out, res)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}
