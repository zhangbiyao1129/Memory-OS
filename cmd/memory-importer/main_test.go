package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
	"memory-os/internal/config"
	"memory-os/internal/hotmemory"
	"memory-os/internal/importer"
	"memory-os/internal/jobs"
)

func TestRunDryRunMem0(t *testing.T) {
	out, err := run([]string{"--source", "mem0", "--batch", "cli_batch", "--dry-run", "--input", "../../internal/importer/fixtures/mem0_sample.jsonl"})
	if err != nil {
		t.Fatalf("run dry-run error = %v", err)
	}
	if !strings.Contains(out, `"dry_run":true`) || !strings.Contains(out, `"item_count":2`) {
		t.Fatalf("dry-run output unexpected: %s", out)
	}
	if strings.Contains(out, "sk-test-redacted-example") {
		t.Fatalf("dry-run leaked fake secret: %s", out)
	}
}

func TestRunExportBundle(t *testing.T) {
	out, err := run([]string{"--source", "mem0", "--batch", "cli_batch", "--apply", "--export-bundle", "--input", "../../internal/importer/fixtures/mem0_sample.jsonl"})
	if err != nil {
		t.Fatalf("run export error = %v", err)
	}
	if !strings.Contains(out, "Memory OS Export Bundle") || !strings.Contains(out, "source_refs") {
		t.Fatalf("export output unexpected: %s", out)
	}
	if strings.Contains(out, "sk-test-redacted-example") {
		t.Fatalf("export leaked fake secret: %s", out)
	}
}

func TestRunApplyUsesStateFileForIdempotency(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "importer-state.json")
	args := []string{"--source", "mem0", "--batch", "cli_state", "--apply", "--state", statePath, "--input", "../../internal/importer/fixtures/mem0_sample.jsonl"}

	firstOut, err := run(args)
	if err != nil {
		t.Fatalf("first run apply error = %v", err)
	}
	secondOut, err := run(args)
	if err != nil {
		t.Fatalf("second run apply error = %v", err)
	}

	var first importer.ImportResult
	if err := json.Unmarshal([]byte(firstOut), &first); err != nil {
		t.Fatalf("decode first output: %v\n%s", err, firstOut)
	}
	var second importer.ImportResult
	if err := json.Unmarshal([]byte(secondOut), &second); err != nil {
		t.Fatalf("decode second output: %v\n%s", err, secondOut)
	}
	if first.CreatedCount != 2 || first.DedupedCount != 0 {
		t.Fatalf("first result = %#v, want 2 created", first)
	}
	if second.CreatedCount != 0 || second.DedupedCount != 2 {
		t.Fatalf("second result = %#v, want 2 deduped from persisted state", second)
	}
}

func TestRunExportBundleReadsStateFileWithoutReimport(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "importer-state.json")
	if _, err := run([]string{"--source", "mem0", "--batch", "cli_state_bundle", "--apply", "--state", statePath, "--input", "../../internal/importer/fixtures/mem0_sample.jsonl"}); err != nil {
		t.Fatalf("run apply error = %v", err)
	}

	out, err := run([]string{"--batch", "cli_state_bundle", "--export-bundle", "--state", statePath})
	if err != nil {
		t.Fatalf("run export-bundle from state error = %v", err)
	}
	if !strings.Contains(out, "Memory OS Export Bundle") || !strings.Contains(out, "source_refs") {
		t.Fatalf("export output unexpected: %s", out)
	}
	if strings.Contains(out, "sk-test-redacted-example") {
		t.Fatalf("export leaked fake secret: %s", out)
	}
}

func TestRunReimportsExportBundleWithStateIdempotency(t *testing.T) {
	tempDir := t.TempDir()
	sourceState := filepath.Join(tempDir, "source-state.json")
	bundleState := filepath.Join(tempDir, "bundle-state.json")
	bundlePath := filepath.Join(tempDir, "bundle.md")

	if _, err := run([]string{"--source", "mem0", "--batch", "cli_source_bundle", "--apply", "--state", sourceState, "--input", "../../internal/importer/fixtures/mem0_sample.jsonl"}); err != nil {
		t.Fatalf("run source apply error = %v", err)
	}
	bundle, err := run([]string{"--batch", "cli_source_bundle", "--export-bundle", "--state", sourceState})
	if err != nil {
		t.Fatalf("run source export error = %v", err)
	}
	if err := os.WriteFile(bundlePath, []byte(bundle), 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	args := []string{"--source", "bundle", "--batch", "cli_reimport_bundle", "--apply", "--state", bundleState, "--input", bundlePath}
	firstOut, err := run(args)
	if err != nil {
		t.Fatalf("first bundle reimport error = %v", err)
	}
	secondOut, err := run(args)
	if err != nil {
		t.Fatalf("second bundle reimport error = %v", err)
	}

	var first importer.ImportResult
	if err := json.Unmarshal([]byte(firstOut), &first); err != nil {
		t.Fatalf("decode first output: %v\n%s", err, firstOut)
	}
	var second importer.ImportResult
	if err := json.Unmarshal([]byte(secondOut), &second); err != nil {
		t.Fatalf("decode second output: %v\n%s", err, secondOut)
	}
	if first.SourceType != importer.SourceBundle || first.CreatedCount != 2 || first.DedupedCount != 0 {
		t.Fatalf("first bundle result = %#v, want bundle source and 2 created", first)
	}
	if second.CreatedCount != 0 || second.DedupedCount != 2 {
		t.Fatalf("second bundle result = %#v, want 2 deduped", second)
	}
	if strings.Contains(firstOut+secondOut, "sk-test-redacted-example") {
		t.Fatalf("bundle reimport leaked fake secret")
	}
}

func TestRunProductionSinkRequiresPostgresDSNForApply(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "")

	_, err := run([]string{"--source", "mem0", "--batch", "cli_prod", "--apply", "--production-sink", "--input", "../../internal/importer/fixtures/mem0_sample.jsonl"})
	if err == nil {
		t.Fatal("run apply with production sink error = nil, want missing postgres dsn")
	}
	if !strings.Contains(err.Error(), "POSTGRES_DSN is required for production sink") {
		t.Fatalf("error = %v, want POSTGRES_DSN requirement", err)
	}
}

func TestRunProductionSinkDryRunDoesNotRequirePostgresDSN(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "")

	out, err := run([]string{"--source", "mem0", "--batch", "cli_prod_dry", "--dry-run", "--production-sink", "--input", "../../internal/importer/fixtures/mem0_sample.jsonl"})
	if err != nil {
		t.Fatalf("run dry-run with production sink error = %v", err)
	}
	if !strings.Contains(out, `"dry_run":true`) {
		t.Fatalf("dry-run output unexpected: %s", out)
	}
}

func TestArchiveIndexQueueAdapterConvertsImporterJob(t *testing.T) {
	queue := &recordingRAGIndexQueue{}
	adapter := archiveIndexQueueAdapter{queue: queue}

	err := adapter.EnqueueArchiveIndex(context.Background(), importer.ArchiveIndexJob{
		IdempotencyKey:   "rag_archive_fastgpt_doc_1_g1",
		ArchiveID:        "archive_fastgpt_doc_1",
		IndexGeneration:  1,
		OrgID:            "org_1",
		ProjectID:        "project_1",
		UserID:           "user_1",
		Visibility:       "project",
		PermissionLabels: []string{"project:project_1:read"},
		Chunks: []archive.Chunk{{
			ChunkID:         "archive_fastgpt_doc_1_g1_c0",
			ArchiveID:       "archive_fastgpt_doc_1",
			IndexGeneration: 1,
			ChunkIndex:      0,
			Content:         "imported archive content",
		}},
	})
	if err != nil {
		t.Fatalf("EnqueueArchiveIndex() error = %v", err)
	}
	if len(queue.jobs) != 1 {
		t.Fatalf("queued jobs = %d, want 1", len(queue.jobs))
	}
	job := queue.jobs[0]
	if job.IdempotencyKey != "rag_archive_fastgpt_doc_1_g1" || job.OrgID != "org_1" || job.ProjectID != "project_1" || len(job.Chunks) != 1 {
		t.Fatalf("queued job mismatch: %#v", job)
	}
}

func TestNewProductionSinkConfiguresHotMemoryVectorIndex(t *testing.T) {
	state := &hotMemoryFactoryState{}
	restoreHotMemory := stubImporterProductionHotMemory(t, state, nil)
	defer restoreHotMemory()
	restoreQueue := stubImporterArchiveIndexQueue(t)
	defer restoreQueue()

	sink, err := newProductionSink(productionImporterConfig(), &pgxpool.Pool{})
	if err != nil {
		t.Fatalf("newProductionSink() error = %v", err)
	}
	if sink.HotMemory == nil {
		t.Fatal("HotMemory sink not configured")
	}
	if sink.Archive == nil {
		t.Fatal("Archive sink not configured")
	}
	if sink.ArchiveIndexQueue == nil {
		t.Fatal("ArchiveIndexQueue not configured")
	}
	if !state.called {
		t.Fatal("production hot memory vector index factory was not called")
	}
}

func TestNewProductionSinkReturnsHotMemoryVectorIndexError(t *testing.T) {
	expected := errors.New("hot memory vector index unavailable")
	state := &hotMemoryFactoryState{}
	restoreHotMemory := stubImporterProductionHotMemory(t, state, expected)
	defer restoreHotMemory()
	restoreQueue := stubImporterArchiveIndexQueue(t)
	defer restoreQueue()

	_, err := newProductionSink(productionImporterConfig(), &pgxpool.Pool{})
	if !errors.Is(err, expected) {
		t.Fatalf("newProductionSink() error = %v, want %v", err, expected)
	}
}

type recordingRAGIndexQueue struct {
	jobs []jobs.RAGIndexJob
}

func (q *recordingRAGIndexQueue) Enqueue(ctx context.Context, job jobs.RAGIndexJob) error {
	q.jobs = append(q.jobs, job)
	return nil
}

type hotMemoryFactoryState struct {
	called bool
}

func stubImporterProductionHotMemory(t *testing.T, state *hotMemoryFactoryState, returnedErr error) func() {
	t.Helper()
	old := newImporterProductionHotMemory
	newImporterProductionHotMemory = func(cfg config.Config, pool *pgxpool.Pool) (importer.HotMemorySink, error) {
		state.called = cfg.AppEnv == "production" && pool != nil
		if returnedErr != nil {
			return nil, returnedErr
		}
		return hotmemory.NewService(hotmemory.NewMemoryRepository()), nil
	}
	return func() { newImporterProductionHotMemory = old }
}

func stubImporterArchiveIndexQueue(t *testing.T) func() {
	t.Helper()
	old := newImporterArchiveIndexQueue
	newImporterArchiveIndexQueue = func(pool *pgxpool.Pool) ragIndexQueue {
		return &recordingRAGIndexQueue{}
	}
	return func() { newImporterArchiveIndexQueue = old }
}

func productionImporterConfig() config.Config {
	return config.Config{
		AppEnv:         "production",
		PostgresDSN:    "postgres://memory_os:secret@postgres:5432/memory_os",
		QdrantURL:      "http://qdrant:6333",
		ArchiveDir:     "/data/memory-os",
		LLMBaseURL:     "http://llm.local:8000",
		LLMAPIKey:      "test-key",
		EmbeddingModel: "bge-m3",
	}
}
