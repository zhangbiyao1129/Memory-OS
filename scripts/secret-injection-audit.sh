#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

SECRET_INJECTION_AUDIT_REPORT_PATH="${SECRET_INJECTION_AUDIT_REPORT_PATH:-$REPO_ROOT/artifacts/security-evidence/secret-injection-audit.md}"
mkdir -p "$(dirname "$SECRET_INJECTION_AUDIT_REPORT_PATH")"

memory_os_container_env() {
  local container="$1"
  local key="$2"
  docker inspect "$container" --format "{{range .Config.Env}}{{println .}}{{end}}" 2>/dev/null | sed -n "s/^${key}=//p" | head -1
}

memory_os_container_network() {
  docker inspect deploy-memory-api-1 --format '{{range $key, $_ := .NetworkSettings.Networks}}{{println $key}}{{end}}' 2>/dev/null | head -1
}

memory_os_container_ip() {
  local container="$1"
  docker inspect "$container" --format '{{range .NetworkSettings.Networks}}{{println .IPAddress}}{{end}}' 2>/dev/null | head -1
}

memory_os_mount_source() {
  local container="$1"
  local destination="$2"
  docker inspect "$container" --format "{{range .Mounts}}{{if eq .Destination \"$destination\"}}{{println .Source}}{{end}}{{end}}" 2>/dev/null | head -1
}

build_default_runtime_secret_injection_cmd() {
  local dsn="${POSTGRES_DSN:-}"
  local archive_dir="${ARCHIVE_DIR:-$REPO_ROOT/data/archive}"
  local qdrant_url="${QDRANT_URL:-http://127.0.0.1:6333}"

  if [[ -n "$dsn" ]]; then
    printf 'go run ./cmd/memory-secret-audit runtime --dsn %q --archive-dir %q --qdrant-url %q' "$dsn" "$archive_dir" "$qdrant_url"
    return 0
  fi

  if ! command -v docker >/dev/null 2>&1 || ! docker inspect deploy-memory-api-1 >/dev/null 2>&1; then
    printf 'go run ./cmd/memory-secret-audit runtime --dsn %q --archive-dir %q --qdrant-url %q' "$dsn" "$archive_dir" "$qdrant_url"
    return 0
  fi

  dsn="$(memory_os_container_env deploy-memory-api-1 POSTGRES_DSN)"
  archive_dir="$(memory_os_mount_source deploy-memory-api-1 /data/memory-os)"
  qdrant_url="http://127.0.0.1:18083"
  local postgres_ip
  postgres_ip="$(memory_os_container_ip deploy-postgres-1)"
  local network
  network="$(memory_os_container_network)"
  if [[ -n "$dsn" && -n "$postgres_ip" ]]; then
    dsn="${dsn/@postgres:/"@$postgres_ip:"}"
  fi
  if [[ -z "$dsn" || -z "$archive_dir" || -z "$qdrant_url" ]]; then
    printf 'go run ./cmd/memory-secret-audit runtime --dsn %q --archive-dir %q --qdrant-url %q' "$dsn" "$archive_dir" "$qdrant_url"
    return 0
  fi

  printf 'PATH=/usr/local/go/bin:$PATH go run ./cmd/memory-secret-audit runtime --dsn %q --archive-dir %q --qdrant-url %q' "$dsn" "$archive_dir" "$qdrant_url"
}

RUNTIME_SECRET_INJECTION_CMD="${RUNTIME_SECRET_INJECTION_CMD:-$(build_default_runtime_secret_injection_cmd)}"

json_output_file="$(mktemp)"
trap 'rm -f "$json_output_file"' EXIT

set +e
bash -lc "$RUNTIME_SECRET_INJECTION_CMD" >"$json_output_file" 2>&1
command_status=$?
set -e

python3 - "$json_output_file" "$SECRET_INJECTION_AUDIT_REPORT_PATH" "$RUNTIME_SECRET_INJECTION_CMD" "$command_status" <<'PY'
import json
import pathlib
import sys

json_path = pathlib.Path(sys.argv[1])
report_path = pathlib.Path(sys.argv[2])
command_text = sys.argv[3]
command_status = int(sys.argv[4])
raw = json_path.read_text()

status = "fail"
result = {"notes": ["command did not return valid JSON"], "raw_output": raw}
if command_status == 0:
    try:
        result = json.loads(raw)
        status = str(result.get("status", "fail"))
    except json.JSONDecodeError:
        result = {"notes": ["command did not return valid JSON"], "raw_output": raw}

runtime_leak_counts = result.get("runtime_leak_counts", {})
cleanup = result.get("cleanup", {})
notes = result.get("notes", [])

def line(name, value):
    return f"- `{name}`: {value}"

report = "\n".join([
    "# Secret Injection Audit Report",
    "",
    f"status: {status}",
    "",
    "## Scope",
    "",
    "- Evidence target: `secret.inject` 审计日志与运行态明文泄露检查。",
    "- Safety rule: Secret 明文不得进入日志、Markdown、Qdrant、Hot Memory 或审计元数据。",
    "",
    "## Command",
    "",
    "```bash",
    command_text,
    "```",
    "",
    f"Exit status: {command_status}",
    "",
    "## Result",
    "",
    line("request_id", result.get("request_id", "")),
    line("secret_ref", result.get("secret_ref", "")),
    line("audit_log_count", result.get("audit_log_count", 0)),
    "",
    "## Runtime Leak Counts",
    "",
    line("audit_metadata_hits", runtime_leak_counts.get("audit_metadata_hits", 0)),
    line("archive_markdown_hits", runtime_leak_counts.get("archive_markdown_hits", 0)),
    line("archive_chunk_hits", runtime_leak_counts.get("archive_chunk_hits", 0)),
    line("hot_memory_hits", runtime_leak_counts.get("hot_memory_hits", 0)),
    line("archive_qdrant_payload_hits", runtime_leak_counts.get("archive_qdrant_payload_hits", 0)),
    line("hot_memory_qdrant_payload_hits", runtime_leak_counts.get("hot_memory_qdrant_payload_hits", 0)),
    line("qdrant_live_payload_hits", runtime_leak_counts.get("qdrant_live_payload_hits", 0)),
    "",
    "## Cleanup",
    "",
    line("secret_disabled", str(cleanup.get("secret_disabled", False)).lower()),
    line("project_deleted", str(cleanup.get("project_deleted", False)).lower()),
    line("org_deleted", str(cleanup.get("org_deleted", False)).lower()),
    line("user_disabled", str(cleanup.get("user_disabled", False)).lower()),
    "",
    "## Notes",
    "",
])

if notes:
    report += "\n".join(f"- {note}" for note in notes)
else:
    report += "- none"

if "raw_output" in result:
    report += "\n\n## Raw Output\n\n```text\n" + result["raw_output"] + "\n```"

report_path.write_text(report + "\n")
PY

if [[ "$command_status" != "0" ]]; then
  echo "secret injection audit report written with command failure: $SECRET_INJECTION_AUDIT_REPORT_PATH" >&2
  exit 1
fi

report_status="$(python3 - "$SECRET_INJECTION_AUDIT_REPORT_PATH" <<'PY'
import pathlib
import sys
report = pathlib.Path(sys.argv[1]).read_text()
print("pass" if "status: pass" in report else "fail")
PY
)"

if [[ "$report_status" != "pass" ]]; then
  echo "secret injection audit report written with non-pass status: $SECRET_INJECTION_AUDIT_REPORT_PATH" >&2
  exit 1
fi

echo "secret injection audit report written: $SECRET_INJECTION_AUDIT_REPORT_PATH"
