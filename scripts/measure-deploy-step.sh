#!/usr/bin/env bash
set -euo pipefail

name="${1:?step name is required}"
shift

log_dir="${DEPLOY_TIMING_DIR:-artifacts/deploy-timing-$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$log_dir"

start="$(date +%s)"
"$@"
end="$(date +%s)"
duration="$((end - start))"

printf '%s=%ss\n' "$name" "$duration" | tee -a "$log_dir/timing.env"
