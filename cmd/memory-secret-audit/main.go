package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-os/internal/audit"
	"memory-os/internal/db"
	"memory-os/internal/qdrant"
	"memory-os/internal/secret"
	"memory-os/internal/tenant"
)

type runtimeAuditRequest struct {
	DSN              string
	ArchiveDir       string
	QdrantURL        string
	SecretVaultKeyID string
	SecretVaultKey   []byte
	ProbeValue       string
}

type runtimeLeakCounts struct {
	AuditMetadataHits          int `json:"audit_metadata_hits"`
	ArchiveMarkdownHits        int `json:"archive_markdown_hits"`
	ArchiveChunkHits           int `json:"archive_chunk_hits"`
	HotMemoryHits              int `json:"hot_memory_hits"`
	ArchiveQdrantPayloadHits   int `json:"archive_qdrant_payload_hits"`
	HotMemoryQdrantPayloadHits int `json:"hot_memory_qdrant_payload_hits"`
	QdrantLivePayloadHits      int `json:"qdrant_live_payload_hits"`
}

type runtimeAuditCleanup struct {
	SecretDisabled bool `json:"secret_disabled"`
	ProjectDeleted bool `json:"project_deleted"`
	OrgDeleted     bool `json:"org_deleted"`
	UserDisabled   bool `json:"user_disabled"`
}

type runtimeAuditResult struct {
	Status            string              `json:"status"`
	RequestID         string              `json:"request_id"`
	SecretRef         string              `json:"secret_ref"`
	AuditLogCount     int                 `json:"audit_log_count"`
	RuntimeLeakCounts runtimeLeakCounts   `json:"runtime_leak_counts"`
	Cleanup           runtimeAuditCleanup `json:"cleanup"`
	Notes             []string            `json:"notes,omitempty"`
}

var runRuntimeSecretAudit = runtimeSecretAudit

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
		return "", errors.New("usage: memory-secret-audit runtime --dsn <postgres-dsn> --archive-dir <path> --qdrant-url <url>")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "runtime":
		return runRuntime(args[1:])
	default:
		return "", fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runRuntime(args []string) (string, error) {
	fs := flag.NewFlagSet("runtime", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var request runtimeAuditRequest
	probeValueEnv := fs.String("probe-value-env", "SECRET_AUDIT_PROBE_VALUE", "environment variable holding the audit probe value")
	fs.StringVar(&request.DSN, "dsn", "", "postgres dsn")
	fs.StringVar(&request.ArchiveDir, "archive-dir", "", "archive directory")
	fs.StringVar(&request.QdrantURL, "qdrant-url", "", "qdrant base url")
	if err := fs.Parse(args); err != nil {
		return "", err
	}

	for flagName, value := range map[string]string{
		"--dsn":         request.DSN,
		"--archive-dir": request.ArchiveDir,
		"--qdrant-url":  request.QdrantURL,
	} {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s is required", flagName)
		}
	}

	keyID := strings.TrimSpace(os.Getenv("SECRET_VAULT_KEY_ID"))
	if keyID == "" {
		return "", errors.New("SECRET_VAULT_KEY_ID is required and must not be empty")
	}
	keyB64 := strings.TrimSpace(os.Getenv("SECRET_VAULT_KEY_B64"))
	if keyB64 == "" {
		return "", errors.New("SECRET_VAULT_KEY_B64 is required and must not be empty")
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return "", fmt.Errorf("decode SECRET_VAULT_KEY_B64: %w", err)
	}

	request.SecretVaultKeyID = keyID
	request.SecretVaultKey = key
	request.ProbeValue = strings.TrimSpace(os.Getenv(*probeValueEnv))
	if request.ProbeValue == "" {
		request.ProbeValue = "runtime-secret-audit-probe"
	}

	result, err := runRuntimeSecretAudit(context.Background(), request)
	if err != nil {
		return "", err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func runtimeSecretAudit(_ context.Context, request runtimeAuditRequest) (runtimeAuditResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if strings.TrimSpace(request.DSN) == "" {
		return runtimeAuditResult{}, errors.New("runtime audit dsn is required")
	}
	if strings.TrimSpace(request.ArchiveDir) == "" {
		return runtimeAuditResult{}, errors.New("runtime audit archive dir is required")
	}
	if strings.TrimSpace(request.QdrantURL) == "" {
		return runtimeAuditResult{}, errors.New("runtime audit qdrant url is required")
	}
	if strings.TrimSpace(request.SecretVaultKeyID) == "" || len(request.SecretVaultKey) == 0 {
		return runtimeAuditResult{}, errors.New("runtime audit secret vault key is required")
	}
	info, err := os.Stat(request.ArchiveDir)
	if err != nil {
		return runtimeAuditResult{}, fmt.Errorf("stat archive dir: %w", err)
	}
	if !info.IsDir() {
		return runtimeAuditResult{}, errors.New("runtime audit archive dir must be a directory")
	}

	pool, err := pgxpool.New(ctx, request.DSN)
	if err != nil {
		return runtimeAuditResult{}, fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()
	if err := db.RunEmbeddedMigrations(ctx, pool); err != nil {
		return runtimeAuditResult{}, fmt.Errorf("run migrations: %w", err)
	}

	codec, err := secret.NewAESGCMCodec(request.SecretVaultKeyID, request.SecretVaultKey)
	if err != nil {
		return runtimeAuditResult{}, fmt.Errorf("build vault codec: %w", err)
	}
	vault := secret.NewVault(secret.NewPGRepository(pool), codec)
	auditService := audit.NewService(audit.NewPGRepository(pool))
	injector := secret.NewInjector(vault, auditService)
	tenantService := tenant.NewService(tenant.NewPGRepository(pool))

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	user, org, project, err := createRuntimeAuditFixtures(tenantService, suffix)
	if err != nil {
		return runtimeAuditResult{}, err
	}

	result := runtimeAuditResult{
		Status:    "fail",
		RequestID: "runtime_secret_audit_" + suffix,
	}

	meta, err := vault.Create(secret.CreateRequest{
		OwnerUserID: user.ID,
		OrgID:       org.ID,
		ProjectID:   project.ID,
		Name:        "runtime-secret-audit-" + suffix,
		Plaintext:   request.ProbeValue,
	})
	if err != nil {
		return result, fmt.Errorf("create runtime audit secret: %w", err)
	}
	result.SecretRef = meta.SecretRef

	injected, err := injector.Inject(secret.InjectRequest{
		ActorUserID: user.ID,
		OrgID:       org.ID,
		ProjectID:   project.ID,
		Tool:        "runtime-secret-audit",
		Purpose:     "runtime-secret-audit",
		Target:      secret.InjectionTargetEnv,
		RequestID:   result.RequestID,
		Template:    "TOKEN=${" + meta.SecretRef + "}",
	})
	if err != nil {
		return result, fmt.Errorf("inject runtime audit secret: %w", err)
	}
	if injected != "TOKEN="+request.ProbeValue {
		return result, errors.New("runtime audit injected output mismatch")
	}

	result.AuditLogCount, err = countAuditLogRows(ctx, pool, result.RequestID)
	if err != nil {
		return result, err
	}
	if result.AuditLogCount != 1 {
		return result, fmt.Errorf("runtime audit expected 1 audit log, got %d", result.AuditLogCount)
	}

	result.RuntimeLeakCounts.AuditMetadataHits, err = countProbeHits(ctx, pool, `SELECT count(*) FROM audit_logs WHERE metadata::text ILIKE '%' || $1 || '%'`, request.ProbeValue)
	if err != nil {
		return result, fmt.Errorf("count audit metadata hits: %w", err)
	}
	result.RuntimeLeakCounts.ArchiveChunkHits, err = countProbeHits(ctx, pool, `SELECT count(*) FROM archive_chunks WHERE content ILIKE '%' || $1 || '%'`, request.ProbeValue)
	if err != nil {
		return result, fmt.Errorf("count archive chunk hits: %w", err)
	}
	result.RuntimeLeakCounts.HotMemoryHits, err = countProbeHits(ctx, pool, `SELECT count(*) FROM hot_memories WHERE fact ILIKE '%' || $1 || '%'`, request.ProbeValue)
	if err != nil {
		return result, fmt.Errorf("count hot memory hits: %w", err)
	}
	result.RuntimeLeakCounts.ArchiveQdrantPayloadHits, err = countProbeHits(ctx, pool, `SELECT count(*) FROM qdrant_points WHERE payload::text ILIKE '%' || $1 || '%'`, request.ProbeValue)
	if err != nil {
		return result, fmt.Errorf("count archive qdrant payload hits: %w", err)
	}
	result.RuntimeLeakCounts.HotMemoryQdrantPayloadHits, err = countProbeHits(ctx, pool, `SELECT count(*) FROM hot_memory_qdrant_points WHERE payload::text ILIKE '%' || $1 || '%'`, request.ProbeValue)
	if err != nil {
		return result, fmt.Errorf("count hot memory qdrant payload hits: %w", err)
	}
	result.RuntimeLeakCounts.ArchiveMarkdownHits, err = countArchiveMarkdownHits(request.ArchiveDir, request.ProbeValue)
	if err != nil {
		return result, fmt.Errorf("count archive markdown hits: %w", err)
	}
	result.RuntimeLeakCounts.QdrantLivePayloadHits, _, err = scanQdrantLivePayloadHits(ctx, request.QdrantURL, qdrant.DefaultCollectionName, request.ProbeValue)
	if err != nil {
		return result, fmt.Errorf("scan qdrant live payload hits: %w", err)
	}

	result.Cleanup.SecretDisabled = vault.Disable(meta.SecretRef) == nil
	result.Cleanup.ProjectDeleted = deleteRuntimeAuditProject(tenantService, user.ID, org.ID, project.ID) == nil
	result.Cleanup.OrgDeleted = deleteRuntimeAuditOrg(tenantService, user.ID, org.ID) == nil
	_, userDisableErr := tenantService.UpdateUserStatus(user.ID, "disabled")
	result.Cleanup.UserDisabled = userDisableErr == nil

	if !result.Cleanup.SecretDisabled {
		return result, errors.New("runtime audit cleanup failed: disable secret")
	}
	if !result.Cleanup.ProjectDeleted {
		return result, errors.New("runtime audit cleanup failed: delete project")
	}
	if !result.Cleanup.OrgDeleted {
		return result, errors.New("runtime audit cleanup failed: delete org")
	}
	if !result.Cleanup.UserDisabled {
		return result, errors.New("runtime audit cleanup failed: disable user")
	}

	totalHits := result.RuntimeLeakCounts.AuditMetadataHits +
		result.RuntimeLeakCounts.ArchiveMarkdownHits +
		result.RuntimeLeakCounts.ArchiveChunkHits +
		result.RuntimeLeakCounts.HotMemoryHits +
		result.RuntimeLeakCounts.ArchiveQdrantPayloadHits +
		result.RuntimeLeakCounts.HotMemoryQdrantPayloadHits +
		result.RuntimeLeakCounts.QdrantLivePayloadHits
	if totalHits != 0 {
		return result, fmt.Errorf("runtime audit found %d secret leak hits", totalHits)
	}

	result.Status = "pass"
	result.Notes = []string{
		"runtime secret injection audit executed with temporary fixtures",
		"secret.inject audit log persisted exactly once",
		"probe plaintext was not found in archive markdown, archive chunks, hot memories, tracked qdrant payloads, or live qdrant payloads",
	}
	return result, nil
}

func createRuntimeAuditFixtures(service tenant.Service, suffix string) (tenant.User, tenant.Org, tenant.Project, error) {
	user, err := service.CreateUser("runtime-secret-audit-"+suffix+"@memory.local", "Runtime Secret Audit "+suffix)
	if err != nil {
		return tenant.User{}, tenant.Org{}, tenant.Project{}, fmt.Errorf("create runtime audit user: %w", err)
	}
	org, err := service.CreateOrg("Runtime Secret Audit Org "+suffix, "runtime-secret-audit-org-"+suffix)
	if err != nil {
		return tenant.User{}, tenant.Org{}, tenant.Project{}, fmt.Errorf("create runtime audit org: %w", err)
	}
	project, err := service.CreateProject(org.ID, "Runtime Secret Audit Project "+suffix, "runtime-secret-audit-project-"+suffix)
	if err != nil {
		return tenant.User{}, tenant.Org{}, tenant.Project{}, fmt.Errorf("create runtime audit project: %w", err)
	}
	if err := service.AddMembership(user.ID, org.ID, "", tenant.RoleOwner); err != nil {
		return tenant.User{}, tenant.Org{}, tenant.Project{}, fmt.Errorf("create runtime audit org membership: %w", err)
	}
	if err := service.AddMembership(user.ID, org.ID, project.ID, tenant.RoleOwner); err != nil {
		return tenant.User{}, tenant.Org{}, tenant.Project{}, fmt.Errorf("create runtime audit project membership: %w", err)
	}
	return user, org, project, nil
}

func deleteRuntimeAuditProject(service tenant.Service, userID, orgID, projectID string) error {
	_, err := service.DeleteProject(userID, orgID, projectID)
	return err
}

func deleteRuntimeAuditOrg(service tenant.Service, userID, orgID string) error {
	_, err := service.DeleteOrg(userID, orgID)
	return err
}

func countAuditLogRows(ctx context.Context, pool *pgxpool.Pool, requestID string) (int, error) {
	return countProbeHits(ctx, pool, `SELECT count(*) FROM audit_logs WHERE request_id = $1 AND action = 'secret.inject'`, requestID)
}

func countProbeHits(ctx context.Context, pool *pgxpool.Pool, sqlText, probe string) (int, error) {
	var count int
	if err := pool.QueryRow(ctx, sqlText, probe).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func countArchiveMarkdownHits(root, probe string) (int, error) {
	hits := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(probe)) {
			hits++
		}
		return nil
	})
	return hits, err
}

func scanQdrantLivePayloadHits(ctx context.Context, baseURL, collection, probe string) (int, int, error) {
	type scrollPoint struct {
		Payload map[string]any `json:"payload"`
	}
	type scrollResult struct {
		Points         []scrollPoint   `json:"points"`
		NextPageOffset json.RawMessage `json:"next_page_offset"`
	}
	type scrollResponse struct {
		Result scrollResult `json:"result"`
	}

	client := &http.Client{Timeout: 10 * time.Second}
	offset := json.RawMessage(nil)
	seenOffsets := map[string]struct{}{}
	totalHits := 0
	totalPoints := 0

	for {
		body := map[string]any{
			"limit":        128,
			"with_payload": true,
			"with_vector":  false,
		}
		if len(offset) > 0 && string(offset) != "null" {
			var decoded any
			if err := json.Unmarshal(offset, &decoded); err != nil {
				return 0, totalPoints, err
			}
			body["offset"] = decoded
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, totalPoints, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/collections/"+collection+"/points/scroll", bytes.NewReader(encoded))
		if err != nil {
			return 0, totalPoints, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return 0, totalPoints, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return 0, totalPoints, fmt.Errorf("qdrant scroll status %d: %s", resp.StatusCode, string(payload))
		}
		var decoded scrollResponse
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			resp.Body.Close()
			return 0, totalPoints, err
		}
		resp.Body.Close()
		for _, point := range decoded.Result.Points {
			totalPoints++
			payloadBytes, err := json.Marshal(point.Payload)
			if err != nil {
				return 0, totalPoints, err
			}
			if bytes.Contains(payloadBytes, []byte(probe)) {
				totalHits++
			}
		}
		next := strings.TrimSpace(string(decoded.Result.NextPageOffset))
		if next == "" || next == "null" || len(decoded.Result.Points) == 0 {
			return totalHits, totalPoints, nil
		}
		if _, exists := seenOffsets[next]; exists {
			return 0, totalPoints, errors.New("qdrant scroll offset repeated")
		}
		seenOffsets[next] = struct{}{}
		offset = decoded.Result.NextPageOffset
	}
}
