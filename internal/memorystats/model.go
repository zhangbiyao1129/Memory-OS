package memorystats

type Filter struct {
	UserID           string
	OrgID            string
	ProjectID        string
	PermissionLabels []string
}

type Snapshot struct {
	Archives    AssetStats     `json:"archives"`
	HotMemories HotMemoryStats `json:"hot_memories"`
	Candidates  CandidateStats `json:"candidates"`
	Topics      TopicStats     `json:"topics"`
}

type AssetStats struct {
	Total    int64            `json:"total"`
	ByStatus map[string]int64 `json:"by_status"`
}

type HotMemoryStats struct {
	Total    int64            `json:"total"`
	ByStatus map[string]int64 `json:"by_status"`
}

type CandidateStats struct {
	Total               int64            `json:"total"`
	ByStatus            map[string]int64 `json:"by_status"`
	ByRisk              map[string]int64 `json:"by_risk"`
	HotScoreBuckets     []ScoreBucket    `json:"hot_score_buckets"`
	ComposeScoreBuckets []ScoreBucket    `json:"compose_score_buckets"`
}

type TopicStats struct {
	Total          int64 `json:"total"`
	ReadyToCompose int64 `json:"ready_to_compose"`
	Composed       int64 `json:"composed"`
	Open           int64 `json:"open"`
}

type ScoreBucket struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}
