package main

import (
	"context"
	"encoding/json"
	"errors"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"memory-os/internal/archive"
	"memory-os/internal/candidatememory"
	"memory-os/internal/config"
	"memory-os/internal/db"
	"memory-os/internal/eventlog"
	"memory-os/internal/hotmemory"
	"memory-os/internal/jobs"
	"memory-os/internal/llm"
	"memory-os/internal/logger"
	"memory-os/internal/qdrant"
	"memory-os/internal/rag"
	"memory-os/internal/tenant"

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
var newHotMemoryService = func(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (hotmemory.Service, error) {
	qdrantClient, err := qdrant.NewClient(cfg.QdrantURL)
	if err != nil {
		return hotmemory.Service{}, err
	}
	if err := qdrantClient.EnsureCollectionSchema(ctx, qdrant.CollectionConfig{Name: qdrant.DefaultCollectionName, VectorSize: qdrant.DefaultVectorSize, Distance: qdrant.DefaultDistance}, qdrant.DefaultPayloadIndexConfigs()); err != nil {
		return hotmemory.Service{}, err
	}
	embedder, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, EmbeddingModel: cfg.EmbeddingModel})
	if err != nil {
		return hotmemory.Service{}, err
	}
	return hotmemory.NewServiceWithVectorIndex(hotmemory.NewPGRepository(pool), hotmemory.NewQdrantIndex(pool, qdrantClient, embedder, qdrant.DefaultCollectionName)), nil
}
var newOpenAICompatibleClient = llm.NewOpenAICompatible
var newCandidateMemoryWorker = jobs.NewCandidateMemoryWorker
var newAutoMaintenanceService = func(cfg config.Config, pool *pgxpool.Pool, candidateRepo candidatememory.Repository, composer *candidatememory.TopicComposer) (jobs.AutoMaintenance, error) {
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

type tenantProjectCatalog struct {
	repo tenant.Repository
}

func (c tenantProjectCatalog) ListProjectsForTriage(userID, orgID string) ([]candidatememory.TriageProject, error) {
	projects, err := c.repo.ListProjects(userID, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]candidatememory.TriageProject, 0, len(projects))
	for _, project := range projects {
		out = append(out, candidatememory.TriageProject{
			ProjectID: project.ID,
			Name:      project.Name,
			Slug:      project.Slug,
			SourceKey: project.SourceKey,
		})
	}
	return out, nil
}

// eventlogCandidateLoader 把候选任务转换为提炼请求:从 eventlog 加载已保存(已脱敏)事件。
type eventlogCandidateLoader struct {
	eventlog eventlog.Service
}

func (l eventlogCandidateLoader) LoadExtractionRequest(ctx context.Context, job candidatememory.Job) (candidatememory.ExtractionRequest, error) {
	event, err := l.eventlog.GetEvent(job.SourceEventID)
	if err != nil {
		return candidatememory.ExtractionRequest{}, err
	}
	payload, _ := json.Marshal(event.Payload)
	return candidatememory.ExtractionRequest{
		OrgID:     event.Actor.OrgID,
		ProjectID: event.Actor.ProjectID,
		UserID:    event.Actor.UserID,
		AgentID:   event.Actor.AgentID,
		ThreadID:  event.ThreadID,
		SessionID: event.SessionID,
		SourceKey: job.SourceKey,
		Events: []candidatememory.ExtractionEvent{{
			EventID: event.EventID,
			Type:    string(event.Type),
			Payload: payload,
		}},
	}, nil
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
	var candidateWorker *jobs.CandidateMemoryWorker
	var candidateQueue jobs.CandidateMemoryQueue
	var autoMaintenance jobs.AutoMaintenance
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
		archiveService := newArchiveService(archive.NewPGRepository(pool), cfg.ArchiveDir)
		worker := jobs.NewArchiveWorkerWithIndexQueue(archiveService, ragIndexQueue)
		archiveWorker = &worker
		archiveQueue = newArchiveQueue(pool)
		ragIndexWorker = newRAGIndexWorker(ragIndexStore)
		cleanup = func() { closePostgresPool(pool) }

		// 候选记忆链路(Phase 4):queue 始终装配(memory-api enqueue 用),worker 需 LLM。
		candidateRepo := candidatememory.NewPGRepository(pool)
		triageRepo := candidatememory.NewPGTriageRepository(pool)
		candidateQueue = jobs.NewPGCandidateMemoryQueue(candidateRepo, jobs.PGCandidateMemoryQueueOptions{WorkerID: "memory-worker"})
		if llmClient, llmErr := newOpenAICompatibleClient(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, LLMModel: cfg.LLMModel, EmbeddingModel: cfg.EmbeddingModel}); llmErr == nil {
			hotMemoryService, err := newHotMemoryService(context.Background(), cfg, pool)
			if err != nil {
				closePostgresPool(pool)
				return nil, err
			}
			extractor := candidatememory.NewLLMExtractor(llmClient).WithModel(cfg.LLMModel)
			candidateService := candidatememory.NewService(candidateRepo, candidatememory.RuleScorer{})
			eventLoader := eventlogCandidateLoader{eventlog: eventlog.NewService(eventlog.NewPGRepository(pool), eventlog.SanitizerOptions{MaxTurnEventBytes: 256 * 1024, MaxToolOutputBytes: 64 * 1024})}
			worker := newCandidateMemoryWorker(extractor, candidatememory.NewRouter(hotMemoryService), candidateService, candidateRepo, eventLoader)
			candidateWorker = &worker
			archiveCreator := jobs.NewProductionArchiveCreator(archiveService, ragIndexQueue)
			composer := candidatememory.NewTopicComposer(candidateRepo, archiveCreator)
			triageService := candidatememory.NewTriageService(candidatememory.TriageServiceOptions{
				Repo:           triageRepo,
				Classifier:     candidatememory.NewLLMTriageClassifier(llmClient).WithModel(cfg.LLMModel),
				Fallback:       candidatememory.RuleTriageClassifier{},
				ProjectCatalog: tenantProjectCatalog{repo: tenant.NewPGRepository(pool)},
				HotMemory:      hotMemoryService,
			})
			autoMaintenance, err = newAutoMaintenanceService(cfg, pool, candidateRepo, &composer)
			if err != nil {
				closePostgresPool(pool)
				return nil, err
			}
			if service, ok := autoMaintenance.(*candidatememory.MaintenanceService); ok {
				service.WithTriage(triageService)
			}
		}
	}
	runner := jobs.NewRunner(jobs.Options{Concurrency: 1, ArchiveWorker: archiveWorker, ArchiveQueue: archiveQueue, RAGIndexWorker: ragIndexWorker, RAGIndexQueue: ragIndexQueue, CandidateWorker: candidateWorker, CandidateQueue: candidateQueue, AutoMaintenance: autoMaintenance, AutoMaintenanceInterval: 5 * time.Minute, Cleanup: cleanup})
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
