package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/archive"
	"memory-os/internal/audit"
	"memory-os/internal/auth"
	"memory-os/internal/candidatememory"
	"memory-os/internal/config"
	"memory-os/internal/db"
	"memory-os/internal/eventlog"
	"memory-os/internal/hotmemory"
	"memory-os/internal/jobs"
	"memory-os/internal/llm"
	"memory-os/internal/logger"
	"memory-os/internal/mcp"
	"memory-os/internal/memorystats"
	"memory-os/internal/qdrant"
	"memory-os/internal/rag"
	"memory-os/internal/retrieval"
	"memory-os/internal/secret"
	"memory-os/internal/tenant"
	"memory-os/internal/workspace"
)

var (
	errMissingMCPAddr                   = errors.New("mcp addr is required")
	errMissingProductionPostgresDSN     = errors.New("postgres dsn is required in production")
	errMissingProductionQdrantURL       = errors.New("qdrant url is required in production")
	errInvalidProductionEmbeddingConfig = errors.New("llm embedding config is required in production")
)

type Server struct {
	Addr            string
	Tools           []mcp.Tool
	Handler         mcp.Handler
	AuthService     auth.Service
	TenantService   tenant.Service
	AuditService    audit.Service
	EventLogService eventlog.Service
	ArchiveService  archive.Service
	StatsService    memorystats.Service
	CandidateQueue  candidateEnqueuer
	RequireAuth     bool
}

type candidateEnqueuer interface {
	Enqueue(ctx context.Context, job candidatememory.Job) error
}

type appendEventRequest struct {
	RequestID string             `json:"request_id"`
	Workspace workspace.Identity `json:"workspace"`
	Event     eventlog.TurnEvent `json:"event"`
}

type archiveToolRequest struct {
	RequestID string             `json:"request_id"`
	ArchiveID string             `json:"archive_id"`
	Title     string             `json:"title"`
	Content   string             `json:"content"`
	Workspace workspace.Identity `json:"workspace"`
	Actor     eventlog.Actor     `json:"actor"`
	CreatedAt time.Time          `json:"created_at"`
}

type getArchiveToolRequest struct {
	ArchiveID string `json:"archive_id"`
}

type statsToolRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
}

type toolCallRequest struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpHTTPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log, err := logger.New(mcpLoggerOptions(cfg))
	if err != nil {
		panic(err)
	}
	defer log.Sync() //nolint:errcheck

	server, err := buildServer(cfg)
	if err != nil {
		panic(err)
	}
	log.Info("memory-mcp starting")
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}

func mcpLoggerOptions(cfg config.Config) logger.Options {
	return logger.Options{Environment: cfg.AppEnv, Service: "memory-mcp"}
}

func buildServer(cfg config.Config) (*Server, error) {
	if cfg.MCPAddr == "" {
		return nil, errMissingMCPAddr
	}
	if err := validateProductionConfig(cfg); err != nil {
		return nil, err
	}
	var pool *pgxpool.Pool
	if cfg.PostgresDSN != "" {
		postgresPool, err := db.NewPool(context.Background(), cfg.PostgresDSN)
		if err != nil {
			return nil, err
		}
		if err := db.RunEmbeddedMigrations(context.Background(), postgresPool); err != nil {
			postgresPool.Close()
			return nil, err
		}
		pool = postgresPool
	}
	return buildServerWithPool(cfg, pool)
}

func buildServerWithPool(cfg config.Config, pool *pgxpool.Pool) (*Server, error) {
	if cfg.MCPAddr == "" {
		return nil, errMissingMCPAddr
	}
	if err := validateProductionConfig(cfg); err != nil {
		return nil, err
	}
	handler := mcp.NewHandler(mcp.HandlerOptions{})
	var authService auth.Service
	var tenantService tenant.Service
	var auditService audit.Service
	var eventLogService eventlog.Service
	var archiveService archive.Service
	var statsService memorystats.Service
	var candidateQueue candidateEnqueuer
	if pool != nil && !(cfg.AppEnv == "development" && cfg.EnableDevEndpoints) {
		service, hot, err := newProductionRetrieval(cfg, pool)
		if err != nil {
			return nil, err
		}
		handler = mcp.NewHandler(mcp.HandlerOptions{Retrieval: service, HotMemory: hot})
		authService = auth.NewService(auth.NewPGRepository(pool))
		tenantService = tenant.NewService(tenant.NewPGRepository(pool))
		auditService = audit.NewService(audit.NewPGRepository(pool))
		eventLogService = eventlog.NewService(eventlog.NewPGRepository(pool), eventlog.SanitizerOptions{MaxTurnEventBytes: 256 * 1024, MaxToolOutputBytes: 64 * 1024})
		archiveService = archive.NewService(archive.NewPGRepository(pool), cfg.ArchiveDir)
		statsService = memorystats.NewService(memorystats.NewPGRepository(pool))
		candidateRepo := candidatememory.NewPGRepository(pool)
		candidateQueue = jobs.NewPGCandidateMemoryQueue(candidateRepo, jobs.PGCandidateMemoryQueueOptions{WorkerID: "memory-mcp"})
	}
	return &Server{Addr: cfg.MCPAddr, Tools: mcp.Tools(), Handler: handler, AuthService: authService, TenantService: tenantService, AuditService: auditService, EventLogService: eventLogService, ArchiveService: archiveService, StatsService: statsService, CandidateQueue: candidateQueue, RequireAuth: cfg.AppEnv == "production"}, nil
}

func validateProductionConfig(cfg config.Config) error {
	if cfg.AppEnv != "production" {
		return nil
	}
	if cfg.PostgresDSN == "" {
		return errMissingProductionPostgresDSN
	}
	if cfg.QdrantURL == "" {
		return errMissingProductionQdrantURL
	}
	return validateProductionEmbeddingConfig(cfg)
}

func validateProductionEmbeddingConfig(cfg config.Config) error {
	if strings.TrimSpace(cfg.LLMBaseURL) == "" || strings.TrimSpace(cfg.LLMAPIKey) == "" || strings.TrimSpace(cfg.EmbeddingModel) == "" {
		return errInvalidProductionEmbeddingConfig
	}
	if strings.TrimSpace(cfg.LLMBaseURL) == "http://example.local:8000" || strings.TrimSpace(cfg.LLMAPIKey) == "replace-me" {
		return errInvalidProductionEmbeddingConfig
	}
	return nil
}

var newProductionRetrieval = func(cfg config.Config, pool *pgxpool.Pool) (retrieval.Service, hotmemory.Service, error) {
	qdrantClient, err := qdrant.NewClient(cfg.QdrantURL)
	if err != nil {
		return retrieval.Service{}, hotmemory.Service{}, err
	}
	if err := qdrantClient.EnsureCollectionSchema(context.Background(), qdrant.CollectionConfig{Name: qdrant.DefaultCollectionName, VectorSize: qdrant.DefaultVectorSize, Distance: qdrant.DefaultDistance}, qdrant.DefaultPayloadIndexConfigs()); err != nil {
		return retrieval.Service{}, hotmemory.Service{}, err
	}
	embedder, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, EmbeddingModel: cfg.EmbeddingModel})
	if err != nil {
		return retrieval.Service{}, hotmemory.Service{}, err
	}
	hot := hotmemory.NewServiceWithVectorIndex(hotmemory.NewPGRepository(pool), hotmemory.NewQdrantIndex(pool, qdrantClient, embedder, qdrant.DefaultCollectionName))
	archiveRAG := rag.NewService(rag.NewQdrantStore(pool, qdrantClient, embedder, qdrant.DefaultCollectionName))
	accessLog := retrieval.NewPGAccessLog(pool)
	service := retrieval.NewService(retrieval.Options{HotMemory: hot, ArchiveRAG: archiveRAG, ArchiveGenerationResolver: retrieval.NewPGArchiveGenerationResolver(pool), Reranker: newProductionReranker(cfg), AccessLog: accessLog, MinRerankScore: cfg.RerankMinScore})
	return service, hot, nil
}

var newProductionReranker = func(cfg config.Config) retrieval.Reranker {
	client, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, RerankModel: cfg.RerankModel})
	if err != nil {
		return retrieval.FailingReranker{Err: err}
	}
	return retrieval.NewLLMReranker(client)
}

func (s *Server) ListenAndServe() error {
	return http.ListenAndServe(s.Addr, s.routes())
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !s.authorizePAT(w, r, "memory:read") {
			return
		}
		_ = json.NewEncoder(w).Encode(s.Tools)
	})
	mux.HandleFunc("/tools/call", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "method_not_allowed"})
			return
		}
		var request toolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_tool_call_request"})
			return
		}
		if !s.authorizeToolCall(w, r, request.Name, request.Arguments) {
			return
		}
		response := s.handleTool(r, request.Name, request.Arguments)
		s.recordMemoryToolAudit(r, request.Name, response)
		if response.Code != "ok" {
			w.WriteHeader(http.StatusBadRequest)
		}
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/mcp", s.handleMCP)
	return mux
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Mcp-Protocol-Version", "2025-03-26")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method_not_allowed"})
		return
	}
	if !s.authorizePAT(w, r, "memory:read") {
		return
	}
	var request rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
		return
	}
	if request.JSONRPC != "2.0" || request.Method == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: request.ID, Error: &rpcError{Code: -32600, Message: "invalid request"}})
		return
	}
	if len(request.ID) == 0 && strings.HasPrefix(request.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	response := s.handleMCPRPC(r, request)
	if response.Error != nil {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleMCPRPC(r *http.Request, request rpcRequest) rpcResponse {
	switch request.Method {
	case "initialize":
		return rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "memory-os", "version": "0.4.0"},
		}}
	case "tools/list":
		return rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{"tools": convertHTTPTools(s.Tools)}}
	case "tools/call":
		result := s.handleMCPToolCall(r, request.Params)
		return rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: result}
	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{}}
	default:
		return rpcResponse{JSONRPC: "2.0", ID: request.ID, Error: &rpcError{Code: -32601, Message: "method not found"}}
	}
}

func (s *Server) handleMCPToolCall(r *http.Request, params json.RawMessage) map[string]any {
	var request toolCallRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return mcpToolContent(true, "invalid tools/call params")
	}
	if request.Arguments == nil {
		request.Arguments = map[string]any{}
	}
	if !s.authorizeToolCallForMCP(r, request.Name, request.Arguments) {
		return mcpToolContent(true, "mcp_forbidden")
	}
	response := s.handleTool(r, request.Name, request.Arguments)
	s.recordMemoryToolAudit(r, request.Name, response)
	if response.Code != "ok" {
		if response.Error != "" {
			return mcpToolContent(true, response.Error)
		}
		return mcpToolContent(true, response.Code)
	}
	var payload any
	if response.Search != nil {
		payload = response.Search
	} else {
		payload = response.Result
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return mcpToolContent(true, "failed to encode tool response")
	}
	return mcpToolContent(false, string(body))
}

func (s *Server) authorizeToolCallForMCP(r *http.Request, name string, args map[string]any) bool {
	if !s.RequireAuth {
		return true
	}
	required := requiredToolScope(name)
	record, ok := s.patRecord(r, required)
	if !ok {
		return false
	}
	if name != "memory_search" {
		return true
	}
	ok, _ = s.applyMemorySearchPermissions(record, r, args)
	return ok
}

func (s *Server) handleTool(r *http.Request, name string, args map[string]any) mcp.ToolResponse {
	switch name {
	case "memory_append_event":
		return s.handleAppendEvent(r, args)
	case "memory_stats":
		return s.handleStats(r, args)
	case "memory_get_archive":
		return s.handleGetArchive(r, args)
	case "memory_archive":
		return s.handleArchive(r, args)
	}
	return s.Handler.HandleTool(name, args)
}

func (s *Server) handleStats(r *http.Request, args map[string]any) mcp.ToolResponse {
	if !s.StatsService.Configured() {
		return mcp.ToolResponse{Code: "memory_stats_not_configured", Error: "memory stats service is not configured"}
	}
	var request statsToolRequest
	if err := decodeToolArgs(args, &request); err != nil {
		return mcp.ToolResponse{Code: "invalid_request", Error: err.Error()}
	}
	filter, err := s.statsFilter(r, request)
	if err != nil {
		return mcp.ToolResponse{Code: "memory_stats_rejected", Error: err.Error()}
	}
	snapshot, err := s.StatsService.Snapshot(r.Context(), filter)
	if err != nil {
		return mcp.ToolResponse{Code: "memory_stats_rejected", Error: err.Error()}
	}
	return mcp.ToolResponse{Code: "ok", Result: snapshot}
}

func (s *Server) handleGetArchive(r *http.Request, args map[string]any) mcp.ToolResponse {
	if !s.ArchiveService.Configured() {
		return mcp.ToolResponse{Code: "archive_not_configured", Error: "archive service is not configured"}
	}
	var request getArchiveToolRequest
	if err := decodeToolArgs(args, &request); err != nil {
		return mcp.ToolResponse{Code: "invalid_request", Error: err.Error()}
	}
	request.ArchiveID = strings.TrimSpace(request.ArchiveID)
	if request.ArchiveID == "" {
		return mcp.ToolResponse{Code: "invalid_request", Error: "archive_id is required"}
	}
	detail, err := s.ArchiveService.Detail(request.ArchiveID)
	if err != nil {
		return mcp.ToolResponse{Code: "archive_get_rejected", Error: err.Error()}
	}
	if err := s.authorizeArchiveMetadata(r, detail.Metadata, "memory:read"); err != nil {
		return mcp.ToolResponse{Code: "archive_get_rejected", Error: err.Error()}
	}
	return mcp.ToolResponse{Code: "ok", Result: map[string]any{"metadata": archiveMetadataResult(detail.Metadata, false), "content": detail.Content}}
}

func (s *Server) handleArchive(r *http.Request, args map[string]any) mcp.ToolResponse {
	if !s.ArchiveService.Configured() {
		return mcp.ToolResponse{Code: "archive_not_configured", Error: "archive service is not configured"}
	}
	var request archiveToolRequest
	if err := decodeToolArgs(args, &request); err != nil {
		return mcp.ToolResponse{Code: "invalid_request", Error: err.Error()}
	}
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.ArchiveID = strings.TrimSpace(request.ArchiveID)
	request.Title = strings.TrimSpace(request.Title)
	if request.RequestID == "" || request.Title == "" || strings.TrimSpace(request.Content) == "" {
		return mcp.ToolResponse{Code: "invalid_request", Error: "request_id, title and content are required"}
	}
	if request.ArchiveID == "" {
		request.ArchiveID = archiveIDFromRequest(request.RequestID, request.Title)
	}
	permissions, err := s.archiveToolPermissions(r, &request)
	if err != nil {
		return mcp.ToolResponse{Code: "archive_create_rejected", Error: err.Error()}
	}
	if request.CreatedAt.IsZero() {
		request.CreatedAt = time.Now().UTC()
	}
	content := secret.Sanitize(request.Content, func(index int, match string) string {
		return fmt.Sprintf("secret_ref_archive_%s_%d", request.ArchiveID, index)
	}).Text
	result, err := s.ArchiveService.Create(archive.CreateRequest{
		RequestID: request.RequestID,
		ArchiveID: request.ArchiveID,
		Title:     request.Title,
		UserID:    permissions.UserID,
		OrgID:     permissions.OrgID,
		ProjectID: permissions.ProjectID,
		CreatedAt: request.CreatedAt,
		Markdown:  content,
	})
	if err != nil {
		return mcp.ToolResponse{Code: "archive_create_rejected", Error: err.Error()}
	}
	return mcp.ToolResponse{Code: "ok", Result: archiveMetadataResult(result.Metadata, result.Deduped)}
}

func decodeToolArgs(args map[string]any, target any) error {
	if args == nil {
		return errors.New("arguments are required")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func (s *Server) statsFilter(r *http.Request, request statsToolRequest) (memorystats.Filter, error) {
	request.OrgID = strings.TrimSpace(request.OrgID)
	request.ProjectID = strings.TrimSpace(request.ProjectID)
	if s.RequireAuth {
		record, ok := s.patRecord(r, "memory:read")
		if !ok {
			return memorystats.Filter{}, errors.New("mcp_forbidden")
		}
		if request.OrgID == "" && request.ProjectID == "" {
			return memorystats.Filter{UserID: record.SubjectID}, nil
		}
		permissions, err := s.TenantService.PermissionContext(record.SubjectID, request.OrgID, request.ProjectID, inferAgentIDFromRequest(r))
		if err != nil {
			return memorystats.Filter{}, err
		}
		return memorystats.Filter{UserID: permissions.UserID, OrgID: permissions.OrgID, ProjectID: permissions.ProjectID, PermissionLabels: permissions.PermissionLabels}, nil
	}
	if request.OrgID == "" && request.ProjectID == "" {
		return memorystats.Filter{}, errors.New("user scoped stats require PAT")
	}
	return memorystats.Filter{UserID: "mcp", OrgID: request.OrgID, ProjectID: request.ProjectID, PermissionLabels: []string{"project:" + request.ProjectID + ":read"}}, nil
}

func (s *Server) archiveToolPermissions(r *http.Request, request *archiveToolRequest) (tenant.PermissionContext, error) {
	actor := request.Actor
	if strings.TrimSpace(actor.AgentID) == "" {
		actor.AgentID = inferAgentIDFromRequest(r)
	}
	if s.RequireAuth {
		record, ok := s.patRecord(r, "memory:write")
		if !ok {
			return tenant.PermissionContext{}, errors.New("mcp_forbidden")
		}
		if strings.TrimSpace(actor.ProjectID) == "" {
			return s.TenantService.EnsureWorkspaceProject(record.SubjectID, actor.AgentID, request.Workspace)
		}
		return s.TenantService.PermissionContext(record.SubjectID, actor.OrgID, actor.ProjectID, actor.AgentID)
	}
	if strings.TrimSpace(actor.UserID) == "" || strings.TrimSpace(actor.OrgID) == "" || strings.TrimSpace(actor.ProjectID) == "" {
		return tenant.PermissionContext{}, errors.New("archive actor context is required")
	}
	return tenant.PermissionContext{UserID: actor.UserID, OrgID: actor.OrgID, ProjectID: actor.ProjectID, AgentID: actor.AgentID, PermissionLabels: []string{"project:" + actor.ProjectID + ":write"}}, nil
}

func (s *Server) authorizeArchiveMetadata(r *http.Request, metadata archive.Metadata, requiredScope string) error {
	if !s.RequireAuth {
		return nil
	}
	record, ok := s.patRecord(r, requiredScope)
	if !ok {
		return errors.New("mcp_forbidden")
	}
	_, err := s.TenantService.PermissionContext(record.SubjectID, metadata.OrgID, metadata.ProjectID, inferAgentIDFromRequest(r))
	return err
}

func archiveIDFromRequest(requestID, title string) string {
	base := strings.ToLower(strings.TrimSpace(requestID))
	if base == "" {
		base = strings.ToLower(strings.TrimSpace(title))
	}
	var builder strings.Builder
	for _, r := range base {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if valid {
			builder.WriteRune(r)
			continue
		}
		if builder.Len() > 0 && builder.String()[builder.Len()-1] != '-' {
			builder.WriteByte('-')
		}
		if builder.Len() >= 80 {
			break
		}
	}
	value := strings.Trim(builder.String(), "-")
	if value == "" {
		return fmt.Sprintf("mcp-archive-%d", time.Now().UTC().UnixNano())
	}
	return "mcp-" + value
}

func archiveMetadataResult(metadata archive.Metadata, deduped bool) map[string]any {
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

func (s *Server) handleAppendEvent(r *http.Request, args map[string]any) mcp.ToolResponse {
	if !s.EventLogService.Configured() {
		return mcp.ToolResponse{Code: "eventlog_not_configured", Error: "event log service is not configured"}
	}
	request, err := appendEventRequestFromArgs(args)
	if err != nil {
		return mcp.ToolResponse{Code: "invalid_request", Error: err.Error()}
	}
	permissions, err := s.appendEventPermissions(r, &request)
	if err != nil {
		return mcp.ToolResponse{Code: "memory_append_event_rejected", Error: err.Error()}
	}
	result, err := s.EventLogService.Ingest(request.Event, request.RequestID, permissions)
	if err != nil {
		return mcp.ToolResponse{Code: "memory_append_event_rejected", Error: err.Error()}
	}
	if !result.Deduped && s.CandidateQueue != nil && shouldEnqueueCandidate(result.Event.Type) {
		if job, ok := candidateJobFromAppendEvent(request, result.Event); ok {
			if err := s.CandidateQueue.Enqueue(context.Background(), job); err != nil {
				return mcp.ToolResponse{Code: "candidate_job_enqueue_failed", Error: err.Error()}
			}
		}
	}
	return mcp.ToolResponse{Code: "ok", Result: result}
}

func appendEventRequestFromArgs(args map[string]any) (appendEventRequest, error) {
	if args == nil {
		return appendEventRequest{}, errors.New("arguments are required")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return appendEventRequest{}, err
	}
	var request appendEventRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return appendEventRequest{}, err
	}
	if strings.TrimSpace(request.RequestID) == "" {
		return appendEventRequest{}, errors.New("request_id is required")
	}
	return request, nil
}

func (s *Server) appendEventPermissions(r *http.Request, request *appendEventRequest) (tenant.PermissionContext, error) {
	actor := request.Event.Actor
	if strings.TrimSpace(actor.AgentID) == "" {
		actor.AgentID = inferAgentIDFromRequest(r)
	}
	if s.RequireAuth {
		record, ok := s.patRecord(r, "memory:write")
		if !ok {
			return tenant.PermissionContext{}, errors.New("mcp_forbidden")
		}
		actor.UserID = record.SubjectID
		var permissions tenant.PermissionContext
		var err error
		if strings.TrimSpace(actor.ProjectID) == "" {
			permissions, err = s.TenantService.EnsureWorkspaceProject(record.SubjectID, actor.AgentID, request.Workspace)
		} else {
			permissions, err = s.TenantService.PermissionContext(record.SubjectID, actor.OrgID, actor.ProjectID, actor.AgentID)
		}
		if err != nil {
			return tenant.PermissionContext{}, err
		}
		request.Event.Actor = eventlog.Actor{UserID: permissions.UserID, OrgID: permissions.OrgID, ProjectID: permissions.ProjectID, AgentID: permissions.AgentID}
		return permissions, nil
	}
	if strings.TrimSpace(actor.UserID) == "" || strings.TrimSpace(actor.OrgID) == "" || strings.TrimSpace(actor.ProjectID) == "" {
		return tenant.PermissionContext{}, errors.New("turn event actor context is required")
	}
	request.Event.Actor = actor
	return tenant.PermissionContext{UserID: actor.UserID, OrgID: actor.OrgID, ProjectID: actor.ProjectID, AgentID: actor.AgentID, PermissionLabels: []string{"project:" + actor.ProjectID + ":write"}}, nil
}

func shouldEnqueueCandidate(eventType eventlog.EventType) bool {
	return eventType == eventlog.EventAssistantFinal || eventType == eventlog.EventManualArchive
}

func candidateJobFromAppendEvent(request appendEventRequest, event eventlog.TurnEvent) (candidatememory.Job, bool) {
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

func mcpToolContent(isError bool, text string) map[string]any {
	return map[string]any{
		"isError": isError,
		"content": []map[string]string{{
			"type": "text",
			"text": text,
		}},
	}
}

// recordMemoryToolAudit 记录 MCP 写入类工具的项目审计日志。
func (s *Server) recordMemoryToolAudit(r *http.Request, name string, response mcp.ToolResponse) {
	if response.Code != "ok" || !s.AuditService.Configured() {
		return
	}
	switch name {
	case "memory_mark_used":
		s.recordMarkUsedAudit(r, response)
	case "memory_append_event":
		s.recordAppendEventAudit(r, response)
	case "memory_archive":
		s.recordArchiveCreateAudit(r, response)
	}
}

func (s *Server) recordMarkUsedAudit(r *http.Request, response mcp.ToolResponse) {
	memory, ok := response.Result.(hotmemory.Memory)
	if !ok {
		return
	}
	action := "hot_memory.mark_used"
	_ = s.AuditService.Record(audit.Log{
		ActorUserID:  s.auditActorUserID(r),
		OrgID:        memory.OrgID,
		ProjectID:    memory.ProjectID,
		Action:       action,
		ResourceType: "hot_memory",
		ResourceID:   memory.MemoryID,
		RequestID:    fmt.Sprintf("%s:%s:%d", action, memory.MemoryID, time.Now().UTC().UnixNano()),
		Result:       "ok",
		Metadata:     map[string]string{"used_count": fmt.Sprintf("%d", memory.UsedCount), "source": "mcp"},
	})
}

func (s *Server) recordAppendEventAudit(r *http.Request, response mcp.ToolResponse) {
	result, ok := response.Result.(eventlog.IngestResult)
	if !ok {
		return
	}
	action := "turn_event.append"
	_ = s.AuditService.Record(audit.Log{
		ActorUserID:  s.auditActorUserID(r),
		OrgID:        result.Event.Actor.OrgID,
		ProjectID:    result.Event.Actor.ProjectID,
		Action:       action,
		ResourceType: "turn_event",
		ResourceID:   result.EventID,
		RequestID:    fmt.Sprintf("%s:%s:%d", action, result.EventID, time.Now().UTC().UnixNano()),
		Result:       "ok",
		Metadata:     map[string]string{"event_type": string(result.Event.Type), "deduped": fmt.Sprintf("%t", result.Deduped), "source": "mcp"},
	})
}

func (s *Server) recordArchiveCreateAudit(r *http.Request, response mcp.ToolResponse) {
	result, ok := response.Result.(map[string]any)
	if !ok {
		return
	}
	archiveID, _ := result["archive_id"].(string)
	orgID, _ := result["org_id"].(string)
	projectID, _ := result["project_id"].(string)
	if archiveID == "" || orgID == "" || projectID == "" {
		return
	}
	action := "archive.create"
	_ = s.AuditService.Record(audit.Log{
		ActorUserID:  s.auditActorUserID(r),
		OrgID:        orgID,
		ProjectID:    projectID,
		Action:       action,
		ResourceType: "archive",
		ResourceID:   archiveID,
		RequestID:    fmt.Sprintf("%s:%s:%d", action, archiveID, time.Now().UTC().UnixNano()),
		Result:       "ok",
		Metadata:     map[string]string{"source": "mcp"},
	})
}

func (s *Server) auditActorUserID(r *http.Request) string {
	if s.AuthService.Configured() {
		if record, err := s.AuthService.ValidatePAT(bearerToken(r), time.Now()); err == nil {
			return record.SubjectID
		}
	}
	return ""
}

func convertHTTPTools(tools []mcp.Tool) []mcpHTTPTool {
	converted := make([]mcpHTTPTool, 0, len(tools))
	for _, tool := range tools {
		converted = append(converted, mcpHTTPTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	return converted
}

func (s *Server) authorizeToolCall(w http.ResponseWriter, r *http.Request, name string, args map[string]any) bool {
	if !s.RequireAuth {
		return true
	}
	required := requiredToolScope(name)
	record, ok := s.authorizePATRecord(w, r, required)
	if !ok {
		return false
	}
	if name == "memory_mark_used" || name == "memory_append_event" || name == "memory_archive" {
		return true
	}
	if name != "memory_search" {
		return true
	}
	ok, err := s.applyMemorySearchPermissions(record, r, args)
	if ok {
		return true
	}
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_mcp_workspace", "message": err.Error()})
		return false
	}
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "mcp_forbidden"})
	return false
}

func (s *Server) applyMemorySearchPermissions(record auth.PATRecord, r *http.Request, args map[string]any) (bool, error) {
	actor, ok := args["actor"].(map[string]any)
	if !ok {
		actor = map[string]any{}
		args["actor"] = actor
	}
	orgID, _ := actor["org_id"].(string)
	projectID, _ := actor["project_id"].(string)
	agentID, _ := actor["agent_id"].(string)
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = inferAgentIDFromRequest(r)
		actor["agent_id"] = agentID
	}
	var permissions tenant.PermissionContext
	var err error
	if strings.TrimSpace(projectID) == "" {
		identity, identityErr := workspaceFromToolArgs(args["workspace"])
		if identityErr != nil {
			return false, identityErr
		}
		permissions, err = s.TenantService.EnsureWorkspaceProject(record.SubjectID, agentID, identity)
	} else {
		permissions, err = s.TenantService.PermissionContext(record.SubjectID, orgID, projectID, agentID)
	}
	if err != nil {
		return false, nil
	}
	actor["user_id"] = permissions.UserID
	actor["org_id"] = permissions.OrgID
	actor["project_id"] = permissions.ProjectID
	actor["agent_id"] = permissions.AgentID
	args["permission_labels"] = permissions.PermissionLabels
	return true, nil
}

func workspaceFromToolArgs(value any) (workspace.Identity, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return workspace.Identity{}, nil
	}
	return workspace.Identity{
		CWD:        stringToolArg(raw["cwd"]),
		GitRoot:    stringToolArg(raw["git_root"]),
		GitRemote:  stringToolArg(raw["git_remote"]),
		GitBranch:  stringToolArg(raw["git_branch"]),
		GitCommit:  stringToolArg(raw["git_commit"]),
		SourceType: stringToolArg(raw["source_type"]),
		SourceKey:  stringToolArg(raw["source_key"]),
	}, nil
}

func stringToolArg(value any) string {
	text, _ := value.(string)
	return text
}

func inferAgentIDFromRequest(r *http.Request) string {
	if r == nil {
		return "mcp"
	}
	if explicit := normalizeAgentID(r.Header.Get("X-Memory-Agent-ID")); explicit != "" {
		return explicit
	}
	userAgent := strings.ToLower(r.Header.Get("User-Agent"))
	switch {
	case strings.Contains(userAgent, "claude-code") || strings.Contains(userAgent, "claude code") || strings.Contains(userAgent, "anthropic"):
		return "claude-code"
	case strings.Contains(userAgent, "codex") || strings.Contains(userAgent, "openai"):
		return "codex"
	case strings.Contains(userAgent, "cursor"):
		return "cursor"
	case strings.Contains(userAgent, "opencode"):
		return "opencode"
	case strings.Contains(userAgent, "cline"):
		return "cline"
	case strings.Contains(userAgent, "roo"):
		return "roo"
	case strings.Contains(userAgent, "hermes"):
		return "hermes"
	default:
		return "mcp"
	}
}

func normalizeAgentID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-'
		if valid {
			builder.WriteRune(r)
			lastDash = r == '-'
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
		if builder.Len() >= 64 {
			break
		}
	}
	return strings.Trim(builder.String(), "-")
}

func (s *Server) authorizePAT(w http.ResponseWriter, r *http.Request, requiredScope string) bool {
	_, ok := s.authorizePATRecord(w, r, requiredScope)
	return ok
}

func (s *Server) authorizePATRecord(w http.ResponseWriter, r *http.Request, requiredScope string) (auth.PATRecord, bool) {
	if !s.RequireAuth {
		return auth.PATRecord{}, true
	}
	if !s.AuthService.Configured() || !s.TenantService.Configured() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "auth_not_configured"})
		return auth.PATRecord{}, false
	}
	token := bearerToken(r)
	if token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "pat_required"})
		return auth.PATRecord{}, false
	}
	record, err := s.AuthService.ValidatePAT(token, time.Now())
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_pat"})
		return auth.PATRecord{}, false
	}
	if !mcpPATScopeAllows(record.Scopes, requiredScope) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "mcp_forbidden"})
		return auth.PATRecord{}, false
	}
	return record, true
}

func (s *Server) patRecord(r *http.Request, requiredScope string) (auth.PATRecord, bool) {
	if !s.RequireAuth {
		return auth.PATRecord{}, true
	}
	if !s.AuthService.Configured() || !s.TenantService.Configured() {
		return auth.PATRecord{}, false
	}
	token := bearerToken(r)
	if token == "" {
		return auth.PATRecord{}, false
	}
	record, err := s.AuthService.ValidatePAT(token, time.Now())
	if err != nil {
		return auth.PATRecord{}, false
	}
	if !mcpPATScopeAllows(record.Scopes, requiredScope) {
		return auth.PATRecord{}, false
	}
	return record, true
}

func bearerToken(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}

func mcpPATScopeAllows(scopes []string, required string) bool {
	if required == "" {
		return true
	}
	for _, scope := range scopes {
		if scope == required || (required == "memory:read" && scope == "memory:write") {
			return true
		}
	}
	return false
}

func requiredToolScope(name string) string {
	switch name {
	case "memory_mark_used", "memory_append_event", "memory_archive":
		return "memory:write"
	default:
		return "memory:read"
	}
}
