package tenant

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

type User struct {
	ID          string
	Email       string
	DisplayName string
	Status      string
}

type Org struct {
	ID     string
	Name   string
	Slug   string
	Status string
}

type Project struct {
	ID         string
	OrgID      string
	Name       string
	Slug       string
	Status     string
	SourceType string
	SourceKey  string
}

type Membership struct {
	UserID    string
	OrgID     string
	ProjectID string
	Role      string
	Status    string
}

type RoleDefinition struct {
	Role             string
	DisplayName      string
	Description      string
	PermissionLabels []string
}

type PermissionContext struct {
	UserID           string
	OrgID            string
	ProjectID        string
	AgentID          string
	PermissionLabels []string
}
