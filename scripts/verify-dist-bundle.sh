#!/bin/bash
#
# Verify that a dist bundle can be served from a plain HTTP server.
# Checks that all assets load correctly (no 404s).
#

set -euo pipefail

# Default to dist/cloud
DIST_DIR="${1:-dist/cloud}"
PORT=18923
BASE_URL="http://localhost:${PORT}"
FAILED_ASSETS=()
TOTAL_ASSETS=0

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Verify directory exists
if [ ! -d "$DIST_DIR" ]; then
    echo "❌ Error: Directory '$DIST_DIR' does not exist"
    exit 1
fi

echo "🔍 Verifying dist bundle in: $DIST_DIR"
echo ""

# Function to check URL
check_url() {
    local url="$1"
    local description="${2:-$1}"
    TOTAL_ASSETS=$((TOTAL_ASSETS + 1))

    # Fetch with curl, check HTTP status code
    status=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "$url" 2>/dev/null || echo "000")

    if [ "$status" = "200" ]; then
        echo "  ✅ ${description}"
        return 0
    else
        echo "  ❌ ${description} (HTTP $status)"
        FAILED_ASSETS+=("$url")
        return 1
    fi
}

# Start HTTP server in background
echo "🚀 Starting HTTP server on port ${PORT}..."
cd "$DIST_DIR" || exit 1
python3 -m http.server "$PORT" > /tmp/dist-verify-server.log 2>&1 &
SERVER_PID=$!
cd - > /dev/null

# Wait for server to start
sleep 1

# Make sure server started successfully
if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "❌ Failed to start HTTP server"
    cat /tmp/dist-verify-server.log
    exit 1
fi

echo "  Server started (PID: $SERVER_PID)"
echo ""

# Cleanup function
cleanup() {
    if [ -n "${SERVER_PID:-}" ]; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -f /tmp/dist-verify-server.log
}

# Trap to ensure cleanup on exit
trap cleanup EXIT

echo "📋 Verifying assets..."
echo ""

# 1. Check index.html
check_url "${BASE_URL}/index.html" "index.html"

# 2. Extract and verify assets from index.html
echo ""
echo "Checking assets from index.html..."

# Fetch index.html content
INDEX_HTML=$(curl -s "${BASE_URL}/index.html" || echo "")

# Extract CSS files (Vite outputs to /assets/, CRA used /static/css/)
CSS_FILES=$(echo "$INDEX_HTML" | grep -oE 'href="(/assets/|/static/css/)[^"]*\.css"' | sed 's/href="//;s/"//' || true)
for css in $CSS_FILES; do
    check_url "${BASE_URL}${css}" "CSS: ${css}"
done

# Extract JS files (Vite outputs to /assets/, CRA used /static/js/)
JS_FILES=$(echo "$INDEX_HTML" | grep -oE '(src|href)="(/assets/|/static/js/)[^"]*\.js"' | sed 's/^[^"]*"//;s/"//' || true)
for js in $JS_FILES; do
    check_url "${BASE_URL}${js}" "JS: ${js}"
done

# Extract favicon and other icons
ICON_FILES=$(echo "$INDEX_HTML" | grep -o 'href="/[^"]*\.\(png\|svg\|ico\|json\)"' | sed 's/href="//;s/"//' || true)
for icon in $ICON_FILES; do
    check_url "${BASE_URL}${icon}" "Icon: ${icon}"
done

# 3. Check asset-manifest.json if it exists
echo ""
echo "Checking asset-manifest.json..."
if curl -sf "${BASE_URL}/asset-manifest.json" > /tmp/asset-manifest.json 2>/dev/null; then
    check_url "${BASE_URL}/asset-manifest.json" "asset-manifest.json"

    # Extract all file paths from asset-manifest.json
    # This JSON has keys like "main.js" -> "/static/js/main.js" or "/assets/main.js"
    MANIFEST_ASSETS=$(cat /tmp/asset-manifest.json | grep -oE '"/(static|assets)/[^"]*"' | sed 's/"//g' || true)
    for asset in $MANIFEST_ASSETS; do
        check_url "${BASE_URL}${asset}" "Manifest: ${asset}"
    done
    rm -f /tmp/asset-manifest.json
else
    echo "  ⚠ asset-manifest.json not found (may not be generated in this build)"
fi

# 4. Check version.json and validate mode
echo ""
echo "Checking version.json..."
check_url "${BASE_URL}/version.json" "version.json"

# Validate that version.json contains the correct mode for this dist bundle
EXPECTED_MODE=$(basename "$DIST_DIR")
ACTUAL_MODE=$(curl -sf "${BASE_URL}/version.json" | jq -r '.mode // empty' 2>/dev/null || echo "")
if [ -z "$ACTUAL_MODE" ]; then
    echo "  ❌ version.json has no 'mode' field"
    FAILED_ASSETS+=("version.json (missing mode field)")
    TOTAL_ASSETS=$((TOTAL_ASSETS + 1))
elif [ "$ACTUAL_MODE" != "$EXPECTED_MODE" ]; then
    echo "  ❌ version.json mode mismatch: expected '${EXPECTED_MODE}', got '${ACTUAL_MODE}'"
    FAILED_ASSETS+=("version.json (mode mismatch: expected ${EXPECTED_MODE}, got ${ACTUAL_MODE})")
    TOTAL_ASSETS=$((TOTAL_ASSETS + 1))
else
    echo "  ✅ version.json mode is '${ACTUAL_MODE}' (matches expected '${EXPECTED_MODE}')"
    TOTAL_ASSETS=$((TOTAL_ASSETS + 1))
fi

# 5. Check WASM files
echo ""
echo "Checking WASM files..."
if [ -d "$DIST_DIR/wasm" ]; then
    check_url "${BASE_URL}/wasm/sprout.wasm" "wasm/sprout.wasm"
    check_url "${BASE_URL}/wasm/wasm_exec.js" "wasm/wasm_exec.js"
else
    echo "  ⚠ wasm/ directory not found (WASM files may be optional for this build)"
fi

# 6. Check service worker
echo ""
echo "Checking service worker..."
check_url "${BASE_URL}/sw.js" "sw.js" || echo "  ⚠ sw.js not found (service worker may be optional)"

# 7. Check manifest.json
echo ""
echo "Checking manifest.json..."
if check_url "${BASE_URL}/manifest.json" "manifest.json"; then
    # Extract icon paths from manifest.json and verify them
    MANIFEST_JSON=$(curl -s "${BASE_URL}/manifest.json" || echo "")

    # Extract icon src values using grep - get the quoted string after "src":
    # Pattern matches: "src": "icon-192.png" and extracts just icon-192.png
    MANIFEST_ICONS=$(echo "$MANIFEST_JSON" | grep -o '"src":[[:space:]]*"[^"]*\.\(png\|jpg\|jpeg\|svg\|webp\)"' | sed 's/.*"\([^"]*\)"/\1/' || true)

    for icon in $MANIFEST_ICONS; do
        # Handle both absolute and relative paths in manifest
        if [[ "$icon" == /* ]]; then
            check_url "${BASE_URL}${icon}" "Manifest icon: ${icon}"
        else
            check_url "${BASE_URL}/${icon}" "Manifest icon: ${icon}"
        fi
    done
fi

# Print summary
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

FAILED_COUNT=${#FAILED_ASSETS[@]}
SUCCESS_COUNT=$((TOTAL_ASSETS - FAILED_COUNT))

if [ "$FAILED_COUNT" -eq 0 ]; then
    echo -e "${GREEN}✅ All ${TOTAL_ASSETS} assets verified successfully${NC}"
    echo ""
    exit 0
else
    echo -e "${RED}❌ ${FAILED_COUNT} of ${TOTAL_ASSETS} assets failed to load${NC}"
    echo ""
    echo "Failed assets:"
    for asset in "${FAILED_ASSETS[@]}"; do
        echo "  - $asset"
    done
    echo ""
    exit 1
fi
