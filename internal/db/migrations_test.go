package db

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrationFilesSortByVersion(t *testing.T) {
	files, err := MigrationFiles(fstest.MapFS{
		"000010_import.sql": {Data: []byte("SELECT 10;")},
		"000001_init.sql":   {Data: []byte("SELECT 1;")},
		"notes.txt":         {Data: []byte("ignored")},
		"000002_auth.sql":   {Data: []byte("SELECT 2;")},
	})
	if err != nil {
		t.Fatalf("MigrationFiles() error = %v", err)
	}
	got := []int64{files[0].Version, files[1].Version, files[2].Version}
	want := []int64{1, 2, 10}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("versions = %#v, want %#v", got, want)
		}
	}
}

func TestMigrationFilesRejectInvalidNames(t *testing.T) {
	_, err := MigrationFiles(fstest.MapFS{
		"000001_init.sql": {Data: []byte("SELECT 1;")},
		"bad.sql":         {Data: []byte("SELECT 2;")},
	})
	if err == nil {
		t.Fatal("MigrationFiles() error = nil, want invalid name error")
	}
}

func TestValidateMigrationSQLRejectsDestructiveStatements(t *testing.T) {
	for _, sql := range []string{
		"DROP TABLE users;",
		"TRUNCATE audit_logs;",
		"DELETE FROM archives;",
		"ALTER TABLE projects DROP COLUMN slug;",
	} {
		if err := ValidateMigrationSQL(sql); err == nil {
			t.Fatalf("ValidateMigrationSQL(%q) error = nil, want destructive rejection", sql)
		}
	}
}

func TestRunMigrationsAppliesPendingFilesAndSkipsApplied(t *testing.T) {
	store := newFakeMigrationStore(map[int64]bool{1: true})
	err := RunMigrations(context.Background(), store, fstest.MapFS{
		"000001_init.sql": {Data: []byte("SELECT 1;")},
		"000002_auth.sql": {Data: []byte("SELECT 2;")},
		"000003_more.sql": {Data: []byte("SELECT 3;")},
	})
	if err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if len(store.executed) != 2 || store.executed[0] != "SELECT 2;" || store.executed[1] != "SELECT 3;" {
		t.Fatalf("executed = %#v, want pending migrations only", store.executed)
	}
	for _, version := range []int64{1, 2, 3} {
		if !store.applied[version] {
			t.Fatalf("version %d not marked applied: %#v", version, store.applied)
		}
	}
}

func TestRunMigrationsStopsWhenApplyFails(t *testing.T) {
	store := newFakeMigrationStore(nil)
	store.failOnSQL = "SELECT 2;"
	err := RunMigrations(context.Background(), store, fstest.MapFS{
		"000001_init.sql": {Data: []byte("SELECT 1;")},
		"000002_auth.sql": {Data: []byte("SELECT 2;")},
		"000003_more.sql": {Data: []byte("SELECT 3;")},
	})
	if err == nil {
		t.Fatal("RunMigrations() error = nil, want apply error")
	}
	if len(store.executed) != 2 {
		t.Fatalf("executed len = %d, want stopped after failing migration", len(store.executed))
	}
	if store.applied[2] {
		t.Fatal("failed migration was marked applied")
	}
	if store.applied[3] {
		t.Fatal("later migration was marked applied")
	}
}

func TestEmbeddedMigrationFSContainsSQLFiles(t *testing.T) {
	files, err := fs.Glob(EmbeddedMigrationFS(), "*.sql")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) == 0 {
		t.Fatal("EmbeddedMigrationFS() contains no SQL migrations")
	}
}

func TestRunEmbeddedMigrationsRejectsNilPool(t *testing.T) {
	if err := RunEmbeddedMigrations(context.Background(), nil); err == nil {
		t.Fatal("RunEmbeddedMigrations() error = nil, want nil pool rejection")
	}
}

func TestEmbeddedMigrationsContainExpectedSchema(t *testing.T) {
	files, err := MigrationFiles(EmbeddedMigrationFS())
	if err != nil {
		t.Fatalf("MigrationFiles() error = %v", err)
	}
	allSQL := ""
	for _, f := range files {
		allSQL += f.SQL + "\n"
	}
	required := []string{
		"CREATE TABLE IF NOT EXISTS memory_units",
		"CREATE TABLE IF NOT EXISTS memory_claims",
		"CREATE TABLE IF NOT EXISTS memory_governance_runs",
		"CREATE TABLE IF NOT EXISTS memory_governance_actions",
		"CREATE TABLE IF NOT EXISTS memory_ci_cases",
		"CREATE TABLE IF NOT EXISTS memory_ci_results",
		"CREATE INDEX IF NOT EXISTS memory_units_scope_status_idx",
		"CREATE UNIQUE INDEX IF NOT EXISTS memory_claims_unit_subject_predicate_unique",
	}
	for _, r := range required {
		if !strings.Contains(allSQL, r) {
			t.Errorf("embedded migrations missing %q", r)
		}
	}
}

func TestRunEmbeddedMigrationsContract(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := RunEmbeddedMigrations(context.Background(), pool); err != nil {
		t.Fatalf("RunEmbeddedMigrations() error = %v", err)
	}
	files, err := MigrationFiles(EmbeddedMigrationFS())
	if err != nil {
		t.Fatalf("MigrationFiles() error = %v", err)
	}
	versions, err := AppliedMigrationVersions(context.Background(), pool)
	if err != nil {
		t.Fatalf("AppliedMigrationVersions() error = %v", err)
	}
	applied := map[int64]bool{}
	for _, version := range versions {
		applied[version] = true
	}
	for _, file := range files {
		if !applied[file.Version] {
			t.Fatalf("migration version %d from %s not applied; applied=%s", file.Version, file.Name, FormatMigrationVersions(versions))
		}
	}
}

type fakeMigrationStore struct {
	applied   map[int64]bool
	executed  []string
	failOnSQL string
}

func newFakeMigrationStore(applied map[int64]bool) *fakeMigrationStore {
	if applied == nil {
		applied = map[int64]bool{}
	}
	return &fakeMigrationStore{applied: applied}
}

func (s *fakeMigrationStore) EnsureSchemaMigrations(context.Context) error {
	return nil
}

func (s *fakeMigrationStore) HasMigration(_ context.Context, version int64) (bool, error) {
	return s.applied[version], nil
}

func (s *fakeMigrationStore) ApplyMigration(_ context.Context, version int64, sql string) error {
	s.executed = append(s.executed, sql)
	if s.failOnSQL == sql {
		return errors.New("apply failed")
	}
	s.applied[version] = true
	return nil
}
