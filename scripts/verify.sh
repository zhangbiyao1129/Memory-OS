#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PREFLIGHT_CMD="${PREFLIGHT_CMD:-ALLOW_EXISTING_DEPLOYMENT=1 make preflight}"
SECRET_SCAN_CMD="${SECRET_SCAN_CMD:-make secret-scan}"
GO_TEST_CMD="${GO_TEST_CMD:-make test}"
WEB_BUILD_CMD="${WEB_BUILD_CMD:-make build-web}"
NPM_AUDIT_CMD="${NPM_AUDIT_CMD:-npm --prefix web audit --omit=dev --audit-level=high --registry=https://registry.npmjs.org}"
SMOKE_CMD="${SMOKE_CMD:-make smoke}"
BACKUP_DRY_RUN_CMD="${BACKUP_DRY_RUN_CMD:-DRY_RUN=1 RUN_ID=verify-dry-run BACKUP_ROOT=/tmp/memory-os-verify-backup ARCHIVE_DIR=/tmp/memory-os-verify-archive make backup}"
RESTORE_DRY_RUN_CMD="${RESTORE_DRY_RUN_CMD:-BACKUP_DIR=/tmp/memory-os-verify-backup/verify-dry-run RESTORE_AUDIT_DIR=/tmp/memory-os-verify-restore make restore}"
CRON_DRY_RUN_CMD="${CRON_DRY_RUN_CMD:-PROJECT_DIR=/opt/memory-os make install-backup-cron}"

run_step() {
  local name="$1"
  local command_text="$2"
  printf '==> %s\n' "$name"
  bash -lc "$command_text"
}

run_step "preflight" "$PREFLIGHT_CMD"
run_step "secret scan" "$SECRET_SCAN_CMD"
run_step "go test" "$GO_TEST_CMD"
run_step "web build" "$WEB_BUILD_CMD"
run_step "npm audit" "$NPM_AUDIT_CMD"
run_step "smoke" "$SMOKE_CMD"
run_step "backup dry-run" "$BACKUP_DRY_RUN_CMD"
run_step "restore dry-run" "$RESTORE_DRY_RUN_CMD"
run_step "backup cron dry-run" "$CRON_DRY_RUN_CMD"

echo "verify completed"
