#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

. scripts/load-prod-env.sh

umask 077
LOG_DIR="${LOG_DIR:-$(mktemp -d /tmp/memory-os-post-deploy.XXXXXX)}"
if [[ -L "$LOG_DIR" ]]; then
  echo "post deploy verify failed: LOG_DIR must not be a symlink"
  exit 1
fi
mkdir -p "$LOG_DIR"
chmod 700 "$LOG_DIR"
LOG_DIR_REPORTED=false

report_log_dir() {
  if [[ "$LOG_DIR_REPORTED" == "false" ]]; then
    echo "post deploy verify logs: $LOG_DIR"
    LOG_DIR_REPORTED=true
  fi
}

shell_quote() {
  local value="$1"
  printf "'%s'" "${value//\'/\'\\\'\'}"
}

API_BASE="${API_BASE:-http://127.0.0.1:18081}"
OPENAPI_SPEC_SOURCE="${OPENAPI_SPEC_SOURCE:-$API_BASE/openapi.json}"
COMPOSE_PS_CMD="${COMPOSE_PS_CMD:-docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps}"
VERSION_CMD="${VERSION_CMD:-curl -fsS "$API_BASE/version"}"
HEALTHZ_CMD="${HEALTHZ_CMD:-curl -fsS "$API_BASE/healthz"}"
OPENAPI_CMD="${OPENAPI_CMD:-curl -fsS "$API_BASE/openapi.json"}"
OPENAPI_VALIDATE_CMD="${OPENAPI_VALIDATE_CMD:-python3 scripts/validate-openapi-runtime.py $(shell_quote "$OPENAPI_SPEC_SOURCE")}"
SMOKE_CMD="${SMOKE_CMD:-make smoke}"
PIPELINE_E2E_CMD="${PIPELINE_E2E_CMD:-}"
VERIFY_MODE="${VERIFY_MODE:-full}"

case "$VERIFY_MODE" in
  light|smoke|full) ;;
  *)
    echo "unsupported VERIFY_MODE: $VERIFY_MODE" >&2
    exit 1
    ;;
esac

run_step() {
  local name="$1"
  local command_text="$2"
  printf '==> %s\n' "$name"
  if ! bash -lc "$command_text" >"$LOG_DIR/$name.log" 2>&1; then
    report_log_dir
    return 1
  fi
}

run_pipeline_e2e() {
  printf '==> pipeline-e2e\n'
  if [[ -n "$PIPELINE_E2E_CMD" ]]; then
    if ! bash -lc "$PIPELINE_E2E_CMD" >"$LOG_DIR/pipeline-e2e.log" 2>&1; then
      report_log_dir
      return 1
    fi
    return
  fi

  if ! (
    SMOKE_POSTGRES_DSN="$(docker exec deploy-memory-api-1 printenv POSTGRES_DSN)"
    export SMOKE_POSTGRES_DSN
    docker run --rm --network deploy_default \
      -e SMOKE_API_URL=http://memory-api:18081/healthz \
      -e SMOKE_QDRANT_URL=http://qdrant:6333 \
      -e SMOKE_WEB_URL=http://memory-web:18080 \
      -e SMOKE_MCP_URL=http://memory-mcp:18082 \
      -e SMOKE_TIMEOUT=3m \
      -e SMOKE_ENABLE_DEV_ENDPOINTS=false \
      -e SMOKE_ENABLE_PIPELINE_E2E=true \
      -e SMOKE_ENABLE_ADAPTER_FIXTURE_E2E=true \
      -e SMOKE_PIPELINE_E2E_TIMEOUT=90s \
      -e SMOKE_POSTGRES_DSN \
      -e GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
      -e NO_PROXY="${NO_PROXY:-localhost,127.0.0.1,postgres,redis,qdrant,memory-api,memory-web,memory-mcp,memory-llm-mock}" \
      -e no_proxy="${NO_PROXY:-localhost,127.0.0.1,postgres,redis,qdrant,memory-api,memory-web,memory-mcp,memory-llm-mock}" \
      -v "$REPO_ROOT:/src" \
      -w /src \
      golang:1.25-bookworm go run ./cmd/memory-smoke
  ) >"$LOG_DIR/pipeline-e2e.log" 2>&1; then
    report_log_dir
    return 1
  fi
}

run_step "compose-ps" "$COMPOSE_PS_CMD"
run_step "version" "$VERSION_CMD"
run_step "healthz" "$HEALTHZ_CMD"

if [[ "$VERIFY_MODE" == "light" ]]; then
  report_log_dir
  echo "post deploy verify completed"
  exit 0
fi

run_step "openapi" "$OPENAPI_CMD"
run_step "openapi-validate" "$OPENAPI_VALIDATE_CMD"
run_step "smoke" "$SMOKE_CMD"

if [[ "$VERIFY_MODE" == "full" ]]; then
  run_pipeline_e2e
fi

report_log_dir
echo "post deploy verify completed"
