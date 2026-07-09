package main

import (
	"context"
	"errors"
	"testing"

	"memory-os/internal/archive"
	"memory-os/internal/memorykernel"
)

type archiveListRepoStub struct {
	expectedUserID string
	lastFilter     archive.ListFilter
	archives       []archive.Metadata
}

func (r *archiveListRepoStub) SaveCreate(metadata archive.Metadata, version archive.Version, eventIDs []string, requestID string) (archive.Metadata, bool, error) {
	return archive.Metadata{}, false, errors.New("not implemented")
}

func (r *archiveListRepoStub) Get(archiveID string) (archive.Metadata, error) {
	return archive.Metadata{}, errors.New("not implemented")
}

func (r *archiveListRepoStub) List(filter archive.ListFilter) ([]archive.Metadata, error) {
	r.lastFilter = filter
	if filter.UserID != r.expectedUserID {
		return nil, nil
	}
	return append([]archive.Metadata(nil), r.archives...), nil
}

func (r *archiveListRepoStub) SaveEdit(metadata archive.Metadata, version archive.Version, audit archive.EditAuditLog, requestID string) (archive.Metadata, bool, error) {
	return archive.Metadata{}, false, errors.New("not implemented")
}

func (r *archiveListRepoStub) SoftDelete(metadata archive.Metadata, audit archive.EditAuditLog, requestID string) (archive.Metadata, bool, error) {
	return archive.Metadata{}, false, errors.New("not implemented")
}

func (r *archiveListRepoStub) MarkReindex(metadata archive.Metadata, requestID string, reason string) (archive.Metadata, bool, error) {
	return archive.Metadata{}, false, errors.New("not implemented")
}

func (r *archiveListRepoStub) Versions(archiveID string) ([]archive.Version, error) {
	return nil, errors.New("not implemented")
}

func TestProductionArchiveSourceUsesScopeUserID(t *testing.T) {
	repo := &archiveListRepoStub{
		expectedUserID: "user_1",
		archives: []archive.Metadata{{
			ArchiveID: "archive_1",
			UserID:    "user_1",
			OrgID:     "org_1",
			ProjectID: "project_1",
			Title:     "archive 1",
		}},
	}
	adapter := memorykernel.NewArchiveSource(repo)

	listed, err := adapter.ListKernelArchives(context.Background(), memorykernel.Scope{
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
	}, 10)
	if err != nil {
		t.Fatalf("ListKernelArchives() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListKernelArchives() = %d items, want 1", len(listed))
	}
	if repo.lastFilter.UserID != "user_1" {
		t.Fatalf("archive list filter user_id = %q, want user_1", repo.lastFilter.UserID)
	}
}
