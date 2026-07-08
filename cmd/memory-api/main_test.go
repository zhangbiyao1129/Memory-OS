package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/candidatememory"
	"memory-os/internal/config"
	"memory-os/internal/health"
	"memory-os/internal/hotmemory"
	"memory-os/internal/rag"
)

func TestBuildServer(t *testing.T) {
	cfg := config.Config{APIAddr: ":18081", RedisAddr: "", QdrantURL: ""}

	server, err := buildServer(cfg)
	if err != nil {
		t.Fatalf("buildServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("buildServer() returned nil server")
	}
}

func TestBuildServerRejectsMissingAPIAddr(t *testing.T) {
	_, err := buildServer(config.Config{})
	if err == nil {
		t.Fatal("buildServer() error = nil, want missing addr error")
	}
}

func TestBuildServerRejectsMissingPostgresDSNInProduction(t *testing.T) {
	_, err := buildServer(config.Config{APIAddr: ":18081", AppEnv: "production"})
	if !errors.Is(err, errMissingProductionPostgresDSN) {
		t.Fatalf("buildServer() error = %v, want %v", err, errMissingProductionPostgresDSN)
	}
}

func TestBuildServerRejectsMissingRedisAddrInProduction(t *testing.T) {
	_, err := buildServer(config.Config{APIAddr: ":18081", AppEnv: "production", PostgresDSN: "postgres://memory_os:secret@postgres:5432/memory_os", QdrantURL: "http://qdrant:6333"})
	if !errors.Is(err, errMissingProductionRedisAddr) {
		t.Fatalf("buildServer() error = %v, want %v", err, errMissingProductionRedisAddr)
	}
}

func TestBuildServerRejectsMissingQdrantURLInProduction(t *testing.T) {
	_, err := buildServer(config.Config{APIAddr: ":18081", AppEnv: "production", PostgresDSN: "postgres://memory_os:secret@postgres:5432/memory_os", RedisAddr: "redis:6379"})
	if !errors.Is(err, errMissingProductionQdrantURL) {
		t.Fatalf("buildServer() error = %v, want %v", err, errMissingProductionQdrantURL)
	}
}

func TestBuildServerRejectsMissingArchiveDirInProduction(t *testing.T) {
	cfg := productionAPIConfig()
	cfg.ArchiveDir = ""

	_, err := buildServer(cfg)
	if !errors.Is(err, errMissingProductionArchiveDir) {
		t.Fatalf("buildServer() error = %v, want %v", err, errMissingProductionArchiveDir)
	}
}

func TestBuildServerRejectsPlaceholderLLMConfigInProduction(t *testing.T) {
	cfg := productionAPIConfig()
	cfg.LLMBaseURL = "http://example.local:8000"
	cfg.LLMAPIKey = "replace-me"

	_, err := buildServer(cfg)
	if !errors.Is(err, errInvalidProductionLLMConfig) {
		t.Fatalf("buildServer() error = %v, want %v", err, errInvalidProductionLLMConfig)
	}
}

func TestRouterOptionsConfiguresCoreServicesWhenPostgresPoolExists(t *testing.T) {
	restoreProductionArchiveRAG := stubProductionArchiveRAG(t)
	restoreProductionHotMemory := stubProductionHotMemory(t)

	options, err := routerOptions(productionAPIConfig(), health.NewService(nil), &pgxpool.Pool{})
	if err != nil {
		t.Fatalf("routerOptions() error = %v", err)
	}

	if !options.AuthService.Configured() {
		t.Fatal("AuthService not configured")
	}
	if !options.TenantService.Configured() {
		t.Fatal("TenantService not configured")
	}
	if !options.RetrievalService.Configured() {
		t.Fatal("RetrievalService not configured")
	}
	if !options.HotMemoryService.Configured() {
		t.Fatal("HotMemoryService not configured")
	}
	if !options.EventLogService.Configured() {
		t.Fatal("EventLogService not configured")
	}
	if !options.AuditService.Configured() {
		t.Fatal("AuditService not configured")
	}
	if !options.SecretStore.Configured() {
		t.Fatal("SecretStore not configured")
	}
	if !options.ArchiveService.Configured() {
		t.Fatal("ArchiveService not configured")
	}
	if options.TopicComposer == nil {
		t.Fatal("TopicComposer not configured")
	}
	if !options.TopicComposer.SummarizerConfigured() {
		t.Fatal("TopicComposer did not configure AI archive summarizer")
	}
	if options.ArchiveQueue == nil {
		t.Fatal("ArchiveQueue not configured")
	}
	if options.ArchiveIndexQueue == nil {
		t.Fatal("ArchiveIndexQueue not configured")
	}
	if !options.QdrantStatusService.Configured() {
		t.Fatal("QdrantStatusService not configured")
	}
	if !options.MemoryStatsService.Configured() {
		t.Fatal("MemoryStatsService not configured")
	}
	if options.AppEnv != "production" {
		t.Fatalf("AppEnv = %q, want production", options.AppEnv)
	}
	if !restoreProductionArchiveRAG.called {
		t.Fatal("production Archive RAG was not configured")
	}
	if !restoreProductionHotMemory.called {
		t.Fatal("production Hot Memory vector index was not configured")
	}
}

func TestRouterOptionsConfiguresMaintenanceServiceWhenLLMConfigured(t *testing.T) {
	restoreProductionArchiveRAG := stubProductionArchiveRAG(t)
	restoreProductionHotMemory := stubProductionHotMemory(t)
	old := newProductionMaintenanceService
	newProductionMaintenanceService = func(cfg config.Config, pool *pgxpool.Pool, candidateRepo candidatememory.Repository, composer *candidatememory.TopicComposer, hotMemory candidatememory.HotMemorySink) (*candidatememory.MaintenanceService, error) {
		maintRepo := maintenanceRepoStub{}
		cleaner := maintenanceCleanerStub{}
		return candidatememory.NewMaintenanceService(maintRepo, candidateRepo, composer, cleaner), nil
	}
	t.Cleanup(func() { newProductionMaintenanceService = old })

	options, err := routerOptions(productionAPIConfig(), health.NewService(nil), &pgxpool.Pool{})
	if err != nil {
		t.Fatalf("routerOptions() error = %v", err)
	}

	if options.MaintenanceService == nil {
		t.Fatal("MaintenanceService not configured")
	}
	if !restoreProductionArchiveRAG.called {
		t.Fatal("production Archive RAG was not configured")
	}
	if !restoreProductionHotMemory.called {
		t.Fatal("production Hot Memory vector index was not configured")
	}
}

func TestNewProductionMaintenanceServiceUsesUnifiedOrganizer(t *testing.T) {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	service, err := newProductionMaintenanceService(productionAPIConfig(), &pgxpool.Pool{}, candidatememory.NewInMemoryRepository(), nil, hot)
	if err != nil {
		t.Fatalf("newProductionMaintenanceService() error = %v", err)
	}
	if service == nil {
		t.Fatal("newProductionMaintenanceService() returned nil")
	}
	if !service.OrganizerConfigured() {
		t.Fatal("production maintenance service did not configure unified organizer")
	}
	if !service.HotMemoryConfigured() {
		t.Fatal("production maintenance service did not configure hot memory sink")
	}
}

func TestRouterOptionsLeavesAuthOpenForDevelopmentSmoke(t *testing.T) {
	options, err := routerOptions(config.Config{AppEnv: "development", EnableDevEndpoints: true}, health.NewService(nil), &pgxpool.Pool{})
	if err != nil {
		t.Fatalf("routerOptions() error = %v", err)
	}

	if options.AuthService.Configured() {
		t.Fatal("AuthService configured in development smoke mode")
	}
	if options.TenantService.Configured() {
		t.Fatal("TenantService configured in development smoke mode")
	}
	if options.RetrievalService.Configured() {
		t.Fatal("RetrievalService configured in development smoke mode")
	}
	if options.HotMemoryService.Configured() {
		t.Fatal("HotMemoryService configured in development smoke mode")
	}
	if options.EventLogService.Configured() {
		t.Fatal("EventLogService configured in development smoke mode")
	}
	if options.AuditService.Configured() {
		t.Fatal("AuditService configured in development smoke mode")
	}
	if options.SecretStore.Configured() {
		t.Fatal("SecretStore configured in development smoke mode")
	}
	if options.ArchiveService.Configured() {
		t.Fatal("ArchiveService configured in development smoke mode")
	}
	if options.ArchiveQueue != nil {
		t.Fatal("ArchiveQueue configured in development smoke mode")
	}
	if options.ArchiveIndexQueue != nil {
		t.Fatal("ArchiveIndexQueue configured in development smoke mode")
	}
	if options.QdrantStatusService.Configured() {
		t.Fatal("QdrantStatusService configured in development smoke mode")
	}
	if options.MemoryStatsService.Configured() {
		t.Fatal("MemoryStatsService configured in development smoke mode")
	}
}

func TestRouterOptionsReturnsArchiveRAGConfigurationError(t *testing.T) {
	stubProductionHotMemory(t)
	old := newProductionArchiveRAG
	newProductionArchiveRAG = func(cfg config.Config, pool *pgxpool.Pool) (rag.Service, error) {
		return rag.Service{}, errArchiveRAGForTest
	}
	t.Cleanup(func() { newProductionArchiveRAG = old })

	_, err := routerOptions(secretVaultTestConfig(), health.NewService(nil), &pgxpool.Pool{})

	if err == nil {
		t.Fatal("routerOptions() error = nil, want archive rag configuration error")
	}
}

func TestRouterOptionsReturnsHotMemoryConfigurationError(t *testing.T) {
	old := newProductionHotMemory
	newProductionHotMemory = func(cfg config.Config, pool *pgxpool.Pool) (hotmemory.Service, error) {
		return hotmemory.Service{}, errHotMemoryForTest
	}
	t.Cleanup(func() { newProductionHotMemory = old })

	_, err := routerOptions(secretVaultTestConfig(), health.NewService(nil), &pgxpool.Pool{})

	if err == nil {
		t.Fatal("routerOptions() error = nil, want hot memory configuration error")
	}
}

func TestProductionAccessLogUsesPostgres(t *testing.T) {
	accessLog := productionAccessLog(&pgxpool.Pool{})

	if accessLog == nil {
		t.Fatal("productionAccessLog() = nil, want *retrieval.PGAccessLog")
	}
}

type archiveRAGStubState struct {
	called bool
}

var errArchiveRAGForTest = &testError{"archive rag unavailable"}
var errHotMemoryForTest = &testError{"hot memory unavailable"}

func stubProductionArchiveRAG(t *testing.T) *archiveRAGStubState {
	t.Helper()
	state := &archiveRAGStubState{}
	old := newProductionArchiveRAG
	newProductionArchiveRAG = func(cfg config.Config, pool *pgxpool.Pool) (rag.Service, error) {
		state.called = cfg.AppEnv == "production" && pool != nil
		return rag.NewService(rag.NewMemoryStore()), nil
	}
	t.Cleanup(func() { newProductionArchiveRAG = old })
	return state
}

type hotMemoryStubState struct {
	called bool
}

func stubProductionHotMemory(t *testing.T) *hotMemoryStubState {
	t.Helper()
	state := &hotMemoryStubState{}
	old := newProductionHotMemory
	newProductionHotMemory = func(cfg config.Config, pool *pgxpool.Pool) (hotmemory.Service, error) {
		state.called = cfg.AppEnv == "production" && pool != nil
		return hotmemory.NewService(hotmemory.NewMemoryRepository()), nil
	}
	t.Cleanup(func() { newProductionHotMemory = old })
	return state
}

type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}

func productionAPIConfig() config.Config {
	return config.Config{
		APIAddr:        ":18081",
		AppEnv:         "production",
		PostgresDSN:    "postgres://memory_os:secret@postgres:5432/memory_os",
		RedisAddr:      "redis:6379",
		QdrantURL:      "http://qdrant:6333",
		ArchiveDir:     "/data/memory-os",
		LLMBaseURL:     "http://llm.local:8000",
		LLMAPIKey:      "test-key",
		EmbeddingModel: "bge-m3",
	}
}

type maintenanceCleanerStub struct{}

func (maintenanceCleanerStub) Clean(ctx context.Context, candidates []candidatememory.Candidate) (candidatememory.CleanResult, error) {
	return candidatememory.CleanResult{Summary: "ok"}, nil
}

type maintenanceRepoStub struct{}

func (maintenanceRepoStub) CreateRun(ctx context.Context, run candidatememory.MaintenanceRun) (candidatememory.MaintenanceRun, error) {
	return run, nil
}

func (maintenanceRepoStub) GetRun(ctx context.Context, runID string) (candidatememory.MaintenanceRun, error) {
	return candidatememory.MaintenanceRun{}, nil
}

func (maintenanceRepoStub) UpdateRun(ctx context.Context, runID string, status candidatememory.MaintenanceRunStatus, update candidatememory.MaintenanceRunUpdate) error {
	return nil
}

func (maintenanceRepoStub) GetRunningRun(ctx context.Context, orgID, projectID string) (*candidatememory.MaintenanceRun, error) {
	return nil, nil
}

func (maintenanceRepoStub) GetRunningRunInScope(ctx context.Context, orgID, projectID, sourceKey, threadID string) (*candidatememory.MaintenanceRun, error) {
	return nil, nil
}

func (maintenanceRepoStub) UpdateStage(ctx context.Context, runID string, stage candidatememory.MaintenanceRunStage, totalCandidates int) error {
	return nil
}

func (maintenanceRepoStub) MarkStaleRunningAsFailed(ctx context.Context, before time.Time) (int, error) {
	return 0, nil
}

func secretVaultTestConfig() config.Config {
	return config.Config{
		AppEnv:     "production",
		ArchiveDir: "/data/memory-os",
		QdrantURL:  "http://qdrant:6333",
	}
}
