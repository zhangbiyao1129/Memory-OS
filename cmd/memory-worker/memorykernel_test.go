package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
	"memory-os/internal/config"
	"memory-os/internal/hotmemory"
	"memory-os/internal/jobs"
	"memory-os/internal/llm"
	"memory-os/internal/rag"
)

func TestBuildWorkerConfiguresMemoryKernelMaintenance(t *testing.T) {
	originalNewPool := newPostgresPool
	originalRunMigrations := runWorkerMigrations
	originalClosePool := closePostgresPool
	originalNewArchiveService := newArchiveService
	originalNewArchiveQueue := newArchiveQueue
	originalNewRAGIndexQueue := newRAGIndexQueue
	originalNewRAGIndexWorker := newRAGIndexWorker
	originalNewRAGIndexStore := newRAGIndexStore
	originalNewHotMemoryService := newHotMemoryService
	originalNewOpenAICompatibleClient := newOpenAICompatibleClient
	t.Cleanup(func() {
		newPostgresPool = originalNewPool
		runWorkerMigrations = originalRunMigrations
		closePostgresPool = originalClosePool
		newArchiveService = originalNewArchiveService
		newArchiveQueue = originalNewArchiveQueue
		newRAGIndexQueue = originalNewRAGIndexQueue
		newRAGIndexWorker = originalNewRAGIndexWorker
		newRAGIndexStore = originalNewRAGIndexStore
		newHotMemoryService = originalNewHotMemoryService
		newOpenAICompatibleClient = originalNewOpenAICompatibleClient
	})

	newPostgresPool = func(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	runWorkerMigrations = func(ctx context.Context, pool *pgxpool.Pool) error { return nil }
	closePostgresPool = func(pool *pgxpool.Pool) {}
	newArchiveService = func(repo archive.Repository, root string) archive.Service {
		return archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	}
	newArchiveQueue = func(pool *pgxpool.Pool) jobs.ArchiveQueue { return fakeWorkerArchiveQueue{} }
	newRAGIndexQueue = func(pool *pgxpool.Pool) jobs.RAGIndexQueue { return fakeWorkerRAGIndexQueue{} }
	newRAGIndexStore = func(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (rag.Store, error) {
		return rag.NewMemoryStore(), nil
	}
	newRAGIndexWorker = func(store rag.Store) *jobs.RAGIndexWorker {
		return jobs.NewRAGIndexWorker(rag.NewService(store))
	}
	newHotMemoryService = func(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (hotmemory.Service, error) {
		return hotmemory.NewService(hotmemory.NewMemoryRepository()), nil
	}
	newOpenAICompatibleClient = func(cfg llm.OpenAICompatibleConfig) (*llm.OpenAICompatibleClient, error) {
		return llm.NewOpenAICompatible(cfg)
	}

	worker, err := buildWorker(config.Config{
		PostgresDSN:    "postgres://memory_os:secret@postgres:5432/memory_os",
		ArchiveDir:     "/data/memory-os",
		QdrantURL:      "http://qdrant:6333",
		LLMBaseURL:     "http://llm.local:8000",
		LLMAPIKey:      "test-key",
		LLMModel:       "MiniMax-M2.7",
		EmbeddingModel: "bge-m3",
	})
	if err != nil {
		t.Fatalf("buildWorker() error = %v", err)
	}
	if worker == nil {
		t.Fatal("buildWorker() returned nil worker")
	}
	if !worker.MemoryKernelMaintenanceConfigured() {
		t.Fatal("buildWorker() did not configure memory kernel maintenance")
	}
}
