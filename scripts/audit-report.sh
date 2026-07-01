#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

AUDIT_REPORT_PATH="${AUDIT_REPORT_PATH:-$REPO_ROOT/artifacts/completion-audit.md}"
RUN_REAL_VERIFY="${RUN_REAL_VERIFY:-0}"
VERIFY_CMD="${VERIFY_CMD:-make verify}"
BACKUP_CHECK_CMD="${BACKUP_CHECK_CMD:-DRY_RUN=1 RUN_ID=audit-report BACKUP_ROOT=/tmp/memory-os-audit-backup ARCHIVE_DIR=/tmp/memory-os-audit-archive make backup}"
mkdir -p "$(dirname "$AUDIT_REPORT_PATH")"

run_capture() {
  local command_text="$1"
  local output status
  set +e
  output="$(bash -lc "$command_text" 2>&1)"
  status=$?
  set -e
  printf '%s' "$status"
  printf '\n%s' "$output" > "$2"
}

verify_log="$(mktemp)"
backup_log="$(mktemp)"
trap 'rm -f "$verify_log" "$backup_log"' EXIT

if [[ "$RUN_REAL_VERIFY" == "1" ]]; then
  verify_status="$(run_capture "$VERIFY_CMD" "$verify_log")"
else
  verify_status=0
  printf 'verify skipped; set RUN_REAL_VERIFY=1 to execute %s\n' "$VERIFY_CMD" > "$verify_log"
fi
backup_status="$(run_capture "$BACKUP_CHECK_CMD" "$backup_log")"

status="pass"
if [[ "$verify_status" != "0" || "$backup_status" != "0" ]]; then
  status="fail"
fi

cat > "$AUDIT_REPORT_PATH" <<REPORT
# Memory OS Completion Audit Report

status: $status

## Scope

- Objective: Native Multi-Agent Memory Platform using Go/Hertz + PostgreSQL + Redis + Qdrant + Nuxt + Docker Compose.
- Spec: docs/memory-os-spec.md
- Checklist: docs/completion-audit-checklist.md
- Verification entrypoint: make verify

## Hard Invariants

- MVP 不依赖 mem0/FastGPT。
- PostgreSQL 是权威元数据源。
- Markdown 文件是 Archive 内容权威源。
- Qdrant 单 collection，查询必须使用 query-time payload filter。
- Secret 明文不得进入日志、Markdown、Qdrant、Hot Memory 或聊天回答。
- 不同用户默认隔离，agent_specific 默认不跨 Agent 召回。

## Verification Evidence

### make verify

Command:

\`\`\`bash
$VERIFY_CMD
\`\`\`

Exit status: $verify_status

Output:

\`\`\`text
$(cat "$verify_log")
\`\`\`

### backup evidence

Command:

\`\`\`bash
$BACKUP_CHECK_CMD
\`\`\`

Exit status: $backup_status

Output:

\`\`\`text
$(cat "$backup_log")
\`\`\`

## Required Next Gate

Before commit or deployment, run this report with real verification:

\`\`\`bash
RUN_REAL_VERIFY=1 make audit-report
\`\`\`
REPORT

if [[ "$status" != "pass" ]]; then
  echo "audit report written with failures: $AUDIT_REPORT_PATH" >&2
  exit 1
fi

echo "audit report written: $AUDIT_REPORT_PATH"
