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
if [[ -z "${COMPOSE_T480_FILE+x}" ]]; then
  COMPOSE_T480_FILE="$REPO_ROOT/deploy/docker-compose.t480.yml"
fi
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
MANIFEST="$BACKUP_DIR/manifest.json"

if [[ ! -f "$MANIFEST" ]]; then
  echo "missing backup manifest: $MANIFEST" >&2
  exit 1
fi
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

sha256_file() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
    return
  fi
  shasum -a 256 "$path" | awk '{print $1}'
}

shell_quote() {
  local value="$1"
  printf "'%s'" "${value//\'/\'\\\'\'}"
}

manifest_sha() {
  local section="$1"
  sed -n "s/.*\"$section\"[[:space:]]*:[[:space:]]*{[^}]*\"sha256\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$MANIFEST" | head -n 1
}

verify_checksum() {
  local label="$1" path="$2" expected actual
  expected="$(manifest_sha "$label")"
  if [[ -z "$expected" ]]; then
    echo "missing checksum for $label in manifest" >&2
    exit 1
  fi
  actual="$(sha256_file "$path")"
  if [[ "$actual" != "$expected" ]]; then
    echo "checksum mismatch for $label: expected $expected got $actual" >&2
    exit 1
  fi
}

verify_checksum postgres "$POSTGRES_DUMP"
verify_checksum archives "$ARCHIVE_TAR"
verify_checksum qdrant "$QDRANT_SNAPSHOT"

mkdir -p "$RESTORE_AUDIT_DIR"

compose_file_args() {
  printf -- "-f %s" "$(shell_quote "$COMPOSE_FILE")"
  if [[ -n "$COMPOSE_T480_FILE" ]]; then
    printf -- " -f %s" "$(shell_quote "$COMPOSE_T480_FILE")"
  fi
}

POSTGRES_COMMAND="$COMPOSE $(compose_file_args) exec -T $POSTGRES_SERVICE psql -U $POSTGRES_USER -d $POSTGRES_DB < $(shell_quote "$POSTGRES_DUMP")"
ARCHIVES_COMMAND="mkdir -p $(shell_quote "$ARCHIVE_DIR") && tar -C $(shell_quote "$ARCHIVE_DIR") -xzf $(shell_quote "$ARCHIVE_TAR")"
if [[ -n "${QDRANT_RESTORE_DOCKER_NETWORK:-}" ]]; then
  QDRANT_SNAPSHOT_DIR="$(dirname "$QDRANT_SNAPSHOT")"
  QDRANT_SNAPSHOT_NAME="$(basename "$QDRANT_SNAPSHOT")"
  QDRANT_COMMAND="docker run --rm --user 0:0 --network $QDRANT_RESTORE_DOCKER_NETWORK -e NO_PROXY=qdrant,postgres,redis,memory-api,memory-worker,memory-mcp,memory-web,localhost,127.0.0.1 -e no_proxy=qdrant,postgres,redis,memory-api,memory-worker,memory-mcp,memory-web,localhost,127.0.0.1 -v $(shell_quote "$QDRANT_SNAPSHOT_DIR"):/snapshot:ro curlimages/curl:8.10.1 -fsS -X POST $(shell_quote "$QDRANT_URL/collections/$QDRANT_COLLECTION/snapshots/upload") -H 'Content-Type:multipart/form-data' -F $(shell_quote "snapshot=@/snapshot/$QDRANT_SNAPSHOT_NAME")"
else
  QDRANT_COMMAND="curl -fsS -X POST $(shell_quote "$QDRANT_URL/collections/$QDRANT_COLLECTION/snapshots/upload") -H 'Content-Type:multipart/form-data' -F $(shell_quote "snapshot=@$QDRANT_SNAPSHOT")"
fi

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
RESTORE_TARGET_ENV="${RESTORE_TARGET_ENV:-}"
case "$RESTORE_TARGET_ENV" in
  test)
    ;;
  production)
    if [[ "${CONFIRM_PRODUCTION_RESTORE:-}" != "I_UNDERSTAND_PRODUCTION_DATA_OVERWRITE" ]]; then
      echo "production restore requires CONFIRM_PRODUCTION_RESTORE=I_UNDERSTAND_PRODUCTION_DATA_OVERWRITE" >&2
      exit 1
    fi
    ;;
  *)
    echo "real restore requires RESTORE_TARGET_ENV=test or RESTORE_TARGET_ENV=production" >&2
    exit 1
    ;;
esac
if ! command -v "$COMPOSE" >/dev/null 2>&1; then
  echo "missing compose command: $COMPOSE" >&2
  exit 1
fi

bash -lc "$POSTGRES_COMMAND"
bash -lc "$ARCHIVES_COMMAND"
bash -lc "$QDRANT_COMMAND"
(cd "$REPO_ROOT" && bash -lc "$SMOKE_CMD")

echo "restore completed: $BACKUP_DIR"
