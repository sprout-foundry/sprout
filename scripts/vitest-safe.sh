#!/usr/bin/env bash
#
# vitest-safe.sh — Safe wrapper for running vitest tests with OOM protection.
#
# Why: Vitest defaults to forking one worker per CPU core. Each jsdom worker
# holds 1–4 GB RSS. On a 24-core machine, the full test suite (~48 files)
# consumed ~52 GB and triggered kernel OOM. This wrapper enforces:
#   1. Explicit test file globs (no bare "vitest run" that runs everything)
#   2. Worker pool cap of 4 (VITEST_MAX_WORKERS=4)
#   3. Memory availability pre-check (≥8 GB required)
#
# Usage:
#   ./scripts/vitest-safe.sh run src/components/App.test.tsx
#   ./scripts/vitest-safe.sh run src/components/{A,B,C}.test.tsx
#   ./scripts/vitest-safe.sh run src/utils/format.test.ts
#
# Exit codes:
#   0 — vitest completed successfully
#   1 — validation error (no test file, insufficient memory)
#   2 — vitest test failures

set -euo pipefail

# ── Constants ────────────────────────────────────────────────────────────────

MIN_MEMORY_KB=$((8 * 1024 * 1024))  # 8 GB in kilobytes
WORKER_CAP=4

# ── Helpers ──────────────────────────────────────────────────────────────────

die() {
  echo "❌ vitest-safe: $*" >&2
  exit 1
}

info() {
  echo "ℹ️  vitest-safe: $*" >&2
}

# ── Validation: Require test file glob ──────────────────────────────────────

# We look for arguments that look like test file paths: containing ".test." or
# ".spec.".  This catches:
#   run src/App.test.tsx
#   run src/{A,B}.test.tsx
#   run "src/**/*.test.tsx"
# But rejects:
#   run
#   run src/components/Button   (directory, not a test file)
#   (no args)

has_test_file_glob() {
  local arg
  local has_list=0
  for arg in "$@"; do
    # Skip vitest subcommands (run, watch, related, etc.)
    case "$arg" in
      run|watch|related|help|--help|-h|--version|-v|--config|--passWithNoTests|--no-color|--reporter*|-- )
        continue
        ;;
      list)
        # 'list' is a read-only discovery subcommand; allow it without test file globs
        has_list=1
        continue
        ;;
    esac
    # Skip other flags
    case "$arg" in
      --*) continue ;;
    esac
    # Check if it looks like a test file path/glob
    if [[ "$arg" == *".test."* ]] || [[ "$arg" == *".spec."* ]]; then
      return 0
    fi
  done
  # Allow 'list' as a standalone read-only subcommand
  if [ "$has_list" -eq 1 ]; then
    return 0
  fi
  return 1
}

if ! has_test_file_glob "$@"; then
  die "No test file specified.

Running vitest without file globs runs ALL test files in parallel, which
with jsdom consumes massive memory (1-4 GB per worker) and can cause OOM
on machines with many CPU cores.

Usage:
  $0 run src/path/to/Specific.test.tsx
  $0 run src/components/{A,B,C}.test.tsx
  $0 run \"src/**/*.test.tsx\"   (scoped glob)

See automate/workflow_prompt.md for details."
fi

# ── Validation: Memory availability ──────────────────────────────────────────

# Support MEM_AVAILABLE_KB override for testing/ci environments without /proc/meminfo
if [ -n "${MEM_AVAILABLE_KB:-}" ]; then
  AVAILABLE_KB="$MEM_AVAILABLE_KB"
elif [ -f /proc/meminfo ]; then
  AVAILABLE_KB=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)
  if [ -z "$AVAILABLE_KB" ]; then
    die "Could not read MemAvailable from /proc/meminfo"
  fi
else
  # macOS / other platforms: skip the check with a warning
  info "Cannot check memory availability (no /proc/meminfo); skipping memory check"
  AVAILABLE_KB=""
fi

if [ -n "$AVAILABLE_KB" ] && [ "$AVAILABLE_KB" -lt "$MIN_MEMORY_KB" ]; then
  AVAILABLE_GB=$(awk "BEGIN {printf \"%.1f\", $AVAILABLE_KB / 1024 / 1024}")
  die "Insufficient memory to run vitest.

Available: ${AVAILABLE_GB} GB
Required:  $((MIN_MEMORY_KB / 1024 / 1024)) GB

Vitest workers with jsdom consume 1-4 GB each. Close some applications or
run fewer tests in parallel."
fi

# ── Set worker cap ───────────────────────────────────────────────────────────

if [ -z "${VITEST_MAX_WORKERS:-}" ]; then
  export VITEST_MAX_WORKERS="$WORKER_CAP"
  info "Set VITEST_MAX_WORKERS=$WORKER_CAP (was not set)"
else
  info "VITEST_MAX_WORKERS already set to ${VITEST_MAX_WORKERS}"
fi

# ── Run vitest ───────────────────────────────────────────────────────────────

# Determine repo root relative to this script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

info "Running: npx vitest $*"
cd "$REPO_ROOT/packages/ui"
npx vitest "$@"
vitest_rc=$?
if [ "$vitest_rc" -ne 0 ]; then
  exit 2
fi
