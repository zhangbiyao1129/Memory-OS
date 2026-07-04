package archive

import "time"

type Metadata struct {
	ArchiveID       string
	UserID          string
	OrgID           string
	ProjectID       string
	Title           string
	FilePath        string
	Status          string
	IndexGeneration int
	CurrentVersion  int
	ContentHash     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ListFilter struct {
	UserID    string
	OrgID     string
	ProjectID string
	Status    string
	Limit     int
}

type Version struct {
	ArchiveID    string
	Version      int
	FilePath     string
	ContentHash  string
	EditorUserID string
	Reason       string
	CreatedAt    time.Time
}

type EditAuditLog struct {
	ArchiveID      string
	ActorUserID    string
	OldVersion     int
	NewVersion     int
	OldContentHash string
	NewContentHash string
	RequestID      string
	Reason         string
	CreatedAt      time.Time
}
