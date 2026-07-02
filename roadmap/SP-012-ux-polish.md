# SP-012: UX Polish

**Status:** ⚠️ ~90% Shipped — Notification center, reduced-motion media-query wrapping, inline tool badges, configurable MAX_PANES, sidebar persistence, mobile responsive breakpoints, loading skeletons, and panel animations shipped (2026-06-23 → 2026-06-30); remaining: ARIA gaps in FileTree (role="treeitem"/aria-expanded) and ChatPanel (role="log"), global :focus-visible styles, and `notificationBus.markAllRead()` method  
**Depends on:** SP-003 (Webui & Frontend Architecture), SP-010 (Editor Modernization)  
**Priority:** Medium  
**Effort Estimate:** ~2-3 weeks (2 phases)

## Problem

The webui provides a good foundation with theme support, keyboard shortcuts, and panel layout. However, several quality-of-life gaps make the application feel less polished than users expect from a modern IDE:

1. **No notification center** — toast notifications disappear; no way to review history
2. **No reduced-motion support** — animations play regardless of OS accessibility settings
3. **Mobile experience** — panels crowd on small screens with no responsive adaptation
4. **Missing accessibility** — incomplete ARIA coverage, no focus indicators in some areas
5. **Editor pane limit** — artificial 3-pane cap frustrates power users
6. **Sidebar state not persisted** — collapses to default on every reload

## Current State

### What Works

| Area | Implementation | Quality |
|------|---------------|---------|
| Theme system | VS Code theme import/export, CSS variables | Good |
| Command palette | Fuzzy search, commands/files/symbols | Good |
| Panel resizing | Drag handles with visual feedback | Good |
| Chat sessions | Multiple independent chats | Good |
| Context panel width | Persisted via localStorage | Good |
| Window/tab title | Shows active file + workspace | Good |
| Error boundary | Catches render crashes gracefully | Good |
| Onboarding | Provider setup wizard for new users | Good |
| Menu bar | File, Edit, View, Help with keyboard shortcuts | Good |
| Quick prompts | Slash command suggestions in chat input | Good |
| Worktree support | Multiple worktrees with independent chats | Good |

### Layout Persistence

Already partially implemented:
- **Panel widths**: `usePanelWidth.ts` persists context panel width to localStorage
- **Editor layout**: `useLayoutPersistence.ts` saves open tabs, pane splits, and active buffer
- **Sidebar width**: NOT persisted
- **Sidebar collapsed state**: NOT persisted

### What's Missing

| Feature | Impact | Effort |
|---------|--------|--------|
| Notification center (history) | High | Medium |
| Reduced-motion CSS | Medium | Low |
| Mobile responsive layout | Medium | Medium |
| ARIA completeness audit | Medium | Medium |
| Editor 3-pane limit removal | Medium | Low |
| Sidebar state persistence | Low | Low |
| Inline tool badges in chat | High | Low |
| Loading skeletons | Low | Low |
| Focus indicators | Low | Low |
| Panel collapse animation | Low | Low |

## Proposed Solution

### U0: Inline Tool Call Badges

Every tool call rendered in assistant messages appears as a small inline badge that links to the tool execution details in the contextual sidebar.

**Requirements:**
- Badges render inline (not block-level) so they flow naturally within assistant text
- 10px horizontal gap between consecutive badges
- Smaller text than body (11px or less)
- Tool name truncated to max 2 words (e.g., "read file" not "read_file")
- Clicking a badge calls `onToolRefClick(toolId)` to open the tool in the context sidebar
- Badges follow the active theme (CSS variables for bg, border, text)
- Completed tools show a compact `[tool_name]` footnote; in-progress tools show the pill with icon
- The ExternalLink icon on hover provides visual feedback that the badge is clickable

**CSS:**
```css
.segment-tool-call {
  display: inline-flex;     /* inline, not block */
  padding: 2px 8px;
  font-size: 11px;
  margin-right: 10px;       /* 10px gap between badges */
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  border: 1px solid var(--border-default);
  color: var(--text-secondary);
}
```

### Phase 1: Notifications & Accessibility (Week 1-2)

#### U1.1: Notification Center

Add a notification history panel accessible from the sidebar or a bell icon in the status bar:

```typescript
// Notification center features:
// - Bell icon in StatusBar with unread count badge
// - Click opens notification history panel
// - Each notification: timestamp, type (error/warning/info/success), title, message
// - Actions: dismiss individual, dismiss all, copy message
// - Persistent: notifications stored in notificationBus history (already has getNotificationHistory())
// - Auto-mark read when panel opens
// - Max history: 100 entries (already enforced by notificationBus)
```

The `notificationBus` already has `getNotificationHistory()` and `_resetForTesting()` — the backend is ready. Need a UI component to display it.

**New files:**
- `webui/src/components/NotificationCenter.tsx` — history panel
- `webui/src/components/NotificationCenter.css`

**Modified files:**
- `webui/src/components/StatusBar.tsx` — add bell icon with badge
- `packages/ui/src/services/notificationBus.ts` — add `markAllRead()` method

#### U1.2: Reduced-Motion Support

CSS `prefers-reduced-motion` media query wrapper:

```css
/* webui/src/index.css additions */
@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
    scroll-behavior: auto !important;
  }
}
```

Low effort, high accessibility impact. Wrap existing animations/transitions.

#### U1.3: ARIA Audit & Fixes

Targeted fixes for the most visible gaps:

| Component | Issue | Fix |
|-----------|-------|-----|
| `FileTree.tsx` | Tree items missing `role="treeitem"` and `aria-expanded` | Add ARIA tree pattern |
| `TerminalPane.tsx` | No `aria-label` on terminal container | Add descriptive label |
| `CommandPalette.tsx` | Results list not announced to screen readers | Add `aria-live` region |
| `ChatPanel.tsx` | Messages not in a landmark region | Add `role="log"` and `aria-label` |
| `EditorTabs.tsx` | Tab close buttons not labeled | Add `aria-label="Close {filename}"` |
| `Sidebar.tsx` | Navigation sections not in landmarks | Add `role="navigation"` |

#### U1.4: Focus Indicators

Ensure all interactive elements have visible focus rings:

```css
/* Global focus styles */
:focus-visible {
  outline: 2px solid var(--focus-ring-color, #4d8fcc);
  outline-offset: 2px;
}

/* Remove default outline for mouse users */
:focus:not(:focus-visible) {
  outline: none;
}
```

### Phase 2: Layout & Responsiveness (Week 2-3)

#### U2.1: Remove 3-Pane Editor Limit

Current code in `EditorManagerContext.tsx` (line 1064):
```typescript
if (panes.length >= 3) return null; // Max 3 panes
```

Change to configurable limit (default 6):
- Update `splitPane` to accept up to `MAX_PANES` (configurable)
- Add overflow handling for extreme splits (minimum pane width enforcement)
- Persist pane count preference

#### U2.2: Sidebar State Persistence

```typescript
// webui/src/hooks/useSidebarState.ts (already exists)
// Ensure these are persisted to localStorage:
// - isCollapsed (boolean)
// - activeTab (string: 'files' | 'git' | 'search' | etc.)
// - width (number, when expanded)
```

Verify all sidebar state survives page reload.

#### U2.3: Mobile Responsive Layout

```css
/* webui/src/index.css / App.css additions */

/* Tablet (768-1024px) */
@media (max-width: 1024px) {
  /* Collapse sidebar to icons-only mode */
  .sidebar { width: 48px; }
  .sidebar .tab-label { display: none; }
  
  /* Stack editor and chat vertically on narrow screens */
  .workspace-pane { flex-direction: column; }
}

/* Mobile (< 768px) */
@media (max-width: 768px) {
  /* Single-panel view: show only active panel */
  .sidebar { position: fixed; z-index: 100; }
  .editor-pane { flex: 1; }
  .context-panel { position: fixed; bottom: 0; height: 40vh; }
  
  /* Tab bar scrolls horizontally */
  .editor-tabs { overflow-x: auto; }
  
  /* Terminal full-screen overlay */
  .terminal-container { position: fixed; inset: 0; z-index: 50; }
}
```

Also add touch gestures:
- Swipe left/right to toggle sidebar on mobile
- Pull down on terminal for scrollback search

#### U2.4: Loading Skeletons

Replace loading spinners with skeleton screens for perceived performance:

```typescript
// Skeleton components for:
// - File tree loading: placeholder rows matching typical tree structure
// - Chat history loading: placeholder message bubbles
// - Editor loading: placeholder code lines with syntax-colored blocks
// - Settings panel loading: placeholder form fields
```

#### U2.5: Panel Collapse Animation

Smooth transitions when panels resize or collapse:

```css
.panel-transition {
  transition: width 200ms ease-out, opacity 150ms ease-out;
}
```

Add `transition` to sidebar collapse, context panel resize, and terminal toggle.

## Implementation Phases

### Phase 1: Notifications & Accessibility (Week 1-2)

**New files:**
- `webui/src/components/NotificationCenter.tsx`
- `webui/src/components/NotificationCenter.css`

**Modified files:**
- `webui/src/components/StatusBar.tsx` — bell icon + unread badge
- `webui/src/index.css` — reduced-motion, focus indicators
- `packages/ui/src/services/notificationBus.ts` — add markAllRead()
- `webui/src/components/FileTree.tsx` — ARIA tree pattern
- `packages/ui/src/components/CommandPalette.tsx` — aria-live
- `packages/ui/src/components/ChatPanel.tsx` — role="log"

### Phase 2: Layout & Responsiveness (Week 2-3)

**Modified files:**
- `webui/src/contexts/EditorManagerContext.tsx` — remove 3-pane limit
- `webui/src/hooks/useSidebarState.ts` — ensure full persistence
- `webui/src/index.css` — responsive breakpoints
- `webui/src/App.css` — mobile layout
- `webui/src/components/AppContent.tsx` — responsive panel logic

## Success Criteria

| Metric | Target |
|--------|--------|
| Notification center | History panel with dismiss/copy actions |
| Reduced motion | All animations respect `prefers-reduced-motion` |
| ARIA | All interactive elements have proper roles and labels |
| Focus indicators | Visible on all keyboard-focusable elements |
| Editor panes | Up to 6 simultaneous panes |
| Sidebar | State persists across reload |
| Mobile | Functional on 768px+ viewport |
| Build | `make build-all` passes |

## Files Reference

| File | Action |
|------|--------|
| `webui/src/components/NotificationCenter.tsx` | Create: notification history panel |
| `webui/src/components/StatusBar.tsx` | Modify: add notification bell |
| `webui/src/index.css` | Modify: reduced-motion, focus, responsive |
| `webui/src/contexts/EditorManagerContext.tsx` | Modify: remove 3-pane limit |
| `webui/src/hooks/useSidebarState.ts` | Modify: full state persistence |
| `webui/src/components/AppContent.tsx` | Modify: responsive layout |
| `packages/ui/src/services/notificationBus.ts` | Modify: add markAllRead |
