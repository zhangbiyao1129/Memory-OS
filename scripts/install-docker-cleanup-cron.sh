#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROJECT_DIR="${PROJECT_DIR:-$REPO_ROOT}"
CRON_SCHEDULE="${CRON_SCHEDULE:-43 4 * * *}"
LOG_FILE="${LOG_FILE:-$PROJECT_DIR/artifacts/docker-cleanup-cron.log}"
MAKE_BIN="${MAKE_BIN:-/usr/bin/make}"
DRY_RUN="${DRY_RUN:-1}"
CONFIRM_CRON_INSTALL="${CONFIRM_CRON_INSTALL:-}"
CRONTAB_CMD="${CRONTAB_CMD:-crontab}"
MARKER="memory-os docker dangling image cleanup"

cron_entry="$CRON_SCHEDULE cd $PROJECT_DIR && DRY_RUN=0 DOCKER_IMAGE_CLEANUP_MODE=dangling CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE $MAKE_BIN docker-cleanup-images >> $LOG_FILE 2>&1 # $MARKER"

echo "$cron_entry"

if [[ "$DRY_RUN" == "1" ]]; then
  echo "docker cleanup cron dry-run completed"
  exit 0
fi

if [[ "$CONFIRM_CRON_INSTALL" != "I_UNDERSTAND" ]]; then
  echo "real docker cleanup cron install requires CONFIRM_CRON_INSTALL=I_UNDERSTAND" >&2
  exit 1
fi

existing="$($CRONTAB_CMD -l 2>/dev/null || true)"
filtered="$(printf '%s\n' "$existing" | grep -v "$MARKER" || true)"
{
  printf '%s\n' "$filtered" | sed '/^$/d'
  printf '%s\n' "$cron_entry"
} | $CRONTAB_CMD -

echo "docker cleanup cron installed"
