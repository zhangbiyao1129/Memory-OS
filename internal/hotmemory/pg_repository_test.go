package hotmemory

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGRepositoryRequiresPool(t *testing.T) {
	repo := NewPGRepository(nil)

	memory := Memory{
		MemoryID:         "hm_missing_pool",
		OrgID:            "org_missing_pool",
		ProjectID:        "project_missing_pool",
		UserID:           "user_missing_pool",
		AgentID:          "codex",
		Scope:            ScopeProject,
		Visibility:       "project",
		PermissionLabels: []string{"project:project_missing_pool:read"},
		Fact:             "missing pool",
		FactHash:         "hash_missing_pool",
		Sources:          []Source{{SourceType: SourceArchive, SourceRef: "archive_missing_pool", Confidence: 0.8}},
		Confidence:       0.8,
		HotScore:         8,
		Status:           StatusActive,
	}

	if _, err := repo.Upsert(memory); err == nil {
		t.Fatal("Upsert() error = nil, want missing pool error")
	}
	if _, err := repo.Get("hm_missing_pool"); err == nil {
		t.Fatal("Get() error = nil, want missing pool error")
	}
	if _, err := repo.Update(memory); err == nil {
		t.Fatal("Update() error = nil, want missing pool error")
	}
}

func TestBuildUpsertSQLIncludesUsageSignalColumns(t *testing.T) {
	query, args := buildUpsertSQL(Memory{MemoryID: "hm_1", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", AgentID: "codex", Scope: ScopeProject, Visibility: "project", PermissionLabels: []string{"project:project_1:read"}, Fact: "fact", FactHash: "hash", Sources: []Source{{SourceType: SourceArchive, SourceRef: "archive_1", Confidence: 0.9}}, Confidence: 0.9, AccessCount: 2, ReturnedCount: 3, UsedCount: 4, Pinned: true, HotScore: 7, Status: StatusPromoted})

	for _, want := range []string{"returned_count", "last_accessed_at", "last_returned_at", "last_used_at", "pinned"} {
		if !strings.Contains(query, want) {
			t.Fatalf("buildUpsertSQL query missing %q: %s", want, query)
		}
	}
	if len(args) < 23 {
		t.Fatalf("buildUpsertSQL args len = %d, want usage-signal args included", len(args))
	}
}

func TestBuildUpdateSQLIncludesUsageSignalColumns(t *testing.T) {
	now := time.Now().UTC()
	query, args := buildUpdateSQL(Memory{MemoryID: "hm_1", Fact: "fact", FactHash: "hash", Confidence: 0.8, AccessCount: 2, ReturnedCount: 3, UsedCount: 4, LastAccessedAt: now, LastReturnedAt: now, LastUsedAt: now, Pinned: true, HotScore: 12, Status: StatusPromoted})

	for _, want := range []string{"returned_count", "last_accessed_at", "last_returned_at", "last_used_at", "pinned"} {
		if !strings.Contains(query, want) {
			t.Fatalf("buildUpdateSQL query missing %q: %s", want, query)
		}
	}
	if len(args) < 14 {
		t.Fatalf("buildUpdateSQL args len = %d, want usage-signal args included", len(args))
	}
}

func TestScanMemoryAcceptsNullUsageSignalTimestamps(t *testing.T) {
	memory, err := scanMemory(nullUsageSignalScanner{})
	if err != nil {
		t.Fatalf("scanMemory() error = %v, want nil for nullable usage timestamps", err)
	}
	if !memory.LastAccessedAt.IsZero() || !memory.LastReturnedAt.IsZero() || !memory.LastUsedAt.IsZero() {
		t.Fatalf("usage timestamps = accessed:%v returned:%v used:%v, want zero values", memory.LastAccessedAt, memory.LastReturnedAt, memory.LastUsedAt)
	}
}

func TestPGRepositoryUpsertReturnsPersistedTimestamps(t *testing.T) {
	pool := hotMemoryPGTestPool(t)
	repo := NewPGRepository(pool)
	userID, orgID, projectID := createHotMemoryTenantFixtures(t, pool)
	fact := "deploy api checklist " + hotMemorySuffix()

	saved, err := repo.Upsert(Memory{
		MemoryID:         "hm_" + hotMemorySuffix(),
		OrgID:            orgID,
		ProjectID:        projectID,
		UserID:           userID,
		AgentID:          "codex",
		Scope:            ScopeProject,
		Visibility:       "project",
		PermissionLabels: []string{"project:" + projectID + ":read", "project:" + projectID + ":write"},
		Fact:             fact,
		FactHash:         "hash_" + hotMemorySuffix(),
		Sources:          []Source{{SourceType: SourceArchive, SourceRef: "archive_" + hotMemorySuffix(), Confidence: 0.9}},
		Confidence:       0.9,
		HotScore:         9,
		Status:           StatusActive,
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if saved.CreatedAt.IsZero() || saved.UpdatedAt.IsZero() {
		t.Fatalf("Upsert() timestamps = created_at=%v updated_at=%v, want persisted timestamps", saved.CreatedAt, saved.UpdatedAt)
	}
	if saved.MemoryID == "" || saved.UserID != userID || saved.ProjectID != projectID {
		t.Fatalf("Upsert() saved = %#v, want persisted scope", saved)
	}

	stored, err := repo.Get(saved.MemoryID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.CreatedAt.IsZero() || stored.UpdatedAt.IsZero() {
		t.Fatalf("Get() timestamps = created_at=%v updated_at=%v, want persisted timestamps", stored.CreatedAt, stored.UpdatedAt)
	}
	if !stored.CreatedAt.Equal(saved.CreatedAt) {
		t.Fatalf("stored.CreatedAt = %v, want %v", stored.CreatedAt, saved.CreatedAt)
	}
}

func hotMemoryPGTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createHotMemoryTenantFixtures(t *testing.T, pool *pgxpool.Pool) (string, string, string) {
	t.Helper()
	suffix := hotMemorySuffix()
	var userID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO users (email, display_name) VALUES ($1, $2) RETURNING id::text`, "hotmemory-"+suffix+"@example.com", "Hot Memory "+suffix).Scan(&userID); err != nil {
		t.Fatalf("insert hot memory user: %v", err)
	}
	var orgID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id::text`, "Hot Memory Org "+suffix, "hot-memory-org-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert hot memory org: %v", err)
	}
	var projectID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO projects (org_id, name, slug) VALUES ($1::uuid, $2, $3) RETURNING id::text`, orgID, "Hot Memory Project "+suffix, "hot-memory-project-"+suffix).Scan(&projectID); err != nil {
		t.Fatalf("insert hot memory project: %v", err)
	}
	return userID, orgID, projectID
}

func hotMemorySuffix() string {
	replacer := strings.NewReplacer("-", "")
	return replacer.Replace(strconv.FormatInt(time.Now().UnixNano(), 10))
}

type nullUsageSignalScanner struct{}

func (nullUsageSignalScanner) Scan(dest ...any) error {
	now := time.Now().UTC()
	values := []any{
		"hm_null_usage",
		"org_null_usage",
		"project_null_usage",
		"user_null_usage",
		"codex",
		string(ScopeProject),
		"project",
		[]string{"project:project_null_usage:read"},
		"fact with null usage timestamps",
		"hash_null_usage",
		0.8,
		0,
		0,
		0,
		nil,
		nil,
		nil,
		false,
		4.2,
		string(StatusActive),
		now,
		now,
		nil,
	}
	if len(dest) != len(values) {
		return fmt.Errorf("dest len = %d, want %d", len(dest), len(values))
	}
	for i, value := range values {
		if err := assignScanValue(dest[i], value); err != nil {
			return fmt.Errorf("scan dest[%d]: %w", i, err)
		}
	}
	return nil
}

func assignScanValue(dest any, value any) error {
	switch pointer := dest.(type) {
	case *string:
		*pointer = value.(string)
	case *[]string:
		*pointer = append([]string(nil), value.([]string)...)
	case *float64:
		*pointer = value.(float64)
	case *int:
		*pointer = value.(int)
	case *bool:
		*pointer = value.(bool)
	case *time.Time:
		if value == nil {
			return fmt.Errorf("cannot scan NULL into *time.Time")
		}
		*pointer = value.(time.Time)
	case **time.Time:
		if value == nil {
			*pointer = nil
			return nil
		}
		v := value.(time.Time)
		*pointer = &v
	default:
		return fmt.Errorf("unsupported destination %T", dest)
	}
	return nil
}
