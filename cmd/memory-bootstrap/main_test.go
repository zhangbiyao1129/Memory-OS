package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestRunBootstrapCreatesInitialAdminFromPasswordEnv(t *testing.T) {
	restore := runBootstrap
	t.Cleanup(func() {
		runBootstrap = restore
	})

	t.Setenv("MEMORY_OS_BOOTSTRAP_PASSWORD", "bootstrap-password")
	runBootstrap = func(_ context.Context, request bootstrapRequest) (bootstrapResult, error) {
		if request.DSN != "postgres://memory-os" {
			t.Fatalf("request.DSN = %q, want postgres://memory-os", request.DSN)
		}
		if request.Password != "bootstrap-password" {
			t.Fatalf("request.Password = %q, want env password", request.Password)
		}
		if request.Email != "admin@example.com" || request.DisplayName != "Admin" {
			t.Fatalf("request user metadata = %#v, want admin@example.com/Admin", request)
		}
		if request.OrgSlug != "memory-org" || request.ProjectSlug != "memory-project" {
			t.Fatalf("request slugs = %#v, want memory-org/memory-project", request)
		}
		return bootstrapResult{
			UserID:    "user_1",
			OrgID:     "org_1",
			ProjectID: "project_1",
			Email:     request.Email,
		}, nil
	}

	out, err := run([]string{
		"bootstrap",
		"--dsn", "postgres://memory-os",
		"--email", "admin@example.com",
		"--display-name", "Admin",
		"--org-name", "Memory Org",
		"--org-slug", "memory-org",
		"--project-name", "Memory Project",
		"--project-slug", "memory-project",
	})
	if err != nil {
		t.Fatalf("run bootstrap error = %v", err)
	}
	if !strings.Contains(out, "user_1") || !strings.Contains(out, "admin@example.com") {
		t.Fatalf("output = %q, want bootstrap metadata", out)
	}
}

func TestRunBootstrapRequiresPasswordEnvValue(t *testing.T) {
	restore := runBootstrap
	t.Cleanup(func() {
		runBootstrap = restore
	})
	runBootstrap = func(context.Context, bootstrapRequest) (bootstrapResult, error) {
		t.Fatal("runBootstrap should not be called when password env is missing")
		return bootstrapResult{}, nil
	}

	_ = os.Unsetenv("MEMORY_OS_BOOTSTRAP_PASSWORD")
	_, err := run([]string{
		"bootstrap",
		"--dsn", "postgres://memory-os",
		"--email", "admin@example.com",
		"--display-name", "Admin",
		"--org-name", "Memory Org",
		"--org-slug", "memory-org",
		"--project-name", "Memory Project",
		"--project-slug", "memory-project",
	})
	if err == nil {
		t.Fatal("run bootstrap error = nil, want missing password env")
	}
	if !strings.Contains(err.Error(), "MEMORY_OS_BOOTSTRAP_PASSWORD") {
		t.Fatalf("error = %v, want missing password env marker", err)
	}
}

func TestRunSetPasswordUsesExistingUserAndPasswordEnv(t *testing.T) {
	restoreBootstrap := runBootstrap
	restoreSetPassword := runSetPassword
	t.Cleanup(func() {
		runBootstrap = restoreBootstrap
		runSetPassword = restoreSetPassword
	})

	t.Setenv("MEMORY_OS_BOOTSTRAP_PASSWORD", "reset-password")
	runSetPassword = func(_ context.Context, request setPasswordRequest) (setPasswordResult, error) {
		if request.DSN != "postgres://memory-os" {
			t.Fatalf("request.DSN = %q, want postgres://memory-os", request.DSN)
		}
		if request.Email != "admin@example.com" {
			t.Fatalf("request.Email = %q, want admin@example.com", request.Email)
		}
		if request.Password != "reset-password" {
			t.Fatalf("request.Password = %q, want env password", request.Password)
		}
		return setPasswordResult{
			UserID:      "user_1",
			Email:       request.Email,
			DisplayName: "Admin",
			Status:      "active",
		}, nil
	}

	out, err := run([]string{"set-password", "--dsn", "postgres://memory-os", "--email", "admin@example.com"})
	if err != nil {
		t.Fatalf("run set-password error = %v", err)
	}
	if !strings.Contains(out, "user_1") || !strings.Contains(out, "admin@example.com") {
		t.Fatalf("output = %q, want updated user metadata", out)
	}
}

func TestRunSetPasswordRequiresEmail(t *testing.T) {
	restoreBootstrap := runBootstrap
	restoreSetPassword := runSetPassword
	t.Cleanup(func() {
		runBootstrap = restoreBootstrap
		runSetPassword = restoreSetPassword
	})

	t.Setenv("MEMORY_OS_BOOTSTRAP_PASSWORD", "reset-password")
	runSetPassword = func(context.Context, setPasswordRequest) (setPasswordResult, error) {
		t.Fatal("runSetPassword should not be called when email is missing")
		return setPasswordResult{}, nil
	}

	_, err := run([]string{"set-password", "--dsn", "postgres://memory-os"})
	if err == nil {
		t.Fatal("run set-password error = nil, want missing email")
	}
	if !strings.Contains(err.Error(), "--email is required") {
		t.Fatalf("error = %v, want missing email marker", err)
	}
}
