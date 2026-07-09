package candidatememory

import "time"

// MemoryType 候选记忆类型。Phase 3 提炼器固定输出这六类。
type MemoryType string

const (
	MemoryTypeFact       MemoryType = "fact"
	MemoryTypeDecision   MemoryType = "decision"
	MemoryTypeBugfix     MemoryType = "bugfix"
	MemoryTypePreference MemoryType = "preference"
	MemoryTypeRisk       MemoryType = "risk"
	MemoryTypeFollowUp   MemoryType = "follow_up"
)

// RiskLevel 候选风险等级。高风险关键词命中默认提升为 high。
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// Status 候选生命周期状态。
type Status string

const (
	StatusPending       Status = "pending"         // 新建,等待分流/整理
	StatusAccepted      Status = "accepted"        // 用户确认接受
	StatusDiscarded     Status = "discarded"       // 用户丢弃
	StatusPromotedToHot Status = "promoted_to_hot" // 自动/手动提升为热记忆
	StatusInComposePool Status = "in_compose_pool" // 归档素材池
	StatusComposed      Status = "composed"        // 已归档进 Archive
	StatusSuperseded    Status = "superseded"      // 被新事实覆盖(Memory Kernel 治理)
)

// JobStatus 候选提炼任务状态(对应 candidate_memory_jobs.status)。
type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

// Candidate 一条候选记忆,绑定 project_id + source_key + thread_id/session_id,
// 避免同名 project 串数据(硬规则 4)。
type Candidate struct {
	CandidateID    string
	OrgID          string
	ProjectID      string
	SourceKey      string
	UserID         string
	AgentID        string
	ThreadID       string
	SessionID      string
	SourceEventIDs []string
	MemoryType     MemoryType
	Content        string
	Summary        string
	RiskLevel      RiskLevel
	Confidence     float64
	Status         Status
	NeedsReview      bool   // AI 整理判定为待人工确认(needs_review 动作)
	GovernanceReason string // Memory Kernel 治理原因
	SupersededBy     string // 被哪个 memory unit 覆盖
	SimilarRefs      []SimilarRef
	Scores         Scores
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SimilarRef 相似候选/记忆引用,BGE embedding 相似度、去重、聚类用。
type SimilarRef struct {
	Kind       string  `json:"kind"` // candidate / hot_memory / archive
	RefID      string  `json:"ref_id"`
	Similarity float64 `json:"similarity"`
}

// Scores 打分明细,影响分流(hot memory vs compose pool)。
type Scores struct {
	HotMemoryScore float64 `json:"hot_memory_score"`
	ComposeScore   float64 `json:"compose_score"`
}

// Job 候选提炼任务(对应 candidate_memory_jobs 表),archive_jobs 风格的幂等重试。
type Job struct {
	ID             int64
	IdempotencyKey string
	OrgID          string
	ProjectID      string
	SourceKey      string
	SourceEventID  string
	Status         JobStatus
	Attempts       int
	MaxAttempts    int
	LockedBy       string
	LockedUntil    *time.Time
	LastError      string
	CandidateIDs   []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

// TopicState 归档任务状态(org+project+source_key+thread 维度,对应 topic_memory_states 表)。
type TopicState struct {
	ID                int64
	OrgID             string
	ProjectID         string
	SourceKey         string
	ThreadID          string
	CandidateCount    int
	CompletionScore   float64
	LastEventAt       *time.Time
	ReadyToCompose    bool
	ComposedArchiveID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
