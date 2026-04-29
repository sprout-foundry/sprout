#!/usr/bin/env bash
# Check if React UI needs rebuild
# Returns 0 (true) if rebuild needed, 1 (false) if up-to-date

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
webui_dir="$PROJECT_ROOT/webui"
static_dir="$PROJECT_ROOT/pkg/webui/static"

# Always rebuild if node_modules or build dir missing
[ ! -d "$webui_dir/node_modules" ] && exit 0
[ ! -d "$webui_dir/build" ] && exit 0
[ ! -d "$static_dir" ] && exit 0

# Find the most recent source file modification time
newest_src=$(find "$webui_dir/src" -type f \( -name "*.ts" -o -name "*.tsx" -o -name "*.js" -o -name "*.jsx" -o -name "*.css" \) -printf '%T@\n' 2>/dev/null | sort -rn | head -1)

# Find the build directory modification time
build_mtime=$(stat -c %Y "$webui_dir/build" 2>/dev/null || stat -f %m "$webui_dir/build" 2>/dev/null)

if [ -z "$newest_src" ]; then
    # No source files found, assume up-to-date
    exit 1
fi

# Convert newest_src from nanoseconds to seconds
newest_src_sec=${newest_src%.*}

# If newest source is newer than build, rebuild needed
if [ "$newest_src_sec" -gt "$build_mtime" ]; then
    exit 0  # Rebuild needed
fi

# Check package.json changes
if [ -f "$webui_dir/package.json" ]; then
    pkg_json_mtime=$(stat -c %Y "$webui_dir/package.json" 2>/dev/null || stat -f %m "$webui_dir/package.json" 2>/dev/null)
    if [ "$pkg_json_mtime" -gt "$build_mtime" ]; then
        exit 0  # Rebuild needed
    fi
fi

# Check package-lock.json changes
if [ -f "$webui_dir/package-lock.json" ]; then
    lock_mtime=$(stat -c %Y "$webui_dir/package-lock.json" 2>/dev/null || stat -f %m "$webui_dir/package-lock.json" 2>/dev/null)
    if [ "$lock_mtime" -gt "$build_mtime" ]; then
        exit 0  # Rebuild needed
    fi
fi

# No rebuild needed
exit 1
