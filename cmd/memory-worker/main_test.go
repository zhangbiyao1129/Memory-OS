package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
	"memory-os/internal/config"
	"memory-os/internal/jobs"
	"memory-os/internal/rag"
)

func TestBuildWorker(t *testing.T) {
	worker, err := buildWorker(config.Config{})
	if err != nil {
		t.Fatalf("buildWorker() error = %v", err)
	}
	if worker == nil {
		t.Fatal("buildWorker() returned nil worker")
	}
}

func TestBuildWorkerRejectsMissingPostgresDSNInProduction(t *testing.T) {
	_, err := buildWorker(config.Config{AppEnv: "production"})
	if !errors.Is(err, errMissingProductionPostgresDSN) {
		t.Fatalf("buildWorker() error = %v, want %v", err, errMissingProductionPostgresDSN)
	}
}

func TestBuildWorkerRejectsMissingQdrantURLInProduction(t *testing.T) {
	_, err := buildWorker(config.Config{AppEnv: "production", PostgresDSN: "postgres://memory_os:secret@postgres:5432/memory_os", ArchiveDir: "/data/memory-os"})
	if !errors.Is(err, errMissingProductionQdrantURL) {
		t.Fatalf("buildWorker() error = %v, want %v", err, errMissingProductionQdrantURL)
	}
}

func TestBuildWorkerRejectsMissingArchiveDirInProduction(t *testing.T) {
	_, err := buildWorker(config.Config{AppEnv: "production", PostgresDSN: "postgres://memory_os:secret@postgres:5432/memory_os", QdrantURL: "http://qdrant:6333"})
	if !errors.Is(err, errMissingProductionArchiveDir) {
		t.Fatalf("buildWorker() error = %v, want %v", err, errMissingProductionArchiveDir)
	}
}

func TestBuildWorkerRejectsPlaceholderLLMConfigInProduction(t *testing.T) {
	cfg := productionWorkerConfig()
	cfg.LLMBaseURL = "http://example.local:8000"
	cfg.LLMAPIKey = "replace-me"

	_, err := buildWorker(cfg)
	if !errors.Is(err, errInvalidProductionLLMConfig) {
		t.Fatalf("buildWorker() error = %v, want %v", err, errInvalidProductionLLMConfig)
	}
}

func TestWorkerLoggerOptionsUsesConfiguredEnvironment(t *testing.T) {
	options := workerLoggerOptions(config.Config{AppEnv: "production"})

	if options.Environment != "production" {
		t.Fatalf("Environment = %q, want production", options.Environment)
	}
	if options.Service != "memory-worker" {
		t.Fatalf("Service = %q, want memory-worker", options.Service)
	}
}

func TestBuildWorkerRunsMigrationsWhenPostgresDSNExists(t *testing.T) {
	originalNewPool := newPostgresPool
	originalRunMigrations := runWorkerMigrations
	originalClosePool := closePostgresPool
	originalNewArchiveService := newArchiveService
	originalNewArchiveQueue := newArchiveQueue
	originalNewRAGIndexQueue := newRAGIndexQueue
	originalNewRAGIndexWorker := newRAGIndexWorker
	originalNewRAGIndexStore := newRAGIndexStore
	t.Cleanup(func() {
		newPostgresPool = originalNewPool
		runWorkerMigrations = originalRunMigrations
		closePostgresPool = originalClosePool
		newArchiveService = originalNewArchiveService
		newArchiveQueue = originalNewArchiveQueue
		newRAGIndexQueue = originalNewRAGIndexQueue
		newRAGIndexWorker = originalNewRAGIndexWorker
		newRAGIndexStore = originalNewRAGIndexStore
	})

	newPoolCalled := false
	migrationsCalled := false
	archiveRoot := ""
	archiveQueueCalled := false
	ragIndexQueueCalled := false
	ragIndexWorkerCalled := false
	ragIndexStoreCalled := false
	newPostgresPool = func(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
		newPoolCalled = dsn == "postgres://memory_os:secret@postgres:5432/memory_os"
		return &pgxpool.Pool{}, nil
	}
	runWorkerMigrations = func(ctx context.Context, pool *pgxpool.Pool) error {
		migrationsCalled = pool != nil
		return nil
	}
	closePostgresPool = func(pool *pgxpool.Pool) {}
	newArchiveService = func(repo archive.Repository, root string) archive.Service {
		archiveRoot = root
		return archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	}
	newArchiveQueue = func(pool *pgxpool.Pool) jobs.ArchiveQueue {
		archiveQueueCalled = pool != nil
		return fakeWorkerArchiveQueue{}
	}
	newRAGIndexQueue = func(pool *pgxpool.Pool) jobs.RAGIndexQueue {
		ragIndexQueueCalled = pool != nil
		return fakeWorkerRAGIndexQueue{}
	}
	newRAGIndexStore = func(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (rag.Store, error) {
		ragIndexStoreCalled = cfg.QdrantURL == "http://qdrant:6333" && cfg.LLMBaseURL == "http://llm.local:8000" && cfg.EmbeddingModel == "bge-m3" && pool != nil
		return rag.NewMemoryStore(), nil
	}
	newRAGIndexWorker = func(store rag.Store) *jobs.RAGIndexWorker {
		ragIndexWorkerCalled = true
		return jobs.NewRAGIndexWorker(rag.NewService(store))
	}

	worker, err := buildWorker(config.Config{PostgresDSN: "postgres://memory_os:secret@postgres:5432/memory_os", ArchiveDir: "/data/memory-os", QdrantURL: "http://qdrant:6333", LLMBaseURL: "http://llm.local:8000", LLMAPIKey: "test-key", EmbeddingModel: "bge-m3"})
	if err != nil {
		t.Fatalf("buildWorker() error = %v", err)
	}
	if worker == nil {
		t.Fatal("buildWorker() returned nil worker")
	}
	if !newPoolCalled {
		t.Fatal("buildWorker() did not create postgres pool")
	}
	if !migrationsCalled {
		t.Fatal("buildWorker() did not run migrations")
	}
	if archiveRoot != "/data/memory-os" {
		t.Fatalf("archive root = %q, want /data/memory-os", archiveRoot)
	}
	if !worker.ArchiveWorkerConfigured() {
		t.Fatal("buildWorker() did not configure archive worker")
	}
	if !archiveQueueCalled {
		t.Fatal("buildWorker() did not create archive queue")
	}
	if !worker.ArchiveQueueConfigured() {
		t.Fatal("buildWorker() did not configure archive queue")
	}
	if !ragIndexQueueCalled {
		t.Fatal("buildWorker() did not create rag index queue")
	}
	if !ragIndexStoreCalled {
		t.Fatal("buildWorker() did not create rag index store from production config")
	}
	if !ragIndexWorkerCalled {
		t.Fatal("buildWorker() did not create rag index worker")
	}
	if !worker.RAGIndexWorkerConfigured() {
		t.Fatal("buildWorker() did not configure rag index worker")
	}
	if !worker.RAGIndexQueueConfigured() {
		t.Fatal("buildWorker() did not configure rag index queue")
	}
}

type fakeWorkerArchiveQueue struct{}

func (fakeWorkerArchiveQueue) Lease(ctx context.Context) (jobs.ArchiveJob, bool, error) {
	return jobs.ArchiveJob{}, false, nil
}

func (fakeWorkerArchiveQueue) Complete(ctx context.Context, job jobs.ArchiveJob, result archive.Result) error {
	return nil
}

func (fakeWorkerArchiveQueue) Fail(ctx context.Context, job jobs.ArchiveJob, err error) error {
	return nil
}

type fakeWorkerRAGIndexQueue struct{}

func (fakeWorkerRAGIndexQueue) Enqueue(ctx context.Context, job jobs.RAGIndexJob) error {
	return nil
}

func (fakeWorkerRAGIndexQueue) Lease(ctx context.Context) (jobs.RAGIndexJob, bool, error) {
	return jobs.RAGIndexJob{}, false, nil
}

func (fakeWorkerRAGIndexQueue) Complete(ctx context.Context, job jobs.RAGIndexJob, result jobs.RAGIndexResult) error {
	return nil
}

func (fakeWorkerRAGIndexQueue) Fail(ctx context.Context, job jobs.RAGIndexJob, err error) error {
	return nil
}

func productionWorkerConfig() config.Config {
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
