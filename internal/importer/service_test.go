package importer

import (
	"os"
	"strings"
	"testing"
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

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return content
}
