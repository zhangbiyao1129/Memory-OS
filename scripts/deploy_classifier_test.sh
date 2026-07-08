#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLASSIFIER="$ROOT/scripts/classify-deploy-changes.sh"

assert_case() {
  local name="$1"
  local files="$2"
  local want_services="$3"
  local want_verify="$4"
  local output
  output="$(printf '%s\n' "$files" | "$CLASSIFIER" --stdin)"
  unset SERVICES VERIFY_MODE
  eval "$output"
  [[ "${SERVICES:-}" == "$want_services" ]] || {
    echo "$name: SERVICES mismatch"
    echo "$output"
    exit 1
  }
  [[ "${VERIFY_MODE:-}" == "$want_verify" ]] || {
    echo "$name: VERIFY_MODE mismatch"
    echo "$output"
    exit 1
  }
}

assert_case "frontend only" "frontend/pages/hot-memory/index.vue" "memory-web" "light"
assert_case "api command only" "cmd/memory-api/main.go" "memory-api" "smoke"
assert_case "shared backend" "internal/retrieval/service.go" "memory-api memory-worker memory-mcp" "full"
assert_case "migration" "migrations/000026_candidate_maintenance_scope_lock.sql" "qdrant memory-api memory-worker memory-mcp memory-web" "full"
assert_case "compose" "deploy/docker-compose.yml" "qdrant memory-api memory-worker memory-mcp memory-web" "full"
assert_case "empty input" "" "memory-api memory-worker memory-mcp memory-web" "smoke"

echo "deploy classifier tests passed"
