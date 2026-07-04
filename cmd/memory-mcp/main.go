package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/auth"
	"memory-os/internal/config"
	"memory-os/internal/db"
	"memory-os/internal/hotmemory"
	"memory-os/internal/llm"
	"memory-os/internal/logger"
	"memory-os/internal/mcp"
	"memory-os/internal/qdrant"
	"memory-os/internal/rag"
	"memory-os/internal/retrieval"
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
	Addr          string
	Tools         []mcp.Tool
	Handler       mcp.Handler
	AuthService   auth.Service
	TenantService tenant.Service
	RequireAuth   bool
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
	if pool != nil && !(cfg.AppEnv == "development" && cfg.EnableDevEndpoints) {
		service, err := newProductionRetrieval(cfg, pool)
		if err != nil {
			return nil, err
		}
		handler = mcp.NewHandler(mcp.HandlerOptions{Retrieval: service})
		authService = auth.NewService(auth.NewPGRepository(pool))
		tenantService = tenant.NewService(tenant.NewPGRepository(pool))
	}
	return &Server{Addr: cfg.MCPAddr, Tools: mcp.Tools(), Handler: handler, AuthService: authService, TenantService: tenantService, RequireAuth: cfg.AppEnv == "production"}, nil
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

var newProductionRetrieval = func(cfg config.Config, pool *pgxpool.Pool) (retrieval.Service, error) {
	qdrantClient, err := qdrant.NewClient(cfg.QdrantURL)
	if err != nil {
		return retrieval.Service{}, err
	}
	if err := qdrantClient.EnsureCollectionSchema(context.Background(), qdrant.CollectionConfig{Name: qdrant.DefaultCollectionName, VectorSize: qdrant.DefaultVectorSize, Distance: qdrant.DefaultDistance}, qdrant.DefaultPayloadIndexConfigs()); err != nil {
		return retrieval.Service{}, err
	}
	embedder, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, EmbeddingModel: cfg.EmbeddingModel})
	if err != nil {
		return retrieval.Service{}, err
	}
	hot := hotmemory.NewServiceWithVectorIndex(hotmemory.NewPGRepository(pool), hotmemory.NewQdrantIndex(pool, qdrantClient, embedder, qdrant.DefaultCollectionName))
	archiveRAG := rag.NewService(rag.NewQdrantStore(pool, qdrantClient, embedder, qdrant.DefaultCollectionName))
	accessLog := retrieval.NewPGAccessLog(pool)
	return retrieval.NewService(retrieval.Options{HotMemory: hot, ArchiveRAG: archiveRAG, ArchiveGenerationResolver: retrieval.NewPGArchiveGenerationResolver(pool), Reranker: retrieval.FailingReranker{}, AccessLog: accessLog}), nil
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
		response := s.Handler.HandleTool(request.Name, request.Arguments)
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
	response := s.Handler.HandleTool(request.Name, request.Arguments)
	if response.Code != "ok" {
		if response.Error != "" {
			return mcpToolContent(true, response.Error)
		}
		return mcpToolContent(true, response.Code)
	}
	body, err := json.Marshal(response.Search)
	if err != nil {
		return mcpToolContent(true, "failed to encode tool response")
	}
	return mcpToolContent(false, string(body))
}

func (s *Server) authorizeToolCallForMCP(r *http.Request, name string, args map[string]any) bool {
	if !s.RequireAuth || name != "memory_search" {
		return true
	}
	record, ok := s.patRecord(r, "memory:read")
	if !ok {
		return false
	}
	ok, _ = s.applyMemorySearchPermissions(record, r, args)
	return ok
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
	record, ok := s.authorizePATRecord(w, r, "memory:read")
	if !ok {
		return false
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
		return workspace.Identity{}, errors.New("workspace is required when actor project_id is omitted")
	}
	return workspace.Identity{
		CWD:       stringToolArg(raw["cwd"]),
		GitRoot:   stringToolArg(raw["git_root"]),
		GitRemote: stringToolArg(raw["git_remote"]),
		GitBranch: stringToolArg(raw["git_branch"]),
		GitCommit: stringToolArg(raw["git_commit"]),
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
