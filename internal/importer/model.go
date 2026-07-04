package importer

type SourceType string

const (
	SourceMem0       SourceType = "mem0"
	SourceFastGPT    SourceType = "fastgpt"
	SourceOpenMemory SourceType = "openmemory"
	SourceZep        SourceType = "zep"
	SourceKhoj       SourceType = "khoj"
	SourceBundle     SourceType = "bundle"
)

type ItemKind string

const (
	KindHotMemory ItemKind = "hot_memory"
	KindArchive   ItemKind = "archive"
)

type Scope struct {
	UserID           string
	OrgID            string
	ProjectID        string
	AgentID          string
	Visibility       string
	PermissionLabels []string
}

func DefaultScope() Scope {
	return Scope{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "importer", Visibility: "project", PermissionLabels: []string{"project:project_1:read"}}
}

type ImportRequest struct {
	BatchID    string
	SourceType SourceType
	Content    []byte
	Scope      Scope
}

type ImportResult struct {
	BatchID      string       `json:"batch_id"`
	SourceType   SourceType   `json:"source_type"`
	DryRun       bool         `json:"dry_run"`
	ItemCount    int          `json:"item_count"`
	CreatedCount int          `json:"created_count"`
	DedupedCount int          `json:"deduped_count"`
	Preview      []ImportItem `json:"preview,omitempty"`
}

type ImportItem struct {
	BatchID    string            `json:"batch_id"`
	SourceType SourceType        `json:"source_type"`
	ExternalID string            `json:"external_id"`
	Kind       ItemKind          `json:"kind"`
	Text       string            `json:"text"`
	SourceRef  map[string]string `json:"source_ref"`
}

type Bundle struct {
	Markdown       string `json:"markdown"`
	MetadataJSON   string `json:"metadata_json"`
	SourceRefsJSON string `json:"source_refs_json"`
}
