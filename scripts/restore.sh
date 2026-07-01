#!/usr/bin/env bash
set -euo pipefail

umask 077

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKUP_DIR="${BACKUP_DIR:-}"
DRY_RUN="${DRY_RUN:-1}"
CONFIRM_RESTORE="${CONFIRM_RESTORE:-}"
RESTORE_AUDIT_DIR="${RESTORE_AUDIT_DIR:-$REPO_ROOT/artifacts/restore-$(date -u +%Y%m%dT%H%M%SZ)}"
COMPOSE="${COMPOSE:-docker-compose}"
COMPOSE_FILE="${COMPOSE_FILE:-$REPO_ROOT/deploy/docker-compose.yml}"
COMPOSE_T480_FILE="${COMPOSE_T480_FILE:-$REPO_ROOT/deploy/docker-compose.t480.yml}"
POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
POSTGRES_DB="${POSTGRES_DB:-memory_os}"
POSTGRES_USER="${POSTGRES_USER:-memory_os}"
ARCHIVE_DIR="${ARCHIVE_DIR:-$REPO_ROOT/archives}"
QDRANT_URL="${QDRANT_URL:-http://localhost:18083}"
QDRANT_COLLECTION="${QDRANT_COLLECTION:-memory_os}"
SMOKE_CMD="${SMOKE_CMD:-make smoke}"

if [[ -z "$BACKUP_DIR" ]]; then
  echo "BACKUP_DIR is required" >&2
  exit 1
fi
if [[ ! -d "$BACKUP_DIR" ]]; then
  echo "BACKUP_DIR does not exist: $BACKUP_DIR" >&2
  exit 1
fi

POSTGRES_DUMP="$BACKUP_DIR/postgres/$POSTGRES_DB.sql"
ARCHIVE_TAR="$BACKUP_DIR/archives/markdown-archive.tar.gz"
QDRANT_SNAPSHOT="$(find "$BACKUP_DIR/qdrant" -maxdepth 1 -type f -name '*.snapshot' | head -n 1 || true)"

if [[ ! -f "$POSTGRES_DUMP" ]]; then
  echo "missing PostgreSQL dump: $POSTGRES_DUMP" >&2
  exit 1
fi
if [[ ! -f "$ARCHIVE_TAR" ]]; then
  echo "missing Markdown archive tarball: $ARCHIVE_TAR" >&2
  exit 1
fi
if [[ -z "$QDRANT_SNAPSHOT" || ! -f "$QDRANT_SNAPSHOT" ]]; then
  echo "missing Qdrant snapshot under $BACKUP_DIR/qdrant" >&2
  exit 1
fi

mkdir -p "$RESTORE_AUDIT_DIR"

POSTGRES_COMMAND="$COMPOSE -f $COMPOSE_FILE -f $COMPOSE_T480_FILE exec -T $POSTGRES_SERVICE psql -U $POSTGRES_USER -d $POSTGRES_DB < $POSTGRES_DUMP"
ARCHIVES_COMMAND="mkdir -p $ARCHIVE_DIR && tar -C $ARCHIVE_DIR -xzf $ARCHIVE_TAR"
QDRANT_COMMAND="curl -fsS -X POST $QDRANT_URL/collections/$QDRANT_COLLECTION/snapshots/upload -H Content-Type:multipart/form-data -F snapshot=@$QDRANT_SNAPSHOT"

printf '%s\n' "$POSTGRES_COMMAND" > "$RESTORE_AUDIT_DIR/postgres.restore.command"
printf '%s\n' "$ARCHIVES_COMMAND" > "$RESTORE_AUDIT_DIR/archives.restore.command"
printf '%s\n' "$QDRANT_COMMAND" > "$RESTORE_AUDIT_DIR/qdrant.restore.command"

if [[ "$DRY_RUN" == "1" ]]; then
  echo "restore dry-run completed: $RESTORE_AUDIT_DIR"
  exit 0
fi

if [[ "$CONFIRM_RESTORE" != "I_UNDERSTAND" ]]; then
  echo "real restore requires CONFIRM_RESTORE=I_UNDERSTAND" >&2
  exit 1
fi
if ! command -v "$COMPOSE" >/dev/null 2>&1; then
  echo "missing compose command: $COMPOSE" >&2
  exit 1
fi

bash -lc "$POSTGRES_COMMAND"
bash -lc "$ARCHIVES_COMMAND"
bash -lc "$QDRANT_COMMAND"
(cd "$REPO_ROOT" && bash -lc "$SMOKE_CMD")

echo "restore completed: $BACKUP_DIR"
