# SP-034: WebUI ↔ Backend Workflow Hardening

**Status:** 📋 Proposed
**Date:** 2026-05-18
**Priority:** CRITICAL (user-visible: Stop button doesn't stop, reload loses work, multi-tab corrupts state)
**Depends on:** SP-027 (chat-session structure), SP-028 (terminal lifecycle baseline)
**Related:** SP-008 (typed errors — error envelope work overlaps), SP-032 (daemon shutdown), SP-033 (trust boundaries)

## Problem

The WebUI ↔ Go-backend protocol has four user-visible defects and several invisible-but-real consistency hazards:

1. **The "Stop" button is a polite suggestion.** `Agent.TriggerInterrupt()` sets a flag the agent loop polls between iterations (`pkg/agent/pause_test.go:33` confirms). The flag never reaches the in-flight HTTP call to the provider. Worse, at least one chat path uses bare `http.NewRequest` with no `context.Context` at all (`pkg/agent_providers/generic_provider.go:1160`) — even if we threaded cancellation through, this site can't honour it. A 30-minute reasoning-model query keeps billing for the full 30 minutes after "Stop."
2. **Reloading the page during an agent run loses the user.** Terminals support `?reattach=<id>` (`pkg/webui/terminal_websocket.go:48-74`) — the ring buffer replays on reconnect. Chats have no equivalent. `pkg/webui/api_query.go:410` returns only `{status: "ok"}`. The backend keeps running; the frontend has no way to see the live stream again.
3. **Multi-tab on the same chat session is unsafe.** `publishClientEvent(clientID, ...)` (`pkg/webui/api_query.go:78`) fans out events to **one client**, not to every tab subscribed to a session. Tab A streams; tab B switches/renames; `AgentState []byte` (`pkg/webui/chat_sessions.go:32`) gets touched under the stream. Silent corruption.
4. **Config writes are last-writer-wins.** No `os.Stat`/`mtime`/`flock` in production `pkg/configuration/config*.go`. The UI's "set provider" can overwrite a CLI's `sprout config set` happening seconds earlier without detection.

Plus structural hygiene: TypeScript types are hand-maintained against Go structs (`webui/src/.../chatSessions.ts:6-21` vs `pkg/webui/chat_sessions.go:27-52` declare different field sets), and the WebSocket message protocol is asymmetric — inbound is validated against a whitelist, outbound isn't.

## What's actually good (don't regress)

- **SSH host-key handling.** `pkg/webui/ssh_binary.go:343`, `pkg/webui/ssh_launch.go:55,73,583` all use `StrictHostKeyChecking=accept-new` — Trust-On-First-Use, not the insecure `=no`. Correct.
- **Image-paste filename.** `pkg/console/image_paste.go:75` `SavePastedImage` uses `paste_<timestamp>_<6 hex chars>` — reasonable entropy; not predictably racing.
- **Path-traversal defense.** `pkg/webui/file_access_control.go:110,128` calls `filepath.EvalSymlinks` *before* the workspace boundary check.
- **Terminal multi-client fanout.** `pkg/webui/terminal_types.go:157-160` broadcasts PTY output to all subscribers — opening the same terminal in two tabs Just Works. The chat path will adopt this pattern.
- **Inbound WebSocket message validation.** `pkg/webui/websocket_message_types.go:42-79` enforces a 10-type whitelist + size cap + panic recovery.

## Current State (verified)

| Area | File:Line | Issue |
|------|-----------|-------|
| LLM call has no `context.Context` | `pkg/agent_providers/generic_provider.go:1160` | Bare `http.NewRequest("POST", p.config.Endpoint, …)` — unkillable from outside the call |
| Agent main entrypoints lack ctx parameter | `pkg/agent_providers/generic_provider.go:184` `SendChatRequest`, `:255` `SendChatRequestStream` | Signature has no `ctx context.Context` — caller can't propagate cancel |
| Interrupt is flag-based | `pkg/agent/pause_test.go:33` (test) | `TriggerInterrupt` sets flag; loop polls `CheckForInterrupt` between iterations |
| No chat reattach | `pkg/webui/api_query.go:410` `handleAPIQueryStatus` | Returns status, not a resumable stream cursor |
| Terminal reattach (positive ref) | `pkg/webui/terminal_websocket.go:48-74` | Ring-buffer + `session_restored` message — the pattern to mirror |
| Event publish keyed on client, not session | `pkg/webui/api_query.go:78,82,85` `publishClientEvent`/`publishClientEventWithChat` | Even with `chatID` parameter, dispatch goes to the single originating client, not to all tabs on that chat |
| `AgentState` mutation under stream | `pkg/webui/chat_sessions.go:32` | `[]byte` field protected by per-session mutex only during a single op; cross-tab mutations interleave |
| No config conflict detection | `pkg/configuration/config*.go` | grep returns zero `os.Stat`/`mtime`/`flock` outside tests; migrations track schema version, not edit version |
| Server→client message types unvalidated | `pkg/webui/websocket_message_types.go:42-79` | Whitelist applies inbound only; rogue/typo server events ship silently |
| TS↔Go type drift | `webui/src/.../chatSessions.ts:6-21` vs `pkg/webui/chat_sessions.go:27-52` | `is_default`/`is_active` exist only on frontend; injected at serialize time at `chat_sessions_api.go:69-70` |
| Inconsistent error envelope | `pkg/webui/websocket_message_handlers.go:144-159` (good) vs `pkg/webui/api_query.go:391-396` (stringy) | Some endpoints emit `{code, message}`; others emit raw 503+text |

## Goals / Non-Goals

**Goals**
- Clicking "Stop" terminates the in-flight LLM HTTP call within ~1s, not at the next loop iteration.
- Reloading the page during an agent run reattaches to the live token stream within ~500ms.
- Two tabs on the same chat see identical, consistent state without one corrupting the other.
- A config write from the UI fails loudly when the on-disk file changed since the UI last read it.
- TypeScript and Go agree on the wire shape by construction, not by convention.
- A single, structured error envelope across every endpoint.

**Non-Goals**
- Migrating the WebSocket to a different protocol (gRPC-Web, SSE, etc.). We're fixing the one we have.
- Multi-user collaboration semantics (operational transforms, CRDTs). Two tabs of one user is the bar.
- Replacing the `kardianos/service`-style libraries; this is webui-layer work.
- Rewriting the WebSocket panic-recovery or whitelist code — both are good already.

## Proposed Solution

### Track A — Cancellation that actually cancels (CRITICAL)

#### A1: Thread `context.Context` through the provider interface
- Change `api.ClientInterface` (`pkg/agent_api/interface.go`) `SendChatRequest`/`SendChatRequestStream` signatures to take `ctx context.Context` as first arg.
- Mechanical update to every implementation in `pkg/agent_providers/` and every call site in `pkg/agent/`.
- This is a wide-but-shallow refactor — touches many files, no logic changes.

#### A2: Fix the contextless POST
- `pkg/agent_providers/generic_provider.go:1160` — change `http.NewRequest("POST", …)` to `http.NewRequestWithContext(ctx, "POST", …)`. The ctx flows in from A1.
- Sweep the rest of `pkg/agent_providers/` for any other bare `http.NewRequest`. The list grep at line 413 already uses the WithContext form; line 1160 is the anomaly.

#### A3: Wire cancellation from the WebSocket session
- `pkg/webui/api_query.go` — when a session starts processing a query, create `ctx, cancel := context.WithCancel(sessionCtx)` and stash `cancel` on the chat session.
- `handleAPIQueryStop` (currently `:399`) calls **both** `TriggerInterrupt()` *and* `cancel()`. The cancel propagates through the provider call; the interrupt is the existing flag-based fallback for sites that haven't been ctx-ified yet.
- When the WebSocket disconnects unexpectedly: by default keep the agent running (cost protection deferred to A5); cancel only on explicit Stop or on session close.

#### A4: Bound the LLM call regardless
- Defensive: add a `RequestTimeout` (default 10min, configurable per provider) to the http.Client used for chat. A hung provider can't burn a daemon forever.

#### A5: Optional cost-cap on disconnect (future)
- Out of scope for v1: behind a config flag, cancel the agent run if no client is attached for >N seconds. v1 keeps the run alive and uses A2-reattach.

### Track B — Chat reattach mirror of terminal pattern

#### B1: Ring buffer per active chat run
- New `chatRunRingBuffer` on each chat session storing the last N (default 5,000) stream chunks with monotonically increasing `seq` numbers.
- Buffer is filled inline as `publishClientEventWithChat(... EventTypeStreamChunk ...)` is called.
- TTL: cleared 60s after the agent run completes.

#### B2: Reattach endpoint
- Extend the chat WebSocket path with `?reattach=<chat-id>&after_seq=<n>`. On connect, server replays buffered chunks with `seq > n` then resumes live streaming.
- Mirror `terminal_websocket.go:48-74`'s shape — send a `chat_run_restored` message with `{chat_id, last_seq, missed_chunks_count}` so the frontend knows what it caught up on.

#### B3: Frontend recovery
- Frontend: on WebSocket open during an active chat (detected via existing `/api/query/status`), automatically reconnect with `reattach` + the last seen `seq`. Hide the reattach from the user — looks like a hiccup, not a reload.

### Track C — Multi-tab consistency

#### C1: Session-scoped event channel
- Refactor `publishClientEventWithChat` (`pkg/webui/api_query.go:85`) so that **events with a non-empty `chatID` fan out to every connection subscribed to that chat**, not just the originating client.
- Subscription model: WebSocket `subscribe` message (already in the whitelist) carries `{chat_id: "..."}` to opt in. Backend maintains `chatSubscribers[chatID] -> []connection`.

#### C2: Per-chat writer lock for state mutation
- Wrap `AgentState []byte` mutation (`pkg/webui/chat_sessions.go:32`) behind a single-writer lock; readers (event broadcast) use a snapshot taken under read lock.
- Any cross-tab side-effect (rename, switch, pin) emits a `session_changed` event so the other tabs refresh their local view.

#### C3: Optimistic-then-canonical UI
- Frontend: on a mutation, optimistically update local state. When the backend emits `session_changed`, reconcile (replace local with server-authoritative).

### Track D — Config conflict detection

#### D1: Read-version tag
- Add `(modTime time.Time, size int64)` to the `Config` in-memory representation as a private field, set during `Load()`.
- Before `Save()`, `os.Stat` the target file. If `(modTime, size)` differ from what we read, return `*ConfigConflictError` (typed; aligns with SP-008).

#### D2: UI flow on conflict
- WebSocket message handlers (`websocket_message_handlers.go:49-59`) that today call `SaveConfig()` unconditionally now check for `ConfigConflictError` and emit `{code: "config_conflict", current_version_summary: {...}}` to the client.
- Frontend shows a small "Settings changed on disk — reload and try again?" toast with a Reload button.

#### D3: File-level atomic write (already present, document it)
- `pkg/configuration/config_persistence.go` already uses an atomic write pattern (SP-029 split). Verify and add a code comment so it doesn't regress.

### Track E — Protocol hygiene

#### E1: Generate TypeScript types from Go
- Adopt `tygo` (or equivalent) in the Makefile: `make generate-ts-types` produces `webui/src/types/generated.ts` from selected Go structs.
- Annotate Go structs with `// tygo:emit` markers. Drop hand-maintained TS interfaces (`webui/src/.../chatSessions.ts:6-21`) in favour of imports from `generated.ts`.

#### E2: Validate server→client message types too
- Extract the whitelist from `websocket_message_types.go:42` into a shared list of message types.
- Add a `validateOutbound(msg)` helper called by every send path. In dev builds, panic on unknown type; in prod, log + drop.

#### E3: Unified error envelope
- Define `WebUIError` struct: `{Code string; Message string; Details map[string]any; Retryable bool}`.
- Replace stringy 503 returns at `api_query.go:391-396` and any other handlers found via grep with `WebUIError` JSON.
- Frontend error-handling switches on `Code`, not on `Message` parsing.

## Implementation Phases

### Phase 1: Cancellation (Week 1) — fixes the most user-visible defect

- [ ] **SP-034-1a**: Add `ctx context.Context` as first arg to `api.ClientInterface.SendChatRequest` and `SendChatRequestStream` in `pkg/agent_api/interface.go`.
- [ ] **SP-034-1b**: Update every implementation in `pkg/agent_providers/` (Generic, Ollama, any others) and the test scripted client.
- [ ] **SP-034-1c**: Update every caller in `pkg/agent/` (`api_client.go`, `conversation.go`, `seed_integration.go`, etc.) to pass through a real context.
- [ ] **SP-034-1d**: At `pkg/agent_providers/generic_provider.go:1160`, change `http.NewRequest` → `http.NewRequestWithContext(ctx, ...)`. Grep for any other bare `http.NewRequest` and convert.
- [ ] **SP-034-1e**: In `pkg/webui/api_query.go`, create `ctx, cancel := context.WithCancel(parent)` when a query starts; stash `cancel` on the chat session.
- [ ] **SP-034-1f**: `handleAPIQueryStop` (around `pkg/webui/api_query.go:399`) calls **both** `TriggerInterrupt()` and the stashed `cancel()`.
- [ ] **SP-034-1g**: Add a `RequestTimeout` to the chat http.Client in `pkg/agent_providers/generic_provider.go:172-176` (default 10min, override via config).
- [ ] **SP-034-1h**: Integration test: start a query against a stub provider that sleeps 30s; click Stop after 1s; assert HTTP request was cancelled within 1s.

### Phase 2: Chat reattach (Week 1-2)

- [ ] **SP-034-2a**: Add `chatRunRingBuffer` (size 5000 chunks, configurable) to the chat session struct in `pkg/webui/chat_sessions.go`.
- [ ] **SP-034-2b**: In `publishClientEventWithChat` (`api_query.go:85`), append stream-chunk events to the ring buffer with a monotonic `seq`.
- [ ] **SP-034-2c**: Extend the WebSocket chat handler to accept `?reattach=<chat-id>&after_seq=<n>` query params; replay buffered events then resume live stream.
- [ ] **SP-034-2d**: Send a `chat_run_restored` message on reattach with `{chat_id, last_seq, missed_chunks_count}`. Add this message type to the outbound list (Phase 5 E2).
- [ ] **SP-034-2e**: Frontend: on WebSocket open, if a chat is currently running per `/api/query/status`, automatically include `reattach` + last-seen `seq` in the connect URL.
- [ ] **SP-034-2f**: Buffer TTL — clear 60s after run completion; cap memory at N MB to prevent runaway.

### Phase 3: Multi-tab consistency (Week 2)

- [ ] **SP-034-3a**: Add `chatSubscribers map[string][]connection` to `ReactWebServer` (`pkg/webui/server.go:42`). Protect with `sync.RWMutex`.
- [ ] **SP-034-3b**: Handle inbound `subscribe` message (already in whitelist) by adding the connection to the chat's subscriber list. On disconnect, remove.
- [ ] **SP-034-3c**: Refactor `publishClientEventWithChat` (`api_query.go:85`) — when `chatID != ""`, fan out to every connection in `chatSubscribers[chatID]` rather than only the originator.
- [ ] **SP-034-3d**: Add a per-chat `sync.Mutex` for `AgentState` mutations in `pkg/webui/chat_sessions.go`. Reads take a snapshot under RLock; writes serialize.
- [ ] **SP-034-3e**: Emit `session_changed` events on rename/pin/switch so other tabs reconcile. Add this message type to the outbound list.
- [ ] **SP-034-3f**: Frontend: on `session_changed`, replace local session state with the broadcast payload (canonical wins over optimistic).

### Phase 4: Config conflict detection (Week 2-3)

- [ ] **SP-034-4a**: Add private `(loadedModTime time.Time, loadedSize int64)` fields to `Config`. Populate in `Load()` (`pkg/configuration/config_persistence.go`).
- [ ] **SP-034-4b**: Before each `Save()`, `os.Stat` the path. If `(modTime, size) != (loadedModTime, loadedSize)`, return a typed `ConfigConflictError`.
- [ ] **SP-034-4c**: Surface the typed error in WebSocket message handlers (`websocket_message_handlers.go:49-59`) as `{code: "config_conflict", current_summary: {...}}`.
- [ ] **SP-034-4d**: Frontend: show a non-blocking "Settings changed on disk" toast with a Reload action.
- [ ] **SP-034-4e**: Add a regression test simulating: load config, modify file externally, attempt save → expect ConfigConflictError.

### Phase 5: Protocol hygiene (Week 3)

- [ ] **SP-034-5a**: Add `tygo` (or equivalent) to dev tooling. New Makefile target `make generate-ts-types` that emits `webui/src/types/generated.ts` from annotated Go structs.
- [ ] **SP-034-5b**: Annotate `pkg/webui/chat_sessions.go` `chatSession`, `pkg/webui/events/*.go` event payloads, and key API response shapes with the tygo emit marker.
- [ ] **SP-034-5c**: Replace the hand-maintained TS interface in `webui/src/.../chatSessions.ts:6-21` with an import from `generated.ts`. Keep computed/derived fields in a separate wrapper type.
- [ ] **SP-034-5d**: Extract the inbound message-type whitelist from `pkg/webui/websocket_message_types.go:42` into a shared registry; add `validateOutbound(msg)` called by every `WriteJSON` site.
- [ ] **SP-034-5e**: Define `WebUIError` struct + JSON encoder helper. Replace stringy 503s in `pkg/webui/api_query.go:391-396` and audit other handlers for the same anti-pattern.
- [ ] **SP-034-5f**: Frontend: shared error-handling util keyed on `Code`; deprecate string-matching on `Message`.

### Phase 6: Documentation

- [ ] **SP-034-6a**: Write `docs/WEBUI_PROTOCOL.md` — REST endpoints table, WebSocket message types (inbound + outbound), event payloads, reattach flow, error envelope shape, type-generation workflow.

## Success Criteria

| Metric | Target |
|--------|--------|
| Stop button → in-flight HTTP cancelled | < 1s end-to-end |
| Provider HTTP requests using `http.NewRequest` (no context) | 0 (down from 1+ in chat path) |
| Page reload during agent run → live stream resumes | < 500ms, with chunks-missed = 0 in typical case |
| Two tabs on same chat, mutation in tab B | Tab A receives `session_changed`; no AgentState corruption under race test |
| Concurrent UI + CLI config write | One side fails with `config_conflict`; no silent overwrite |
| Server→client message type whitelist violations in CI | 0 |
| TS interfaces hand-maintained from Go structs | 0 (all generated) |
| Endpoints returning unstructured 503 strings | 0 (all use `WebUIError` envelope) |

## Risks

- **Wide ctx refactor (Phase 1) introduces churn across many files.** Mitigation: do it in a single PR with mechanical Go signature edits; provider implementations are well-isolated.
- **Reattach buffer memory growth** if a chat run produces millions of tokens. Mitigation: cap by chunk-count *and* by total bytes; oldest chunks drop first.
- **`session_changed` broadcast storms** if a tab spams renames. Mitigation: debounce on the frontend; backend rate-limit per session to 10 events/sec.
- **tygo type generation may not handle every Go type cleanly** (generics, interfaces). Mitigation: only emit DTO structs that the wire actually carries; keep complex domain types Go-only.
- **`os.Stat` for conflict detection has a TOCTOU window** between stat and write. Mitigation: open the file with `O_EXCL`-style flag (or use the existing atomic-write pattern's temp-file rename, comparing inode before swap). Document that this catches "I forgot I edited that elsewhere," not "two daemons writing in lockstep."

## Files Reference

| File | Action |
|------|--------|
| `pkg/agent_api/interface.go` | Modify: add `ctx context.Context` to `SendChatRequest`/`SendChatRequestStream` |
| `pkg/agent_providers/generic_provider.go` | Modify: ctx threading; line 1160 → `NewRequestWithContext`; default `RequestTimeout` |
| `pkg/agent_providers/ollama_provider.go` (and others) | Modify: ctx threading |
| `pkg/agent/api_client.go`, `pkg/agent/conversation.go`, `pkg/agent/seed_integration.go` | Modify: pass ctx through |
| `pkg/webui/api_query.go` | Modify: stash cancel per chat session; `handleAPIQueryStop` calls cancel; add ring buffer; refactor `publishClientEventWithChat` fan-out |
| `pkg/webui/chat_sessions.go` | Modify: ring buffer; per-chat writer lock; `chatSubscribers` integration |
| `pkg/webui/server.go` | Modify: add `chatSubscribers map`; subscribe/unsubscribe lifecycle |
| `pkg/webui/websocket_handler.go` | Modify: subscribe handler; outbound validation hook |
| `pkg/webui/websocket_message_types.go` | Modify: shared whitelist; `validateOutbound` |
| `pkg/webui/websocket_message_handlers.go` | Modify: surface `ConfigConflictError` |
| `pkg/configuration/config_persistence.go` | Modify: capture mtime/size on load; check before save |
| `pkg/configuration/errors.go` (new) | Create: `ConfigConflictError` typed error |
| `pkg/webui/errors.go` (new) | Create: `WebUIError` envelope |
| `Makefile` | Modify: add `generate-ts-types` target |
| `webui/src/types/generated.ts` | Create (generated): TS types from Go structs |
| `webui/src/.../chatSessions.ts` | Modify: import from `generated.ts`; remove hand-maintained interface |
| `docs/WEBUI_PROTOCOL.md` | Create: protocol reference |
