package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/test/assert"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"memory-os/internal/archive"
	"memory-os/internal/audit"
	"memory-os/internal/auth"
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
)

func TestHealthzReturnsReport(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(router, "GET", "/healthz", nil)

	assert.DeepEqual(t, 200, response.Code)

	var report health.Report
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatalf("healthz response is not health report JSON: %v", err)
	}
	if report.Status != health.StatusOK {
		t.Fatalf("status = %q, want %q", report.Status, health.StatusOK)
	}
}

func TestNotFound(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(router, "GET", "/missing", nil)

	assert.DeepEqual(t, 404, response.Code)
}

func TestCORSPreflightAllowsBrowserAPIRequests(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(
		router,
		"OPTIONS",
		"/memory/search",
		nil,
		ut.Header{Key: "Origin", Value: "http://your-server:18080"},
		ut.Header{Key: "Access-Control-Request-Method", Value: "POST"},
		ut.Header{Key: "Access-Control-Request-Headers", Value: "content-type, authorization"},
	)

	assert.DeepEqual(t, 204, response.Code)
	assert.DeepEqual(t, "http://your-server:18080", string(response.Header().Peek("Access-Control-Allow-Origin")))
	if !strings.Contains(string(response.Header().Peek("Access-Control-Allow-Methods")), "POST") {
		t.Fatalf("Access-Control-Allow-Methods missing POST: %s", response.Header().Peek("Access-Control-Allow-Methods"))
	}
	if !strings.Contains(strings.ToLower(string(response.Header().Peek("Access-Control-Allow-Headers"))), "authorization") {
		t.Fatalf("Access-Control-Allow-Headers missing authorization: %s", response.Header().Peek("Access-Control-Allow-Headers"))
	}
}

func TestCORSHeadersAreAttachedToAPIResponses(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(router, "GET", "/healthz", nil, ut.Header{Key: "Origin", Value: "http://your-server:18080"})

	assert.DeepEqual(t, 200, response.Code)
	assert.DeepEqual(t, "http://your-server:18080", string(response.Header().Peek("Access-Control-Allow-Origin")))
}

func TestOpenAPIJSON(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(router, "GET", "/openapi.json", nil)

	assert.DeepEqual(t, 200, response.Code)

	var spec map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &spec); err != nil {
		t.Fatalf("openapi response is not JSON: %v", err)
	}
	if spec["openapi"] == "" {
		t.Fatal("openapi version is empty")
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("openapi paths missing")
	}
	if _, ok := paths["/healthz"]; !ok {
		t.Fatal("openapi missing /healthz path")
	}
	if _, ok := paths["/version"]; !ok {
		t.Fatal("openapi missing /version path")
	}
	for _, path := range []string{"/memory/secrets/create", "/memory/secrets/list", "/memory/secrets/disable"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("openapi missing %s path", path)
		}
	}
	for _, path := range []string{"/memory/tokens/pat/create", "/memory/tokens/pat/list", "/memory/tokens/pat/revoke", "/memory/tokens/adapter/create", "/memory/tokens/adapter/list", "/memory/tokens/adapter/revoke"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("openapi missing %s path", path)
		}
	}
	for _, path := range []string{"/memory/tenant/users/create", "/memory/tenant/orgs/create", "/memory/tenant/orgs/list", "/memory/tenant/projects/create", "/memory/tenant/projects/list", "/memory/tenant/memberships/add", "/memory/tenant/memberships/list", "/memory/tenant/roles/list"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("openapi missing %s path", path)
		}
	}
	for _, path := range []string{"/memory/audit/list", "/memory/retrieval/access-log/list"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("openapi missing %s path", path)
		}
	}
	if _, ok := paths["/memory/qdrant/status"]; !ok {
		t.Fatal("openapi missing /memory/qdrant/status path")
	}
}

func TestOpenAPICoversRegisteredProductionRoutes(t *testing.T) {
	router := NewRouter(health.NewService(nil))
	response := ut.PerformRequest(router, "GET", "/openapi.json", nil)
	assert.DeepEqual(t, 200, response.Code)

	var spec map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &spec); err != nil {
		t.Fatalf("openapi response is not JSON: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("openapi paths missing")
	}

	source, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("read router.go: %v", err)
	}
	routePattern := regexp.MustCompile(`engine\.(GET|POST)\("([^"]+)"`)
	matches := routePattern.FindAllStringSubmatch(string(source), -1)
	if len(matches) == 0 {
		t.Fatal("no registered routes found in router.go")
	}
	for _, match := range matches {
		method := strings.ToLower(match[1])
		path := match[2]
		if strings.HasPrefix(path, "/dev/") {
			continue
		}
		pathSpec, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("openapi missing registered route %s %s", strings.ToUpper(method), path)
		}
		if _, ok := pathSpec[method]; !ok {
			t.Fatalf("openapi missing method %s for registered route %s", method, path)
		}
	}
}

func TestVersionEndpointReturnsBuildMetadata(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(router, "GET", "/version", nil)

	assert.DeepEqual(t, 200, response.Code)
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("version response is not JSON: %v", err)
	}
	for _, key := range []string{"version", "commit", "build_time", "dirty"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("version response missing %s: %v", key, payload)
		}
	}
}

func TestDevPhase2SmokeDisabledOutsideDevelopment(t *testing.T) {
	router := NewRouter(health.NewService(nil))

	response := ut.PerformRequest(router, "POST", "/dev/smoke/phase2", nil)

	assert.DeepEqual(t, 404, response.Code)
}

func TestRegisterRoutesPanicsInProductionWhenCoreServicesMissing(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("RegisterRoutes should panic when production dependencies are missing")
		}
		if !strings.Contains(recovered.(error).Error(), "production router missing configured services") {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()

	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AppEnv: "production"})
}

func TestDevPhase2SmokeDisabledEvenInDevelopmentWithoutExplicitFlag(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AppEnv: "development"})

	response := ut.PerformRequest(h.Engine, "POST", "/dev/smoke/phase2", nil)

	assert.DeepEqual(t, 404, response.Code)
}

func TestDevPhase2SmokeRunsInDevelopment(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AppEnv: "development", EnableDevEndpoints: true})

	response := ut.PerformRequest(h.Engine, "POST", "/dev/smoke/phase2", nil)

	assert.DeepEqual(t, 200, response.Code)

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("phase2 smoke response is not JSON: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status = %v, want ok", payload["status"])
	}
}

func TestTurnEventEndpointAcceptsEvent(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AppEnv: "development", EnableDevEndpoints: true})

	body := `{"request_id":"request_1","event":{"version":"v1","event_id":"event_1","turn_id":"turn_1","thread_id":"thread_1","session_id":"session_1","type":"user_message","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"token sk-test-redacted-example"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"status":"accepted"`) {
		t.Fatalf("response missing accepted status: %s", response.Body.String())
	}
}

func TestTurnEventEndpointRequiresAdapterTokenWhenAuthConfigured(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService})

	body := `{"request_id":"request_1","event":{"version":"v1","event_id":"event_1","turn_id":"turn_1","thread_id":"thread_1","session_id":"session_1","type":"user_message","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"token sk-test-redacted-example"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 401, response.Code)
}

func TestTurnEventEndpointRejectsAdapterTokenBindingMismatch(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreateAdapterToken(auth.AdapterTokenRequest{UserID: "user_1", OrgID: "org_1", ProjectID: "project_2", AgentID: "codex", Scopes: []string{"turn_event:write"}, TTL: time.Hour})
	if err != nil {
		t.Fatalf("CreateAdapterToken() error = %v", err)
	}
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService})

	body := `{"request_id":"request_1","event":{"version":"v1","event_id":"event_1","turn_id":"turn_1","thread_id":"thread_1","session_id":"session_1","type":"user_message","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"hello"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})

	assert.DeepEqual(t, 403, response.Code)
}

func TestTurnEventEndpointAcceptsValidAdapterToken(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreateAdapterToken(auth.AdapterTokenRequest{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex", Scopes: []string{"turn_event:write"}, TTL: time.Hour})
	if err != nil {
		t.Fatalf("CreateAdapterToken() error = %v", err)
	}
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService})

	body := `{"request_id":"request_1","event":{"version":"v1","event_id":"event_1","turn_id":"turn_1","thread_id":"thread_1","session_id":"session_1","type":"user_message","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"attacker_user","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"hello"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"status":"accepted"`) {
		t.Fatalf("response missing accepted status: %s", response.Body.String())
	}
}

func TestTurnEventEndpointAcceptsPATWorkspaceIdentity(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "mcp", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := tenant.NewService(tenant.NewMemoryRepository())
	user, err := tenantService.CreateUser("workspace-event@example.test", "Workspace Event")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if user.ID != "user_1" {
		t.Fatalf("fixture user id = %q, want user_1", user.ID)
	}
	queue := &fakeArchiveQueue{}
	eventService := eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, EventLogService: eventService, ArchiveQueue: queue, LegacyTurnEventArchive: true})

	body := `{"request_id":"request_workspace_1","workspace":{"git_remote":"git@gitlab.example.com:team/memory-os.git","git_root":"/work/memory-os","cwd":"/work/memory-os","git_branch":"main"},"event":{"version":"v1","event_id":"event_workspace_1","turn_id":"turn_workspace_1","thread_id":"thread_workspace_1","session_id":"session_workspace_1","type":"user_message","created_at":"2026-07-01T00:00:00Z","actor":{"agent_id":"claude-code"},"payload":{"text":"hello workspace"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"status":"accepted"`) {
		t.Fatalf("response missing accepted status: %s", response.Body.String())
	}
	if len(queue.jobs) != 1 {
		t.Fatalf("archive jobs = %d, want 1", len(queue.jobs))
	}
	job := queue.jobs[0]
	if job.UserID != "user_1" || job.OrgID == "" || job.ProjectID == "" {
		t.Fatalf("workspace archive job ids were not resolved: %#v", job)
	}
	if job.Events[0].Actor.AgentID != "claude-code" || job.Events[0].Actor.ProjectID != job.ProjectID {
		t.Fatalf("workspace event actor mismatch: %#v job=%#v", job.Events[0].Actor, job)
	}
}

func TestTenantProjectResponseIncludesWorkspaceSourceMetadata(t *testing.T) {
	payload := tenantProjectResponse(tenant.Project{
		ID:         "project_1",
		OrgID:      "org_1",
		Name:       "Memory OS",
		Slug:       "gitlab-example-com-team-memory-os",
		Status:     "active",
		SourceType: "git",
		SourceKey:  "gitlab.example.com/team/memory-os",
	})

	if payload["source_type"] != "git" || payload["source_key"] != "gitlab.example.com/team/memory-os" {
		t.Fatalf("tenantProjectResponse source metadata mismatch: %#v", payload)
	}
}

func TestTurnEventEndpointLegacyModeEnqueuesArchiveJob(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	queue := &fakeArchiveQueue{}
	eventService := eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), EventLogService: eventService, ArchiveQueue: queue, LegacyTurnEventArchive: true})

	body := `{"request_id":"request_1","event":{"version":"v1","event_id":"event_1","turn_id":"turn_1","thread_id":"thread_1","session_id":"session_1","type":"user_message","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"token sk-test-redacted-example"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, response.Code)
	if len(queue.jobs) != 1 {
		t.Fatalf("archive jobs = %d, want 1", len(queue.jobs))
	}
	job := queue.jobs[0]
	if job.RequestID != "archive_request_1" || job.ArchiveID == "" || job.UserID != "user_1" || job.OrgID != "org_1" || job.ProjectID != "project_1" {
		t.Fatalf("archive job mismatch: %#v", job)
	}
	if len(job.Events) != 1 || job.Events[0].EventID != "event_1" {
		t.Fatalf("archive job events mismatch: %#v", job.Events)
	}
	text, _ := job.Events[0].Payload["text"].(string)
	if strings.Contains(text, "sk-test-redacted-example") || !strings.Contains(text, "secret_ref") {
		t.Fatalf("archive job event was not sanitized: %#v", job.Events[0].Payload)
	}
}

func TestTurnEventEndpointLegacyModeDoesNotEnqueueDuplicateArchiveJob(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	queue := &fakeArchiveQueue{}
	eventService := eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), EventLogService: eventService, ArchiveQueue: queue, LegacyTurnEventArchive: true})

	body := `{"request_id":"request_1","event":{"version":"v1","event_id":"event_1","turn_id":"turn_1","thread_id":"thread_1","session_id":"session_1","type":"user_message","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"hello"}}}`
	first := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	second := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, first.Code)
	assert.DeepEqual(t, 200, second.Code)
	if len(queue.jobs) != 1 {
		t.Fatalf("archive jobs = %d, want 1", len(queue.jobs))
	}
}

type fakeArchiveQueue struct {
	jobs []jobs.ArchiveJob
}

func (q *fakeArchiveQueue) Enqueue(ctx context.Context, job jobs.ArchiveJob) error {
	q.jobs = append(q.jobs, job)
	return nil
}

type fakeArchiveIndexQueue struct {
	jobs            []jobs.RAGIndexJob
	retryArchiveID  string
	retryGeneration int
	retriedJobs     int64
}

func (q *fakeArchiveIndexQueue) Enqueue(ctx context.Context, job jobs.RAGIndexJob) error {
	q.jobs = append(q.jobs, job)
	return nil
}

type fakeCandidateQueue struct {
	jobs []candidatememory.Job
}

func (q *fakeCandidateQueue) Enqueue(ctx context.Context, job candidatememory.Job) error {
	q.jobs = append(q.jobs, job)
	return nil
}

// 默认 LegacyTurnEventArchive=false:不再自动 enqueue archive job(Phase 2 新默认语义)。
func TestTurnEventEndpointDefaultDoesNotEnqueueArchiveJob(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	archiveQueue := &fakeArchiveQueue{}
	candidateQueue := &fakeCandidateQueue{}
	eventService := eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), EventLogService: eventService, ArchiveQueue: archiveQueue, CandidateQueue: candidateQueue})

	body := `{"request_id":"r1","workspace":{"source_key":"github.com/acme/web"},"event":{"version":"v1","event_id":"e1","turn_id":"t1","thread_id":"th1","session_id":"s1","type":"assistant_final","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"u1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"done"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, response.Code)
	if len(archiveQueue.jobs) != 0 {
		t.Fatalf("默认不应 enqueue archive job,得到 %d", len(archiveQueue.jobs))
	}
}

// 默认链路:触发事件类型(assistant_final)+ 有效 source_key → enqueue candidate job,idempotency_key 符合规范。
func TestTurnEventEndpointDefaultEnqueuesCandidateJob(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	candidateQueue := &fakeCandidateQueue{}
	eventService := eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), EventLogService: eventService, CandidateQueue: candidateQueue})

	body := `{"request_id":"r1","workspace":{"source_key":"github.com/acme/web"},"event":{"version":"v1","event_id":"e1","turn_id":"t1","thread_id":"th1","session_id":"s1","type":"assistant_final","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"u1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"done"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, response.Code)
	if len(candidateQueue.jobs) != 1 {
		t.Fatalf("应 enqueue 1 个 candidate job,得到 %d", len(candidateQueue.jobs))
	}
	job := candidateQueue.jobs[0]
	if job.IdempotencyKey != "candidate:project_1:github.com/acme/web:e1:extract" {
		t.Fatalf("idempotency_key 不正确: %s", job.IdempotencyKey)
	}
	if job.SourceKey != "github.com/acme/web" || job.SourceEventID != "e1" || job.OrgID != "org_1" || job.ProjectID != "project_1" {
		t.Fatalf("candidate job 字段不正确: %+v", job)
	}
}

// 缺 source_key(workspace 无 source_key 且无 git_remote 可解析)→ 不写无归属候选,event 仍入库。
func TestTurnEventEndpointCandidateJobRequiresSourceKey(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	candidateQueue := &fakeCandidateQueue{}
	eventService := eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), EventLogService: eventService, CandidateQueue: candidateQueue})

	body := `{"request_id":"r2","workspace":{},"event":{"version":"v1","event_id":"e2","turn_id":"t2","thread_id":"th2","session_id":"s2","type":"assistant_final","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"u1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"done"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, response.Code)
	if len(candidateQueue.jobs) != 0 {
		t.Fatalf("缺 source_key 不应 enqueue candidate,得到 %d", len(candidateQueue.jobs))
	}
}

// 重复 event(deduped)→ 不重复 enqueue candidate。
func TestTurnEventEndpointDoesNotEnqueueDuplicateCandidateJob(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	candidateQueue := &fakeCandidateQueue{}
	eventService := eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), EventLogService: eventService, CandidateQueue: candidateQueue})

	body := `{"request_id":"r3","workspace":{"source_key":"github.com/acme/web"},"event":{"version":"v1","event_id":"e3","turn_id":"t3","thread_id":"th3","session_id":"s3","type":"assistant_final","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"u1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"done"}}}`
	ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	if len(candidateQueue.jobs) != 1 {
		t.Fatalf("重复 event 不应重复 enqueue candidate,得到 %d", len(candidateQueue.jobs))
	}
}

func TestTurnEventEndpointSkipsCandidateForLifecycleStatusEvents(t *testing.T) {
	for _, eventType := range []string{"turn_completed", "turn_failed"} {
		t.Run(eventType, func(t *testing.T) {
			h := server.New(server.WithHostPorts("127.0.0.1:0"))
			candidateQueue := &fakeCandidateQueue{}
			eventService := eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{})
			RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), EventLogService: eventService, CandidateQueue: candidateQueue})

			body := `{"request_id":"r_lifecycle","workspace":{"source_key":"github.com/acme/web"},"event":{"version":"v1","event_id":"e_lifecycle_` + eventType + `","turn_id":"t_lifecycle","thread_id":"th_lifecycle","session_id":"s_lifecycle","type":"` + eventType + `","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"u1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"status only"}}}`
			response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

			assert.DeepEqual(t, 200, response.Code)
			if len(candidateQueue.jobs) != 0 {
				t.Fatalf("%s 不应 enqueue candidate,得到 %d", eventType, len(candidateQueue.jobs))
			}
		})
	}
}

// 非触发事件类型(user_message)→ 不 enqueue candidate。
func TestTurnEventEndpointSkipsCandidateForNonTriggerType(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	candidateQueue := &fakeCandidateQueue{}
	eventService := eventlog.NewService(eventlog.NewMemoryRepository(), eventlog.SanitizerOptions{})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), EventLogService: eventService, CandidateQueue: candidateQueue})

	body := `{"request_id":"r4","workspace":{"source_key":"github.com/acme/web"},"event":{"version":"v1","event_id":"e4","turn_id":"t4","thread_id":"th4","session_id":"s4","type":"user_message","created_at":"2026-07-01T00:00:00Z","actor":{"user_id":"u1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"hi"}}}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/turn-event", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, response.Code)
	if len(candidateQueue.jobs) != 0 {
		t.Fatalf("非触发类型不应 enqueue candidate,得到 %d", len(candidateQueue.jobs))
	}
}

func (q *fakeArchiveIndexQueue) RetryFailed(ctx context.Context, archiveID string, indexGeneration int) (int64, error) {
	q.retryArchiveID = archiveID
	q.retryGeneration = indexGeneration
	if q.retriedJobs == 0 {
		q.retriedJobs = 1
	}
	return q.retriedJobs, nil
}

func TestDevArchiveSmokeRunsInDevelopment(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AppEnv: "development", EnableDevEndpoints: true})

	response := ut.PerformRequest(h.Engine, "POST", "/dev/smoke/archive", nil)

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"index_generation":2`) {
		t.Fatalf("archive smoke missing index generation increment: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "sk-test-redacted-example") {
		t.Fatal("archive smoke leaked fake secret")
	}
}

func TestArchiveCreateReturnsServiceUnavailableWithoutArchiveService(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil)})

	body := archiveCreateBody("archive_prod_1", "request_archive_create_1")
	response := ut.PerformRequest(h.Engine, "POST", "/memory/archive/create", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 503, response.Code)
	if !strings.Contains(response.Body.String(), "archive_not_configured") {
		t.Fatalf("archive create response = %s, want archive_not_configured", response.Body.String())
	}
}

func TestArchiveCreateAndEditUseConfiguredService(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	root := t.TempDir()
	archiveService := archive.NewService(archive.NewMemoryRepository(), root)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), ArchiveService: archiveService})

	createBody := archiveCreateBody("archive_prod_1", "request_archive_create_1")
	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, createResponse.Code)
	var createPayload map[string]any
	if err := json.Unmarshal(createResponse.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("archive create response is not JSON: %v", err)
	}
	if createPayload["archive_id"] != "archive_prod_1" || createPayload["current_version"].(float64) != 1 || createPayload["index_generation"].(float64) != 1 {
		t.Fatalf("archive create payload mismatch: %v", createPayload)
	}
	filePath, _ := createPayload["file_path"].(string)
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("archive file was not written: %v", err)
	}
	if !strings.HasPrefix(filePath, root) || !strings.Contains(string(content), "production archive note") {
		t.Fatalf("archive file mismatch path=%q content=%s", filePath, string(content))
	}

	editBody := `{"request_id":"request_archive_edit_1","archive_id":"archive_prod_1","actor_user_id":"user_1","reason":"production edit","content":"# Edited\n\nsk-test-redacted-example"}`
	editResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/edit", &ut.Body{Body: strings.NewReader(editBody), Len: len(editBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, editResponse.Code)
	var editPayload map[string]any
	if err := json.Unmarshal(editResponse.Body.Bytes(), &editPayload); err != nil {
		t.Fatalf("archive edit response is not JSON: %v", err)
	}
	if editPayload["current_version"].(float64) != 2 || editPayload["index_generation"].(float64) != 2 {
		t.Fatalf("archive edit payload mismatch: %v", editPayload)
	}
	editedContent, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("edited archive file missing: %v", err)
	}
	if strings.Contains(string(editedContent), "sk-test-redacted-example") || !strings.Contains(string(editedContent), "secret_ref") {
		t.Fatalf("archive edit leaked secret-like content: %s", string(editedContent))
	}
}

func TestArchiveDetailAndVersionsUseConfiguredService(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	root := t.TempDir()
	archiveService := archive.NewService(archive.NewMemoryRepository(), root)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), ArchiveService: archiveService})

	createBody := archiveCreateBody("archive_prod_1", "request_archive_create_1")
	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 200, createResponse.Code)
	editBody := `{"request_id":"request_archive_edit_1","archive_id":"archive_prod_1","actor_user_id":"user_1","reason":"production edit","content":"# Edited\n\nsafe content"}`
	editResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/edit", &ut.Body{Body: strings.NewReader(editBody), Len: len(editBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 200, editResponse.Code)

	detailBody := `{"archive_id":"archive_prod_1"}`
	detailResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/detail", &ut.Body{Body: strings.NewReader(detailBody), Len: len(detailBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, detailResponse.Code)
	if !strings.Contains(detailResponse.Body.String(), `"content"`) || !strings.Contains(detailResponse.Body.String(), "safe content") {
		t.Fatalf("archive detail response missing content: %s", detailResponse.Body.String())
	}

	versionsResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/versions", &ut.Body{Body: strings.NewReader(detailBody), Len: len(detailBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, versionsResponse.Code)
	if !strings.Contains(versionsResponse.Body.String(), `"version":1`) || !strings.Contains(versionsResponse.Body.String(), `"version":2`) {
		t.Fatalf("archive versions response mismatch: %s", versionsResponse.Body.String())
	}
}

func TestArchiveDetailReturnsServiceUnavailableWithoutArchiveService(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil)})

	body := `{"archive_id":"archive_prod_1"}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/archive/detail", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 503, response.Code)
	if !strings.Contains(response.Body.String(), "archive_not_configured") {
		t.Fatalf("archive detail response = %s, want archive_not_configured", response.Body.String())
	}
}

func TestArchiveListDeleteAndReindexUseConfiguredService(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	root := t.TempDir()
	archiveService := archive.NewService(archive.NewMemoryRepository(), root)
	indexQueue := &fakeArchiveIndexQueue{}
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), ArchiveService: archiveService, ArchiveIndexQueue: indexQueue})

	createBody := archiveCreateBody("archive_prod_1", "request_archive_create_1")
	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 200, createResponse.Code)

	listBody := `{"user_id":"user_1","org_id":"org_1","project_id":"project_1","status":"active"}`
	listResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/list", &ut.Body{Body: strings.NewReader(listBody), Len: len(listBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 200, listResponse.Code)
	if !strings.Contains(listResponse.Body.String(), `"archive_id":"archive_prod_1"`) {
		t.Fatalf("archive list response mismatch: %s", listResponse.Body.String())
	}

	reindexBody := `{"request_id":"request_archive_reindex_1","archive_id":"archive_prod_1","reason":"manual rebuild"}`
	reindexResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/reindex", &ut.Body{Body: strings.NewReader(reindexBody), Len: len(reindexBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 200, reindexResponse.Code)
	if !strings.Contains(reindexResponse.Body.String(), `"index_generation":2`) {
		t.Fatalf("archive reindex response mismatch: %s", reindexResponse.Body.String())
	}
	if len(indexQueue.jobs) != 1 || indexQueue.jobs[0].IdempotencyKey != "request_archive_reindex_1" || len(indexQueue.jobs[0].Chunks) == 0 {
		t.Fatalf("archive index job mismatch: %#v", indexQueue.jobs)
	}

	deleteBody := `{"request_id":"request_archive_delete_1","archive_id":"archive_prod_1","actor_user_id":"user_1","reason":"cleanup"}`
	deleteResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/delete", &ut.Body{Body: strings.NewReader(deleteBody), Len: len(deleteBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 200, deleteResponse.Code)
	if !strings.Contains(deleteResponse.Body.String(), `"status":"deleted"`) {
		t.Fatalf("archive delete response mismatch: %s", deleteResponse.Body.String())
	}

	listAfterDelete := ut.PerformRequest(h.Engine, "POST", "/memory/archive/list", &ut.Body{Body: strings.NewReader(listBody), Len: len(listBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 200, listAfterDelete.Code)
	if strings.Contains(listAfterDelete.Body.String(), `"archive_id":"archive_prod_1"`) {
		t.Fatalf("deleted archive still appears in active list: %s", listAfterDelete.Body.String())
	}
}

func TestArchiveAPIsRequirePATWhenAuthAndTenantConfigured(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	archiveService := archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, ArchiveService: archiveService})

	body := `{"user_id":"user_1","org_id":"org_1","project_id":"project_1","status":"active"}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/archive/list", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 401, response.Code)
	if !strings.Contains(response.Body.String(), "pat_required") {
		t.Fatalf("archive list response = %s, want pat_required", response.Body.String())
	}
}

func TestArchiveAPIsRejectPATWithoutProjectMembership(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	archiveService := archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_2", "reader", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, ArchiveService: archiveService})

	body := `{"user_id":"user_2","org_id":"org_1","project_id":"project_1","status":"active"}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/archive/list", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})

	assert.DeepEqual(t, 403, response.Code)
	if !strings.Contains(response.Body.String(), "archive_forbidden") {
		t.Fatalf("archive list response = %s, want archive_forbidden", response.Body.String())
	}
}

func TestArchiveCreateUsesPATSubjectAndRequiresWritePermission(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	archiveService := archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	authService := auth.NewService(auth.NewMemoryRepository())
	memberToken, _, err := authService.CreatePAT("user_1", "reader", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(member) error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleMember)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, ArchiveService: archiveService})

	createBody := archiveCreateBody("archive_auth_1", "request_archive_auth_1")
	createAsMember := ut.PerformRequest(h.Engine, "POST", "/memory/archive/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + memberToken})
	assert.DeepEqual(t, 403, createAsMember.Code)

	h = server.New(server.WithHostPorts("127.0.0.1:0"))
	archiveService = archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	authService = auth.NewService(auth.NewMemoryRepository())
	ownerToken, _, err := authService.CreatePAT("user_1", "writer", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner) error = %v", err)
	}
	tenantService = archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, ArchiveService: archiveService})
	createBody = strings.Replace(archiveCreateBody("archive_auth_2", "request_archive_auth_2"), `"user_id":"user_1"`, `"user_id":"attacker_user"`, 1)

	createAsOwner := ut.PerformRequest(h.Engine, "POST", "/memory/archive/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})

	assert.DeepEqual(t, 200, createAsOwner.Code)
	if !strings.Contains(createAsOwner.Body.String(), `"user_id":"user_1"`) || strings.Contains(createAsOwner.Body.String(), "attacker_user") {
		t.Fatalf("archive create did not use PAT subject: %s", createAsOwner.Body.String())
	}
}

func TestArchiveReindexUsesPermissionContextLabels(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	archiveService := archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	indexQueue := &fakeArchiveIndexQueue{}
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "writer", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, ArchiveService: archiveService, ArchiveIndexQueue: indexQueue})
	createBody := archiveCreateBody("archive_auth_reindex_1", "request_archive_auth_reindex_create_1")
	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, createResponse.Code)

	reindexBody := `{"request_id":"request_archive_auth_reindex_1","archive_id":"archive_auth_reindex_1","reason":"manual rebuild"}`
	reindexResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/reindex", &ut.Body{Body: strings.NewReader(reindexBody), Len: len(reindexBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})

	assert.DeepEqual(t, 200, reindexResponse.Code)
	if len(indexQueue.jobs) != 1 || !contains(indexQueue.jobs[0].PermissionLabels, "project:project_1:write") {
		t.Fatalf("index job permission labels mismatch: %#v", indexQueue.jobs)
	}
}

func TestArchiveReindexReturnsServiceUnavailableWithoutIndexQueue(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	archiveService := archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), ArchiveService: archiveService})

	body := `{"request_id":"request_archive_reindex_1","archive_id":"archive_prod_1","reason":"manual rebuild"}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/archive/reindex", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 503, response.Code)
	if !strings.Contains(response.Body.String(), "archive_index_not_configured") {
		t.Fatalf("archive reindex response = %s, want archive_index_not_configured", response.Body.String())
	}
}

func TestArchiveIndexRetryRequiresWritePermission(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	archiveService := archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	indexQueue := &fakeArchiveIndexQueue{}
	authService := auth.NewService(auth.NewMemoryRepository())
	ownerToken, _, err := authService.CreatePAT("user_1", "writer", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner) error = %v", err)
	}
	memberToken, _, err := authService.CreatePAT("user_1", "member-writer", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(member) error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, ArchiveService: archiveService, ArchiveIndexQueue: indexQueue})

	createBody := archiveCreateBody("archive_retry_1", "request_archive_retry_create_1")
	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, createResponse.Code)

	body := `{"archive_id":"archive_retry_1"}`
	noToken := ut.PerformRequest(h.Engine, "POST", "/memory/archive/index-retry", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, noToken.Code)

	tenantService = archiveTenantService(t, tenant.RoleMember)
	h = server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, ArchiveService: archiveService, ArchiveIndexQueue: indexQueue})
	member := ut.PerformRequest(h.Engine, "POST", "/memory/archive/index-retry", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + memberToken})
	assert.DeepEqual(t, 403, member.Code)

	tenantService = archiveTenantService(t, tenant.RoleOwner)
	h = server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, ArchiveService: archiveService, ArchiveIndexQueue: indexQueue})
	owner := ut.PerformRequest(h.Engine, "POST", "/memory/archive/index-retry", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, owner.Code)
	if indexQueue.retryArchiveID != "archive_retry_1" || indexQueue.retryGeneration != 1 {
		t.Fatalf("retry queue scope mismatch: archive=%q generation=%d", indexQueue.retryArchiveID, indexQueue.retryGeneration)
	}
	for _, want := range []string{`"archive_id":"archive_retry_1"`, `"index_generation":1`, `"retried_jobs":1`} {
		if !strings.Contains(owner.Body.String(), want) {
			t.Fatalf("archive index retry response missing %s: %s", want, owner.Body.String())
		}
	}
}

func TestArchiveIndexStatusRequiresArchivePermission(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	archiveService := archive.NewService(archive.NewMemoryRepository(), t.TempDir())
	authService := auth.NewService(auth.NewMemoryRepository())
	ownerToken, _, err := authService.CreatePAT("user_1", "reader", []string{"memory:read", "memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner) error = %v", err)
	}
	outsiderToken, _, err := authService.CreatePAT("user_2", "reader", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(outsider) error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	status := qdrant.NewStatusService(qdrant.StatusOptions{
		Client:         fakeQdrantStatusClient{},
		Store:          fakeQdrantStatusStore{},
		CollectionName: qdrant.DefaultCollectionName,
	})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, ArchiveService: archiveService, QdrantStatusService: status})

	createBody := archiveCreateBody("archive_status_1", "request_archive_status_create_1")
	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/archive/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, createResponse.Code)

	body := `{"archive_id":"archive_status_1"}`
	noToken := ut.PerformRequest(h.Engine, "POST", "/memory/archive/index-status", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, noToken.Code)

	outsider := ut.PerformRequest(h.Engine, "POST", "/memory/archive/index-status", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + outsiderToken})
	assert.DeepEqual(t, 403, outsider.Code)

	owner := ut.PerformRequest(h.Engine, "POST", "/memory/archive/index-status", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, owner.Code)
	responseBody := owner.Body.String()
	for _, want := range []string{
		`"archive_id":"archive_status_1"`,
		`"index_generation":1`,
		`"jobs_by_status":{"pending":1}`,
		`"chunks_by_status":{"pending":3}`,
		`"points_by_status":{"indexed":2}`,
		`"index_jobs":[{"idempotency_key":"rag_archive_status_1_g1","status":"pending","error_message":"temporary index failure","attempts":1,"max_attempts":3`,
		`"archive_chunks":[{"chunk_id":"archive_status_1_g1_c0","chunk_index":0,"vector_status":"pending","content_hash":"hash_archive_status_1_c0"`,
		`"qdrant_point_id":"point_archive_status_1_c0","qdrant_vector_status":"indexed"`,
	} {
		if !strings.Contains(responseBody, want) {
			t.Fatalf("archive index status response missing %s: %s", want, responseBody)
		}
	}
}

func TestSecretAPIsRequirePATWhenAuthAndTenantConfigured(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	vault := secret.NewVault(secret.NewMemoryRepository(), testSecretCodec(t))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, SecretVault: vault})

	body := `{"org_id":"org_1","project_id":"project_1","name":"api-key","plaintext":"fake-secret-value"}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/secrets/create", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 401, response.Code)
	if !strings.Contains(response.Body.String(), "pat_required") {
		t.Fatalf("secret create response = %s, want pat_required", response.Body.String())
	}
}

func TestSecretCreateRequiresWritePermissionAndReturnsMetadataOnly(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	vault := secret.NewVault(secret.NewMemoryRepository(), testSecretCodec(t))
	authService := auth.NewService(auth.NewMemoryRepository())
	memberToken, _, err := authService.CreatePAT("user_1", "reader", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(member) error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleMember)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, SecretVault: vault})

	body := `{"org_id":"org_1","project_id":"project_1","name":"api-key","plaintext":"fake-secret-value"}`
	memberResponse := ut.PerformRequest(h.Engine, "POST", "/memory/secrets/create", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + memberToken})
	assert.DeepEqual(t, 403, memberResponse.Code)

	h = server.New(server.WithHostPorts("127.0.0.1:0"))
	vault = secret.NewVault(secret.NewMemoryRepository(), testSecretCodec(t))
	authService = auth.NewService(auth.NewMemoryRepository())
	ownerToken, _, err := authService.CreatePAT("user_1", "writer", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner) error = %v", err)
	}
	tenantService = archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, SecretVault: vault})

	ownerResponse := ut.PerformRequest(h.Engine, "POST", "/memory/secrets/create", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})

	assert.DeepEqual(t, 200, ownerResponse.Code)
	if strings.Contains(ownerResponse.Body.String(), "fake-secret-value") || strings.Contains(ownerResponse.Body.String(), "ciphertext") {
		t.Fatalf("secret create leaked secret material: %s", ownerResponse.Body.String())
	}
	if !strings.Contains(ownerResponse.Body.String(), `"secret_ref"`) || !strings.Contains(ownerResponse.Body.String(), `"status":"active"`) {
		t.Fatalf("secret create response missing metadata: %s", ownerResponse.Body.String())
	}
}

func TestSecretListAndDisableUsePATSubjectAndPermission(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	vault := secret.NewVault(secret.NewMemoryRepository(), testSecretCodec(t))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "writer", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, SecretVault: vault})
	createBody := `{"org_id":"org_1","project_id":"project_1","name":"api-key","plaintext":"fake-secret-value"}`
	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/secrets/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, createResponse.Code)
	var created map[string]any
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("secret create response is not JSON: %v", err)
	}
	secretRef, _ := created["secret_ref"].(string)
	if secretRef == "" {
		t.Fatalf("secret_ref missing: %v", created)
	}

	listBody := `{"user_id":"attacker_user","org_id":"org_1","project_id":"project_1","status":"active"}`
	listResponse := ut.PerformRequest(h.Engine, "POST", "/memory/secrets/list", &ut.Body{Body: strings.NewReader(listBody), Len: len(listBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, listResponse.Code)
	if strings.Contains(listResponse.Body.String(), "fake-secret-value") || strings.Contains(listResponse.Body.String(), "ciphertext") {
		t.Fatalf("secret list leaked secret material: %s", listResponse.Body.String())
	}
	if !strings.Contains(listResponse.Body.String(), secretRef) || !strings.Contains(listResponse.Body.String(), `"owner_user_id":"user_1"`) {
		t.Fatalf("secret list did not use PAT subject: %s", listResponse.Body.String())
	}

	disableBody := `{"secret_ref":"` + secretRef + `","org_id":"org_1","project_id":"project_1"}`
	disableResponse := ut.PerformRequest(h.Engine, "POST", "/memory/secrets/disable", &ut.Body{Body: strings.NewReader(disableBody), Len: len(disableBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, disableResponse.Code)
	if !strings.Contains(disableResponse.Body.String(), `"status":"disabled"`) {
		t.Fatalf("secret disable response mismatch: %s", disableResponse.Body.String())
	}
}

func TestQdrantStatusRequiresPATAndConfiguredService(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService})

	response := ut.PerformRequest(h.Engine, "POST", "/memory/qdrant/status", nil)

	assert.DeepEqual(t, 503, response.Code)
	if !strings.Contains(response.Body.String(), "qdrant_status_not_configured") {
		t.Fatalf("qdrant status response = %s, want qdrant_status_not_configured", response.Body.String())
	}

	h = server.New(server.WithHostPorts("127.0.0.1:0"))
	status := qdrant.NewStatusService(qdrant.StatusOptions{
		Client:         fakeQdrantStatusClient{},
		CollectionName: qdrant.DefaultCollectionName,
	})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, QdrantStatusService: status})

	response = ut.PerformRequest(h.Engine, "POST", "/memory/qdrant/status", nil)

	assert.DeepEqual(t, 401, response.Code)
	if !strings.Contains(response.Body.String(), "pat_required") {
		t.Fatalf("qdrant status response = %s, want pat_required", response.Body.String())
	}
}

func TestQdrantStatusUsesRealServiceAndReturnsIndexStats(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "reader", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	status := qdrant.NewStatusService(qdrant.StatusOptions{
		Client:         fakeQdrantStatusClient{},
		Store:          fakeQdrantStatusStore{},
		CollectionName: qdrant.DefaultCollectionName,
	})
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, QdrantStatusService: status})

	response := ut.PerformRequest(h.Engine, "POST", "/memory/qdrant/status", nil, ut.Header{Key: "Authorization", Value: "Bearer " + token})

	assert.DeepEqual(t, 200, response.Code)
	body := response.Body.String()
	for _, want := range []string{`"collection_name":"memory_os"`, `"collection_status":"green"`, `"points_count":5`, `"indexed":4`, `"archive_points_by_status"`, `"hot_memory_points_by_status"`, `"promoted":1`, `"pending":2`, `"query_time_filter_enforced":false`, `"missing_required_payload_fields":["doc_type","index_generation","permission_labels","visibility"]`, `"user_id"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("qdrant status response missing %s: %s", want, body)
		}
	}
}

func TestTokenAPIsRequirePATWhenAuthConfigured(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	body := `{"name":"web","scopes":["memory:read"],"ttl_seconds":3600}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/tokens/pat/create", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 401, response.Code)
	if !strings.Contains(response.Body.String(), "pat_required") {
		t.Fatalf("pat create response = %s, want pat_required", response.Body.String())
	}
}

func TestTenantUserListRequiresPATAndReturnsUserMetadata(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := tenant.NewService(tenant.NewMemoryRepository())
	alice, err := tenantService.CreateUser("alice-users@example.com", "Alice Users")
	if err != nil {
		t.Fatalf("CreateUser(alice) error = %v", err)
	}
	if _, err := tenantService.CreateUser("bob-users@example.com", "Bob Users"); err != nil {
		t.Fatalf("CreateUser(bob) error = %v", err)
	}
	token, _, err := authService.CreatePAT(alice.ID, "reader", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	unauthorized := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/users/list", nil)
	assert.DeepEqual(t, 401, unauthorized.Code)

	response := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/users/list", nil, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, response.Code)
	body := response.Body.String()
	for _, want := range []string{`"users"`, `"email":"alice-users@example.com"`, `"display_name":"Bob Users"`, `"status":"active"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("tenant users list response missing %s: %s", want, body)
		}
	}
	for _, forbidden := range []string{"password", "credential", "token", "hash"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("tenant users list leaked forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestTenantUserUpdateStatusRequiresWritePATAndReturnsMetadata(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := tenant.NewService(tenant.NewMemoryRepository())
	admin, err := tenantService.CreateUser("admin-users-status@example.com", "Admin Users Status")
	if err != nil {
		t.Fatalf("CreateUser(admin) error = %v", err)
	}
	target, err := tenantService.CreateUser("target-users-status@example.com", "Target Users Status")
	if err != nil {
		t.Fatalf("CreateUser(target) error = %v", err)
	}
	readToken, _, err := authService.CreatePAT(admin.ID, "reader", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(reader) error = %v", err)
	}
	writeToken, _, err := authService.CreatePAT(admin.ID, "writer", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(writer) error = %v", err)
	}
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	body := `{"user_id":"` + target.ID + `","status":"disabled"}`
	unauthorized := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/users/update-status", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, unauthorized.Code)

	forbidden := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/users/update-status", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + readToken})
	assert.DeepEqual(t, 403, forbidden.Code)

	response := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/users/update-status", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + writeToken})
	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"user_id":"`+target.ID+`"`) || !strings.Contains(response.Body.String(), `"status":"disabled"`) {
		t.Fatalf("update status response missing user metadata: %s", response.Body.String())
	}
	for _, forbiddenField := range []string{"password", "credential", "token", "hash"} {
		if strings.Contains(response.Body.String(), forbiddenField) {
			t.Fatalf("tenant user update leaked forbidden field %q: %s", forbiddenField, response.Body.String())
		}
	}
}

func TestPATTokenLifecycleUsesPATSubjectAndReturnsPlainOnce(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	adminToken, _, err := authService.CreatePAT("user_1", "admin", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(admin) error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	auditRepo := audit.NewMemoryRepository()
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, AuditService: audit.NewService(auditRepo)})

	createBody := `{"user_id":"attacker_user","name":"web","scopes":["memory:read"],"ttl_seconds":3600}`
	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tokens/pat/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + adminToken})
	assert.DeepEqual(t, 200, createResponse.Code)
	if strings.Contains(createResponse.Body.String(), "token_hash") || strings.Contains(createResponse.Body.String(), "hash") {
		t.Fatalf("pat create leaked hash material: %s", createResponse.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("pat create response is not JSON: %v", err)
	}
	plain, _ := created["token"].(string)
	if !strings.HasPrefix(plain, "pat_") {
		t.Fatalf("pat create token = %q, want pat_ prefix", plain)
	}
	metadata, _ := created["token_metadata"].(map[string]any)
	id, _ := metadata["id"].(string)
	if id == "" || metadata["user_id"] != "user_1" {
		t.Fatalf("pat metadata mismatch: %v", metadata)
	}

	listBody := `{"user_id":"attacker_user","status":"active"}`
	listResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tokens/pat/list", &ut.Body{Body: strings.NewReader(listBody), Len: len(listBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + adminToken})
	assert.DeepEqual(t, 200, listResponse.Code)
	if strings.Contains(listResponse.Body.String(), plain) || strings.Contains(listResponse.Body.String(), "token_hash") || strings.Contains(listResponse.Body.String(), "hash") {
		t.Fatalf("pat list leaked token material: %s", listResponse.Body.String())
	}
	if !strings.Contains(listResponse.Body.String(), id) || !strings.Contains(listResponse.Body.String(), `"user_id":"user_1"`) {
		t.Fatalf("pat list did not use PAT subject: %s", listResponse.Body.String())
	}

	revokeBody := `{"token_id":"` + id + `"}`
	revokeResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tokens/pat/revoke", &ut.Body{Body: strings.NewReader(revokeBody), Len: len(revokeBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + adminToken})
	assert.DeepEqual(t, 200, revokeResponse.Code)
	if !strings.Contains(revokeResponse.Body.String(), `"status":"revoked"`) {
		t.Fatalf("pat revoke response mismatch: %s", revokeResponse.Body.String())
	}
	logs := auditRepo.All()
	if len(logs) != 2 {
		t.Fatalf("PAT lifecycle should write create/revoke audit logs, got %#v", logs)
	}
	if logs[0].Action != "token.pat.create" || logs[0].ActorUserID != "user_1" || logs[0].ResourceType != "pat" || logs[0].ResourceID != id || logs[0].Result != "ok" {
		t.Fatalf("PAT create audit log mismatch: %#v", logs[0])
	}
	if logs[1].Action != "token.pat.revoke" || logs[1].ActorUserID != "user_1" || logs[1].ResourceType != "pat" || logs[1].ResourceID != id || logs[1].Result != "ok" {
		t.Fatalf("PAT revoke audit log mismatch: %#v", logs[1])
	}
	for _, log := range logs {
		if strings.Contains(strings.Join(mapValues(log.Metadata), " "), plain) || strings.Contains(strings.Join(mapValues(log.Metadata), " "), "token_hash") {
			t.Fatalf("PAT audit log leaked token material: %#v", log)
		}
	}
}

func TestPATCreateReturnsInstallCodeAndBootstrapConsumesOnce(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	adminToken, _, err := authService.CreatePAT("user_1", "admin", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(admin) error = %v", err)
	}
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: archiveTenantService(t, tenant.RoleOwner)})

	createBody := `{"name":"memory-os-mcp","scopes":["memory:read","memory:write"],"ttl_seconds":3600}`
	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tokens/pat/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + adminToken})
	assert.DeepEqual(t, 200, createResponse.Code)
	var created map[string]any
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("pat create response is not JSON: %v", err)
	}
	plain, _ := created["token"].(string)
	installCode, _ := created["install_code"].(string)
	setupCommand, _ := created["setup_command"].(string)
	if !strings.HasPrefix(installCode, "mosc_") {
		t.Fatalf("install_code = %q, want mosc_ prefix", installCode)
	}
	if strings.Contains(setupCommand, plain) || !strings.Contains(setupCommand, "curl -fsSL") || !strings.Contains(setupCommand, "--code "+installCode+" --agent auto") {
		t.Fatalf("setup_command should contain install code but not token: %s", setupCommand)
	}

	bootstrapBody := `{"code":"` + installCode + `"}`
	bootstrapResponse := ut.PerformRequest(h.Engine, "POST", "/memory/setup/bootstrap", &ut.Body{Body: strings.NewReader(bootstrapBody), Len: len(bootstrapBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 200, bootstrapResponse.Code)
	var bootstrap map[string]any
	if err := json.Unmarshal(bootstrapResponse.Body.Bytes(), &bootstrap); err != nil {
		t.Fatalf("bootstrap response is not JSON: %v", err)
	}
	if bootstrap["token"] != plain || bootstrap["api_url"] == "" || bootstrap["mcp_url"] == "" {
		t.Fatalf("bootstrap config mismatch: %#v", bootstrap)
	}
	if agents, ok := bootstrap["agents"].([]any); !ok || len(agents) == 0 {
		t.Fatalf("bootstrap agents missing: %#v", bootstrap)
	}

	replayResponse := ut.PerformRequest(h.Engine, "POST", "/memory/setup/bootstrap", &ut.Body{Body: strings.NewReader(bootstrapBody), Len: len(bootstrapBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 404, replayResponse.Code)
	if strings.Contains(replayResponse.Body.String(), plain) {
		t.Fatalf("bootstrap replay leaked token: %s", replayResponse.Body.String())
	}
}

func TestSetupInstallScriptRegistersMainstreamAgentMCPWithoutPlainToken(t *testing.T) {
	script := setupInstallScript()
	for _, required := range []string{
		".claude/.mcp.json",
		".codex",
		"opencode",
		".hermes",
		".openclaw",
		"mcpServers",
		"memory-os",
		"bearer_token_env_var",
		"{env:MEMORY_OS_TOKEN}",
		"Bearer ${MEMORY_OS_TOKEN}",
		"MEMORY_OS_TOKEN",
		"secrets.env",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("setup install script missing mainstream Agent MCP marker %q", required)
		}
	}
	for _, forbidden := range []string{
		`"Authorization": "Bearer " + config["token"]`,
		`ANTHROPIC_AUTH_TOKEN`,
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("setup install script must not write plaintext token marker %q", forbidden)
		}
	}
}

func TestSetupInstallScriptConfiguresMainstreamAgentMCP(t *testing.T) {
	bootstrapToken := "pat_test_install_token"
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodPost || r.URL.Path != "/memory/setup/bootstrap" {
			t.Fatalf("unexpected bootstrap request %s %s", r.Method, r.URL.Path)
		}
		baseURL := "http://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(setupBootstrapConfig{
			Server: baseURL,
			APIURL: baseURL,
			MCPURL: baseURL + "/mcp",
			Token:  bootstrapToken,
			Agents: []string{"codex", "claude-code", "opencode", "hermes", "openclaw"},
		})
	}))
	defer server.Close()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("create preexisting codex dir: %v", err)
	}
	preexistingCodexConfig := strings.Join([]string{
		`model = "gpt-5"`,
		``,
		`[mcp_servers.memory-os]`,
		`url = "http://192.168.188.20:18082/mcp"`,
		`bearer_token_env_var = "MEMORY_OS_TOKEN"`,
		`startup_timeout_sec = 30`,
		`tool_timeout_sec = 60`,
		``,
		`[mcp_servers.other]`,
		`url = "http://127.0.0.1:9999/mcp"`,
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(preexistingCodexConfig), 0o600); err != nil {
		t.Fatalf("write preexisting codex config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatalf("create preexisting claude dir: %v", err)
	}
	preexistingClaudeSettings := `{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash ` + filepath.ToSlash(filepath.Join(home, ".claude", "scripts", "mem0_load.sh")) + `"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash /Users/kanyun/.claude/scripts/mem0_save.sh"
          }
        ]
      },
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash /Users/kanyun/.claude/scripts/memory_os_turn_event.sh"
          }
        ]
      }
    ]
  }
}
`
	if err := os.MkdirAll(filepath.Join(home, ".claude", "scripts"), 0o755); err != nil {
		t.Fatalf("create preexisting claude scripts dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "scripts", "mem0_load.sh"), []byte("#!/usr/bin/env sh\n"), 0o700); err != nil {
		t.Fatalf("write existing mem0 hook script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(preexistingClaudeSettings), 0o600); err != nil {
		t.Fatalf("write preexisting Claude Code settings: %v", err)
	}
	scriptPath := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(scriptPath, []byte(setupInstallScript()), 0o700); err != nil {
		t.Fatalf("write install script: %v", err)
	}
	cmd := exec.Command("sh", scriptPath, "--server", server.URL, "--code", "mosc_test", "--agent", "auto")
	cmd.Env = append(os.Environ(), "HOME="+home)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install script failed: %v\n%s", err, output)
	}

	secretPath := filepath.Join(home, ".config", "ai-secrets", "secrets.env")
	secretBytes, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("read secrets.env: %v", err)
	}
	if !strings.Contains(string(secretBytes), "MEMORY_OS_TOKEN='"+bootstrapToken+"'") {
		t.Fatalf("secrets.env missing token assignment: %s", secretBytes)
	}
	if !strings.Contains(string(secretBytes), "MEMORY_OS_API_URL='"+server.URL+"'") {
		t.Fatalf("secrets.env missing API URL assignment: %s", secretBytes)
	}
	if info, err := os.Stat(secretPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets.env mode = %v, %v; want 0600", info, err)
	}

	claudeBytes, err := os.ReadFile(filepath.Join(home, ".claude", ".mcp.json"))
	if err != nil {
		t.Fatalf("read .claude/.mcp.json: %v", err)
	}
	if strings.Contains(string(claudeBytes), bootstrapToken) {
		t.Fatalf(".claude/.mcp.json leaked plaintext token: %s", claudeBytes)
	}
	var claudeConfig map[string]any
	if err := json.Unmarshal(claudeBytes, &claudeConfig); err != nil {
		t.Fatalf(".claude/.mcp.json is not JSON: %v", err)
	}
	servers := claudeConfig["mcpServers"].(map[string]any)
	memoryOS := servers["memory-os"].(map[string]any)
	headers := memoryOS["headers"].(map[string]any)
	if memoryOS["type"] != "http" || memoryOS["url"] != server.URL+"/mcp" || headers["Authorization"] != "Bearer ${MEMORY_OS_TOKEN}" {
		t.Fatalf("unexpected memory-os MCP config: %#v", memoryOS)
	}

	codexBytes, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("read codex config: %v", err)
	}
	codexConfig := string(codexBytes)
	for _, marker := range []string{
		`[mcp_servers.memory-os]`,
		`url = "` + server.URL + `/mcp"`,
		`bearer_token_env_var = "MEMORY_OS_TOKEN"`,
		`[mcp_servers.other]`,
	} {
		if !strings.Contains(codexConfig, marker) {
			t.Fatalf("codex config missing marker %q:\n%s", marker, codexConfig)
		}
	}
	if strings.Count(codexConfig, "[mcp_servers.memory-os]") != 1 || strings.Contains(codexConfig, "192.168.188.20:18082") {
		t.Fatalf("codex config must replace old memory-os section without duplicates:\n%s", codexConfig)
	}

	opencodeBytes, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode config: %v", err)
	}
	if strings.Contains(string(opencodeBytes), bootstrapToken) {
		t.Fatalf("opencode config leaked plaintext token: %s", opencodeBytes)
	}
	var opencodeConfig map[string]any
	if err := json.Unmarshal(opencodeBytes, &opencodeConfig); err != nil {
		t.Fatalf("opencode config is not JSON: %v", err)
	}
	opencodeMCP := opencodeConfig["mcp"].(map[string]any)["memory-os"].(map[string]any)
	opencodeHeaders := opencodeMCP["headers"].(map[string]any)
	if opencodeMCP["type"] != "remote" || opencodeMCP["url"] != server.URL+"/mcp" || opencodeHeaders["Authorization"] != "Bearer {env:MEMORY_OS_TOKEN}" {
		t.Fatalf("unexpected opencode MCP config: %#v", opencodeMCP)
	}

	hermesBytes, err := os.ReadFile(filepath.Join(home, ".hermes", "config.yaml"))
	if err != nil {
		t.Fatalf("read hermes config: %v", err)
	}
	hermesConfig := string(hermesBytes)
	for _, marker := range []string{
		"mcp_servers:",
		"memory-os:",
		"url: \"" + server.URL + "/mcp\"",
		"Authorization: \"Bearer ${MEMORY_OS_TOKEN}\"",
		"enabled: true",
	} {
		if !strings.Contains(hermesConfig, marker) {
			t.Fatalf("hermes config missing marker %q:\n%s", marker, hermesConfig)
		}
	}

	openclawBytes, err := os.ReadFile(filepath.Join(home, ".openclaw", "openclaw.json"))
	if err != nil {
		t.Fatalf("read openclaw config: %v", err)
	}
	if strings.Contains(string(openclawBytes), bootstrapToken) {
		t.Fatalf("openclaw config leaked plaintext token: %s", openclawBytes)
	}
	var openclawConfig map[string]any
	if err := json.Unmarshal(openclawBytes, &openclawConfig); err != nil {
		t.Fatalf("openclaw config is not JSON: %v", err)
	}
	openclawMCP := openclawConfig["mcp"].(map[string]any)["servers"].(map[string]any)["memory-os"].(map[string]any)
	openclawHeaders := openclawMCP["headers"].(map[string]any)
	if openclawMCP["type"] != "http" || openclawMCP["url"] != server.URL+"/mcp" || openclawHeaders["Authorization"] != "Bearer ${MEMORY_OS_TOKEN}" {
		t.Fatalf("unexpected openclaw MCP config: %#v", openclawMCP)
	}

	hookScript := filepath.Join(home, ".memory-os", "hooks", "turn_event.py")
	hookBytes, err := os.ReadFile(hookScript)
	if err != nil {
		t.Fatalf("read common hook script: %v", err)
	}
	if strings.Contains(string(hookBytes), bootstrapToken) {
		t.Fatalf("common hook script leaked plaintext token: %s", hookBytes)
	}
	for _, marker := range []string{"/memory/turn-event", "MEMORY_OS_TOKEN", "MEMORY_OS_HOOK_AGENT"} {
		if !strings.Contains(string(hookBytes), marker) {
			t.Fatalf("common hook script missing marker %q", marker)
		}
	}
	for _, marker := range []string{
		`DEFAULT_MEMORY_OS_API_URL = "` + server.URL + `"`,
		"def workspace_identity(cwd):",
		`"local/" + abs_cwd.lstrip("/")`,
		`"workspace": workspace_identity(cwd)`,
	} {
		if !strings.Contains(string(hookBytes), marker) {
			t.Fatalf("common hook script missing non-git workspace fallback marker %q", marker)
		}
	}

	claudeSettingsBytes, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read Claude Code settings: %v", err)
	}
	var claudeSettings map[string]any
	if err := json.Unmarshal(claudeSettingsBytes, &claudeSettings); err != nil {
		t.Fatalf("Claude Code settings are not JSON: %v", err)
	}
	if !strings.Contains(string(claudeSettingsBytes), "MEMORY_OS_HOOK_AGENT=claude-code") || !strings.Contains(string(claudeSettingsBytes), hookScript) {
		t.Fatalf("Claude Code settings missing Memory OS Stop hook: %s", claudeSettingsBytes)
	}
	if strings.Contains(string(claudeSettingsBytes), "mem0_save.sh") {
		t.Fatalf("Claude Code settings must prune missing local hook scripts: %s", claudeSettingsBytes)
	}
	if !strings.Contains(string(claudeSettingsBytes), "mem0_load.sh") {
		t.Fatalf("Claude Code settings must preserve existing non-Memory OS hooks: %s", claudeSettingsBytes)
	}
	if strings.Contains(string(claudeSettingsBytes), "memory_os_turn_event.sh") || strings.Count(string(claudeSettingsBytes), "MEMORY_OS_HOOK_AGENT=claude-code") != 1 {
		t.Fatalf("Claude Code settings must replace old Memory OS hooks without duplicates: %s", claudeSettingsBytes)
	}

	codexHooksBytes, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	if err != nil {
		t.Fatalf("read Codex hooks: %v", err)
	}
	if !strings.Contains(string(codexHooksBytes), "MEMORY_OS_HOOK_AGENT=codex") || !strings.Contains(string(codexHooksBytes), hookScript) {
		t.Fatalf("Codex hooks missing Memory OS Stop hook: %s", codexHooksBytes)
	}

	opencodePluginBytes, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "plugins", "memory-os.js"))
	if err != nil {
		t.Fatalf("read opencode plugin: %v", err)
	}
	if !strings.Contains(string(opencodePluginBytes), "session.idle") || !strings.Contains(string(opencodePluginBytes), "MEMORY_OS_HOOK_AGENT") {
		t.Fatalf("opencode plugin missing Memory OS session hook: %s", opencodePluginBytes)
	}

	hermesPluginBytes, err := os.ReadFile(filepath.Join(home, ".hermes", "plugins", "memory_os_hook.py"))
	if err != nil {
		t.Fatalf("read Hermes plugin: %v", err)
	}
	if !strings.Contains(string(hermesPluginBytes), "post_llm_call") || !strings.Contains(string(hermesPluginBytes), "MEMORY_OS_HOOK_AGENT") {
		t.Fatalf("Hermes plugin missing Memory OS post_llm_call hook: %s", hermesPluginBytes)
	}

	openclawPluginBytes, err := os.ReadFile(filepath.Join(home, ".openclaw", "plugins", "memory-os.js"))
	if err != nil {
		t.Fatalf("read OpenClaw plugin: %v", err)
	}
	if !strings.Contains(string(openclawPluginBytes), "message:sent") || !strings.Contains(string(openclawPluginBytes), "MEMORY_OS_HOOK_AGENT") {
		t.Fatalf("OpenClaw plugin missing Memory OS message hook: %s", openclawPluginBytes)
	}
}

func TestPasswordLoginEndpointReturnsShortLivedPATAndUserMetadata(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := tenant.NewService(tenant.NewMemoryRepository())
	user, err := tenantService.CreateUser("login-password@example.com", "Password Login")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if err := authService.SetPassword(user.ID, "correct-password"); err != nil {
		t.Fatalf("SetPassword() error = %v", err)
	}
	auditRepo := audit.NewMemoryRepository()
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, AuditService: audit.NewService(auditRepo)})

	body := `{"email":"LOGIN-password@example.com","password":"correct-password"}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/auth/login-password", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 200, response.Code)
	for _, forbidden := range []string{"password_hash", "token_hash", "credential"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("password login leaked forbidden field %q: %s", forbidden, response.Body.String())
		}
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("password login response is not JSON: %v", err)
	}
	token, _ := payload["token"].(string)
	if !strings.HasPrefix(token, "pat_") {
		t.Fatalf("password login token = %q, want pat_ prefix", token)
	}
	userMetadata, _ := payload["user"].(map[string]any)
	if userMetadata["user_id"] != user.ID || userMetadata["email"] != "login-password@example.com" {
		t.Fatalf("password login user metadata mismatch: %v", userMetadata)
	}

	record, err := authService.ValidatePAT(token, time.Now())
	if err != nil {
		t.Fatalf("ValidatePAT() error = %v", err)
	}
	if record.SubjectID != user.ID || !strings.HasPrefix(record.Name, "password-login") {
		t.Fatalf("password login PAT metadata = %#v, want subject %s and password-login name", record, user.ID)
	}
	logs := auditRepo.All()
	if len(logs) != 1 {
		t.Fatalf("password login should write one audit log, got %#v", logs)
	}
	if logs[0].Action != "auth.password_login" || logs[0].ActorUserID != user.ID || logs[0].ResourceType != "user" || logs[0].ResourceID != user.ID || logs[0].Result != "ok" {
		t.Fatalf("password login audit log mismatch: %#v", logs[0])
	}
	metadataValues := strings.Join(mapValues(logs[0].Metadata), " ")
	for _, forbidden := range []string{"correct-password", token, "token_hash", "password_hash"} {
		if strings.Contains(metadataValues, forbidden) {
			t.Fatalf("password login audit log leaked forbidden value %q: %#v", forbidden, logs[0])
		}
	}
}

func TestPasswordLoginEndpointRejectsInvalidCredentialsAndDisabledUser(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := tenant.NewService(tenant.NewMemoryRepository())
	activeUser, err := tenantService.CreateUser("login-active@example.com", "Login Active")
	if err != nil {
		t.Fatalf("CreateUser(active) error = %v", err)
	}
	if err := authService.SetPassword(activeUser.ID, "correct-password"); err != nil {
		t.Fatalf("SetPassword(active) error = %v", err)
	}
	disabledUser, err := tenantService.CreateUser("login-disabled@example.com", "Login Disabled")
	if err != nil {
		t.Fatalf("CreateUser(disabled) error = %v", err)
	}
	if err := authService.SetPassword(disabledUser.ID, "correct-password"); err != nil {
		t.Fatalf("SetPassword(disabled) error = %v", err)
	}
	if _, err := tenantService.UpdateUserStatus(disabledUser.ID, "disabled"); err != nil {
		t.Fatalf("UpdateUserStatus(disabled) error = %v", err)
	}
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	wrongPassword := `{"email":"login-active@example.com","password":"wrong-password"}`
	wrongResponse := ut.PerformRequest(h.Engine, "POST", "/memory/auth/login-password", &ut.Body{Body: strings.NewReader(wrongPassword), Len: len(wrongPassword)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, wrongResponse.Code)

	missingUser := `{"email":"missing@example.com","password":"correct-password"}`
	missingResponse := ut.PerformRequest(h.Engine, "POST", "/memory/auth/login-password", &ut.Body{Body: strings.NewReader(missingUser), Len: len(missingUser)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, missingResponse.Code)

	disabledPassword := `{"email":"login-disabled@example.com","password":"correct-password"}`
	disabledResponse := ut.PerformRequest(h.Engine, "POST", "/memory/auth/login-password", &ut.Body{Body: strings.NewReader(disabledPassword), Len: len(disabledPassword)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 403, disabledResponse.Code)
}

func TestAdapterTokenLifecycleRequiresProjectWriteAndReturnsMetadataOnlyOnList(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	memberToken, _, err := authService.CreatePAT("user_1", "member", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(member) error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleMember)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	createBody := `{"org_id":"org_1","project_id":"project_1","agent_id":"codex","scopes":["turn_event:write"],"ttl_seconds":3600}`
	memberResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tokens/adapter/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + memberToken})
	assert.DeepEqual(t, 403, memberResponse.Code)

	h = server.New(server.WithHostPorts("127.0.0.1:0"))
	authService = auth.NewService(auth.NewMemoryRepository())
	ownerToken, _, err := authService.CreatePAT("user_1", "owner", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner) error = %v", err)
	}
	tenantService = archiveTenantService(t, tenant.RoleOwner)
	auditRepo := audit.NewMemoryRepository()
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, AuditService: audit.NewService(auditRepo)})

	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tokens/adapter/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, createResponse.Code)
	if strings.Contains(createResponse.Body.String(), "token_hash") || strings.Contains(createResponse.Body.String(), "hash") {
		t.Fatalf("adapter create leaked hash material: %s", createResponse.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("adapter create response is not JSON: %v", err)
	}
	plain, _ := created["token"].(string)
	if !strings.HasPrefix(plain, "adapter_") {
		t.Fatalf("adapter token = %q, want adapter_ prefix", plain)
	}
	metadata, _ := created["token_metadata"].(map[string]any)
	id, _ := metadata["id"].(string)
	if id == "" || metadata["user_id"] != "user_1" || metadata["project_id"] != "project_1" {
		t.Fatalf("adapter metadata mismatch: %v", metadata)
	}

	listBody := `{"user_id":"attacker_user","org_id":"org_1","project_id":"project_1","agent_id":"codex","status":"active"}`
	listResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tokens/adapter/list", &ut.Body{Body: strings.NewReader(listBody), Len: len(listBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, listResponse.Code)
	if strings.Contains(listResponse.Body.String(), plain) || strings.Contains(listResponse.Body.String(), "token_hash") || strings.Contains(listResponse.Body.String(), "hash") {
		t.Fatalf("adapter list leaked token material: %s", listResponse.Body.String())
	}
	if !strings.Contains(listResponse.Body.String(), id) || !strings.Contains(listResponse.Body.String(), `"user_id":"user_1"`) {
		t.Fatalf("adapter list did not use PAT subject: %s", listResponse.Body.String())
	}

	revokeBody := `{"token_id":"` + id + `"}`
	revokeResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tokens/adapter/revoke", &ut.Body{Body: strings.NewReader(revokeBody), Len: len(revokeBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, revokeResponse.Code)
	if !strings.Contains(revokeResponse.Body.String(), `"status":"revoked"`) {
		t.Fatalf("adapter revoke response mismatch: %s", revokeResponse.Body.String())
	}
	logs := auditRepo.All()
	if len(logs) != 2 {
		t.Fatalf("adapter token lifecycle should write create/revoke audit logs, got %#v", logs)
	}
	if logs[0].Action != "token.adapter.create" || logs[0].OrgID != "org_1" || logs[0].ProjectID != "project_1" || logs[0].ResourceType != "adapter_token" || logs[0].ResourceID != id || logs[0].Result != "ok" {
		t.Fatalf("adapter token create audit log mismatch: %#v", logs[0])
	}
	if logs[1].Action != "token.adapter.revoke" || logs[1].OrgID != "org_1" || logs[1].ProjectID != "project_1" || logs[1].ResourceType != "adapter_token" || logs[1].ResourceID != id || logs[1].Result != "ok" {
		t.Fatalf("adapter token revoke audit log mismatch: %#v", logs[1])
	}
	for _, log := range logs {
		if strings.Contains(strings.Join(mapValues(log.Metadata), " "), plain) || strings.Contains(strings.Join(mapValues(log.Metadata), " "), "token_hash") {
			t.Fatalf("adapter token audit log leaked token material: %#v", log)
		}
	}
}

func TestTenantAPIsRequirePATWhenAuthConfigured(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := tenantServiceWithUser(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	body := `{"name":"Org API","slug":"org-api"}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/create", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 401, response.Code)
	if !strings.Contains(response.Body.String(), "pat_required") {
		t.Fatalf("tenant org create response = %s, want pat_required", response.Body.String())
	}
}

func TestTenantOrgProjectAndMembershipManagementUsesPATSubject(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	adminToken, _, err := authService.CreatePAT("user_1", "admin", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(admin) error = %v", err)
	}
	tenantService := tenantServiceWithUser(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	orgBody := `{"name":"Org API","slug":"org-api"}`
	orgResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/create", &ut.Body{Body: strings.NewReader(orgBody), Len: len(orgBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + adminToken})
	assert.DeepEqual(t, 200, orgResponse.Code)
	var orgCreated map[string]any
	if err := json.Unmarshal(orgResponse.Body.Bytes(), &orgCreated); err != nil {
		t.Fatalf("org create response is not JSON: %v", err)
	}
	org, _ := orgCreated["org"].(map[string]any)
	orgID, _ := org["org_id"].(string)
	if orgID == "" {
		t.Fatalf("org create response missing org_id: %v", orgCreated)
	}
	if membership, _ := orgCreated["owner_membership"].(map[string]any); membership["user_id"] != "user_1" || membership["role"] != tenant.RoleOwner {
		t.Fatalf("owner membership mismatch: %v", orgCreated)
	}

	projectBody := `{"org_id":"` + orgID + `","name":"Project API","slug":"project-api"}`
	projectResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/create", &ut.Body{Body: strings.NewReader(projectBody), Len: len(projectBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + adminToken})
	assert.DeepEqual(t, 200, projectResponse.Code)
	var projectCreated map[string]any
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &projectCreated); err != nil {
		t.Fatalf("project create response is not JSON: %v", err)
	}
	project, _ := projectCreated["project"].(map[string]any)
	projectID, _ := project["project_id"].(string)
	if projectID == "" || project["org_id"] != orgID {
		t.Fatalf("project create response mismatch: %v", projectCreated)
	}

	memberBody := `{"user_id":"user_2","org_id":"` + orgID + `","project_id":"` + projectID + `","role":"member"}`
	memberResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/add", &ut.Body{Body: strings.NewReader(memberBody), Len: len(memberBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + adminToken})
	assert.DeepEqual(t, 200, memberResponse.Code)
	if !strings.Contains(memberResponse.Body.String(), `"user_id":"user_2"`) || !strings.Contains(memberResponse.Body.String(), `"role":"member"`) {
		t.Fatalf("membership add response mismatch: %s", memberResponse.Body.String())
	}

	orgListResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/list", &ut.Body{Body: strings.NewReader(`{"user_id":"attacker_user"}`), Len: len(`{"user_id":"attacker_user"}`)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + adminToken})
	assert.DeepEqual(t, 200, orgListResponse.Code)
	if !strings.Contains(orgListResponse.Body.String(), orgID) || strings.Contains(orgListResponse.Body.String(), "attacker_user") {
		t.Fatalf("org list did not use PAT subject: %s", orgListResponse.Body.String())
	}

	projectListBody := `{"org_id":"` + orgID + `"}`
	projectListResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/list", &ut.Body{Body: strings.NewReader(projectListBody), Len: len(projectListBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + adminToken})
	assert.DeepEqual(t, 200, projectListResponse.Code)
	if !strings.Contains(projectListResponse.Body.String(), projectID) {
		t.Fatalf("project list missing created project: %s", projectListResponse.Body.String())
	}

	membershipListBody := `{"org_id":"` + orgID + `","project_id":"` + projectID + `"}`
	membershipListResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/list", &ut.Body{Body: strings.NewReader(membershipListBody), Len: len(membershipListBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + adminToken})
	assert.DeepEqual(t, 200, membershipListResponse.Code)
	if !strings.Contains(membershipListResponse.Body.String(), `"user_id":"user_2"`) {
		t.Fatalf("membership list missing member: %s", membershipListResponse.Body.String())
	}
}

func TestTenantOrgProjectDeleteRequiresWritePermission(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	ownerToken, _, err := authService.CreatePAT("user_1", "owner", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner) error = %v", err)
	}
	memberToken, _, err := authService.CreatePAT("user_2", "member", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(member) error = %v", err)
	}
	tenantService := tenantServiceWithUser(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	orgBody := `{"name":"Org Delete API","slug":"org-delete-api"}`
	orgResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/create", &ut.Body{Body: strings.NewReader(orgBody), Len: len(orgBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, orgResponse.Code)
	var orgCreated map[string]any
	if err := json.Unmarshal(orgResponse.Body.Bytes(), &orgCreated); err != nil {
		t.Fatalf("org create response is not JSON: %v", err)
	}
	org, _ := orgCreated["org"].(map[string]any)
	orgID, _ := org["org_id"].(string)
	projectBody := `{"org_id":"` + orgID + `","name":"Project Delete API","slug":"project-delete-api"}`
	projectResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/create", &ut.Body{Body: strings.NewReader(projectBody), Len: len(projectBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, projectResponse.Code)
	var projectCreated map[string]any
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &projectCreated); err != nil {
		t.Fatalf("project create response is not JSON: %v", err)
	}
	project, _ := projectCreated["project"].(map[string]any)
	projectID, _ := project["project_id"].(string)

	projectDeleteBody := `{"org_id":"` + orgID + `","project_id":"` + projectID + `"}`
	noToken := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/delete", &ut.Body{Body: strings.NewReader(projectDeleteBody), Len: len(projectDeleteBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, noToken.Code)
	memberDelete := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/delete", &ut.Body{Body: strings.NewReader(projectDeleteBody), Len: len(projectDeleteBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + memberToken})
	assert.DeepEqual(t, 403, memberDelete.Code)
	ownerDelete := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/delete", &ut.Body{Body: strings.NewReader(projectDeleteBody), Len: len(projectDeleteBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, ownerDelete.Code)
	if !strings.Contains(ownerDelete.Body.String(), `"status":"deleted"`) {
		t.Fatalf("project delete response mismatch: %s", ownerDelete.Body.String())
	}
	projectListResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/list", &ut.Body{Body: strings.NewReader(`{"org_id":"` + orgID + `"}`), Len: len(`{"org_id":"` + orgID + `"}`)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, projectListResponse.Code)
	if strings.Contains(projectListResponse.Body.String(), projectID) {
		t.Fatalf("deleted project still appears in list: %s", projectListResponse.Body.String())
	}

	orgDeleteBody := `{"org_id":"` + orgID + `"}`
	orgDelete := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/delete", &ut.Body{Body: strings.NewReader(orgDeleteBody), Len: len(orgDeleteBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, orgDelete.Code)
	if !strings.Contains(orgDelete.Body.String(), `"status":"deleted"`) {
		t.Fatalf("org delete response mismatch: %s", orgDelete.Body.String())
	}
	orgListResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/list", &ut.Body{Body: strings.NewReader(`{}`), Len: len(`{}`)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, orgListResponse.Code)
	if strings.Contains(orgListResponse.Body.String(), orgID) {
		t.Fatalf("deleted org still appears in list: %s", orgListResponse.Body.String())
	}
}

func TestTenantOrgProjectEditRequiresWritePermission(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	ownerToken, _, err := authService.CreatePAT("user_1", "owner", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner) error = %v", err)
	}
	memberToken, _, err := authService.CreatePAT("user_2", "member", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(member) error = %v", err)
	}
	tenantService := tenantServiceWithUser(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	orgBody := `{"name":"Org Edit API","slug":"org-edit-api"}`
	orgResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/create", &ut.Body{Body: strings.NewReader(orgBody), Len: len(orgBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, orgResponse.Code)
	var orgCreated map[string]any
	if err := json.Unmarshal(orgResponse.Body.Bytes(), &orgCreated); err != nil {
		t.Fatalf("org create response is not JSON: %v", err)
	}
	org, _ := orgCreated["org"].(map[string]any)
	orgID, _ := org["org_id"].(string)
	projectBody := `{"org_id":"` + orgID + `","name":"Project Edit API","slug":"project-edit-api"}`
	projectResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/create", &ut.Body{Body: strings.NewReader(projectBody), Len: len(projectBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, projectResponse.Code)
	var projectCreated map[string]any
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &projectCreated); err != nil {
		t.Fatalf("project create response is not JSON: %v", err)
	}
	project, _ := projectCreated["project"].(map[string]any)
	projectID, _ := project["project_id"].(string)

	orgEditBody := `{"org_id":"` + orgID + `","name":"Org Renamed API","slug":"org-renamed-api"}`
	noToken := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/edit", &ut.Body{Body: strings.NewReader(orgEditBody), Len: len(orgEditBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, noToken.Code)
	memberEdit := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/edit", &ut.Body{Body: strings.NewReader(orgEditBody), Len: len(orgEditBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + memberToken})
	assert.DeepEqual(t, 403, memberEdit.Code)
	ownerEdit := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/edit", &ut.Body{Body: strings.NewReader(orgEditBody), Len: len(orgEditBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, ownerEdit.Code)
	if !strings.Contains(ownerEdit.Body.String(), `"name":"Org Renamed API"`) || !strings.Contains(ownerEdit.Body.String(), `"slug":"org-renamed-api"`) || !strings.Contains(ownerEdit.Body.String(), `"status":"active"`) {
		t.Fatalf("org edit response mismatch: %s", ownerEdit.Body.String())
	}

	projectEditBody := `{"org_id":"` + orgID + `","project_id":"` + projectID + `","name":"Project Renamed API","slug":"project-renamed-api"}`
	memberProjectEdit := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/edit", &ut.Body{Body: strings.NewReader(projectEditBody), Len: len(projectEditBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + memberToken})
	assert.DeepEqual(t, 403, memberProjectEdit.Code)
	ownerProjectEdit := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/edit", &ut.Body{Body: strings.NewReader(projectEditBody), Len: len(projectEditBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, ownerProjectEdit.Code)
	if !strings.Contains(ownerProjectEdit.Body.String(), `"name":"Project Renamed API"`) || !strings.Contains(ownerProjectEdit.Body.String(), `"slug":"project-renamed-api"`) || !strings.Contains(ownerProjectEdit.Body.String(), `"status":"active"`) {
		t.Fatalf("project edit response mismatch: %s", ownerProjectEdit.Body.String())
	}

	orgListResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/list", &ut.Body{Body: strings.NewReader(`{}`), Len: len(`{}`)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, orgListResponse.Code)
	if !strings.Contains(orgListResponse.Body.String(), `"name":"Org Renamed API"`) || strings.Contains(orgListResponse.Body.String(), `"name":"Org Edit API"`) {
		t.Fatalf("org list did not reflect edit: %s", orgListResponse.Body.String())
	}
	projectListResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/list", &ut.Body{Body: strings.NewReader(`{"org_id":"` + orgID + `"}`), Len: len(`{"org_id":"` + orgID + `"}`)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, projectListResponse.Code)
	if !strings.Contains(projectListResponse.Body.String(), `"name":"Project Renamed API"`) || strings.Contains(projectListResponse.Body.String(), `"name":"Project Edit API"`) {
		t.Fatalf("project list did not reflect edit: %s", projectListResponse.Body.String())
	}
}

func TestTenantMembershipRoleAndRemoveRequiresWritePermission(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	ownerToken, _, err := authService.CreatePAT("user_1", "owner", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner) error = %v", err)
	}
	memberToken, _, err := authService.CreatePAT("user_2", "member", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(member) error = %v", err)
	}
	outsiderToken, _, err := authService.CreatePAT("user_3", "outsider", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(outsider) error = %v", err)
	}
	tenantService := tenantServiceWithUser(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	orgBody := `{"name":"Org Membership API","slug":"org-membership-api"}`
	orgResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/create", &ut.Body{Body: strings.NewReader(orgBody), Len: len(orgBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, orgResponse.Code)
	var orgCreated map[string]any
	if err := json.Unmarshal(orgResponse.Body.Bytes(), &orgCreated); err != nil {
		t.Fatalf("org create response is not JSON: %v", err)
	}
	org, _ := orgCreated["org"].(map[string]any)
	orgID, _ := org["org_id"].(string)
	projectBody := `{"org_id":"` + orgID + `","name":"Project Membership API","slug":"project-membership-api"}`
	projectResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/create", &ut.Body{Body: strings.NewReader(projectBody), Len: len(projectBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, projectResponse.Code)
	var projectCreated map[string]any
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &projectCreated); err != nil {
		t.Fatalf("project create response is not JSON: %v", err)
	}
	project, _ := projectCreated["project"].(map[string]any)
	projectID, _ := project["project_id"].(string)
	memberBody := `{"user_id":"user_2","org_id":"` + orgID + `","project_id":"` + projectID + `","role":"member"}`
	memberResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/add", &ut.Body{Body: strings.NewReader(memberBody), Len: len(memberBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, memberResponse.Code)

	updateBody := `{"user_id":"user_2","org_id":"` + orgID + `","project_id":"` + projectID + `","role":"admin"}`
	noToken := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/update-role", &ut.Body{Body: strings.NewReader(updateBody), Len: len(updateBody)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, noToken.Code)
	memberUpdate := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/update-role", &ut.Body{Body: strings.NewReader(updateBody), Len: len(updateBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + memberToken})
	assert.DeepEqual(t, 403, memberUpdate.Code)
	ownerUpdate := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/update-role", &ut.Body{Body: strings.NewReader(updateBody), Len: len(updateBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, ownerUpdate.Code)
	if !strings.Contains(ownerUpdate.Body.String(), `"user_id":"user_2"`) || !strings.Contains(ownerUpdate.Body.String(), `"role":"admin"`) || !strings.Contains(ownerUpdate.Body.String(), `"status":"active"`) {
		t.Fatalf("membership update response mismatch: %s", ownerUpdate.Body.String())
	}

	removeBody := `{"user_id":"user_2","org_id":"` + orgID + `","project_id":"` + projectID + `"}`
	outsiderRemove := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/remove", &ut.Body{Body: strings.NewReader(removeBody), Len: len(removeBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + outsiderToken})
	assert.DeepEqual(t, 403, outsiderRemove.Code)
	ownerRemove := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/remove", &ut.Body{Body: strings.NewReader(removeBody), Len: len(removeBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, ownerRemove.Code)
	if !strings.Contains(ownerRemove.Body.String(), `"user_id":"user_2"`) || !strings.Contains(ownerRemove.Body.String(), `"role":"admin"`) || !strings.Contains(ownerRemove.Body.String(), `"status":"disabled"`) {
		t.Fatalf("membership remove response mismatch: %s", ownerRemove.Body.String())
	}

	membershipListBody := `{"org_id":"` + orgID + `","project_id":"` + projectID + `"}`
	membershipListResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/list", &ut.Body{Body: strings.NewReader(membershipListBody), Len: len(membershipListBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerToken})
	assert.DeepEqual(t, 200, membershipListResponse.Code)
	if !strings.Contains(membershipListResponse.Body.String(), `"status":"disabled"`) {
		t.Fatalf("membership list did not reflect removal: %s", membershipListResponse.Body.String())
	}
}

func TestTenantMembershipRoleAndRemoveAllowProjectOwnerWithoutOrgMembership(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	projectOwnerToken, _, err := authService.CreatePAT("user_1", "project-owner", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(project owner) error = %v", err)
	}
	tenantService := tenant.NewService(tenant.NewMemoryRepository())
	if _, err := tenantService.CreateUser("project-owner-api@example.com", "Project Owner API"); err != nil {
		t.Fatalf("CreateUser(project owner) error = %v", err)
	}
	if _, err := tenantService.CreateUser("project-member-api@example.com", "Project Member API"); err != nil {
		t.Fatalf("CreateUser(project member) error = %v", err)
	}
	org, _ := tenantService.CreateOrg("Org Project Owner API", "org-project-owner-api")
	project, _ := tenantService.CreateProject(org.ID, "Project Project Owner API", "project-project-owner-api")
	if err := tenantService.AddMembership("user_1", org.ID, project.ID, tenant.RoleOwner); err != nil {
		t.Fatalf("AddMembership(project owner) error = %v", err)
	}
	if err := tenantService.AddMembership("user_2", org.ID, project.ID, tenant.RoleMember); err != nil {
		t.Fatalf("AddMembership(member) error = %v", err)
	}
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	updateBody := `{"user_id":"user_2","org_id":"` + org.ID + `","project_id":"` + project.ID + `","role":"admin"}`
	updateResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/update-role", &ut.Body{Body: strings.NewReader(updateBody), Len: len(updateBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + projectOwnerToken})
	assert.DeepEqual(t, 200, updateResponse.Code)
	if !strings.Contains(updateResponse.Body.String(), `"role":"admin"`) || !strings.Contains(updateResponse.Body.String(), `"status":"active"`) {
		t.Fatalf("project owner membership update response mismatch: %s", updateResponse.Body.String())
	}

	removeBody := `{"user_id":"user_2","org_id":"` + org.ID + `","project_id":"` + project.ID + `"}`
	removeResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/remove", &ut.Body{Body: strings.NewReader(removeBody), Len: len(removeBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + projectOwnerToken})
	assert.DeepEqual(t, 200, removeResponse.Code)
	if !strings.Contains(removeResponse.Body.String(), `"role":"admin"`) || !strings.Contains(removeResponse.Body.String(), `"status":"disabled"`) {
		t.Fatalf("project owner membership remove response mismatch: %s", removeResponse.Body.String())
	}
}

func TestTenantRolesListRequiresReadPermissionAndReturnsLabels(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	ownerWriteToken, _, err := authService.CreatePAT("user_1", "owner-write", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner write) error = %v", err)
	}
	ownerReadToken, _, err := authService.CreatePAT("user_1", "owner-read", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner read) error = %v", err)
	}
	outsiderToken, _, err := authService.CreatePAT("user_2", "outsider-read", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(outsider read) error = %v", err)
	}
	tenantService := tenantServiceWithUser(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	orgBody := `{"name":"Org Roles API","slug":"org-roles-api"}`
	orgResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/create", &ut.Body{Body: strings.NewReader(orgBody), Len: len(orgBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerWriteToken})
	assert.DeepEqual(t, 200, orgResponse.Code)
	var orgCreated map[string]any
	if err := json.Unmarshal(orgResponse.Body.Bytes(), &orgCreated); err != nil {
		t.Fatalf("org create response is not JSON: %v", err)
	}
	org, _ := orgCreated["org"].(map[string]any)
	orgID, _ := org["org_id"].(string)
	projectBody := `{"org_id":"` + orgID + `","name":"Project Roles API","slug":"project-roles-api"}`
	projectResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/create", &ut.Body{Body: strings.NewReader(projectBody), Len: len(projectBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerWriteToken})
	assert.DeepEqual(t, 200, projectResponse.Code)
	var projectCreated map[string]any
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &projectCreated); err != nil {
		t.Fatalf("project create response is not JSON: %v", err)
	}
	project, _ := projectCreated["project"].(map[string]any)
	projectID, _ := project["project_id"].(string)
	body := `{"org_id":"` + orgID + `","project_id":"` + projectID + `"}`

	noToken := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/roles/list", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, noToken.Code)
	outsider := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/roles/list", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + outsiderToken})
	assert.DeepEqual(t, 403, outsider.Code)
	owner := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/roles/list", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerReadToken})
	assert.DeepEqual(t, 200, owner.Code)
	response := owner.Body.String()
	for _, required := range []string{
		`"role":"owner"`,
		`"role":"admin"`,
		`"role":"member"`,
		`"permission_labels"`,
		`"project:` + projectID + `:write"`,
		`"secret:` + projectID + `:use"`,
	} {
		if !strings.Contains(response, required) {
			t.Fatalf("roles response missing %q: %s", required, response)
		}
	}
}

func TestTenantRoleUpsertRequiresWritePermissionAndReturnsDefinition(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	ownerWriteToken, _, err := authService.CreatePAT("user_1", "owner-write", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(owner write) error = %v", err)
	}
	memberToken, _, err := authService.CreatePAT("user_2", "member", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT(member) error = %v", err)
	}
	tenantService := tenantServiceWithUser(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	orgBody := `{"name":"Org Role Upsert API","slug":"org-role-upsert-api"}`
	orgResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/orgs/create", &ut.Body{Body: strings.NewReader(orgBody), Len: len(orgBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerWriteToken})
	assert.DeepEqual(t, 200, orgResponse.Code)
	var orgCreated map[string]any
	if err := json.Unmarshal(orgResponse.Body.Bytes(), &orgCreated); err != nil {
		t.Fatalf("org create response is not JSON: %v", err)
	}
	org := orgCreated["org"].(map[string]any)
	orgID := org["org_id"].(string)

	projectBody := `{"org_id":"` + orgID + `","name":"Project Role Upsert API","slug":"project-role-upsert-api"}`
	projectResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/projects/create", &ut.Body{Body: strings.NewReader(projectBody), Len: len(projectBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerWriteToken})
	assert.DeepEqual(t, 200, projectResponse.Code)
	var projectCreated map[string]any
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &projectCreated); err != nil {
		t.Fatalf("project create response is not JSON: %v", err)
	}
	project := projectCreated["project"].(map[string]any)
	projectID := project["project_id"].(string)

	addMemberBody := `{"user_id":"user_2","org_id":"` + orgID + `","project_id":"` + projectID + `","role":"member"}`
	addMemberResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/memberships/add", &ut.Body{Body: strings.NewReader(addMemberBody), Len: len(addMemberBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerWriteToken})
	assert.DeepEqual(t, 200, addMemberResponse.Code)

	body := `{"org_id":"` + orgID + `","project_id":"` + projectID + `","role":"Auditor","display_name":"Auditor","description":"Code reviewer","permission_labels":["project:{project_id}:read"]}`
	noToken := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/roles/upsert", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, noToken.Code)

	outsider := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/roles/upsert", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + memberToken})
	assert.DeepEqual(t, 403, outsider.Code)

	owner := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/roles/upsert", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerWriteToken})
	assert.DeepEqual(t, 200, owner.Code)
	if !strings.Contains(owner.Body.String(), `"role":"auditor"`) || !strings.Contains(owner.Body.String(), `"display_name":"Auditor"`) || !strings.Contains(owner.Body.String(), `"description":"Code reviewer"`) || !strings.Contains(owner.Body.String(), `"permission_labels":["project:{project_id}:read"]`) {
		t.Fatalf("role upsert create response mismatch: %s", owner.Body.String())
	}

	updateBody := `{"org_id":"` + orgID + `","project_id":"` + projectID + `","role":"Auditor","display_name":"Lead Auditor","permission_labels":["project:{project_id}:read","project:{project_id}:write"]}`
	updated := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/roles/upsert", &ut.Body{Body: strings.NewReader(updateBody), Len: len(updateBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerWriteToken})
	assert.DeepEqual(t, 200, updated.Code)
	if !strings.Contains(updated.Body.String(), `"display_name":"Lead Auditor"`) || !strings.Contains(updated.Body.String(), `"project:{project_id}:write"`) {
		t.Fatalf("role upsert update response mismatch: %s", updated.Body.String())
	}

	listBody := `{"org_id":"` + orgID + `","project_id":"` + projectID + `"}`
	listResponse := ut.PerformRequest(h.Engine, "POST", "/memory/tenant/roles/list", &ut.Body{Body: strings.NewReader(listBody), Len: len(listBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + ownerWriteToken})
	assert.DeepEqual(t, 200, listResponse.Code)
	if !strings.Contains(listResponse.Body.String(), `"role":"auditor"`) || !strings.Contains(listResponse.Body.String(), `"Lead Auditor"`) || !strings.Contains(listResponse.Body.String(), `"project:`+projectID+`:write"`) {
		t.Fatalf("role list should include updated custom role: %s", listResponse.Body.String())
	}
}

func TestAuditAndAccessLogAPIsRequirePATWhenConfigured(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, AuditService: audit.NewService(audit.NewMemoryRepository()), RetrievalAccessLog: retrieval.NewMemoryAccessLog()})

	body := `{"org_id":"org_1","project_id":"project_1"}`
	auditResponse := ut.PerformRequest(h.Engine, "POST", "/memory/audit/list", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, auditResponse.Code)

	accessResponse := ut.PerformRequest(h.Engine, "POST", "/memory/retrieval/access-log/list", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 401, accessResponse.Code)
}

func TestSecurityLogListReturnsActorScopedAuthAndTokenAuditLogs(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "reader", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	auditRepo := audit.NewMemoryRepository()
	auditService := audit.NewService(auditRepo)
	for _, log := range []audit.Log{
		{ActorUserID: "user_1", Action: "auth.password_login", ResourceType: "user", ResourceID: "user_1", RequestID: "login_audit_1", Result: "ok", Metadata: map[string]string{"credential": "password_login_pat", "token_prefix": "pat"}},
		{ActorUserID: "user_1", Action: "token.pat.revoke", ResourceType: "pat", ResourceID: "pat_1", RequestID: "pat_revoke_1", Result: "ok", Metadata: map[string]string{"token_prefix": "pat"}},
		{ActorUserID: "user_2", Action: "auth.password_login", ResourceType: "user", ResourceID: "user_2", RequestID: "login_audit_2", Result: "ok", Metadata: map[string]string{"credential": "password_login_pat"}},
	} {
		if err := auditService.Record(log); err != nil {
			t.Fatalf("Record(%s) error = %v", log.RequestID, err)
		}
	}
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, AuditService: auditService})

	body := `{"limit":50}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/security/logs/list", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"action":"auth.password_login"`) || !strings.Contains(response.Body.String(), `"action":"token.pat.revoke"`) {
		t.Fatalf("security log list missing actor logs: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "user_2") || strings.Contains(response.Body.String(), "login_audit_2") {
		t.Fatalf("security log list leaked another actor: %s", response.Body.String())
	}
	for _, forbidden := range []string{"token_hash", "password_hash"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("security log list leaked forbidden token material %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestAuditAndAccessLogListUseTenantPermissionAndDoNotExposeQueryText(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "reader", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleMember)
	auditRepo := audit.NewMemoryRepository()
	auditService := audit.NewService(auditRepo)
	if err := auditService.Record(audit.Log{ActorUserID: "user_1", OrgID: "org_1", ProjectID: "project_1", Action: "archive.create", ResourceType: "archive", ResourceID: "archive_1", RequestID: "audit_request_1", Result: "ok", Metadata: map[string]string{"source": "http_test"}}); err != nil {
		t.Fatalf("audit Record() error = %v", err)
	}
	accessLog := retrieval.NewMemoryAccessLog()
	searchRequest := retrieval.SearchRequest{RequestID: "retrieval_request_1", Query: "super private query text", Actor: retrieval.Actor{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: "codex"}}
	if err := accessLog.LogRequest(searchRequest, true); err != nil {
		t.Fatalf("LogRequest() error = %v", err)
	}
	if err := accessLog.LogResult(searchRequest.RequestID, 1, retrieval.SearchResult{Score: 0.9, Source: retrieval.SourceRef{Kind: retrieval.SourceArchiveChunk, ArchiveID: "archive_1", ChunkID: "chunk_1"}}); err != nil {
		t.Fatalf("LogResult() error = %v", err)
	}
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, AuditService: auditService, RetrievalAccessLog: accessLog})

	body := `{"org_id":"org_1","project_id":"project_1"}`
	auditResponse := ut.PerformRequest(h.Engine, "POST", "/memory/audit/list", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, auditResponse.Code)
	if !strings.Contains(auditResponse.Body.String(), `"request_id":"audit_request_1"`) || !strings.Contains(auditResponse.Body.String(), `"action":"archive.create"`) {
		t.Fatalf("audit list response mismatch: %s", auditResponse.Body.String())
	}

	accessResponse := ut.PerformRequest(h.Engine, "POST", "/memory/retrieval/access-log/list", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, accessResponse.Code)
	if !strings.Contains(accessResponse.Body.String(), `"request_id":"retrieval_request_1"`) || !strings.Contains(accessResponse.Body.String(), `"query_hash"`) {
		t.Fatalf("access log response mismatch: %s", accessResponse.Body.String())
	}
	if strings.Contains(accessResponse.Body.String(), "super private query text") {
		t.Fatalf("access log leaked raw query text: %s", accessResponse.Body.String())
	}
}

func archiveCreateBody(archiveID, requestID string) string {
	return `{"request_id":"` + requestID + `","archive_id":"` + archiveID + `","title":"Production Archive","user_id":"user_1","org_id":"org_1","project_id":"project_1","created_at":"2026-07-03T00:00:00Z","events":[{"version":"v1","event_id":"event_archive_1","turn_id":"turn_archive_1","thread_id":"thread_archive_1","session_id":"session_archive_1","type":"user_message","created_at":"2026-07-03T00:00:00Z","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"payload":{"text":"production archive note"}}]}`
}

func mapValues(values map[string]string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return items
}

func TestDevRAGSmokeRunsInDevelopment(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AppEnv: "development", EnableDevEndpoints: true})

	response := ut.PerformRequest(h.Engine, "POST", "/dev/smoke/rag", nil)

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"results":1`) {
		t.Fatalf("rag smoke missing result count: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "cross_tenant_leaked") {
		t.Fatal("rag smoke leaked cross tenant data")
	}
}

func TestDevHotMemorySmokeRunsInDevelopment(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AppEnv: "development", EnableDevEndpoints: true})

	response := ut.PerformRequest(h.Engine, "POST", "/dev/smoke/hot-memory", nil)

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"results":1`) {
		t.Fatalf("hot memory smoke missing result count: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"used_count":1`) {
		t.Fatalf("hot memory smoke missing mark_used count: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "sk-test-redacted-example") {
		t.Fatal("hot memory smoke leaked fake secret")
	}
}

func TestMemorySearchEndpointReturnsUnifiedResults(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	tenantService := seededTenantService(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AppEnv: "development", EnableDevEndpoints: true, TenantService: tenantService, RetrievalService: fixtureRetrievalService()})

	body := `{"request_id":"search_smoke_1","query":"deploy API","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"claude"},"scope":"project","visibility":"project","permission_labels":["project:project_2:read"],"archive_index_generation":2,"max_context_bytes":512}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/search", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"rerank_degraded":true`) {
		t.Fatalf("memory search response missing rerank degraded fallback: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"kind":"hot_memory"`) || !strings.Contains(response.Body.String(), `"kind":"archive_chunk"`) {
		t.Fatalf("memory search response missing unified sources: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "cross_tenant_leaked") || strings.Contains(response.Body.String(), "sk-test-redacted-example") {
		t.Fatalf("memory search leaked isolated or secret content: %s", response.Body.String())
	}
}

func TestMemorySearchRequiresPATWhenAuthConfigured(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := seededTenantService(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, RetrievalService: fixtureRetrievalService()})

	body := `{"request_id":"search_auth_required","query":"deploy API","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"scope":"project","visibility":"project","max_context_bytes":512}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/search", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 401, response.Code)
	if !strings.Contains(response.Body.String(), "pat_required") {
		t.Fatalf("memory search response = %s, want pat_required", response.Body.String())
	}
}

func TestMemorySearchUsesPATSubjectInsteadOfRequestActorUser(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "reader", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := seededTenantService(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, RetrievalService: fixtureRetrievalService()})

	body := `{"request_id":"search_pat_subject","query":"deploy API","actor":{"user_id":"user_2","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"scope":"project","visibility":"project","archive_index_generation":2,"max_context_bytes":512}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/search", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})

	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"kind":"hot_memory"`) {
		t.Fatalf("memory search did not use PAT subject membership: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "cross_tenant_leaked") {
		t.Fatalf("memory search leaked cross tenant data: %s", response.Body.String())
	}
}

func TestHotMemoryAPIsRequireConfiguredServiceAndPAT(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := seededTenantService(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService})

	body := `{"org_id":"org_1","project_id":"project_1","fact":"Production hot memory","scope":"project","visibility":"project","agent_id":"codex"}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/hot-memory/create", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 503, response.Code)
	if !strings.Contains(response.Body.String(), "hot_memory_not_configured") {
		t.Fatalf("hot memory unconfigured response = %s", response.Body.String())
	}

	h = server.New(server.WithHostPorts("127.0.0.1:0"))
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, HotMemoryService: hotmemory.NewService(hotmemory.NewMemoryRepository())})
	response = ut.PerformRequest(h.Engine, "POST", "/memory/hot-memory/create", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 401, response.Code)
	if !strings.Contains(response.Body.String(), "pat_required") {
		t.Fatalf("hot memory unauthenticated response = %s", response.Body.String())
	}
}

func TestHotMemoryCreateListAndLifecycleUsePATSubject(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "writer", []string{"memory:read", "memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, HotMemoryService: hotmemory.NewService(hotmemory.NewMemoryRepository())})

	createBody := `{"org_id":"org_1","project_id":"project_1","user_id":"user_2","agent_id":"codex","scope":"project","visibility":"project","fact":"Production Hot Memory keeps search grounded","source_type":"archive","source_ref":"archive_1","confidence":0.8}`
	createResponse := ut.PerformRequest(h.Engine, "POST", "/memory/hot-memory/create", &ut.Body{Body: strings.NewReader(createBody), Len: len(createBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})

	assert.DeepEqual(t, 200, createResponse.Code)
	createPayload := createResponse.Body.String()
	if !strings.Contains(createPayload, `"user_id":"user_1"`) || strings.Contains(createPayload, `"user_id":"user_2"`) {
		t.Fatalf("hot memory create did not use PAT subject: %s", createPayload)
	}
	if strings.Contains(createPayload, "sk-test-redacted-example") {
		t.Fatalf("hot memory create leaked secret: %s", createPayload)
	}
	memoryID := jsonStringField(t, createPayload, "memory_id")

	listBody := `{"org_id":"org_1","project_id":"project_1","agent_id":"codex","status":"active","limit":20}`
	listResponse := ut.PerformRequest(h.Engine, "POST", "/memory/hot-memory/list", &ut.Body{Body: strings.NewReader(listBody), Len: len(listBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, listResponse.Code)
	if !strings.Contains(listResponse.Body.String(), memoryID) || !strings.Contains(listResponse.Body.String(), "Production Hot Memory") {
		t.Fatalf("hot memory list missing created item: %s", listResponse.Body.String())
	}

	editBody := `{"memory_id":"` + memoryID + `","fact":"Production Hot Memory edit path replaces sk-test-redacted-example","confidence":0.9}`
	editResponse := ut.PerformRequest(h.Engine, "POST", "/memory/hot-memory/edit", &ut.Body{Body: strings.NewReader(editBody), Len: len(editBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, editResponse.Code)
	if !strings.Contains(editResponse.Body.String(), "Production Hot Memory edit path") || strings.Contains(editResponse.Body.String(), "sk-test-redacted-example") {
		t.Fatalf("hot memory edit response mismatch or leaked secret: %s", editResponse.Body.String())
	}

	for _, endpoint := range []string{"/memory/hot-memory/promote", "/memory/hot-memory/demote", "/memory/hot-memory/mark-used"} {
		body := `{"memory_id":"` + memoryID + `"}`
		response := ut.PerformRequest(h.Engine, "POST", endpoint, &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
		assert.DeepEqual(t, 200, response.Code)
	}
	deleteBody := `{"memory_id":"` + memoryID + `"}`
	deleteResponse := ut.PerformRequest(h.Engine, "POST", "/memory/hot-memory/delete", &ut.Body{Body: strings.NewReader(deleteBody), Len: len(deleteBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, deleteResponse.Code)
	if !strings.Contains(deleteResponse.Body.String(), `"status":"deleted"`) {
		t.Fatalf("hot memory delete response = %s", deleteResponse.Body.String())
	}
	listResponse = ut.PerformRequest(h.Engine, "POST", "/memory/hot-memory/list", &ut.Body{Body: strings.NewReader(listBody), Len: len(listBody)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	if strings.Contains(listResponse.Body.String(), memoryID) {
		t.Fatalf("hot memory deleted item still listed: %s", listResponse.Body.String())
	}
}

func TestMemorySearchReturnsServiceUnavailableWithoutRetrievalService(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	tenantService := seededTenantService(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), TenantService: tenantService})

	body := `{"request_id":"search_not_ready","query":"deploy API","actor":{"user_id":"user_1","org_id":"org_1","project_id":"project_1","agent_id":"claude"},"scope":"project","visibility":"project","permission_labels":["project:project_1:read"],"archive_index_generation":2,"max_context_bytes":512}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/search", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 503, response.Code)
	if strings.Contains(response.Body.String(), "Project deploy API with docker compose on T480") {
		t.Fatalf("memory search used seeded demo data without retrieval service: %s", response.Body.String())
	}
}

func TestMemorySearchRejectsActorWithoutProjectMembership(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	tenantService := seededTenantService(t)
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), TenantService: tenantService})

	body := `{"request_id":"search_forbidden","query":"deploy API","actor":{"user_id":"user_2","org_id":"org_1","project_id":"project_1","agent_id":"codex"},"scope":"project","visibility":"project","permission_labels":["project:project_1:read"],"archive_index_generation":2,"max_context_bytes":512}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/search", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.DeepEqual(t, 403, response.Code)
}

func TestHealthHandlerUsesService(t *testing.T) {
	called := false
	service := health.NewService(map[string]health.Checker{
		"api": checkerFunc(func(context.Context) error {
			called = true
			return nil
		}),
	})
	handler := HealthHandler(service)
	ctx := app.NewContext(0)

	handler(context.Background(), ctx)

	if !called {
		t.Fatal("health checker was not called")
	}
	assert.DeepEqual(t, 200, ctx.Response.StatusCode())
}

type fakeQdrantStatusClient struct{}

func (fakeQdrantStatusClient) Health(ctx context.Context) error {
	return nil
}

func (fakeQdrantStatusClient) CollectionInfo(ctx context.Context, collection string) (qdrant.CollectionInfo, error) {
	return qdrant.CollectionInfo{
		Name:                collection,
		Status:              "green",
		PointsCount:         5,
		VectorsCount:        7,
		IndexedVectorsCount: 6,
		SegmentsCount:       1,
		VectorSize:          qdrant.DefaultVectorSize,
		Distance:            qdrant.DefaultDistance,
		PayloadSchema:       map[string]bool{"user_id": true, "org_id": true, "project_id": true},
	}, nil
}

type fakeQdrantStatusStore struct{}

func (fakeQdrantStatusStore) IndexStats(ctx context.Context, collection string) (qdrant.IndexStats, error) {
	return qdrant.IndexStats{
		PointsByStatus:          map[string]int64{"indexed": 4},
		ArchivePointsByStatus:   map[string]int64{"indexed": 3},
		HotMemoryPointsByStatus: map[string]int64{"indexed": 1, "promoted": 1},
		JobsByStatus:            map[string]int64{"pending": 2},
	}, nil
}

func (fakeQdrantStatusStore) ArchiveIndexStats(ctx context.Context, collection, archiveID string, indexGeneration int) (qdrant.ArchiveIndexStats, error) {
	return qdrant.ArchiveIndexStats{
		ArchiveID:       archiveID,
		IndexGeneration: indexGeneration,
		JobsByStatus:    map[string]int64{"pending": 1},
		ChunksByStatus:  map[string]int64{"pending": 3},
		PointsByStatus:  map[string]int64{"indexed": 2},
		IndexJobs: []qdrant.ArchiveIndexJobStatus{{
			IdempotencyKey: "rag_" + archiveID + "_g1",
			Status:         "pending",
			ErrorMessage:   "temporary index failure",
			Attempts:       1,
			MaxAttempts:    3,
		}},
		ArchiveChunks: []qdrant.ArchiveChunkStatus{{
			ChunkID:            archiveID + "_g1_c0",
			ChunkIndex:         0,
			VectorStatus:       "pending",
			ContentHash:        "hash_" + archiveID + "_c0",
			QdrantPointID:      "point_" + archiveID + "_c0",
			QdrantVectorStatus: "indexed",
		}},
	}, nil
}

type checkerFunc func(context.Context) error

func (f checkerFunc) Check(ctx context.Context) error {
	return f(ctx)
}

func seededTenantService(t *testing.T) tenant.Service {
	t.Helper()
	service := tenant.NewService(tenant.NewMemoryRepository())
	user, err := service.CreateUser("alice@example.com", "Alice")
	if err != nil {
		t.Fatalf("CreateUser(alice) error = %v", err)
	}
	if user.ID != "user_1" {
		t.Fatalf("alice id = %q, want user_1", user.ID)
	}
	if _, err := service.CreateUser("bob@example.com", "Bob"); err != nil {
		t.Fatalf("CreateUser(bob) error = %v", err)
	}
	org, err := service.CreateOrg("Org One", "org-one")
	if err != nil {
		t.Fatalf("CreateOrg() error = %v", err)
	}
	if org.ID != "org_1" {
		t.Fatalf("org id = %q, want org_1", org.ID)
	}
	project, err := service.CreateProject(org.ID, "Project One", "project-one")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if project.ID != "project_1" {
		t.Fatalf("project id = %q, want project_1", project.ID)
	}
	if err := service.AddMembership(user.ID, org.ID, project.ID, tenant.RoleMember); err != nil {
		t.Fatalf("AddMembership() error = %v", err)
	}
	return service
}

func archiveTenantService(t *testing.T, role string) tenant.Service {
	t.Helper()
	service := tenant.NewService(tenant.NewMemoryRepository())
	if _, err := service.CreateUser("alice@example.com", "Alice"); err != nil {
		t.Fatalf("CreateUser(alice) error = %v", err)
	}
	if _, err := service.CreateUser("bob@example.com", "Bob"); err != nil {
		t.Fatalf("CreateUser(bob) error = %v", err)
	}
	org, err := service.CreateOrg("Org One", "org-one")
	if err != nil {
		t.Fatalf("CreateOrg() error = %v", err)
	}
	project, err := service.CreateProject(org.ID, "Project One", "project-one")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if err := service.AddMembership("user_1", org.ID, project.ID, role); err != nil {
		t.Fatalf("AddMembership() error = %v", err)
	}
	return service
}

func tenantServiceWithUser(t *testing.T) tenant.Service {
	t.Helper()
	service := tenant.NewService(tenant.NewMemoryRepository())
	if _, err := service.CreateUser("alice@example.com", "Alice"); err != nil {
		t.Fatalf("CreateUser(alice) error = %v", err)
	}
	if _, err := service.CreateUser("bob@example.com", "Bob"); err != nil {
		t.Fatalf("CreateUser(bob) error = %v", err)
	}
	return service
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func jsonStringField(t *testing.T, payload string, field string) string {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("decode json payload: %v", err)
	}
	value, ok := decoded[field].(string)
	if !ok || value == "" {
		t.Fatalf("json field %q missing in %s", field, payload)
	}
	return value
}

func testSecretCodec(t *testing.T) secret.AESGCMCodec {
	t.Helper()
	codec, err := secret.NewAESGCMCodec("test-key", []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAESGCMCodec() error = %v", err)
	}
	return codec
}

func fixtureRetrievalService() retrieval.Service {
	hot := hotmemory.NewService(hotmemory.NewMemoryRepository())
	_, _ = hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Project deploy API with docker compose on T480", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_1", Confidence: 0.8})
	_, _ = hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: hotmemory.ScopeAgentSpecific, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "Codex private deploy shortcut", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_agent", Confidence: 0.9})
	_, _ = hot.Upsert(hotmemory.UpsertRequest{OrgID: "org_2", ProjectID: "project_2", UserID: "user_2", AgentID: "codex", Scope: hotmemory.ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_2:read"}, Fact: "cross_tenant_leaked deploy note", SourceType: hotmemory.SourceTurnEvent, SourceRef: "turn_event_cross", Confidence: 0.9})

	ragService := rag.NewService(rag.NewMemoryStore())
	_ = ragService.Index(rag.IndexRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Chunks: []archive.Chunk{{ChunkID: "chunk_1", ArchiveID: "archive_1", IndexGeneration: 2, Content: "Archive says deploy API through docker compose on T480", ContentHash: "hash_1", SourceEventIDs: []string{"turn_event_2"}}}})
	_ = ragService.Index(rag.IndexRequest{OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Chunks: []archive.Chunk{{ChunkID: "chunk_old", ArchiveID: "archive_1", IndexGeneration: 1, Content: "old deploy API note", ContentHash: "hash_old"}}})

	return retrieval.NewService(retrieval.Options{HotMemory: hot, ArchiveRAG: ragService, Reranker: retrieval.FailingReranker{}, AccessLog: retrieval.NewMemoryAccessLog()})
}

func TestMemoryLifecycleStatsRequiresPATAndConfiguredService(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	tenantService := tenant.NewService(tenant.NewMemoryRepository())
	RegisterRoutes(h.Engine, RouterOptions{
		HealthService: health.NewService(nil),
		AuthService:   authService,
		TenantService: tenantService,
	})
	response := ut.PerformRequest(h.Engine, "POST", "/memory/stats/lifecycle", &ut.Body{Body: strings.NewReader(`{"org_id":"org_1","project_id":"project_1"}`), Len: len(`{"org_id":"org_1","project_id":"project_1"}`)}, ut.Header{Key: "Content-Type", Value: "application/json"})
	if response.Code != 503 {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func TestMemoryLifecycleStatsReturnsSnapshotForCurrentProject(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "reader", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	repo := memorystats.NewMemoryRepository(memorystats.Snapshot{
		Archives:    memorystats.AssetStats{Total: 2, ByStatus: map[string]int64{"active": 2}},
		HotMemories: memorystats.HotMemoryStats{Total: 1, ByStatus: map[string]int64{"active": 1}},
		Topics:      memorystats.TopicStats{Total: 3, ReadyToCompose: 1, Composed: 1, Open: 1},
	})
	RegisterRoutes(h.Engine, RouterOptions{
		HealthService:      health.NewService(nil),
		AuthService:        authService,
		TenantService:      tenantService,
		MemoryStatsService: memorystats.NewService(repo),
	})

	body := `{"org_id":"org_1","project_id":"project_1"}`
	response := ut.PerformRequest(h.Engine, "POST", "/memory/stats/lifecycle", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	assert.DeepEqual(t, 200, response.Code)
	if !strings.Contains(response.Body.String(), `"total":2`) || !strings.Contains(response.Body.String(), `"ready_to_compose":1`) {
		t.Fatalf("memory stats response mismatch: %s", response.Body.String())
	}
	if repo.LastFilter.UserID != "user_1" || repo.LastFilter.OrgID != "org_1" || repo.LastFilter.ProjectID != "project_1" {
		t.Fatalf("repo filter = %#v, want PAT subject and requested project", repo.LastFilter)
	}
	if len(repo.LastFilter.PermissionLabels) == 0 {
		t.Fatalf("repo filter permission labels empty: %#v", repo.LastFilter)
	}
}
