# SP-139: Context Panel De-Necessitation

## Problem

The right-hand context panel grew into a six-tab alternate UI (Tools,
Subagents, Tasks, Status, Changes, Sessions). Every feature that had nowhere
in-flow to live landed there, which trained users to *watch the panel*
instead of the conversation — the opposite of the product's chat-first
direction. Each tab also duplicated a surface that already existed elsewhere
(todo list, costs page, status bar).

Goal: displace panel content into in-flow surfaces (the chat transcript, the
chat footer, the status bar) until the panel is an **opt-in power-user
column** — still useful for archaeology and bulk operations, never required
for ordinary monitoring or review.

## Guiding rule

Before adding anything to the panel, the question is: can the chat surface
carry it? A tab earns its place only by providing something the in-flow
surfaces structurally cannot (cross-turn aggregation, bulk ops, arbitrary
history access).

## Status: ✅ All Phases Shipped (2026-09-14)

### Phase 1: Consolidation 6 → 3 ✅ (2026-09-10)

- `0b333b5d3` — Activity tab merges Tools + Subagents (both were filtered
  views of the same `toolExecutions` array). Tasks tab removed
  (`InlineTodoSummary` renders the full todo panel inline in chat). Status
  tab removed (costs → Costs page, counts → Activity, context-window
  readout → `ChatStatusBarItems`). Net −1,169 lines.
- `fb0306490` — dead-code drop after review (orphaned formatters,
  `fileEdits` prop, dead CustomEvent).
- `339cd1100` — desktop panel column stays mounted with an idle rail when a
  file buffer is focused; layout stops reflowing between chat and files.

In-flow surfaces that already cover live monitoring (no panel needed):

| Need | In-flow surface |
|------|-----------------|
| Running tools at a glance | `ToolTimelineBar` strip above the chat input |
| Tool detail / status | clickable tool pills inline in transcripts (`MessageSegments` + `toolRefs`) |
| Subagent progress | `SubagentActivityFeed` cards inline |
| Todos | `InlineTodoSummary` in the chat surface |
| Cost / context window / model | `ChatStatusBarItems` segments in the status bar |

### Phase 2: Changes — in-flow agent-change summaries ✅ (2026-09-14, `af8cdb8f5`)

`TurnChangesStrip` renders under the last completed turn: collapsed
"N files changed this turn", expandable to per-file rows with Review
(opens a diff buffer seeded from /api/changes/diff) and Revert
(`revert(since=firstServerTs−1s)`; labeled honestly — exact for the
latest turn, "this turn onward" for older ones; no accept action, that
belongs to LLM edit approval). Data: `file_changed` events carry a
server-side `ts`, `fileEdits` entries carry `queryId` correlation.
Performance contract: no polling, no new event types, no new store
slices; memo'd component over the 50-capped fileEdits array.

Acceptance met: "what did this turn change?" is answerable in the chat
flow. The panel tab remains the cross-revision timeline / rollback
surface — no overlap introduced.

### Phase 3: Sessions relocated to chat-header history switcher ✅ (2026-09-14)

Decision: relocate. Prerequisite fix first — `POST /api/sessions/restore`
ignored chat identity and wrote the restored state into the client's
ACTIVE chat, so a background pane restoring its own history would clobber
the focused chat. The handler now accepts `chat_id` (empty = active chat,
backward compatible), writes via `setAgentStateForClientChat`, imports
into the target chat's agent, scopes config overrides and the
connection_status event. Pinned by TestRestoreSessionScopesToRequestedChat.

The switcher: a permanent header row above the transcript (outside
Virtuoso so the popover doesn't scroll) with a History trigger.
Popover = debounced search (existing /api/sessions/search) over the
recent-sessions list (existing /api/sessions), restore with confirm
(chat-scoped), and export-all in the footer. Closed popover renders one
button; all fetches are open/typing-triggered — same performance
contract as the Phase 2 strip.

Panel drops to Activity + Changes (2 tabs): SessionsTab,
useSessionManager, sessionSearchHelpers and 10 orphaned testids deleted
(−5 files, ~1000 lines). Tab persistence migrates legacy 'sessions'.

## End state

- Panel default-collapsed for new users; expanding it is a power-user act.
- No feature ships whose only home is the panel.
- Panel file inventory keeps shrinking (Phase 1 removed 2 tabs and ~1.2k
  lines; `contextPanel/` directory is the boundary — nothing outside it
  and `ContextPanel.tsx`/`ContextSidebar.tsx` may grow for panel-only
  features).

## Non-goals

- Removing the panel entirely — cross-turn archaeology and bulk operations
  are legitimate column-shaped work.
- Mobile/tablet redesign — overlay behavior already works; this SP is about
  content placement, not layout.
