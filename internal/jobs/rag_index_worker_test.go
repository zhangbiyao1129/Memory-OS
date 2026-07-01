package jobs

import (
	"testing"

	"memory-os/internal/archive"
	"memory-os/internal/rag"
)

func TestRAGIndexWorkerDedupesJob(t *testing.T) {
	service := rag.NewService(rag.NewMemoryStore())
	worker := NewRAGIndexWorker(service)
	job := RAGIndexJob{
		IdempotencyKey: "rag_job_1",
		OrgID:          "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project",
		PermissionLabels: []string{"project:project_1:read"},
		Chunks:           []archive.Chunk{{ChunkID: "chunk_1", ArchiveID: "archive_1", IndexGeneration: 1, Content: "deploy api", ContentHash: "hash"}},
	}

	first, err := worker.Handle(job)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	second, err := worker.Handle(job)
	if err != nil {
		t.Fatalf("Handle() duplicate error = %v", err)
	}
	if first.Deduped {
		t.Fatal("first deduped = true, want false")
	}
	if !second.Deduped {
		t.Fatal("second deduped = false, want true")
	}
}
