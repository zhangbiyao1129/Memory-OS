package retrieval

import "sync"

// ArchiveHit 记录一次 Archive chunk 命中。
type ArchiveHit struct {
	ArchiveID string
	ChunkID   string
	Content   string
	OrgID     string
	ProjectID string
	UserID    string
}

// ArchiveCandidate 由高频 Archive 命中生成的摘要型候选。
type ArchiveCandidate struct {
	ArchiveID string
	ChunkID   string
	Content   string
	OrgID     string
	ProjectID string
	UserID    string
	HitCount  int
}

// ArchiveFeedbackTracker 跟踪 Archive chunk 命中频率,
// 达到阈值时生成摘要型候选(不直接变 hot memory)。
type ArchiveFeedbackTracker struct {
	mu        sync.Mutex
	hits      map[string]int        // key: archiveID/chunkID
	generated map[string]bool       // 已生成候选的 chunk,避免重复
	content   map[string]string     // key → content
	scope     map[string]ArchiveHit // key → scope info
	threshold int
	pending   []ArchiveCandidate
}

// NewArchiveFeedbackTracker 创建 tracker,threshold 为生成候选所需的最小命中次数。
func NewArchiveFeedbackTracker(threshold int) *ArchiveFeedbackTracker {
	if threshold <= 0 {
		threshold = 3
	}
	return &ArchiveFeedbackTracker{
		hits:      map[string]int{},
		generated: map[string]bool{},
		content:   map[string]string{},
		scope:     map[string]ArchiveHit{},
		threshold: threshold,
	}
}

func archiveFeedbackKey(hit ArchiveHit) string {
	return hit.ArchiveID + "/" + hit.ChunkID
}

// RecordHit 记录一次 Archive chunk 命中。
func (t *ArchiveFeedbackTracker) RecordHit(hit ArchiveHit) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := archiveFeedbackKey(hit)
	t.hits[key]++
	t.content[key] = hit.Content
	t.scope[key] = hit

	if t.hits[key] >= t.threshold && !t.generated[key] {
		t.generated[key] = true
		t.pending = append(t.pending, ArchiveCandidate{
			ArchiveID: hit.ArchiveID,
			ChunkID:   hit.ChunkID,
			Content:   hit.Content,
			OrgID:     hit.OrgID,
			ProjectID: hit.ProjectID,
			UserID:    hit.UserID,
			HitCount:  t.hits[key],
		})
	}
}

// PendingCandidates 返回待处理的高频 Archive 候选(取出后清空)。
func (t *ArchiveFeedbackTracker) PendingCandidates() []ArchiveCandidate {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.pending) == 0 {
		return nil
	}
	out := t.pending
	t.pending = nil
	return out
}
