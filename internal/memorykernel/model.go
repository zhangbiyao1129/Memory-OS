package memorykernel

import "time"

// UnitType memory unit 类型，覆盖 Agent 任务中常见的记忆形式。
type UnitType string

const (
	UnitFact        UnitType = "fact"
	UnitPreference  UnitType = "preference"
	UnitProcedure   UnitType = "procedure"
	UnitDecision    UnitType = "decision"
	UnitRisk        UnitType = "risk"
	UnitTask        UnitType = "task"
	UnitEnvironment UnitType = "environment"
	UnitEvidence    UnitType = "evidence"
)

// UnitStatus memory unit 生命周期状态。
type UnitStatus string

const (
	UnitCurrent     UnitStatus = "current"
	UnitStale       UnitStatus = "stale"
	UnitSuperseded  UnitStatus = "superseded"
	UnitHistorical  UnitStatus = "historical"
	UnitNeedsReview UnitStatus = "needs_review"
)

// SourceRef 记忆来源引用，用于追溯到候选、归档或热记忆。
type SourceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// MemoryUnit Agent 可信上下文层的基本单元。
type MemoryUnit struct {
	ID           int64
	UnitID       string
	OrgID        string
	ProjectID    string
	SourceKey    string
	ThreadID     string
	UserID       string
	AgentID      string
	Type         UnitType
	Content      string
	AppliesWhen  string
	AgentShould  string
	Status       UnitStatus
	Confidence   float64
	TrustScore   float64
	RiskLevel    string
	SourceRefs   []SourceRef
	SupersededBy string
	ValidFrom    *time.Time
	ValidTo      *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// MemoryClaim 事实类附属结构，用于冲突判断。
type MemoryClaim struct {
	ID           int64
	ClaimID      string
	UnitID       string
	OrgID        string
	ProjectID    string
	Subject      string
	Predicate    string
	Value        string
	Polarity     string
	Confidence   float64
	EvidenceRefs []SourceRef
	ObservedAt   *time.Time
	CreatedAt    time.Time
}

// RunStatus 治理运行状态。
type RunStatus string

const (
	RunRunning RunStatus = "running"
	RunDone    RunStatus = "done"
	RunFailed  RunStatus = "failed"
)

// GovernanceRun 一次记忆治理操作的完整记录。
type GovernanceRun struct {
	ID                     int64
	RunID                  string
	OrgID                  string
	ProjectID              string
	SourceKey              string
	ThreadID               string
	TriggerType            string
	Status                 RunStatus
	ProcessedCandidates    int
	ProcessedHotMemories   int
	ProcessedArchives      int
	CreatedUnits           int
	SupersededUnits        int
	StaleCandidates        int
	DemotedHotMemories     int
	CICasesCreated         int
	CICasesPassed          int
	CorrectionArchiveID    string
	Summary                string
	LastError              string
	StartedAt              time.Time
	CompletedAt            *time.Time
	UpdatedAt              time.Time
}

// GovernanceRunUpdate 用于更新 run 的统计字段。
type GovernanceRunUpdate struct {
	Status                 RunStatus
	ProcessedCandidates    int
	ProcessedHotMemories   int
	ProcessedArchives      int
	CreatedUnits           int
	SupersededUnits        int
	StaleCandidates        int
	DemotedHotMemories     int
	CICasesCreated         int
	CICasesPassed          int
	CorrectionArchiveID    string
	Summary                string
	LastError              string
}

// GovernanceActionName 治理动作白名单。
type GovernanceActionName string

const (
	ActionKeep                GovernanceActionName = "keep"
	ActionDiscardNoise        GovernanceActionName = "discard_noise"
	ActionDiscardStale        GovernanceActionName = "discard_stale"
	ActionMarkSuperseded      GovernanceActionName = "mark_superseded"
	ActionDemoteHotMemory     GovernanceActionName = "demote_hot_memory"
	ActionCreateUnit          GovernanceActionName = "create_memory_unit"
	ActionSupersedeUnit       GovernanceActionName = "supersede_memory_unit"
	ActionCreateCorrection    GovernanceActionName = "create_correction_archive"
	ActionCreateCICase        GovernanceActionName = "create_ci_case"
	ActionNeedsReview         GovernanceActionName = "needs_review"
)

// GovernanceAction 一次治理动作的审计记录。
type GovernanceAction struct {
	ID           int64
	ActionID     string
	RunID        string
	OrgID        string
	ProjectID    string
	TargetType   string
	TargetID     string
	Action       GovernanceActionName
	Reason       string
	EvidenceRefs []SourceRef
	Applied      bool
	CreatedAt    time.Time
}

// CICase 记忆 CI 验收用例。
type CICase struct {
	ID              int64
	CaseID          string
	OrgID           string
	ProjectID       string
	SourceKey       string
	Question        string
	MustInclude     []string
	MustNotInclude  []string
	Status          string
	SourceRunID     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CIResult 记忆 CI 验收结果。
type CIResult struct {
	ID              int64
	ResultID        string
	CaseID          string
	RunID           string
	RequestID       string
	Passed          bool
	MatchedInclude  []string
	MatchedExclude  []string
	ResponseExcerpt string
	CreatedAt       time.Time
}

// --- 输入输出共享类型 ---

// Scope 治理操作的作用域。
type Scope struct {
	OrgID     string
	ProjectID string
	SourceKey string
	ThreadID  string
	UserID    string
	AgentID   string
}

// CandidateInput 供 classifier 使用的候选输入。
type CandidateInput struct {
	ID         string
	Content    string
	Summary    string
	Type       string
	RiskLevel  string
	Status     string
	Confidence float64
}

// HotMemoryInput 供 classifier 使用的热记忆输入。
type HotMemoryInput struct {
	ID            string
	Fact          string
	Status        string
	AccessCount   int
	ReturnedCount int
	UsedCount     int
	Pinned        bool
}

// ArchiveInput 供 classifier 使用的归档摘要输入。
type ArchiveInput struct {
	ID        string
	Title     string
	Excerpt   string
	Status    string
	UpdatedAt time.Time
}

// RetrievalInput 供 classifier 使用的检索日志输入。
type RetrievalInput struct {
	RequestID  string
	SourceKind string
	SourceID   string
	CreatedAt  time.Time
}

// ClassifyInput classifier 的完整输入。
type ClassifyInput struct {
	Scope         Scope
	Candidates    []CandidateInput
	HotMemories   []HotMemoryInput
	Archives      []ArchiveInput
	Retrievals     []RetrievalInput
	ExistingUnits []MemoryUnit
}

// ClassifyResult classifier 的输出。
type ClassifyResult struct {
	Units   []MemoryUnit
	Claims  []MemoryClaim
	Actions []GovernanceAction
	CICases []CICase
	Summary string
}

// UnitFilter 查询 memory_units 的过滤器。
type UnitFilter struct {
	OrgID     string
	ProjectID string
	SourceKey string
	Status    string
	Limit     int
}

// ClaimFilter 查询 memory_claims 的过滤器。
type ClaimFilter struct {
	OrgID     string
	ProjectID string
	UnitID    string
	Limit     int
}

// ActionFilter 查询 governance actions 的过滤器。
type ActionFilter struct {
	OrgID      string
	ProjectID  string
	RunID      string
	TargetType string
	TargetID   string
	Limit      int
}

// CICaseFilter 查询 CI cases 的过滤器。
type CICaseFilter struct {
	OrgID     string
	ProjectID string
	Status    string
	Limit     int
}

// CIResultFilter 查询 CI results 的过滤器。
type CIResultFilter struct {
	CaseID string
	RunID  string
	Limit  int
}

// GovernanceRequest 触发治理的请求。
type GovernanceRequest struct {
	OrgID       string
	ProjectID   string
	SourceKey   string
	ThreadID    string
	TriggerType string
}

// CorrectionArchiveRequest 创建修订归档的请求。
type CorrectionArchiveRequest struct {
	OrgID     string
	ProjectID string
	SourceKey string
	ThreadID  string
	UserID    string
	Units     []MemoryUnit
	Actions   []GovernanceAction
}

// CorrectionArchiveResult 创建修订归档的结果。
type CorrectionArchiveResult struct {
	ArchiveID string
	Title     string
}
