#!/usr/bin/env bash
#
# vitest-safe_test.sh — Tests for scripts/vitest-safe.sh
#
# Run: bash scripts/vitest-safe_test.sh
#
# Each test runs the wrapper in a subshell with controlled environment so we
# never actually invoke vitest.  A fake `npx` binary is prepended to PATH.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WRAPPER="$SCRIPT_DIR/vitest-safe.sh"

PASS=0
FAIL=0
TOTAL=0

# ── Helpers ──────────────────────────────────────────────────────────────────

# run_test [expected_exit] "env_var=val ..." -- "script_arg1" "script_arg2" ...
#   Exports the env vars, prepends a fake npx to PATH, runs the wrapper,
#   and checks the exit code.  Captures stderr in $LAST_STDERR.
run_test() {
  local expected_exit="$1"; shift

  # Collect env vars until "--", then collect script args
  local env_str=""
  local args=()
  local past_sep=0
  for arg in "$@"; do
    if [ "$arg" = "--" ]; then
      past_sep=1
      continue
    fi
    if [ "$past_sep" -eq 0 ]; then
      env_str="$env_str $arg"
    else
      args+=("$arg")
    fi
  done

  # Create a temporary directory with a fake `npx`
  local mock_dir
  mock_dir="$(mktemp -d)"
  cat > "$mock_dir/npx" <<'FAKE_NPX'
#!/usr/bin/env bash
echo "[npx-mock] vitest $@" >&2
exit 0
FAKE_NPX
  chmod +x "$mock_dir/npx"

  # Run the wrapper; capture exit code and stderr.
  # We use a subshell with explicit exports so PATH quoting is safe.
  LAST_EXIT=0
  LAST_STDERR="$(
    set +e
    # Export env vars from env_str
    for ev in $env_str; do
      export "$ev"
    done
    export PATH="$mock_dir:$PATH"
    bash "$WRAPPER" "${args[@]+"${args[@]}"}" 2>&1
    echo "__EXIT__=$?"
  )"
  # Extract the exit code from the sentinel line
  LAST_EXIT=$(echo "$LAST_STDERR" | grep '__EXIT__=' | tail -1 | cut -d= -f2)
  # Remove the sentinel line from stderr
  LAST_STDERR=$(echo "$LAST_STDERR" | grep -v '__EXIT__=')
  # Default exit to 0 if we couldn't parse it
  LAST_EXIT=${LAST_EXIT:-0}

  rm -rf "$mock_dir"

  TOTAL=$((TOTAL + 1))
  if [ "$LAST_EXIT" -eq "$expected_exit" ]; then
    PASS=$((PASS + 1))
    echo "  ✓ PASS"
  else
    FAIL=$((FAIL + 1))
    echo "  ✗ FAIL: expected exit $expected_exit, got $LAST_EXIT"
    echo "    stderr: $(echo "$LAST_STDERR" | head -1)"
  fi
}

# assert_stderr — check that $LAST_STDERR contains a pattern (counts as a test)
assert_stderr() {
  local pattern="$1"
  TOTAL=$((TOTAL + 1))
  if echo "$LAST_STDERR" | grep -q "$pattern"; then
    PASS=$((PASS + 1))
    echo "  ✓ PASS — stderr contains '$pattern'"
  else
    FAIL=$((FAIL + 1))
    echo "  ✗ FAIL — expected '$pattern' in stderr"
    echo "    got: $(echo "$LAST_STDERR" | head -3)"
  fi
}

# ── Tests ────────────────────────────────────────────────────────────────────

echo "=== vitest-safe.sh tests ==="
echo ""

# -------------------------------------------------------------------
# 1. Rejects no arguments
# -------------------------------------------------------------------
echo "1. Rejects no arguments"
run_test 1 --
assert_stderr "No test file specified"

# -------------------------------------------------------------------
# 2. Rejects bare "run" subcommand
# -------------------------------------------------------------------
echo "2. Rejects bare 'run' subcommand"
run_test 1 -- "run"
assert_stderr "No test file specified"

# -------------------------------------------------------------------
# 3. Accepts valid test file (.test.tsx)
# -------------------------------------------------------------------
echo "3. Accepts valid test file (.test.tsx)"
run_test 0 "MEM_AVAILABLE_KB=16777216" -- "run" "src/App.test.tsx"
assert_stderr "\[npx-mock\]"

# -------------------------------------------------------------------
# 4. Accepts spec file (.spec.tsx)
# -------------------------------------------------------------------
echo "4. Accepts spec file (.spec.tsx)"
run_test 0 "MEM_AVAILABLE_KB=16777216" -- "run" "src/App.spec.tsx"
assert_stderr "\[npx-mock\]"

# -------------------------------------------------------------------
# 5. Rejects src/ prefix without .test. or .spec. (too permissive)
# -------------------------------------------------------------------
echo "5. Rejects src/ prefix without .test. or .spec."
run_test 1 "MEM_AVAILABLE_KB=16777216" -- "run" "src/components/Button"
assert_stderr "No test file specified"

# -------------------------------------------------------------------
# 6. Rejects non-test args (flags only, no test file path)
# -------------------------------------------------------------------
echo "6. Rejects non-test args (flags only)"
run_test 1 -- "run" "--config" "vitest.config.ts"
assert_stderr "No test file specified"

# -------------------------------------------------------------------
# 7. VITEST_MAX_WORKERS default (not set → set to 4)
# -------------------------------------------------------------------
echo "7. VITEST_MAX_WORKERS default set to 4"
run_test 0 "MEM_AVAILABLE_KB=16777216" -- "run" "src/App.test.tsx"
assert_stderr "Set VITEST_MAX_WORKERS=4"

# -------------------------------------------------------------------
# 8. Respects existing VITEST_MAX_WORKERS
# -------------------------------------------------------------------
echo "8. Respects existing VITEST_MAX_WORKERS"
run_test 0 "MEM_AVAILABLE_KB=16777216" "VITEST_MAX_WORKERS=2" -- "run" "src/App.test.tsx"
assert_stderr "VITEST_MAX_WORKERS already set"

# -------------------------------------------------------------------
# 9. Low memory rejection
# -------------------------------------------------------------------
echo "9. Low memory rejection (< 8 GB)"
run_test 1 "MEM_AVAILABLE_KB=4000000" -- "run" "src/App.test.tsx"
assert_stderr "Insufficient memory"

# -------------------------------------------------------------------
# 10. Sufficient memory acceptance
# -------------------------------------------------------------------
echo "10. Sufficient memory acceptance (16 GB)"
run_test 0 "MEM_AVAILABLE_KB=16777216" -- "run" "src/App.test.tsx"
assert_stderr "\[npx-mock\]"

# -------------------------------------------------------------------
# 11. macOS fallback (MEM_AVAILABLE_KB unset, falls through to /proc/meminfo on Linux)
# -------------------------------------------------------------------
echo "11. Handles empty MEM_AVAILABLE_KB (falls through to /proc/meminfo)"
run_test 0 "MEM_AVAILABLE_KB=" -- "run" "src/App.test.tsx"
assert_stderr "\[npx-mock\]"

# -------------------------------------------------------------------
# 12. Accepts paths with spaces
# -------------------------------------------------------------------
echo "12. Accepts paths with spaces"
run_test 0 "MEM_AVAILABLE_KB=16777216" -- "run" "src/My Component.test.tsx"
assert_stderr "\[npx-mock\]"

# -------------------------------------------------------------------
# 13. Accepts vitest list subcommand (read-only, no test file required)
# -------------------------------------------------------------------
echo "13. Accepts vitest list subcommand"
run_test 0 "MEM_AVAILABLE_KB=16777216" -- "list"
assert_stderr "\[npx-mock\]"

# -------------------------------------------------------------------
# 14. VITEST_MAX_WORKERS already-set message shows actual value
# -------------------------------------------------------------------
echo "14. VITEST_MAX_WORKERS already-set message shows actual value"
run_test 0 "MEM_AVAILABLE_KB=16777216" "VITEST_MAX_WORKERS=3" -- "run" "src/App.test.tsx"
assert_stderr "VITEST_MAX_WORKERS already set to 3"

# ── Summary ──────────────────────────────────────────────────────────────────

echo ""
echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
