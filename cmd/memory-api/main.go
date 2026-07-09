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
	"memory-os/internal/memorykernel"
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

var newProductionMaintenanceService = func(cfg config.Config, pool *pgxpool.Pool, candidateRepo candidatememory.Repository, composer *candidatememory.TopicComposer, hotMemory candidatememory.HotMemorySink) (*candidatememory.MaintenanceService, error) {
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
	organizer := candidatememory.NewLLMOrganizer(client).WithModel(model)
	return candidatememory.NewMaintenanceService(candidatememory.NewPGMaintenanceRepository(pool), candidateRepo, composer, nil).WithOrganizer(organizer).WithHotMemory(hotMemory), nil
}

var newProductionArchiveSummarizer = func(cfg config.Config) (candidatememory.ArchiveSummarizer, error) {
	model := strings.TrimSpace(cfg.LLMModel)
	if model == "" {
		model = "MiniMax-M2.7"
	}
	if strings.TrimSpace(cfg.LLMBaseURL) == "" || strings.TrimSpace(cfg.LLMAPIKey) == "" || strings.TrimSpace(model) == "" {
		return nil, nil
	}
	client, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, LLMModel: model, Timeout: 5 * time.Minute})
	if err != nil {
		return nil, err
	}
	return candidatememory.NewLLMArchiveSummarizer(client).WithModel(model), nil
}

var newProductionHotMemoryOrganizer = func(cfg config.Config) (hotmemory.Organizer, error) {
	model := strings.TrimSpace(cfg.LLMModel)
	if model == "" {
		model = "MiniMax-M2.7"
	}
	if strings.TrimSpace(cfg.LLMBaseURL) == "" || strings.TrimSpace(cfg.LLMAPIKey) == "" || strings.TrimSpace(model) == "" {
		return nil, nil
	}
	client, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, LLMModel: model, Timeout: 5 * time.Minute})
	if err != nil {
		return nil, err
	}
	return hotmemory.NewLLMOrganizer(client).WithModel(model), nil
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
	hotOrganizer, err := newProductionHotMemoryOrganizer(cfg)
	if err != nil {
		return httpapi.RouterOptions{}, err
	}
	if hotOrganizer != nil {
		hot = hot.WithOrganizer(hotOrganizer)
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
	options.TriageRepository = candidatememory.NewPGTriageRepository(pool)
	archiveIndexQueue := jobs.NewPGArchiveIndexQueue(pool, jobs.PGArchiveIndexQueueOptions{WorkerID: "memory-api"})
	archiveCreator := jobs.NewProductionArchiveCreator(options.ArchiveService, archiveIndexQueue)
	composer := candidatememory.NewTopicComposer(candidateRepo, archiveCreator)
	archiveSummarizer, err := newProductionArchiveSummarizer(cfg)
	if err != nil {
		return httpapi.RouterOptions{}, err
	}
	if archiveSummarizer != nil {
		composer = composer.WithSummarizer(archiveSummarizer)
	}
	options.TopicComposer = &composer
	maintenanceService, err := newProductionMaintenanceService(cfg, pool, candidateRepo, &composer, hot)
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

	// Memory Kernel
	kernelRepo := memorykernel.NewPGRepository(pool)
	model := strings.TrimSpace(cfg.LLMModel)
	if model == "" {
		model = "MiniMax-M2.7"
	}
	llmClient, llmErr := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, LLMModel: model, EmbeddingModel: cfg.EmbeddingModel, Timeout: 5 * time.Minute})
	if llmErr == nil {
		kernelCollector := memorykernel.NewCollector(memorykernel.CollectorOptions{
			Candidates:    &kernelCandidateAdapter{repo: candidateRepo},
			HotMemories:   &kernelHotMemoryAdapter{},
			Archives:      &kernelArchiveAdapter{repo: archive.NewPGRepository(pool)},
			Retrievals:    &kernelRetrievalAdapter{log: accessLog},
			ExistingUnits: kernelRepo,
		})
		kernelClassifier := memorykernel.NewLLMClassifier(llmClient).WithModel(model)
		kernelService := memorykernel.NewService(memorykernel.ServiceOptions{
			Repository:       kernelRepo,
			Collector:        kernelCollector,
			Classifier:       kernelClassifier,
			CandidateApplier: &kernelCandidateApplier{repo: candidateRepo},
		})
		options.MemoryKernelService = kernelService
		options.ContextPackService = memorykernel.NewContextPackBuilder(kernelRepo)
	}
	return options, nil
}

func productionAccessLog(pool *pgxpool.Pool) *retrieval.PGAccessLog {
	return retrieval.NewPGAccessLog(pool)
}

// --- Memory Kernel adapters ---

type kernelCandidateAdapter struct {
	repo candidatememory.Repository
}

func (a *kernelCandidateAdapter) ListKernelCandidates(ctx context.Context, scope memorykernel.Scope, limit int) ([]memorykernel.CandidateInput, error) {
	candidates, err := a.repo.ListCandidates(ctx, candidatememory.ListFilter{OrgID: scope.OrgID, ProjectID: scope.ProjectID, SourceKey: scope.SourceKey, Limit: limit})
	if err != nil {
		return nil, err
	}
	var out []memorykernel.CandidateInput
	for _, c := range candidates {
		out = append(out, memorykernel.CandidateInput{
			ID: c.CandidateID, Content: c.Content, Summary: c.Summary,
			Type: string(c.MemoryType), RiskLevel: string(c.RiskLevel), Status: string(c.Status), Confidence: c.Confidence,
		})
	}
	return out, nil
}

type kernelHotMemoryAdapter struct{}

func (a *kernelHotMemoryAdapter) ListKernelHotMemories(_ context.Context, _ memorykernel.Scope, _ int) ([]memorykernel.HotMemoryInput, error) {
	return nil, nil
}

type kernelArchiveAdapter struct {
	repo archive.Repository
}

func (a *kernelArchiveAdapter) ListKernelArchives(_ context.Context, scope memorykernel.Scope, limit int) ([]memorykernel.ArchiveInput, error) {
	archives, err := a.repo.List(archive.ListFilter{OrgID: scope.OrgID, ProjectID: scope.ProjectID, Limit: limit})
	if err != nil {
		return nil, err
	}
	var out []memorykernel.ArchiveInput
	for _, a := range archives {
		out = append(out, memorykernel.ArchiveInput{ID: a.ArchiveID, Title: a.Title, Status: a.Status, UpdatedAt: a.UpdatedAt})
	}
	return out, nil
}

type kernelRetrievalAdapter struct {
	log retrieval.AccessLogReader
}

func (a *kernelRetrievalAdapter) ListKernelRetrievals(_ context.Context, scope memorykernel.Scope, limit int) ([]memorykernel.RetrievalInput, error) {
	if a.log == nil {
		return nil, nil
	}
	results, err := a.log.ListResults(retrieval.AccessLogListFilter{OrgID: scope.OrgID, ProjectID: scope.ProjectID, Limit: limit})
	if err != nil {
		return nil, err
	}
	var out []memorykernel.RetrievalInput
	for _, r := range results {
		out = append(out, memorykernel.RetrievalInput{RequestID: r.RequestID, SourceKind: r.SourceKind, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

type kernelCandidateApplier struct {
	repo candidatememory.Repository
}

func (a *kernelCandidateApplier) UpdateCandidateGovernance(ctx context.Context, orgID, candidateID string, status interface{}, needsReview bool, reason string, supersededBy string) (interface{}, error) {
	return a.repo.UpdateCandidateGovernance(ctx, orgID, candidateID, candidatememory.Status(status.(string)), needsReview, reason, supersededBy)
}
