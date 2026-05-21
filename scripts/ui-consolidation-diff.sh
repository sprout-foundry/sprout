#!/usr/bin/env bash
#
# UI Consolidation Diff Report
# Compares components between packages/ui/src/components/ and webui/src/components/
# Usage: ./scripts/ui-consolidation-diff.sh
# Run from the repo root directory.
#

set -uo pipefail

PACKAGES_DIR="packages/ui/src/components"
WEBUI_DIR="webui/src/components"

# Threshold (bytes) below which a file is considered a "thin wrapper / stub"
THIN_THRESHOLD=500

# ── helpers ────────────────────────────────────────────────────────────────

die() { echo "Error: $*" >&2; exit 1; }

file_size() { wc -c < "$1" 2>/dev/null || echo 0; }

repeat_char() {
    # repeat_char <char> <count>
    local ch="$1" n="$2" out=""
    local i=0
    while [ "$i" -lt "$n" ]; do out="${out}${ch}"; i=$((i+1)); done
    printf '%s' "$out"
}

# ── preflight checks ───────────────────────────────────────────────────────

[ -d "$PACKAGES_DIR" ] || die "Directory $PACKAGES_DIR does not exist. Run from the repo root."
[ -d "$WEBUI_DIR"   ] || die "Directory $WEBUI_DIR does not exist. Run from the repo root."

# ── collect file lists ─────────────────────────────────────────────────────

# Gather .tsx files (exclude .test.tsx and .stories.tsx — those are not components)
mapfile -t PKG_TSX < <(find "$PACKAGES_DIR" -maxdepth 1 -name "*.tsx" \
    ! -name "*.test.tsx" ! -name "*.stories.tsx" -printf '%f\n' | sort)
mapfile -t WEB_TSX < <(find "$WEBUI_DIR"   -maxdepth 1 -name "*.tsx" \
    ! -name "*.test.tsx" ! -name "*.stories.tsx" -printf '%f\n' | sort)

# Gather .css files
mapfile -t PKG_CSS < <(find "$PACKAGES_DIR" -maxdepth 1 -name "*.css" -printf '%f\n' | sort)
mapfile -t WEB_CSS < <(find "$WEBUI_DIR"   -maxdepth 1 -name "*.css" -printf '%f\n' | sort)

# ── counts ─────────────────────────────────────────────────────────────────

pkg_tsx_count=${#PKG_TSX[@]}
pkg_css_count=${#PKG_CSS[@]}
pkg_total=$((pkg_tsx_count + pkg_css_count))

web_tsx_count=${#WEB_TSX[@]}
web_css_count=${#WEB_CSS[@]}
web_total=$((web_tsx_count + web_css_count))

# ── find overlapping basenames (across both .tsx and .css) ─────────────────

all_pkg_basenames=()
if [ "$pkg_tsx_count" -gt 0 ]; then all_pkg_basenames+=("${PKG_TSX[@]}"); fi
if [ "$pkg_css_count" -gt 0 ]; then all_pkg_basenames+=("${PKG_CSS[@]}"); fi
all_web_basenames=()
if [ "$web_tsx_count" -gt 0 ]; then all_web_basenames+=("${WEB_TSX[@]}"); fi
if [ "$web_css_count" -gt 0 ]; then all_web_basenames+=("${WEB_CSS[@]}"); fi

pkg_base_sorted=$(printf '%s\n' "${all_pkg_basenames[@]}" | sort -u)
web_base_sorted=$(printf '%s\n' "${all_web_basenames[@]}" | sort -u)

mapfile -t overlap_list < <(comm -12 <(echo "$pkg_base_sorted") <(echo "$web_base_sorted"))

overlap_count=${#overlap_list[@]}

# ── print header ───────────────────────────────────────────────────────────

echo ""
echo "=== UI Consolidation Diff Report ==="
echo ""
echo "Total packages/ui components: $pkg_tsx_count (.tsx) + $pkg_css_count (.css) = $pkg_total"
echo "Total webui components:      $web_tsx_count (.tsx) + $web_css_count (.css) = $web_total"
echo "Overlapping basenames:       $overlap_count"

# ── categorize each overlapping file ───────────────────────────────────────

declare -A categories
declare -A details
declare -A pkg_sizes
declare -A web_sizes

for bname in "${overlap_list[@]}"; do
    pkg_total_size=0
    web_total_size=0

    # Derive the base name (strip .tsx or .css)
    base_name="${bname%.tsx}"
    base_name="${base_name%.css}"

    # Count .tsx sizes
    tsx_name="${base_name}.tsx"
    if [ -f "$PACKAGES_DIR/$tsx_name" ]; then
        pkg_total_size=$((pkg_total_size + $(file_size "$PACKAGES_DIR/$tsx_name")))
    fi
    if [ -f "$WEBUI_DIR/$tsx_name" ]; then
        web_total_size=$((web_total_size + $(file_size "$WEBUI_DIR/$tsx_name")))
    fi

    # Count .css sizes
    css_name="${base_name}.css"
    if [ -f "$PACKAGES_DIR/$css_name" ]; then
        pkg_total_size=$((pkg_total_size + $(file_size "$PACKAGES_DIR/$css_name")))
    fi
    if [ -f "$WEBUI_DIR/$css_name" ]; then
        web_total_size=$((web_total_size + $(file_size "$WEBUI_DIR/$css_name")))
    fi

    pkg_sizes["$bname"]="$pkg_total_size"
    web_sizes["$bname"]="$web_total_size"

    # ── categorize ───────────────────────────────────────────────────

    has_diff=false

    # Compare .tsx if both exist
    if [ -f "$PACKAGES_DIR/$tsx_name" ] && [ -f "$WEBUI_DIR/$tsx_name" ]; then
        if ! diff -q "$PACKAGES_DIR/$tsx_name" "$WEBUI_DIR/$tsx_name" >/dev/null 2>&1; then
            has_diff=true
        fi
    elif [ -f "$PACKAGES_DIR/$tsx_name" ] || [ -f "$WEBUI_DIR/$tsx_name" ]; then
        has_diff=true
    fi

    # Compare .css if both exist
    if [ -f "$PACKAGES_DIR/$css_name" ] && [ -f "$WEBUI_DIR/$css_name" ]; then
        if ! diff -q "$PACKAGES_DIR/$css_name" "$WEBUI_DIR/$css_name" >/dev/null 2>&1; then
            has_diff=true
        fi
    elif [ -f "$PACKAGES_DIR/$css_name" ] || [ -f "$WEBUI_DIR/$css_name" ]; then
        has_diff=true
    fi

    if ! $has_diff; then
        categories["$bname"]="identical"
        details["$bname"]="Files are byte-for-byte identical"
    elif [ "$pkg_total_size" -lt "$THIN_THRESHOLD" ]; then
        categories["$bname"]="webui-leads"
        details["$bname"]="packages/ui version is thin (${pkg_total_size}B)"
    elif [ "$web_total_size" -lt "$THIN_THRESHOLD" ]; then
        categories["$bname"]="packages-leads"
        details["$bname"]="webui version is thin (${web_total_size}B)"
    else
        categories["$bname"]="divergent"
        details["$bname"]="Both versions are substantial and differ"
    fi
done

# ── print overlap analysis table ───────────────────────────────────────────

echo ""
echo "=== Overlap Analysis ==="
echo ""

# Calculate column widths
max_name_len=0
for bname in "${overlap_list[@]}"; do
    if [ ${#bname} -gt $max_name_len ]; then
        max_name_len=${#bname}
    fi
done
[ $max_name_len -lt 24 ] && max_name_len=24

# Print header
printf "%-${max_name_len}s | %12s | %12s | %s\n" "Component" "packages/ui" "webui" "Category"
# Print separator
printf '%s\n' "$(repeat_char '-' $max_name_len) | ------------ | ------------ | ------------"

for bname in "${overlap_list[@]}"; do
    ps="${pkg_sizes[$bname]}B"
    ws="${web_sizes[$bname]}B"
    printf "%-${max_name_len}s | %12s | %12s | %s\n" "$bname" "$ps" "$ws" "${categories[$bname]}"
done

# ── summary counts ─────────────────────────────────────────────────────────

count_identical=0
count_pkg_leads=0
count_web_leads=0
count_divergent=0

for bname in "${overlap_list[@]}"; do
    case "${categories[$bname]}" in
        identical)      count_identical=$((count_identical + 1)) ;;
        packages-leads) count_pkg_leads=$((count_pkg_leads + 1)) ;;
        webui-leads)    count_web_leads=$((count_web_leads + 1)) ;;
        divergent)      count_divergent=$((count_divergent + 1)) ;;
    esac
done

echo ""
echo "=== Summary ==="
echo ""
printf '  identical:      %d\n' "$count_identical"
printf '  packages-leads: %d\n' "$count_pkg_leads"
printf '  webui-leads:    %d\n' "$count_web_leads"
printf '  divergent:      %d\n' "$count_divergent"

# ── detail section ─────────────────────────────────────────────────────────

echo ""
echo "=== Details ==="
echo ""

for bname in "${overlap_list[@]}"; do
    printf '[%s] %s — %s\n' "${categories[$bname]}" "$bname" "${details[$bname]}"
done

echo ""
exit 0
