#!/usr/bin/env bash
# Build script for the ledit WebAssembly shell module.
# Usage: ./scripts/build-wasm.sh [output-dir]
#
# This script:
#   1. Copies wasm_exec.js from GOROOT to the webui public directory
#   2. Compiles the Go WASM module from cmd/wasm/

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Output directory defaults to webui/public/wasm/
OUTPUT_DIR="${1:-$PROJECT_ROOT/webui/public/wasm}"

# Ensure output directory exists.
mkdir -p "$OUTPUT_DIR"

GOROOT="$(go env GOROOT)"
WASM_EXEC_SRC="$GOROOT/lib/wasm/wasm_exec.js"
WASM_EXEC_DST="$OUTPUT_DIR/wasm_exec.js"

if [ ! -f "$WASM_EXEC_SRC" ]; then
    echo "Error: wasm_exec.js not found at $WASM_EXEC_SRC" >&2
    echo "Make sure your Go installation includes WASM support." >&2
    exit 1
fi

echo "→ Copying wasm_exec.js to $OUTPUT_DIR/"
cp "$WASM_EXEC_SRC" "$WASM_EXEC_DST"
echo "  ✓ wasm_exec.js"

echo "→ Building ledit.wasm (GOOS=js GOARCH=wasm)..."
(cd "$PROJECT_ROOT" && GOOS=js GOARCH=wasm go build -o "$OUTPUT_DIR/ledit.wasm" ./cmd/wasm/)

echo "  ✓ ledit.wasm"

WASM_SIZE=$(ls -lh "$OUTPUT_DIR/ledit.wasm" | awk '{print $5}')
echo ""
echo "Build complete:"
echo "  $WASM_EXEC_DST ($(ls -lh "$WASM_EXEC_DST" | awk '{print $5}'))"
echo "  $OUTPUT_DIR/ledit.wasm ($WASM_SIZE)"
