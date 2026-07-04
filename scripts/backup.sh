#!/usr/bin/env bash
set -euo pipefail

umask 077

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_ID="${RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
BACKUP_ROOT="${BACKUP_ROOT:-$REPO_ROOT/backups}"
ARCHIVE_DIR="${ARCHIVE_DIR:-$REPO_ROOT/archives}"
RETENTION_DAYS="${RETENTION_DAYS:-30}"
DRY_RUN="${DRY_RUN:-0}"
COMPOSE="${COMPOSE:-docker-compose}"
COMPOSE_FILE="${COMPOSE_FILE:-$REPO_ROOT/deploy/docker-compose.yml}"
COMPOSE_T480_FILE="${COMPOSE_T480_FILE:-$REPO_ROOT/deploy/docker-compose.t480.yml}"
POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
POSTGRES_DB="${POSTGRES_DB:-memory_os}"
POSTGRES_USER="${POSTGRES_USER:-memory_os}"
QDRANT_URL="${QDRANT_URL:-http://localhost:18083}"
QDRANT_COLLECTION="${QDRANT_COLLECTION:-memory_os}"

DEST="$BACKUP_ROOT/$RUN_ID"
POSTGRES_DIR="$DEST/postgres"
ARCHIVES_DIR="$DEST/archives"
QDRANT_DIR="$DEST/qdrant"

mkdir -p "$POSTGRES_DIR" "$ARCHIVES_DIR" "$QDRANT_DIR"

write_file() {
  local path="$1"
  shift
  printf '%s\n' "$*" > "$path"
}

sha256_file() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
    return
  fi
  shasum -a 256 "$path" | awk '{print $1}'
}

backup_postgres() {
  local command_text="$COMPOSE -f $COMPOSE_FILE -f $COMPOSE_T480_FILE exec -T $POSTGRES_SERVICE pg_dump -U $POSTGRES_USER -d $POSTGRES_DB"
  write_file "$POSTGRES_DIR/pg_dump.command" "$command_text > $POSTGRES_DIR/$POSTGRES_DB.sql"
  if [[ "$DRY_RUN" == "1" ]]; then
    write_file "$POSTGRES_DIR/$POSTGRES_DB.sql" "-- dry-run PostgreSQL dump placeholder for $POSTGRES_DB"
    return
  fi
  if ! command -v "$COMPOSE" >/dev/null 2>&1; then
    echo "missing compose command: $COMPOSE" >&2
    exit 1
  fi
  "$COMPOSE" -f "$COMPOSE_FILE" -f "$COMPOSE_T480_FILE" exec -T "$POSTGRES_SERVICE" pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" > "$POSTGRES_DIR/$POSTGRES_DB.sql"
}

backup_archives() {
  local archive_tar="$ARCHIVES_DIR/markdown-archive.tar.gz"
  if [[ -d "$ARCHIVE_DIR" ]]; then
    tar -C "$ARCHIVE_DIR" -czf "$archive_tar" .
  else
    mkdir -p "$DEST/empty-archives"
    tar -C "$DEST/empty-archives" -czf "$archive_tar" .
  fi
}

backup_qdrant() {
  local snapshot_endpoint="$QDRANT_URL/collections/$QDRANT_COLLECTION/snapshots"
  write_file "$QDRANT_DIR/snapshot.command" "curl -fsS -X POST $snapshot_endpoint"
  if [[ "$DRY_RUN" == "1" ]]; then
    write_file "$QDRANT_DIR/snapshot-response.json" '{"name":"dry-run.snapshot"}'
    write_file "$QDRANT_DIR/dry-run.snapshot" "dry-run qdrant snapshot placeholder"
    return
  fi
  local response snapshot_name
  response="$(curl -fsS -X POST "$snapshot_endpoint")"
  printf '%s\n' "$response" > "$QDRANT_DIR/snapshot-response.json"
  snapshot_name="$(printf '%s' "$response" | sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  if [[ -z "$snapshot_name" ]]; then
    echo "qdrant snapshot response did not include a snapshot name" >&2
    exit 1
  fi
  curl -fsS "$snapshot_endpoint/$snapshot_name" -o "$QDRANT_DIR/$snapshot_name"
}

write_manifest() {
  local completed_at postgres_sha archives_sha qdrant_file qdrant_sha
  completed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  postgres_sha="$(sha256_file "$POSTGRES_DIR/$POSTGRES_DB.sql")"
  archives_sha="$(sha256_file "$ARCHIVES_DIR/markdown-archive.tar.gz")"
  qdrant_file="$(find "$QDRANT_DIR" -maxdepth 1 -type f ! -name '*.command' ! -name 'snapshot-response.json' | head -n 1)"
  if [[ -z "$qdrant_file" ]]; then
    echo "missing Qdrant snapshot artifact" >&2
    exit 1
  fi
  qdrant_sha="$(sha256_file "$qdrant_file")"
  cat > "$DEST/manifest.json" <<JSON
{"run_id":"$RUN_ID","completed_at":"$completed_at","retention_days":$RETENTION_DAYS,"postgres":{"database":"$POSTGRES_DB","file":"postgres/$POSTGRES_DB.sql","sha256":"$postgres_sha"},"archives":{"source":"$ARCHIVE_DIR","file":"archives/markdown-archive.tar.gz","sha256":"$archives_sha"},"qdrant":{"collection":"$QDRANT_COLLECTION","source":"$QDRANT_URL","file":"qdrant/$(basename "$qdrant_file")","sha256":"$qdrant_sha"}}
JSON
}

cleanup_old_backups() {
  find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d -mtime +"$RETENTION_DAYS" -exec rm -rf {} +
}

backup_postgres
backup_archives
backup_qdrant
write_manifest
cleanup_old_backups

echo "backup completed: $DEST"
