#!/bin/bash
# Tests for scripts/generate-provider-index.sh
# Run from repo root: bash scripts/generate-provider-index_test.sh

set -euo pipefail

PASS=0
FAIL=0
TOTAL=0

pass() {
    PASS=$((PASS + 1))
    TOTAL=$((TOTAL + 1))
    echo "  ✓ $1"
}

fail() {
    FAIL=$((FAIL + 1))
    TOTAL=$((TOTAL + 1))
    echo "  ✗ $1"
}

# --- Test helpers ---

# Create a temporary directory with mock configs and a providers dir
setup_test_env() {
    TEST_DIR=$(mktemp -d /tmp/provider-index-test.XXXXXX)
    mkdir -p "$TEST_DIR/pkg/agent_providers/configs"
    mkdir -p "$TEST_DIR/providers"

    # Create mock provider configs
    echo '{"name":"zeta"}' > "$TEST_DIR/pkg/agent_providers/configs/zeta.json"
    echo '{"name":"alpha"}' > "$TEST_DIR/pkg/agent_providers/configs/alpha.json"
    echo '{"name":"beta"}'  > "$TEST_DIR/pkg/agent_providers/configs/beta.json"
    echo '{"name":"gamma"}' > "$TEST_DIR/pkg/agent_providers/configs/gamma.json"

    # Patch the script to use our test dir by overriding CONFIGS_DIR logic
    # We'll run the script with a modified path via env var or symlink
    # Simplest: create a symlink from the script that points to test dir
    # Actually, the script resolves REPO_ROOT from BASH_SOURCE, so we need to
    # copy the script into our test dir structure.

    # Create scripts/ dir in test dir with a copy
    mkdir -p "$TEST_DIR/scripts"
    cp "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../scripts/generate-provider-index.sh" "$TEST_DIR/scripts/"

    echo "$TEST_DIR"
}

cleanup_test_env() {
    rm -rf "$1"
}

# --- Tests ---

echo "=== Test: Valid JSON output ==="
TEST_DIR=$(setup_test_env)
# Run script from within test dir (so BASH_SOURCE resolves correctly)
cd "$TEST_DIR"
output=$(bash scripts/generate-provider-index.sh 2>&1)
cd - > /dev/null

if jq empty "$TEST_DIR/providers/index.json" 2>/dev/null; then
    pass "Output is valid JSON"
else
    fail "Output is not valid JSON"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: schema_version is 1 ==="
TEST_DIR=$(setup_test_env)
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

sv=$(jq -r '.schema_version' "$TEST_DIR/providers/index.json")
if [[ "$sv" == "1" ]]; then
    pass "schema_version is 1"
else
    fail "schema_version is '$sv', expected '1'"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: published_at is ISO 8601 UTC ==="
TEST_DIR=$(setup_test_env)
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

pa=$(jq -r '.published_at' "$TEST_DIR/providers/index.json")
if [[ "$pa" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
    pass "published_at matches ISO 8601 UTC format: $pa"
else
    fail "published_at '$pa' does not match ISO 8601 UTC format"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: providers array contains correct names ==="
TEST_DIR=$(setup_test_env)
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

providers=$(jq -r '.providers[]' "$TEST_DIR/providers/index.json" | tr '\n' ' ')
expected="alpha beta gamma zeta "
if [[ "$providers" == "$expected" ]]; then
    pass "providers array contains correct names"
else
    fail "providers: got '$providers', expected '$expected'"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: providers are sorted alphabetically ==="
TEST_DIR=$(setup_test_env)
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

sorted_check=$(jq -r '.providers[]' "$TEST_DIR/providers/index.json")
sorted_expected=$(echo "$sorted_check" | sort)
if [[ "$sorted_check" == "$sorted_expected" ]]; then
    pass "providers are sorted alphabetically"
else
    fail "providers are NOT sorted alphabetically"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: no individual provider files created ==="
TEST_DIR=$(setup_test_env)
cd "$TEST_DIR"
# Before running, ensure providers/ dir is empty
rm -f "$TEST_DIR/providers/"*.json
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

# Count .json files in providers/ - should be exactly 1 (index.json)
json_count=$(find "$TEST_DIR/providers/" -name '*.json' | wc -l)
if [[ "$json_count" -eq 1 ]]; then
    pass "Only index.json exists in providers/ (no side-effect file copies)"
else
    fail "$json_count JSON files found in providers/, expected 1"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: error on missing configs directory ==="
TEST_DIR=$(setup_test_env)
rm -rf "$TEST_DIR/pkg/agent_providers/configs"
cd "$TEST_DIR"
if bash scripts/generate-provider-index.sh > /dev/null 2>&1; then
    fail "Script should fail when configs directory is missing"
else
    pass "Script correctly fails when configs directory is missing"
fi
cd - > /dev/null
cleanup_test_env "$TEST_DIR"

echo "=== Test: error on empty configs directory ==="
TEST_DIR=$(setup_test_env)
rm -f "$TEST_DIR/pkg/agent_providers/configs/"*.json
cd "$TEST_DIR"
if bash scripts/generate-provider-index.sh > /dev/null 2>&1; then
    fail "Script should fail when configs directory is empty"
else
    pass "Script correctly fails when configs directory is empty"
fi
cd - > /dev/null
cleanup_test_env "$TEST_DIR"

echo "=== Test: idempotent - running twice produces valid output ==="
TEST_DIR=$(setup_test_env)
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
first_output=$(cat "$TEST_DIR/providers/index.json")
# Remove index.json and run again
rm -f "$TEST_DIR/providers/index.json"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
second_output=$(cat "$TEST_DIR/providers/index.json")
cd - > /dev/null

# Both should have the same structure (timestamps may differ, so check structure only)
first_struct=$(echo "$first_output" | jq '{schema_version, providers}')
second_struct=$(echo "$second_output" | jq '{schema_version, providers}')
if [[ "$first_struct" == "$second_struct" ]]; then
    pass "Script is idempotent (same structure on repeated runs)"
else
    fail "Script produced different structure on second run"
fi
cleanup_test_env "$TEST_DIR"

echo ""
echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="
if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
echo "All tests passed!"
exit 0
