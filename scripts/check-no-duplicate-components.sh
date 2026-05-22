#!/usr/bin/env bash
# check-no-duplicate-components.sh
# Fails if any component file (.tsx, .css) exists in BOTH packages/ui and webui.
# Part of CI enforcement for SP-039 (UI consolidation).
set -euo pipefail

# Known exemptions: components intentionally duplicated during migration.
# Remove entries from this list as duplicates are resolved.
ALLOWED_DUPLICATES=(
    # SP-039-3a: Notification dedup (partially done - NotificationItem resolved)
    "Notification.css"
    "NotificationStack.tsx"
    # SP-039-3c: Sidebar/StatusBar dedup (pending)
    "Sidebar.tsx"
    "Sidebar.css"
    "StatusBar.tsx"
    "StatusBar.css"
    # SP-039-3d: CommandPalette/CommandInput dedup (pending)
    "CommandPalette.tsx"
    "CommandPalette.css"
    "CommandInput.tsx"
    "CommandInput.css"
    # Git components with packages/ui placeholder
    "GitSidebarPanel.tsx"
    # Chat components (pending SP-039-4d)
    "ChatMessageContextMenu.tsx"
    "MessageBubble.tsx"
    "MessageContent.tsx"
    "MessageSegments.tsx"
    "QueuedMessagesPanel.tsx"
    "SelectionActionBar.tsx"
    "LiveLog.tsx"
    # Other primitives with webui copies (pending Phase 3)
    "FileTree.tsx"
    "FileTree.css"
    "Terminal.tsx"
    "Terminal.css"
    "TerminalPane.tsx"
    "TerminalTabBar.tsx"
    "ThemedDialog.tsx"
    # Test files often exist in both during migration
    "ChatMessageContextMenu.test.tsx"
    "CommandPalette.test.tsx"
    "ContextMenu.test.tsx"
    "FileTree.test.tsx"
    "GitFileSection.test.tsx"
    "GitSidebarPanel.test.tsx"
    "QueuedMessagesPanel.test.tsx"
    "Sidebar.test.tsx"
    "StatusBar.test.tsx"
    "Terminal.test.tsx"
    "TerminalPane.test.tsx"
    "TerminalTabBar.test.tsx"
    # Component exist in both but GitFileSection/GitPanel are separate issues
    "GitFileSection.tsx"
    "GitPanel.tsx"
)

ui_dir="packages/ui/src/components"
webui_dir="webui/src/components"

if [ ! -d "$ui_dir" ] || [ ! -d "$webui_dir" ]; then
    echo "ERROR: Component directories not found"
    echo "  $ui_dir"
    echo "  $webui_dir"
    exit 1
fi

# Get sorted basenames of .tsx and .css files from each directory
ui_files=$(find "$ui_dir" -maxdepth 1 \( -name "*.tsx" -o -name "*.css" \) -printf '%f\n' | sort)
webui_files=$(find "$webui_dir" -maxdepth 1 \( -name "*.tsx" -o -name "*.css" \) -printf '%f\n' | sort)

# Find overlapping basenames
duplicates=$(comm -12 <(echo "$ui_files") <(echo "$webui_files"))

if [ -z "$duplicates" ]; then
    echo "✅ No duplicate component files between packages/ui and webui"
    exit 0
fi

# Filter out allowed duplicates
violations=""
while IFS= read -r dup; do
    is_allowed=false
    for allowed in "${ALLOWED_DUPLICATES[@]}"; do
        if [ "$dup" = "$allowed" ]; then
            is_allowed=true
            break
        fi
    done
    if [ "$is_allowed" = false ]; then
        violations="$violations\n$dup"
    fi
done <<< "$duplicates"

# Report results
echo "=== Duplicate Component Check ==="
echo ""
echo "Allowed duplicates (pending migration):"
while IFS= read -r dup; do
    for allowed in "${ALLOWED_DUPLICATES[@]}"; do
        if [ "$dup" = "$allowed" ]; then
            echo "  ⚠️  $dup (exempted)"
        fi
    done
done <<< "$duplicates"

if [ -n "$violations" ]; then
    echo ""
    echo "❌ UNEXCEPTED duplicate component files:"
    echo -e "$violations"
    echo ""
    echo "These files exist in both packages/ui and webui but are not in the exemption list."
    echo "Either resolve the duplicate or add to ALLOWED_DUPLICATES in this script."
    exit 1
fi

echo ""
echo "✅ All duplicates are known/exempted. No new violations."
exit 0
