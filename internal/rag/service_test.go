package rag

import (
	"testing"

	"memory-os/internal/archive"
	"memory-os/internal/qdrant"
)

func TestServiceIndexesAndSearchesWithFilter(t *testing.T) {
	service := NewService(NewMemoryStore())
	chunks := []archive.Chunk{{
		ChunkID: "chunk_1", ArchiveID: "archive_1", IndexGeneration: 1, ChunkIndex: 0,
		Content: "deploy api to t480", ContentHash: "hash_1", SourceEventIDs: []string{"event_1"},
	}}

	if err := service.Index(IndexRequest{
		OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project",
		PermissionLabels: []string{"project:project_1:read"}, Chunks: chunks,
	}); err != nil {
		t.Fatalf("Index() error = %v", err)
	}

	results, err := service.Search(SearchRequest{
		Query: "deploy",
		Filter: qdrant.PayloadFilter{Must: map[string][]string{
			"doc_type": {"archive_chunk"}, "org_id": {"org_1"}, "project_id": {"project_1"}, "user_id": {"user_1"},
			"visibility": {"project"}, "permission_labels": {"project:project_1:read"}, "index_generation": {"1"},
		}},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Source.ChunkID != "chunk_1" || results[0].Source.ArchiveID != "archive_1" {
		t.Fatalf("source mismatch: %#v", results[0].Source)
	}
}

func TestSearchRejectsMissingFilter(t *testing.T) {
	service := NewService(NewMemoryStore())

	_, err := service.Search(SearchRequest{Query: "deploy"})

	if err == nil {
		t.Fatal("Search() error = nil, want missing filter rejection")
	}
}

func TestSearchDoesNotReturnCrossTenantOrOldGeneration(t *testing.T) {
	service := NewService(NewMemoryStore())
	if err := service.Index(IndexRequest{
		OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project",
		PermissionLabels: []string{"project:project_1:read"},
		Chunks:           []archive.Chunk{{ChunkID: "chunk_old", ArchiveID: "archive_1", IndexGeneration: 1, Content: "old deploy note", ContentHash: "old"}},
	}); err != nil {
		t.Fatalf("Index old error = %v", err)
	}
	if err := service.Index(IndexRequest{
		OrgID: "org_2", ProjectID: "project_2", UserID: "user_2", Visibility: "project",
		PermissionLabels: []string{"project:project_2:read"},
		Chunks:           []archive.Chunk{{ChunkID: "chunk_other", ArchiveID: "archive_2", IndexGeneration: 2, Content: "deploy secret elsewhere", ContentHash: "other"}},
	}); err != nil {
		t.Fatalf("Index other error = %v", err)
	}
	if err := service.Index(IndexRequest{
		OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project",
		PermissionLabels: []string{"project:project_1:read"},
		Chunks:           []archive.Chunk{{ChunkID: "chunk_new", ArchiveID: "archive_1", IndexGeneration: 2, Content: "new deploy note", ContentHash: "new"}},
	}); err != nil {
		t.Fatalf("Index new error = %v", err)
	}

	results, err := service.Search(SearchRequest{
		Query: "deploy",
		Filter: qdrant.PayloadFilter{Must: map[string][]string{
			"doc_type": {"archive_chunk"}, "org_id": {"org_1"}, "project_id": {"project_1"}, "user_id": {"user_1"},
			"visibility": {"project"}, "permission_labels": {"project:project_1:read"}, "index_generation": {"2"},
		}},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].Source.ChunkID != "chunk_new" {
		t.Fatalf("results = %#v, want only chunk_new", results)
	}
}
