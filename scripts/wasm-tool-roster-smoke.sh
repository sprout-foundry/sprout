#!/usr/bin/env bash
# Smoke test: builds the WASM target and asserts the tool roster excludes
# vision, run_automate, browse_url, codegraph (the SP-112-7/6/8 exclusions)
# and asserts every design tool is on the roster (SP-140 invariant 7 as
# amended by SP-158: rendering goes through the host page in WASM).
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

# 5. SP-140 invariant 7 as amended by SP-158: every design tool ships to WASM.
#
#    The handlers are shared (no build tag, constructed in all.go's
#    unconditional list). Rendering reaches the host page through
#    pkg/webcontent's page renderer; the critique's vision pass has a js
#    variant that leaves the critique to the primary model.
echo "→ Checking design-tool WASM roster (SP-158)..."

DESIGN_HANDLERS="design_assets_handler.go design_validate_handler.go design_brief_handler.go design_export_handler.go design_sync_handler.go design_render_handler.go design_critique_handler.go design_import_sketch_handler.go"

# 5a. Every design handler is compiled into the WASM build.
for shared in $DESIGN_HANDLERS; do
    if echo "$WASMFILES" | grep -q "^${shared}$"; then
        echo "✓ WASM includes $shared"
    else
        echo "FAIL: WASM build missing $shared — design tools ship to every build (SP-158)"
        exit 1
    fi
done

# 5b. No design handler carries a build tag of its own.
for shared in $DESIGN_HANDLERS; do
    if head -1 "pkg/agent_tools/$shared" | grep -q "^//go:build"; then
        echo "FAIL: $shared has a build constraint; design tools must stay shared"
        exit 1
    fi
done
echo "✓ design handlers carry no build constraint"

# 5c. Each design handler is constructed in all.go's shared list.
for handler in designAssetsHandler designValidateHandler designBriefHandler designExportHandler designSyncHandler designRenderHandler designCritiqueHandler designImportSketchHandler; do
    if ! grep -q "&${handler}{}" pkg/agent_tools/all.go; then
        echo "FAIL: all.go does not construct ${handler} in the shared list"
        exit 1
    fi
done
echo "✓ all.go constructs every design handler"

# 5d. The browser-specific pieces are selected: the critique's vision pass
#     variant, and the page-backed renderer in pkg/webcontent.
if ! echo "$WASMFILES" | grep -q "^design_critique_vision_js\.go$"; then
    echo "FAIL: WASM build missing design_critique_vision_js.go"
    exit 1
fi
WEBCONTENTFILES=$(GOOS=js GOARCH=wasm go list -f '{{range .GoFiles}}{{.}}{{"\n"}}{{end}}' ./pkg/webcontent/ 2>/dev/null || echo "")
if ! echo "$WEBCONTENTFILES" | grep -q "^browser_page_js\.go$"; then
    echo "FAIL: WASM build missing pkg/webcontent/browser_page_js.go (page renderer)"
    exit 1
fi
echo "✓ WASM selects the page renderer and the primary-model critique pass"

# Cleanup: remove any artifacts created during the smoke test. The WASM
# build may have written to /tmp/sprout.wasm (Linux/macOS) or the local
# ./sprout-test.wasm (Windows/CI fallback). Both paths are cleaned.
rm -f /tmp/sprout.wasm ./sprout-test.wasm 2>/dev/null || true

echo ""
echo "✓✓✓ WASM tool roster smoke test passed ✓✓✓"
