package http

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

	"memory-os/internal/archive"
	"memory-os/internal/audit"
	"memory-os/internal/auth"
	"memory-os/internal/eventlog"
	"memory-os/internal/health"
	"memory-os/internal/hotmemory"
	"memory-os/internal/qdrant"
	"memory-os/internal/rag"
	"memory-os/internal/retrieval"
	"memory-os/internal/secret"
	"memory-os/internal/tenant"
)

var devEventLogService = eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{MaxTurnEventBytes: 256 * 1024, MaxToolOutputBytes: 64 * 1024})

// NewRouter 创建 Memory API 的 Hertz 路由。
func NewRouter(healthService health.Service) *route.Engine {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, healthService, "test")
	return h.Engine
}

func RegisterRoutes(engine *route.Engine, healthService health.Service, appEnv string) {
	engine.GET("/healthz", HealthHandler(healthService))
	engine.GET("/openapi.json", OpenAPIHandler())
	engine.POST("/memory/turn-event", TurnEventHandler(devEventLogService))
	engine.POST("/memory/search", MemorySearchHandler())
	if appEnv == "development" {
		engine.POST("/dev/smoke/phase2", DevPhase2SmokeHandler())
		engine.POST("/dev/smoke/archive", DevArchiveSmokeHandler())
		engine.POST("/dev/smoke/rag", DevRAGSmokeHandler())
		engine.POST("/dev/smoke/hot-memory", DevHotMemorySmokeHandler())
	}
}

// HealthHandler 返回统一健康检查结果。
func HealthHandler(healthService health.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		report := healthService.Check(ctx)
		c.JSON(consts.StatusOK, report)
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
				"/memory/search": map[string]any{
					"post": map[string]any{
						"summary": "Search unified hot memory and archive RAG",
						"responses": map[string]any{
							"200": map[string]any{"description": "Unified retrieval response"},
						},
					},
				},
			},
		})
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

		codec, err := secret.NewAESGCMCodec("dev-smoke", []byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		vault := secret.NewVault(secret.NewMemoryRepository(), codec)
		meta, err := vault.Create(secret.CreateRequest{OwnerUserID: user.ID, OrgID: org.ID, ProjectID: project.ID, Name: "smoke", Plaintext: "fake-secret-value"})
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		auditRepo := audit.NewMemoryRepository()
		injector := secret.NewInjector(vault, audit.NewService(auditRepo))
		injected, err := injector.Inject(secret.InjectRequest{ActorUserID: user.ID, OrgID: org.ID, ProjectID: project.ID, Tool: "smoke", Purpose: "phase2", RequestID: "phase2-smoke", Template: "TOKEN=${" + meta.SecretRef + "}"})
		if err != nil {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		if !strings.Contains(injected, "fake-secret-value") || len(auditRepo.All()) != 1 {
			c.JSON(consts.StatusInternalServerError, map[string]string{"status": "error", "message": "secret injection smoke failed"})
			return
		}

		c.JSON(consts.StatusOK, map[string]any{
			"status":     "ok",
			"user_id":    user.ID,
			"org_id":     org.ID,
			"project_id": project.ID,
			"secret_ref": meta.SecretRef,
			"audit_logs": len(auditRepo.All()),
		})
	}
}

type turnEventRequest struct {
	RequestID string             `json:"request_id"`
	Event     eventlog.TurnEvent `json:"event"`
}

func TurnEventHandler(service eventlog.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request turnEventRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_turn_event_request"})
			return
		}
		permissions := tenant.PermissionContext{
			UserID:           request.Event.Actor.UserID,
			OrgID:            request.Event.Actor.OrgID,
			ProjectID:        request.Event.Actor.ProjectID,
			AgentID:          request.Event.Actor.AgentID,
			PermissionLabels: []string{"project:" + request.Event.Actor.ProjectID + ":write"},
		}
		result, err := service.Ingest(request.Event, request.RequestID, permissions)
		if err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "turn_event_rejected", "message": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, result)
	}
}

type memorySearchRequest struct {
	RequestID              string   `json:"request_id"`
	Query                  string   `json:"query"`
	Actor                  actorDTO `json:"actor"`
	Scope                  string   `json:"scope"`
	Visibility             string   `json:"visibility"`
	PermissionLabels       []string `json:"permission_labels"`
	ArchiveIndexGeneration int      `json:"archive_index_generation"`
	MaxContextBytes        int      `json:"max_context_bytes"`
}

type actorDTO struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
}

func MemorySearchHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request memorySearchRequest
		if err := c.BindAndValidate(&request); err != nil {
			c.JSON(consts.StatusBadRequest, map[string]string{"error": "invalid_memory_search_request"})
			return
		}
		service := seededRetrievalService()
		response, err := service.Search(retrieval.SearchRequest{
			RequestID:              request.RequestID,
			Query:                  request.Query,
			Actor:                  retrieval.Actor{UserID: request.Actor.UserID, OrgID: request.Actor.OrgID, ProjectID: request.Actor.ProjectID, AgentID: request.Actor.AgentID},
			Scope:                  hotmemory.Scope(request.Scope),
			Visibility:             request.Visibility,
			PermissionLabels:       request.PermissionLabels,
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

func seededRetrievalService() retrieval.Service {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	_, _ = hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Project deploy API with docker compose on T480", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_1", Confidence: 0.8})
	_, _ = hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeAgentSpecific, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Codex private deploy shortcut", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_agent", Confidence: 0.9})
	_, _ = hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_2", ProjectID: "project_2", UserID: "user_2", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_2:read"}, Fact: "cross_tenant_leaked deploy note", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_cross", Confidence: 0.9})

	ragService := rag.NewService(rag.NewMemoryStore())
	_ = ragService.Index(rag.IndexRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Chunks: []archive.Chunk{{ChunkID: "chunk_1", ArchiveID: "archive_1", IndexGeneration: 2, Content: "Archive says deploy API through docker compose on T480", ContentHash: "hash_1", SourceEventIDs: []string{"turn_event_2"}}}})
	_ = ragService.Index(rag.IndexRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Chunks: []archive.Chunk{{ChunkID: "chunk_old", ArchiveID: "archive_1", IndexGeneration: 1, Content: "old deploy API note", ContentHash: "hash_old"}}})

	return retrieval.NewService(retrieval.Options{HotMemory: hot, ArchiveRAG: ragService, Reranker: retrieval.FailingReranker{}, AccessLog: retrieval.NewMemoryAccessLog()})
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
