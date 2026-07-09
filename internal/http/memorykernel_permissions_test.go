package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"memory-os/internal/auth"
	"memory-os/internal/health"
	"memory-os/internal/memorykernel"
	"memory-os/internal/tenant"
)

type kernelCollectorStub struct{}

func (kernelCollectorStub) Collect(_ context.Context, scope memorykernel.Scope) (memorykernel.ClassifyInput, error) {
	return memorykernel.ClassifyInput{Scope: scope}, nil
}

type kernelClassifierStub struct{}

func (kernelClassifierStub) Classify(_ context.Context, _ memorykernel.ClassifyInput) (memorykernel.ClassifyResult, error) {
	return memorykernel.ClassifyResult{Summary: "ok"}, nil
}

func newMemoryKernelTestService() *memorykernel.Service {
	return memorykernel.NewService(memorykernel.ServiceOptions{
		Repository: memorykernel.NewInMemoryRepository(),
		Collector:  kernelCollectorStub{},
		Classifier: kernelClassifierStub{},
	})
}

func TestMemoryKernelGovernanceRunUsesProjectPermissions(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "memory-kernel", []string{"memory:write"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	service := newMemoryKernelTestService()
	RegisterRoutes(h.Engine, RouterOptions{
		HealthService:       health.NewService(nil),
		AuthService:         authService,
		TenantService:       tenantService,
		MemoryKernelService: service,
	})

	body := `{"request_id":"gov_kernel_1","org_id":"org_1","project_id":"project_1","trigger_type":"manual"}`
	response := ut.PerformRequest(
		h.Engine,
		"POST",
		"/memory/kernel/governance/run",
		&ut.Body{Body: strings.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)

	if response.Code != 200 {
		t.Fatalf("status = %d body = %s, want 200", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status":"done"`) {
		t.Fatalf("response missing done status: %s", response.Body.String())
	}
}

func TestMemoryKernelUnitListRequiresProjectPermissions(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "memory-kernel", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	if _, err := tenantService.CreateProject("org_1", "Project Two", "project-two"); err != nil {
		t.Fatalf("CreateProject(project-two) error = %v", err)
	}
	service := memorykernel.NewService(memorykernel.ServiceOptions{Repository: memorykernel.NewInMemoryRepository()})
	RegisterRoutes(h.Engine, RouterOptions{
		HealthService:       health.NewService(nil),
		AuthService:         authService,
		TenantService:       tenantService,
		MemoryKernelService: service,
	})

	body := `{"org_id":"org_1","project_id":"project_2","limit":10}`
	response := ut.PerformRequest(
		h.Engine,
		"POST",
		"/memory/kernel/units/list",
		&ut.Body{Body: strings.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)

	if response.Code != 403 {
		t.Fatalf("status = %d body = %s, want 403", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "kernel_units_forbidden") {
		t.Fatalf("response = %s, want kernel_units_forbidden", response.Body.String())
	}
}

func TestMemoryKernelContextPackRequiresProjectPermissions(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	authService := auth.NewService(auth.NewMemoryRepository())
	token, _, err := authService.CreatePAT("user_1", "memory-kernel", []string{"memory:read"}, time.Hour)
	if err != nil {
		t.Fatalf("CreatePAT() error = %v", err)
	}
	tenantService := archiveTenantService(t, tenant.RoleOwner)
	if _, err := tenantService.CreateProject("org_1", "Project Two", "project-two"); err != nil {
		t.Fatalf("CreateProject(project-two) error = %v", err)
	}
	contextPackService := memorykernel.NewContextPackBuilder(memorykernel.NewInMemoryRepository())
	RegisterRoutes(h.Engine, RouterOptions{
		HealthService:      health.NewService(nil),
		AuthService:        authService,
		TenantService:      tenantService,
		ContextPackService: contextPackService,
	})

	body := `{"request_id":"ctx_kernel_1","query":"部署 Memory OS","org_id":"org_1","project_id":"project_2","max_context_bytes":512}`
	response := ut.PerformRequest(
		h.Engine,
		"POST",
		"/memory/kernel/context-pack",
		&ut.Body{Body: strings.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)

	if response.Code != 403 {
		t.Fatalf("status = %d body = %s, want 403", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "kernel_context_pack_forbidden") {
		t.Fatalf("response = %s, want kernel_context_pack_forbidden", response.Body.String())
	}
}
