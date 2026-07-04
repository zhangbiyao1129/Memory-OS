#!/usr/bin/env bash
set -euo pipefail

umask 077

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

shell_quote() {
  local value="$1"
  printf "'%s'" "${value//\'/\'\\\'\'}"
}

BACKUP_DIR="${BACKUP_DIR:-}"
RESTORE_REHEARSAL_MODE="${RESTORE_REHEARSAL_MODE:-dry-run}"
RESTORE_REHEARSAL_PROJECT="${RESTORE_REHEARSAL_PROJECT:-memory-os-restore-rehearsal}"
RESTORE_REHEARSAL_AUDIT_DIR="${RESTORE_REHEARSAL_AUDIT_DIR:-$REPO_ROOT/artifacts/restore-rehearsal-$(date -u +%Y%m%dT%H%M%SZ)}"
CONFIRM_RESTORE_REHEARSAL="${CONFIRM_RESTORE_REHEARSAL:-}"
PREFLIGHT_CMD="${PREFLIGHT_CMD:-bash scripts/restore-rehearsal-preflight.sh}"
RESTORE_CMD="${RESTORE_CMD:-scripts/restore.sh}"
RESTORE_REHEARSAL_COMPOSE_FILE="${RESTORE_REHEARSAL_COMPOSE_FILE:-deploy/docker-compose.restore-rehearsal.yml}"
RESTORE_REHEARSAL_USES_DEFAULT_UP=false
if [[ -z "${RESTORE_REHEARSAL_UP_CMD:-}" && -z "${RESTORE_REHEARSAL_INFRA_UP_CMD:-}" && -z "${RESTORE_REHEARSAL_APP_UP_CMD:-}" ]]; then
  RESTORE_REHEARSAL_USES_DEFAULT_UP=true
  RESTORE_REHEARSAL_INFRA_UP_CMD="docker-compose -p $(shell_quote "$RESTORE_REHEARSAL_PROJECT") -f $(shell_quote "$RESTORE_REHEARSAL_COMPOSE_FILE") up -d postgres redis qdrant"
  RESTORE_REHEARSAL_APP_UP_CMD="docker-compose -p $(shell_quote "$RESTORE_REHEARSAL_PROJECT") -f $(shell_quote "$RESTORE_REHEARSAL_COMPOSE_FILE") up -d memory-api memory-worker memory-mcp memory-web"
  RESTORE_REHEARSAL_WAIT_CMD="for i in {1..60}; do docker-compose -p $(shell_quote "$RESTORE_REHEARSAL_PROJECT") -f $(shell_quote "$RESTORE_REHEARSAL_COMPOSE_FILE") exec -T postgres pg_isready -U memory_os -d memory_os >/dev/null 2>&1 && docker run --rm --network $(shell_quote "${RESTORE_REHEARSAL_PROJECT}_default") -e NO_PROXY=qdrant,postgres,redis,memory-api,memory-worker,memory-mcp,memory-web,localhost,127.0.0.1 -e no_proxy=qdrant,postgres,redis,memory-api,memory-worker,memory-mcp,memory-web,localhost,127.0.0.1 curlimages/curl:8.10.1 -fsS http://qdrant:6333/healthz >/dev/null 2>&1 && exit 0; sleep 1; done; echo 'restore rehearsal infrastructure did not become healthy' >&2; exit 1"
fi
if [[ -z "${RESTORE_REHEARSAL_DOWN_CMD:-}" ]]; then
  RESTORE_REHEARSAL_DOWN_CMD="docker-compose -p $(shell_quote "$RESTORE_REHEARSAL_PROJECT") -f $(shell_quote "$RESTORE_REHEARSAL_COMPOSE_FILE") down -v"
fi
if [[ -z "${SMOKE_CMD:-}" ]]; then
  SMOKE_CMD="docker run --rm --network $(shell_quote "${RESTORE_REHEARSAL_PROJECT}_default") -e SMOKE_API_URL=http://memory-api:18081/healthz -e SMOKE_QDRANT_URL=http://qdrant:6333 -e SMOKE_MCP_URL=http://memory-mcp:18082 -e SMOKE_WEB_URL=http://memory-web:18080 -e SMOKE_TIMEOUT=3m -e SMOKE_ENABLE_DEV_ENDPOINTS=false -e NO_PROXY=qdrant,postgres,redis,memory-api,memory-worker,memory-mcp,memory-web,localhost,127.0.0.1 -e no_proxy=qdrant,postgres,redis,memory-api,memory-worker,memory-mcp,memory-web,localhost,127.0.0.1 -v $(shell_quote "$REPO_ROOT:/src") -w /src golang:1.25-bookworm go run ./cmd/memory-smoke"
fi

if [[ -z "$BACKUP_DIR" ]]; then
  echo "BACKUP_DIR is required" >&2
  exit 1
fi

case "$RESTORE_REHEARSAL_PROJECT" in
  deploy|memory-os|memory_os|memoryos|production|prod)
    echo "RESTORE_REHEARSAL_PROJECT must not target production project: $RESTORE_REHEARSAL_PROJECT" >&2
    exit 1
    ;;
esac

mkdir -p "$RESTORE_REHEARSAL_AUDIT_DIR"
cat > "$RESTORE_REHEARSAL_AUDIT_DIR/rehearsal-plan.txt" <<PLAN
backup_dir=$BACKUP_DIR
mode=$RESTORE_REHEARSAL_MODE
project=$RESTORE_REHEARSAL_PROJECT
restore_target_env=test
PLAN

case "$RESTORE_REHEARSAL_MODE" in
  dry-run)
    BACKUP_DIR="$BACKUP_DIR" \
      DRY_RUN=1 \
      RESTORE_TARGET_ENV=test \
      COMPOSE_FILE="$RESTORE_REHEARSAL_COMPOSE_FILE" \
      COMPOSE_T480_FILE="" \
      COMPOSE_PROJECT_NAME="$RESTORE_REHEARSAL_PROJECT" \
      QDRANT_URL="${QDRANT_URL:-http://qdrant:6333}" \
      QDRANT_RESTORE_DOCKER_NETWORK="${QDRANT_RESTORE_DOCKER_NETWORK:-${RESTORE_REHEARSAL_PROJECT}_default}" \
      RESTORE_AUDIT_DIR="$RESTORE_REHEARSAL_AUDIT_DIR/restore" \
      ARCHIVE_DIR="$RESTORE_REHEARSAL_AUDIT_DIR/archive" \
      bash -lc "$RESTORE_CMD"
    echo "restore rehearsal dry-run completed: $RESTORE_REHEARSAL_AUDIT_DIR"
    ;;
  real)
    if [[ "$CONFIRM_RESTORE_REHEARSAL" != "I_UNDERSTAND_TEST_RESTORE" ]]; then
      echo "real restore rehearsal requires CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE" >&2
      exit 1
    fi
    BACKUP_DIR="$BACKUP_DIR" \
      RESTORE_REHEARSAL_PROJECT="$RESTORE_REHEARSAL_PROJECT" \
      RESTORE_REHEARSAL_COMPOSE_FILE="$RESTORE_REHEARSAL_COMPOSE_FILE" \
      RESTORE_REHEARSAL_AUDIT_DIR="$RESTORE_REHEARSAL_AUDIT_DIR/preflight" \
      bash -lc "$PREFLIGHT_CMD"
    cleanup_rehearsal() {
      bash -lc "$RESTORE_REHEARSAL_DOWN_CMD"
    }
    trap cleanup_rehearsal EXIT
    if [[ "$RESTORE_REHEARSAL_USES_DEFAULT_UP" == "true" ]]; then
      . scripts/load-prod-env.sh
    fi
    if [[ -n "${RESTORE_REHEARSAL_UP_CMD:-}" ]]; then
      bash -lc "$RESTORE_REHEARSAL_UP_CMD"
    else
      bash -lc "$RESTORE_REHEARSAL_INFRA_UP_CMD"
    fi
    if [[ -n "${RESTORE_REHEARSAL_WAIT_CMD:-}" ]]; then
      bash -lc "$RESTORE_REHEARSAL_WAIT_CMD"
    fi
    BACKUP_DIR="$BACKUP_DIR" \
      DRY_RUN=0 \
      CONFIRM_RESTORE=I_UNDERSTAND \
      RESTORE_TARGET_ENV=test \
      COMPOSE_FILE="$RESTORE_REHEARSAL_COMPOSE_FILE" \
      COMPOSE_T480_FILE="" \
      COMPOSE_PROJECT_NAME="$RESTORE_REHEARSAL_PROJECT" \
      QDRANT_URL="${QDRANT_URL:-http://qdrant:6333}" \
      QDRANT_RESTORE_DOCKER_NETWORK="${QDRANT_RESTORE_DOCKER_NETWORK:-${RESTORE_REHEARSAL_PROJECT}_default}" \
      RESTORE_AUDIT_DIR="$RESTORE_REHEARSAL_AUDIT_DIR/restore" \
      ARCHIVE_DIR="$RESTORE_REHEARSAL_AUDIT_DIR/archive" \
      bash -lc "$RESTORE_CMD"
    if [[ -n "${RESTORE_REHEARSAL_APP_UP_CMD:-}" ]]; then
      bash -lc "$RESTORE_REHEARSAL_APP_UP_CMD"
    fi
    bash -lc "$SMOKE_CMD"
    trap - EXIT
    cleanup_rehearsal
    echo "restore rehearsal completed: $RESTORE_REHEARSAL_AUDIT_DIR"
    ;;
  *)
    echo "unsupported RESTORE_REHEARSAL_MODE: $RESTORE_REHEARSAL_MODE" >&2
    echo "supported modes: dry-run, real" >&2
    exit 1
    ;;
esac
