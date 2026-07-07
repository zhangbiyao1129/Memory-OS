package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
	"memory-os/internal/audit"
	"memory-os/internal/auth"
	"memory-os/internal/candidatememory"
	"memory-os/internal/config"
	"memory-os/internal/db"
	"memory-os/internal/eventlog"
	"memory-os/internal/health"
	"memory-os/internal/hotmemory"
	httpapi "memory-os/internal/http"
	"memory-os/internal/jobs"
	"memory-os/internal/llm"
	"memory-os/internal/logger"
	"memory-os/internal/memorystats"
	"memory-os/internal/qdrant"
	"memory-os/internal/rag"
	"memory-os/internal/redis"
	"memory-os/internal/retrieval"
	"memory-os/internal/secret"
	"memory-os/internal/tenant"
)

var (
	errMissingProductionPostgresDSN = errors.New("postgres dsn is required in production")
	errMissingProductionRedisAddr   = errors.New("redis addr is required in production")
	errMissingProductionQdrantURL   = errors.New("qdrant url is required in production")
	errMissingProductionArchiveDir  = errors.New("archive dir is required in production")
	errInvalidProductionLLMConfig   = errors.New("llm embedding config is required in production")
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	log, err := logger.New(logger.Options{Environment: cfg.AppEnv, Service: "memory-api"})
	if err != nil {
		panic(err)
	}
	defer log.Sync() //nolint:errcheck

	h, err := buildServer(cfg)
	if err != nil {
		panic(err)
	}

	log.Info("memory-api starting")
	h.Spin()
}

func buildServer(cfg config.Config) (*server.Hertz, error) {
	if cfg.APIAddr == "" {
		return nil, errors.New("api addr is required")
	}
	if cfg.AppEnv == "production" {
		if cfg.PostgresDSN == "" {
			return nil, errMissingProductionPostgresDSN
		}
		if cfg.RedisAddr == "" {
			return nil, errMissingProductionRedisAddr
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

	checkers := map[string]health.Checker{}
	var pool *pgxpool.Pool
	if cfg.PostgresDSN != "" {
		postgresPool, err := db.NewPool(context.Background(), cfg.PostgresDSN)
		if err != nil {
			return nil, err
		}
		if err := db.RunEmbeddedMigrations(context.Background(), postgresPool); err != nil {
			return nil, err
		}
		pool = postgresPool
		checkers["db"] = db.Checker{Pool: pool}
	}
	if cfg.RedisAddr != "" {
		client, err := redis.NewClient(cfg.RedisAddr)
		if err != nil {
			return nil, err
		}
		checkers["redis"] = redis.Checker{Client: client}
	}
	if cfg.QdrantURL != "" {
		client, err := qdrant.NewClient(cfg.QdrantURL)
		if err != nil {
			return nil, err
		}
		checkers["qdrant"] = qdrant.Checker{Client: client}
	}

	healthService := health.NewService(checkers)
	h := server.New(server.WithHostPorts(cfg.APIAddr))
	options, err := routerOptions(cfg, healthService, pool)
	if err != nil {
		return nil, err
	}
	httpapi.RegisterRoutes(h.Engine, options)
	return h, nil
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

var newProductionArchiveRAG = func(cfg config.Config, pool *pgxpool.Pool) (rag.Service, error) {
	qdrantClient, err := qdrant.NewClient(cfg.QdrantURL)
	if err != nil {
		return rag.Service{}, err
	}
	if err := qdrantClient.EnsureCollectionSchema(context.Background(), qdrant.CollectionConfig{Name: qdrant.DefaultCollectionName, VectorSize: qdrant.DefaultVectorSize, Distance: qdrant.DefaultDistance}, qdrant.DefaultPayloadIndexConfigs()); err != nil {
		return rag.Service{}, err
	}
	embedder, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, EmbeddingModel: cfg.EmbeddingModel})
	if err != nil {
		return rag.Service{}, err
	}
	return rag.NewService(rag.NewQdrantStore(pool, qdrantClient, embedder, qdrant.DefaultCollectionName)), nil
}

var newProductionHotMemory = func(cfg config.Config, pool *pgxpool.Pool) (hotmemory.Service, error) {
	qdrantClient, err := qdrant.NewClient(cfg.QdrantURL)
	if err != nil {
		return hotmemory.Service{}, err
	}
	if err := qdrantClient.EnsureCollectionSchema(context.Background(), qdrant.CollectionConfig{Name: qdrant.DefaultCollectionName, VectorSize: qdrant.DefaultVectorSize, Distance: qdrant.DefaultDistance}, qdrant.DefaultPayloadIndexConfigs()); err != nil {
		return hotmemory.Service{}, err
	}
	embedder, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, EmbeddingModel: cfg.EmbeddingModel})
	if err != nil {
		return hotmemory.Service{}, err
	}
	return hotmemory.NewServiceWithVectorIndex(hotmemory.NewPGRepository(pool), hotmemory.NewQdrantIndex(pool, qdrantClient, embedder, qdrant.DefaultCollectionName)), nil
}

var newProductionMaintenanceService = func(cfg config.Config, pool *pgxpool.Pool, candidateRepo candidatememory.Repository, composer *candidatememory.TopicComposer) (*candidatememory.MaintenanceService, error) {
	model := strings.TrimSpace(cfg.LLMModel)
	if model == "" {
		model = "MiniMax-M2.7"
	}
	if strings.TrimSpace(cfg.LLMBaseURL) == "" || strings.TrimSpace(cfg.LLMAPIKey) == "" || strings.TrimSpace(model) == "" {
		return nil, nil
	}
	client, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, LLMModel: model, EmbeddingModel: cfg.EmbeddingModel, Timeout: 5 * time.Minute})
	if err != nil {
		return nil, err
	}
	cleaner := candidatememory.NewLLMMaintenanceCleaner(client).WithModel(model)
	return candidatememory.NewMaintenanceService(candidatememory.NewPGMaintenanceRepository(pool), candidateRepo, composer, cleaner), nil
}

var newProductionReranker = func(cfg config.Config) retrieval.Reranker {
	client, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, RerankModel: cfg.RerankModel})
	if err != nil {
		return retrieval.FailingReranker{Err: err}
	}
	return retrieval.NewLLMReranker(client)
}

func routerOptions(cfg config.Config, healthService health.Service, pool *pgxpool.Pool) (httpapi.RouterOptions, error) {
	options := httpapi.RouterOptions{HealthService: healthService, AppEnv: cfg.AppEnv, EnableDevEndpoints: cfg.EnableDevEndpoints, LegacyTurnEventArchive: cfg.LegacyTurnEventArchive}
	if pool == nil {
		return options, nil
	}
	if cfg.AppEnv == "development" && cfg.EnableDevEndpoints {
		return options, nil
	}
	hot, err := newProductionHotMemory(cfg, pool)
	if err != nil {
		return httpapi.RouterOptions{}, err
	}
	archiveRAG, err := newProductionArchiveRAG(cfg, pool)
	if err != nil {
		return httpapi.RouterOptions{}, err
	}
	options.AuthService = auth.NewService(auth.NewPGRepository(pool))
	options.TenantService = tenant.NewService(tenant.NewPGRepository(pool))
	accessLog := productionAccessLog(pool)
	options.RetrievalService = retrieval.NewService(retrieval.Options{HotMemory: hot, ArchiveRAG: archiveRAG, ArchiveGenerationResolver: retrieval.NewPGArchiveGenerationResolver(pool), Reranker: newProductionReranker(cfg), AccessLog: accessLog, MinRerankScore: cfg.RerankMinScore})
	options.HotMemoryService = hot
	options.RetrievalAccessLog = accessLog
	options.EventLogService = eventlog.NewService(eventlog.NewPGRepository(pool), eventlog.SanitizerOptions{MaxTurnEventBytes: 256 * 1024, MaxToolOutputBytes: 64 * 1024})
	options.AuditService = audit.NewService(audit.NewPGRepository(pool))
	options.ArchiveService = archive.NewService(archive.NewPGRepository(pool), cfg.ArchiveDir)
	options.SecretStore = secret.NewStore(secret.NewPGRepository(pool))
	options.ArchiveQueue = jobs.NewPGArchiveQueue(pool, jobs.PGArchiveQueueOptions{WorkerID: "memory-api"})
	options.ArchiveIndexQueue = jobs.NewPGArchiveIndexQueue(pool, jobs.PGArchiveIndexQueueOptions{WorkerID: "memory-api"})
	candidateRepo := candidatememory.NewPGRepository(pool)
	options.CandidateQueue = jobs.NewPGCandidateMemoryQueue(candidateRepo, jobs.PGCandidateMemoryQueueOptions{WorkerID: "memory-api"})
	options.CandidateService = candidatememory.NewService(candidateRepo, candidatememory.RuleScorer{})
	options.TopicRepository = candidateRepo
	archiveIndexQueue := jobs.NewPGArchiveIndexQueue(pool, jobs.PGArchiveIndexQueueOptions{WorkerID: "memory-api"})
	archiveCreator := jobs.NewProductionArchiveCreator(options.ArchiveService, archiveIndexQueue)
	composer := candidatememory.NewTopicComposer(candidateRepo, archiveCreator)
	options.TopicComposer = &composer
	maintenanceService, err := newProductionMaintenanceService(cfg, pool, candidateRepo, &composer)
	if err != nil {
		return httpapi.RouterOptions{}, err
	}
	options.MaintenanceService = maintenanceService
	qdrantClient, err := qdrant.NewClient(cfg.QdrantURL)
	if err != nil {
		return httpapi.RouterOptions{}, err
	}
	options.QdrantStatusService = qdrant.NewStatusService(qdrant.StatusOptions{
		Client:         qdrantClient,
		Store:          qdrant.NewPGStatusStore(pool),
		CollectionName: qdrant.DefaultCollectionName,
	})
	options.MemoryStatsService = memorystats.NewService(memorystats.NewPGRepository(pool))
	return options, nil
}

func productionAccessLog(pool *pgxpool.Pool) *retrieval.PGAccessLog {
	return retrieval.NewPGAccessLog(pool)
}
