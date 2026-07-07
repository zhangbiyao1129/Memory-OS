#!/usr/bin/env bash
set -euo pipefail

PORTS="${PORTS:-18080 18081 18082 18083}"
MIN_DISK_KB="${MIN_DISK_KB:-41943040}"
if [[ -n "${SS_CMD:-}" ]]; then
  SS_CMD="$SS_CMD"
elif command -v ss >/dev/null 2>&1; then
  SS_CMD="ss -tlnp"
elif command -v lsof >/dev/null 2>&1; then
  SS_CMD="lsof -nP -iTCP -sTCP:LISTEN"
else
  echo "ss or lsof is required for port checks" >&2
  exit 1
fi
DF_CMD="${DF_CMD:-df -Pk .}"
DOCKER_CMD="${DOCKER_CMD:-docker --version}"
COMPOSE_CMD="${COMPOSE_CMD:-docker-compose --version}"
COMPOSE_PS_CMD="${COMPOSE_PS_CMD:-docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps}"
ALLOW_EXISTING_DEPLOYMENT="${ALLOW_EXISTING_DEPLOYMENT:-0}"

run_text() {
  bash -lc "$1"
}

load_compose_env_for_ps() {
  if [[ -n "${COMPOSE_PS_CMD:-}" && "$COMPOSE_PS_CMD" != "docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps" ]]; then
    return 0
  fi
  if [[ ! -f "scripts/load-prod-env.sh" ]]; then
    return 0
  fi
  # 只在需要识别当前部署占用端口时懒加载生产环境，避免普通 preflight 对 prod env 形成硬依赖。
  # shellcheck disable=SC1091
  . scripts/load-prod-env.sh >/dev/null 2>&1 || true
}

if ! run_text "$DOCKER_CMD" >/dev/null; then
  echo "docker is required" >&2
  exit 1
fi
if ! run_text "$COMPOSE_CMD" >/dev/null; then
  echo "docker-compose is required" >&2
  exit 1
fi

listening="$(run_text "$SS_CMD" || true)"
compose_ps=""
for port in $PORTS; do
  echo "checking port $port"
  if printf '%s\n' "$listening" | grep -Eq "[:.]$port([[:space:]]|$)"; then
    if [[ "$ALLOW_EXISTING_DEPLOYMENT" == "1" ]]; then
      if [[ -z "$compose_ps" ]]; then
        load_compose_env_for_ps
        compose_ps="$(run_text "$COMPOSE_PS_CMD" || true)"
      fi
      if printf '%s\n' "$compose_ps" | grep -q ":$port->"; then
        echo "port $port is already used by current deployment"
        continue
      fi
    fi
    echo "port $port is already in use" >&2
    exit 1
  fi
done

df_output="$(run_text "$DF_CMD")"
available_kb="$(printf '%s\n' "$df_output" | awk 'NR==2 {print $4}')"
if [[ -z "$available_kb" || ! "$available_kb" =~ ^[0-9]+$ ]]; then
  echo "could not determine available disk from df output" >&2
  exit 1
fi
if (( available_kb < MIN_DISK_KB )); then
  echo "available disk ${available_kb}KB is below required ${MIN_DISK_KB}KB" >&2
  exit 1
fi

# placeholder secret 检查：生产环境不允许占位值
check_placeholder_secrets() {
  local app_env="${APP_ENV:-}"
  if [[ "$app_env" != "production" ]]; then
    return 0
  fi
  local placeholder_patterns='replace-me|example|dev-only|mock'
  local secret_vars=("POSTGRES_PASSWORD" "LLM_API_KEY")
  for var in "${secret_vars[@]}"; do
    local val="${!var:-}"
    # 使用 :- 兜底，兼容脚本里的 set -u。
    if [[ -z "$val" ]]; then
      echo "production secret check failed: $var is empty" >&2
      exit 1
    fi
    if [[ "$val" =~ ^($placeholder_patterns) ]]; then
      echo "production secret check failed: $var has placeholder value" >&2
      exit 1
    fi
  done
}
check_placeholder_secrets

echo "preflight ok: ports=[$PORTS] available_disk_kb=$available_kb"
