# SP-142 — Chat Mode Lanes: Per-Mode Chat Ownership on the Server

> **Status (2026-09-24):** Draft. Not started.
> Motivation: the multi-chat surface (New Chat, chat tabs) shipped with
> per-chat agents server-side, but chats carry no mode identity — the chat
> list is one flat list shared by the Code and Design modes, and the
> frontend's per-mode pins (`workspaces/useChatModePinning.ts`, SP-140-10c)
> are advisory localStorage only. A design conversation is clickable from
> Code mode (and vice versa), every cross-mode click re-pins, and the
> Design "fresh session" boot path mints new empty chats into the same flat
> list. Users report the result as "chats don't stay in their lane."

## Problem

1. **No lane ownership.** `chatSession` (pkg/webui/chat_sessions.go) has
   no mode field. Every mode sees every chat.
2. **Pins are advisory and single-slot.** One remembered chat per mode,
   client-side only, blind to deletes on other tabs, and re-written by any
   tab click in any mode (`switchSessionWithModePin` pins whatever was
   clicked into the *current* mode).
3. **Flat-tab contamination.** `useChatSessionsSync` mirrors every session
   into the tab strip regardless of mode; a design chat sits between code
   chats and takes over the active slot when clicked.
4. **Concurrent-agent cost & collision.** Each chat is a full agent
   (config, tools, embedding manager) on one workspace; two running chats
   can edit the same files unaware of each other.

## Non-goals (deliberate)

- **No cross-agent shared context / agent-to-agent awareness.** Models are
  not trained for peer-agent negotiation; the coordination surface this
  spec builds is (a) lane separation and (b) a serialize-or-warn gate.
  Richer coordination is explicitly out of scope until evidence demands it.
- No change to shared-agent mode (CLI + WebUI single conversation).
- No per-worktree lanes beyond what `worktree_path` already provides.

## Design

### 1. Server-side lane ownership

- `chatSession` gains `Mode string` (wire: `"mode"`, `"code"` | `"design"`,
  `""` = legacy/unassigned). New chats stamp the creating mode; legacy
  chats default `""` and are treated as `code` (read side) so existing
  installs degrade to today's behavior.
- `POST /api/chat-sessions/create` accepts `mode`; `chatSessionSummary`
  and `/api/chat-sessions` list entries carry it. Update the TS mirror
  (`webui/src/types/generated.ts` + `services/chatSessions.ts`).
- `POST /api/chat-sessions/switch` rejects a cross-mode switch
  (`409 mode_mismatch`) — switching *within* a mode stays free. The
  mode-aware client never issues cross-mode switches; the rejection is the
  backstop for stale tabs and third-party clients.
- Migration: none on disk (chat sessions are in-memory per client
  context); the field simply appears with `""` for pre-existing sessions.

### 2. Mode-aware client

- `useChatSessionsSync` filters the tab strip to the active mode's chats
  (`mode === 'code'` shows `code` + `""`; `design` shows `design`).
- `useChatModePinning` pins become mode-scoped *by construction*: a switch
  within the mode is the only pin write. Cross-mode restore keeps the
  create→switch→pin boot path but the created chat stamps the mode, so a
  stale pin can never resurrect the other mode's chat into this one.
- New Chat stamps the current mode; the Design mode's fresh-session boot
  continues to create design-mode chats (now labeled as such).
- `handleActiveChatChange` failure path: a `mode_mismatch` rejection falls
  back to the mode's pinned chat or a fresh session — never silently
  leaves the wrong transcript active.

### 3. Concurrent-query gate (one workspace, one runner)

- Server: a workspace-level query gate — when any chat in the client
  context has an active query, a new query from a *different* chat in the
  same workspace returns `409 workspace_busy` with a payload naming the
  running chat (`{running_chat_id, running_chat_name}`).
- The client renders this as an inline notice in the composer: "Another
  chat (Name) is working in this workspace — send anyway queues after
  it." Send-anyway enqueues locally (the existing queue already tags
  entries with `chatId`); the gate releases on `query_completed`.
- Rationale: agents share one file tree; serialization is the minimal
  machine-readable coordination — the running chat's `current_query` and
  changed-file set are already tracked per chat and surface in the busy
  payload (no new context plumbing).

### 4. Design-mode chat surface

- Design mode's Agent panel header shows the design chat's name + a New
  Chat affordance scoped to design (SP-140-6 §6f panel; no Code-mode
  chrome).
- Not-found/stale-pin boot in design mode already creates fresh; with lane
  stamping this can no longer collide with a code chat of the same name.

## Items

- **142.1** Server: `Mode` field on chatSession + create/summary/list/switch
  (mode stamping, `mode_mismatch` backstop, TS mirror). Go unit tests:
  stamping, legacy `""` reads as code, cross-mode switch rejection.
- **142.2** Client: mode-filtered tab strip + mode-scoped pin writes +
  `mode_mismatch` fallback. Vitest: no cross-mode tabs, pin isolation,
  stale-pin fallback.
- **142.3** Server: workspace query gate + `workspace_busy` payload.
  Go unit tests: second-chat query rejected while first runs, released
  after completion, shared-mode unaffected.
- **142.4** Client: busy notice + send-anyway queueing. Vitest: notice on
  409, queue drains after completion, cancel path clears.
- **142.5** Design agent panel header (name + scoped New Chat). Vitest +
  testids.

## Acceptance criteria

1. Creating chats in Code and Design yields two disjoint tab lists per
   mode; no chat is reachable from the other mode's UI.
2. Switching modes restores that mode's own chat (or fresh), never the
   other mode's, across reload and stale-pin cases.
3. A second chat's send while another runs in the same workspace gets the
   busy notice and queues; it never starts concurrently.
4. Shared-agent mode behavior unchanged; e2e suite (shared-mode) green
   without modification.
5. All existing multi-chat Go tests and chat vitest suites green.

## Verification plan

- `make vet && make fmt-check && make lint && make build-all`
- `make test-unit-lowmem TEST_PKGS=./pkg/webui/` (bounded, per repo test
  safety rules — never bare `go test ./pkg/webui/`)
- `cd webui && npx vitest run src/` for the client suites touched
- Manual dogfood: two chats in two modes, reload mid-conversation, queue
  behind a running chat, cancel.
