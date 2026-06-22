#!/usr/bin/env bash
# Build script for the sprout WebAssembly shell module.
# Usage: ./scripts/build-wasm.sh [output-dir]
#        ./scripts/build-wasm.sh --dist <dist-dir>
#        ./scripts/build-wasm.sh --help
#
# Default behavior (no --dist flag):
#   1. Copies wasm_exec.js from GOROOT to the webui public directory
#   2. Compiles the Go WASM module from cmd/wasm/
#
# With --dist flag:
#   1. Builds the React webui (npm install + npm run build)
#   2. Builds the WASM binary
#   3. Generates a version.json with git metadata
#   4. Copies everything into a self-contained distribution directory

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WEBUI_DIR="$PROJECT_ROOT/webui"

# Parse command line arguments
DIST_MODE=false
OUTPUT_DIR="$PROJECT_ROOT/webui/public/wasm"

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
    echo "Usage: $0 [output-dir]"
    echo "       $0 --dist <dist-dir>"
    echo ""
    echo "Default behavior (no --dist flag):"
    echo "  Builds only the WASM module (sprout.wasm + wasm_exec.js)"
    echo "  to the specified directory (default: webui/public/wasm/)"
    echo ""
    echo "With --dist flag:"
    echo "  Creates a self-contained distribution package:"
    echo "    1. Builds the React webui (npm install + npm run build)"
    echo "    2. Builds the WASM binary"
    echo "    3. Generates a version.json with git metadata"
    echo "    4. Copies everything into the specified distribution directory"
    echo ""
    echo "Examples:"
    echo "  $0                          # Build WASM to default location"
    echo "  $0 /path/to/output          # Build WASM to custom directory"
    echo "  $0 --dist ./dist           # Build full distribution package"
    echo "  $0 --dist /opt/sprout      # Build full package to /opt/sprout"
    echo ""
    exit 0
fi

if [ "${1:-}" = "--dist" ]; then
    DIST_MODE=true
    if [ -z "${2:-}" ]; then
        echo "Error: --dist requires an output directory argument" >&2
        echo "Usage: $0 --dist <output-dir>" >&2
        exit 1
    fi
    DIST_DIR="${2}"
    # Resolve to absolute path (create directory temporarily if needed)
    mkdir -p "$DIST_DIR"
    DIST_DIR="$(cd "$DIST_DIR" && pwd)"
else
    # Backward compatible: positional arg or default
    OUTPUT_DIR="${1:-$PROJECT_ROOT/webui/public/wasm}"
fi

# Get current git tag or empty string
get_git_tag() {
    git describe --tags --abbrev=0 2>/dev/null || echo ""
}

# Get current git commit hash (short)
get_git_commit() {
    git rev-parse --short HEAD 2>/dev/null || echo ""
}

# Get build timestamp in ISO 8601 UTC format
get_build_date() {
    date -u +"%Y-%m-%dT%H:%M:%SZ"
}

# Build the React webui
build_webui() {
    echo "→ Building React webui..."
    (
        cd "$WEBUI_DIR"

        # Install dependencies (prefer npm ci for reproducibility, fallback to npm install)
        if [ ! -d "node_modules" ] || [ "package-lock.json" -nt "node_modules" ]; then
            echo "  Installing npm dependencies..."
            npm ci 2>/dev/null || npm install
        fi

        # Build the webui
        echo "  Running npm run build..."
        DISABLE_ESLINT_PLUGIN=true npm run build
        echo "  ✓ Webui build complete"
    )
}

# Build the WASM binary (original logic)
build_wasm() {
    local target_dir="$1"
    echo "→ Building WASM binary..."

    GOROOT="$(go env GOROOT)"
    WASM_EXEC_SRC="$GOROOT/lib/wasm/wasm_exec.js"
    WASM_EXEC_DST="$target_dir/wasm_exec.js"

    if [ ! -f "$WASM_EXEC_SRC" ]; then
        echo "Error: wasm_exec.js not found at $WASM_EXEC_SRC" >&2
        echo "Make sure your Go installation includes WASM support." >&2
        exit 1
    fi

    echo "  Copying wasm_exec.js..."
    cp "$WASM_EXEC_SRC" "$WASM_EXEC_DST"
    echo "    ✓ wasm_exec.js"

    # End of static asset copies — semantic search now requires the
    # __sproutONNX bridge (see Tier 2a in WASM_API.md).
    echo "  Compiling sprout.wasm (GOOS=js GOARCH=wasm)..."
    # -s -w strips the symbol table and DWARF debug info from the binary.
    # Saves ~25% of the WASM size with no runtime impact — symbols only
    # matter for stack traces inside Go's runtime panics, which the browser
    # surfaces via its own debugger anyway. Toggle with WASM_KEEP_SYMBOLS=1
    # if you need a full-detail panic trace for debugging.
    #
    # grammar_blobs_external disables gotreesitter's bulk grammar embed
    # entirely. pkg/ast/grammars_embed.go (SP-058) supplies only the five
    # grammars we actually use (go/ts/tsx/js/python — ~717KB) and overrides
    # the gotreesitter registry entries for them. The library's filesystem
    # blob-source path is never invoked because lookup is short-circuited
    # by our overrides.
    WASM_TAGS="grammar_blobs_external osusergo"
    LDFLAGS="-s -w"
    if [ "${WASM_KEEP_SYMBOLS:-}" = "1" ]; then
        LDFLAGS=""
        echo "    (WASM_KEEP_SYMBOLS=1: skipping symbol strip)"
    fi
    (cd "$PROJECT_ROOT" && GOOS=js GOARCH=wasm go build -tags "$WASM_TAGS" -ldflags="$LDFLAGS" -o "$target_dir/sprout.wasm" ./cmd/wasm/)

    echo "    ✓ sprout.wasm"

    WASM_SIZE=$(ls -lh "$target_dir/sprout.wasm" | awk '{print $5}')
    echo "  WASM binary size: $WASM_SIZE"

    # Build the embedding WASM (lazy-loaded by the browser when semantic
    # search or memory features are first used). This is a separate module
    # so the main sprout.wasm stays small for casual page loads (SP-045-3).
    echo "  Compiling embedding.wasm (GOOS=js GOARCH=wasm)..."
    (cd "$PROJECT_ROOT" && GOOS=js GOARCH=wasm go build -tags "$WASM_TAGS" -ldflags="$LDFLAGS" -o "$target_dir/embedding.wasm" ./cmd/embedding-wasm/)
    echo "    ✓ embedding.wasm"

    EMB_SIZE=$(ls -lh "$target_dir/embedding.wasm" | awk '{print $5}')
    echo "  Embedding WASM binary size: $EMB_SIZE"

    # Size threshold check: post-SP-058 the stripped binary lands ~40MB.
    # 50MB allows headroom for future Go runtime / dependency growth without
    # silently regressing the grammar embed back to the full set.
    WASM_SIZE_BYTES=$(stat -f%z "$target_dir/sprout.wasm" 2>/dev/null || stat -c%s "$target_dir/sprout.wasm" 2>/dev/null || echo 0)
    WASM_MAX_BYTES=$((50 * 1024 * 1024)) # 50MB
    if [ "$WASM_SIZE_BYTES" -gt "$WASM_MAX_BYTES" ] 2>/dev/null; then
        echo "  ⚠ WARNING: WASM binary ($WASM_SIZE) exceeds 50MB threshold!" >&2
        echo "  Consider auditing large dependencies or build tags." >&2
    fi
}

# Escape special characters for JSON string values
json_escape() {
    local str="$1"
    str="${str//\\/\\\\}"      # backslash → \\
    str="${str//\"/\\\"}"      # double quote → \"
    str="${str//$'\n'/\\n}"    # newline → \n
    str="${str//$'\r'/\\r}"    # carriage return → \r
    str="${str//$'\t'/\\t}"    # tab → \t
    echo "$str"
}

# Generate version.json with git metadata
generate_version_json() {
    local output_file="$1"
    echo "→ Generating version.json..."

    local tag
    local commit
    local date
    local version

    tag=$(get_git_tag)
    commit=$(get_git_commit)
    date=$(get_build_date)

    # If no tag, use commit hash as version
    if [ -z "$tag" ]; then
        version="dev-$commit"
    else
        version="$tag"
    fi

    # Escape for JSON
    local escaped_version escaped_commit escaped_date escaped_tag
    escaped_version=$(json_escape "$version")
    escaped_commit=$(json_escape "$commit")
    escaped_date=$(json_escape "$date")
    escaped_tag=$(json_escape "$tag")

    # Create JSON file
    cat > "$output_file" <<EOF
{
  "version": "$escaped_version",
  "commit": "$escaped_commit",
  "buildDate": "$escaped_date",
  "gitTag": "$escaped_tag"
}
EOF

    echo "  ✓ version.json"
    echo "    version: $version"
    echo "    commit: $commit"
    echo "    buildDate: $date"
    echo "    gitTag: $tag"
}

# Copy all dist files to the distribution directory
copy_dist_files() {
    local dist_dir="$1"
    echo "→ Copying files to distribution directory..."

    # Create subdirectories
    mkdir -p "$dist_dir/webui"
    mkdir -p "$dist_dir/wasm"

    # Copy webui build output
    if [ -d "$WEBUI_DIR/build" ] && [ "$(ls -A "$WEBUI_DIR/build" 2>/dev/null)" ]; then
        echo "  Copying webui/build/* → $dist_dir/webui/"
        cp -r "$WEBUI_DIR/build/"* "$dist_dir/webui/"
    else
        echo "  Warning: webui/build directory is empty or missing" >&2
    fi

    # Copy WASM files
    echo "  Copying wasm files → $dist_dir/wasm/"
    cp "$PROJECT_ROOT/webui/public/wasm/wasm_exec.js" "$dist_dir/wasm/"
    cp "$PROJECT_ROOT/webui/public/wasm/sprout.wasm" "$dist_dir/wasm/"
    if [ -f "$PROJECT_ROOT/webui/public/wasm/embedding.wasm" ]; then
        cp "$PROJECT_ROOT/webui/public/wasm/embedding.wasm" "$dist_dir/wasm/"
    fi

    # Copy version.json
    local version_json="$PROJECT_ROOT/webui/public/wasm/version.json"
    if [ -f "$version_json" ]; then
        echo "  Copying version.json → $dist_dir/"
        cp "$version_json" "$dist_dir/version.json"
    else
        echo "  Warning: version.json not found at $version_json" >&2
    fi

    echo "  ✓ All files copied"
}

# Calculate directory size
get_dir_size() {
    local dir="$1"
    # Use du -s for total size in KB, then format
    if command -v du >/dev/null 2>&1; then
        local size_kb
        size_kb=$(du -sk "$dir" 2>/dev/null | awk '{print $1}')
        if [ -n "$size_kb" ] && [ "$size_kb" -gt 0 ] 2>/dev/null; then
            if [ "$size_kb" -lt 1024 ]; then
                echo "${size_kb}KB"
            else
                local size_mb
                size_mb=$(awk "BEGIN {printf \"%.1f\", $size_kb / 1024}")
                echo "${size_mb}MB"
            fi
        else
            echo "unknown"
        fi
    else
        echo "unknown"
    fi
}

# Print distribution summary
print_dist_summary() {
    local dist_dir="$1"
    echo ""
    echo "✅ Distribution build complete!"
    echo ""
    echo "Output: $dist_dir"
    echo "Size: $(get_dir_size "$dist_dir")"
    echo ""
    echo "Contents:"
    echo "  webui/          - React application"
    echo "  wasm/           - WASM binary and runtime"
    echo "    wasm_exec.js  - Go WASM runtime"
    echo "    sprout.wasm   - Compiled WASM module"
    echo "  version.json    - Version metadata"
    echo ""
}

# ============================================================================
# Main execution
# ============================================================================

if [ "$DIST_MODE" = true ]; then
    # ===== DIST MODE =====
    echo "🏗️  Building sprout distribution package..."
    echo ""

    # Clean and create dist directory
    echo "→ Preparing distribution directory: $DIST_DIR"
    # Safety: never delete critical directories
    case "$DIST_DIR" in
        ""|"/"|"/usr"|"/var"|"/etc"|"/opt"|"/home"|"/tmp"|"$HOME"|"$PROJECT_ROOT")
            echo "Error: Refusing to delete directory '$DIST_DIR' (safety check)" >&2
            exit 1
            ;;
    esac
    if [ "${#DIST_DIR}" -lt 5 ]; then
        echo "Error: Distribution path '$DIST_DIR' looks too short to be safe" >&2
        exit 1
    fi
    if [ -d "$DIST_DIR" ]; then
        echo "  Removing existing directory..."
        rm -rf "$DIST_DIR"
    fi
    mkdir -p "$DIST_DIR"
    echo "  ✓ Directory ready"
    echo ""

    # Build webui
    build_webui
    echo ""

    # Build WASM to webui/public/wasm (temporary location)
    mkdir -p "$PROJECT_ROOT/webui/public/wasm"
    build_wasm "$PROJECT_ROOT/webui/public/wasm"
    echo ""

    # Generate version.json
    generate_version_json "$PROJECT_ROOT/webui/public/wasm/version.json"
    echo ""

    # Copy everything to dist directory
    copy_dist_files "$DIST_DIR"
    echo ""

    # Print summary
    print_dist_summary "$DIST_DIR"

else
    # ===== DEFAULT MODE (backward compatible) =====
    # Ensure output directory exists.
    mkdir -p "$OUTPUT_DIR"

    build_wasm "$OUTPUT_DIR"

    echo ""
    echo "Build complete:"
    echo "  $OUTPUT_DIR/wasm_exec.js ($(ls -lh "$OUTPUT_DIR/wasm_exec.js" | awk '{print $5}'))"
    echo "  $OUTPUT_DIR/sprout.wasm ($(ls -lh "$OUTPUT_DIR/sprout.wasm" | awk '{print $5}'))"
fi
