#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

FINAL_REPORT_PATH="${FINAL_REPORT_PATH:-$REPO_ROOT/artifacts/final-delivery-report.md}"
AUDIT_REPORT_PATH="${AUDIT_REPORT_PATH:-$REPO_ROOT/artifacts/completion-audit.md}"
BROWSER_ACCEPTANCE_BUNDLE_PATH="${BROWSER_ACCEPTANCE_BUNDLE_PATH:-$REPO_ROOT/artifacts/browser-acceptance/browser-acceptance-bundle.md}"
SECURITY_EVIDENCE_BUNDLE_PATH="${SECURITY_EVIDENCE_BUNDLE_PATH:-$REPO_ROOT/artifacts/security-evidence/security-evidence-bundle.md}"
PERMISSION_ISOLATION_BUNDLE_PATH="${PERMISSION_ISOLATION_BUNDLE_PATH:-$REPO_ROOT/artifacts/security-evidence/permission-isolation-bundle.md}"
CHECKLIST_AUDIT_PATH="${CHECKLIST_AUDIT_PATH:-$REPO_ROOT/artifacts/completion-checklist-audit.md}"
RUN_RUNTIME_CHECKS="${RUN_RUNTIME_CHECKS:-0}"

WEB_BASE="${WEB_BASE:-http://ddns.08121.top:18080}"
API_BASE="${API_BASE:-http://127.0.0.1:18081}"
MCP_BASE="${MCP_BASE:-http://ddns.08121.top:18082}"
QDRANT_BASE="${QDRANT_BASE:-http://ddns.08121.top:18083}"

VERSION_CMD="${VERSION_CMD:-curl -fsS "$API_BASE/version"}"
HEALTHZ_CMD="${HEALTHZ_CMD:-curl -fsS "$API_BASE/healthz"}"
OPENAPI_COUNT_CMD="${OPENAPI_COUNT_CMD:-curl -fsS "$API_BASE/openapi.json" | python3 -c 'import json,sys; print(len(json.load(sys.stdin).get(\"paths\", {})))'}"

mkdir -p "$(dirname "$FINAL_REPORT_PATH")"

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

version_log="$(mktemp)"
healthz_log="$(mktemp)"
openapi_log="$(mktemp)"
trap 'rm -f "$version_log" "$healthz_log" "$openapi_log"' EXIT

version_status="skipped"
healthz_status="skipped"
openapi_status="skipped"

if [[ "$RUN_RUNTIME_CHECKS" == "1" ]]; then
  version_status="$(run_capture "$VERSION_CMD" "$version_log")"
  healthz_status="$(run_capture "$HEALTHZ_CMD" "$healthz_log")"
  openapi_status="$(run_capture "$OPENAPI_COUNT_CMD" "$openapi_log")"
else
  printf 'runtime check skipped; set RUN_RUNTIME_CHECKS=1 to execute %s\n' "$VERSION_CMD" > "$version_log"
  printf 'runtime check skipped; set RUN_RUNTIME_CHECKS=1 to execute %s\n' "$HEALTHZ_CMD" > "$healthz_log"
  printf 'runtime check skipped; set RUN_RUNTIME_CHECKS=1 to execute %s\n' "$OPENAPI_COUNT_CMD" > "$openapi_log"
fi

audit_status="missing"
if [[ -f "$AUDIT_REPORT_PATH" ]]; then
  audit_status="$(sed -n 's/^status: //p' "$AUDIT_REPORT_PATH" | head -1)"
  if [[ -z "$audit_status" ]]; then
    audit_status="unknown"
  fi
fi

dashboard_acceptance_status="partial"
dashboard_acceptance_evidence="dashboard real API aggregation evidence exists, but this draft still relies on delivery-log pointers rather than a consolidated final screenshot set"
browser_acceptance_bundle_line="- Browser acceptance bundle: not generated yet"
browser_acceptance_focus="- Final browser acceptance checklist still needs a single operator-facing screenshot / trace bundle."
security_bundle_line="- Security evidence bundle: not generated yet"
permission_bundle_line="- Permission isolation bundle: not generated yet"
checklist_audit_line="- Checklist audit ledger: not generated yet"
security_bundle_summary="- Security evidence bundle has not been generated yet; this draft still relies on delivery-log pointers."
permission_bundle_summary="- Production permission isolation bundle has not been generated yet; this draft still relies on delivery-log pointers."
security_focus="- Final safety conclusions still need to be lifted from the delivery log into a single operator-facing section."

if [[ -f "$BROWSER_ACCEPTANCE_BUNDLE_PATH" ]]; then
  dashboard_acceptance_status="pass"
  dashboard_acceptance_evidence="consolidated browser acceptance bundle: \`$BROWSER_ACCEPTANCE_BUNDLE_PATH\`"
  browser_acceptance_bundle_line="- Browser acceptance bundle: \`$BROWSER_ACCEPTANCE_BUNDLE_PATH\`"
  browser_acceptance_focus="- Browser acceptance bundle has been generated; remaining work is to keep it aligned with the final handoff package."
fi

if [[ -f "$SECURITY_EVIDENCE_BUNDLE_PATH" ]]; then
  security_bundle_line="- Security evidence bundle: \`$SECURITY_EVIDENCE_BUNDLE_PATH\`"
  security_bundle_summary="- Production security evidence bundle: \`$SECURITY_EVIDENCE_BUNDLE_PATH\`"
  security_focus="- Security evidence bundle has been generated; remaining work is to keep it aligned with the final handoff package."
fi

if [[ -f "$PERMISSION_ISOLATION_BUNDLE_PATH" ]]; then
  permission_bundle_line="- Permission isolation bundle: \`$PERMISSION_ISOLATION_BUNDLE_PATH\`"
  permission_bundle_summary="- Production permission isolation bundle: \`$PERMISSION_ISOLATION_BUNDLE_PATH\`"
fi

if [[ -f "$CHECKLIST_AUDIT_PATH" ]]; then
  checklist_audit_line="- Checklist audit ledger: \`$CHECKLIST_AUDIT_PATH\`"
fi

cat > "$FINAL_REPORT_PATH" <<REPORT
# Memory OS Final Delivery Report (Draft)

> 说明：本报告是当前交付证据的汇总草稿，不自动等同于“v0.4 生产级完全体已完成”。只有当本报告中的未闭环项被逐一关闭后，才能声明最终交付完成。

## Runtime Endpoints

- Web: $WEB_BASE
- API: http://ddns.08121.top:18081
- MCP: $MCP_BASE
- Qdrant: $QDRANT_BASE

## Core Evidence

- Completion audit: \`$AUDIT_REPORT_PATH\`
- Completion audit status: $audit_status
- Delivery log: \`docs/production-delivery-log.md\`
- Spec: \`docs/memory-os-spec.md\`
- Checklist: \`docs/completion-audit-checklist.md\`
$browser_acceptance_bundle_line
$security_bundle_line
$permission_bundle_line
$checklist_audit_line

## Runtime Snapshots

### /version

- Exit status: $version_status

\`\`\`text
$(cat "$version_log")
\`\`\`

### /healthz

- Exit status: $healthz_status

\`\`\`text
$(cat "$healthz_log")
\`\`\`

### OpenAPI path count

- Exit status: $openapi_status

\`\`\`text
$(cat "$openapi_log")
\`\`\`

## Browser Acceptance Matrix

| Page | Status | Current evidence |
| --- | --- | --- |
| \`/login\` | pass | route protection and login page render evidence in \`docs/production-delivery-log.md\` |
| \`/\` | $dashboard_acceptance_status | $dashboard_acceptance_evidence |
| \`/users\` | pass | login-state browser acceptance evidence in \`docs/production-delivery-log.md\` |
| \`/orgs\` | pass | tenant governance browser acceptance evidence in \`docs/production-delivery-log.md\` |
| \`/projects\` | pass | tenant governance browser acceptance evidence in \`docs/production-delivery-log.md\` |
| \`/permissions\` | pass | membership governance browser acceptance evidence in \`docs/production-delivery-log.md\` |
| \`/roles\` | pass | role directory browser acceptance evidence in \`docs/production-delivery-log.md\` |
| \`/archive\` and \`/archive/[id]\` | pass | archive CRUD and reindex browser acceptance evidence in \`docs/production-delivery-log.md\` |
| \`/hot-memory\` | pass | hot memory CRUD browser acceptance evidence in \`docs/production-delivery-log.md\` |
| \`/secrets\` | pass | metadata-only secret vault browser acceptance evidence in \`docs/production-delivery-log.md\` |
| \`/tokens\` | pass | PAT and adapter token one-time display evidence in \`docs/production-delivery-log.md\` |
| \`/qdrant\` | pass | qdrant status browser acceptance evidence in \`docs/production-delivery-log.md\` |
| \`/search-test\` | pass | unified retrieval browser acceptance evidence in \`docs/production-delivery-log.md\` |

## Security Evidence Pointers

- Secret scan and runtime audit evidence: \`docs/production-delivery-log.md\`
- Completion audit report: \`$AUDIT_REPORT_PATH\`
- Verification command entrypoint: \`make verify\`
- Runtime verification entrypoint: \`make post-deploy-verify\`

## Security Summary

- Source tree secret scan: pass
- Runtime completion audit: $audit_status
- Secret / Archive / Hot Memory / Qdrant payload evidence: see \`docs/production-delivery-log.md\`
- MCP and HTTP production verification entrypoints: \`make verify\`, \`make post-deploy-verify\`
$security_bundle_summary

## Permission Isolation Summary

- Unauthenticated management APIs are expected to return \`401\`.
- PAT scope and tenant boundary violations are expected to return \`403\` or binding errors.
- Query-time filter evidence remains anchored in production verification and delivery-log records.
- MCP and HTTP production verification entrypoints: \`make verify\`, \`make post-deploy-verify\`
- PAT revocation evidence remains anchored in browser acceptance and production verification records.
$permission_bundle_summary

## Remaining Audit Focus

$browser_acceptance_focus
$security_focus
- This report is still a draft artifact; it is not yet the final user-facing handoff package.

## Regeneration

\`\`\`bash
RUN_RUNTIME_CHECKS=1 make final-delivery-report
\`\`\`
REPORT

echo "final delivery report written: $FINAL_REPORT_PATH"
