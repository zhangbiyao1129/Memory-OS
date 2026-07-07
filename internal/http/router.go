package http

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

	"memory-os/internal/archive"
	"memory-os/internal/audit"
	"memory-os/internal/auth"
	"memory-os/internal/buildinfo"
	"memory-os/internal/candidatememory"
	"memory-os/internal/eventlog"
	"memory-os/internal/health"
	"memory-os/internal/hotmemory"
	"memory-os/internal/jobs"
	"memory-os/internal/memorystats"
	"memory-os/internal/qdrant"
	"memory-os/internal/rag"
	"memory-os/internal/retrieval"
	"memory-os/internal/secret"
	"memory-os/internal/tenant"
	"memory-os/internal/workspace"
)

var devEventLogService = eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{MaxTurnEventBytes: 256 * 1024, MaxToolOutputBytes: 64 * 1024})
var defaultSetupCodes = newMemorySetupCodeStore(10 * time.Minute)

type RouterOptions struct {
	HealthService          health.Service
	AppEnv                 string
	EnableDevEndpoints     bool
	AuthService            auth.Service
	TenantService          tenant.Service
	RetrievalService       retrieval.Service
	RetrievalAccessLog     retrieval.AccessLogReader
	HotMemoryService       hotmemory.Service
	EventLogService        eventlog.Service
	AuditService           audit.Service
	SecretStore            secret.Store
	ArchiveService         archive.Service
	ArchiveQueue           archiveEnqueuer
	CandidateQueue         candidateEnqueuer
	CandidateService       *candidatememory.Service
	TopicComposer          *candidatememory.TopicComposer
	TopicRepository        candidatememory.Repository
	LegacyTurnEventArchive bool
	ArchiveIndexQueue      archiveIndexQueue
	QdrantStatusService    qdrant.StatusService
	MemoryStatsService     memorystats.Service
	MaintenanceService     *candidatememory.MaintenanceService
}

type archiveEnqueuer interface {
	Enqueue(ctx context.Context, job jobs.ArchiveJob) error
}

type candidateEnqueuer interface {
	Enqueue(ctx context.Context, job candidatememory.Job) error
}

type archiveIndexEnqueuer interface {
	Enqueue(ctx context.Context, job jobs.RAGIndexJob) error
}

type archiveIndexRetrier interface {
	RetryFailed(ctx context.Context, archiveID string, indexGeneration int) (int64, error)
}

type archiveIndexQueue interface {
	archiveIndexEnqueuer
	archiveIndexRetrier
}

// NewRouter 创建 Memory API 的 Hertz 路由。
func NewRouter(healthService health.Service) *route.Engine {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: healthService, AppEnv: "test"})
	return h.Engine
}

func RegisterRoutes(engine *route.Engine, options RouterOptions) {
	if err := validateProductionRouterOptions(options); err != nil {
		panic(err)
	}
	engine.Use(CORSMiddleware())
	engine.OPTIONS("/*path", CORSPreflightHandler())
	engine.GET("/healthz", HealthHandler(options.HealthService))
	engine.GET("/version", VersionHandler())
	engine.GET("/openapi.json", OpenAPIHandler())
	engine.GET("/memory/setup/install.sh", SetupInstallScriptHandler())
	eventLogService := options.EventLogService
	if !eventLogService.Configured() {
		eventLogService = devEventLogService
	}
	engine.POST("/memory/turn-event", TurnEventHandler(eventLogService, options.AuthService, options.TenantService, options.ArchiveQueue, options.CandidateQueue, options.LegacyTurnEventArchive))
	engine.POST("/memory/auth/login-password", PasswordLoginHandler(options.AuthService, options.TenantService, options.AuditService))
	engine.POST("/memory/search", MemorySearchHandler(options.AuthService, options.TenantService, options.RetrievalService))
	engine.POST("/memory/archive/create", ArchiveCreateHandler(options.ArchiveService, options.AuthService, options.TenantService))
	engine.POST("/memory/archive/edit", ArchiveEditHandler(options.ArchiveService, options.AuthService, options.TenantService))
	engine.POST("/memory/archive/detail", ArchiveDetailHandler(options.ArchiveService, options.AuthService, options.TenantService))
	engine.POST("/memory/archive/versions", ArchiveVersionsHandler(options.ArchiveService, options.AuthService, options.TenantService))
	engine.POST("/memory/archive/list", ArchiveListHandler(options.ArchiveService, options.AuthService, options.TenantService))
	engine.POST("/memory/archive/delete", ArchiveDeleteHandler(options.ArchiveService, options.AuthService, options.TenantService))
	engine.POST("/memory/archive/reindex", ArchiveReindexHandler(options.ArchiveService, options.ArchiveIndexQueue, options.AuthService, options.TenantService))
	engine.POST("/memory/archive/index-retry", ArchiveIndexRetryHandler(options.ArchiveService, options.ArchiveIndexQueue, options.AuthService, options.TenantService))
	engine.POST("/memory/archive/index-status", ArchiveIndexStatusHandler(options.ArchiveService, options.QdrantStatusService, options.AuthService, options.TenantService))
	engine.POST("/memory/hot-memory/create", HotMemoryCreateHandler(options.HotMemoryService, options.AuthService, options.TenantService))
	engine.POST("/memory/hot-memory/list", HotMemoryListHandler(options.HotMemoryService, options.AuthService, options.TenantService))
	engine.POST("/memory/hot-memory/edit", HotMemoryEditHandler(options.HotMemoryService, options.AuthService, options.TenantService))
	engine.POST("/memory/hot-memory/promote", HotMemoryPromoteHandler(options.HotMemoryService, options.AuthService, options.TenantService, options.AuditService))
	engine.POST("/memory/hot-memory/demote", HotMemoryDemoteHandler(options.HotMemoryService, options.AuthService, options.TenantService, options.AuditService))
	engine.POST("/memory/hot-memory/mark-used", HotMemoryMarkUsedHandler(options.HotMemoryService, options.AuthService, options.TenantService, options.AuditService))
	engine.POST("/memory/hot-memory/delete", HotMemoryDeleteHandler(options.HotMemoryService, options.AuthService, options.TenantService, options.AuditService))
	engine.POST("/memory/hot-memory/pin", HotMemoryPinHandler(options.HotMemoryService, options.AuthService, options.TenantService, options.AuditService))
	engine.POST("/memory/hot-memory/unpin", HotMemoryUnpinHandler(options.HotMemoryService, options.AuthService, options.TenantService, options.AuditService))
	if options.CandidateService != nil {
		engine.POST("/memory/candidates/list", CandidateListHandler(options.CandidateService, options.AuthService, options.TenantService))
		engine.POST("/memory/candidates/accept", CandidateAcceptHandler(options.CandidateService, options.AuthService, options.TenantService))
		engine.POST("/memory/candidates/discard", CandidateDiscardHandler(options.CandidateService, options.AuthService, options.TenantService))
	}
	if options.TopicComposer != nil {
		engine.POST("/memory/candidates/compose", TopicComposeHandler(options.TopicComposer, options.AuthService, options.TenantService))
	}
	if options.TopicRepository != nil {
		engine.POST("/memory/topics/list", TopicListHandler(options.TopicRepository, options.AuthService, options.TenantService))
	}
	if options.MaintenanceService != nil {
		engine.POST("/memory/candidates/maintenance/run", MaintenanceRunHandler(options.MaintenanceService, options.AuthService, options.TenantService))
		engine.POST("/memory/candidates/maintenance/status", MaintenanceStatusHandler(options.MaintenanceService, options.AuthService, options.TenantService))
	}
	engine.POST("/memory/secrets/create", SecretCreateHandler(options.SecretStore, options.AuthService, options.TenantService))
	engine.POST("/memory/secrets/list", SecretListHandler(options.SecretStore, options.AuthService, options.TenantService))
	engine.POST("/memory/secrets/ciphertext", SecretCiphertextHandler(options.SecretStore, options.AuthService, options.TenantService))
	engine.POST("/memory/secrets/disable", SecretDisableHandler(options.SecretStore, options.AuthService, options.TenantService))
	engine.POST("/memory/tokens/pat/create", PATCreateHandler(options.AuthService, options.AuditService))
	engine.POST("/memory/setup/bootstrap", SetupBootstrapHandler(defaultSetupCodes))
	engine.POST("/memory/tokens/pat/list", PATListHandler(options.AuthService))
	engine.POST("/memory/tokens/pat/revoke", PATRevokeHandler(options.AuthService, options.AuditService))
	engine.POST("/memory/tokens/adapter/create", AdapterTokenCreateHandler(options.AuthService, options.TenantService, options.AuditService))
	engine.POST("/memory/tokens/adapter/list", AdapterTokenListHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tokens/adapter/revoke", AdapterTokenRevokeHandler(options.AuthService, options.TenantService, options.AuditService))
	engine.POST("/memory/tenant/users/create", TenantUserCreateHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/users/list", TenantUserListHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/users/update-status", TenantUserUpdateStatusHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/orgs/create", TenantOrgCreateHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/orgs/list", TenantOrgListHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/orgs/edit", TenantOrgEditHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/orgs/delete", TenantOrgDeleteHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/projects/create", TenantProjectCreateHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/projects/list", TenantProjectListHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/projects/edit", TenantProjectEditHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/projects/delete", TenantProjectDeleteHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/memberships/add", TenantMembershipAddHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/memberships/list", TenantMembershipListHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/memberships/update-role", TenantMembershipUpdateRoleHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/memberships/remove", TenantMembershipRemoveHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/roles/list", TenantRolesListHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/tenant/roles/upsert", TenantRoleUpsertHandler(options.AuthService, options.TenantService))
	engine.POST("/memory/security/logs/list", SecurityLogListHandler(options.AuditService, options.AuthService))
	engine.POST("/memory/audit/list", AuditListHandler(options.AuditService, options.AuthService, options.TenantService))
	engine.POST("/memory/retrieval/access-log/list", RetrievalAccessLogListHandler(options.RetrievalAccessLog, options.AuthService, options.TenantService))
	engine.POST("/memory/qdrant/status", QdrantStatusHandler(options.QdrantStatusService, options.AuthService))
	engine.POST("/memory/stats/lifecycle", MemoryStatsHandler(options.MemoryStatsService, options.AuthService, options.TenantService))
	if options.AppEnv == "development" && options.EnableDevEndpoints {
		engine.POST("/dev/smoke/phase2", DevPhase2SmokeHandler())
		engine.POST("/dev/smoke/archive", DevArchiveSmokeHandler())
		engine.POST("/dev/smoke/rag", DevRAGSmokeHandler())
		engine.POST("/dev/smoke/hot-memory", DevHotMemorySmokeHandler())
	}
}

func validateProductionRouterOptions(options RouterOptions) error {
	if options.AppEnv != "production" {
		return nil
	}
	missing := []string{}
	if !options.AuthService.Configured() {
		missing = append(missing, "auth")
	}
	if !options.TenantService.Configured() {
		missing = append(missing, "tenant")
	}
	if !options.RetrievalService.Configured() {
		missing = append(missing, "retrieval")
	}
	if options.RetrievalAccessLog == nil {
		missing = append(missing, "retrieval_access_log")
	}
	if !options.HotMemoryService.Configured() {
		missing = append(missing, "hot_memory")
	}
	if !options.EventLogService.Configured() {
		missing = append(missing, "event_log")
	}
	if !options.AuditService.Configured() {
		missing = append(missing, "audit")
	}
	if !options.SecretStore.Configured() {
		missing = append(missing, "secret_store")
	}
	if !options.ArchiveService.Configured() {
		missing = append(missing, "archive")
	}
	if options.ArchiveQueue == nil {
		missing = append(missing, "archive_queue")
	}
	if options.ArchiveIndexQueue == nil {
		missing = append(missing, "archive_index_queue")
	}
	if !options.QdrantStatusService.Configured() {
		missing = append(missing, "qdrant_status")
	}
	if !options.MemoryStatsService.Configured() {
		missing = append(missing, "memory_stats")
	}
	if len(missing) > 0 {
		return fmt.Errorf("production router missing configured services: %s", strings.Join(missing, ", "))
	}
	return nil
}

func CORSMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		setCORSHeaders(c)
		c.Next(ctx)
	}
}

func CORSPreflightHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		setCORSHeaders(c)
		c.Status(consts.StatusNoContent)
	}
}

func setCORSHeaders(c *app.RequestContext) {
	origin := string(c.GetHeader("Origin"))
	if origin == "" {
		return
	}
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Vary", "Origin")
	c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
	c.Header("Access-Control-Max-Age", "600")
}

// HealthHandler 返回统一健康检查结果。
func HealthHandler(healthService health.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		report := healthService.Check(ctx)
		c.JSON(consts.StatusOK, report)
	}
}

func VersionHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, buildinfo.Current())
	}
}

func OpenAPIHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{
			"openapi": "3.0.3",
			"info": map[string]any{
				"title":   "Memory OS API",
				"version": "0.4.0-phase1",
			},
			"paths": map[string]any{
				"/healthz": map[string]any{
					"get": map[string]any{
						"summary": "Get service health status",
						"responses": map[string]any{
							"200": map[string]any{"description": "Health report"},
						},
					},
				},
				"/openapi.json": map[string]any{
					"get": map[string]any{
						"summary": "Get OpenAPI specification",
						"responses": map[string]any{
							"200": map[string]any{"description": "OpenAPI document"},
						},
					},
				},
				"/version": map[string]any{
					"get": map[string]any{
						"summary": "Get build metadata",
						"responses": map[string]any{
							"200": map[string]any{"description": "Build metadata"},
						},
					},
				},
				"/memory/setup/install.sh": map[string]any{
					"get": map[string]any{
						"summary": "Download the Memory OS setup installer script",
						"responses": map[string]any{
							"200": map[string]any{"description": "Shell installer script"},
						},
					},
				},
				"/memory/setup/bootstrap": map[string]any{
					"post": map[string]any{
						"summary": "Exchange a one-time setup code for local installer config",
						"responses": map[string]any{
							"200": map[string]any{"description": "One-time setup bootstrap config"},
						},
					},
				},
				"/memory/auth/login-password": map[string]any{
					"post": map[string]any{
						"summary": "Login with email and password, then issue a short-lived PAT",
						"responses": map[string]any{
							"200": map[string]any{"description": "Password login result"},
						},
					},
				},
				"/memory/search": map[string]any{
					"post": map[string]any{
						"summary": "Search unified hot memory and archive RAG",
						"responses": map[string]any{
							"200": map[string]any{"description": "Unified retrieval response"},
						},
					},
				},
				"/memory/turn-event": map[string]any{
					"post": map[string]any{
						"summary": "Ingest a sanitized TurnEvent v1 event",
						"responses": map[string]any{
							"200": map[string]any{"description": "TurnEvent ingest result"},
						},
					},
				},
				"/memory/qdrant/status": map[string]any{
					"post": map[string]any{
						"summary": "Get Qdrant collection and index job status",
						"responses": map[string]any{
							"200": map[string]any{"description": "Qdrant collection and index status"},
						},
					},
				},
				"/memory/stats/lifecycle": map[string]any{
					"post": map[string]any{
						"summary": "Get memory lifecycle statistics",
						"responses": map[string]any{
							"200": map[string]any{"description": "Memory lifecycle statistics"},
						},
					},
				},
				"/memory/archive/create": map[string]any{
					"post": map[string]any{
						"summary": "Create a Markdown archive",
						"responses": map[string]any{
							"200": map[string]any{"description": "Archive metadata"},
						},
					},
				},
				"/memory/archive/edit": map[string]any{
					"post": map[string]any{
						"summary": "Edit a Markdown archive and increment index generation",
						"responses": map[string]any{
							"200": map[string]any{"description": "Archive metadata"},
						},
					},
				},
				"/memory/archive/detail": map[string]any{
					"post": map[string]any{
						"summary": "Get archive metadata and Markdown content",
						"responses": map[string]any{
							"200": map[string]any{"description": "Archive detail"},
						},
					},
				},
				"/memory/archive/index-status": map[string]any{
					"post": map[string]any{
						"summary": "Get current Archive RAG index job, chunk and Qdrant point status",
						"responses": map[string]any{
							"200": map[string]any{"description": "Archive index status"},
						},
					},
				},
				"/memory/archive/index-retry": map[string]any{
					"post": map[string]any{
						"summary": "Retry failed Archive RAG index jobs for the current generation",
						"responses": map[string]any{
							"200": map[string]any{"description": "Archive index retry result"},
						},
					},
				},
				"/memory/archive/versions": map[string]any{
					"post": map[string]any{
						"summary": "List archive versions",
						"responses": map[string]any{
							"200": map[string]any{"description": "Archive versions"},
						},
					},
				},
				"/memory/archive/list": map[string]any{
					"post": map[string]any{
						"summary": "List active Markdown archives",
						"responses": map[string]any{
							"200": map[string]any{"description": "Archive list"},
						},
					},
				},
				"/memory/archive/delete": map[string]any{
					"post": map[string]any{
						"summary": "Soft delete a Markdown archive",
						"responses": map[string]any{
							"200": map[string]any{"description": "Archive metadata"},
						},
					},
				},
				"/memory/archive/reindex": map[string]any{
					"post": map[string]any{
						"summary": "Rebuild archive chunks and enqueue RAG indexing",
						"responses": map[string]any{
							"200": map[string]any{"description": "Archive reindex metadata"},
						},
					},
				},
				"/memory/hot-memory/create": map[string]any{
					"post": map[string]any{
						"summary": "Create or update a Hot Memory fact",
						"responses": map[string]any{
							"200": map[string]any{"description": "Hot Memory metadata"},
						},
					},
				},
				"/memory/hot-memory/list": map[string]any{
					"post": map[string]any{
						"summary": "List Hot Memory facts for the current project",
						"responses": map[string]any{
							"200": map[string]any{"description": "Hot Memory list"},
						},
					},
				},
				"/memory/hot-memory/edit": map[string]any{
					"post": map[string]any{
						"summary": "Edit a Hot Memory fact",
						"responses": map[string]any{
							"200": map[string]any{"description": "Hot Memory metadata"},
						},
					},
				},
				"/memory/hot-memory/promote": map[string]any{
					"post": map[string]any{
						"summary": "Promote a Hot Memory fact",
						"responses": map[string]any{
							"200": map[string]any{"description": "Hot Memory metadata"},
						},
					},
				},
				"/memory/hot-memory/demote": map[string]any{
					"post": map[string]any{
						"summary": "Demote a Hot Memory fact",
						"responses": map[string]any{
							"200": map[string]any{"description": "Hot Memory metadata"},
						},
					},
				},
				"/memory/hot-memory/mark-used": map[string]any{
					"post": map[string]any{
						"summary": "Mark a Hot Memory fact as used",
						"responses": map[string]any{
							"200": map[string]any{"description": "Hot Memory metadata"},
						},
					},
				},
				"/memory/hot-memory/delete": map[string]any{
					"post": map[string]any{
						"summary": "Soft delete a Hot Memory fact",
						"responses": map[string]any{
							"200": map[string]any{"description": "Hot Memory metadata"},
						},
					},
				},
				"/memory/hot-memory/pin": map[string]any{
					"post": map[string]any{
						"summary": "Pin a Hot Memory fact (manual override)",
						"responses": map[string]any{
							"200": map[string]any{"description": "Hot Memory metadata"},
						},
					},
				},
				"/memory/hot-memory/unpin": map[string]any{
					"post": map[string]any{
						"summary": "Unpin a Hot Memory fact",
						"responses": map[string]any{
							"200": map[string]any{"description": "Hot Memory metadata"},
						},
					},
				},
				"/memory/candidates/list": map[string]any{
					"post": map[string]any{
						"summary": "List candidate memories",
						"responses": map[string]any{
							"200": map[string]any{"description": "Candidate list"},
						},
					},
				},
				"/memory/candidates/accept": map[string]any{
					"post": map[string]any{
						"summary": "Accept a candidate memory",
						"responses": map[string]any{
							"200": map[string]any{"description": "Candidate metadata"},
						},
					},
				},
				"/memory/candidates/discard": map[string]any{
					"post": map[string]any{
						"summary": "Discard a candidate memory",
						"responses": map[string]any{
							"200": map[string]any{"description": "Candidate metadata"},
						},
					},
				},
				"/memory/candidates/compose": map[string]any{
					"post": map[string]any{
						"summary": "Compose topic candidates into a Markdown archive",
						"responses": map[string]any{
							"200": map[string]any{"description": "Compose result"},
						},
					},
				},
				"/memory/candidates/maintenance/run": map[string]any{
					"post": map[string]any{
						"summary": "Start AI maintenance (returns immediately, runs in background)",
						"responses": map[string]any{
							"200": map[string]any{"description": "Maintenance status DTO"},
						},
					},
				},
				"/memory/candidates/maintenance/status": map[string]any{
					"post": map[string]any{
						"summary": "Query maintenance task progress and status",
						"responses": map[string]any{
							"200": map[string]any{"description": "Maintenance status DTO"},
						},
					},
				},
				"/memory/topics/list": map[string]any{
					"post": map[string]any{
						"summary": "List topic memory states",
						"responses": map[string]any{
							"200": map[string]any{"description": "Topic state list"},
						},
					},
				},
				"/memory/secrets/create": map[string]any{
					"post": map[string]any{
						"summary": "Store client-encrypted secret; server never receives plaintext",
						"responses": map[string]any{
							"200": map[string]any{"description": "Secret metadata"},
						},
					},
				},
				"/memory/secrets/list": map[string]any{
					"post": map[string]any{
						"summary": "List secret metadata",
						"responses": map[string]any{
							"200": map[string]any{"description": "Secret metadata list"},
						},
					},
				},
				"/memory/secrets/ciphertext": map[string]any{
					"post": map[string]any{
						"summary": "Owner-only fetch of encrypted ciphertext blob (no server decryption)",
						"responses": map[string]any{
							"200": map[string]any{"description": "Secret metadata and ciphertext blob"},
						},
					},
				},
				"/memory/secrets/disable": map[string]any{
					"post": map[string]any{
						"summary": "Disable a secret",
						"responses": map[string]any{
							"200": map[string]any{"description": "Secret metadata"},
						},
					},
				},
				"/memory/tokens/pat/create": map[string]any{
					"post": map[string]any{
						"summary": "Create a PAT and return plaintext once",
						"responses": map[string]any{
							"200": map[string]any{"description": "PAT token and metadata"},
						},
					},
				},
				"/memory/tokens/pat/list": map[string]any{
					"post": map[string]any{
						"summary": "List PAT metadata",
						"responses": map[string]any{
							"200": map[string]any{"description": "PAT metadata list"},
						},
					},
				},
				"/memory/tokens/pat/revoke": map[string]any{
					"post": map[string]any{
						"summary": "Revoke a PAT",
						"responses": map[string]any{
							"200": map[string]any{"description": "PAT metadata"},
						},
					},
				},
				"/memory/tokens/adapter/create": map[string]any{
					"post": map[string]any{
						"summary": "Create an Adapter Token and return plaintext once",
						"responses": map[string]any{
							"200": map[string]any{"description": "Adapter token and metadata"},
						},
					},
				},
				"/memory/tokens/adapter/list": map[string]any{
					"post": map[string]any{
						"summary": "List Adapter Token metadata",
						"responses": map[string]any{
							"200": map[string]any{"description": "Adapter token metadata list"},
						},
					},
				},
				"/memory/tokens/adapter/revoke": map[string]any{
					"post": map[string]any{
						"summary": "Revoke an Adapter Token",
						"responses": map[string]any{
							"200": map[string]any{"description": "Adapter token metadata"},
						},
					},
				},
				"/memory/tenant/users/create": map[string]any{
					"post": map[string]any{
						"summary": "Create a tenant user",
						"responses": map[string]any{
							"200": map[string]any{"description": "User metadata"},
						},
					},
				},
				"/memory/tenant/users/list": map[string]any{
					"post": map[string]any{
						"summary": "List tenant users metadata",
						"responses": map[string]any{
							"200": map[string]any{"description": "User metadata list"},
						},
					},
				},
				"/memory/tenant/users/update-status": map[string]any{
					"post": map[string]any{
						"summary": "Update a tenant user status",
						"responses": map[string]any{
							"200": map[string]any{"description": "User metadata"},
						},
					},
				},
				"/memory/tenant/orgs/create": map[string]any{
					"post": map[string]any{
						"summary": "Create an organization and owner membership",
						"responses": map[string]any{
							"200": map[string]any{"description": "Organization metadata"},
						},
					},
				},
				"/memory/tenant/orgs/list": map[string]any{
					"post": map[string]any{
						"summary": "List organizations for current user",
						"responses": map[string]any{
							"200": map[string]any{"description": "Organization list"},
						},
					},
				},
				"/memory/tenant/orgs/edit": map[string]any{
					"post": map[string]any{
						"summary": "Edit an active organization",
						"responses": map[string]any{
							"200": map[string]any{"description": "Organization metadata"},
						},
					},
				},
				"/memory/tenant/orgs/delete": map[string]any{
					"post": map[string]any{
						"summary": "Soft delete an organization",
						"responses": map[string]any{
							"200": map[string]any{"description": "Organization metadata"},
						},
					},
				},
				"/memory/tenant/projects/create": map[string]any{
					"post": map[string]any{
						"summary": "Create a project",
						"responses": map[string]any{
							"200": map[string]any{"description": "Project metadata"},
						},
					},
				},
				"/memory/tenant/projects/list": map[string]any{
					"post": map[string]any{
						"summary": "List projects for current user and organization",
						"responses": map[string]any{
							"200": map[string]any{"description": "Project list"},
						},
					},
				},
				"/memory/tenant/projects/edit": map[string]any{
					"post": map[string]any{
						"summary": "Edit an active project",
						"responses": map[string]any{
							"200": map[string]any{"description": "Project metadata"},
						},
					},
				},
				"/memory/tenant/projects/delete": map[string]any{
					"post": map[string]any{
						"summary": "Soft delete a project",
						"responses": map[string]any{
							"200": map[string]any{"description": "Project metadata"},
						},
					},
				},
				"/memory/tenant/memberships/add": map[string]any{
					"post": map[string]any{
						"summary": "Add project membership",
						"responses": map[string]any{
							"200": map[string]any{"description": "Membership metadata"},
						},
					},
				},
				"/memory/tenant/memberships/list": map[string]any{
					"post": map[string]any{
						"summary": "List memberships",
						"responses": map[string]any{
							"200": map[string]any{"description": "Membership list"},
						},
					},
				},
				"/memory/tenant/memberships/update-role": map[string]any{
					"post": map[string]any{
						"summary": "Update a membership role",
						"responses": map[string]any{
							"200": map[string]any{"description": "Membership metadata"},
						},
					},
				},
				"/memory/tenant/memberships/remove": map[string]any{
					"post": map[string]any{
						"summary": "Disable a membership without deleting audit history",
						"responses": map[string]any{
							"200": map[string]any{"description": "Membership metadata"},
						},
					},
				},
				"/memory/tenant/roles/list": map[string]any{
					"post": map[string]any{
						"summary": "List role definitions and permission labels for a project",
						"responses": map[string]any{
							"200": map[string]any{"description": "Role definition list"},
						},
					},
				},
				"/memory/tenant/roles/upsert": map[string]any{
					"post": map[string]any{
						"summary": "Create or update a role definition",
						"responses": map[string]any{
							"200": map[string]any{"description": "Role definition"},
						},
					},
				},
				"/memory/security/logs/list": map[string]any{
					"post": map[string]any{
						"summary": "List actor-scoped security audit logs",
						"responses": map[string]any{
							"200": map[string]any{"description": "Security audit log list"},
						},
					},
				},
				"/memory/audit/list": map[string]any{
					"post": map[string]any{
						"summary": "List audit logs for a project",
						"responses": map[string]any{
							"200": map[string]any{"description": "Audit log list"},
						},
					},
				},
				"/memory/retrieval/access-log/list": map[string]any{
					"post": map[string]any{
						"summary": "List retrieval access log entries for a project",
						"responses": map[string]any{
							"200": map[string]any{"description": "Retrieval access log list"},
						},
					},
				},
			},
		})
	}
}

type memoryStatsRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
}

func MemoryStatsHandler(service memorystats.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "memory_stats_not_configured"})
			return
		}
		var request memoryStatsRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_memory_stats_request"})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "", "memory_stats_forbidden")
		if !ok {
			return
		}
		snapshot, err := service.Snapshot(ctx, memorystats.Filter{
			UserID:           permissions.UserID,
			OrgID:            permissions.OrgID,
			ProjectID:        permissions.ProjectID,
			PermissionLabels: permissions.PermissionLabels,
		})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "memory_stats_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, snapshot)
	}
}

func DevPhase2SmokeHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		tenantService := tenant.NewService(tenant.NewMemoryRepository())
		user, err := tenantService.CreateUser("alice@example.com", "Alice")
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		org, err := tenantService.CreateOrg("Org Alpha", "org-alpha")
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		project, err := tenantService.CreateProject(org.ID, "Project Alpha", "project-alpha")
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		if err := tenantService.AddMembership(user.ID, org.ID, project.ID, tenant.RoleOwner); err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		if _, err := tenantService.PermissionContext(user.ID, org.ID, project.ID, "codex"); err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}

		authService := auth.NewService(auth.NewMemoryRepository())
		if err := authService.SetPassword(user.ID, "password-test-redacted"); err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		if _, err := authService.LoginPassword(user.ID, "password-test-redacted"); err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		pat, _, err := authService.CreatePAT(user.ID, "smoke", []string{"memory:read"}, time.Hour)
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		if _, err := authService.ValidatePAT(pat, time.Now()); err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		adapter, _, err := authService.CreateAdapterToken(auth.AdapterTokenRequest{UserID: user.ID, OrgID: org.ID, ProjectID: project.ID, AgentID: "codex", Scopes: []string{"turn_event:write"}, TTL: time.Hour})
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		if _, err := authService.ValidateAdapterToken(adapter, auth.AdapterTokenBinding{OrgID: org.ID, ProjectID: project.ID, AgentID: "codex"}, time.Now()); err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}

		// 服务端零明文：模拟本机 MCP 已在本地加密后上传密文 blob，
		// 服务端只落库/回传密文，全程不接触明文，也没有任何解密能力。
		store := secret.NewStore(secret.NewMemoryRepository())
		blob := secret.EncryptedBlob{
			Algorithm:      "AES-256-GCM",
			DeviceKeyID:    "dev-smoke",
			KeyFingerprint: "fp-smoke",
			Nonce:          []byte("smoke-nonce-"),
			Ciphertext:     []byte("smoke-ciphertext-blob"),
		}
		meta, err := store.CreateEncrypted(secret.CreateEncryptedRequest{OwnerUserID: user.ID, OrgID: org.ID, ProjectID: project.ID, Name: "smoke", Purpose: "phase2"}, blob)
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		// owner 可取回密文；非 owner 被拒。
		_, gotBlob, err := store.GetCiphertext(meta.SecretRef, user.ID)
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		if _, _, err := store.GetCiphertext(meta.SecretRef, "someone-else"); !errors.Is(err, secret.ErrForbidden) {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": "secret non-owner access not rejected"})
			return
		}
		if string(gotBlob.Ciphertext) != string(blob.Ciphertext) {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": "secret ciphertext round-trip mismatch"})
			return
		}

		c.JSON(consts.StatusOK, map[string]any{
			"status":     "ok",
			"user_id":    user.ID,
			"org_id":     org.ID,
			"project_id": project.ID,
			"secret_ref": meta.SecretRef,
		})
	}
}

type turnEventRequest struct {
	RequestID string             `json:"request_id"`
	Workspace workspace.Identity `json:"workspace"`
	Event     eventlog.TurnEvent `json:"event"`
}

func TurnEventHandler(service eventlog.Service, authService auth.Service, tenantService tenant.Service, archiveQueue archiveEnqueuer, candidateQueue candidateEnqueuer, legacyArchive bool) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request turnEventRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_turn_event_request"})
			return
		}
		actorUserID := request.Event.Actor.UserID
		permissions := tenant.PermissionContext{}
		if authService.Configured() {
			token := bearerToken(c)
			if token == "" {
				c.JSON(consts.StatusUnauthorized, map[string]string{"error": "adapter_token_required"})
				return
			}
			if patRecord, err := authService.ValidatePAT(token, time.Now()); err == nil {
				if !patScopeAllows(patRecord.Scopes, "memory:write") {
					c.JSON(consts.StatusForbidden, map[string]string{"error": "turn_event_forbidden"})
					return
				}
				actorUserID = patRecord.SubjectID
				request.Event.Actor.UserID = patRecord.SubjectID
				resolved, ok := turnEventPATPermissions(c, tenantService, request.Event.Actor, request.Workspace)
				if !ok {
					return
				}
				permissions = resolved
				request.Event.Actor.UserID = permissions.UserID
				request.Event.Actor.OrgID = permissions.OrgID
				request.Event.Actor.ProjectID = permissions.ProjectID
				request.Event.Actor.AgentID = permissions.AgentID
			} else {
				record, err := authService.ValidateAdapterToken(token, auth.AdapterTokenBinding{OrgID: request.Event.Actor.OrgID, ProjectID: request.Event.Actor.ProjectID, AgentID: request.Event.Actor.AgentID}, time.Now())
				if err != nil {
					c.JSON(consts.StatusForbidden, map[string]string{"error": "adapter_token_forbidden"})
					return
				}
				actorUserID = record.UserID
				request.Event.Actor.UserID = record.UserID
			}
		}
		if permissions.UserID == "" {
			permissions = tenant.PermissionContext{
				UserID:           actorUserID,
				OrgID:            request.Event.Actor.OrgID,
				ProjectID:        request.Event.Actor.ProjectID,
				AgentID:          request.Event.Actor.AgentID,
				PermissionLabels: []string{"project:" + request.Event.Actor.ProjectID + ":write"},
			}
		}
		result, err := service.Ingest(request.Event, request.RequestID, permissions)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "turn_event_rejected", "message": err.Error()})
			return
		}
		if result.Deduped {
			c.JSON(consts.StatusOK, result)
			return
		}
		// LEGACY_TURN_EVENT_ARCHIVE=true:保留旧 per-turn archive 自动入库(兼容)
		if legacyArchive && archiveQueue != nil {
			if err := archiveQueue.Enqueue(ctx, archiveJobFromTurnEvent(request.RequestID, result.Event)); err != nil {
				c.JSON(consts.StatusInternalServerError, map[string]string{"error": "archive_job_enqueue_failed"})
				return
			}
			c.JSON(consts.StatusOK, result)
			return
		}
		// 默认链路:对触发事件类型 enqueue 候选提炼任务(Phase 4 worker 消费)。
		// source_key 缺失则跳过,不写无归属候选,event 已入库 eventlog。
		if candidateQueue != nil && shouldEnqueueCandidate(result.Event.Type) {
			if job, ok := candidateJobFromTurnEvent(request, result.Event); ok {
				if err := candidateQueue.Enqueue(ctx, job); err != nil {
					c.JSON(consts.StatusInternalServerError, map[string]string{"error": "candidate_job_enqueue_failed"})
					return
				}
			}
		}
		c.JSON(consts.StatusOK, result)
	}
}

func turnEventPATPermissions(c *app.RequestContext, tenantService tenant.Service, actor eventlog.Actor, identity workspace.Identity) (tenant.PermissionContext, bool) {
	if actor.ProjectID == "" && strings.TrimSpace(identity.GitRemote) != "" {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return tenant.PermissionContext{}, false
		}
		permissions, err := tenantService.EnsureWorkspaceProject(actor.UserID, actor.AgentID, identity)
		if err != nil {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "workspace_project_forbidden", "message": err.Error()})
			return tenant.PermissionContext{}, false
		}
		return permissions, true
	}
	if tenantService.Configured() {
		permissions, err := tenantService.PermissionContext(actor.UserID, actor.OrgID, actor.ProjectID, actor.AgentID)
		if err != nil {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "turn_event_forbidden"})
			return tenant.PermissionContext{}, false
		}
		return permissions, true
	}
	return tenant.PermissionContext{
		UserID:           actor.UserID,
		OrgID:            actor.OrgID,
		ProjectID:        actor.ProjectID,
		AgentID:          actor.AgentID,
		PermissionLabels: []string{"project:" + actor.ProjectID + ":write"},
	}, true
}

func archiveJobFromTurnEvent(requestID string, event eventlog.TurnEvent) jobs.ArchiveJob {
	title := "Turn " + event.TurnID
	if title == "Turn " {
		title = "TurnEvent " + event.EventID
	}
	return jobs.ArchiveJob{
		RequestID: "archive_" + requestID,
		ArchiveID: "archive_" + event.EventID,
		Title:     title,
		UserID:    event.Actor.UserID,
		OrgID:     event.Actor.OrgID,
		ProjectID: event.Actor.ProjectID,
		CreatedAt: event.CreatedAt,
		Events:    []eventlog.TurnEvent{event},
	}
}

// shouldEnqueueCandidate 判断事件类型是否触发候选提炼。
// 只对真正含用户可读结论的事件提炼候选,避免状态事件反复触发 LLM。
func shouldEnqueueCandidate(eventType eventlog.EventType) bool {
	switch eventType {
	case eventlog.EventAssistantFinal, eventlog.EventManualArchive:
		return true
	}
	return false
}

// candidateJobFromTurnEvent 构造候选提炼任务。
// source_key 优先取 workspace 显式值,其次从 git_remote 解析;缺失则返回 ok=false,不写无归属候选(硬规则 4)。
func candidateJobFromTurnEvent(request turnEventRequest, event eventlog.TurnEvent) (candidatememory.Job, bool) {
	identity := request.Workspace
	if identity.SourceKey == "" {
		if resolved, err := workspace.Resolve(identity); err == nil {
			identity = resolved
		}
	}
	if identity.SourceKey == "" {
		return candidatememory.Job{}, false
	}
	return candidatememory.Job{
		IdempotencyKey: fmt.Sprintf("candidate:%s:%s:%s:extract", event.Actor.ProjectID, identity.SourceKey, event.EventID),
		OrgID:          event.Actor.OrgID,
		ProjectID:      event.Actor.ProjectID,
		SourceKey:      identity.SourceKey,
		SourceEventID:  event.EventID,
	}, true
}

func bearerToken(c *app.RequestContext) string {
	value := string(c.GetHeader("Authorization"))
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}

func authorizeArchiveScope(c *app.RequestContext, authService auth.Service, tenantService tenant.Service, orgID, projectID, patScope, permissionLabel string) (tenant.PermissionContext, bool) {
	return authorizeProjectScope(c, authService, tenantService, orgID, projectID, patScope, permissionLabel, "archive_forbidden")
}

func authorizeSecretScope(c *app.RequestContext, authService auth.Service, tenantService tenant.Service, orgID, projectID, patScope, permissionLabel string) (tenant.PermissionContext, bool) {
	return authorizeProjectScope(c, authService, tenantService, orgID, projectID, patScope, permissionLabel, "secret_forbidden")
}

func authorizeProjectScope(c *app.RequestContext, authService auth.Service, tenantService tenant.Service, orgID, projectID, patScope, permissionLabel, forbiddenError string) (tenant.PermissionContext, bool) {
	if !authService.Configured() && !tenantService.Configured() {
		return tenant.PermissionContext{OrgID: orgID, ProjectID: projectID, PermissionLabels: []string{"project:" + projectID + ":read"}}, true
	}
	if !authService.Configured() || !tenantService.Configured() {
		c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "auth_not_configured"})
		return tenant.PermissionContext{}, false
	}
	token := bearerToken(c)
	if token == "" {
		c.JSON(consts.StatusUnauthorized, map[string]string{"error": "pat_required"})
		return tenant.PermissionContext{}, false
	}
	record, err := authService.ValidatePAT(token, time.Now())
	if err != nil {
		c.JSON(consts.StatusUnauthorized, map[string]string{"error": "invalid_pat"})
		return tenant.PermissionContext{}, false
	}
	if !patScopeAllows(record.Scopes, patScope) {
		c.JSON(consts.StatusForbidden, map[string]string{"error": forbiddenError})
		return tenant.PermissionContext{}, false
	}
	permissions, err := tenantService.PermissionContext(record.SubjectID, orgID, projectID, "web")
	if err != nil {
		c.JSON(consts.StatusForbidden, map[string]string{"error": forbiddenError})
		return tenant.PermissionContext{}, false
	}
	if permissionLabel != "" && !hasString(permissions.PermissionLabels, permissionLabel) {
		c.JSON(consts.StatusForbidden, map[string]string{"error": forbiddenError})
		return tenant.PermissionContext{}, false
	}
	return permissions, true
}

func authorizePAT(c *app.RequestContext, authService auth.Service, patScope, forbiddenError string) (auth.PATRecord, bool) {
	if !authService.Configured() {
		c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "auth_not_configured"})
		return auth.PATRecord{}, false
	}
	token := bearerToken(c)
	if token == "" {
		c.JSON(consts.StatusUnauthorized, map[string]string{"error": "pat_required"})
		return auth.PATRecord{}, false
	}
	record, err := authService.ValidatePAT(token, time.Now())
	if err != nil {
		c.JSON(consts.StatusUnauthorized, map[string]string{"error": "invalid_pat"})
		return auth.PATRecord{}, false
	}
	if !patScopeAllows(record.Scopes, patScope) {
		c.JSON(consts.StatusForbidden, map[string]string{"error": forbiddenError})
		return auth.PATRecord{}, false
	}
	return record, true
}

func patScopeAllows(scopes []string, required string) bool {
	if required == "" {
		return true
	}
	if hasString(scopes, required) {
		return true
	}
	return required == "memory:read" && hasString(scopes, "memory:write")
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type memorySearchRequest struct {
	RequestID              string             `json:"request_id"`
	Query                  string             `json:"query"`
	Actor                  actorDTO           `json:"actor"`
	Workspace              workspace.Identity `json:"workspace"`
	Scope                  string             `json:"scope"`
	Visibility             string             `json:"visibility"`
	PermissionLabels       []string           `json:"permission_labels"`
	ArchiveIndexGeneration int                `json:"archive_index_generation"`
	MaxContextBytes        int                `json:"max_context_bytes"`
}

type actorDTO struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
}

type passwordLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func PasswordLoginHandler(authService auth.Service, tenantService tenant.Service, auditService audit.Service) app.HandlerFunc {
	const loginPATTTL = 12 * time.Hour

	return func(ctx context.Context, c *app.RequestContext) {
		if !authService.Configured() || !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "auth_not_configured"})
			return
		}

		var request passwordLoginRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		if strings.TrimSpace(request.Email) == "" || strings.TrimSpace(request.Password) == "" {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "email_and_password_required"})
			return
		}

		user, err := tenantService.FindUserByEmail(request.Email)
		if err != nil {
			c.JSON(consts.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
			return
		}
		if user.Status != "active" {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "user_disabled"})
			return
		}
		if _, err := authService.LoginPassword(user.ID, request.Password); err != nil {
			c.JSON(consts.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
			return
		}

		tokenName := "password-login-" + time.Now().UTC().Format("20060102T150405Z")
		token, tokenRecord, err := authService.CreatePAT(user.ID, tokenName, []string{"memory:write"}, loginPATTTL)
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"error": "password_login_token_issue_failed"})
			return
		}
		recordAudit(auditService, audit.Log{
			ActorUserID:  user.ID,
			Action:       "auth.password_login",
			ResourceType: "user",
			ResourceID:   user.ID,
			RequestID:    auditRequestID("auth.password_login", user.ID),
			Result:       "ok",
			Metadata: map[string]string{
				"credential":   "password_login_pat",
				"token_id":     tokenRecord.ID,
				"token_prefix": tokenRecord.TokenPrefix,
				"token_name":   tokenRecord.Name,
				"ttl_seconds":  fmt.Sprintf("%d", int(loginPATTTL/time.Second)),
			},
		})

		c.JSON(consts.StatusOK, map[string]any{
			"token": token,
			"user": map[string]any{
				"user_id":      user.ID,
				"email":        user.Email,
				"display_name": user.DisplayName,
				"status":       user.Status,
			},
			"expires_in_seconds": int(loginPATTTL / time.Second),
		})
	}
}

func MemorySearchHandler(authService auth.Service, tenantService tenant.Service, retrievalService retrieval.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request memorySearchRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_memory_search_request"})
			return
		}
		if authService.Configured() {
			record, ok := authorizePAT(c, authService, "memory:read", "memory_search_forbidden")
			if !ok {
				return
			}
			request.Actor.UserID = record.SubjectID
		}
		permissionLabels := request.PermissionLabels
		if tenantService.Configured() {
			var permissions tenant.PermissionContext
			var err error
			if request.Actor.ProjectID == "" && strings.TrimSpace(request.Workspace.GitRemote) != "" {
				permissions, err = tenantService.EnsureWorkspaceProject(request.Actor.UserID, request.Actor.AgentID, request.Workspace)
				request.Actor.OrgID = permissions.OrgID
				request.Actor.ProjectID = permissions.ProjectID
				request.Actor.AgentID = permissions.AgentID
			} else {
				permissions, err = tenantService.PermissionContext(request.Actor.UserID, request.Actor.OrgID, request.Actor.ProjectID, request.Actor.AgentID)
			}
			if err != nil {
				c.JSON(consts.StatusForbidden, map[string]string{"error": "memory_search_forbidden"})
				return
			}
			permissionLabels = permissions.PermissionLabels
		}
		service := retrievalService
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "retrieval_not_configured"})
			return
		}
		response, err := service.Search(retrieval.SearchRequest{
			RequestID:              request.RequestID,
			Query:                  request.Query,
			Actor:                  retrieval.Actor{UserID: request.Actor.UserID, OrgID: request.Actor.OrgID, ProjectID: request.Actor.ProjectID, AgentID: request.Actor.AgentID},
			Scope:                  hotmemory.Scope(request.Scope),
			Visibility:             request.Visibility,
			PermissionLabels:       permissionLabels,
			ArchiveIndexGeneration: request.ArchiveIndexGeneration,
			MaxContextBytes:        request.MaxContextBytes,
		})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "memory_search_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, response)
	}
}

func QdrantStatusHandler(statusService qdrant.StatusService, authService auth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !statusService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "qdrant_status_not_configured"})
			return
		}
		if authService.Configured() {
			if _, ok := authorizePAT(c, authService, "memory:read", "qdrant_status_forbidden"); !ok {
				return
			}
		}
		snapshot, err := statusService.Snapshot(ctx)
		if err != nil {
			c.JSON(consts.StatusBadGateway, map[string]string{"error": "qdrant_status_unavailable", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, qdrantStatusResponse(snapshot))
	}
}

type archiveCreateRequest struct {
	RequestID string               `json:"request_id"`
	ArchiveID string               `json:"archive_id"`
	Title     string               `json:"title"`
	UserID    string               `json:"user_id"`
	OrgID     string               `json:"org_id"`
	ProjectID string               `json:"project_id"`
	CreatedAt time.Time            `json:"created_at"`
	Events    []eventlog.TurnEvent `json:"events"`
}

type archiveEditRequest struct {
	RequestID   string `json:"request_id"`
	ArchiveID   string `json:"archive_id"`
	ActorUserID string `json:"actor_user_id"`
	Reason      string `json:"reason"`
	Content     string `json:"content"`
}

type archiveIDRequest struct {
	ArchiveID string `json:"archive_id"`
}

type archiveListRequest struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
	Limit     int    `json:"limit"`
}

type archiveDeleteRequest struct {
	RequestID   string `json:"request_id"`
	ArchiveID   string `json:"archive_id"`
	ActorUserID string `json:"actor_user_id"`
	Reason      string `json:"reason"`
}

type archiveReindexRequest struct {
	RequestID string `json:"request_id"`
	ArchiveID string `json:"archive_id"`
	Reason    string `json:"reason"`
}

type encryptedBlobRequest struct {
	Algorithm      string `json:"algorithm"`
	DeviceKeyID    string `json:"device_key_id"`
	KeyFingerprint string `json:"key_fingerprint"`
	NonceB64       string `json:"nonce_b64"`
	CiphertextB64  string `json:"ciphertext_b64"`
}

type secretCreateRequest struct {
	UserID    string               `json:"user_id"`
	OrgID     string               `json:"org_id"`
	ProjectID string               `json:"project_id"`
	Name      string               `json:"name"`
	EnvName   string               `json:"env_name"`
	Site      string               `json:"site"`
	Purpose   string               `json:"purpose"`
	ExpiresAt string               `json:"expires_at"`
	Plaintext string               `json:"plaintext"`
	Encrypted encryptedBlobRequest `json:"encrypted"`
}

type secretListRequest struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
	Limit     int    `json:"limit"`
}

type secretCiphertextRequest struct {
	SecretRef string `json:"secret_ref"`
}

type secretDisableRequest struct {
	SecretRef string `json:"secret_ref"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
}

type hotMemoryCreateRequest struct {
	UserID     string   `json:"user_id"`
	OrgID      string   `json:"org_id"`
	ProjectID  string   `json:"project_id"`
	AgentID    string   `json:"agent_id"`
	Scope      string   `json:"scope"`
	Visibility string   `json:"visibility"`
	Labels     []string `json:"permission_labels"`
	Fact       string   `json:"fact"`
	SourceType string   `json:"source_type"`
	SourceRef  string   `json:"source_ref"`
	Confidence float64  `json:"confidence"`
}

type hotMemoryListRequest struct {
	OrgID      string `json:"org_id"`
	ProjectID  string `json:"project_id"`
	AgentID    string `json:"agent_id"`
	Scope      string `json:"scope"`
	Visibility string `json:"visibility"`
	Status     string `json:"status"`
	Limit      int    `json:"limit"`
}

type hotMemoryActionRequest struct {
	MemoryID string `json:"memory_id"`
}

type hotMemoryEditRequest struct {
	MemoryID   string  `json:"memory_id"`
	Fact       string  `json:"fact"`
	Confidence float64 `json:"confidence"`
}

func HotMemoryCreateHandler(service hotmemory.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "hot_memory_not_configured"})
			return
		}
		var request hotMemoryCreateRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_hot_memory_create_request"})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:write", "project:"+request.ProjectID+":write", "hot_memory_forbidden")
		if !ok {
			return
		}
		userID := request.UserID
		if authService.Configured() {
			userID = permissions.UserID
		}
		labels := request.Labels
		if len(labels) == 0 {
			labels = permissions.PermissionLabels
		}
		scope := hotmemory.Scope(request.Scope)
		if scope == "" {
			scope = hotmemory.ScopeProject
		}
		visibility := request.Visibility
		if visibility == "" {
			visibility = "project"
		}
		sourceType := hotmemory.SourceType(request.SourceType)
		if sourceType == "" {
			sourceType = hotmemory.SourceArchive
		}
		sourceRef := request.SourceRef
		if sourceRef == "" {
			sourceRef = "manual_hot_memory"
		}
		confidence := request.Confidence
		if confidence <= 0 {
			confidence = 0.7
		}
		memory, err := service.Upsert(hotmemory.UpsertRequest{OrgID: request.OrgID, ProjectID: request.ProjectID, UserID: userID, AgentID: request.AgentID, Scope: scope, Visibility: visibility, PermissionLabels: labels, Fact: request.Fact, SourceType: sourceType, SourceRef: sourceRef, Confidence: confidence})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "hot_memory_create_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, hotMemoryResponse(memory))
	}
}

func HotMemoryListHandler(service hotmemory.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "hot_memory_not_configured"})
			return
		}
		var request hotMemoryListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_hot_memory_list_request"})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "", "hot_memory_forbidden")
		if !ok {
			return
		}
		scope := hotmemory.Scope(request.Scope)
		if scope == "" {
			scope = hotmemory.ScopeProject
		}
		visibility := request.Visibility
		if visibility == "" {
			visibility = "project"
		}
		filter, err := hotmemory.BuildFilter(hotmemory.FilterContext{OrgID: request.OrgID, ProjectID: request.ProjectID, UserID: permissions.UserID, AgentID: request.AgentID, Scope: scope, Visibility: visibility, PermissionLabels: permissions.PermissionLabels})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "hot_memory_list_rejected", "message": err.Error()})
			return
		}
		items, err := service.List(filter.Must)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "hot_memory_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"memories": hotMemoryListResponse(filterHotMemories(items, request.Status, request.Limit))})
	}
}

func HotMemoryEditHandler(service hotmemory.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "hot_memory_not_configured"})
			return
		}
		var request hotMemoryEditRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_hot_memory_edit_request"})
			return
		}
		memory, err := service.Get(request.MemoryID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "hot_memory_edit_rejected", "message": err.Error()})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, memory.OrgID, memory.ProjectID, "memory:write", "project:"+memory.ProjectID+":write", "hot_memory_forbidden")
		if !ok {
			return
		}
		if authService.Configured() && memory.UserID != permissions.UserID {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "hot_memory_forbidden"})
			return
		}
		updated, err := service.Edit(hotmemory.EditRequest{MemoryID: request.MemoryID, Fact: request.Fact, Confidence: request.Confidence})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "hot_memory_edit_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, hotMemoryResponse(updated))
	}
}

func HotMemoryPromoteHandler(service hotmemory.Service, authService auth.Service, tenantService tenant.Service, auditService audit.Service) app.HandlerFunc {
	return hotMemoryActionHandler(service, authService, tenantService, auditService, "memory:write", "project:%s:write", "hot_memory.promote", func(memoryID string) (hotmemory.Memory, error) {
		return service.Promote(memoryID)
	})
}

func HotMemoryDemoteHandler(service hotmemory.Service, authService auth.Service, tenantService tenant.Service, auditService audit.Service) app.HandlerFunc {
	return hotMemoryActionHandler(service, authService, tenantService, auditService, "memory:write", "project:%s:write", "hot_memory.demote", func(memoryID string) (hotmemory.Memory, error) {
		return service.Demote(memoryID)
	})
}

func HotMemoryMarkUsedHandler(service hotmemory.Service, authService auth.Service, tenantService tenant.Service, auditService audit.Service) app.HandlerFunc {
	return hotMemoryActionHandler(service, authService, tenantService, auditService, "memory:write", "project:%s:write", "hot_memory.mark_used", func(memoryID string) (hotmemory.Memory, error) {
		return service.MarkUsed(memoryID)
	})
}

func HotMemoryDeleteHandler(service hotmemory.Service, authService auth.Service, tenantService tenant.Service, auditService audit.Service) app.HandlerFunc {
	return hotMemoryActionHandler(service, authService, tenantService, auditService, "memory:write", "project:%s:write", "hot_memory.delete", func(memoryID string) (hotmemory.Memory, error) {
		if err := service.Delete(memoryID); err != nil {
			return hotmemory.Memory{}, err
		}
		return service.Get(memoryID)
	})
}

func HotMemoryPinHandler(service hotmemory.Service, authService auth.Service, tenantService tenant.Service, auditService audit.Service) app.HandlerFunc {
	return hotMemoryActionHandler(service, authService, tenantService, auditService, "memory:write", "project:%s:write", "hot_memory.pin", func(memoryID string) (hotmemory.Memory, error) {
		return service.SetPinned(memoryID, true)
	})
}

func HotMemoryUnpinHandler(service hotmemory.Service, authService auth.Service, tenantService tenant.Service, auditService audit.Service) app.HandlerFunc {
	return hotMemoryActionHandler(service, authService, tenantService, auditService, "memory:write", "project:%s:write", "hot_memory.unpin", func(memoryID string) (hotmemory.Memory, error) {
		return service.SetPinned(memoryID, false)
	})
}

func hotMemoryActionHandler(service hotmemory.Service, authService auth.Service, tenantService tenant.Service, auditService audit.Service, patScope, permissionLabelFormat, auditAction string, action func(string) (hotmemory.Memory, error)) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "hot_memory_not_configured"})
			return
		}
		var request hotMemoryActionRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_hot_memory_action_request"})
			return
		}
		memory, err := service.Get(request.MemoryID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "hot_memory_action_rejected", "message": err.Error()})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, memory.OrgID, memory.ProjectID, patScope, fmt.Sprintf(permissionLabelFormat, memory.ProjectID), "hot_memory_forbidden")
		if !ok {
			return
		}
		if authService.Configured() && memory.UserID != permissions.UserID {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "hot_memory_forbidden"})
			return
		}
		updated, err := action(request.MemoryID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "hot_memory_action_rejected", "message": err.Error()})
			return
		}
		recordAudit(auditService, audit.Log{
			ActorUserID:  permissions.UserID,
			OrgID:        memory.OrgID,
			ProjectID:    memory.ProjectID,
			Action:       auditAction,
			ResourceType: "hot_memory",
			ResourceID:   updated.MemoryID,
			RequestID:    auditRequestID(auditAction, updated.MemoryID),
			Result:       "ok",
			Metadata: map[string]string{
				"used_count":     fmt.Sprintf("%d", updated.UsedCount),
				"returned_count": fmt.Sprintf("%d", updated.ReturnedCount),
				"access_count":   fmt.Sprintf("%d", updated.AccessCount),
			},
		})
		c.JSON(consts.StatusOK, hotMemoryResponse(updated))
	}
}

// --- 候选记忆 API (Phase 6) ---

type candidateListRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	SourceKey string `json:"source_key"`
	ThreadID  string `json:"thread_id"`
	Status    string `json:"status"`
	RiskLevel string `json:"risk_level"`
	Limit     int    `json:"limit"`
}

type candidateActionRequest struct {
	CandidateID string `json:"candidate_id"`
	OrgID       string `json:"org_id"`
}

type topicComposeRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	SourceKey string `json:"source_key"`
	ThreadID  string `json:"thread_id"`
	Force     bool   `json:"force"`
}

type topicListRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	SourceKey string `json:"source_key"`
	Limit     int    `json:"limit"`
}

func CandidateListHandler(service *candidatememory.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request candidateListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_candidate_list_request"})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "", "candidate_forbidden")
		if !ok {
			return
		}
		filter := candidatememory.ListFilter{
			OrgID:     permissions.OrgID,
			ProjectID: permissions.ProjectID,
			SourceKey: request.SourceKey,
			ThreadID:  request.ThreadID,
			Status:    candidatememory.Status(request.Status),
			RiskLevel: candidatememory.RiskLevel(request.RiskLevel),
			Limit:     request.Limit,
		}
		candidates, err := service.List(ctx, filter)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "candidate_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"candidates": candidateListResponse(candidates)})
	}
}

func CandidateAcceptHandler(service *candidatememory.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request candidateActionRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_candidate_action_request"})
			return
		}
		existing, err := service.Get(ctx, request.OrgID, request.CandidateID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "candidate_not_found", "message": err.Error()})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, existing.OrgID, existing.ProjectID, "memory:write", "project:"+existing.ProjectID+":write", "candidate_forbidden")
		if !ok {
			return
		}
		_ = permissions
		updated, err := service.Accept(ctx, request.OrgID, request.CandidateID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "candidate_accept_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, candidateResponse(updated))
	}
}

func CandidateDiscardHandler(service *candidatememory.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request candidateActionRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_candidate_action_request"})
			return
		}
		existing, err := service.Get(ctx, request.OrgID, request.CandidateID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "candidate_not_found", "message": err.Error()})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, existing.OrgID, existing.ProjectID, "memory:write", "project:"+existing.ProjectID+":write", "candidate_forbidden")
		if !ok {
			return
		}
		_ = permissions
		updated, err := service.Discard(ctx, request.OrgID, request.CandidateID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "candidate_discard_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, candidateResponse(updated))
	}
}

func TopicComposeHandler(composer *candidatememory.TopicComposer, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request topicComposeRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_topic_compose_request"})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:write", "project:"+request.ProjectID+":write", "topic_forbidden")
		if !ok {
			return
		}
		_ = permissions
		result, err := composer.Compose(ctx, candidatememory.ComposeRequest{
			OrgID:     request.OrgID,
			ProjectID: request.ProjectID,
			SourceKey: request.SourceKey,
			ThreadID:  request.ThreadID,
			Force:     request.Force,
		})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "topic_compose_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, result)
	}
}

func TopicListHandler(repo candidatememory.Repository, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request topicListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_topic_list_request"})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "", "topic_forbidden")
		if !ok {
			return
		}
		topics, err := repo.ListTopicStates(ctx, candidatememory.TopicStateFilter{
			OrgID:     permissions.OrgID,
			ProjectID: permissions.ProjectID,
			SourceKey: request.SourceKey,
			Limit:     request.Limit,
		})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "topic_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"topics": topicStateListResponse(topics)})
	}
}

func candidateListResponse(candidates []candidatememory.Candidate) []map[string]any {
	out := make([]map[string]any, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, candidateResponse(c))
	}
	return out
}

func candidateResponse(c candidatememory.Candidate) map[string]any {
	return map[string]any{
		"candidate_id":     c.CandidateID,
		"org_id":           c.OrgID,
		"project_id":       c.ProjectID,
		"source_key":       c.SourceKey,
		"user_id":          c.UserID,
		"agent_id":         c.AgentID,
		"thread_id":        c.ThreadID,
		"session_id":       c.SessionID,
		"source_event_ids": c.SourceEventIDs,
		"memory_type":      c.MemoryType,
		"content":          c.Content,
		"summary":          c.Summary,
		"risk_level":       c.RiskLevel,
		"confidence":       c.Confidence,
		"status":           c.Status,
		"scores":           c.Scores,
		"created_at":       c.CreatedAt,
		"updated_at":       c.UpdatedAt,
	}
}

func topicStateListResponse(topics []candidatememory.TopicState) []map[string]any {
	out := make([]map[string]any, 0, len(topics))
	for _, ts := range topics {
		out = append(out, topicStateResponse(ts))
	}
	return out
}

func topicStateResponse(ts candidatememory.TopicState) map[string]any {
	resp := map[string]any{
		"id":                  ts.ID,
		"org_id":              ts.OrgID,
		"project_id":          ts.ProjectID,
		"source_key":          ts.SourceKey,
		"thread_id":           ts.ThreadID,
		"candidate_count":     ts.CandidateCount,
		"completion_score":    ts.CompletionScore,
		"ready_to_compose":    ts.ReadyToCompose,
		"composed_archive_id": ts.ComposedArchiveID,
		"created_at":          ts.CreatedAt,
		"updated_at":          ts.UpdatedAt,
	}
	if ts.LastEventAt != nil {
		resp["last_event_at"] = ts.LastEventAt
	}
	return resp
}

func SecretCreateHandler(store secret.Store, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !store.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "secret_store_not_configured"})
			return
		}
		var request secretCreateRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_secret_create_request"})
			return
		}
		// 服务端零明文：任何明文字段都拒绝。
		if strings.TrimSpace(request.Plaintext) != "" {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "plaintext_not_allowed", "message": "server never receives secret plaintext; encrypt locally via MCP"})
			return
		}
		permissions, ok := authorizeSecretScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:write", "project:"+request.ProjectID+":write")
		if !ok {
			return
		}
		ownerUserID := request.UserID
		if authService.Configured() {
			ownerUserID = permissions.UserID
		}
		blob, err := decodeEncryptedBlob(request.Encrypted)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_secret_ciphertext", "message": err.Error()})
			return
		}
		var expiresAt *time.Time
		if strings.TrimSpace(request.ExpiresAt) != "" {
			parsed, err := time.Parse(time.RFC3339, request.ExpiresAt)
			if err != nil {
				c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_secret_expires_at", "message": err.Error()})
				return
			}
			expiresAt = &parsed
		}
		meta, err := store.CreateEncrypted(secret.CreateEncryptedRequest{
			OwnerUserID: ownerUserID,
			OrgID:       request.OrgID,
			ProjectID:   request.ProjectID,
			Name:        request.Name,
			EnvName:     request.EnvName,
			Site:        request.Site,
			Purpose:     request.Purpose,
			ExpiresAt:   expiresAt,
		}, blob)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "secret_create_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, secretMetadataResponse(meta))
	}
}

func SecretListHandler(store secret.Store, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !store.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "secret_store_not_configured"})
			return
		}
		var request secretListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_secret_list_request"})
			return
		}
		permissions, ok := authorizeSecretScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "")
		if !ok {
			return
		}
		if authService.Configured() {
			request.UserID = permissions.UserID
		}
		items, err := store.List(secret.ListFilter{OwnerUserID: request.UserID, OrgID: request.OrgID, ProjectID: request.ProjectID, Status: request.Status, Limit: request.Limit})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "secret_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"secrets": secretListResponse(items)})
	}
}

func SecretCiphertextHandler(store secret.Store, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !store.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "secret_store_not_configured"})
			return
		}
		var request secretCiphertextRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_secret_ciphertext_request"})
			return
		}
		meta, err := store.Metadata(request.SecretRef)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "secret_ciphertext_rejected", "message": err.Error()})
			return
		}
		permissions, ok := authorizeSecretScope(c, authService, tenantService, meta.OrgID, meta.ProjectID, "memory:read", "")
		if !ok {
			return
		}
		ownerUserID := meta.OwnerUserID
		if authService.Configured() {
			ownerUserID = permissions.UserID
		}
		gotMeta, blob, err := store.GetCiphertext(request.SecretRef, ownerUserID)
		if err != nil {
			if errors.Is(err, secret.ErrForbidden) {
				c.JSON(consts.StatusForbidden, map[string]string{"error": "secret_forbidden"})
				return
			}
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "secret_ciphertext_rejected", "message": err.Error()})
			return
		}
		response := secretMetadataResponse(gotMeta)
		response["encrypted"] = map[string]any{
			"algorithm":       blob.Algorithm,
			"device_key_id":   blob.DeviceKeyID,
			"key_fingerprint": blob.KeyFingerprint,
			"nonce_b64":       base64.StdEncoding.EncodeToString(blob.Nonce),
			"ciphertext_b64":  base64.StdEncoding.EncodeToString(blob.Ciphertext),
		}
		c.JSON(consts.StatusOK, response)
	}
}

func SecretDisableHandler(store secret.Store, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !store.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "secret_store_not_configured"})
			return
		}
		var request secretDisableRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_secret_disable_request"})
			return
		}
		meta, err := store.Metadata(request.SecretRef)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "secret_disable_rejected", "message": err.Error()})
			return
		}
		permissions, ok := authorizeSecretScope(c, authService, tenantService, meta.OrgID, meta.ProjectID, "memory:write", "project:"+meta.ProjectID+":write")
		if !ok {
			return
		}
		if authService.Configured() && meta.OwnerUserID != permissions.UserID {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "secret_forbidden"})
			return
		}
		if err := store.Disable(request.SecretRef); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "secret_disable_rejected", "message": err.Error()})
			return
		}
		disabled, err := store.Metadata(request.SecretRef)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "secret_disable_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, secretMetadataResponse(disabled))
	}
}

func decodeEncryptedBlob(request encryptedBlobRequest) (secret.EncryptedBlob, error) {
	if strings.TrimSpace(request.Algorithm) == "" {
		return secret.EncryptedBlob{}, errors.New("algorithm is required")
	}
	nonce, err := base64.StdEncoding.DecodeString(request.NonceB64)
	if err != nil {
		return secret.EncryptedBlob{}, errors.New("nonce_b64 is invalid base64")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(request.CiphertextB64)
	if err != nil {
		return secret.EncryptedBlob{}, errors.New("ciphertext_b64 is invalid base64")
	}
	if len(nonce) == 0 || len(ciphertext) == 0 {
		return secret.EncryptedBlob{}, errors.New("nonce and ciphertext are required")
	}
	return secret.EncryptedBlob{
		Algorithm:      request.Algorithm,
		DeviceKeyID:    request.DeviceKeyID,
		KeyFingerprint: request.KeyFingerprint,
		Nonce:          nonce,
		Ciphertext:     ciphertext,
	}, nil
}

type patCreateRequest struct {
	UserID     string   `json:"user_id"`
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	TTLSeconds int      `json:"ttl_seconds"`
}

type patListRequest struct {
	UserID string `json:"user_id"`
	Status string `json:"status"`
	Limit  int    `json:"limit"`
}

type patRevokeRequest struct {
	TokenID string `json:"token_id"`
}

type setupBootstrapRequest struct {
	Code string `json:"code"`
}

type setupBootstrapConfig struct {
	Server string   `json:"server"`
	APIURL string   `json:"api_url"`
	MCPURL string   `json:"mcp_url"`
	Token  string   `json:"token"`
	Agents []string `json:"agents"`
}

type setupCodeRecord struct {
	Config    setupBootstrapConfig
	ExpiresAt time.Time
}

type memorySetupCodeStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	records map[string]setupCodeRecord
}

func newMemorySetupCodeStore(ttl time.Duration) *memorySetupCodeStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &memorySetupCodeStore{ttl: ttl, records: map[string]setupCodeRecord{}}
}

func (s *memorySetupCodeStore) Create(config setupBootstrapConfig) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, err := randomSetupCode()
	if err != nil {
		return "", err
	}
	s.records[code] = setupCodeRecord{Config: config, ExpiresAt: time.Now().Add(s.ttl)}
	return code, nil
}

func (s *memorySetupCodeStore) Consume(code string) (setupBootstrapConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[strings.TrimSpace(code)]
	if !ok {
		return setupBootstrapConfig{}, false
	}
	delete(s.records, strings.TrimSpace(code))
	if time.Now().After(record.ExpiresAt) {
		return setupBootstrapConfig{}, false
	}
	return record.Config, true
}

func randomSetupCode() (string, error) {
	bytes := make([]byte, 18)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "mosc_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

type adapterTokenCreateRequest struct {
	UserID     string   `json:"user_id"`
	OrgID      string   `json:"org_id"`
	ProjectID  string   `json:"project_id"`
	AgentID    string   `json:"agent_id"`
	Scopes     []string `json:"scopes"`
	TTLSeconds int      `json:"ttl_seconds"`
}

type adapterTokenListRequest struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
	Status    string `json:"status"`
	Limit     int    `json:"limit"`
}

type adapterTokenRevokeRequest struct {
	TokenID string `json:"token_id"`
}

func PATCreateHandler(authService auth.Service, auditService audit.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request patCreateRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_pat_create_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:write", "token_forbidden")
		if !ok {
			return
		}
		ttl := tokenTTL(request.TTLSeconds)
		plain, record, err := authService.CreatePAT(caller.SubjectID, request.Name, request.Scopes, ttl)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "pat_create_rejected", "message": err.Error()})
			return
		}
		recordAudit(auditService, audit.Log{
			ActorUserID:  caller.SubjectID,
			Action:       "token.pat.create",
			ResourceType: "pat",
			ResourceID:   record.ID,
			RequestID:    auditRequestID("token.pat.create", record.ID),
			Result:       "ok",
			Metadata: map[string]string{
				"token_prefix": record.TokenPrefix,
				"token_name":   record.Name,
				"scope_count":  fmt.Sprintf("%d", len(record.Scopes)),
				"ttl_seconds":  fmt.Sprintf("%d", int(ttl/time.Second)),
			},
		})
		serverURL := publicServerURL(c)
		installCode, err := defaultSetupCodes.Create(setupBootstrapConfig{
			Server: serverURL,
			APIURL: serverURL,
			MCPURL: serverURLForPort(serverURL, "18082") + "/mcp",
			Token:  plain,
			Agents: []string{"codex", "claude-code", "opencode", "hermes", "openclaw"},
		})
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"error": "setup_code_issue_failed"})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{
			"token":          plain,
			"token_metadata": patMetadataResponse(record),
			"install_code":   installCode,
			"setup_command":  setupCommand(serverURL, installCode),
		})
	}
}

func SetupBootstrapHandler(store *memorySetupCodeStore) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request setupBootstrapRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_setup_bootstrap_request"})
			return
		}
		config, ok := store.Consume(request.Code)
		if !ok {
			c.JSON(consts.StatusNotFound, map[string]string{"error": "setup_code_not_found"})
			return
		}
		c.JSON(consts.StatusOK, config)
	}
}

func SetupInstallScriptHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Header("Content-Type", "text/x-shellscript; charset=utf-8")
		c.String(consts.StatusOK, setupInstallScript())
	}
}

func publicServerURL(c *app.RequestContext) string {
	proto := strings.TrimSpace(string(c.GetHeader("X-Forwarded-Proto")))
	if proto == "" {
		proto = "http"
	}
	host := strings.TrimSpace(string(c.GetHeader("X-Forwarded-Host")))
	if host == "" {
		host = strings.TrimSpace(string(c.Host()))
	}
	if host == "" {
		host = "localhost:18081"
	}
	return proto + "://" + host
}

func setupCommand(serverURL, installCode string) string {
	return fmt.Sprintf("curl -fsSL %s/memory/setup/install.sh | sh -s -- --server %s --code %s --agent auto", serverURL, serverURL, installCode)
}

func serverURLForPort(serverURL, port string) string {
	if strings.HasSuffix(serverURL, ":18081") {
		return strings.TrimSuffix(serverURL, ":18081") + ":" + port
	}
	return serverURL
}

func setupInstallScript() string {
	return `#!/usr/bin/env sh
set -eu

SERVER=""
CODE=""
AGENT="auto"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --server) SERVER="$2"; shift 2 ;;
    --code) CODE="$2"; shift 2 ;;
    --agent) AGENT="$2"; shift 2 ;;
    *) shift ;;
  esac
done

if [ -z "$SERVER" ] || [ -z "$CODE" ]; then
  echo "Usage: install.sh --server <url> --code <install_code> [--agent auto]" >&2
  exit 1
fi

export MEMORY_OS_SETUP_SERVER="$SERVER"
export MEMORY_OS_SETUP_CODE="$CODE"
export MEMORY_OS_SETUP_AGENT="$AGENT"
python3 <<'PY'
import json
import os
import pathlib
import shlex
import tempfile
import urllib.request

server = os.environ["MEMORY_OS_SETUP_SERVER"].rstrip("/")
code = os.environ["MEMORY_OS_SETUP_CODE"]
agent = os.environ.get("MEMORY_OS_SETUP_AGENT", "auto")
body = json.dumps({"code": code}).encode("utf-8")
request = urllib.request.Request(
    server + "/memory/setup/bootstrap",
    data=body,
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(request, timeout=30) as response:
    config = json.loads(response.read().decode("utf-8"))

home = pathlib.Path.home()
secret_dir = home / ".config" / "ai-secrets"
secret_dir.mkdir(parents=True, exist_ok=True)
secret_file = secret_dir / "secrets.env"
existing = ""
if secret_file.exists():
    existing = secret_file.read_text()
lines = [
    line for line in existing.splitlines()
    if not line.startswith("MEMORY_OS_TOKEN=")
    and not line.startswith("MEMORY_OS_API_URL=")
]
lines.append("MEMORY_OS_TOKEN='" + config["token"].replace("'", "'\"'\"'") + "'")
lines.append("MEMORY_OS_API_URL='" + config["api_url"].replace("'", "'\"'\"'") + "'")
secret_file.write_text("\n".join(lines) + "\n")
secret_dir.chmod(0o700)
secret_file.chmod(0o600)

source_line = '[ -f "$HOME/.config/ai-secrets/secrets.env" ] && set -a && . "$HOME/.config/ai-secrets/secrets.env" && set +a'
source_block = "\n# Added by Memory OS installer\n" + source_line + "\n"
for rc_name in (".zshrc", ".bashrc"):
    rc_file = home / rc_name
    try:
        rc_existing = rc_file.read_text() if rc_file.exists() else ""
        if source_line not in rc_existing:
            rc_file.write_text(rc_existing.rstrip() + source_block)
    except OSError:
        pass

def atomic_json_write(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    encoded = json.dumps(data, ensure_ascii=False, indent=2) + "\n"
    fd, tmp_name = tempfile.mkstemp(prefix=path.name + ".", dir=str(path.parent))
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as tmp:
            tmp.write(encoded)
        os.replace(tmp_name, path)
    finally:
        if os.path.exists(tmp_name):
            os.unlink(tmp_name)

def atomic_text_write(path, text):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp_name = tempfile.mkstemp(prefix=path.name + ".", dir=str(path.parent))
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as tmp:
            tmp.write(text)
        os.replace(tmp_name, path)
    finally:
        if os.path.exists(tmp_name):
            os.unlink(tmp_name)

def json_file(path):
    if path.exists():
        existing_text = path.read_text()
        data = json.loads(existing_text) if existing_text.strip() else {}
        if not isinstance(data, dict):
            raise ValueError(str(path) + " must contain a JSON object")
        return data
    return {}

def managed_text(existing, block, label="Memory OS MCP"):
    begin = "# BEGIN " + label
    end = "# END " + label
    managed = begin + "\n" + block.rstrip() + "\n" + end + "\n"
    if begin in existing and end in existing:
        before, rest = existing.split(begin, 1)
        _, after = rest.split(end, 1)
        return before.rstrip() + "\n\n" + managed + after.lstrip()
    prefix = existing.rstrip()
    return (prefix + "\n\n" if prefix else "") + managed

def remove_managed_text(existing, label="Memory OS MCP"):
    begin = "# BEGIN " + label
    end = "# END " + label
    text = existing
    while begin in text and end in text:
        before, rest = text.split(begin, 1)
        _, after = rest.split(end, 1)
        text = before.rstrip() + "\n\n" + after.lstrip()
    return text

def remove_toml_table(existing, table_name):
    lines = existing.splitlines()
    output = []
    skipping = False
    target = "[" + table_name + "]"
    for line in lines:
        stripped = line.strip()
        if stripped == target:
            skipping = True
            continue
        if skipping and stripped.startswith("[") and stripped.endswith("]"):
            skipping = False
        if not skipping:
            output.append(line)
    return "\n".join(output).rstrip() + ("\n" if output else "")

def selected(*names):
    normalized = agent.strip().lower()
    return normalized == "auto" or normalized in names

def register_claude_code_mcp():
    claude_config = home / ".claude" / ".mcp.json"
    data = json_file(claude_config)
    servers = data.setdefault("mcpServers", {})
    if not isinstance(servers, dict):
        raise ValueError("~/.claude/.mcp.json mcpServers must be a JSON object")
    servers["memory-os"] = {
        "type": "http",
        "url": config["mcp_url"],
        "headers": {
            "Authorization": "Bearer ${MEMORY_OS_TOKEN}",
        },
    }
    atomic_json_write(claude_config, data)

def register_codex_mcp():
    codex_config = home / ".codex" / "config.toml"
    existing = codex_config.read_text() if codex_config.exists() else ""
    existing = remove_managed_text(existing)
    existing = remove_toml_table(existing, "mcp_servers.memory-os")
    block = "\n".join([
        "[mcp_servers.memory-os]",
        "url = " + json.dumps(config["mcp_url"]),
        'bearer_token_env_var = "MEMORY_OS_TOKEN"',
    ])
    atomic_text_write(codex_config, managed_text(existing, block))

def register_opencode_mcp():
    opencode_config = home / ".config" / "opencode" / "opencode.json"
    data = json_file(opencode_config)
    mcp = data.setdefault("mcp", {})
    if not isinstance(mcp, dict):
        raise ValueError("opencode mcp must be a JSON object")
    mcp["memory-os"] = {
        "type": "remote",
        "url": config["mcp_url"],
        "headers": {
            "Authorization": "Bearer {env:MEMORY_OS_TOKEN}",
        },
    }
    atomic_json_write(opencode_config, data)

def register_hermes_mcp():
    hermes_config = home / ".hermes" / "config.yaml"
    existing = hermes_config.read_text() if hermes_config.exists() else ""
    block = "\n".join([
        "mcp_servers:",
        "  memory-os:",
        "    enabled: true",
        "    url: " + json.dumps(config["mcp_url"]),
        "    headers:",
        "      Authorization: \"Bearer ${MEMORY_OS_TOKEN}\"",
    ])
    atomic_text_write(hermes_config, managed_text(existing, block, "Memory OS Hermes Hook"))

def register_openclaw_mcp():
    openclaw_config = home / ".openclaw" / "openclaw.json"
    data = json_file(openclaw_config)
    mcp = data.setdefault("mcp", {})
    if not isinstance(mcp, dict):
        raise ValueError("openclaw mcp must be a JSON object")
    servers = mcp.setdefault("servers", {})
    if not isinstance(servers, dict):
        raise ValueError("openclaw mcp.servers must be a JSON object")
    servers["memory-os"] = {
        "type": "http",
        "url": config["mcp_url"],
        "headers": {
            "Authorization": "Bearer ${MEMORY_OS_TOKEN}",
        },
    }
    atomic_json_write(openclaw_config, data)

def install_common_hook_script():
    hook_path = home / ".memory-os" / "hooks" / "turn_event.py"
    script = r'''#!/usr/bin/env python3
import hashlib
import json
import os
import re
import socket
import subprocess
import sys
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

DEFAULT_MEMORY_OS_API_URL = __MEMORY_OS_API_URL__

def load_secrets():
    secrets = Path.home() / ".config" / "ai-secrets" / "secrets.env"
    if not secrets.exists():
        return
    for line in secrets.read_text(errors="replace").splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        value = value.strip().strip("'").strip('"')
        os.environ.setdefault(key.strip(), value)

def clean_text(text):
    if not text:
        return ""
    text = re.sub(r"sk-[A-Za-z0-9_\-]{16,}", "sk-[REDACTED]", text)
    text = re.sub(r"pat_[A-Za-z0-9_\-]{16,}", "pat_[REDACTED]", text)
    text = re.sub(r"m0sk_[A-Za-z0-9_\-]{16,}", "m0sk_[REDACTED]", text)
    text = re.sub(r"(?i)(authorization\s*[:=]\s*bearer\s+)[A-Za-z0-9._\-]+", r"\1[REDACTED]", text)
    text = re.sub(r"(?i)(https?://)([^:@/\s]+):([^@/\s]+)@", r"\1[REDACTED]:[REDACTED]@", text)
    text = text.strip()
    if len(text) > 12000:
        text = text[:12000] + "\n[TRUNCATED]"
    return text

def git_value(cwd, *args):
    try:
        return subprocess.check_output(["git", "-C", cwd, *args], text=True, stderr=subprocess.DEVNULL, timeout=2).strip()
    except Exception:
        return ""

def workspace_identity(cwd):
    git_root = git_value(cwd, "rev-parse", "--show-toplevel")
    git_remote = git_value(cwd, "remote", "get-url", "origin")
    if git_remote:
        return {
            "git_remote": git_remote,
            "git_root": git_root,
            "cwd": cwd,
            "git_branch": git_value(cwd, "branch", "--show-current"),
            "git_commit": git_value(cwd, "rev-parse", "HEAD"),
        }
    abs_cwd = str(Path(cwd).expanduser().resolve())
    return {
        "git_remote": "local/" + abs_cwd.lstrip("/"),
        "git_root": abs_cwd,
        "cwd": abs_cwd,
        "git_branch": "",
        "git_commit": "",
    }

def extract_text(data, raw):
    if isinstance(data, dict):
        for key in ("last_assistant_message", "assistant_response", "assistant", "message", "prompt", "input", "text"):
            value = data.get(key)
            if isinstance(value, str) and value.strip():
                return value
        for key in ("messages", "conversation", "conversation_history"):
            value = data.get(key)
            if isinstance(value, list):
                parts = []
                for item in value[-12:]:
                    if isinstance(item, dict):
                        role = item.get("role") or item.get("type") or "message"
                        content = item.get("content") or item.get("text") or item.get("message")
                        if isinstance(content, str) and content.strip():
                            parts.append(f"{role}: {content}")
                    elif isinstance(item, str):
                        parts.append(item)
                if parts:
                    return "\n".join(parts)
    return raw

load_secrets()
token = os.environ.get("MEMORY_OS_TOKEN", "")
if not token:
    sys.exit(0)

raw = sys.stdin.read()
try:
    data = json.loads(raw) if raw.strip() else {}
except Exception:
    data = {}

cwd = data.get("cwd") if isinstance(data, dict) else ""
cwd = cwd or os.getcwd()
agent = os.environ.get("MEMORY_OS_HOOK_AGENT", "unknown-agent")
api = os.environ.get("MEMORY_OS_API_URL", os.environ.get("MEMORY_OS_SETUP_API_URL", "")).rstrip("/")
if not api:
    api = os.environ.get("MEMORY_OS_SERVER", "").rstrip("/")
if not api:
    api = DEFAULT_MEMORY_OS_API_URL.rstrip("/")

text = clean_text(extract_text(data, raw))
if not text:
    sys.exit(0)

digest = hashlib.sha256((agent + "\n" + cwd + "\n" + text).encode("utf-8")).hexdigest()[:24]

body = {
    "request_id": f"{agent}_hook_{digest}",
    "workspace": workspace_identity(cwd),
    "event": {
        "version": "v1",
        "event_id": f"event_{agent}_{digest}",
        "turn_id": f"turn_{agent}_{digest}",
        "thread_id": f"thread_{agent}_{hashlib.sha256(cwd.encode()).hexdigest()[:16]}",
        "session_id": f"session_{agent}_{hashlib.sha256((cwd + socket.gethostname()).encode()).hexdigest()[:16]}",
        "type": "assistant_final",
        "created_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "actor": {"agent_id": agent},
        "source": {"platform": agent, "host": socket.gethostname()},
        "payload": {"text": text, "raw_event": clean_text(raw)},
    },
}

request = urllib.request.Request(
    api + "/memory/turn-event",
    data=json.dumps(body, ensure_ascii=False).encode("utf-8"),
    headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
    method="POST",
)
try:
    with urllib.request.urlopen(request, timeout=15) as response:
        response.read()
except (urllib.error.URLError, TimeoutError, OSError):
    sys.exit(0)
'''
    script = script.replace("__MEMORY_OS_API_URL__", json.dumps(config["api_url"]))
    atomic_text_write(hook_path, script)
    hook_path.chmod(0o700)
    return hook_path

def hook_command(agent_name, hook_path):
    return "MEMORY_OS_HOOK_AGENT=" + shlex.quote(agent_name) + " python3 " + shlex.quote(str(hook_path))

def append_hook(data, event_name, hook_entry):
    hooks = data.setdefault("hooks", {})
    if not isinstance(hooks, dict):
        raise ValueError("hooks must be a JSON object")
    groups = hooks.setdefault(event_name, [])
    if not isinstance(groups, list):
        raise ValueError(event_name + " hooks must be a list")
    groups[:] = prune_memory_os_hook_groups(groups)
    command = hook_entry.get("command", "")
    for group in groups:
        if not isinstance(group, dict):
            continue
        for existing in group.get("hooks", []) or []:
            if isinstance(existing, dict) and existing.get("command") == command:
                return
    groups.append({"hooks": [hook_entry]})

def is_memory_os_hook_command(command):
    return (
        "MEMORY_OS_HOOK_AGENT=" in command
        or ".memory-os/hooks/turn_event.py" in command
        or "memory_os_turn_event.sh" in command
    )

def is_missing_claude_script_command(command):
    parts = shlex.split(command)
    for part in parts:
        if "/.claude/scripts/" in part and part.startswith("/"):
            return not pathlib.Path(part).exists()
    return False

def prune_memory_os_hook_groups(groups):
    pruned_groups = []
    for group in groups:
        if not isinstance(group, dict):
            pruned_groups.append(group)
            continue
        hooks = group.get("hooks", [])
        if not isinstance(hooks, list):
            pruned_groups.append(group)
            continue
        kept = []
        for item in hooks:
            command = item.get("command", "") if isinstance(item, dict) else ""
            if command and is_memory_os_hook_command(command):
                continue
            if command and is_missing_claude_script_command(command):
                continue
            kept.append(item)
        if kept:
            next_group = dict(group)
            next_group["hooks"] = kept
            pruned_groups.append(next_group)
    return pruned_groups

def register_claude_code_hook(hook_path):
    settings_path = home / ".claude" / "settings.json"
    data = json_file(settings_path)
    append_hook(data, "Stop", {
        "type": "command",
        "command": hook_command("claude-code", hook_path),
        "statusMessage": "保存对话到 Memory OS",
        "timeout": 30,
        "async": True,
    })
    atomic_json_write(settings_path, data)

def register_codex_hook(hook_path):
    hooks_path = home / ".codex" / "hooks.json"
    data = json_file(hooks_path)
    append_hook(data, "Stop", {
        "type": "command",
        "command": hook_command("codex", hook_path),
        "statusMessage": "Saving conversation to Memory OS",
        "timeout": 30,
    })
    atomic_json_write(hooks_path, data)

def add_plugin_path(data, plugin_path):
    plugins = data.setdefault("plugin", [])
    if isinstance(plugins, str):
        plugins = [plugins]
        data["plugin"] = plugins
    if not isinstance(plugins, list):
        raise ValueError("plugin must be a list or string")
    plugin_text = str(plugin_path)
    if plugin_text not in plugins:
        plugins.append(plugin_text)

def register_opencode_hook(hook_path):
    plugin_path = home / ".config" / "opencode" / "plugins" / "memory-os.js"
    code = """import { spawnSync } from "node:child_process";

export const MemoryOS = async () => ({
  event: async ({ event }) => {
    if (event?.type !== "session.idle") return;
    spawnSync("python3", [__HOOK_PATH__], {
      input: JSON.stringify(event),
      env: { ...process.env, MEMORY_OS_HOOK_AGENT: "opencode" },
      stdio: ["pipe", "ignore", "ignore"],
    });
  },
});
""".replace("__HOOK_PATH__", json.dumps(str(hook_path)))
    atomic_text_write(plugin_path, code)
    data = json_file(home / ".config" / "opencode" / "opencode.json")
    add_plugin_path(data, plugin_path)
    atomic_json_write(home / ".config" / "opencode" / "opencode.json", data)

def register_hermes_hook(hook_path):
    plugin_path = home / ".hermes" / "plugins" / "memory_os_hook.py"
    code = """import json
import os
import subprocess

def post_llm_call(user_message=None, assistant_response=None, conversation_history=None, **kwargs):
    payload = {
        "user_message": user_message,
        "assistant_response": assistant_response,
        "conversation_history": conversation_history,
    }
    env = os.environ.copy()
    env["MEMORY_OS_HOOK_AGENT"] = "hermes"
    subprocess.run(["python3", __HOOK_PATH__], input=json.dumps(payload), text=True, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
""".replace("__HOOK_PATH__", json.dumps(str(hook_path)))
    atomic_text_write(plugin_path, code)
    hermes_config = home / ".hermes" / "config.yaml"
    existing = hermes_config.read_text() if hermes_config.exists() else ""
    block = "plugins:\n  - " + json.dumps(str(plugin_path))
    atomic_text_write(hermes_config, managed_text(existing, block))

def register_openclaw_hook(hook_path):
    plugin_path = home / ".openclaw" / "plugins" / "memory-os.js"
    code = """const { spawnSync } = require("node:child_process");

module.exports = {
  hooks: {
    "message:sent": async (event) => {
      spawnSync("python3", [__HOOK_PATH__], {
        input: JSON.stringify(event || {}),
        env: { ...process.env, MEMORY_OS_HOOK_AGENT: "openclaw" },
        stdio: ["pipe", "ignore", "ignore"],
      });
    },
  },
};
""".replace("__HOOK_PATH__", json.dumps(str(hook_path)))
    atomic_text_write(plugin_path, code)
    data = json_file(home / ".openclaw" / "openclaw.json")
    add_plugin_path(data, plugin_path)
    atomic_json_write(home / ".openclaw" / "openclaw.json", data)

configured_agents = []
hook_path = install_common_hook_script()
if selected("claude", "claude-code"):
	register_claude_code_mcp()
	register_claude_code_hook(hook_path)
	configured_agents.append("Claude Code")
if selected("codex"):
	register_codex_mcp()
	register_codex_hook(hook_path)
	configured_agents.append("Codex")
if selected("opencode", "open-code"):
	register_opencode_mcp()
	register_opencode_hook(hook_path)
	configured_agents.append("opencode")
if selected("hermes"):
	register_hermes_mcp()
	register_hermes_hook(hook_path)
	configured_agents.append("Hermes")
if selected("openclaw", "open-claw"):
	register_openclaw_mcp()
	register_openclaw_hook(hook_path)
	configured_agents.append("OpenClaw")

print("Memory OS setup bootstrap saved token to ~/.config/ai-secrets/secrets.env")
print("API:", config["api_url"])
print("MCP:", config["mcp_url"])
if configured_agents:
    print("Configured MCP for:", ", ".join(configured_agents))
print("Agents:", ", ".join(config.get("agents", [])))
PY
`
}

func PATListHandler(authService auth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request patListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_pat_list_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:read", "token_forbidden")
		if !ok {
			return
		}
		items, err := authService.ListPATs(auth.TokenListFilter{UserID: caller.SubjectID, Status: request.Status, Limit: request.Limit})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "pat_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"tokens": patListResponse(items)})
	}
}

func PATRevokeHandler(authService auth.Service, auditService audit.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request patRevokeRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_pat_revoke_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:write", "token_forbidden")
		if !ok {
			return
		}
		record, err := authService.GetPAT(request.TokenID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "pat_revoke_rejected", "message": err.Error()})
			return
		}
		if record.SubjectID != caller.SubjectID {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "token_forbidden"})
			return
		}
		if err := authService.RevokePAT(request.TokenID); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "pat_revoke_rejected", "message": err.Error()})
			return
		}
		revoked, err := authService.GetPAT(request.TokenID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "pat_revoke_rejected", "message": err.Error()})
			return
		}
		recordAudit(auditService, audit.Log{
			ActorUserID:  caller.SubjectID,
			Action:       "token.pat.revoke",
			ResourceType: "pat",
			ResourceID:   revoked.ID,
			RequestID:    auditRequestID("token.pat.revoke", revoked.ID),
			Result:       "ok",
			Metadata: map[string]string{
				"token_prefix": revoked.TokenPrefix,
				"token_name":   revoked.Name,
				"scope_count":  fmt.Sprintf("%d", len(revoked.Scopes)),
			},
		})
		c.JSON(consts.StatusOK, patMetadataResponse(revoked))
	}
}

func AdapterTokenCreateHandler(authService auth.Service, tenantService tenant.Service, auditService audit.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request adapterTokenCreateRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_adapter_token_create_request"})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:write", "project:"+request.ProjectID+":write", "token_forbidden")
		if !ok {
			return
		}
		plain, record, err := authService.CreateAdapterToken(auth.AdapterTokenRequest{UserID: permissions.UserID, OrgID: request.OrgID, ProjectID: request.ProjectID, AgentID: request.AgentID, Scopes: request.Scopes, TTL: tokenTTL(request.TTLSeconds)})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "adapter_token_create_rejected", "message": err.Error()})
			return
		}
		recordAudit(auditService, audit.Log{
			ActorUserID:  permissions.UserID,
			OrgID:        request.OrgID,
			ProjectID:    request.ProjectID,
			Action:       "token.adapter.create",
			ResourceType: "adapter_token",
			ResourceID:   record.ID,
			RequestID:    auditRequestID("token.adapter.create", record.ID),
			Result:       "ok",
			Metadata: map[string]string{
				"agent_id":     record.AgentID,
				"token_prefix": record.TokenPrefix,
				"scope_count":  fmt.Sprintf("%d", len(record.Scopes)),
				"ttl_seconds":  fmt.Sprintf("%d", int(tokenTTL(request.TTLSeconds)/time.Second)),
			},
		})
		c.JSON(consts.StatusOK, map[string]any{"token": plain, "token_metadata": adapterTokenMetadataResponse(record)})
	}
}

func AdapterTokenListHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request adapterTokenListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_adapter_token_list_request"})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "", "token_forbidden")
		if !ok {
			return
		}
		items, err := authService.ListAdapterTokens(auth.AdapterTokenListFilter{UserID: permissions.UserID, OrgID: request.OrgID, ProjectID: request.ProjectID, AgentID: request.AgentID, Status: request.Status, Limit: request.Limit})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "adapter_token_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"tokens": adapterTokenListResponse(items)})
	}
}

func AdapterTokenRevokeHandler(authService auth.Service, tenantService tenant.Service, auditService audit.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request adapterTokenRevokeRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_adapter_token_revoke_request"})
			return
		}
		record, err := authService.GetAdapterToken(request.TokenID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "adapter_token_revoke_rejected", "message": err.Error()})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, record.OrgID, record.ProjectID, "memory:write", "project:"+record.ProjectID+":write", "token_forbidden")
		if !ok {
			return
		}
		if record.UserID != permissions.UserID {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "token_forbidden"})
			return
		}
		if err := authService.RevokeAdapterToken(request.TokenID); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "adapter_token_revoke_rejected", "message": err.Error()})
			return
		}
		revoked, err := authService.GetAdapterToken(request.TokenID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "adapter_token_revoke_rejected", "message": err.Error()})
			return
		}
		recordAudit(auditService, audit.Log{
			ActorUserID:  permissions.UserID,
			OrgID:        revoked.OrgID,
			ProjectID:    revoked.ProjectID,
			Action:       "token.adapter.revoke",
			ResourceType: "adapter_token",
			ResourceID:   revoked.ID,
			RequestID:    auditRequestID("token.adapter.revoke", revoked.ID),
			Result:       "ok",
			Metadata: map[string]string{
				"agent_id":     revoked.AgentID,
				"token_prefix": revoked.TokenPrefix,
				"scope_count":  fmt.Sprintf("%d", len(revoked.Scopes)),
			},
		})
		c.JSON(consts.StatusOK, adapterTokenMetadataResponse(revoked))
	}
}

type tenantUserCreateRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type tenantUserListRequest struct {
	Status string `json:"status"`
}

type tenantUserUpdateStatusRequest struct {
	UserID string `json:"user_id"`
	Status string `json:"status"`
}

type tenantOrgCreateRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type tenantOrgListRequest struct {
	UserID string `json:"user_id"`
}

type tenantOrgDeleteRequest struct {
	OrgID string `json:"org_id"`
}

type tenantOrgEditRequest struct {
	OrgID string `json:"org_id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
}

type tenantProjectCreateRequest struct {
	OrgID string `json:"org_id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
}

type tenantProjectListRequest struct {
	OrgID string `json:"org_id"`
}

type tenantProjectDeleteRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
}

type tenantProjectEditRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
}

type tenantMembershipAddRequest struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Role      string `json:"role"`
}

type tenantMembershipListRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
}

type tenantMembershipUpdateRoleRequest struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Role      string `json:"role"`
}

type tenantMembershipRemoveRequest struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
}

type tenantRolesListRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
}

type tenantRoleUpsertRequest struct {
	OrgID            string   `json:"org_id"`
	ProjectID        string   `json:"project_id"`
	Role             string   `json:"role"`
	DisplayName      string   `json:"display_name"`
	Description      string   `json:"description"`
	PermissionLabels []string `json:"permission_labels"`
}

func TenantUserCreateHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantUserCreateRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_user_create_request"})
			return
		}
		if _, ok := authorizePAT(c, authService, "memory:write", "tenant_forbidden"); !ok {
			return
		}
		user, err := tenantService.CreateUser(request.Email, request.DisplayName)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_user_create_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"user": tenantUserResponse(user)})
	}
}

func TenantUserListHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantUserListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_user_list_request"})
			return
		}
		if _, ok := authorizePAT(c, authService, "memory:read", "tenant_forbidden"); !ok {
			return
		}
		items, err := tenantService.ListUsers(request.Status)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_user_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"users": tenantUserListResponse(items)})
	}
}

func TenantUserUpdateStatusHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantUserUpdateStatusRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_user_update_status_request"})
			return
		}
		if _, ok := authorizePAT(c, authService, "memory:write", "tenant_forbidden"); !ok {
			return
		}
		user, err := tenantService.UpdateUserStatus(request.UserID, request.Status)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_user_update_status_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"user": tenantUserResponse(user)})
	}
}

func TenantOrgCreateHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantOrgCreateRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_org_create_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:write", "tenant_forbidden")
		if !ok {
			return
		}
		org, err := tenantService.CreateOrg(request.Name, request.Slug)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_org_create_rejected", "message": err.Error()})
			return
		}
		if err := tenantService.AddMembership(caller.SubjectID, org.ID, "", tenant.RoleOwner); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_org_owner_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"org": tenantOrgResponse(org), "owner_membership": tenantMembershipResponse(tenant.Membership{UserID: caller.SubjectID, OrgID: org.ID, Role: tenant.RoleOwner, Status: "active"})})
	}
}

func TenantOrgListHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantOrgListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_org_list_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:read", "tenant_forbidden")
		if !ok {
			return
		}
		items, err := tenantService.ListOrgs(caller.SubjectID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_org_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"orgs": tenantOrgListResponse(items)})
	}
}

func TenantOrgEditHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantOrgEditRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_org_edit_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:write", "tenant_forbidden")
		if !ok {
			return
		}
		org, err := tenantService.UpdateOrg(caller.SubjectID, request.OrgID, request.Name, request.Slug)
		if err != nil {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "tenant_forbidden", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"org": tenantOrgResponse(org)})
	}
}

func TenantOrgDeleteHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantOrgDeleteRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_org_delete_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:write", "tenant_forbidden")
		if !ok {
			return
		}
		org, err := tenantService.DeleteOrg(caller.SubjectID, request.OrgID)
		if err != nil {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "tenant_forbidden", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"org": tenantOrgResponse(org)})
	}
}

func TenantProjectCreateHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantProjectCreateRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_project_create_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:write", "tenant_forbidden")
		if !ok {
			return
		}
		if err := tenantService.RequireOrgWrite(caller.SubjectID, request.OrgID); err != nil {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "tenant_forbidden"})
			return
		}
		project, err := tenantService.CreateProject(request.OrgID, request.Name, request.Slug)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_project_create_rejected", "message": err.Error()})
			return
		}
		if err := tenantService.AddMembership(caller.SubjectID, request.OrgID, project.ID, tenant.RoleOwner); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_project_owner_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"project": tenantProjectResponse(project)})
	}
}

func TenantProjectListHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantProjectListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_project_list_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:read", "tenant_forbidden")
		if !ok {
			return
		}
		items, err := tenantService.ListProjects(caller.SubjectID, request.OrgID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_project_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"projects": tenantProjectListResponse(items)})
	}
}

func TenantProjectEditHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantProjectEditRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_project_edit_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:write", "tenant_forbidden")
		if !ok {
			return
		}
		project, err := tenantService.UpdateProject(caller.SubjectID, request.OrgID, request.ProjectID, request.Name, request.Slug)
		if err != nil {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "tenant_forbidden", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"project": tenantProjectResponse(project)})
	}
}

func TenantProjectDeleteHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantProjectDeleteRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_project_delete_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:write", "tenant_forbidden")
		if !ok {
			return
		}
		project, err := tenantService.DeleteProject(caller.SubjectID, request.OrgID, request.ProjectID)
		if err != nil {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "tenant_forbidden", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"project": tenantProjectResponse(project)})
	}
}

func TenantMembershipAddHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantMembershipAddRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_membership_add_request"})
			return
		}
		if _, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:write", "project:"+request.ProjectID+":write", "tenant_forbidden"); !ok {
			return
		}
		if err := tenantService.AddMembership(request.UserID, request.OrgID, request.ProjectID, request.Role); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_membership_add_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, tenantMembershipResponse(tenant.Membership{UserID: request.UserID, OrgID: request.OrgID, ProjectID: request.ProjectID, Role: request.Role, Status: "active"}))
	}
}

func TenantMembershipListHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantMembershipListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_membership_list_request"})
			return
		}
		if _, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "", "tenant_forbidden"); !ok {
			return
		}
		items, err := tenantService.ListMemberships(request.OrgID, request.ProjectID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_membership_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"memberships": tenantMembershipListResponse(items)})
	}
}

func TenantMembershipUpdateRoleHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantMembershipUpdateRoleRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_membership_update_role_request"})
			return
		}
		caller := auth.PATRecord{}
		var ok bool
		if strings.TrimSpace(request.ProjectID) != "" {
			if _, ok = authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:write", "project:"+request.ProjectID+":write", "tenant_forbidden"); !ok {
				return
			}
			caller, ok = authorizePAT(c, authService, "memory:write", "tenant_forbidden")
			if !ok {
				return
			}
		} else {
			caller, ok = authorizePAT(c, authService, "memory:write", "tenant_forbidden")
			if !ok {
				return
			}
		}
		membership, err := tenantService.UpdateMembershipRole(caller.SubjectID, request.UserID, request.OrgID, request.ProjectID, request.Role)
		if err != nil {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "tenant_forbidden", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, tenantMembershipResponse(membership))
	}
}

func TenantMembershipRemoveHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantMembershipRemoveRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_membership_remove_request"})
			return
		}
		caller := auth.PATRecord{}
		var ok bool
		if strings.TrimSpace(request.ProjectID) != "" {
			if _, ok = authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:write", "project:"+request.ProjectID+":write", "tenant_forbidden"); !ok {
				return
			}
			caller, ok = authorizePAT(c, authService, "memory:write", "tenant_forbidden")
			if !ok {
				return
			}
		} else {
			caller, ok = authorizePAT(c, authService, "memory:write", "tenant_forbidden")
			if !ok {
				return
			}
		}
		membership, err := tenantService.RemoveMembership(caller.SubjectID, request.UserID, request.OrgID, request.ProjectID)
		if err != nil {
			c.JSON(consts.StatusForbidden, map[string]string{"error": "tenant_forbidden", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, tenantMembershipResponse(membership))
	}
}

func TenantRolesListHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantRolesListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_roles_list_request"})
			return
		}
		if _, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "", "tenant_forbidden"); !ok {
			return
		}
		roles, err := tenantService.ListRoles(request.ProjectID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_roles_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"roles": tenantRoleDefinitionListResponse(roles)})
	}
}

func TenantRoleUpsertHandler(authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !tenantService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "tenant_not_configured"})
			return
		}
		var request tenantRoleUpsertRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_tenant_role_upsert_request"})
			return
		}
		if _, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:write", "project:"+request.ProjectID+":write", "tenant_forbidden"); !ok {
			return
		}
		definition, err := tenantService.UpsertRoleDefinition(tenant.RoleDefinition{
			Role:             request.Role,
			DisplayName:      request.DisplayName,
			Description:      request.Description,
			PermissionLabels: request.PermissionLabels,
		})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "tenant_role_upsert_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"role": tenantRoleDefinitionResponse(definition)})
	}
}

type auditListRequest struct {
	OrgID        string `json:"org_id"`
	ProjectID    string `json:"project_id"`
	ActorUserID  string `json:"actor_user_id"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Limit        int    `json:"limit"`
}

type securityLogListRequest struct {
	ResourceType string `json:"resource_type"`
	Limit        int    `json:"limit"`
}

type retrievalAccessLogListRequest struct {
	OrgID       string `json:"org_id"`
	ProjectID   string `json:"project_id"`
	ActorUserID string `json:"actor_user_id"`
	RequestID   string `json:"request_id"`
	Limit       int    `json:"limit"`
}

func SecurityLogListHandler(auditService audit.Service, authService auth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !auditService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "audit_not_configured"})
			return
		}
		var request securityLogListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_security_log_list_request"})
			return
		}
		caller, ok := authorizePAT(c, authService, "memory:read", "security_log_forbidden")
		if !ok {
			return
		}
		items, err := auditService.ListSecurity(audit.ListFilter{ActorUserID: caller.SubjectID, ResourceType: request.ResourceType, Limit: request.Limit})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "security_log_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"audit_logs": auditLogListResponse(items)})
	}
}

func AuditListHandler(auditService audit.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !auditService.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "audit_not_configured"})
			return
		}
		var request auditListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_audit_list_request"})
			return
		}
		if _, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "", "audit_forbidden"); !ok {
			return
		}
		items, err := auditService.List(audit.ListFilter{OrgID: request.OrgID, ProjectID: request.ProjectID, ActorUserID: request.ActorUserID, ResourceType: request.ResourceType, ResourceID: request.ResourceID, Limit: request.Limit})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "audit_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"audit_logs": auditLogListResponse(items)})
	}
}

func RetrievalAccessLogListHandler(accessLog retrieval.AccessLogReader, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if accessLog == nil {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "retrieval_access_log_not_configured"})
			return
		}
		var request retrievalAccessLogListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_retrieval_access_log_list_request"})
			return
		}
		if _, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "", "retrieval_access_log_forbidden"); !ok {
			return
		}
		filter := retrieval.AccessLogListFilter{OrgID: request.OrgID, ProjectID: request.ProjectID, ActorUserID: request.ActorUserID, RequestID: request.RequestID, Limit: request.Limit}
		requests, err := accessLog.ListRequests(filter)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "retrieval_access_log_list_rejected", "message": err.Error()})
			return
		}
		results, err := accessLog.ListResults(filter)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "retrieval_access_log_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"requests": retrievalRequestLogListResponse(requests), "results": retrievalResultLogListResponse(results)})
	}
}

func ArchiveCreateHandler(service archive.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_not_configured"})
			return
		}
		var request archiveCreateRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_archive_create_request"})
			return
		}
		permissions, ok := authorizeArchiveScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:write", "project:"+request.ProjectID+":write")
		if !ok {
			return
		}
		if authService.Configured() {
			request.UserID = permissions.UserID
		}
		result, err := service.Create(archive.CreateRequest{
			RequestID: request.RequestID,
			ArchiveID: request.ArchiveID,
			Title:     request.Title,
			UserID:    request.UserID,
			OrgID:     request.OrgID,
			ProjectID: request.ProjectID,
			CreatedAt: request.CreatedAt,
			Events:    request.Events,
		})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_create_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, archiveMetadataResponse(result.Metadata, result.Deduped))
	}
}

func ArchiveEditHandler(service archive.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_not_configured"})
			return
		}
		var request archiveEditRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_archive_edit_request"})
			return
		}
		metadata, err := service.Metadata(request.ArchiveID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_edit_rejected", "message": err.Error()})
			return
		}
		permissions, ok := authorizeArchiveScope(c, authService, tenantService, metadata.OrgID, metadata.ProjectID, "memory:write", "project:"+metadata.ProjectID+":write")
		if !ok {
			return
		}
		if authService.Configured() {
			request.ActorUserID = permissions.UserID
		}
		result, err := service.Edit(archive.EditRequest{
			RequestID:   request.RequestID,
			ArchiveID:   request.ArchiveID,
			ActorUserID: request.ActorUserID,
			Reason:      request.Reason,
			Content:     request.Content,
		})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_edit_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, archiveMetadataResponse(result.Metadata, result.Deduped))
	}
}

func ArchiveListHandler(service archive.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_not_configured"})
			return
		}
		var request archiveListRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_archive_list_request"})
			return
		}
		permissions, ok := authorizeArchiveScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "")
		if !ok {
			return
		}
		if authService.Configured() {
			request.UserID = permissions.UserID
		}
		archives, err := service.List(archive.ListFilter{UserID: request.UserID, OrgID: request.OrgID, ProjectID: request.ProjectID, Status: request.Status, Limit: request.Limit})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_list_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"archives": archiveListResponse(archives)})
	}
}

func ArchiveDeleteHandler(service archive.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_not_configured"})
			return
		}
		var request archiveDeleteRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_archive_delete_request"})
			return
		}
		metadata, err := service.Metadata(request.ArchiveID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_delete_rejected", "message": err.Error()})
			return
		}
		permissions, ok := authorizeArchiveScope(c, authService, tenantService, metadata.OrgID, metadata.ProjectID, "memory:write", "project:"+metadata.ProjectID+":write")
		if !ok {
			return
		}
		if authService.Configured() {
			request.ActorUserID = permissions.UserID
		}
		result, err := service.Delete(archive.DeleteRequest{RequestID: request.RequestID, ArchiveID: request.ArchiveID, ActorUserID: request.ActorUserID, Reason: request.Reason})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_delete_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, archiveMetadataResponse(result.Metadata, result.Deduped))
	}
}

func ArchiveReindexHandler(service archive.Service, indexQueue archiveIndexEnqueuer, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_not_configured"})
			return
		}
		if indexQueue == nil {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_index_not_configured"})
			return
		}
		var request archiveReindexRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_archive_reindex_request"})
			return
		}
		metadata, err := service.Metadata(request.ArchiveID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_reindex_rejected", "message": err.Error()})
			return
		}
		permissions, ok := authorizeArchiveScope(c, authService, tenantService, metadata.OrgID, metadata.ProjectID, "memory:write", "project:"+metadata.ProjectID+":write")
		if !ok {
			return
		}
		result, err := service.Reindex(archive.ReindexRequest{RequestID: request.RequestID, ArchiveID: request.ArchiveID, Reason: request.Reason})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_reindex_rejected", "message": err.Error()})
			return
		}
		if err := indexQueue.Enqueue(ctx, jobs.RAGIndexJob{
			IdempotencyKey:   request.RequestID,
			OrgID:            result.Metadata.OrgID,
			ProjectID:        result.Metadata.ProjectID,
			UserID:           result.Metadata.UserID,
			Visibility:       "project",
			PermissionLabels: permissions.PermissionLabels,
			Chunks:           result.Chunks,
		}); err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"error": "archive_reindex_enqueue_failed"})
			return
		}
		response := archiveMetadataResponse(result.Metadata, result.Deduped)
		response["chunks"] = len(result.Chunks)
		c.JSON(consts.StatusOK, response)
	}
}

func ArchiveIndexRetryHandler(service archive.Service, indexQueue archiveIndexRetrier, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_not_configured"})
			return
		}
		if indexQueue == nil {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_index_not_configured"})
			return
		}
		var request archiveIDRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_archive_index_retry_request"})
			return
		}
		metadata, err := service.Metadata(request.ArchiveID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_index_retry_rejected", "message": err.Error()})
			return
		}
		if _, ok := authorizeArchiveScope(c, authService, tenantService, metadata.OrgID, metadata.ProjectID, "memory:write", "project:"+metadata.ProjectID+":write"); !ok {
			return
		}
		retried, err := indexQueue.RetryFailed(ctx, metadata.ArchiveID, metadata.IndexGeneration)
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"error": "archive_index_retry_failed"})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{
			"archive_id":        metadata.ArchiveID,
			"index_generation":  metadata.IndexGeneration,
			"retried_jobs":      retried,
			"current_job_state": "pending",
		})
	}
}

func ArchiveIndexStatusHandler(service archive.Service, statusService qdrant.StatusService, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_not_configured"})
			return
		}
		var request archiveIDRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_archive_index_status_request"})
			return
		}
		metadata, err := service.Metadata(request.ArchiveID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_index_status_rejected", "message": err.Error()})
			return
		}
		if _, ok := authorizeArchiveScope(c, authService, tenantService, metadata.OrgID, metadata.ProjectID, "memory:read", ""); !ok {
			return
		}
		stats, err := statusService.ArchiveIndexStats(ctx, metadata.ArchiveID, metadata.IndexGeneration)
		if err != nil {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_index_status_not_configured", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, archiveIndexStatusResponse(stats))
	}
}

func ArchiveDetailHandler(service archive.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_not_configured"})
			return
		}
		var request archiveIDRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_archive_detail_request"})
			return
		}
		metadata, err := service.Metadata(request.ArchiveID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_detail_rejected", "message": err.Error()})
			return
		}
		if _, ok := authorizeArchiveScope(c, authService, tenantService, metadata.OrgID, metadata.ProjectID, "memory:read", ""); !ok {
			return
		}
		detail, err := service.Detail(request.ArchiveID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_detail_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{
			"metadata": archiveMetadataResponse(detail.Metadata, false),
			"content":  detail.Content,
		})
	}
}

func ArchiveVersionsHandler(service archive.Service, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Configured() {
			c.JSON(consts.StatusServiceUnavailable, map[string]string{"error": "archive_not_configured"})
			return
		}
		var request archiveIDRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_archive_versions_request"})
			return
		}
		metadata, err := service.Metadata(request.ArchiveID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_versions_rejected", "message": err.Error()})
			return
		}
		if _, ok := authorizeArchiveScope(c, authService, tenantService, metadata.OrgID, metadata.ProjectID, "memory:read", ""); !ok {
			return
		}
		versions, err := service.Versions(request.ArchiveID)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "archive_versions_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"archive_id": request.ArchiveID, "versions": archiveVersionsResponse(versions)})
	}
}

func archiveListResponse(archives []archive.Metadata) []map[string]any {
	response := make([]map[string]any, 0, len(archives))
	for _, metadata := range archives {
		response = append(response, archiveMetadataResponse(metadata, false))
	}
	return response
}

func hotMemoryListResponse(items []hotmemory.Memory) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, memory := range items {
		response = append(response, hotMemoryResponse(memory))
	}
	return response
}

func hotMemoryResponse(memory hotmemory.Memory) map[string]any {
	return map[string]any{
		"memory_id":         memory.MemoryID,
		"org_id":            memory.OrgID,
		"project_id":        memory.ProjectID,
		"user_id":           memory.UserID,
		"agent_id":          memory.AgentID,
		"scope":             memory.Scope,
		"visibility":        memory.Visibility,
		"permission_labels": memory.PermissionLabels,
		"fact":              memory.Fact,
		"confidence":        memory.Confidence,
		"access_count":      memory.AccessCount,
		"returned_count":    memory.ReturnedCount,
		"used_count":        memory.UsedCount,
		"last_accessed_at":  memory.LastAccessedAt,
		"last_returned_at":  memory.LastReturnedAt,
		"last_used_at":      memory.LastUsedAt,
		"pinned":            memory.Pinned,
		"hot_score":         memory.HotScore,
		"status":            memory.Status,
		"created_at":        memory.CreatedAt,
		"updated_at":        memory.UpdatedAt,
		"deleted_at":        memory.DeletedAt,
	}
}

func filterHotMemories(items []hotmemory.Memory, status string, limit int) []hotmemory.Memory {
	if status == "" {
		status = string(hotmemory.StatusActive)
	}
	response := []hotmemory.Memory{}
	for _, memory := range items {
		if string(memory.Status) != status {
			continue
		}
		response = append(response, memory)
		if limit > 0 && len(response) >= limit {
			return response
		}
	}
	return response
}

func secretListResponse(items []secret.Metadata) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, meta := range items {
		response = append(response, secretMetadataResponse(meta))
	}
	return response
}

func secretMetadataResponse(meta secret.Metadata) map[string]any {
	response := map[string]any{
		"secret_ref":      meta.SecretRef,
		"owner_user_id":   meta.OwnerUserID,
		"org_id":          meta.OrgID,
		"project_id":      meta.ProjectID,
		"name":            meta.Name,
		"env_name":        meta.EnvName,
		"site":            meta.Site,
		"purpose":         meta.Purpose,
		"status":          meta.Status,
		"current_version": meta.CurrentVersion,
	}
	if meta.ExpiresAt != nil {
		response["expires_at"] = meta.ExpiresAt.Format(time.RFC3339)
	}
	return response
}

func patListResponse(items []auth.PATRecord) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, patMetadataResponse(item))
	}
	return response
}

func patMetadataResponse(record auth.PATRecord) map[string]any {
	return map[string]any{
		"id":           record.ID,
		"user_id":      record.SubjectID,
		"name":         record.Name,
		"token_prefix": record.TokenPrefix,
		"scopes":       record.Scopes,
		"expires_at":   record.ExpiresAt,
		"revoked_at":   record.RevokedAt,
		"status":       tokenStatus(record.RevokedAt),
	}
}

func adapterTokenListResponse(items []auth.AdapterTokenRecord) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, adapterTokenMetadataResponse(item))
	}
	return response
}

func adapterTokenMetadataResponse(record auth.AdapterTokenRecord) map[string]any {
	return map[string]any{
		"id":           record.ID,
		"user_id":      record.UserID,
		"org_id":       record.OrgID,
		"project_id":   record.ProjectID,
		"agent_id":     record.AgentID,
		"token_prefix": record.TokenPrefix,
		"scopes":       record.Scopes,
		"expires_at":   record.ExpiresAt,
		"revoked_at":   record.RevokedAt,
		"status":       tokenStatus(record.RevokedAt),
	}
}

func tenantUserResponse(user tenant.User) map[string]any {
	return map[string]any{
		"user_id":      user.ID,
		"email":        user.Email,
		"display_name": user.DisplayName,
		"status":       user.Status,
	}
}

func tenantUserListResponse(items []tenant.User) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, tenantUserResponse(item))
	}
	return response
}

func tenantOrgListResponse(items []tenant.Org) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, tenantOrgResponse(item))
	}
	return response
}

func tenantOrgResponse(org tenant.Org) map[string]any {
	return map[string]any{
		"org_id": org.ID,
		"name":   org.Name,
		"slug":   org.Slug,
		"status": defaultStatus(org.Status),
	}
}

func tenantProjectListResponse(items []tenant.Project) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, tenantProjectResponse(item))
	}
	return response
}

func tenantProjectResponse(project tenant.Project) map[string]any {
	return map[string]any{
		"project_id":  project.ID,
		"org_id":      project.OrgID,
		"name":        project.Name,
		"slug":        project.Slug,
		"status":      defaultStatus(project.Status),
		"source_type": project.SourceType,
		"source_key":  project.SourceKey,
	}
}

func defaultStatus(status string) string {
	if status == "" {
		return "active"
	}
	return status
}

func tenantMembershipListResponse(items []tenant.Membership) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, tenantMembershipResponse(item))
	}
	return response
}

func tenantMembershipResponse(membership tenant.Membership) map[string]any {
	role := membership.Role
	if role == "" {
		role = tenant.RoleMember
	}
	status := membership.Status
	if status == "" {
		status = "active"
	}
	return map[string]any{
		"user_id":    membership.UserID,
		"org_id":     membership.OrgID,
		"project_id": membership.ProjectID,
		"role":       role,
		"status":     status,
	}
}

func tenantRoleDefinitionListResponse(items []tenant.RoleDefinition) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, tenantRoleDefinitionResponse(item))
	}
	return response
}

func tenantRoleDefinitionResponse(item tenant.RoleDefinition) map[string]any {
	return map[string]any{
		"role":              item.Role,
		"display_name":      item.DisplayName,
		"description":       item.Description,
		"permission_labels": item.PermissionLabels,
	}
}

func auditLogListResponse(items []audit.Log) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, map[string]any{
			"id":            item.ID,
			"actor_user_id": item.ActorUserID,
			"org_id":        item.OrgID,
			"project_id":    item.ProjectID,
			"action":        item.Action,
			"resource_type": item.ResourceType,
			"resource_id":   item.ResourceID,
			"request_id":    item.RequestID,
			"result":        item.Result,
			"metadata":      item.Metadata,
			"created_at":    item.CreatedAt,
		})
	}
	return response
}

func recordAudit(service audit.Service, log audit.Log) {
	if !service.Configured() {
		return
	}
	_ = service.Record(log)
}

func auditRequestID(action, resourceID string) string {
	return fmt.Sprintf("%s:%s:%d", action, resourceID, time.Now().UTC().UnixNano())
}

func retrievalRequestLogListResponse(items []retrieval.AccessLogRequestEntry) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, map[string]any{
			"request_id":      item.RequestID,
			"actor_user_id":   item.ActorUserID,
			"org_id":          item.OrgID,
			"project_id":      item.ProjectID,
			"agent_id":        item.AgentID,
			"query_hash":      item.QueryHash,
			"rerank_degraded": item.RerankDegraded,
			"created_at":      item.CreatedAt,
		})
	}
	return response
}

func retrievalResultLogListResponse(items []retrieval.AccessLogResultEntry) []map[string]any {
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, map[string]any{
			"request_id":  item.RequestID,
			"rank":        item.Rank,
			"score":       item.Score,
			"source_kind": item.SourceKind,
			"source_ref":  item.SourceRef,
			"created_at":  item.CreatedAt,
		})
	}
	return response
}

func tokenStatus(revokedAt *time.Time) string {
	if revokedAt != nil {
		return "revoked"
	}
	return "active"
}

func tokenTTL(ttlSeconds int) time.Duration {
	if ttlSeconds <= 0 {
		return time.Hour
	}
	return time.Duration(ttlSeconds) * time.Second
}

func archiveVersionsResponse(versions []archive.Version) []map[string]any {
	response := make([]map[string]any, 0, len(versions))
	for _, version := range versions {
		response = append(response, map[string]any{
			"archive_id":     version.ArchiveID,
			"version":        version.Version,
			"file_path":      version.FilePath,
			"content_hash":   version.ContentHash,
			"editor_user_id": version.EditorUserID,
			"reason":         version.Reason,
			"created_at":     version.CreatedAt,
		})
	}
	return response
}

func qdrantStatusResponse(snapshot qdrant.StatusSnapshot) map[string]any {
	return map[string]any{
		"collection_name":                 snapshot.Collection.Name,
		"collection_status":               snapshot.Collection.Status,
		"points_count":                    snapshot.Collection.PointsCount,
		"vectors_count":                   snapshot.Collection.VectorsCount,
		"indexed_vectors_count":           snapshot.Collection.IndexedVectorsCount,
		"segments_count":                  snapshot.Collection.SegmentsCount,
		"vector_size":                     snapshot.Collection.VectorSize,
		"distance":                        snapshot.Collection.Distance,
		"payload_schema":                  snapshot.Collection.PayloadSchema,
		"points_by_status":                snapshot.PointsByStatus,
		"archive_points_by_status":        snapshot.ArchivePointsByStatus,
		"hot_memory_points_by_status":     snapshot.HotMemoryPointsByStatus,
		"index_jobs_by_status":            snapshot.JobsByStatus,
		"query_time_filter_enforced":      snapshot.QueryTimeFilterEnforced,
		"required_payload_fields":         snapshot.RequiredPayloadFields,
		"missing_required_payload_fields": snapshot.MissingRequiredPayloadFields,
		"latest_point_at":                 snapshot.LatestPointAt,
	}
}

func archiveIndexStatusResponse(stats qdrant.ArchiveIndexStats) map[string]any {
	return map[string]any{
		"archive_id":       stats.ArchiveID,
		"index_generation": stats.IndexGeneration,
		"jobs_by_status":   stats.JobsByStatus,
		"chunks_by_status": stats.ChunksByStatus,
		"points_by_status": stats.PointsByStatus,
		"index_jobs":       stats.IndexJobs,
		"archive_chunks":   stats.ArchiveChunks,
		"latest_point_at":  stats.LatestPointAt,
	}
}

func archiveMetadataResponse(metadata archive.Metadata, deduped bool) map[string]any {
	return map[string]any{
		"archive_id":       metadata.ArchiveID,
		"user_id":          metadata.UserID,
		"org_id":           metadata.OrgID,
		"project_id":       metadata.ProjectID,
		"title":            metadata.Title,
		"file_path":        metadata.FilePath,
		"status":           metadata.Status,
		"index_generation": metadata.IndexGeneration,
		"current_version":  metadata.CurrentVersion,
		"content_hash":     metadata.ContentHash,
		"created_at":       metadata.CreatedAt,
		"updated_at":       metadata.UpdatedAt,
		"deduped":          deduped,
	}
}

func DevArchiveSmokeHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		root, err := os.MkdirTemp("", "memory-os-archive-smoke-*")
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		service := archive.NewService(archive.NewMemoryRepository(), root)
		event := eventlog.TurnEvent{
			Version:   "v1",
			EventID:   "archive_smoke_event_1",
			TurnID:    "archive_smoke_turn_1",
			ThreadID:  "archive_smoke_thread_1",
			SessionID: "archive_smoke_session_1",
			Type:      eventlog.EventUserMessage,
			CreatedAt: time.Now().UTC(),
			Actor:     eventlog.Actor{UserID: "user_smoke", OrgID: "org_smoke", ProjectID: "project_smoke", AgentID: "codex"},
			Payload:   map[string]any{"text": "deploy note secret_ref:secret_ref_archive_smoke"},
		}
		created, err := service.Create(archive.CreateRequest{
			RequestID: "archive-smoke-create",
			ArchiveID: "archive_smoke_1",
			Title:     "Archive Smoke",
			UserID:    "user_smoke",
			OrgID:     "org_smoke",
			ProjectID: "project_smoke",
			CreatedAt: time.Now().UTC(),
			Events:    []eventlog.TurnEvent{event},
		})
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		edited, err := service.Edit(archive.EditRequest{
			RequestID:   "archive-smoke-edit",
			ArchiveID:   created.Metadata.ArchiveID,
			ActorUserID: "user_smoke",
			Reason:      "smoke edit",
			Content:     "# Archive Smoke Edited\n\nsecret_ref:secret_ref_archive_smoke",
		})
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{
			"status":           "ok",
			"archive_id":       edited.Metadata.ArchiveID,
			"version":          edited.Metadata.CurrentVersion,
			"index_generation": edited.Metadata.IndexGeneration,
			"file_path":        edited.Metadata.FilePath,
		})
	}
}

func DevRAGSmokeHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		service := rag.NewService(rag.NewMemoryStore())
		permissions := []string{"project:project_1:read"}
		if err := service.Index(rag.IndexRequest{
			OrgID:            "org_1",
			ProjectID:        "project_1",
			UserID:           "user_1",
			Visibility:       "project",
			PermissionLabels: permissions,
			Chunks: []archive.Chunk{
				{ChunkID: "chunk_old", ArchiveID: "archive_1", IndexGeneration: 1, Content: "old deploy note", ContentHash: "old", SourceEventIDs: []string{"event_old"}},
				{ChunkID: "chunk_new", ArchiveID: "archive_1", IndexGeneration: 2, Content: "new deploy note", ContentHash: "new", SourceEventIDs: []string{"event_new"}},
			},
		}); err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		if err := service.Index(rag.IndexRequest{
			OrgID:            "org_2",
			ProjectID:        "project_2",
			UserID:           "user_2",
			Visibility:       "project",
			PermissionLabels: []string{"project:project_2:read"},
			Chunks:           []archive.Chunk{{ChunkID: "chunk_cross", ArchiveID: "archive_2", IndexGeneration: 2, Content: "cross_tenant_leaked deploy", ContentHash: "cross", SourceEventIDs: []string{"event_cross"}}},
		}); err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		filter, err := qdrant.BuildPayloadFilter(qdrant.FilterContext{
			UserID:           "user_1",
			OrgID:            "org_1",
			ProjectID:        "project_1",
			Visibility:       "project",
			PermissionLabels: permissions,
			DocType:          "archive_chunk",
			IndexGeneration:  2,
		})
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		results, err := service.Search(rag.SearchRequest{Query: "deploy", Filter: filter})
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		if len(results) != 1 || results[0].Source.ChunkID != "chunk_new" {
			c.JSON(consts.StatusInternalServerError, map[string]any{"status": "error", "message": "rag smoke returned unexpected results", "results": len(results)})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{
			"status":   "ok",
			"results":  1,
			"chunk_id": results[0].Source.ChunkID,
		})
	}
}

func DevHotMemorySmokeHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		service := hotmemory.NewService(hotmemory.NewMemoryRepository())
		extractor := hotmemory.NewExtractor()
		event := eventlog.TurnEvent{
			Version:   "v1",
			EventID:   "hot_memory_smoke_event_1",
			TurnID:    "hot_memory_smoke_turn_1",
			ThreadID:  "hot_memory_smoke_thread_1",
			SessionID: "hot_memory_smoke_session_1",
			Type:      eventlog.EventAssistantFinal,
			CreatedAt: time.Now().UTC(),
			Actor:     eventlog.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"},
			Payload:   map[string]any{"text": "Project uses docker compose on T480. Project uses docker compose on T480. token sk-test-redacted-example"},
		}
		candidates := extractor.ExtractFromTurnEvent(event)
		if len(candidates) == 0 {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": "hot memory extractor produced no candidates"})
			return
		}
		permissions := []string{"project:project_1:read"}
		var memory hotmemory.Memory
		for _, candidate := range candidates {
			created, err := service.Upsert(hotmemory.UpsertRequest{
				OrgID:            event.Actor.OrgID,
				ProjectID:        event.Actor.ProjectID,
				UserID:           event.Actor.UserID,
				AgentID:          event.Actor.AgentID,
				Scope:            candidate.Scope,
				Visibility:       "project",
				PermissionLabels: permissions,
				Fact:             candidate.Fact,
				SourceType:       candidate.SourceType,
				SourceRef:        candidate.SourceRef,
				Confidence:       candidate.Confidence,
			})
			if err != nil {
				c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
				return
			}
			memory = created
		}
		filter, err := hotmemory.BuildFilter(hotmemory.FilterContext{OrgID: event.Actor.OrgID, ProjectID: event.Actor.ProjectID, UserID: event.Actor.UserID, AgentID: event.Actor.AgentID, Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: permissions})
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		results, err := service.Search(hotmemory.SearchRequest{Query: "docker", Filter: filter})
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		if len(results) != 1 {
			c.JSON(consts.StatusInternalServerError, map[string]any{"status": "error", "message": "hot memory smoke returned unexpected results", "results": len(results)})
			return
		}
		if _, err := service.Promote(memory.MemoryID); err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		used, err := service.MarkUsed(memory.MemoryID)
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{
			"status":      "ok",
			"results":     len(results),
			"memory_id":   used.MemoryID,
			"used_count":  used.UsedCount,
			"hot_score":   used.HotScore,
			"memory_type": "hot_memory",
		})
	}
}

// --- 候选记忆清洗整合 API ---

type maintenanceRunRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	SourceKey string `json:"source_key"`
	ThreadID  string `json:"thread_id"`
}

func MaintenanceRunHandler(service *candidatememory.MaintenanceService, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request maintenanceRunRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_maintenance_run_request"})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:write", "project:"+request.ProjectID+":write", "maintenance_forbidden")
		if !ok {
			return
		}
		_ = permissions
		run, err := service.StartRun(ctx, candidatememory.MaintenanceRequest{
			OrgID:     request.OrgID,
			ProjectID: request.ProjectID,
			SourceKey: request.SourceKey,
			ThreadID:  request.ThreadID,
			Trigger:   candidatememory.MaintenanceTriggerManual,
		})
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "maintenance_run_rejected", "message": err.Error()})
			return
		}
		// 后台执行清洗,不阻塞 HTTP 请求
		go service.ExecuteRun(context.Background(), candidatememory.MaintenanceRequest{
			OrgID:     request.OrgID,
			ProjectID: request.ProjectID,
			SourceKey: request.SourceKey,
			ThreadID:  request.ThreadID,
			Trigger:   candidatememory.MaintenanceTriggerManual,
		}, run.RunID)
		c.JSON(consts.StatusOK, run.ToStatusDTO())
	}
}

// --- 候选记忆清洗整合状态查询 API ---

type maintenanceStatusRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	RunID     string `json:"run_id"`
}

func MaintenanceStatusHandler(service *candidatememory.MaintenanceService, authService auth.Service, tenantService tenant.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request maintenanceStatusRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_maintenance_status_request"})
			return
		}
		permissions, ok := authorizeProjectScope(c, authService, tenantService, request.OrgID, request.ProjectID, "memory:read", "", "maintenance_forbidden")
		if !ok {
			return
		}
		_ = permissions
		if request.RunID != "" {
			run, err := service.GetRun(ctx, request.RunID)
			if err != nil {
				c.JSON(consts.StatusOK, map[string]any{"active": false})
				return
			}
			dto := run.ToStatusDTO()
			dto.Active = run.Status == candidatememory.MaintenanceRunRunning
			c.JSON(consts.StatusOK, dto)
			return
		}
		// 不传 run_id,返回当前项目正在运行的任务
		active, err := service.GetActiveRun(ctx, request.OrgID, request.ProjectID)
		if err != nil || active == nil {
			c.JSON(consts.StatusOK, map[string]any{"active": false})
			return
		}
		c.JSON(consts.StatusOK, active.ToStatusDTO())
	}
}
