package memorystats

type Filter struct {
	UserID           string
	OrgID            string
	ProjectID        string
	PermissionLabels []string
}

type Snapshot struct {
	Archives      AssetStats        `json:"archives"`
	HotMemories   HotMemoryStats    `json:"hot_memories"`
	Candidates    CandidateStats    `json:"candidates"`
	CandidateJobs CandidateJobStats `json:"candidate_jobs"`
	Topics        TopicStats        `json:"topics"`
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
	ActionableTotal     int64            `json:"actionable_total"`
	ByStatus            map[string]int64 `json:"by_status"`
	ByRisk              map[string]int64 `json:"by_risk"`
	HotScoreBuckets     []ScoreBucket    `json:"hot_score_buckets"`
	ComposeScoreBuckets []ScoreBucket    `json:"compose_score_buckets"`
}

// CandidateJobStats 候选提炼任务健康统计。
type CandidateJobStats struct {
	Total           int64            `json:"total"`
	ByStatus        map[string]int64 `json:"by_status"`
	Pending         int64            `json:"pending"`
	Running         int64            `json:"running"`
	Failed          int64            `json:"failed"`
	Done            int64            `json:"done"`
	LatestError     string           `json:"latest_error"`
	OldestPendingAt string           `json:"oldest_pending_at"`
	LastCompletedAt string           `json:"last_completed_at"`
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
