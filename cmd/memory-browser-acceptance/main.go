package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"memory-os/internal/auth"
	"memory-os/internal/tenant"

	"github.com/jackc/pgx/v5/pgxpool"
)

type browserAcceptanceState struct {
	UserEmail        string `json:"user_email"`
	WriteToken       string `json:"write_token"`
	SearchToken      string `json:"search_token"`
	AdapterToken     string `json:"adapter_token"`
	UserID           string `json:"user_id"`
	OrgID            string `json:"org_id"`
	ProjectID        string `json:"project_id"`
	AgentID          string `json:"agent_id"`
	PermissionLabel  string `json:"permission_label"`
	AdapterTokenID   string `json:"adapter_token_id"`
	WriteTokenID     string `json:"write_token_id"`
	SearchTokenID    string `json:"search_token_id"`
	ProvisionedAtUTC string `json:"provisioned_at_utc"`
}

var (
	provisionBrowserAcceptanceSession = createBrowserAcceptanceSession
	cleanupBrowserAcceptanceSession   = revokeBrowserAcceptanceSession
)

func main() {
	out, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "memory-browser-acceptance failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}

func run(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("subcommand is required: provision or cleanup")
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "provision":
		return runProvision(args[1:])
	case "cleanup":
		return runCleanup(args[1:])
	default:
		return "", fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runProvision(args []string) (string, error) {
	fs := flag.NewFlagSet("provision", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dsn := fs.String("dsn", "", "PostgreSQL DSN")
	statePath := fs.String("state", "", "输出状态文件路径")
	ttlValue := fs.Duration("ttl", 30*time.Minute, "短期 token TTL")
	namePrefix := fs.String("name-prefix", "browser-acceptance", "测试主体名前缀")
	agentID := fs.String("agent-id", "codex", "agent id")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if strings.TrimSpace(*dsn) == "" {
		return "", errors.New("--dsn is required")
	}
	if strings.TrimSpace(*statePath) == "" {
		return "", errors.New("--state is required")
	}

	state, err := provisionBrowserAcceptanceSession(context.Background(), strings.TrimSpace(*dsn), strings.TrimSpace(*namePrefix), strings.TrimSpace(*agentID), *ttlValue)
	if err != nil {
		return "", err
	}
	if err := writeSessionState(strings.TrimSpace(*statePath), state); err != nil {
		return "", err
	}
	return fmt.Sprintf("browser acceptance state written: %s", strings.TrimSpace(*statePath)), nil
}

func runCleanup(args []string) (string, error) {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dsn := fs.String("dsn", "", "PostgreSQL DSN")
	statePath := fs.String("state", "", "状态文件路径")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if strings.TrimSpace(*dsn) == "" {
		return "", errors.New("--dsn is required")
	}
	if strings.TrimSpace(*statePath) == "" {
		return "", errors.New("--state is required")
	}

	state, err := readSessionState(strings.TrimSpace(*statePath))
	if err != nil {
		return "", err
	}
	if err := cleanupBrowserAcceptanceSession(context.Background(), strings.TrimSpace(*dsn), state); err != nil {
		return "", err
	}
	if err := os.Remove(strings.TrimSpace(*statePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return fmt.Sprintf("browser acceptance state cleaned: %s", strings.TrimSpace(*statePath)), nil
}

func createBrowserAcceptanceSession(ctx context.Context, dsn, namePrefix, agentID string, ttl time.Duration) (browserAcceptanceState, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return browserAcceptanceState{}, err
	}
	defer pool.Close()

	if strings.TrimSpace(namePrefix) == "" {
		namePrefix = "browser-acceptance"
	}
	if strings.TrimSpace(agentID) == "" {
		agentID = "codex"
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantService := tenant.NewService(tenant.NewPGRepository(pool))
	userEmail := fmt.Sprintf("%s-%s@memory.local", namePrefix, suffix)
	user, err := tenantService.CreateUser(userEmail, "Browser Acceptance "+suffix)
	if err != nil {
		return browserAcceptanceState{}, err
	}
	org, err := tenantService.CreateOrg("Browser Acceptance Org "+suffix, fmt.Sprintf("%s-org-%s", namePrefix, suffix))
	if err != nil {
		return browserAcceptanceState{}, err
	}
	project, err := tenantService.CreateProject(org.ID, "Browser Acceptance Project "+suffix, fmt.Sprintf("%s-project-%s", namePrefix, suffix))
	if err != nil {
		return browserAcceptanceState{}, err
	}
	if err := tenantService.AddMembership(user.ID, org.ID, project.ID, tenant.RoleOwner); err != nil {
		return browserAcceptanceState{}, err
	}

	authService := auth.NewService(auth.NewPGRepository(pool))
	adapterToken, adapterRecord, err := authService.CreateAdapterToken(auth.AdapterTokenRequest{
		UserID:    user.ID,
		OrgID:     org.ID,
		ProjectID: project.ID,
		AgentID:   agentID,
		Scopes:    []string{"turn_event:write"},
		TTL:       ttl,
	})
	if err != nil {
		return browserAcceptanceState{}, err
	}
	writeToken, writeRecord, err := authService.CreatePAT(user.ID, namePrefix+"-write", []string{"memory:write"}, ttl)
	if err != nil {
		_ = authService.RevokeAdapterToken(adapterRecord.ID)
		return browserAcceptanceState{}, err
	}
	searchToken, searchRecord, err := authService.CreatePAT(user.ID, namePrefix+"-read", []string{"memory:read"}, ttl)
	if err != nil {
		_ = authService.RevokePAT(writeRecord.ID)
		_ = authService.RevokeAdapterToken(adapterRecord.ID)
		return browserAcceptanceState{}, err
	}

	return browserAcceptanceState{
		UserEmail:        userEmail,
		WriteToken:       writeToken,
		SearchToken:      searchToken,
		AdapterToken:     adapterToken,
		UserID:           user.ID,
		OrgID:            org.ID,
		ProjectID:        project.ID,
		AgentID:          agentID,
		PermissionLabel:  "project:" + project.ID + ":read",
		AdapterTokenID:   adapterRecord.ID,
		WriteTokenID:     writeRecord.ID,
		SearchTokenID:    searchRecord.ID,
		ProvisionedAtUTC: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func revokeBrowserAcceptanceSession(ctx context.Context, dsn string, state browserAcceptanceState) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	authService := auth.NewService(auth.NewPGRepository(pool))
	if strings.TrimSpace(state.AdapterTokenID) != "" {
		if err := authService.RevokeAdapterToken(state.AdapterTokenID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(state.WriteTokenID) != "" {
		if err := authService.RevokePAT(state.WriteTokenID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(state.SearchTokenID) != "" {
		if err := authService.RevokePAT(state.SearchTokenID); err != nil {
			return err
		}
	}
	return nil
}

func writeSessionState(path string, state browserAcceptanceState) error {
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}

func readSessionState(path string) (browserAcceptanceState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return browserAcceptanceState{}, err
	}
	var state browserAcceptanceState
	if err := json.Unmarshal(data, &state); err != nil {
		return browserAcceptanceState{}, err
	}
	return state, nil
}
