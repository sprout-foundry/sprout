#!/usr/bin/env bash
# Smoke test: builds the WASM target and asserts the tool roster excludes
# vision, run_automate, browse_url, codegraph (the SP-112-7/6/8 exclusions)
# and the browser/vision-dependent design tools design_render and
# design_import_sketch (the SP-140 invariant 7 exclusion), while keeping
# design_assets and design_validate on the roster.
#
# Runs as part of CI's make build-all verification (added in .github/workflows/
# build.yml by SP-112-9; extended by SP-140-2 item 2.10). Also runnable
# locally:
#   bash scripts/wasm-tool-roster-smoke.sh
#
# The smoke test verifies that the WASM build correctly excludes platform-specific
# tools at registration time, as specified in SP-112-6, SP-112-7, SP-112-8,
# and SP-140 invariant 7.

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

# 5. SP-140 invariant 7 / SP-140-2 item 2.10: design-tool roster split.
#
#    The design tier is partitioned by dependency:
#      - design_assets, design_validate: pure Go → shared AllTools, on WASM.
#      - design_render (host browser), design_import_sketch (vision):
#        native-only, with _handler_js.go stubs returning nil.
#
#    The tools are registered by broad file families (design*), so we assert on
#    the build-tag file selection AND that no design handler is constructed
#    directly in all.go's unconditional list.
echo "→ Checking design-tool WASM roster split (SP-140 invariant 7)..."

# 5a. WASM must include the two browser/vision design stubs...
for stub in design_render_handler_js.go design_import_sketch_handler_js.go; do
    if echo "$WASMFILES" | grep -q "^${stub}$"; then
        echo "✓ WASM includes $stub (design stub - SP-140 invariant 7)"
    else
        echo "FAIL: WASM build missing $stub"
        exit 1
    fi
done

# 5b. ...and must NOT include their native implementations.
for native in design_render_handler.go design_import_sketch_handler.go; do
    if echo "$WASMFILES" | grep -q "^${native}$"; then
        echo "FAIL: WASM build incorrectly includes native $native"
        exit 1
    fi
done
echo "✓ WASM excludes native design_render / design_import_sketch"

# 5c. design_assets and design_validate are pure Go: they must be compiled into
#     the WASM build unconditionally (no build tag), which is what puts them on
#     the WASM roster via AllTools.
for shared in design_assets_handler.go design_validate_handler.go; do
    if echo "$WASMFILES" | grep -q "^${shared}$"; then
        echo "✓ WASM includes $shared (shared design tool)"
    else
        echo "FAIL: WASM build missing $shared — design_assets/design_validate are pure Go"
        echo "      and must compile on GOOS=js (SP-140-2 AC, SP-140 invariant 7)"
        exit 1
    fi
done

# 5d. design_assets must have no build tag of its own (a !js tag here would
#     drop it from WASM even though the file is named in the shared list).
if head -1 pkg/agent_tools/design_assets_handler.go | grep -q "^//go:build"; then
    echo "FAIL: design_assets_handler.go gained a build constraint; it must stay shared"
    exit 1
fi
echo "✓ design_assets_handler.go carries no build constraint"

# 5e. The native-only handlers must not be constructed directly in all.go —
#     that would not compile on js, and would mean the stub is not the path.
for handler in designRenderHandler designImportSketchHandler; do
    if grep -q "&${handler}{}" pkg/agent_tools/all.go; then
        echo "FAIL: all.go constructs &${handler}{} directly; it is native-only and"
        echo "      must be reached through its build-tagged registrar (SP-140 invariant 7)"
        exit 1
    fi
done
echo "✓ all.go reaches native-only design handlers via their registrars"

# 5f. The WASM stubs must actually return nil (unregistered), not a handler.
for stub in design_render_handler_js.go design_import_sketch_handler_js.go; do
    if ! awk '/^func registerDesign/{f=1} f' "pkg/agent_tools/${stub}" | grep -q "return nil"; then
        echo "FAIL: pkg/agent_tools/${stub} does not return nil; the tool would be"
        echo "      advertised on WASM without a working implementation"
        exit 1
    fi
done
echo "✓ design WASM stubs return nil (tools unregistered, not registered-but-broken)"

# 5g. No design_assets WASM variant exists. design_assets is shared, so a
#     per-tool stub file (the all_assets_wasm.go shape the AC anticipated)
#     would be redundant and would signal the shared registration regressed.
for candidate in all_assets_wasm.go design_assets_handler_js.go design_assets_handler_wasm.go; do
    if [ -e "pkg/agent_tools/${candidate}" ]; then
        echo "FAIL: pkg/agent_tools/${candidate} exists; design_assets is WASM-eligible"
        echo "      via the shared AllTools list and needs no per-tool WASM variant"
        exit 1
    fi
done
echo "✓ no redundant design_assets WASM variant"

# 5h. design_assets is on the roster by construction: it is listed in the
#     shared (untagged) list in all.go, which the WASM build compiles.
if ! grep -q "&designAssetsHandler{}" pkg/agent_tools/all.go; then
    echo "FAIL: all.go does not construct &designAssetsHandler{} in the shared list"
    exit 1
fi
if ! grep -q "&designValidateHandler{}" pkg/agent_tools/all.go; then
    echo "FAIL: all.go does not construct &designValidateHandler{} in the shared list"
    exit 1
fi
echo "✓ design_assets and design_validate are in all.go's shared tool list"

# Cleanup: remove any artifacts created during the smoke test. The WASM
# build may have written to /tmp/sprout.wasm (Linux/macOS) or the local
# ./sprout-test.wasm (Windows/CI fallback). Both paths are cleaned.
rm -f /tmp/sprout.wasm ./sprout-test.wasm 2>/dev/null || true

echo ""
echo "✓✓✓ WASM tool roster smoke test passed ✓✓✓"
