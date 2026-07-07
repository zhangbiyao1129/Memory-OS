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

func TestArchiveIdempotencyMigrationContainsRequiredTableAndIndex(t *testing.T) {
	sql := readMigration(t, "000011_archive_idempotency.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS archive_request_idempotency",
		"request_id TEXT NOT NULL",
		"operation TEXT NOT NULL",
		"archive_id TEXT NOT NULL REFERENCES archives(archive_id) ON DELETE CASCADE",
		"CREATE UNIQUE INDEX IF NOT EXISTS archive_request_idempotency_request_unique",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("archive idempotency migration missing %q", item)
		}
	}
}

func TestArchiveJobsMigrationContainsRequiredTableAndIndexes(t *testing.T) {
	sql := readMigration(t, "000013_archive_jobs.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS archive_jobs",
		"request_id TEXT NOT NULL UNIQUE",
		"archive_id TEXT NOT NULL",
		"event_ids TEXT[] NOT NULL",
		"status TEXT NOT NULL DEFAULT 'pending'",
		"attempts INTEGER NOT NULL DEFAULT 0",
		"max_attempts INTEGER NOT NULL DEFAULT 3",
		"locked_until TIMESTAMPTZ",
		"CREATE INDEX IF NOT EXISTS archive_jobs_ready_idx",
		"CREATE INDEX IF NOT EXISTS archive_jobs_scope_idx",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("archive jobs migration missing %q", item)
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

func TestSecretLocalCryptoMigrationAddsMetadataAndKeyColumns(t *testing.T) {
	sql := readMigration(t, "000023_secret_local_crypto.sql")

	required := []string{
		"ALTER TABLE secrets ADD COLUMN IF NOT EXISTS env_name TEXT",
		"ALTER TABLE secrets ADD COLUMN IF NOT EXISTS site TEXT",
		"ALTER TABLE secrets ADD COLUMN IF NOT EXISTS purpose TEXT",
		"ALTER TABLE secrets ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ",
		"ALTER TABLE secret_versions ADD COLUMN IF NOT EXISTS algorithm TEXT NOT NULL DEFAULT 'AES-256-GCM'",
		"ALTER TABLE secret_versions ADD COLUMN IF NOT EXISTS device_key_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE secret_versions ADD COLUMN IF NOT EXISTS key_fingerprint TEXT NOT NULL DEFAULT ''",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("secret local crypto migration missing %q", item)
		}
	}

	for _, forbidden := range []string{"DROP TABLE", "DROP COLUMN"} {
		if strings.Contains(strings.ToUpper(sql), forbidden) {
			t.Fatalf("secret local crypto migration contains destructive statement %q", forbidden)
		}
	}
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
		"CREATE UNIQUE INDEX IF NOT EXISTS hot_memory_sources_memory_source_unique",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("hot memory migration missing %q", item)
		}
	}
}

func TestHotMemoryQdrantMigrationContainsRequiredPointTracking(t *testing.T) {
	sql := readMigration(t, "000017_hot_memory_qdrant_points.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS hot_memory_qdrant_points",
		"point_id TEXT NOT NULL UNIQUE",
		"memory_id TEXT NOT NULL REFERENCES hot_memories(memory_id) ON DELETE CASCADE",
		"collection_name TEXT NOT NULL",
		"payload JSONB NOT NULL",
		"vector_status TEXT NOT NULL DEFAULT 'pending'",
		"CREATE INDEX IF NOT EXISTS hot_memory_qdrant_points_memory_idx",
		"CREATE INDEX IF NOT EXISTS hot_memory_qdrant_points_status_idx",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("hot memory qdrant migration missing %q", item)
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

func TestRetrievalIdempotencyMigrationContainsRequiredIndex(t *testing.T) {
	sql := readMigration(t, "000012_retrieval_result_idempotency.sql")

	required := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS retrieval_results_request_rank_unique",
		"ON retrieval_results (request_id, rank)",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("retrieval idempotency migration missing %q", item)
		}
	}
}

func TestArchiveIndexJobLeaseMigrationContainsRequiredColumnsAndIndexes(t *testing.T) {
	sql := readMigration(t, "000014_archive_index_job_lease.sql")

	required := []string{
		"ALTER TABLE archive_index_jobs ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE archive_index_jobs ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 3",
		"ALTER TABLE archive_index_jobs ADD COLUMN IF NOT EXISTS locked_by TEXT",
		"ALTER TABLE archive_index_jobs ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ",
		"ALTER TABLE archive_index_jobs ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ",
		"CREATE INDEX IF NOT EXISTS archive_index_jobs_ready_idx",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("archive index job lease migration missing %q", item)
		}
	}
}

func TestTenantSoftDeleteMigrationContainsRequiredColumnsAndIndexes(t *testing.T) {
	sql := readMigration(t, "000015_tenant_soft_delete.sql")

	required := []string{
		"ALTER TABLE orgs ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'",
		"ALTER TABLE projects ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'",
		"CREATE INDEX IF NOT EXISTS orgs_status_idx",
		"CREATE INDEX IF NOT EXISTS projects_org_status_idx",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("tenant soft delete migration missing %q", item)
		}
	}
	for _, forbidden := range []string{"DROP TABLE", "DROP COLUMN", "DELETE FROM orgs", "DELETE FROM projects"} {
		if strings.Contains(strings.ToUpper(sql), forbidden) {
			t.Fatalf("tenant soft delete migration contains destructive statement %q", forbidden)
		}
	}
}

func TestTenantMembershipUpdatedAtMigrationContainsRequiredColumn(t *testing.T) {
	sql := readMigration(t, "000016_membership_updated_at.sql")

	required := []string{
		"ALTER TABLE memberships ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now()",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("tenant membership updated_at migration missing %q", item)
		}
	}
	for _, forbidden := range []string{"DROP TABLE", "DROP COLUMN", "DELETE FROM memberships"} {
		if strings.Contains(strings.ToUpper(sql), forbidden) {
			t.Fatalf("tenant membership updated_at migration contains destructive statement %q", forbidden)
		}
	}
}

func TestWorkspaceIdentityMigrationContainsRequiredProjectSourceFields(t *testing.T) {
	sql := readMigration(t, "000018_workspace_identity.sql")

	required := []string{
		"ALTER TABLE projects ADD COLUMN IF NOT EXISTS source_type TEXT",
		"ALTER TABLE projects ADD COLUMN IF NOT EXISTS source_key TEXT",
		"CREATE UNIQUE INDEX IF NOT EXISTS projects_source_unique",
		"CREATE INDEX IF NOT EXISTS projects_source_idx",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("workspace identity migration missing %q", item)
		}
	}
	for _, forbidden := range []string{"DROP TABLE", "DROP COLUMN", "DELETE FROM projects"} {
		if strings.Contains(strings.ToUpper(sql), forbidden) {
			t.Fatalf("workspace identity migration contains destructive statement %q", forbidden)
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

func TestCandidateMemoriesMigrationContainsRequiredTablesAndIndexes(t *testing.T) {
	sql := readMigration(t, "000019_candidate_memories.sql")

	required := []string{
		"CREATE TABLE IF NOT EXISTS candidate_memories",
		"CREATE TABLE IF NOT EXISTS candidate_memory_jobs",
		"CREATE TABLE IF NOT EXISTS topic_memory_states",
		// candidate_memories 字段
		"candidate_id TEXT NOT NULL UNIQUE",
		"org_id TEXT NOT NULL",
		"project_id TEXT NOT NULL",
		"source_key TEXT NOT NULL",
		"user_id TEXT NOT NULL",
		"agent_id TEXT NOT NULL",
		"thread_id TEXT NOT NULL",
		"session_id TEXT NOT NULL DEFAULT ''",
		"source_event_ids TEXT[] NOT NULL DEFAULT '{}'",
		"memory_type TEXT NOT NULL",
		"content TEXT NOT NULL",
		"summary TEXT NOT NULL DEFAULT ''",
		"risk_level TEXT NOT NULL DEFAULT 'low'",
		"confidence DOUBLE PRECISION NOT NULL DEFAULT 0",
		"status TEXT NOT NULL DEFAULT 'pending'",
		"similar_refs JSONB NOT NULL DEFAULT '[]'",
		"scores JSONB NOT NULL DEFAULT '{}'",
		// candidate_memory_jobs(archive_jobs 风格)
		"idempotency_key TEXT NOT NULL UNIQUE",
		"attempts INTEGER NOT NULL DEFAULT 0",
		"max_attempts INTEGER NOT NULL DEFAULT 3",
		"locked_by TEXT",
		"locked_until TIMESTAMPTZ",
		"last_error TEXT NOT NULL DEFAULT ''",
		"completed_at TIMESTAMPTZ",
		// topic_memory_states
		"candidate_count INTEGER NOT NULL DEFAULT 0",
		"completion_score DOUBLE PRECISION NOT NULL DEFAULT 0",
		"last_event_at TIMESTAMPTZ",
		"ready_to_compose BOOLEAN NOT NULL DEFAULT false",
		"composed_archive_id TEXT NOT NULL DEFAULT ''",
		// 索引
		"CREATE INDEX IF NOT EXISTS candidate_memories_scope_idx",
		"CREATE INDEX IF NOT EXISTS candidate_memories_status_idx",
		"CREATE INDEX IF NOT EXISTS candidate_memories_thread_idx",
		"CREATE INDEX IF NOT EXISTS candidate_memory_jobs_ready_idx",
		"CREATE UNIQUE INDEX IF NOT EXISTS topic_memory_states_topic_unique",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("candidate memories migration missing %q", item)
		}
	}

	for _, forbidden := range []string{"DROP TABLE", "DROP COLUMN"} {
		if strings.Contains(strings.ToUpper(sql), forbidden) {
			t.Fatalf("candidate memories migration contains destructive statement %q", forbidden)
		}
	}
}
