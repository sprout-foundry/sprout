#!/usr/bin/env bash
# Check whether the React web UI bundle is stale relative to its inputs.
#
# Exit codes:
#   0  rebuild needed
#   1  bundle is up-to-date
#
# Inputs watched:
#   - webui/src/**/*.{ts,tsx,js,jsx,css}     (app source)
#   - webui/public/**                        (verbatim public assets — manifest, icons, wasm)
#   - webui/index.html, vite.config.ts, tsconfig.json (build entry + config)
#   - webui/package.json + root package-lock.json     (deps + workspace lockfile)
#   - packages/{ui,events}/src/**            (shared workspace packages)
#   - packages/{ui,events}/{package,tsconfig}.json    (workspace pkg configs)
#
# Reference output:
#   webui/dist/index.html — the actual bundle file. The dist directory's
#   mtime only advances on add/remove; relying on it lets `vite build`
#   overwrite the same filenames without busting the cache. The index.html
#   gets rewritten on every build, so its mtime is a reliable "last built".
#
# This is intentionally conservative: when in doubt (missing files,
# unreadable mtimes), rebuild. A spurious rebuild costs seconds; a missed
# rebuild has caused "is it me or is it cached?" debugging sessions.

set -u

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
webui_dir="$PROJECT_ROOT/webui"
static_dir="$PROJECT_ROOT/pkg/webui/static"
pkgs_dir="$PROJECT_ROOT/packages"

# Hard prerequisites — if any are missing, rebuild from scratch.
[ ! -d "$webui_dir/node_modules" ] && exit 0
[ ! -d "$webui_dir/dist" ] && exit 0
[ ! -d "$static_dir" ] && exit 0
[ ! -f "$webui_dir/dist/index.html" ] && exit 0

# Portable mtime read: GNU stat first, BSD/macOS stat second. Echoes the
# epoch seconds to stdout; empty string on any failure (caller decides).
mtime() {
    local f="$1"
    [ -e "$f" ] || { echo ""; return; }
    stat -c %Y "$f" 2>/dev/null || stat -f %m "$f" 2>/dev/null || echo ""
}

# Newest mtime across a list of files / globs. Args may be plain files or
# `find`-rooted directories; relies on `find -printf '%T@\n'` (GNU) with a
# `stat`-based fallback for portability.
newest_mtime_below() {
    local root="$1"
    [ -d "$root" ] || { echo "0"; return; }
    # Restrict by extension where it matters; for catch-alls (public/),
    # the caller passes the dir alone and we walk every file.
    shift
    local find_args=( "$root" -type f )
    if [ "$#" -gt 0 ]; then
        find_args+=( '(' )
        local first=1
        for ext in "$@"; do
            if [ "$first" -eq 1 ]; then
                find_args+=( -name "*.$ext" )
                first=0
            else
                find_args+=( -o -name "*.$ext" )
            fi
        done
        find_args+=( ')' )
    fi
    # Prefer `find -printf '%T@'` (GNU). On BSD/macOS that flag doesn't
    # exist; fall back to stat per-file (slower but reliable).
    local out
    out=$(find "${find_args[@]}" -printf '%T@\n' 2>/dev/null | sort -rn | head -1)
    if [ -n "$out" ]; then
        echo "${out%.*}"
        return
    fi
    # BSD/macOS path.
    local newest=0 t
    while IFS= read -r f; do
        t=$(mtime "$f")
        [ -n "$t" ] && [ "$t" -gt "$newest" ] && newest=$t
    done < <(find "${find_args[@]}" 2>/dev/null)
    echo "$newest"
}

# Reference mtime: when the bundle was last built. dist/index.html
# rewrites on every build, so its mtime is the canonical "built at".
build_mtime=$(mtime "$webui_dir/dist/index.html")
if [ -z "$build_mtime" ]; then
    exit 0  # Can't read the bundle's mtime — safer to rebuild.
fi

newer_than_build() {
    local label="$1" t="$2"
    [ -n "$t" ] || return 1
    [ "$t" -gt "$build_mtime" ] || return 1
    # Stay quiet by default; uncomment for debugging:
    # echo "[rebuild-check] $label is newer than bundle ($t > $build_mtime)" >&2
    return 0
}

# ── Inputs that should trigger a rebuild ───────────────────────────────

# 1) Webui app source — the original check.
t=$(newest_mtime_below "$webui_dir/src" ts tsx js jsx mjs cjs css scss html)
newer_than_build "webui/src" "$t" && exit 0

# 2) Webui public assets (manifest, icons, wasm copied verbatim into dist).
t=$(newest_mtime_below "$webui_dir/public")
newer_than_build "webui/public" "$t" && exit 0

# 3) Webui build configs + entry HTML — these change bundle output even
#    when no source file in src/ moved.
for f in \
    "$webui_dir/index.html" \
    "$webui_dir/vite.config.ts" \
    "$webui_dir/vite.config.js" \
    "$webui_dir/vite.config.mts" \
    "$webui_dir/tsconfig.json" \
    "$webui_dir/tsconfig.app.json" \
    "$webui_dir/tsconfig.node.json" \
    "$webui_dir/package.json" \
    "$PROJECT_ROOT/package-lock.json"
do
    [ -f "$f" ] || continue
    t=$(mtime "$f")
    newer_than_build "${f#$PROJECT_ROOT/}" "$t" && exit 0
done

# 4) Shared workspace packages — webui imports @sprout/{ui,events}, which
#    resolve via symlinks (npm workspaces) to packages/{ui,events}.
#    Their dist/ output is what webui actually consumes at bundle time,
#    but their *source* is what we care about for staleness — if the
#    source moved we want to rebuild the package AND the webui bundle.
for pkg in ui events; do
    pkg_root="$pkgs_dir/$pkg"
    [ -d "$pkg_root" ] || continue
    t=$(newest_mtime_below "$pkg_root/src" ts tsx js jsx mjs cjs css scss)
    newer_than_build "packages/$pkg/src" "$t" && exit 0
    for f in \
        "$pkg_root/package.json" \
        "$pkg_root/package-lock.json" \
        "$pkg_root/tsconfig.json" \
        "$pkg_root/vite.config.ts" \
        "$pkg_root/vite.config.js" \
        "$pkg_root/vite.config.mts"
    do
        [ -f "$f" ] || continue
        t=$(mtime "$f")
        newer_than_build "${f#$PROJECT_ROOT/}" "$t" && exit 0
    done
done

# All inputs are older than the bundle — up-to-date.
exit 1
