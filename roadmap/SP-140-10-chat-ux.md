# SP-140-10 — Design-Mode Chat UX: Placement, Pinning, Visibility, Inline Tool Details

> **Status (2026-09-22):** Draft — in flight (automation run).
> Parent: [SP-140](./SP-140-design-workspace.md). Depends on SP-140-6
> (loop surface — §6f side-column agent panel), SP-140-7 (co-editing).
> Fixes the four design-mode dogfood gripes of 2026-09-22. Item 10d
> changes the **shared** chat mechanism, so Code mode is covered too,
> with regression gates.

## Provenance (gripes, lightly paraphrased)

1. **Placement.** The agent chat docked at the far right doesn't make
   sense for RTL readers. We think in a left-to-right flow (inverted for
   RTL languages) — the column should sit at the *end* of the reading
   flow in either direction.
2. **Pinning.** The chat auto-restores the last chat context, which is
   likely *not* a design chat. Different modes need different chat
   pinning.
3. **Visibility.** The chat started by not filling the full height, and
   content was never visible even though it was doing things.
4. **Tool badges.** Clicking a tool badge doesn't work in design mode.
   Change the mechanism so the tool-details sidebar is no longer needed:
   a selected tool-call badge shows its details inline in the chat, with
   contextually more detail, and hides on re-press or loss of focus.

## Current-state pointers (recon 2026-09-22)

- **Design-mode chat** — `components/design/DesignAgentPanel.tsx` mounts
  the same `Chat` (`components/ChatView.tsx`) the Code shell mounts,
  inside `DesignSideColumn`'s Agent tab (the single right column,
  SP-140-6 §6f rework). Design mode has *no* context panel — "those are
  the Code surface's chrome" (`workspaces/DesignShell.tsx`).
- **Tool-pill click** — `ChatProps.onToolPillClick` (packages/ui
  ChatProps, `packages/ui/src/types/chat.ts:211`) →
  `AppContent.handleToolPillClick` (`components/AppContent.tsx:799`) →
  `contextPanelRef.current?.highlightTool(toolId)` — the Code-mode
  ContextPanel sidebar. Design mode never mounts the ContextPanel, so
  the click is a silent no-op (gripe 4).
- **Conversation restore** — `hooks/useAppInitialization.ts` (~L330–400)
  auto-restores the most recent non-empty session at boot, mode-blind
  (gripe 2). The *mode* persists per instance + UI context
  (`WORKSPACE_MODE_STORAGE_KEY`, `constants/app.ts:13`;
  `workspaces/useWorkspaceMode.ts`) — there is no per-mode session
  pinning.
- **Height chain** — `.main-content.design-shell` → `.design-shell-body`
  → `.design-surface` → `.design-view` (`height: 100%`) →
  `.design-view-body` → `.design-side-column` → `.design-side-body` /
  `.design-agent-panel` / `.design-agent-chat` (DesignView.css
  ~1274–1380). Any auto-height ancestor collapses the 100%; and a
  zero-height transcript container starves react-virtuoso's metrics,
  `isAtBottom` stays false, and `followOutput` never fires — the two
  halves of gripe 3 are likely one bug.
- **RTL** — the side column is placed with physical properties
  (`border-left`; the mobile fixed overlay `inset: 0 0 0 auto`,
  DesignView.css ~1298–1386).

## Progress (gate scans this list)

- [ ] SP140-10a — RTL-aware side column placement
- [ ] SP140-10b — Full-height agent panel + live visibility while working
- [ ] SP140-10c — Per-mode conversation pinning
- [ ] SP140-10d — Inline tool details (retire the sidebar dependency)
- [ ] SP140-10e — Verify, dogfood, and mark shipped

## Items

### 10a. RTL-aware side column placement

The design side column must sit at the **end** of the reading flow: far
right under LTR (today's behavior — no visual change), far left under
`dir="rtl"`.

Mechanism:

- `DesignView.css`: `.design-side-column`'s `border-left` →
  `border-inline-start`; the mobile fixed overlay's
  `inset: 0 0 0 auto` → `inset-block: 0; inset-inline-end: 0` (width
  unchanged). Audit the side-column / agent-panel rules for any other
  physical placement (margins, offsets) and switch to logical
  properties.
- Scope note: the webui is otherwise not RTL-ready (no `dir` handling
  anywhere else). This item makes only the design side column
  direction-aware; no i18n work.

Acceptance:

- LTR renders unchanged (existing `DesignSideColumn.test.tsx` /
  `DesignView` tests stay green, no visual regression).
- A regression test renders the side column under `dir="rtl"` and
  asserts the border and the mobile overlay land on the inline-end side
  (jsdom computed style, or a CSS-contract assertion that the
  design-side-column rules carry no physical `border-left` /
  `inset: … auto`).
- Files: `webui/src/components/design/DesignView.css`,
  `DesignSideColumn.test.tsx` (or a new CSS-contract test).

### 10b. Full-height agent panel + live visibility while working

Two failures observed at once: (1) the Agent tab did not fill the
column height on first render — a gap below the input, or the input
floating mid-panel; (2) while the agent was working, its new content was
off-screen ("content was never visible though it was doing things").

Mechanism:

- Audit the height chain (pointer above) and eliminate every
  auto-height ancestor between `.main-content.design-shell` and
  `.design-agent-chat`: each link needs `flex: 1` + `min-height: 0`
  under a definite parent height; `.design-view`'s `height: 100%`
  requires a definite parent.
- The zero-height container also starves react-virtuoso — fix the
  height so auto-follow works: while at the bottom, streaming messages
  must keep the latest message in view (the existing
  `followOutput={(isAtBottom) => isAtBottom ? 'smooth' : false}` at
  `ChatView.tsx:490`).
- The existing jump-to-latest button (`.scroll-to-bottom-btn`) must
  appear while scrolled up during an active run.

Acceptance:

- Layout test: after mount — and after a Details→Agent tab flip —
  `.design-agent-chat`'s bounding height equals the column's height (no
  gap below the input).
- Streaming test: with the user at the bottom, appending messages keeps
  the last message in view; scrolled up, the jump button appears and
  clicking it lands at the last message.
- Files: `DesignView.css`, `ChatView.tsx` (only if the follow fix
  needs it), targeted vitest tests.

### 10c. Per-mode conversation pinning

Switching to Design mode must not resurrect a Code-mode conversation.

Mechanism:

- New storage key `CHAT_MODE_PIN_STORAGE_KEY = 'sprout:webui:chatModePin:v1'`
  in `constants/app.ts` (next to the workspace-mode key). Value:
  `JSON { code?: string; design?: string }` (session/chat ids), scoped
  per instance PID + UI context exactly like `workspaceModeStorageKey()`.
- New `useChatModePinning` hook (or a `useWorkspaceMode` extension):
  on mode switch, restore that mode's pinned session through the
  existing session-switch path (the same `sprout:session-restored` /
  `switchChatSession` flow AppContent already consumes). Record the
  mode's pin when a session becomes active in that mode (message send
  or explicit switch).
  - **Design mode with no pin → start a fresh conversation** (never
    fall back to a code session).
  - Code mode with no pin → today's behavior (most recent non-empty).
- Boot: `useAppInitialization`'s auto-restore respects the persisted
  mode — persisted mode design means use the design pin (or a fresh
  session) instead of the cross-mode "most recent non-empty" fallback.
- Persistence is best-effort: a read or write throw must not break the
  shell (same convention as workspace-mode persistence).

Acceptance:

- Switching code→design→code restores each mode's own chat.
- Design mode with no pin shows a fresh, empty chat.
- Boot with persisted mode design restores the design pin.
- Storage failure degrades to defaults (no crash, no stale
  cross-mode restore).
- Unit tests: the hook (pin write/read, the fresh-chat rule) and the
  init-path branch; targeted vitest.
- Files: `constants/app.ts`, `workspaces/useWorkspaceMode.ts` (or a new
  hook), `hooks/useAppInitialization.ts`, `components/AppContent.tsx`,
  tests.

### 10d. Inline tool details; retire the sidebar dependency

Tool badges must work in **every** mode, and the tool-details sidebar
must stop being the mechanism.

Mechanism:

- New `ToolDetailInline` (`webui/src/components/chat/ToolDetailInline.tsx`):
  renders a `ToolExecution`'s detail — args, status, output, duration,
  subagent prompt — reusing `contextPanel/ToolCard.tsx`'s rendering
  (extract a shared body if the two diverge). Renders inside the
  message (`MessageItem` / `MessageSegments`), directly under the pill
  row.
- ChatView state: a single `activeToolDetail` per transcript. Clicking
  a pill toggles it; clicking another pill swaps the expansion.
  Collapse triggers: re-press, outside blur (focusout whose
  relatedTarget is outside the expanded block), or `Escape`. At most
  one expansion open across the transcript at a time.
- Remove the `onToolPillClick` → `contextPanelRef.highlightTool`
  wiring: drop `onToolPillClick` from `@sprout/ui` ChatProps +
  ChatPanel (`packages/ui/src/types/chat.ts`,
  `packages/ui/src/components/ChatPanel.tsx`) and from AppContent's
  chatProps (`handleToolPillClick`). `DesignAgentPanel` inherits the
  inline behavior for free (same Chat component).
- The ContextPanel itself is **not** retired (its Activity/Subagents
  tabs stay a Code-mode feature); only its tool-pill highlight path is
  retired. Whether the panel itself goes is a separate decision.
- A11y: the pill is a toggle button (`aria-expanded`, `aria-controls`
  pointing at the detail block); the block is a labeled region; Escape
  closes and returns focus to the pill.

Acceptance:

- In Design mode, clicking a tool pill shows the detail inline (today:
  a no-op).
- Re-press / outside blur / Escape collapse it; at most one open at a
  time.
- Code-mode regression: the pill no longer scrolls the ContextPanel
  (update or remove the corresponding tests).
- `packages/ui` builds and its tests pass (shared package —
  `cd packages/ui && npx vitest run <touched files>`, `npm run
  type-check` when types change).
- Files: `packages/ui/src/types/chat.ts`,
  `packages/ui/src/components/ChatPanel.tsx`,
  `webui/src/components/chat/MessageItem.tsx`,
  `webui/src/components/chat/ToolDetailInline.tsx` (new),
  `webui/src/components/contextPanel/ToolCard.tsx`,
  `webui/src/components/AppContent.tsx`, `ChatView.tsx` (state +
  wiring), tests.

### 10e. Verify, dogfood, and mark shipped

- `cd feat-design-workspace && make build` (full pipeline) green.
- Targeted vitest runs for every test file touched by 10a–10d (bounded
  runs; never chained with a go build).
- `cd webui && npx prettier --check` and `make lint` (repo gates).
- Dogfood checklist — do the mechanical parts; mark the manual-browser
  lines *awaiting manual verification* in this spec rather than
  claiming them:
  1. `dir=rtl`: side column, border, and mobile overlay on the left.
  2. Agent tab full-height on first render; latest message visible
     during an active run; jump button when scrolled up.
  3. Mode switches restore per-mode chats; design-with-no-pin → fresh
     chat.
  4. Tool pill expands inline in design mode; code mode no longer
     drives the sidebar.
- Flip this spec's Status to Shipped; add the `roadmap/00-INDEX.md`
  row and the umbrella phase-map row (both added at spec-creation time
  with status Draft — update them, don't re-add). Commit with this
  item.
