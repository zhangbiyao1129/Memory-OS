#!/usr/bin/env bash

# Source this file from deployment entrypoints to populate non-secret BUILD_* metadata.
# It supports either a git worktree or a synced .build-info.env file.

memory_os_load_build_info_file() {
  local build_info_file="${MEMORY_OS_BUILD_INFO_FILE:-}"
  local had_build_version="${BUILD_VERSION+x}"
  local had_build_commit="${BUILD_COMMIT+x}"
  local had_build_time="${BUILD_TIME+x}"
  local had_build_dirty="${BUILD_DIRTY+x}"
  local original_build_version="${BUILD_VERSION-}"
  local original_build_commit="${BUILD_COMMIT-}"
  local original_build_time="${BUILD_TIME-}"
  local original_build_dirty="${BUILD_DIRTY-}"
  if [[ -z "$build_info_file" ]]; then
    for candidate in ".build-info.env" ".build/build-info.env"; do
      if [[ -f "$candidate" ]]; then
        build_info_file="$candidate"
        break
      fi
    done
  fi
  if [[ -z "$build_info_file" ]]; then
    return 1
  fi
  if [[ -L "$build_info_file" ]]; then
    echo "build info load failed: metadata file must not be a symlink" >&2
    return 2
  fi
  if [[ ! -r "$build_info_file" ]]; then
    echo "build info load failed: metadata file is not readable" >&2
    return 2
  fi
  set -a
  # shellcheck disable=SC1090
  . "$build_info_file"
  set +a
  if [[ "$had_build_version" == "x" ]]; then
    export BUILD_VERSION="$original_build_version"
  fi
  if [[ "$had_build_commit" == "x" ]]; then
    export BUILD_COMMIT="$original_build_commit"
  fi
  if [[ "$had_build_time" == "x" ]]; then
    export BUILD_TIME="$original_build_time"
  fi
  if [[ "$had_build_dirty" == "x" ]]; then
    export BUILD_DIRTY="$original_build_dirty"
  fi
  return 0
}

memory_os_load_build_info_from_git() {
  command -v git >/dev/null 2>&1 || return 1
  git rev-parse --is-inside-work-tree >/dev/null 2>&1 || return 1

  if [[ -z "${BUILD_COMMIT:-}" ]]; then
    BUILD_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || true)"
    export BUILD_COMMIT
  fi
  if [[ -z "${BUILD_DIRTY:-}" ]]; then
    if [[ -z "$(git status --porcelain 2>/dev/null)" ]]; then
      BUILD_DIRTY=false
    else
      BUILD_DIRTY=true
    fi
    export BUILD_DIRTY
  fi
  return 0
}

memory_os_default_build_info() {
  export BUILD_VERSION="${BUILD_VERSION:-0.4.0-dev}"
  export BUILD_COMMIT="${BUILD_COMMIT:-unknown}"
  export BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
  export BUILD_DIRTY="${BUILD_DIRTY:-unknown}"
}

memory_os_load_build_info_file || memory_os_load_build_info_from_git || true
memory_os_default_build_info
