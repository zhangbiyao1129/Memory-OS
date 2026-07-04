#!/usr/bin/env bash
set -euo pipefail

DRY_RUN_ONLY=1
REPORT_LIMIT="${DOCKER_CLEANUP_PLAN_LIMIT:-80}"

echo "Memory OS Docker cleanup plan (dry-run only)"
echo "This script only prints evidence and suggested cleanup commands."
echo "It does not delete containers, images, volumes, networks, or Memory OS data."
echo "docker volume prune is intentionally excluded."
echo

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is not available; cannot inspect local Docker state."
  exit 0
fi

run_or_warn() {
  local title="$1"
  shift
  echo "==> ${title}"
  if ! "$@"; then
    echo "warning: command failed: $*"
  fi
  echo
}

run_shell_or_warn() {
  local title="$1"
  local command_text="$2"
  echo "==> ${title}"
  if ! bash -lc "$command_text"; then
    echo "warning: command failed: ${command_text}"
  fi
  echo
}

run_or_warn "root filesystem usage" df -h /
run_or_warn "docker system df" docker system df
run_shell_or_warn "running container images" "docker ps --format '{{.Image}}' | sort -u | head -n ${REPORT_LIMIT}"
run_shell_or_warn "dangling images (first ${REPORT_LIMIT})" "docker image ls --filter dangling=true --format 'table {{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.Size}}\t{{.CreatedSince}}' | head -n ${REPORT_LIMIT}"
run_shell_or_warn "all images by size (first ${REPORT_LIMIT})" "docker image ls --format 'table {{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.Size}}\t{{.CreatedSince}}' | head -n ${REPORT_LIMIT}"

echo "Recommended staged commands after explicit user confirmation:"
printf '%s\n' '1. docker image prune -f'
printf '%s\n' '2. docker image prune -a --filter "until=24h" -f'
echo
echo "Do not run docker volume prune for Memory OS without a backup/restore confirmation."
echo "cleanup plan completed"
