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
	return mux
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
	actor, ok := args["actor"].(map[string]any)
	if !ok {
		actor = map[string]any{}
		args["actor"] = actor
	}
	orgID, _ := actor["org_id"].(string)
	projectID, _ := actor["project_id"].(string)
	agentID, _ := actor["agent_id"].(string)
	var permissions tenant.PermissionContext
	var err error
	if strings.TrimSpace(projectID) == "" {
		identity, identityErr := workspaceFromToolArgs(args["workspace"])
		if identityErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_mcp_workspace", "message": identityErr.Error()})
			return false
		}
		permissions, err = s.TenantService.EnsureWorkspaceProject(record.SubjectID, agentID, identity)
	} else {
		permissions, err = s.TenantService.PermissionContext(record.SubjectID, orgID, projectID, agentID)
	}
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "mcp_forbidden"})
		return false
	}
	actor["user_id"] = permissions.UserID
	actor["org_id"] = permissions.OrgID
	actor["project_id"] = permissions.ProjectID
	actor["agent_id"] = permissions.AgentID
	args["permission_labels"] = permissions.PermissionLabels
	return true
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
