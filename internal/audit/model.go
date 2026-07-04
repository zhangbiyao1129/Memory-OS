package audit

import "time"

type Log struct {
	ID           string
	ActorUserID  string
	OrgID        string
	ProjectID    string
	Action       string
	ResourceType string
	ResourceID   string
	RequestID    string
	Result       string
	Metadata     map[string]string
	CreatedAt    time.Time
}

type ListFilter struct {
	OrgID        string
	ProjectID    string
	ActorUserID  string
	ResourceType string
	ResourceID   string
	Limit        int
}
