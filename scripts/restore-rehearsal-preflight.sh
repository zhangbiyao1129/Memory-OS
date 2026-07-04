#!/usr/bin/env bash
set -euo pipefail

umask 077

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKUP_DIR="${BACKUP_DIR:-}"
RESTORE_REHEARSAL_PROJECT="${RESTORE_REHEARSAL_PROJECT:-memory-os-restore-rehearsal}"
RESTORE_REHEARSAL_COMPOSE_FILE="${RESTORE_REHEARSAL_COMPOSE_FILE:-deploy/docker-compose.restore-rehearsal.yml}"
RESTORE_REHEARSAL_AUDIT_DIR="${RESTORE_REHEARSAL_AUDIT_DIR:-$REPO_ROOT/artifacts/restore-rehearsal-preflight-$(date -u +%Y%m%dT%H%M%SZ)}"
DOCKER_CMD="${DOCKER_CMD:-docker}"

case "$RESTORE_REHEARSAL_PROJECT" in
  deploy|memory-os|memory_os|memoryos|production|prod)
    echo "RESTORE_REHEARSAL_PROJECT must not target production project: $RESTORE_REHEARSAL_PROJECT" >&2
    exit 1
    ;;
esac

if [[ -z "$BACKUP_DIR" ]]; then
  echo "BACKUP_DIR is required" >&2
  exit 1
fi
if [[ ! -d "$BACKUP_DIR" ]]; then
  echo "BACKUP_DIR does not exist: $BACKUP_DIR" >&2
  exit 1
fi
if [[ ! -f "$BACKUP_DIR/manifest.json" ]]; then
  echo "missing backup manifest: $BACKUP_DIR/manifest.json" >&2
  exit 1
fi
if [[ ! -f "$RESTORE_REHEARSAL_COMPOSE_FILE" ]]; then
  echo "restore rehearsal compose file does not exist: $RESTORE_REHEARSAL_COMPOSE_FILE" >&2
  exit 1
fi

compose_content="$(cat "$RESTORE_REHEARSAL_COMPOSE_FILE")"
for forbidden in \
  "ports:" \
  "postgres_data:" \
  "redis_data:" \
  "qdrant_data:" \
  "archive_data:"
do
  if grep -q "$forbidden" <<<"$compose_content"; then
    echo "restore rehearsal compose must not contain production/exposed marker: $forbidden" >&2
    exit 1
  fi
done

for required in \
  "name: memory-os-restore-rehearsal" \
  "restore_rehearsal_pg:" \
  "restore_rehearsal_qdrant:" \
  "restore_rehearsal_archive:" \
  "QDRANT_URL: http://qdrant:6333" \
  "ARCHIVE_DIR: /data/memory-os"
do
  if ! grep -q "$required" <<<"$compose_content"; then
    echo "restore rehearsal compose missing isolation marker: $required" >&2
    exit 1
  fi
done

if command -v "$DOCKER_CMD" >/dev/null 2>&1; then
  existing_containers="$($DOCKER_CMD ps -a --filter "label=com.docker.compose.project=$RESTORE_REHEARSAL_PROJECT" -q 2>/dev/null || true)"
  if [[ -n "$existing_containers" ]]; then
    echo "restore rehearsal project has existing containers; clean or inspect before real rehearsal: $RESTORE_REHEARSAL_PROJECT" >&2
    exit 1
  fi
  existing_volumes="$($DOCKER_CMD volume ls --filter "label=com.docker.compose.project=$RESTORE_REHEARSAL_PROJECT" -q 2>/dev/null || true)"
  if [[ -n "$existing_volumes" ]]; then
    echo "restore rehearsal project has existing volumes; clean or inspect before real rehearsal: $RESTORE_REHEARSAL_PROJECT" >&2
    exit 1
  fi
fi

mkdir -p "$RESTORE_REHEARSAL_AUDIT_DIR"
cat > "$RESTORE_REHEARSAL_AUDIT_DIR/preflight.txt" <<PREFLIGHT
backup_dir=$BACKUP_DIR
project=$RESTORE_REHEARSAL_PROJECT
compose_file=$RESTORE_REHEARSAL_COMPOSE_FILE
status=ok
PREFLIGHT

echo "restore rehearsal preflight ok: $RESTORE_REHEARSAL_AUDIT_DIR"
