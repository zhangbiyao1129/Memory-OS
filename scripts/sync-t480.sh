#!/usr/bin/env bash
set -euo pipefail

# 本地到 T480 的开发同步脚本。GitHub 只作为稳定节点仓库使用。
# 默认不删除远端文件，避免误删 T480 上的生产环境配置和运行产物。

TARGET_HOST="${TARGET_HOST:-thinkpad}"
TARGET_DIR="${TARGET_DIR:-/opt/memory-os}"
DRY_RUN="${DRY_RUN:-0}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

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
