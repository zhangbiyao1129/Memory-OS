#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCAN_ROOT="${SCAN_ROOT:-$REPO_ROOT}"

if [[ ! -d "$SCAN_ROOT" ]]; then
  echo "SCAN_ROOT does not exist: $SCAN_ROOT" >&2
  exit 1
fi

should_skip_path() {
  local path="$1"
  case "$path" in
    */.git/*|*/node_modules/*|*/.nuxt/*|*/.output/*|*/dist/*|*/backups/*|*/artifacts/*|*/package-lock.json|*.sum) return 0 ;;
  esac
  return 1
}

allow_line() {
  local line="$1"
  for allowed in     "replace-me"     "example.local"     "fake-secret-value"     "sk-test-redacted-example"     "password-test-redacted"     "fake-test-redacted"     "-----BEGIN PRIVATE KEY-----"; do
    if [[ "$line" == *"$allowed"* ]]; then
      return 0
    fi
  done
  return 1
}
is_secret_line() {
  local line="$1"
  [[ "$line" =~ sk-[A-Za-z0-9_-]{16,} ]] && return 0
  [[ "$line" =~ pk_[A-Za-z0-9]{24,} ]] && return 0
  [[ "$line" =~ BEGIN[[:space:]]+(RSA[[:space:]]+|OPENSSH[[:space:]]+|EC[[:space:]]+|DSA[[:space:]]+)?PRIVATE[[:space:]]+KEY ]] && return 0
  [[ "$line" =~ AKIA[0-9A-Z]{16} ]] && return 0
  [[ "$line" =~ xox[baprs]-[A-Za-z0-9-]{20,} ]] && return 0
  return 1
}

findings=0
while IFS= read -r -d '' file; do
  should_skip_path "$file" && continue
  if ! grep -Iq . "$file" 2>/dev/null; then
    continue
  fi
  line_no=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    line_no=$((line_no + 1))
    if allow_line "$line"; then
      continue
    fi
    if is_secret_line "$line"; then
      echo "potential secret: $file:$line_no" >&2
      findings=$((findings + 1))
    fi
  done < "$file"
done < <(find "$SCAN_ROOT" -type f -print0)

if (( findings > 0 )); then
  echo "secret scan failed: $findings potential secret(s) found" >&2
  exit 1
fi

echo "secret scan ok"
