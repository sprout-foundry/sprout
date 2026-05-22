#!/usr/bin/env bash
# check-storybook-coverage.sh
# Verifies every primitive component in packages/ui has a matching .stories.tsx file.
# Part of SP-039-5c CI enforcement.
set -euo pipefail

ui_dir="packages/ui/src/components"

if [ ! -d "$ui_dir" ]; then
    echo "ERROR: $ui_dir not found"
    exit 1
fi

missing=0
total=0

for f in "$ui_dir"/*.tsx; do
    name=$(basename "$f" .tsx)
    # Skip test and story files
    case "$name" in *.test) continue ;; *.stories) continue ;; esac
    total=$((total + 1))
    if [ ! -f "$ui_dir/${name}.stories.tsx" ]; then
        echo "MISSING: $name has no .stories.tsx"
        missing=$((missing + 1))
    fi
done

echo ""
echo "Storybook coverage: $((total - missing))/$total components have stories"

if [ "$missing" -gt 0 ]; then
    echo ""
    echo "❌ $missing component(s) missing Storybook stories"
    exit 1
fi

echo "✅ All primitives have Storybook stories"
exit 0
