#!/usr/bin/env bash
set -euo pipefail

umask 077

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DRY_RUN="${DRY_RUN:-1}"
DOCKER_IMAGE_CLEANUP_MODE="${DOCKER_IMAGE_CLEANUP_MODE:-dangling}"
CONFIRM_DOCKER_IMAGE_CLEANUP="${CONFIRM_DOCKER_IMAGE_CLEANUP:-}"
DOCKER_CLEANUP_AUDIT_DIR="${DOCKER_CLEANUP_AUDIT_DIR:-$REPO_ROOT/artifacts/docker-cleanup-$(date -u +%Y%m%dT%H%M%SZ)}"

case "$DOCKER_IMAGE_CLEANUP_MODE" in
  dangling)
    CLEANUP_COMMAND="docker image prune -f"
    ;;
  unused-24h)
    CLEANUP_COMMAND='docker image prune -a --filter "until=24h" -f'
    ;;
  *)
    echo "unsupported DOCKER_IMAGE_CLEANUP_MODE: $DOCKER_IMAGE_CLEANUP_MODE" >&2
    echo "supported modes: dangling, unused-24h" >&2
    exit 1
    ;;
esac

mkdir -p "$DOCKER_CLEANUP_AUDIT_DIR"
printf '%s\n' "$CLEANUP_COMMAND" > "$DOCKER_CLEANUP_AUDIT_DIR/docker-image-cleanup.command"

if [[ "$DRY_RUN" == "1" ]]; then
  echo "docker image cleanup dry-run completed: $DOCKER_CLEANUP_AUDIT_DIR"
  echo "planned command: $CLEANUP_COMMAND"
  exit 0
fi

if [[ "$CONFIRM_DOCKER_IMAGE_CLEANUP" != "I_UNDERSTAND_IMAGE_DELETE" ]]; then
  echo "real docker image cleanup requires CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is not available" >&2
  exit 1
fi

docker system df > "$DOCKER_CLEANUP_AUDIT_DIR/docker-system-before.txt" || true
bash -lc "$CLEANUP_COMMAND"
docker system df > "$DOCKER_CLEANUP_AUDIT_DIR/docker-system-after.txt" || true

echo "docker image cleanup completed: $DOCKER_CLEANUP_AUDIT_DIR"
