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

    # Create mock provider configs (HTTPS endpoints — publishable)
    echo '{"name":"zeta","endpoint":"https://api.zeta.example/v1/chat/completions"}' > "$TEST_DIR/pkg/agent_providers/configs/zeta.json"
    echo '{"name":"alpha","endpoint":"https://api.alpha.example/v1/chat/completions"}' > "$TEST_DIR/pkg/agent_providers/configs/alpha.json"
    echo '{"name":"beta","endpoint":"https://api.beta.example/v1/chat/completions"}' > "$TEST_DIR/pkg/agent_providers/configs/beta.json"
    echo '{"name":"gamma","endpoint":"https://api.gamma.example/v1/chat/completions"}' > "$TEST_DIR/pkg/agent_providers/configs/gamma.json"

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

echo "=== Test: community-configs/ providers are included in index ==="
TEST_DIR=$(setup_test_env)
mkdir -p "$TEST_DIR/pkg/agent_providers/community-configs"
echo '{"name":"omega-community","endpoint":"https://api.omega.example/v1/chat/completions"}' > "$TEST_DIR/pkg/agent_providers/community-configs/omega-community.json"
echo '{"name":"delta-community","endpoint":"https://api.delta.example/v1/chat/completions"}' > "$TEST_DIR/pkg/agent_providers/community-configs/delta-community.json"
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

providers=$(jq -r '.providers[]' "$TEST_DIR/providers/index.json" | tr '\n' ' ')
expected="alpha beta delta-community gamma omega-community zeta "
if [[ "$providers" == "$expected" ]]; then
    pass "community-configs providers merged + sorted into index"
else
    fail "providers: got '$providers', expected '$expected'"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: error on cross-dir provider collision ==="
TEST_DIR=$(setup_test_env)
mkdir -p "$TEST_DIR/pkg/agent_providers/community-configs"
# Same id 'alpha' exists in both — should fail loud rather than pick a winner.
echo '{"name":"alpha","endpoint":"https://api.alpha.example/v1/chat/completions"}' > "$TEST_DIR/pkg/agent_providers/community-configs/alpha.json"
cd "$TEST_DIR"
if bash scripts/generate-provider-index.sh > /dev/null 2>&1; then
    fail "Script should fail when the same id exists in configs/ and community-configs/"
else
    pass "Script correctly fails on cross-dir collision"
fi
cd - > /dev/null
cleanup_test_env "$TEST_DIR"

echo "=== Test: community-configs/ only (no embedded configs) ==="
TEST_DIR=$(setup_test_env)
# Remove the embedded configs entirely so only community-configs/ has entries.
rm -f "$TEST_DIR/pkg/agent_providers/configs/"*.json
mkdir -p "$TEST_DIR/pkg/agent_providers/community-configs"
echo '{"name":"only-community","endpoint":"https://api.only.example/v1/chat/completions"}' > "$TEST_DIR/pkg/agent_providers/community-configs/only-community.json"
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

providers=$(jq -r '.providers[]' "$TEST_DIR/providers/index.json" | tr '\n' ' ')
if [[ "$providers" == "only-community " ]]; then
    pass "community-configs-only case produces the expected index"
else
    fail "providers: got '$providers', expected 'only-community '"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: error when BOTH dirs are empty ==="
TEST_DIR=$(setup_test_env)
rm -f "$TEST_DIR/pkg/agent_providers/configs/"*.json
mkdir -p "$TEST_DIR/pkg/agent_providers/community-configs"
cd "$TEST_DIR"
if bash scripts/generate-provider-index.sh > /dev/null 2>&1; then
    fail "Script should fail when both source dirs are empty"
else
    pass "Script correctly fails when both source dirs are empty"
fi
cd - > /dev/null
cleanup_test_env "$TEST_DIR"

echo "=== Test: non-HTTPS (local-only) providers are excluded from index ==="
TEST_DIR=$(setup_test_env)
# Add a local-only provider (lmstudio-style http://127.0.0.1 endpoint).
echo '{"name":"lmstudio","endpoint":"http://127.0.0.1:1234/v1/chat/completions"}' \
    > "$TEST_DIR/pkg/agent_providers/configs/lmstudio.json"
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

providers=$(jq -r '.providers[]' "$TEST_DIR/providers/index.json" | tr '\n' ' ')
expected="alpha beta gamma zeta "
if [[ "$providers" == "$expected" ]]; then
    pass "non-HTTPS provider excluded from index"
else
    fail "providers: got '$providers', expected '$expected' (lmstudio should be absent)"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: case-insensitive HTTPS scheme (HTTPS:// accepted) ==="
TEST_DIR=$(setup_test_env)
# Overwrite alpha with an uppercase-scheme endpoint.
echo '{"name":"alpha","endpoint":"HTTPS://api.alpha.example/v1/chat/completions"}' \
    > "$TEST_DIR/pkg/agent_providers/configs/alpha.json"
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

providers=$(jq -r '.providers[]' "$TEST_DIR/providers/index.json" | tr '\n' ' ')
expected="alpha beta gamma zeta "
if [[ "$providers" == "$expected" ]]; then
    pass "uppercase HTTPS:// scheme accepted"
else
    fail "providers: got '$providers', expected '$expected'"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: missing endpoint field is excluded ==="
TEST_DIR=$(setup_test_env)
# Overwrite beta with no endpoint field (legacy/bare mocks).
echo '{"name":"beta"}' > "$TEST_DIR/pkg/agent_providers/configs/beta.json"
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

providers=$(jq -r '.providers[]' "$TEST_DIR/providers/index.json" | tr '\n' ' ')
expected="alpha gamma zeta "
if [[ "$providers" == "$expected" ]]; then
    pass "provider with missing endpoint excluded"
else
    fail "providers: got '$providers', expected '$expected' (beta should be absent)"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: local-only community provider also excluded ==="
TEST_DIR=$(setup_test_env)
mkdir -p "$TEST_DIR/pkg/agent_providers/community-configs"
echo '{"name":"local-comm","endpoint":"http://localhost:8080/v1/chat/completions"}' \
    > "$TEST_DIR/pkg/agent_providers/community-configs/local-comm.json"
echo '{"name":"remote-comm","endpoint":"https://api.remote.example/v1/chat/completions"}' \
    > "$TEST_DIR/pkg/agent_providers/community-configs/remote-comm.json"
cd "$TEST_DIR"
bash scripts/generate-provider-index.sh > /dev/null 2>&1
cd - > /dev/null

providers=$(jq -r '.providers[]' "$TEST_DIR/providers/index.json" | tr '\n' ' ')
expected="alpha beta gamma remote-comm zeta "
if [[ "$providers" == "$expected" ]]; then
    pass "local-only community provider excluded, remote one included"
else
    fail "providers: got '$providers', expected '$expected' (local-comm should be absent)"
fi
cleanup_test_env "$TEST_DIR"

echo "=== Test: all providers local-only fails loudly (no empty publish) ==="
TEST_DIR=$(setup_test_env)
# Overwrite every config with a local endpoint.
for name in zeta alpha beta gamma; do
    echo "{\"name\":\"$name\",\"endpoint\":\"http://127.0.0.1:8080\"}" \
        > "$TEST_DIR/pkg/agent_providers/configs/$name.json"
done
cd "$TEST_DIR"
if bash scripts/generate-provider-index.sh > /dev/null 2>&1; then
    fail "Script should fail when no providers are publishable (all local-only)"
else
    pass "Script correctly fails when all providers are local-only (no empty publish)"
fi
cd - > /dev/null
cleanup_test_env "$TEST_DIR"

echo ""
echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="
if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
echo "All tests passed!"
exit 0
