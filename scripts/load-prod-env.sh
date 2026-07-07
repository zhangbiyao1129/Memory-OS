#!/usr/bin/env bash

# Source this file from Makefile targets or verification scripts.
# It loads production-only compose variables without printing secret values.

memory_os_load_env_file() {
  local env_file="${MEMORY_OS_ENV_FILE:-}"
  if [[ -z "$env_file" ]]; then
    for candidate in ".env.production" ".env"; do
      if [[ -f "$candidate" ]]; then
        env_file="$candidate"
        break
      fi
    done
  fi
  if [[ -z "$env_file" ]]; then
    return 1
  fi
  if [[ -L "$env_file" ]]; then
    echo "production env load failed: env file must not be a symlink" >&2
    return 2
  fi
  if [[ ! -r "$env_file" ]]; then
    echo "production env load failed: env file is not readable" >&2
    return 2
  fi
  set -a
  # shellcheck disable=SC1090
  . "$env_file"
  set +a
  return 0
}

memory_os_container_env() {
  local container="$1"
  local key="$2"
  docker inspect "$container" --format "{{range .Config.Env}}{{println .}}{{end}}" 2>/dev/null | sed -n "s/^${key}=//p" | head -1
}

memory_os_load_env_from_running_containers() {
  command -v docker >/dev/null 2>&1 || return 1
  docker inspect deploy-memory-api-1 >/dev/null 2>&1 || return 1
  docker inspect deploy-postgres-1 >/dev/null 2>&1 || return 1

  export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-$(memory_os_container_env deploy-postgres-1 POSTGRES_PASSWORD)}"
  export LLM_BASE_URL="${LLM_BASE_URL:-$(memory_os_container_env deploy-memory-api-1 LLM_BASE_URL)}"
  export LLM_API_KEY="${LLM_API_KEY:-$(memory_os_container_env deploy-memory-api-1 LLM_API_KEY)}"
}

memory_os_require_prod_env() {
  local missing=()
  for key in POSTGRES_PASSWORD LLM_BASE_URL LLM_API_KEY; do
    if [[ -z "${!key:-}" ]]; then
      missing+=("$key")
    fi
  done
  if (( ${#missing[@]} > 0 )); then
    echo "production env load failed: missing ${missing[*]}; set MEMORY_OS_ENV_FILE or export variables before running production compose commands" >&2
    return 1
  fi
}

memory_os_load_env_file || memory_os_load_env_from_running_containers || true
memory_os_require_prod_env
