# SP-039-1c: Component Categorization

**Date**: May 21, 2025
**Related**: SP-039 — UI Package Consolidation

## Primitives (stay in `packages/ui`)

Reusable components with no domain coupling. Accept props, emit callbacks. Can be used
in any React app without Sprout-specific infrastructure.

| Component | Category | Notes |
|-----------|----------|-------|
| ChatMessageContextMenu | Primitive | Menu actions for chat messages, props-driven |
| ChatPanel | Primitive | Chat layout container, 5 props interfaces, no domain imports |
| CommandInput | Primitive | Text input with autocomplete |
| CommandPalette | Primitive | Search/command palette overlay |
| ContextMenu | Primitive | Generic right-click/long-press context menu |
| Editor | Primitive | Code editor (Monaco/CodeMirror wrapper) |
| FileTree | Primitive | File/directory tree view |
| GitFileSection | Primitive | Git status file list section |
| GitPanel | Primitive | Git status operations panel |
| GitSidebarPanel | Primitive | Sidebar panel for git info |
| LiveLog | Primitive | Real-time log streaming display |
| MessageBubble | Primitive | Chat message bubble |
| MessageContent | Primitive | Rich message content renderer |
| MessageSegments | Primitive | Message segment parsing/display |
| NotificationItem | Primitive | Single notification display |
| NotificationStack | Primitive | Stack of notifications |
| QueuedMessagesPanel | Primitive | Panel showing queued/pending messages |
| SelectionActionBar | Primitive | Floating action bar on text selection |
| Sidebar | Primitive | Main sidebar layout container |
| Skeleton | Primitive | Loading skeleton placeholder |
| StatusBar | Primitive | Bottom status bar |
| Terminal | Primitive | Terminal emulator component |
| TerminalPane | Primitive | Single terminal pane |
| TerminalTabBar | Primitive | Tab bar for multiple terminals |
| ThemedDialog | Primitive | Themed modal/dialog wrapper |

## Composites (candidates for `webui/`)

Domain-specific components that import from app-specific packages (`@sprout/events`,
`@sprout/api`, `useAuth`, `useSession`, etc.) or wire directly to application state.

| Component | Domain Coupling | Notes |
|-----------|----------------|-------|
| BillingPage | `@sprout/events`, billing context | Platform billing page, imports app-specific events |
| TasksPage | Multiple hooks/API imports | Platform tasks page, domain state management |
| TeamPage | Multiple hooks/API imports | Platform team management page |

## Audit Notes

- **28 non-test, non-story component .tsx files** in `packages/ui/src/components/`
- **25 are primitives** — reusable, props-driven, no domain imports
- **3 are composites** — domain-coupled, need migration to `webui/src/components/` (SP-039-2a)
- Several `.stories.tsx` files import mock data that references domain types (e.g., `GitFileSection.stories.tsx`), but the components themselves remain primitive

## Next Steps

Per SP-039 Phase 2:
1. Move `BillingPage`, `TasksPage`, `TeamPage` to `webui/src/components/` (SP-039-2a)
2. Update all imports across both packages
3. Audit for any other domain-coupled components hiding in primitives (SP-039-2b)
