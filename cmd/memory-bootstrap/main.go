package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"memory-os/internal/auth"
	"memory-os/internal/db"
	"memory-os/internal/tenant"
)

type bootstrapRequest struct {
	DSN         string
	Email       string
	DisplayName string
	OrgName     string
	OrgSlug     string
	ProjectName string
	ProjectSlug string
	Password    string
}

type bootstrapResult struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Email     string `json:"email"`
}

type setPasswordRequest struct {
	DSN      string
	Email    string
	Password string
}

type setPasswordResult struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

var runBootstrap = bootstrapFirstAdmin
var runSetPassword = setExistingUserPassword

func main() {
	out, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	fmt.Println(out)
}

func run(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("usage: memory-bootstrap bootstrap --dsn <postgres-dsn> --email <email> --display-name <name> --org-name <org> --org-slug <slug> --project-name <project> --project-slug <slug>")
	}
	switch args[0] {
	case "bootstrap":
		return runBootstrapCommand(args[1:])
	case "set-password":
		return runSetPasswordCommand(args[1:])
	default:
		return "", fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runBootstrapCommand(args []string) (string, error) {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var request bootstrapRequest
	passwordEnv := fs.String("password-env", "MEMORY_OS_BOOTSTRAP_PASSWORD", "environment variable holding the bootstrap admin password")
	fs.StringVar(&request.DSN, "dsn", "", "postgres dsn")
	fs.StringVar(&request.Email, "email", "", "bootstrap admin email")
	fs.StringVar(&request.DisplayName, "display-name", "", "bootstrap admin display name")
	fs.StringVar(&request.OrgName, "org-name", "", "initial org name")
	fs.StringVar(&request.OrgSlug, "org-slug", "", "initial org slug")
	fs.StringVar(&request.ProjectName, "project-name", "", "initial project name")
	fs.StringVar(&request.ProjectSlug, "project-slug", "", "initial project slug")
	if err := fs.Parse(args); err != nil {
		return "", err
	}

	for field, value := range map[string]string{
		"--dsn":          request.DSN,
		"--email":        request.Email,
		"--display-name": request.DisplayName,
		"--org-name":     request.OrgName,
		"--org-slug":     request.OrgSlug,
		"--project-name": request.ProjectName,
		"--project-slug": request.ProjectSlug,
	} {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s is required", field)
		}
	}

	request.Password = strings.TrimSpace(os.Getenv(*passwordEnv))
	if request.Password == "" {
		return "", fmt.Errorf("%s is required and must not be empty", *passwordEnv)
	}

	result, err := runBootstrap(context.Background(), request)
	if err != nil {
		return "", err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func runSetPasswordCommand(args []string) (string, error) {
	fs := flag.NewFlagSet("set-password", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var request setPasswordRequest
	passwordEnv := fs.String("password-env", "MEMORY_OS_BOOTSTRAP_PASSWORD", "environment variable holding the password")
	fs.StringVar(&request.DSN, "dsn", "", "postgres dsn")
	fs.StringVar(&request.Email, "email", "", "existing user email")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if strings.TrimSpace(request.DSN) == "" {
		return "", errors.New("--dsn is required")
	}
	if strings.TrimSpace(request.Email) == "" {
		return "", errors.New("--email is required")
	}

	request.Password = strings.TrimSpace(os.Getenv(*passwordEnv))
	if request.Password == "" {
		return "", fmt.Errorf("%s is required and must not be empty", *passwordEnv)
	}

	result, err := runSetPassword(context.Background(), request)
	if err != nil {
		return "", err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func bootstrapFirstAdmin(ctx context.Context, request bootstrapRequest) (bootstrapResult, error) {
	pool, err := db.NewPool(ctx, request.DSN)
	if err != nil {
		return bootstrapResult{}, err
	}
	defer pool.Close()

	if err := db.RunEmbeddedMigrations(ctx, pool); err != nil {
		return bootstrapResult{}, err
	}

	tenantService := tenant.NewService(tenant.NewPGRepository(pool))
	existingUsers, err := tenantService.ListUsers("")
	if err != nil {
		return bootstrapResult{}, err
	}
	if len(existingUsers) > 0 {
		return bootstrapResult{}, errors.New("bootstrap refused: users already exist")
	}

	authService := auth.NewService(auth.NewPGRepository(pool))
	user, err := tenantService.CreateUser(request.Email, request.DisplayName)
	if err != nil {
		return bootstrapResult{}, err
	}
	org, err := tenantService.CreateOrg(request.OrgName, request.OrgSlug)
	if err != nil {
		return bootstrapResult{}, err
	}
	project, err := tenantService.CreateProject(org.ID, request.ProjectName, request.ProjectSlug)
	if err != nil {
		return bootstrapResult{}, err
	}
	if err := tenantService.AddMembership(user.ID, org.ID, "", tenant.RoleOwner); err != nil {
		return bootstrapResult{}, err
	}
	if err := tenantService.AddMembership(user.ID, org.ID, project.ID, tenant.RoleOwner); err != nil {
		return bootstrapResult{}, err
	}
	if err := authService.SetPassword(user.ID, request.Password); err != nil {
		return bootstrapResult{}, err
	}

	return bootstrapResult{
		UserID:    user.ID,
		OrgID:     org.ID,
		ProjectID: project.ID,
		Email:     user.Email,
	}, nil
}

func setExistingUserPassword(ctx context.Context, request setPasswordRequest) (setPasswordResult, error) {
	pool, err := db.NewPool(ctx, request.DSN)
	if err != nil {
		return setPasswordResult{}, err
	}
	defer pool.Close()

	if err := db.RunEmbeddedMigrations(ctx, pool); err != nil {
		return setPasswordResult{}, err
	}

	tenantService := tenant.NewService(tenant.NewPGRepository(pool))
	user, err := tenantService.FindUserByEmail(request.Email)
	if err != nil {
		return setPasswordResult{}, err
	}
	if user.Status != "active" {
		return setPasswordResult{}, fmt.Errorf("user %s is not active", user.Email)
	}

	authService := auth.NewService(auth.NewPGRepository(pool))
	if err := authService.SetPassword(user.ID, request.Password); err != nil {
		return setPasswordResult{}, err
	}

	return setPasswordResult{
		UserID:      user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      user.Status,
	}, nil
}
