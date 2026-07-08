package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
	"memory-os/internal/candidatememory"
	"memory-os/internal/config"
	"memory-os/internal/hotmemory"
	"memory-os/internal/jobs"
	"memory-os/internal/llm"
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

func TestBuildWorkerPassesLLMModelToCandidateWorker(t *testing.T) {
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

	var capturedLLMConfig llm.OpenAICompatibleConfig
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
		capturedLLMConfig = cfg
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
	if capturedLLMConfig.LLMModel != "MiniMax-M2.7" {
		t.Fatalf("LLMModel = %q, want MiniMax-M2.7 — buildWorker did not pass LLMModel to LLM client", capturedLLMConfig.LLMModel)
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
	originalNewHotMemoryService := newHotMemoryService
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
	newHotMemoryService = func(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (hotmemory.Service, error) {
		return hotmemory.NewService(hotmemory.NewMemoryRepository()), nil
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

func TestBuildWorkerInjectsHotMemoryIntoCandidateRouter(t *testing.T) {
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
	originalNewCandidateMemoryWorker := newCandidateMemoryWorker
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
		newCandidateMemoryWorker = originalNewCandidateMemoryWorker
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
	candidateObserved := false
	newCandidateMemoryWorker = func(extractor candidatememory.Extractor, router candidatememory.Router, service *candidatememory.Service, repo candidatememory.Repository, eventLoader jobs.CandidateEventLoader) jobs.CandidateMemoryWorker {
		candidate := candidatememory.Candidate{
			CandidateID: "cand-worker-hot",
			OrgID:       "org_1",
			ProjectID:   "project_1",
			UserID:      "user_1",
			AgentID:     "codex",
			SourceKey:   "local/workspace",
			Content:     "用户默认使用 Go 开发后端服务",
			MemoryType:  candidatememory.MemoryTypePreference,
			RiskLevel:   candidatememory.RiskLow,
			Confidence:  0.95,
		}
		routed, decision, err := router.ApplyRouting(candidate)
		if err != nil {
			t.Fatalf("ApplyRouting() error = %v", err)
		}
		candidateObserved = decision.Target == candidatememory.RoutingTargetPending && routed.Status == candidatememory.StatusPending
		return jobs.NewCandidateMemoryWorker(extractor, router, service, repo, eventLoader)
	}

	worker, err := buildWorker(config.Config{PostgresDSN: "postgres://memory_os:secret@postgres:5432/memory_os", ArchiveDir: "/data/memory-os", QdrantURL: "http://qdrant:6333", LLMBaseURL: "http://llm.local:8000", LLMAPIKey: "test-key", EmbeddingModel: "bge-m3"})
	if err != nil {
		t.Fatalf("buildWorker() error = %v", err)
	}
	if worker == nil || !worker.CandidateWorkerConfigured() {
		t.Fatal("buildWorker() did not configure candidate worker")
	}
	if !candidateObserved {
		t.Fatal("buildWorker() still auto-promotes candidates into hot memory")
	}
}

func TestNewAutoMaintenanceServiceUsesUnifiedOrganizer(t *testing.T) {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	service, err := newAutoMaintenanceService(productionWorkerConfig(), &pgxpool.Pool{}, candidatememory.NewInMemoryRepository(), nil, hot)
	if err != nil {
		t.Fatalf("newAutoMaintenanceService() error = %v", err)
	}
	maintenance, ok := service.(*candidatememory.MaintenanceService)
	if !ok || maintenance == nil {
		t.Fatalf("newAutoMaintenanceService() = %T, want *MaintenanceService", service)
	}
	if !maintenance.OrganizerConfigured() {
		t.Fatal("auto maintenance service did not configure unified organizer")
	}
	if !maintenance.HotMemoryConfigured() {
		t.Fatal("auto maintenance service did not configure hot memory sink")
	}
}

func TestBuildWorkerConfiguresAutoMaintenance(t *testing.T) {
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
	originalNewAutoMaintenanceService := newAutoMaintenanceService
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
		newAutoMaintenanceService = originalNewAutoMaintenanceService
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
	autoMaintenanceCalled := false
	newAutoMaintenanceService = func(cfg config.Config, pool *pgxpool.Pool, candidateRepo candidatememory.Repository, composer *candidatememory.TopicComposer, hotMemory candidatememory.HotMemorySink) (jobs.AutoMaintenance, error) {
		autoMaintenanceCalled = pool != nil && candidateRepo != nil && composer != nil && hotMemory != nil
		return fakeWorkerAutoMaintenance{}, nil
	}

	worker, err := buildWorker(config.Config{PostgresDSN: "postgres://memory_os:secret@postgres:5432/memory_os", ArchiveDir: "/data/memory-os", QdrantURL: "http://qdrant:6333", LLMBaseURL: "http://llm.local:8000", LLMAPIKey: "test-key", EmbeddingModel: "bge-m3"})
	if err != nil {
		t.Fatalf("buildWorker() error = %v", err)
	}
	if !autoMaintenanceCalled {
		t.Fatal("buildWorker() did not create auto maintenance service")
	}
	if worker == nil || !worker.AutoMaintenanceConfigured() {
		t.Fatal("buildWorker() did not configure auto maintenance")
	}
}

func TestBuildWorkerConfiguresHotMemoryAutoMaintenance(t *testing.T) {
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

	worker, err := buildWorker(config.Config{PostgresDSN: "postgres://memory_os:secret@postgres:5432/memory_os", ArchiveDir: "/data/memory-os", QdrantURL: "http://qdrant:6333", LLMBaseURL: "http://llm.local:8000", LLMAPIKey: "test-key", EmbeddingModel: "bge-m3"})
	if err != nil {
		t.Fatalf("buildWorker() error = %v", err)
	}
	if worker == nil || !worker.HotMemoryMaintenanceConfigured() {
		t.Fatal("buildWorker() did not configure hot memory auto maintenance")
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

type fakeWorkerAutoMaintenance struct{}

func (fakeWorkerAutoMaintenance) RunAutoClean(ctx context.Context) (int, error) {
	return 0, nil
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
