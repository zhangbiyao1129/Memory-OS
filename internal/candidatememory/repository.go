package candidatememory

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	// ErrNotFound 候选/任务/主题不存在。
	ErrNotFound = errors.New("candidate not found")
	// ErrConflict 候选已存在(同 org + candidate_id)。
	ErrConflict = errors.New("candidate already exists")
)

// ListFilter 候选列表过滤。SourceKey 用于隔离同名 project(硬规则 4)。
type ListFilter struct {
	OrgID     string
	ProjectID string
	SourceKey string
	ThreadID  string
	Status    Status
	RiskLevel RiskLevel
	Limit     int
}

// TopicStateFilter 主题状态列表过滤。
type TopicStateFilter struct {
	OrgID     string
	ProjectID string
	SourceKey string
	Limit     int
}

// Repository 候选记忆持久化接口。PG 实现见 pg_repository.go,内存实现用于测试。
// 所有写操作必须以 org_id 限定,避免跨租户读写。
type Repository interface {
	CreateCandidate(ctx context.Context, c Candidate) (Candidate, error)
	GetCandidate(ctx context.Context, orgID, candidateID string) (Candidate, error)
	ListCandidates(ctx context.Context, filter ListFilter) ([]Candidate, error)
	UpdateCandidateStatus(ctx context.Context, orgID, candidateID string, status Status, scores Scores, needsReview bool) (Candidate, error)
	UpdateCandidateGovernance(ctx context.Context, orgID, candidateID string, status Status, needsReview bool, reason string, supersededBy string) (Candidate, error)

	UpsertJob(ctx context.Context, job Job) (Job, error)
	LeaseJob(ctx context.Context, now time.Time, lockedBy string, lockTTL time.Duration) (*Job, error)
	CompleteJob(ctx context.Context, id int64, candidateIDs []string) error
	FailJob(ctx context.Context, id int64, lastError string) error

	UpsertTopicState(ctx context.Context, ts TopicState) (TopicState, error)
	GetTopicState(ctx context.Context, orgID, projectID, sourceKey, threadID string) (TopicState, error)
	ListTopicStates(ctx context.Context, filter TopicStateFilter) ([]TopicState, error)
}

// InMemoryRepository 内存版 Repository,用于单元测试与本地无 DB 场景。
type InMemoryRepository struct {
	mu          sync.Mutex
	candidates  map[string]Candidate
	jobs        map[string]Job // key: idempotency_key
	jobByID     map[int64]string
	jobOrder    []string // 创建顺序,Lease 按 FIFO
	topics      map[string]TopicState
	nextJobID   int64
	nextTopicID int64
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		candidates: map[string]Candidate{},
		jobs:       map[string]Job{},
		jobByID:    map[int64]string{},
		topics:     map[string]TopicState{},
	}
}

func candidateKey(orgID, candidateID string) string {
	return orgID + "/" + candidateID
}

func topicKey(orgID, projectID, sourceKey, threadID string) string {
	return orgID + "/" + projectID + "/" + sourceKey + "/" + threadID
}

func (r *InMemoryRepository) CreateCandidate(ctx context.Context, c Candidate) (Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c.OrgID == "" || c.CandidateID == "" {
		return Candidate{}, errors.New("org_id and candidate_id are required")
	}
	key := candidateKey(c.OrgID, c.CandidateID)
	if _, ok := r.candidates[key]; ok {
		return Candidate{}, ErrConflict
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	c.SourceEventIDs = append([]string(nil), c.SourceEventIDs...)
	c.SimilarRefs = append([]SimilarRef(nil), c.SimilarRefs...)
	r.candidates[key] = c
	return cloneCandidate(c), nil
}

func (r *InMemoryRepository) GetCandidate(ctx context.Context, orgID, candidateID string) (Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.candidates[candidateKey(orgID, candidateID)]
	if !ok {
		return Candidate{}, ErrNotFound
	}
	return cloneCandidate(c), nil
}

func (r *InMemoryRepository) ListCandidates(ctx context.Context, filter ListFilter) ([]Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Candidate{}
	for _, c := range r.candidates {
		if !matchCandidate(c, filter) {
			continue
		}
		out = append(out, cloneCandidate(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func matchCandidate(c Candidate, f ListFilter) bool {
	if f.OrgID != "" && c.OrgID != f.OrgID {
		return false
	}
	if f.ProjectID != "" && c.ProjectID != f.ProjectID {
		return false
	}
	if f.SourceKey != "" && c.SourceKey != f.SourceKey {
		return false
	}
	if f.ThreadID != "" && c.ThreadID != f.ThreadID {
		return false
	}
	if f.Status != "" && c.Status != f.Status {
		return false
	}
	if f.RiskLevel != "" && c.RiskLevel != f.RiskLevel {
		return false
	}
	return true
}

func (r *InMemoryRepository) UpdateCandidateStatus(ctx context.Context, orgID, candidateID string, status Status, scores Scores, needsReview bool) (Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := candidateKey(orgID, candidateID)
	c, ok := r.candidates[key]
	if !ok {
		return Candidate{}, ErrNotFound
	}
	c.Status = status
	c.Scores = scores
	c.NeedsReview = needsReview
	c.UpdatedAt = time.Now().UTC()
	r.candidates[key] = c
	return cloneCandidate(c), nil
}

func (r *InMemoryRepository) UpdateCandidateGovernance(_ context.Context, orgID, candidateID string, status Status, needsReview bool, reason string, supersededBy string) (Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := candidateKey(orgID, candidateID)
	c, ok := r.candidates[key]
	if !ok {
		return Candidate{}, ErrNotFound
	}
	c.Status = status
	c.NeedsReview = needsReview
	c.GovernanceReason = reason
	c.SupersededBy = supersededBy
	c.UpdatedAt = time.Now().UTC()
	r.candidates[key] = c
	return cloneCandidate(c), nil
}

func (r *InMemoryRepository) UpsertJob(ctx context.Context, job Job) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job.IdempotencyKey == "" {
		return Job{}, errors.New("idempotency_key is required")
	}
	if existing, ok := r.jobs[job.IdempotencyKey]; ok {
		return cloneJob(existing), nil
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 3
	}
	if job.Status == "" {
		job.Status = JobPending
	}
	r.nextJobID++
	job.ID = r.nextJobID
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	r.jobs[job.IdempotencyKey] = job
	r.jobByID[job.ID] = job.IdempotencyKey
	r.jobOrder = append(r.jobOrder, job.IdempotencyKey)
	return cloneJob(job), nil
}

func (r *InMemoryRepository) LeaseJob(ctx context.Context, now time.Time, lockedBy string, lockTTL time.Duration) (*Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range r.jobOrder {
		job := r.jobs[key]
		if job.Status != JobPending {
			continue
		}
		job.Status = JobRunning
		job.LockedBy = lockedBy
		until := now.Add(lockTTL)
		job.LockedUntil = &until
		job.Attempts++
		job.UpdatedAt = now
		r.jobs[key] = job
		cloned := cloneJob(job)
		return &cloned, nil
	}
	return nil, nil
}

func (r *InMemoryRepository) CompleteJob(ctx context.Context, id int64, candidateIDs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.jobByID[id]
	if !ok {
		return ErrNotFound
	}
	job := r.jobs[key]
	now := time.Now().UTC()
	job.Status = JobDone
	job.CandidateIDs = append([]string(nil), candidateIDs...)
	job.LastError = ""
	completed := now
	job.CompletedAt = &completed
	job.UpdatedAt = now
	r.jobs[key] = job
	return nil
}

func (r *InMemoryRepository) FailJob(ctx context.Context, id int64, lastError string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.jobByID[id]
	if !ok {
		return ErrNotFound
	}
	job := r.jobs[key]
	now := time.Now().UTC()
	job.LastError = lastError
	job.UpdatedAt = now
	job.LockedBy = ""
	job.LockedUntil = nil
	// 达到最大重试次数则永久失败,否则回到 pending 等待下次 lease。
	if job.Attempts >= job.MaxAttempts {
		job.Status = JobFailed
	} else {
		job.Status = JobPending
	}
	r.jobs[key] = job
	return nil
}

func (r *InMemoryRepository) UpsertTopicState(ctx context.Context, ts TopicState) (TopicState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := topicKey(ts.OrgID, ts.ProjectID, ts.SourceKey, ts.ThreadID)
	now := time.Now().UTC()
	if existing, ok := r.topics[key]; ok {
		ts.ID = existing.ID
		ts.CreatedAt = existing.CreatedAt
		ts.UpdatedAt = now
		r.topics[key] = ts
		return cloneTopicState(ts), nil
	}
	r.nextTopicID++
	ts.ID = r.nextTopicID
	if ts.CreatedAt.IsZero() {
		ts.CreatedAt = now
	}
	ts.UpdatedAt = now
	r.topics[key] = ts
	return cloneTopicState(ts), nil
}

func (r *InMemoryRepository) GetTopicState(ctx context.Context, orgID, projectID, sourceKey, threadID string) (TopicState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ts, ok := r.topics[topicKey(orgID, projectID, sourceKey, threadID)]
	if !ok {
		return TopicState{}, ErrNotFound
	}
	return cloneTopicState(ts), nil
}

func (r *InMemoryRepository) ListTopicStates(ctx context.Context, filter TopicStateFilter) ([]TopicState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []TopicState{}
	for _, ts := range r.topics {
		if filter.OrgID != "" && ts.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && ts.ProjectID != filter.ProjectID {
			continue
		}
		if filter.SourceKey != "" && ts.SourceKey != filter.SourceKey {
			continue
		}
		out = append(out, cloneTopicState(ts))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func cloneCandidate(c Candidate) Candidate {
	c.SourceEventIDs = append([]string(nil), c.SourceEventIDs...)
	c.SimilarRefs = append([]SimilarRef(nil), c.SimilarRefs...)
	return c
}

func cloneJob(j Job) Job {
	j.CandidateIDs = append([]string(nil), j.CandidateIDs...)
	if j.LockedUntil != nil {
		v := *j.LockedUntil
		j.LockedUntil = &v
	}
	if j.CompletedAt != nil {
		v := *j.CompletedAt
		j.CompletedAt = &v
	}
	return j
}

func cloneTopicState(ts TopicState) TopicState {
	if ts.LastEventAt != nil {
		v := *ts.LastEventAt
		ts.LastEventAt = &v
	}
	return ts
}
