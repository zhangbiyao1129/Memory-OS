package archive

import (
	"os"
	"strings"
	"testing"
	"time"

	"memory-os/internal/eventlog"
)

func TestServiceCreateArchiveWritesFileAndMetadata(t *testing.T) {
	root := t.TempDir()
	service := NewService(NewMemoryRepository(), root)

	result, err := service.Create(CreateRequest{
		RequestID: "request_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Events:    []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Metadata.CurrentVersion != 1 || result.Metadata.IndexGeneration != 1 {
		t.Fatalf("metadata version/index = %d/%d, want 1/1", result.Metadata.CurrentVersion, result.Metadata.IndexGeneration)
	}
	content, err := os.ReadFile(result.Metadata.FilePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(content), "Deploy Notes") {
		t.Fatalf("archive content missing title: %s", content)
	}
}

func TestServiceCreateKnowledgeArchiveWritesSummaryMarkdownAndReindexes(t *testing.T) {
	root := t.TempDir()
	service := NewService(NewMemoryRepository(), root)

	result, err := service.Create(CreateRequest{
		RequestID:  "request_knowledge_1",
		ArchiveID:  "archive_knowledge_1",
		Title:      "API 地址修复知识",
		UserID:     "user_1",
		OrgID:      "org_1",
		ProjectID:  "project_1",
		CreatedAt:  time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC),
		RenderMode: "knowledge",
		Events: []eventlog.TurnEvent{
			archiveEvent("event_1", "页面请求 your-server 失败"),
			archiveEvent("event_2", "修复 useApi 默认按当前 hostname 拼接 :18081，并验证 go test ./internal/webdeploy 通过"),
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	content, err := os.ReadFile(result.Metadata.FilePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	markdown := string(content)
	for _, want := range []string{"## 结论", "## 关键事实", "## 来源", "your-server", "useApi", "event_1", "event_2"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("knowledge archive missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "## Timeline") {
		t.Fatalf("knowledge archive should not use timeline renderer:\n%s", markdown)
	}

	reindexed, err := service.Reindex(ReindexRequest{RequestID: "reindex_knowledge_1", ArchiveID: result.Metadata.ArchiveID, Reason: "test index"})
	if err != nil {
		t.Fatalf("Reindex() error = %v", err)
	}
	if len(reindexed.Chunks) == 0 {
		t.Fatal("knowledge archive reindex produced no chunks")
	}
	if !strings.Contains(reindexed.Chunks[0].Content, "## 结论") {
		t.Fatalf("first chunk should contain knowledge summary content: %+v", reindexed.Chunks[0])
	}
}

func TestServiceCreateDedupesRequestID(t *testing.T) {
	service := NewService(NewMemoryRepository(), t.TempDir())
	request := CreateRequest{
		RequestID: "request_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Now().UTC(),
		Events:    []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")},
	}
	first, err := service.Create(request)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	second, err := service.Create(request)
	if err != nil {
		t.Fatalf("Create() duplicate error = %v", err)
	}
	if !second.Deduped {
		t.Fatal("duplicate create deduped = false, want true")
	}
	if first.Metadata.ArchiveID != second.Metadata.ArchiveID {
		t.Fatal("duplicate create returned different archive")
	}
}

func TestServiceEditArchiveIncrementsVersionAndIndexGeneration(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, t.TempDir())
	created, err := service.Create(CreateRequest{
		RequestID: "request_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Now().UTC(),
		Events:    []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	edited, err := service.Edit(EditRequest{
		RequestID:   "edit_1",
		ArchiveID:   created.Metadata.ArchiveID,
		ActorUserID: "user_1",
		Reason:      "manual correction",
		Content:     "# Edited\n\nnew content",
	})
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}

	if edited.Metadata.CurrentVersion != 2 {
		t.Fatalf("version = %d, want 2", edited.Metadata.CurrentVersion)
	}
	if edited.Metadata.IndexGeneration != 2 {
		t.Fatalf("index_generation = %d, want 2", edited.Metadata.IndexGeneration)
	}
	versions, err := repo.Versions(created.Metadata.ArchiveID)
	if err != nil {
		t.Fatalf("Versions() error = %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions len = %d, want 2", len(versions))
	}
	if len(repo.AuditLogs(created.Metadata.ArchiveID)) != 1 {
		t.Fatalf("audit logs len = %d, want 1", len(repo.AuditLogs(created.Metadata.ArchiveID)))
	}
}

func TestServiceEditSanitizesSecretBeforeWritingArchive(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, t.TempDir())
	created, err := service.Create(CreateRequest{
		RequestID: "request_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Now().UTC(),
		Events:    []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	edited, err := service.Edit(EditRequest{
		RequestID:   "edit_1",
		ArchiveID:   created.Metadata.ArchiveID,
		ActorUserID: "user_1",
		Reason:      "manual correction",
		Content:     "# Edited\n\noperator pasted sk-test-redacted-example",
	})
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}

	content, err := os.ReadFile(edited.Metadata.FilePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(content), "sk-test-redacted-example") {
		t.Fatalf("edited archive leaked secret: %s", content)
	}
	if !strings.Contains(string(content), "secret_ref:") {
		t.Fatalf("edited archive missing secret_ref replacement: %s", content)
	}
	if edited.Metadata.ContentHash != contentHash(string(content)) {
		t.Fatal("metadata content hash does not match sanitized file content")
	}
}

func TestServiceDetailReadsMarkdownContent(t *testing.T) {
	service := NewService(NewMemoryRepository(), t.TempDir())
	created, err := service.Create(CreateRequest{
		RequestID: "request_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Now().UTC(),
		Events:    []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	detail, err := service.Detail(created.Metadata.ArchiveID)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}

	if detail.Metadata.ArchiveID != created.Metadata.ArchiveID {
		t.Fatalf("detail archive id = %q", detail.Metadata.ArchiveID)
	}
	if !strings.Contains(detail.Content, "Deploy Notes") || !strings.Contains(detail.Content, "deploy api") {
		t.Fatalf("detail content mismatch: %s", detail.Content)
	}
}

func TestServiceVersionsReturnsStoredVersions(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, t.TempDir())
	created, err := service.Create(CreateRequest{
		RequestID: "request_1",
		ArchiveID: "archive_1",
		Title:     "Deploy Notes",
		UserID:    "user_1",
		OrgID:     "org_1",
		ProjectID: "project_1",
		CreatedAt: time.Now().UTC(),
		Events:    []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := service.Edit(EditRequest{RequestID: "edit_1", ArchiveID: created.Metadata.ArchiveID, ActorUserID: "user_1", Reason: "manual correction", Content: "# Edited\n\nnew content"}); err != nil {
		t.Fatalf("Edit() error = %v", err)
	}

	versions, err := service.Versions(created.Metadata.ArchiveID)
	if err != nil {
		t.Fatalf("Versions() error = %v", err)
	}

	if len(versions) != 2 || versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatalf("versions mismatch: %#v", versions)
	}
}

func TestServiceListFiltersArchives(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, t.TempDir())
	if _, err := service.Create(CreateRequest{RequestID: "request_1", ArchiveID: "archive_1", Title: "One", UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", CreatedAt: time.Now().UTC(), Events: []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")}}); err != nil {
		t.Fatalf("Create(archive_1) error = %v", err)
	}
	if _, err := service.Create(CreateRequest{RequestID: "request_2", ArchiveID: "archive_2", Title: "Two", UserID: "user_2", OrgID: "org_1", ProjectID: "project_1", CreatedAt: time.Now().UTC(), Events: []eventlog.TurnEvent{archiveEvent("event_2", "other api")}}); err != nil {
		t.Fatalf("Create(archive_2) error = %v", err)
	}

	archives, err := service.List(ListFilter{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Status: "active"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(archives) != 1 || archives[0].ArchiveID != "archive_1" {
		t.Fatalf("archives mismatch: %#v", archives)
	}
}

func TestServiceDeleteSoftDeletesAndKeepsVersions(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, t.TempDir())
	created, err := service.Create(CreateRequest{RequestID: "request_1", ArchiveID: "archive_1", Title: "One", UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", CreatedAt: time.Now().UTC(), Events: []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	deleted, err := service.Delete(DeleteRequest{RequestID: "delete_1", ArchiveID: created.Metadata.ArchiveID, ActorUserID: "user_1", Reason: "cleanup"})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if deleted.Metadata.Status != "deleted" {
		t.Fatalf("status = %q, want deleted", deleted.Metadata.Status)
	}
	versions, err := service.Versions(created.Metadata.ArchiveID)
	if err != nil {
		t.Fatalf("Versions() error = %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions len = %d, want 1", len(versions))
	}
	if len(repo.AuditLogs(created.Metadata.ArchiveID)) != 1 {
		t.Fatalf("audit logs len = %d, want 1", len(repo.AuditLogs(created.Metadata.ArchiveID)))
	}
	active, err := service.List(ListFilter{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Status: "active"})
	if err != nil {
		t.Fatalf("List(active) error = %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active archives = %#v, want none", active)
	}
}

func TestServiceReindexIncrementsGenerationAndChunksMarkdown(t *testing.T) {
	service := NewService(NewMemoryRepository(), t.TempDir())
	created, err := service.Create(CreateRequest{RequestID: "request_1", ArchiveID: "archive_1", Title: "One", UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", CreatedAt: time.Now().UTC(), Events: []eventlog.TurnEvent{archiveEvent("event_1", "deploy api")}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	reindexed, err := service.Reindex(ReindexRequest{RequestID: "reindex_1", ArchiveID: created.Metadata.ArchiveID, Reason: "manual rebuild"})
	if err != nil {
		t.Fatalf("Reindex() error = %v", err)
	}

	if reindexed.Metadata.IndexGeneration != 2 {
		t.Fatalf("index generation = %d, want 2", reindexed.Metadata.IndexGeneration)
	}
	if len(reindexed.Chunks) == 0 || reindexed.Chunks[0].ArchiveID != created.Metadata.ArchiveID || reindexed.Chunks[0].IndexGeneration != 2 {
		t.Fatalf("chunks mismatch: %#v", reindexed.Chunks)
	}
}

func archiveEvent(eventID, text string) eventlog.TurnEvent {
	return eventlog.TurnEvent{
		Version:   "v1",
		EventID:   eventID,
		TurnID:    "turn_1",
		ThreadID:  "thread_1",
		SessionID: "session_1",
		Type:      eventlog.EventUserMessage,
		CreatedAt: time.Now().UTC(),
		Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
		Payload:   map[string]any{"text": text},
	}
}
