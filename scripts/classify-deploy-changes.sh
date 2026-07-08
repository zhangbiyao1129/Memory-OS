#!/usr/bin/env bash
set -euo pipefail

source_mode="${1:-git}"
if [[ "$source_mode" == "--stdin" ]]; then
  files=()
  while IFS= read -r file; do
    files+=("$file")
  done
else
  files=()
  while IFS= read -r file; do
    files+=("$file")
  done < <(git diff --name-only HEAD -- .)
fi

services=()
verify_mode="light"

add_service() {
  local service="$1"
  local existing
  for existing in "${services[@]:-}"; do
    [[ "$existing" == "$service" ]] && return 0
  done
  services+=("$service")
}

shell_quote() {
  local value="$1"
  printf "'%s'" "${value//\'/\'\\\'\'}"
}

full_stack() {
  services=(qdrant memory-api memory-worker memory-mcp memory-web)
  verify_mode="full"
}

for file in "${files[@]:-}"; do
  [[ -z "$file" ]] && continue
  case "$file" in
    migrations/*|deploy/*|scripts/load-prod-env.sh|scripts/preflight.sh)
      full_stack
      ;;
    cmd/memory-api/*)
      add_service memory-api
      [[ "$verify_mode" == "light" ]] && verify_mode="smoke"
      ;;
    cmd/memory-worker/*)
      add_service memory-worker
      [[ "$verify_mode" == "light" ]] && verify_mode="smoke"
      ;;
    cmd/memory-mcp/*|internal/mcp*|internal/mcp/*|internal/mcpstdio/*|internal/mcpproxy/*)
      add_service memory-mcp
      [[ "$verify_mode" == "light" ]] && verify_mode="smoke"
      ;;
    internal/retrieval/*|internal/rag/*|internal/qdrant/*|internal/archive/*|internal/candidatememory/*|internal/hotmemory/*|internal/http/*)
      add_service memory-api
      add_service memory-worker
      add_service memory-mcp
      verify_mode="full"
      ;;
    internal/*)
      add_service memory-api
      add_service memory-worker
      add_service memory-mcp
      [[ "$verify_mode" == "light" ]] && verify_mode="smoke"
      ;;
    frontend/*)
      add_service memory-web
      ;;
    Makefile)
      full_stack
      ;;
  esac
done

if [[ ${#services[@]} -eq 0 ]]; then
  services=(memory-api memory-worker memory-mcp memory-web)
  verify_mode="smoke"
fi

printf 'SERVICES=%s\n' "$(shell_quote "${services[*]}")"
printf 'VERIFY_MODE=%s\n' "$(shell_quote "$verify_mode")"
