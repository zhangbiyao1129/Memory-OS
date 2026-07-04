package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	projectmigrations "memory-os/migrations"
)

type MigrationFile struct {
	Version int64
	Name    string
	SQL     string
}

type MigrationStore interface {
	EnsureSchemaMigrations(context.Context) error
	HasMigration(context.Context, int64) (bool, error)
	ApplyMigration(context.Context, int64, string) error
}

type PGMigrationStore struct {
	pool *pgxpool.Pool
}

func NewPGMigrationStore(pool *pgxpool.Pool) PGMigrationStore {
	return PGMigrationStore{pool: pool}
}

func EmbeddedMigrationFS() fs.FS {
	return projectmigrations.FS
}

func RunEmbeddedMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	return RunMigrations(ctx, NewPGMigrationStore(pool), EmbeddedMigrationFS())
}

func RunMigrations(ctx context.Context, store MigrationStore, migrationFS fs.FS) error {
	if store == nil {
		return errors.New("migration store is required")
	}
	if err := store.EnsureSchemaMigrations(ctx); err != nil {
		return err
	}
	files, err := MigrationFiles(migrationFS)
	if err != nil {
		return err
	}
	for _, file := range files {
		applied, err := store.HasMigration(ctx, file.Version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := ValidateMigrationSQL(file.SQL); err != nil {
			return fmt.Errorf("validate migration %s: %w", file.Name, err)
		}
		if err := store.ApplyMigration(ctx, file.Version, file.SQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", file.Name, err)
		}
	}
	return nil
}

var migrationNamePattern = regexp.MustCompile(`^([0-9]{6})_.+\.sql$`)

func MigrationFiles(migrationFS fs.FS) ([]MigrationFile, error) {
	entries, err := fs.ReadDir(migrationFS, ".")
	if err != nil {
		return nil, err
	}
	files := []MigrationFile{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		matches := migrationNamePattern.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			return nil, err
		}
		content, err := fs.ReadFile(migrationFS, entry.Name())
		if err != nil {
			return nil, err
		}
		files = append(files, MigrationFile{Version: version, Name: entry.Name(), SQL: string(content)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Version < files[j].Version })
	return files, nil
}

var destructiveMigrationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\bdrop\s+table\b`),
	regexp.MustCompile(`(?is)\btruncate\b`),
	regexp.MustCompile(`(?is)\bdelete\s+from\b`),
	regexp.MustCompile(`(?is)\balter\s+table\b.*\bdrop\b`),
}

func ValidateMigrationSQL(sql string) error {
	for _, pattern := range destructiveMigrationPatterns {
		if pattern.MatchString(sql) {
			return errors.New("destructive migration statements require manual approval")
		}
	}
	return nil
}

func (s PGMigrationStore) EnsureSchemaMigrations(ctx context.Context) error {
	if s.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`)
	return err
}

func (s PGMigrationStore) HasMigration(ctx context.Context, version int64) (bool, error) {
	if s.pool == nil {
		return false, errors.New("postgres pool is not configured")
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&exists)
	return exists, err
}

func (s PGMigrationStore) ApplyMigration(ctx context.Context, version int64, sql string) error {
	if s.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT (version) DO NOTHING`, version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func AppliedMigrationVersions(ctx context.Context, pool *pgxpool.Pool) ([]int64, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is not configured")
	}
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []int64{}
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, nil
}

func FormatMigrationVersions(versions []int64) string {
	values := make([]string, 0, len(versions))
	for _, version := range versions {
		values = append(values, strconv.FormatInt(version, 10))
	}
	return strings.Join(values, ",")
}
