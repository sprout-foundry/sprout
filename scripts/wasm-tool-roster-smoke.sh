#!/usr/bin/env bash
# Smoke test: builds the WASM target and asserts the tool roster excludes
# vision, run_automate, browse_url, and codegraph (the SP-112-7/6/8 exclusions).
#
# Runs as part of CI's make build-all verification (added in .github/workflows/
# build.yml by SP-112-9). Also runnable locally:
#   bash scripts/wasm-tool-roster-smoke.sh
#
# The smoke test verifies that the WASM build correctly excludes platform-specific
# tools at registration time, as specified in SP-112-6, SP-112-7, and SP-112-8.

set -euo pipefail

cd "$(dirname "$0")/.."

# 1. Verify WASM build compiles.
# The WASM binary is built from cmd/wasm/ (not the root package, which has !js constraint)
echo "→ Building WASM target..."
WASM_TAGS="grammar_blobs_external osusergo"
GOOS=js GOARCH=wasm go build -tags "$WASM_TAGS" -o /tmp/sprout.wasm ./cmd/wasm/ 2>&1 || {
    # Try current directory if /tmp fails
    GOOS=js GOARCH=wasm go build -tags "$WASM_TAGS" -o ./sprout-test.wasm ./cmd/wasm/ 2>&1 || {
        echo "FAIL: WASM build failed"
        exit 1
    }
}
echo "✓ WASM build succeeded"

# 2. Verify excluded tools don't appear in the WASM-stripped tool list.
#    We use `go list` with build tag filtering to confirm the file selection.
echo "→ Checking WASM tool roster via go list..."

# Get files included in WASM build
WASMFILES=$(GOOS=js GOARCH=wasm go list -f '{{range .GoFiles}}{{.}}{{"\n"}}{{end}}' ./pkg/agent_tools/ 2>/dev/null || echo "")

# Verify WASM includes stub files (these are the WASM-specific versions)
# SP-112-6: Vision tools - should include all_vision_js.go (stub), NOT all_vision.go (native)
if echo "$WASMFILES" | grep -q "^all_vision_js\.go$"; then
    echo "✓ WASM includes all_vision_js.go (vision stub - SP-112-6)"
else
    echo "FAIL: WASM build missing all_vision_js.go"
    exit 1
fi

# SP-112-8: run_automate - should include all_run_automate_js.go (stub), NOT all_run_automate.go (native)
if echo "$WASMFILES" | grep -q "^all_run_automate_js\.go$"; then
    echo "✓ WASM includes all_run_automate_js.go (run_automate stub - SP-112-8)"
else
    echo "FAIL: WASM build missing all_run_automate_js.go"
    exit 1
fi

# SP-112-7: browse_url - should include all_browse_url_wasm.go (stub), NOT all_browse_url.go (native)
if echo "$WASMFILES" | grep -q "^all_browse_url_wasm\.go$"; then
    echo "✓ WASM includes all_browse_url_wasm.go (browse_url stub - SP-112-7)"
else
    echo "FAIL: WASM build missing all_browse_url_wasm.go"
    exit 1
fi

# SP-112-7: codegraph - should include all_codegraph_wasm.go (stub), NOT all_codegraph.go (native)
if echo "$WASMFILES" | grep -q "^all_codegraph_wasm\.go$"; then
    echo "✓ WASM includes all_codegraph_wasm.go (codegraph stub - SP-112-7)"
else
    echo "FAIL: WASM build missing all_codegraph_wasm.go"
    exit 1
fi

# 3. Verify that native files are NOT included in WASM build
# (negative test - these files should be excluded)
NATIVE_FILES="all_vision.go all_run_automate.go all_browse_url.go all_codegraph.go"

for file in $NATIVE_FILES; do
    if echo "$WASMFILES" | grep -q "^${file}$"; then
        echo "FAIL: WASM build incorrectly includes native $file"
        exit 1
    fi
done

echo "✓ WASM correctly excludes native platform files"

# 4. Verify we have the expected number of WASM-specific files
WASM_STUB_COUNT=$(echo "$WASMFILES" | grep -E "^(all_vision_js|all_run_automate_js|all_browse_url_wasm|all_codegraph_wasm)\.go$" | wc -l)
if [ "$WASM_STUB_COUNT" -ne 4 ]; then
    echo "FAIL: Expected 4 WASM stub files, found $WASM_STUB_COUNT"
    exit 1
fi
echo "✓ Found all 4 expected WASM stub files"

# Cleanup: remove any artifacts created during the smoke test. The WASM
# build may have written to /tmp/sprout.wasm (Linux/macOS) or the local
# ./sprout-test.wasm (Windows/CI fallback). Both paths are cleaned.
rm -f /tmp/sprout.wasm ./sprout-test.wasm 2>/dev/null || true

echo ""
echo "✓✓✓ WASM tool roster smoke test passed ✓✓✓"
