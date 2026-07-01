package audit

type Log struct {
	ActorUserID  string
	OrgID        string
	ProjectID    string
	Action       string
	ResourceType string
	ResourceID   string
	RequestID    string
	Result       string
	Metadata     map[string]string
}
