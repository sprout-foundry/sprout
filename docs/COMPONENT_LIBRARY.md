# Component Library Guide

This document describes the architecture and conventions for UI components in Sprout.

## Architecture Overview

Sprout uses a two-library architecture for UI components:

| Package | Role | Published as |
|---------|------|-------------|
| `packages/ui` | Canonical component library (primitives) | `@sprout/ui` |
| `webui/src/components` | Application-specific components (composites) | Internal only |

**Decision**: Option B from SP-039 — keep `packages/ui` as the canonical shared library. See [`roadmap/SP-039-DECISION.md`](../roadmap/SP-039-DECISION.md) for full rationale.

## Consumption Guide

For developers consuming `@sprout/ui` from an external project, see the **[Consumption Guide](CONSUMPTION_GUIDE.md)** — it covers installation, peer dependencies, component usage examples, TypeScript support, and more.

## Import Direction

```
webui/src/components/  ──────>  @sprout/ui  (packages/ui)
     ✓  webui imports from @sprout/ui
     ✗  @sprout/ui must NEVER import from webui
```

This is a one-way dependency:
- `webui` may import anything from `@sprout/ui`
- `@sprout/ui` must never import from `webui` or reference application-specific state

## Primitive vs Composite Rubric

### Primitives (belong in `packages/ui`)

A component is a **primitive** if ALL of the following are true:

- Accepts data via props only (no context hooks for app state)
- Emits events via callback props (`onClick`, `onChange`, etc.)
- Can render in isolation with mock data (no API calls, no auth context)
- Has no imports from application-specific modules (`../services/*`, `../contexts/*`, etc.)
- Is potentially reusable in a different React application

**Examples**: `Button`, `ContextMenu`, `FileTree`, `Sidebar`, `StatusBar`, `Terminal`, `CommandPalette`, `MessageBubble`, `NotificationItem`, `Skeleton`

### Composites (belong in `webui`)

A component is a **composite** if ANY of the following are true:

- Imports from application-specific services (`apiAdapter`, `billingService`, etc.)
- Uses application-specific context hooks (`useAuth`, `useSession`, `useSproutFetch`)
- Makes direct API calls or manages server state
- Wires primitives to application-specific business logic
- Would not function outside the Sprout webui application

**Examples**: `BillingPage`, `TasksPage`, `TeamPage`, `EditorWorkspace`, platform-specific page wrappers

### Edge Cases

| Situation | Decision | Example |
|-----------|----------|---------|
| Component uses a generic utility logger | **Primitive** | `CommandInput` uses `useLog` (package-internal) |
| Component wraps a primitive with app-specific wiring | **Composite** | `Notification` in webui (wires NotificationContext to @sprout/ui NotificationItem) |
| Component is a placeholder/stub | Keep in `packages/ui` for API stability | `GitSidebarPanel` placeholder |
| Component has both generic and specific logic | Split: generic in `packages/ui`, specific wrapper in `webui` | — |

## How to Add a New Component

### Decision Tree

```
Is it reusable outside Sprout?
├── YES → packages/ui/src/components/
│         1. Create Component.tsx + Component.css
│         2. Add .stories.tsx (Storybook) and .test.tsx
│         3. Export from packages/ui/src/index.ts
│         4. Import in webui: import { Component } from '@sprout/ui'
│
└── NO  → webui/src/components/
          1. Create Component.tsx + Component.css
          2. Import primitives from '@sprout/ui' as needed
          3. Do NOT re-export from webui back to packages/ui
```

### Adding a Primitive to `@sprout/ui`

1. Create `packages/ui/src/components/MyWidget.tsx` and `MyWidget.css`
2. Implement with props-driven interface (no app context hooks)
3. Add `MyWidget.stories.tsx` for Storybook
4. Add `MyWidget.test.tsx` with vitest
5. Add to barrel export in `packages/ui/src/index.ts`:
   ```ts
   export { default as MyWidget } from './components/MyWidget';
   export type { MyWidgetProps } from './components/MyWidget';
   ```
6. Build: `cd packages/ui && npm run build`
7. Use in webui: `import { MyWidget } from '@sprout/ui';`

### Adding a Composite to `webui`

1. Create `webui/src/components/MyPage.tsx`
2. Import primitives from `@sprout/ui` as needed
3. Wire to application context, API services, etc.
4. Import in parent component normally

## Current Component Inventory

| Category | Components (in `@sprout/ui`) |
|----------|------------------------------|
| Chat | ChatMessageContextMenu, ChatPanel, MessageBubble, MessageContent, MessageSegments, QueuedMessagesPanel, SelectionActionBar |
| Editor | Editor |
| File Management | FileTree |
| Git | GitFileSection, GitPanel |
| Layout | Sidebar, StatusBar |
| Notification | NotificationItem, NotificationStack |
| Terminal | Terminal, TerminalPane, TerminalTabBar |
| UI Primitives | CommandInput, CommandPalette, ContextMenu, LiveLog, Skeleton, ThemedDialog |

| Category | Components (in `webui`) |
|----------|-------------------------|
| Platform Pages | BillingPage, TasksPage, TeamPage, AdminBillingPage |
| Application | EditorWorkspace, Notification, ContextMenu extensions |

## CI Enforcement

- `scripts/ui-consolidation-diff.sh` reports component overlaps
- Duplicate component names between `packages/ui` and `webui` should be eliminated
- ESLint `no-restricted-paths` rule (planned) will enforce import direction

## Related Documentation

- [`roadmap/SP-039-DECISION.md`](../roadmap/SP-039-DECISION.md) — Architecture decision
- [`roadmap/SP-039-component-categorization.md`](../roadmap/SP-039-component-categorization.md) — Full categorization
- [`CONTRIBUTING.md`](../CONTRIBUTING.md) — Contribution guidelines
