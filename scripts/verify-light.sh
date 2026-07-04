#!/usr/bin/env bash
set -euo pipefail

API_BASE_URL="${API_BASE_URL:-http://127.0.0.1:18081}"
WEB_BASE_URL="${WEB_BASE_URL:-http://127.0.0.1:18080}"
MCP_BASE_URL="${MCP_BASE_URL:-http://127.0.0.1:18082}"

if [[ -f scripts/load-prod-env.sh ]]; then
  . scripts/load-prod-env.sh
fi

curl_json() {
  local name="$1"
  local url="$2"
  echo "==> $name"
  curl -fsS "$url" >/dev/null
}

curl_text() {
  local name="$1"
  local url="$2"
  echo "==> $name"
  curl -fsS "$url" >/dev/null
}

echo "==> compose-ps-light"
docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps memory-api memory-web memory-mcp >/dev/null

curl_json healthz "$API_BASE_URL/healthz"
curl_json version "$API_BASE_URL/version"
curl_json openapi "$API_BASE_URL/openapi.json"
curl_text setup-installer "$API_BASE_URL/memory/setup/install.sh"
curl_text web "$WEB_BASE_URL/"
curl_json mcp-health "$MCP_BASE_URL/healthz"

echo "verify light completed"
