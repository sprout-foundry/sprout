# SP-039 — UI Consolidation Decision

**Date:** May 21, 2025
**Status:** ✅ Decision made
**Related:** SP-039 series — UI component consolidation

## Decision

**Option B — Keep `packages/ui` as the canonical component library. `webui` imports from it.**

## Rationale

### Why Option B

1. **`packages/ui` has mature testing infrastructure**: Every component has Storybook stories (.stories.tsx) and vitest unit tests. Moving this to webui would mean losing the standalone component library benefits.

2. **`packages/ui` is designed as a shared library**: It's published as `@sprout/ui` with a proper `index.ts` barrel export, TypeScript types, and is designed for reuse across consumers.

3. **Avoiding a massive move diff**: Moving ~50 components from packages/ui to webui would create enormous git diffs, lose history, and require touching every import across the entire codebase.

4. **Avoiding circular dependencies**: Keeping the shared library separate ensures clean import direction: `webui → @sprout/ui` (never reverse).

### Architecture

**Primitives stay in `packages/ui`** (reusable, no domain types):
- Visual building blocks: Button, Modal, Dropdown, Skeleton, etc.
- UI widgets: ContextMenu, NotificationItem, Sidebar, StatusBar, etc.
- Chat primitives: MessageBubble, MessageContent, MessageSegments
- Terminal primitives: Terminal, TerminalPane, TerminalTabBar
- Editor primitive: Editor
- FileTree, CommandInput, CommandPalette
- Any component that can be used without Sprout-specific domain types

**Composites move to `webui`** (domain-specific, wire to app state):
- ChatPanel → Already domain-specific, wires chat state
- BillingPage, TasksPage, TeamPage → Platform domain pages
- AppContent, ContextPanel, EditorPane, EditorTabs → Application layout
- Settings tabs, location switcher, platform pages → All webui-specific

### Import Direction

```
webui/src/components/  ──────>  @sprout/ui  (packages/ui)
     ✓  webui imports from @sprout/ui
     ✗  @sprout/ui must NEVER import from webui
```

### Current State

Approximately 30 components exist in both locations. Over time, duplicates in webui should be replaced with imports from `@sprout/ui`, reducing the total surface area.

### Next Steps

1. Audit all overlapping components (SP-039-1b)
2. Categorize primitives vs composites (SP-039-1c)
3. Plan migration of remaining duplicates to use @sprout/ui imports
4. Remove duplicate code from webui where appropriate

## Alternatives Considered

**Option A — Move everything to webui**: Rejected. Loses shared library benefits, massive diff, loses Storybook.

**Option C — Merge into single monolithic directory**: Rejected. Blurs the boundary between reusable primitives and domain-specific composites.
