package importer

import (
	"context"
	"os"
	"strings"
	"testing"

	"memory-os/internal/archive"
	"memory-os/internal/hotmemory"
)

func TestServiceDryRunMem0SanitizesAndDoesNotPersist(t *testing.T) {
	content := readFixture(t, "fixtures/mem0_sample.jsonl")
	service := NewService(NewMemoryRepository())

	result, err := service.DryRun(ImportRequest{BatchID: "batch_mem0", SourceType: SourceMem0, Content: content, Scope: DefaultScope()})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if result.BatchID != "batch_mem0" || result.ItemCount != 2 || result.DryRun != true {
		t.Fatalf("dry run result = %#v", result)
	}
	if strings.Contains(result.Preview[0].Text, "sk-test-redacted-example") {
		t.Fatalf("dry run leaked fake secret: %#v", result.Preview[0])
	}
	if service.Repository().Count() != 0 {
		t.Fatalf("repository count = %d, want 0 for dry run", service.Repository().Count())
	}
}

func TestServiceApplyMem0IsIdempotentAndProducesHotMemory(t *testing.T) {
	content := readFixture(t, "fixtures/mem0_sample.jsonl")
	service := NewService(NewMemoryRepository())
	request := ImportRequest{BatchID: "batch_mem0", SourceType: SourceMem0, Content: content, Scope: DefaultScope()}

	first, err := service.Apply(request)
	if err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}
	second, err := service.Apply(request)
	if err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}
	if first.CreatedCount != 2 || second.CreatedCount != 0 || second.DedupedCount != 2 {
		t.Fatalf("apply results = %#v then %#v", first, second)
	}
	items := service.Repository().Items()
	if items[0].Kind != KindHotMemory || strings.Contains(items[0].Text, "sk-test-redacted-example") {
		t.Fatalf("imported item invalid: %#v", items[0])
	}
}

func TestServiceApplyMem0WritesHotMemoryThroughProductionSink(t *testing.T) {
	content := readFixture(t, "fixtures/mem0_sample.jsonl")
	hotRepo := hotmemory.NewMemoryRepository()
	hotService := hotmemory.NewService(hotRepo)
	service := NewServiceWithProductionSink(NewMemoryRepository(), ProductionSink{HotMemory: hotService})
	request := ImportRequest{BatchID: "batch_mem0_prod", SourceType: SourceMem0, Content: content, Scope: DefaultScope()}

	if _, err := service.Apply(request); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if _, err := service.Apply(request); err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}

	memories := hotRepo.Search(map[string][]string{"user_id": {"user_1"}, "org_id": {"org_1"}, "project_id": {"project_1"}})
	if len(memories) != 2 {
		t.Fatalf("hot memory count = %d, want 2", len(memories))
	}
	for _, memory := range memories {
		if memory.Scope != hotmemory.ScopeProject || memory.Visibility != "project" {
			t.Fatalf("memory scope invalid: %#v", memory)
		}
		if len(memory.PermissionLabels) == 0 || len(memory.Sources) == 0 || memory.Sources[0].SourceRef == "" {
			t.Fatalf("memory governance metadata missing: %#v", memory)
		}
		if strings.Contains(memory.Fact, "sk-test-redacted-example") {
			t.Fatalf("hot memory leaked fake secret: %#v", memory)
		}
	}
}

func TestServiceApplyFastGPTWritesArchiveThroughProductionSink(t *testing.T) {
	content := readFixture(t, "fixtures/fastgpt_sample.json")
	archiveRepo := archive.NewMemoryRepository()
	archiveService := archive.NewService(archiveRepo, t.TempDir())
	service := NewServiceWithProductionSink(NewMemoryRepository(), ProductionSink{Archive: archiveService})
	request := ImportRequest{BatchID: "batch_fastgpt_prod", SourceType: SourceFastGPT, Content: content, Scope: DefaultScope()}

	if _, err := service.Apply(request); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if _, err := service.Apply(request); err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}

	archives, err := archiveRepo.List(archive.ListFilter{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", Status: "active"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(archives) != 1 {
		t.Fatalf("archive count = %d, want 1", len(archives))
	}
	detail, err := archiveService.Detail(archives[0].ArchiveID)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if !strings.Contains(detail.Content, "FastGPT Deploy Doc") {
		t.Fatalf("archive detail missing imported content: %s", detail.Content)
	}
	if strings.Contains(detail.Content, "sk-test-redacted-example") {
		t.Fatalf("archive leaked fake secret")
	}
}

func TestServiceApplyFastGPTEnqueuesArchiveRAGIndex(t *testing.T) {
	content := readFixture(t, "fixtures/fastgpt_sample.json")
	archiveRepo := archive.NewMemoryRepository()
	archiveService := archive.NewService(archiveRepo, t.TempDir())
	queue := &recordingArchiveIndexQueue{}
	service := NewServiceWithProductionSink(NewMemoryRepository(), ProductionSink{Archive: archiveService, ArchiveIndexQueue: queue})
	request := ImportRequest{BatchID: "batch_fastgpt_index", SourceType: SourceFastGPT, Content: content, Scope: DefaultScope()}

	if _, err := service.Apply(request); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if _, err := service.Apply(request); err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}

	if len(queue.jobs) != 2 {
		t.Fatalf("index job count = %d, want 2 enqueues for retry-safe idempotent queue", len(queue.jobs))
	}
	first := queue.jobs[0]
	second := queue.jobs[1]
	if first.IdempotencyKey == "" || first.IdempotencyKey != second.IdempotencyKey {
		t.Fatalf("idempotency keys = %q then %q, want stable non-empty key", first.IdempotencyKey, second.IdempotencyKey)
	}
	if first.ArchiveID == "" || first.IndexGeneration != 1 || len(first.Chunks) == 0 {
		t.Fatalf("index job missing archive metadata or chunks: %#v", first)
	}
	if first.UserID != "user_1" || first.OrgID != "org_1" || first.ProjectID != "project_1" || first.Visibility != "project" || len(first.PermissionLabels) == 0 {
		t.Fatalf("index job missing permission scope: %#v", first)
	}
	for _, chunk := range first.Chunks {
		if strings.Contains(chunk.Content, "sk-test-redacted-example") {
			t.Fatalf("index job leaked fake secret: %#v", first)
		}
	}
}

func TestFastGPTImporterProducesMarkdownArchive(t *testing.T) {
	content := readFixture(t, "fixtures/fastgpt_sample.json")
	service := NewService(NewMemoryRepository())

	result, err := service.DryRun(ImportRequest{BatchID: "batch_fastgpt", SourceType: SourceFastGPT, Content: content, Scope: DefaultScope()})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if result.ItemCount != 1 || result.Preview[0].Kind != KindArchive {
		t.Fatalf("fastgpt result = %#v", result)
	}
	if !strings.Contains(result.Preview[0].Text, "# FastGPT Deploy Doc") {
		t.Fatalf("fastgpt archive markdown missing title: %s", result.Preview[0].Text)
	}
	if strings.Contains(result.Preview[0].Text, "sk-test-redacted-example") {
		t.Fatalf("fastgpt archive leaked fake secret")
	}
}

func TestSkeletonImportersValidateSchema(t *testing.T) {
	service := NewService(NewMemoryRepository())
	for _, source := range []SourceType{SourceOpenMemory, SourceZep, SourceKhoj} {
		_, err := service.DryRun(ImportRequest{BatchID: "batch_" + string(source), SourceType: source, Content: []byte(`{"items":[]}`), Scope: DefaultScope()})
		if err != nil {
			t.Fatalf("DryRun(%s) error = %v", source, err)
		}
		_, err = service.DryRun(ImportRequest{BatchID: "bad_" + string(source), SourceType: source, Content: []byte(`{"unexpected":true}`), Scope: DefaultScope()})
		if err == nil {
			t.Fatalf("DryRun(%s) error = nil, want schema error", source)
		}
	}
}

func TestBundleExportContainsMetadataAndNoSecrets(t *testing.T) {
	content := readFixture(t, "fixtures/mem0_sample.jsonl")
	service := NewService(NewMemoryRepository())
	if _, err := service.Apply(ImportRequest{BatchID: "batch_mem0", SourceType: SourceMem0, Content: content, Scope: DefaultScope()}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	bundle, err := service.ExportBundle("batch_mem0")
	if err != nil {
		t.Fatalf("ExportBundle() error = %v", err)
	}
	if !strings.Contains(bundle.Markdown, "# Memory OS Export Bundle") || !strings.Contains(bundle.MetadataJSON, "batch_mem0") || !strings.Contains(bundle.SourceRefsJSON, "mem0_1") {
		t.Fatalf("bundle incomplete: %#v", bundle)
	}
	if strings.Contains(bundle.Markdown+bundle.MetadataJSON+bundle.SourceRefsJSON, "sk-test-redacted-example") {
		t.Fatalf("bundle leaked fake secret")
	}
}

func TestBundleExportCanBeReimportedIdempotently(t *testing.T) {
	content := readFixture(t, "fixtures/mem0_sample.jsonl")
	exporter := NewService(NewMemoryRepository())
	if _, err := exporter.Apply(ImportRequest{BatchID: "batch_mem0", SourceType: SourceMem0, Content: content, Scope: DefaultScope()}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	bundle, err := exporter.ExportBundle("batch_mem0")
	if err != nil {
		t.Fatalf("ExportBundle() error = %v", err)
	}

	importerService := NewService(NewMemoryRepository())
	request := ImportRequest{BatchID: "batch_bundle", SourceType: SourceBundle, Content: bundleContent(bundle), Scope: DefaultScope()}
	first, err := importerService.Apply(request)
	if err != nil {
		t.Fatalf("first bundle Apply() error = %v", err)
	}
	second, err := importerService.Apply(request)
	if err != nil {
		t.Fatalf("second bundle Apply() error = %v", err)
	}

	if first.CreatedCount != 2 || first.DedupedCount != 0 {
		t.Fatalf("first bundle result = %#v, want 2 created", first)
	}
	if second.CreatedCount != 0 || second.DedupedCount != 2 {
		t.Fatalf("second bundle result = %#v, want 2 deduped", second)
	}
	for _, item := range importerService.Repository().Items() {
		if item.SourceType != SourceBundle || item.Kind != KindHotMemory {
			t.Fatalf("bundle item mismatch: %#v", item)
		}
		if strings.Contains(item.Text, "sk-test-redacted-example") {
			t.Fatalf("bundle reimport leaked fake secret: %#v", item)
		}
		if item.SourceRef["source_type"] != string(SourceMem0) || item.SourceRef["external_id"] == "" {
			t.Fatalf("bundle source ref not preserved: %#v", item)
		}
	}
}

func TestBundleReimportPreservesArchiveKind(t *testing.T) {
	content := readFixture(t, "fixtures/fastgpt_sample.json")
	exporter := NewService(NewMemoryRepository())
	if _, err := exporter.Apply(ImportRequest{BatchID: "batch_fastgpt", SourceType: SourceFastGPT, Content: content, Scope: DefaultScope()}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	bundle, err := exporter.ExportBundle("batch_fastgpt")
	if err != nil {
		t.Fatalf("ExportBundle() error = %v", err)
	}

	result, err := NewService(NewMemoryRepository()).DryRun(ImportRequest{BatchID: "batch_bundle_archive", SourceType: SourceBundle, Content: bundleContent(bundle), Scope: DefaultScope()})
	if err != nil {
		t.Fatalf("bundle DryRun() error = %v", err)
	}
	if result.ItemCount != 1 || result.Preview[0].Kind != KindArchive {
		t.Fatalf("bundle archive result = %#v", result)
	}
	if !strings.Contains(result.Preview[0].Text, "FastGPT Deploy Doc") {
		t.Fatalf("bundle archive text missing source content: %s", result.Preview[0].Text)
	}
	if result.Preview[0].SourceRef["source_type"] != string(SourceFastGPT) {
		t.Fatalf("bundle archive source ref not preserved: %#v", result.Preview[0])
	}
}

func TestBundleApplyWritesOriginalSourceRefToHotMemoryProductionSink(t *testing.T) {
	content := readFixture(t, "fixtures/mem0_sample.jsonl")
	exporter := NewService(NewMemoryRepository())
	if _, err := exporter.Apply(ImportRequest{BatchID: "batch_mem0", SourceType: SourceMem0, Content: content, Scope: DefaultScope()}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	bundle, err := exporter.ExportBundle("batch_mem0")
	if err != nil {
		t.Fatalf("ExportBundle() error = %v", err)
	}

	hotRepo := hotmemory.NewMemoryRepository()
	hotService := hotmemory.NewService(hotRepo)
	service := NewServiceWithProductionSink(NewMemoryRepository(), ProductionSink{HotMemory: hotService})
	if _, err := service.Apply(ImportRequest{BatchID: "batch_bundle_prod", SourceType: SourceBundle, Content: bundleContent(bundle), Scope: DefaultScope()}); err != nil {
		t.Fatalf("bundle Apply() error = %v", err)
	}

	memories := hotRepo.Search(map[string][]string{"user_id": {"user_1"}, "org_id": {"org_1"}, "project_id": {"project_1"}})
	if len(memories) != 2 {
		t.Fatalf("hot memory count = %d, want 2", len(memories))
	}
	for _, memory := range memories {
		if len(memory.Sources) == 0 {
			t.Fatalf("memory sources missing: %#v", memory)
		}
		if !strings.HasPrefix(memory.Sources[0].SourceRef, "importer:mem0:") {
			t.Fatalf("source ref = %q, want original mem0 source ref", memory.Sources[0].SourceRef)
		}
	}
}

func TestBundleApplyWritesOriginalSourceRefToArchiveProductionSink(t *testing.T) {
	content := readFixture(t, "fixtures/fastgpt_sample.json")
	exporter := NewService(NewMemoryRepository())
	if _, err := exporter.Apply(ImportRequest{BatchID: "batch_fastgpt", SourceType: SourceFastGPT, Content: content, Scope: DefaultScope()}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	bundle, err := exporter.ExportBundle("batch_fastgpt")
	if err != nil {
		t.Fatalf("ExportBundle() error = %v", err)
	}

	archiveRepo := archive.NewMemoryRepository()
	archiveService := archive.NewService(archiveRepo, t.TempDir())
	service := NewServiceWithProductionSink(NewMemoryRepository(), ProductionSink{Archive: archiveService})
	if _, err := service.Apply(ImportRequest{BatchID: "batch_bundle_archive_prod", SourceType: SourceBundle, Content: bundleContent(bundle), Scope: DefaultScope()}); err != nil {
		t.Fatalf("bundle Apply() error = %v", err)
	}

	archives, err := archiveRepo.List(archive.ListFilter{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", Status: "active"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(archives) != 1 {
		t.Fatalf("archive count = %d, want 1", len(archives))
	}
	detail, err := archiveService.Detail(archives[0].ArchiveID)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if !strings.Contains(detail.Content, "**original source type:** fastgpt") || !strings.Contains(detail.Content, "**original external id:** fg_doc_1") {
		t.Fatalf("archive markdown missing original source ref: %s", detail.Content)
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return content
}

func bundleContent(bundle Bundle) []byte {
	return []byte(bundle.Markdown + "metadata:\n" + bundle.MetadataJSON + "\nsource_refs:\n" + bundle.SourceRefsJSON + "\n")
}

type recordingArchiveIndexQueue struct {
	jobs []ArchiveIndexJob
}

func (q *recordingArchiveIndexQueue) EnqueueArchiveIndex(ctx context.Context, job ArchiveIndexJob) error {
	q.jobs = append(q.jobs, job)
	return nil
}
