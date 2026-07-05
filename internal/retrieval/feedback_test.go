package retrieval

import (
	"testing"
)

func TestArchiveFeedbackTracker_HighFrequencyGeneratesCandidate(t *testing.T) {
	tracker := NewArchiveFeedbackTracker(3) // 阈值 3 次

	// 模拟同一 archive chunk 被命中 3 次
	tracker.RecordHit(ArchiveHit{ArchiveID: "archive_1", ChunkID: "chunk_1", Content: "部署步骤: docker compose up", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1"})
	tracker.RecordHit(ArchiveHit{ArchiveID: "archive_1", ChunkID: "chunk_1", Content: "部署步骤: docker compose up", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1"})
	tracker.RecordHit(ArchiveHit{ArchiveID: "archive_1", ChunkID: "chunk_1", Content: "部署步骤: docker compose up", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1"})

	candidates := tracker.PendingCandidates()
	if len(candidates) != 1 {
		t.Fatalf("pending candidates = %d, want 1", len(candidates))
	}
	c := candidates[0]
	if c.ArchiveID != "archive_1" || c.ChunkID != "chunk_1" {
		t.Fatalf("candidate source = archive_1/%s, want archive_1/chunk_1", c.ChunkID)
	}
	if c.OrgID != "org_1" || c.ProjectID != "project_1" {
		t.Fatalf("candidate scope = %s/%s, want org_1/project_1", c.OrgID, c.ProjectID)
	}
}

func TestArchiveFeedbackTracker_BelowThresholdNoCandidate(t *testing.T) {
	tracker := NewArchiveFeedbackTracker(3)

	tracker.RecordHit(ArchiveHit{ArchiveID: "archive_1", ChunkID: "chunk_1", Content: "some content", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1"})
	tracker.RecordHit(ArchiveHit{ArchiveID: "archive_1", ChunkID: "chunk_1", Content: "some content", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1"})

	candidates := tracker.PendingCandidates()
	if len(candidates) != 0 {
		t.Fatalf("pending candidates = %d, want 0 (below threshold)", len(candidates))
	}
}

func TestArchiveFeedbackTracker_DeduplicatesAfterGeneration(t *testing.T) {
	tracker := NewArchiveFeedbackTracker(2)

	tracker.RecordHit(ArchiveHit{ArchiveID: "a1", ChunkID: "c1", Content: "content", OrgID: "o1", ProjectID: "p1", UserID: "u1"})
	tracker.RecordHit(ArchiveHit{ArchiveID: "a1", ChunkID: "c1", Content: "content", OrgID: "o1", ProjectID: "p1", UserID: "u1"})

	c1 := tracker.PendingCandidates()
	if len(c1) != 1 {
		t.Fatalf("first batch = %d, want 1", len(c1))
	}

	// 再次命中不应重复生成
	tracker.RecordHit(ArchiveHit{ArchiveID: "a1", ChunkID: "c1", Content: "content", OrgID: "o1", ProjectID: "p1", UserID: "u1"})
	c2 := tracker.PendingCandidates()
	if len(c2) != 0 {
		t.Fatalf("second batch = %d, want 0 (already generated)", len(c2))
	}
}

func TestArchiveFeedbackTracker_DifferentChunks(t *testing.T) {
	tracker := NewArchiveFeedbackTracker(2)

	tracker.RecordHit(ArchiveHit{ArchiveID: "a1", ChunkID: "c1", Content: "content1", OrgID: "o1", ProjectID: "p1", UserID: "u1"})
	tracker.RecordHit(ArchiveHit{ArchiveID: "a1", ChunkID: "c2", Content: "content2", OrgID: "o1", ProjectID: "p1", UserID: "u1"})
	tracker.RecordHit(ArchiveHit{ArchiveID: "a1", ChunkID: "c1", Content: "content1", OrgID: "o1", ProjectID: "p1", UserID: "u1"})
	tracker.RecordHit(ArchiveHit{ArchiveID: "a1", ChunkID: "c2", Content: "content2", OrgID: "o1", ProjectID: "p1", UserID: "u1"})

	candidates := tracker.PendingCandidates()
	if len(candidates) != 2 {
		t.Fatalf("pending candidates = %d, want 2 (different chunks)", len(candidates))
	}
}
