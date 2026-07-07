#!/usr/bin/env bash
set -euo pipefail

# 本地到 T480 的开发同步脚本。GitHub 只作为稳定节点仓库使用。
# 默认不删除远端文件，避免误删 T480 上的生产环境配置和运行产物。

TARGET_HOST="${TARGET_HOST:-thinkpad}"
TARGET_DIR="${TARGET_DIR:-/opt/memory-os}"
DRY_RUN="${DRY_RUN:-0}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

memory_os_write_build_info() {
  if [[ "$DRY_RUN" == "1" ]]; then
    return 0
  fi

  local build_version="${BUILD_VERSION:-0.9.0-dev}"
  local build_commit="${BUILD_COMMIT:-unknown}"
  local build_dirty="${BUILD_DIRTY:-unknown}"
  local build_time="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

  if command -v git >/dev/null 2>&1 && git -C "$REPO_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    if [[ -z "${BUILD_COMMIT:-}" ]]; then
      build_commit="$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
    fi
    if [[ -z "${BUILD_DIRTY:-}" ]]; then
      if [[ -z "$(git -C "$REPO_ROOT" status --porcelain)" ]]; then
        build_dirty=false
      else
        build_dirty=true
      fi
    fi
  fi

  local remote_file="${TARGET_DIR%/}/.build-info.env"
  local remote_tmp="${remote_file}.$$"
  {
    printf 'BUILD_VERSION=%s\n' "$build_version"
    printf 'BUILD_COMMIT=%s\n' "$build_commit"
    printf 'BUILD_DIRTY=%s\n' "$build_dirty"
    printf 'BUILD_TIME=%s\n' "$build_time"
  } | ssh "$TARGET_HOST" "umask 077 && cat > $(printf '%q' "$remote_tmp") && mv $(printf '%q' "$remote_tmp") $(printf '%q' "$remote_file")"
}

RSYNC_ARGS=(
  -az
  --no-owner
  --no-group
  --human-readable
  --itemize-changes
  --exclude=.git/
  --exclude=.DS_Store
  --include=.env.example
  --exclude=.env
  --exclude=.env.*
  --exclude=.build-info.env
  --exclude=.gocache/
  --exclude=.playwright-mcp/
  --exclude=.codebase-memory/
  --exclude=frontend/node_modules/
  --exclude=frontend/.nuxt/
  --exclude=frontend/.output/
  --exclude=frontend/dist/
  --exclude=artifacts/
  --exclude=docs/
  --exclude=specs/
  --exclude=backups/
  --exclude=tmp/
)

if [[ "$DRY_RUN" == "1" ]]; then
  RSYNC_ARGS+=(--dry-run)
fi

echo "syncing $REPO_ROOT/ -> $TARGET_HOST:$TARGET_DIR"
rsync "${RSYNC_ARGS[@]}" "$REPO_ROOT/" "$TARGET_HOST:$TARGET_DIR/"
memory_os_write_build_info
