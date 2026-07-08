package candidatememory

import (
	"strings"
	"time"
)

// TriageScope 表示候选记忆 triage 的作用域。
type TriageScope string

const (
	TriageScopeProject      TriageScope = "project"
	TriageScopeGlobal       TriageScope = "global"
	TriageScopeTooling      TriageScope = "tooling"
	TriageScopePersonalPref TriageScope = "personal_pref"
	TriageScopeInbox        TriageScope = "inbox"
	TriageScopeDiscard      TriageScope = "discard"
)

// Valid 检查 scope 合法性。
func (s TriageScope) Valid() bool {
	switch s {
	case TriageScopeProject, TriageScopeGlobal, TriageScopeTooling, TriageScopePersonalPref, TriageScopeInbox, TriageScopeDiscard:
		return true
	default:
		return false
	}
}

// TriageReviewState 记录 triage 的复核状态。
type TriageReviewState string

const (
	TriageReviewAutoApplied TriageReviewState = "auto_applied"
	TriageReviewWeak        TriageReviewState = "weak"
	TriageReviewNeedsReview TriageReviewState = "needs_review"
	TriageReviewRejected    TriageReviewState = "rejected"
)

// Valid 检查 review_state 合法性。
func (s TriageReviewState) Valid() bool {
	switch s {
	case TriageReviewAutoApplied, TriageReviewWeak, TriageReviewNeedsReview, TriageReviewRejected:
		return true
	default:
		return false
	}
}

// GlobalHotMemoryProjectID is the sentinel project id for user-level global hot memory.
const GlobalHotMemoryProjectID = "__global__"

// TriageSourceRef 记录 triage 中引用到的来源。
type TriageSourceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// TriageResult 是一次候选 triage 的持久化结果。
type TriageResult struct {
	ID                   int64
	OrgID                string
	CandidateID          string
	SourceProjectID      string
	SourceKey            string
	TriageScope          TriageScope
	Confidence           float64
	ReviewState          TriageReviewState
	Reason               string
	SourceRefs           []TriageSourceRef
	PromotedHotMemoryIDs []string
	Attempts             int
	LastError            string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// TriageScanFilter 控制候选 triage 扫描。
type TriageScanFilter struct {
	OrgID         string
	MinConfidence float64
	Scope         TriageScope
	Limit         int
}

// TriageListFilter 控制 triage 结果查询。
type TriageListFilter struct {
	OrgID           string
	SourceProjectID string
	ReviewState     TriageReviewState
	SourceKey       string
	Limit           int
	Offset          int
}

// CandidateProjectLink 是候选跨项目映射。
type CandidateProjectLink struct {
	ID                  int64
	OrgID               string
	CandidateID         string
	LinkedProjectID     string
	LinkedSourceKey     string
	Confidence          float64
	Evidence            string
	Status              string
	PromotedHotMemoryID string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// CandidateProjectLinksFilter 控制项目链接查询。
type CandidateProjectLinksFilter struct {
	OrgID           string
	CandidateID     string
	LinkedProjectID string
	Status          string
	MinConfidence   float64
	Limit           int
}

func normalizeReviewState(state TriageReviewState) TriageReviewState {
	trimmed := strings.ToLower(strings.TrimSpace(string(state)))

	switch TriageReviewState(trimmed) {
	case TriageReviewAutoApplied, TriageReviewWeak, TriageReviewNeedsReview, TriageReviewRejected:
		return TriageReviewState(trimmed)
	default:
		return TriageReviewWeak
	}
}
