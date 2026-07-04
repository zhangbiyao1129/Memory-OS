package main

import (
	"context"
	"errors"
	"os/signal"
	"strings"
	"syscall"

	"memory-os/internal/archive"
	"memory-os/internal/config"
	"memory-os/internal/db"
	"memory-os/internal/jobs"
	"memory-os/internal/llm"
	"memory-os/internal/logger"
	"memory-os/internal/qdrant"
	"memory-os/internal/rag"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	errMissingProductionPostgresDSN = errors.New("postgres dsn is required in production")
	errMissingProductionQdrantURL   = errors.New("qdrant url is required in production")
	errMissingProductionArchiveDir  = errors.New("archive dir is required in production")
	errInvalidProductionLLMConfig   = errors.New("llm embedding config is required in production")
)

var newPostgresPool = db.NewPool
var runWorkerMigrations = db.RunEmbeddedMigrations
var closePostgresPool = func(pool *pgxpool.Pool) {
	pool.Close()
}
var newArchiveService = archive.NewService
var newArchiveQueue = func(pool *pgxpool.Pool) jobs.ArchiveQueue {
	return jobs.NewPGArchiveQueue(pool, jobs.PGArchiveQueueOptions{WorkerID: "memory-worker"})
}
var newRAGIndexQueue = func(pool *pgxpool.Pool) jobs.RAGIndexQueue {
	return jobs.NewPGArchiveIndexQueue(pool, jobs.PGArchiveIndexQueueOptions{WorkerID: "memory-worker"})
}
var newRAGIndexStore = func(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (rag.Store, error) {
	qdrantClient, err := qdrant.NewClient(cfg.QdrantURL)
	if err != nil {
		return nil, err
	}
	if err := qdrantClient.EnsureCollectionSchema(ctx, qdrant.CollectionConfig{Name: qdrant.DefaultCollectionName, VectorSize: qdrant.DefaultVectorSize, Distance: qdrant.DefaultDistance}, qdrant.DefaultPayloadIndexConfigs()); err != nil {
		return nil, err
	}
	embedder, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, EmbeddingModel: cfg.EmbeddingModel})
	if err != nil {
		return nil, err
	}
	return rag.NewQdrantStore(pool, qdrantClient, embedder, qdrant.DefaultCollectionName), nil
}
var newRAGIndexWorker = func(store rag.Store) *jobs.RAGIndexWorker {
	return jobs.NewRAGIndexWorker(rag.NewService(store))
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	log, err := logger.New(workerLoggerOptions(cfg))
	if err != nil {
		panic(err)
	}
	defer log.Sync() //nolint:errcheck

	worker, err := buildWorker(cfg)
	if err != nil {
		panic(err)
	}

	log.Info("memory-worker starting")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := worker.Run(ctx); err != nil {
		panic(err)
	}
}

func workerLoggerOptions(cfg config.Config) logger.Options {
	return logger.Options{Environment: cfg.AppEnv, Service: "memory-worker"}
}

func buildWorker(cfg config.Config) (*jobs.Runner, error) {
	if cfg.AppEnv == "production" {
		if cfg.PostgresDSN == "" {
			return nil, errMissingProductionPostgresDSN
		}
		if cfg.QdrantURL == "" {
			return nil, errMissingProductionQdrantURL
		}
		if cfg.ArchiveDir == "" {
			return nil, errMissingProductionArchiveDir
		}
		if err := validateProductionLLMConfig(cfg); err != nil {
			return nil, err
		}
	}
	var archiveWorker *jobs.ArchiveWorker
	var archiveQueue jobs.ArchiveQueue
	var ragIndexWorker *jobs.RAGIndexWorker
	var ragIndexQueue jobs.RAGIndexQueue
	var cleanup func()
	if cfg.PostgresDSN != "" {
		pool, err := newPostgresPool(context.Background(), cfg.PostgresDSN)
		if err != nil {
			return nil, err
		}
		if err := runWorkerMigrations(context.Background(), pool); err != nil {
			closePostgresPool(pool)
			return nil, err
		}
		ragIndexQueue = newRAGIndexQueue(pool)
		ragIndexStore, err := newRAGIndexStore(context.Background(), cfg, pool)
		if err != nil {
			closePostgresPool(pool)
			return nil, err
		}
		service := newArchiveService(archive.NewPGRepository(pool), cfg.ArchiveDir)
		worker := jobs.NewArchiveWorkerWithIndexQueue(service, ragIndexQueue)
		archiveWorker = &worker
		archiveQueue = newArchiveQueue(pool)
		ragIndexWorker = newRAGIndexWorker(ragIndexStore)
		cleanup = func() { closePostgresPool(pool) }
	}
	runner := jobs.NewRunner(jobs.Options{Concurrency: 1, ArchiveWorker: archiveWorker, ArchiveQueue: archiveQueue, RAGIndexWorker: ragIndexWorker, RAGIndexQueue: ragIndexQueue, Cleanup: cleanup})
	return &runner, nil
}

func validateProductionLLMConfig(cfg config.Config) error {
	if strings.TrimSpace(cfg.LLMBaseURL) == "" || strings.TrimSpace(cfg.LLMAPIKey) == "" || strings.TrimSpace(cfg.EmbeddingModel) == "" {
		return errInvalidProductionLLMConfig
	}
	if strings.TrimSpace(cfg.LLMBaseURL) == "http://example.local:8000" || strings.TrimSpace(cfg.LLMAPIKey) == "replace-me" {
		return errInvalidProductionLLMConfig
	}
	return nil
}
