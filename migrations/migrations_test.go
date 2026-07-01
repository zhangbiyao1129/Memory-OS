package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestAuthTenantSecretMigrationContainsRequiredTables(t *testing.T) {
	sql := readMigration(t, "000002_auth_tenant_secret.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS users",
		"CREATE TABLE IF NOT EXISTS orgs",
		"CREATE TABLE IF NOT EXISTS projects",
		"CREATE TABLE IF NOT EXISTS memberships",
		"CREATE TABLE IF NOT EXISTS roles",
		"CREATE TABLE IF NOT EXISTS resource_permissions",
		"CREATE TABLE IF NOT EXISTS password_credentials",
		"CREATE TABLE IF NOT EXISTS personal_access_tokens",
		"CREATE TABLE IF NOT EXISTS adapter_tokens",
		"CREATE TABLE IF NOT EXISTS secrets",
		"CREATE TABLE IF NOT EXISTS secret_versions",
		"CREATE TABLE IF NOT EXISTS audit_logs",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("migration missing %q", item)
		}
	}
}

func TestAuthTenantSecretMigrationStoresHashesAndCiphertextOnly(t *testing.T) {
	sql := readMigration(t, "000002_auth_tenant_secret.sql")

	required := []string{
		"token_hash TEXT NOT NULL",
		"password_hash TEXT NOT NULL",
		"ciphertext BYTEA NOT NULL",
		"nonce BYTEA NOT NULL",
		"secret_ref TEXT NOT NULL UNIQUE",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("migration missing secure storage field %q", item)
		}
	}

	forbidden := []string{
		"token_plain",
		"plain_token",
		"secret_plain",
		"plaintext",
		"password_plain",
	}
	for _, item := range forbidden {
		if strings.Contains(strings.ToLower(sql), item) {
			t.Fatalf("migration contains forbidden plaintext field %q", item)
		}
	}
}

func TestAuthTenantSecretMigrationHasTenantIndexes(t *testing.T) {
	sql := readMigration(t, "000002_auth_tenant_secret.sql")

	required := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique",
		"CREATE UNIQUE INDEX IF NOT EXISTS memberships_unique_member",
		"CREATE INDEX IF NOT EXISTS projects_org_id_idx",
		"CREATE INDEX IF NOT EXISTS resource_permissions_subject_idx",
		"CREATE INDEX IF NOT EXISTS audit_logs_actor_idx",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("migration missing index %q", item)
		}
	}
}

func TestTurnEventMigrationContainsRequiredTablesAndIndexes(t *testing.T) {
	sql := readMigration(t, "000003_turn_events.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS turn_events",
		"CREATE TABLE IF NOT EXISTS turn_event_payloads",
		"CREATE TABLE IF NOT EXISTS event_ingest_requests",
		"CREATE TABLE IF NOT EXISTS adapter_ingest_logs",
		"event_id TEXT NOT NULL UNIQUE",
		"payload JSONB NOT NULL",
		"safe_payload_hash TEXT NOT NULL",
		"CREATE INDEX IF NOT EXISTS turn_events_turn_id_idx",
		"CREATE INDEX IF NOT EXISTS turn_events_thread_id_idx",
		"CREATE INDEX IF NOT EXISTS turn_events_session_id_idx",
		"CREATE UNIQUE INDEX IF NOT EXISTS event_ingest_requests_request_id_unique",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("turn event migration missing %q", item)
		}
	}
}

func TestArchiveMigrationContainsRequiredTablesAndIndexes(t *testing.T) {
	sql := readMigration(t, "000004_archive.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS archives",
		"CREATE TABLE IF NOT EXISTS archive_versions",
		"CREATE TABLE IF NOT EXISTS archive_events",
		"CREATE TABLE IF NOT EXISTS archive_edit_audit_logs",
		"CREATE TABLE IF NOT EXISTS archive_index_generations",
		"archive_id TEXT NOT NULL UNIQUE",
		"index_generation INTEGER NOT NULL DEFAULT 1",
		"content_hash TEXT NOT NULL",
		"CREATE INDEX IF NOT EXISTS archives_project_user_idx",
		"CREATE INDEX IF NOT EXISTS archives_index_generation_idx",
		"CREATE UNIQUE INDEX IF NOT EXISTS archive_versions_unique_version",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("archive migration missing %q", item)
		}
	}
}

func TestArchiveRAGMigrationContainsRequiredTablesAndIndexes(t *testing.T) {
	sql := readMigration(t, "000005_archive_rag.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS archive_chunks",
		"CREATE TABLE IF NOT EXISTS archive_index_jobs",
		"CREATE TABLE IF NOT EXISTS qdrant_points",
		"chunk_id TEXT NOT NULL UNIQUE",
		"archive_id TEXT NOT NULL",
		"index_generation INTEGER NOT NULL",
		"permission_labels TEXT[] NOT NULL DEFAULT '{}'",
		"vector_status TEXT NOT NULL DEFAULT 'pending'",
		"CREATE INDEX IF NOT EXISTS archive_chunks_archive_generation_idx",
		"CREATE INDEX IF NOT EXISTS archive_chunks_scope_idx",
		"CREATE UNIQUE INDEX IF NOT EXISTS archive_index_jobs_idempotency_unique",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("archive rag migration missing %q", item)
		}
	}
}

func readMigration(t *testing.T, name string) string {
	t.Helper()

	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(content)
}

func TestHotMemoryMigrationContainsRequiredTablesAndIndexes(t *testing.T) {
	sql := readMigration(t, "000006_hot_memory.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS hot_memories",
		"CREATE TABLE IF NOT EXISTS hot_memory_sources",
		"CREATE TABLE IF NOT EXISTS hot_memory_events",
		"CREATE TABLE IF NOT EXISTS hot_memory_index_jobs",
		"memory_id TEXT NOT NULL UNIQUE",
		"permission_labels TEXT[] NOT NULL DEFAULT '{}'",
		"hot_score DOUBLE PRECISION NOT NULL DEFAULT 0",
		"status TEXT NOT NULL DEFAULT 'active'",
		"CREATE UNIQUE INDEX IF NOT EXISTS hot_memories_scope_fact_unique",
		"CREATE INDEX IF NOT EXISTS hot_memories_scope_idx",
		"CREATE INDEX IF NOT EXISTS hot_memories_status_score_idx",
		"CREATE INDEX IF NOT EXISTS hot_memory_sources_memory_idx",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("hot memory migration missing %q", item)
		}
	}
}

func TestRetrievalMigrationContainsRequiredTablesAndIndexes(t *testing.T) {
	sql := readMigration(t, "000007_retrieval.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS memory_access_logs",
		"CREATE TABLE IF NOT EXISTS retrieval_requests",
		"CREATE TABLE IF NOT EXISTS retrieval_results",
		"request_id TEXT NOT NULL UNIQUE",
		"source_kind TEXT NOT NULL",
		"source_ref JSONB NOT NULL DEFAULT '{}'",
		"CREATE INDEX IF NOT EXISTS memory_access_logs_actor_idx",
		"CREATE INDEX IF NOT EXISTS retrieval_results_request_idx",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("retrieval migration missing %q", item)
		}
	}
}

func TestImportBatchesMigrationContainsRequiredTablesAndIndexes(t *testing.T) {
	sql := readMigration(t, "000010_import_batches.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS import_batches",
		"CREATE TABLE IF NOT EXISTS import_items",
		"CREATE TABLE IF NOT EXISTS import_errors",
		"CREATE TABLE IF NOT EXISTS external_source_refs",
		"batch_id TEXT NOT NULL UNIQUE",
		"external_id TEXT NOT NULL",
		"source_type TEXT NOT NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS import_items_source_external_unique",
		"CREATE INDEX IF NOT EXISTS import_items_batch_idx",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("import migration missing %q", item)
		}
	}
}
