package hotmemory

import "time"

type Scope string

const (
	ScopeUser          Scope = "user"
	ScopeProject       Scope = "project"
	ScopeOrg           Scope = "org"
	ScopeAgentSpecific Scope = "agent_specific"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusPromoted Status = "promoted"
	StatusDemoted  Status = "demoted"
	StatusDeleted  Status = "deleted"
)

type SourceType string

const (
	SourceTurnEvent SourceType = "turn_event"
	SourceArchive   SourceType = "archive"
)

type Memory struct {
	MemoryID         string
	OrgID            string
	ProjectID        string
	UserID           string
	AgentID          string
	Scope            Scope
	Visibility       string
	PermissionLabels []string
	Fact             string
	FactHash         string
	Sources          []Source
	Confidence       float64
	AccessCount      int
	UsedCount        int
	HotScore         float64
	Status           Status
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

type Source struct {
	SourceType SourceType
	SourceRef  string
	Confidence float64
}

type UpsertRequest struct {
	OrgID            string
	ProjectID        string
	UserID           string
	AgentID          string
	Scope            Scope
	Visibility       string
	PermissionLabels []string
	Fact             string
	SourceType       SourceType
	SourceRef        string
	Confidence       float64
}

type SearchRequest struct {
	Query  string
	Filter PayloadFilter
}

type SearchResult struct {
	Memory Memory
	Score  float64
}

type Candidate struct {
	Fact       string
	Scope      Scope
	SourceType SourceType
	SourceRef  string
	Confidence float64
}
