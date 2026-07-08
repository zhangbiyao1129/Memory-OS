#!/usr/bin/env bash
set -euo pipefail

scripts=(
  scripts/classify-deploy-changes.sh
  scripts/deploy_classifier_test.sh
  scripts/measure-deploy-step.sh
  scripts/post-deploy-verify.sh
  scripts/verify-light.sh
  scripts/sync-t480.sh
)

echo "==> bash syntax"
bash -n "${scripts[@]}"

echo "==> deploy classifier"
scripts/deploy_classifier_test.sh

echo "==> make deploy targets"
grep -F "t480-deploy-dry-run:" Makefile >/dev/null
grep -F "t480-deploy-auto:" Makefile >/dev/null
grep -F '$(MAKE) deploy-safety-check' Makefile >/dev/null

if command -v shellcheck >/dev/null 2>&1; then
  echo "==> shellcheck"
  shellcheck "${scripts[@]}"
else
  echo "==> shellcheck skipped: command not found"
fi

echo "deploy safety check completed"
