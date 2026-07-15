# SP-118: Daemon Multi-Window Session Isolation

**Status:** 🔵 Proposed — not yet started. Author: orchestrator session 2026-07-14.
**Cross-refs:** SP-116 (Multi-Instance Isolation, in-flight), SP-046 (archived —
*unrelated; do NOT migrate to the new model*), `multi_tab_fanout_test.go` and
`websocket_session_conflict_test.go`.

> **Scope clarification (read first).** SP-046 is *not* the policy this spec
> replaces. The completed `roadmap/_completed/SP-046-workspace-sync-model.md`
> covers the browser-primary *workspace sync* model — Lamport seq counters,
> container/browser divergence, `/api/workspace/takeover`. None of that is in
> scope here.
>
> The "SP-046" label on the WS single-active code is **mislabeled policy**,
> not a real cross-reference. The comments at `pkg/webui/websocket_handler.go`
> (15+ `[SP-046]` log lines, plus the type comment at `:14-16`) and the
> doc-comment at `pkg/webui/multi_device_takeover.go:7-12` (cites
> `SP-046-5`, a sub-section of an archived spec that doesn't have that
> sub-section) all attribute the policy to SP-046. They should attribute it
> to SP-118 once shipped. The cleanup sweep is in scope as part of Phase 1.
>
> SP-046 itself stays archived and untouched. The WS policy was added
> without a corresponding spec body; this spec is the missing document.

## Problem statement

The Sprout daemon (`sprout service`, port 56000 — see `cmd/service.go:33-37`,
`const servicePort = 56000`) is the default way users interact with Sprout.
Users open multiple browser windows against the same daemon: same user
account, same machine or across machines, sometimes pointed at the same
workspace, sometimes at different ones. The daemon must support N parallel
browser sessions per user.

Today, every window against the daemon enters the **single-active-session
WebSocket policy** baked into `pkg/webui/websocket_handler.go:93-156`. The
policy is enforced through `activeWSByUserID sync.Map` at
`pkg/webui/server.go:56`, keyed by `userID`. A second window to the daemon
triggers `session_conflict` and blocks in `waitForTakeover` for up to 10s
(`websocket_handler.go:798-826`). If the new tab never sends
`session_takeover`, the old tab loses its socket and its UI appears
frozen even though the agent keeps running.

The architecture already supports the right behavior — `chatSubscribers`
(`pkg/webui/chat_subscribers.go`), `shouldForwardEventToConnection`
(`websocket_handler.go:425-540`), and the `multi_tab_fanout_test.go`
regression suite all encode fanout-to-multiple-tabs-by-chat-id with
strict security carve-outs. None of it is reachable today because the
single-active registry gates every connection behind one slot.

**Two operating modes, two policies.** This spec splits them:

1. **Mode 1 — `sprout agent` (TUI/CWS-bound).** Single owner per port,
   WebUI is a duplicate of the terminal session. Multiple browser windows
   against this port are duplicates of one session. The current
   single-active WS enforcement is acceptable here. **Keep it.**
2. **Mode 2 — `sprout service` (daemon, port 56000).** Multiple parallel
   browser windows per user, each its own chat, each its own agent. The
   current single-active WS enforcement is wrong here. **Replace it.**

The split point is **the `sprout agent` server, not the connection**.
Use a new field `ws.agentEnforceSingleSession bool`, set by `sprout agent`
when it constructs the React web server. Default false; the daemon path
leaves it false. Dispatch on it, *not* on `ws.serviceMode` — see
"Dispatch signal" below.

## Goals

- Mode 2 (daemon) supports N parallel browser windows per user.
- Each window has its own chat session and its own agent; events for chat
  X reach all windows viewing chat X and no others.
- Two windows on the same workspace are *isolated by default*: picking the
  same workspace does not collapse them into one session. Users on the
  same chat continue to get the fanout they have today (preserved by
  `chatSubscribers` + `shouldForwardEventToConnection`).
- Multi-tab *within a single browser window* (two tabs of the same URL,
  same browser instance, same `clientID` cookie): the existing
  same-chat-across-tabs fanout is preserved byte-identically.
- Multi-tab *across browser instances* (two browsers, two `clientID`s):
  each tab is its own session; events for chat X reach all tabs viewing
  X. This is what `multi_tab_fanout_test.go` already tests.
- Mode 1 (agent) behavior is byte-identical: same single-active
  enforcement, same `session_conflict` / `session_takeover` flow, same
  log lines — just scoped to a single handler and rebranded `[SP-118-Mode1]`.
- `cleanupAfterPanic` stops nuking sibling windows. In Mode 2 a panic in
  one window must not invalidate another window's chat session or agent.
- Recent-workspaces tracker (`~/.sprout/recent_workspaces.json` per SP-116
  Phase 4) is preserved with its current user-global semantics — two
  windows in different workspaces will see each other's recents. Documented
  in §"Open questions" for a follow-up; not changed in this spec.
- Roll out behind a feature flag (`DAEMON_MULTI_SESSION`) defaulting to ON
  for the daemon and OFF for everything else, so we can roll forward,
  watch metrics, and roll back via config rather than re-deploy.

## Non-goals

- Replacing the SP-046 *workspace sync* model (Lamport seq counters,
  `/api/workspace/takeover`). That's a separate, completed spec.
- Migrating the dead `pkg/webui/multi_device_takeover.go` registry into
  the new model. It has no callers (`multi_device_takeover_test.go:8` has
  no shared `ReactWebServer` wiring). Leave it in place; clean up in a
  follow-up PR with explicit scope.
- Changing the port assignments (56000 daemon, dynamic agent).
- Multi-user daemon (still one system daemon per machine).
- Adding WS-layer persistence (chats, agents). Client contexts and chat
  sessions are in-memory only (`pkg/webui/server.go:48-58`,
  `pkg/webui/client_context.go`); a daemon restart loses them. That's
  pre-existing and not in scope here.
- WASM/browser-mode rewrite (separate from `_completed/SP-045-wasm-feature-parity.md`).
- Per-user soft cap on connection count. Deferred; §"User-connections soft
  cap" covers why.

## Design

### Dispatch signal: `ws.agentEnforceSingleSession`

**Do not** reuse `ws.serviceMode` as the dispatch signal. `TestSessionConflict_Takeover_UserMode`
at `pkg/webui/websocket_session_conflict_test.go:292-325` sets
`srv.serviceMode = true` and *exercises the takeover flow as the
service-mode behavior*. Re-using `serviceMode` would break that test.

Instead, add a new field `agentEnforceSingleSession bool` to
`ReactWebServer` (`pkg/webui/server.go`, near the existing `serviceMode`
field at `:76`):

- `sprout agent` sets it to `true` at the point where it constructs the
  React web server.
- `sprout service` (the daemon path) leaves it at the default `false`.
- The dispatch in `handleWebSocket` becomes:
  `if ws.agentEnforceSingleSession { handleWebSocket_Agent(...) } else { handleWebSocket_Daemon(...) }`.

This preserves the existing `serviceMode == true` test at
`websocket_session_conflict_test.go:292` *because the dispatch is now
orthogonal to `serviceMode`*. Mode 1 becomes a property of "agent-mode
binding to a CWS port," which is exactly what the test setup invokes.

`ws.serviceMode` continues to track the `SPROUT_SERVICE=1` env var as it
does today (`server.go:136`, `:291`); it is *not* changed.

### Mode 1 (`handleWebSocket_Agent`) — keep current behavior, rebrand log tag

The existing handler body stays essentially unchanged; rename the entry
function and adjust log tags:

- Function rename: `handleWebSocket` → `handleWebSocket_Agent`. Internal
  callers updated.
- Log tag sweep: every `[SP-046]` log line on the Mode-1 path
  (24 lines today, verified: `websocket_handler.go:107, 120, 137, 729,
  811, 816, 821, 836, 850, 860, 868, 879, 889, 905, 909, 914, 924,
  939, 943, 953, 959, 963, 969, 973`) becomes `[SP-118-Mode1]`.
  Sweep is one commit in Phase 1.
- Type comment at `websocket_handler.go:14-16` updated to reference
  SP-118 Phase 1.
- Existing `pkg/webui/multi_device_takeover.go` doc comment
  (`SP-046-5`) updated to say the registry is dead code under SP-118;
  don't reference SP-046-5 since that sub-section never existed.
- `cleanupAfterPanic(clientID, sessionID)` body unchanged — clears
  whole-clientID state — but renamed to `cleanupAfterPanicAgent` for
  parallelism with the new Mode-2 helper.
- `notifyTerminalConnectionsDisplaced` unchanged.

Tests: `websocket_session_conflict_test.go` and
`multi_tab_fanout_test.go` keep passing byte-identically under the
rename. (Verified by reviewer: the multi-tab tests construct a bare
`&ReactWebServer{chatSubscribers: newChatSubscribersRegistry()}` and
never touch the dispatch. `TestSessionConflict_Takeover_UserMode` exercises
the takeover path under `serviceMode=true`; with the new dispatch the
test must additionally set `agentEnforceSingleSession=true`, since
today's setup is "service-mode + takeover = Mode 1." Add that one
line in the test setup. The other TestSessionConflict_* tests probably
need the same flag.)

### Mode 2 (`handleWebSocket_Daemon`) — N parallel sessions

**Connection registry.**

New type `UserConnections` in
`pkg/webui/multi_connection_registry.go`. Replaces the per-user single
slot with a `sync.Map[userID][]*activeWSConn` shape guarded by per-user
`sync.RWMutex` (use a `sync.Map[*sync.RWMutex, struct{}]` of per-key
locks, lazy-allocated). API:

- `Add(userID string, conn *activeWSConn)`
- `Remove(conn *websocket.Conn) (removed bool, count int)` — best-effort
  if the per-user lock is contended (we accept a brief stale entry until
  next sweep).
- `Count(userID string) int`
- `ForEach(userID string, fn func(*activeWSConn) bool)` — used by
  fanout writers.

Implementation note: defer the slice-vs-`sync.Map[conn]*conn` choice.
Start with the slice — for typical user-scoped connection counts
(≤ ~16) it's faster and simpler. Switch if benchmarks show contention.

**Per-connection lifecycle.**

- Connection dial: generate `sessionID` (existing
  `crypto/rand+hex` pattern at `websocket_handler.go:42-49`); register
  into the user slice; load `ConnectionInfo` into `ws.connections` (the
  current `sync.Map` keyed by `*websocket.Conn`).
- Connection close: `Remove(conn)`, then
  `ws.connections.Delete(conn)`, then
  `ws.chatSubscribers.UnsubscribeAll(conn)`. The current defers at
  `websocket_handler.go:175, 193-196` keep working unchanged.
- Fresh-connection unpause: `ws.setClientPaused(clientID, false)` at
  `websocket_handler.go:172` becomes **window-scoped, not
  clientID-scoped**. Mode 2 with two windows under one user (different
  clientIDs is the common case; same clientID in same browser instance
  is the exception per §Open Questions): pausing one must not unpause
  another. The fresh-connect-unpause should still happen for *all*
  connections on that clientID, since pause-by-clientID is itself a
  per-clientID signal (one process per client); document this as
  "intended" rather than changing semantics here.
- `ws.connections` Range lock duration: noted as O(N) per event, where
  N is the number of connections on a chat during fanout. With N
  bounded at soft-cap ~16, this is fine. No change.

**`cleanupAfterPanic` — Mode-2 variant.**

The current implementation at `websocket_handler.go:750-822` clears the
**whole clientID**'s state. The doc-comment at `:750-761` justifies this:
"a panicked goroutine may have corrupted shared state (e.g. the MCP
manager or conversation history), so it's safer to force full agent
recreation than to risk using a half-initialized agent in other chat
sessions."

That justification *still holds* in Mode 2: agent state, MCP manager,
and conversation history are still shared across windows for the same
user. But Mode 2 introduces a new failure mode the per-clientID clear
makes worse: a panic in window A invalidates window B's chat entirely,
even when window B's chat has no shared state with window A's chat.

The fix is not to drop the corruption defense; it's to **bound the
blast radius** by what could plausibly share state with the panicked
session:

- New helper `cleanupAfterPanicSession(clientID, sessionID)` —
  Mode-2 path. Drops this session from the user registry; clears
  only this session's chat state in the client context; clears
  cached agents for `clientID` only if `Count(userID) <= 1` (i.e.,
  this was the last window for that user).
- Existing `cleanupAfterPanic(clientID, sessionID)` keeps its
  semantics and is renamed `cleanupAfterPanicAgent` (Mode-1 path).
- `safeHandleGoroutine` calls the mode-appropriate one.

This way the corruption defense is preserved when only one window is
open, and the per-chat blast radius is reduced when multiple windows
are open. The original "MCP manager corruption" case is uncommon but
real; we don't pretend it can never happen.

**Event fanout.**

Already in place via `chatSubscribers` + `shouldForwardEventToConnection`
(`websocket_handler.go:425-540`). In Mode 2:

- `chatSubscribers.Subscribe(chatID, conn)` on connect (existing logic
  at `websocket_handler.go:190-192`).
- Each event for chat X reaches every connection registered to chat X,
  regardless of clientID — `multi_tab_fanout_test.go` already validates
  this contract. Preserved unchanged.
- Security events (`security_approval_request`, `ask_user_request`,
  `security_prompt_request`, `edit_approval_request`) stay glued to
  their originating clientID — preserved unchanged by the carve-out at
  `websocket_handler.go:472-482`.
- Per-window write loops (`websocket_handler.go:266-340`) continue
  each holding their own `eventCh` channel. No change. Each window
  receives its own copy of every event for chats it subscribed to; the
  deduplication cost is bounded by the per-user soft cap.

**`notifyTerminalConnectionsDisplaced` — Mode 2 behavior.**

In Mode 1 the function dispatches `session_displaced` to all terminal
WebSocket connections for the displaced tracking key. In Mode 2 there is
no displacement event: terminal tabs persist by design (the function
doc at `:976-980` already says so), and the daemon doesn't kick users
out of terminals at all.

What does "terminal lifecycle in Mode 2" look like? PTY processes are
tied to the *terminal session*, not the browser window. Closing a
browser window does NOT close the PTY; the daemon keeps it running
until the user explicitly closes the terminal or the daemon exits. The
Mode-2 handler should:

- Keep terminal connections in the user registry (they're WS connections
  too).
- On terminal-WS disconnect, clean up entry from the user registry but
  NOT from `clientContexts` (the PTY lifecycle is separate).
- NOT send `session_displaced` to terminal connections in any Mode-2
  path — the trigger condition (a window's WS being evicted for
  takeover) doesn't exist.

Implementation: keep `notifyTerminalConnectionsDisplaced` callable
but no-op it at the dispatch site when `ws.agentEnforceSingleSession`
is false. Document in code why.

### User-connections soft cap (deferred)

If we want to prevent one user from opening hundreds of windows on the
daemon, add a per-user soft cap (default 8) inside `Add`. On exceed:
log a warning, delay the connection accept by N ms up to a budget, then
accept. **Defer this to a follow-up PR; the initial cut accepts as many
windows as the OS will file descriptors for.** In the meantime, expose
`active_ws_count_by_user` as a metric via `pkg/webui/api_diagnostics.go`
so on-call has visibility even before the cap ships.

## Files to change

| File | Change | Risk |
|---|---|---|
| `pkg/webui/server.go` | Add `agentEnforceSingleSession bool` field to `ReactWebServer`; replace `activeWSByUserID` registry shape with the slice-and-mutex variant for Mode 2 (keep the old shape for Mode 1). | Medium — server wiring |
| `pkg/webui/websocket_handler.go` | Split entry into `handleWebSocket_Agent` and `handleWebSocket_Daemon`; dispatch on `agentEnforceSingleSession`. Sweep `[SP-046]` → `[SP-118-Mode1]` log lines. Update type comment. Add `Count(userID)` use in `cleanupAfterPanicSession`. | High — connection entry point |
| `pkg/webui/multi_connection_registry.go` (new) | `UserConnections` type with `Add`, `Remove`, `Count`, `ForEach`. | Low — new code |
| `pkg/webui/multi_connection_registry_test.go` (new) | Unit tests: single add/remove, concurrent adds, remove-by-pointer, count invariants, empty-slice cleanup. | Low |
| `pkg/webui/daemon_session_isolation_test.go` (new) | Integration test: two `httptest.Server` instances + a stub `ReactWebServer` per user; assert both connections stay live across registry operations; assert `chatSubscribers` delivers a chat-X event to both connections watching chat X. | Medium — first end-to-end |
| `pkg/webui/cleanup_after_panic_modes_test.go` (new) | Regression test: in Mode 2, panic in connection A clears only A's session; connection B (same clientID) survives and retains its chat state when `Count(userID) > 1`. | Medium |
| `cmd/agent_modes.go` (or wherever the agent path constructs `ReactWebServer`) | Set `ws.agentEnforceSingleSession = true` at the call site that does `NewReactWebServer(...)`. TBD exact line at implementation; cite in PR. | Low |
| `cmd/service.go` | Leave `agentEnforceSingleSession` as default `false`. | Trivial |
| `pkg/configuration/config_load_save.go` | Add `daemon_multi_session` setting (default true when `agentEnforceSingleSession` is false, false otherwise). Read at handler entry; dispatch uses both. | Low — additive config |
| `pkg/webui/websocket_handler_daemon.go` (new, optional) | Optional decomposition: `handleWebSocket_Daemon` could live here if `websocket_handler.go` exceeds 1100 lines after the split (current 1008). | Low |
| `pkg/webui/websocket_session_conflict_test.go` | Add `srv.agentEnforceSingleSession = true` to existing `TestSessionConflict_*` setups so they continue to route to Mode-1 behavior. Document why. | Low |
| `pkg/webui/multi_device_takeover.go` | Update package comment to say "Dead code under SP-118; see `multi_connection_registry.go` for the live path. Retained for back-compat with any pre-SP-118 callers." | Trivial |
| `pkg/webui/api_diagnostics.go` | Add `active_ws_count_by_user` and `daemon_multi_session` flag value to `sprout diagnose` output so on-call can correlate "did Mode 2 actually engage?" with user reports. | Low |
| `pkg/webui/metrics.go` (if exists; otherwise add) | Expose `active_ws_count_by_user` as a runtime metric. | Low |

## Feature flag

- Setting key: `daemon_multi_session`. Toggle via
  `sprout config set daemon_multi_session=true|false`.
- Effective value: `(ws.agentEnforceSingleSession == false) && setting`.
- Default: true when the server is in service mode AND the agent path
  hasn't forced single-session. False when agent path forced
  single-session (we always use Mode 1 there).
- Wired into `handleWebSocket` dispatch.
- Operational check: `sprout config get daemon_multi_session`,
  `sprout diagnose` shows both the effective value AND
  `active_ws_count_by_user`.

## Testing strategy

1. **Unit tests for `UserConnections`.** Concurrent adds under one user;
   remove by pointer; count invariants; correctness after a
   panic-induced removal in another goroutine.
2. **Mode 1 regression.** Existing `websocket_session_conflict_test.go`
   continues to pass byte-identically when run against the renamed
   `handleWebSocket_Agent`. CI gates this — running the test file
   with the test setup un-touched should produce the same pass/fail as
   before (modulo the `agentEnforceSingleSession = true` line we'll
   add to each test's setup).
3. **Mode 2 end-to-end.** New `daemon_session_isolation_test.go`:
   - Construct a `ReactWebServer` with `agentEnforceSingleSession=false`.
   - Open two `gorilla/websocket` connections, each claiming a
     distinct `clientID` and `chatID`.
   - Publish a `stream_chunk` event with `client_id=A`, `chat_id=X`.
     Assert the subscriber for chat X (connection B) receives it;
     assert a non-subscriber (a third connection, not subscribed to
     X) does not.
4. **Cleanup-after-panic regression.** New
   `cleanup_after_panic_modes_test.go`: trigger
   `cleanupAfterPanicSession` on connection A under `clientID=client-1`
   with a second connection B also `clientID=client-1`, assert
   `Count(userID) > 1` and connection B's chat session survives.
5. **Fanout preservation.** `multi_tab_fanout_test.go` runs without
   behavioral modification and asserts the same fanout contract holds.
   (The tests construct a bare `&ReactWebServer{chatSubscribers: ...}`
   so they neither set nor care about `agentEnforceSingleSession`.)
6. **Manual / acceptance.** Open three browser windows against a real
   daemon (`sprout service` install); each opens a chat; each sends a
   query; confirm each window sees only its own chat's events and that
   the daemon serves them all concurrently without `session_conflict`
   prompts. `instances.json` should show three independent instances
   pointed at potentially-different workspaces.

## Migration / rollout

1. Land the registry + dispatch behind the feature flag, default **off**
   for the daemon in the first PR. Run all CI. Confirm Mode 1
   byte-identical. Confirm `daemon_session_isolation_test.go` passes
   when the flag is true (run with override `daemon_multi_session=true`).
2. Default **on** for the daemon in a second PR. Watch metrics
   (`active_ws_count_by_user`, `panic_cleanup_scope_metric`,
   `chat_subscribers_count`) for at least one release cycle.
3. Document the new model in `README.md` and the WebUI settings panel.
4. Open a follow-up issue to (a) clean up the dead
   `multi_device_takeover.go` registry and its tests, and
   (b) investigate per-clientID workspace recents.

## Acceptance criteria

- `go test -race ./pkg/webui/...` passes with `agentEnforceSingleSession=true`
  AND with `agentEnforceSingleSession=false` covering both modes.
- New `daemon_session_isolation_test.go` passes: two windows under
  one user on the daemon each receive their own events, no
  displacement notification appears on either socket.
- New `cleanup_after_panic_modes_test.go` passes: a panic in
  connection A of `clientID=client-1` does **not** invalidate
  connection B of the same clientID when `Count(userID) > 1`.
- Existing `websocket_session_conflict_test.go` and
  `multi_tab_fanout_test.go` produce identical pass/fail before and
  after the refactor (proves Mode 1 is byte-identical, Mode 2 fanout
  is unchanged).
- Manual smoke: three browser windows on a real daemon, each with a
  distinct chat, each receiving independent events; a fourth window
  opening the same chat as one of them sees live updates.

## Open questions for the implementation pass

1. **Same-clientID, two browser windows, same chat.** Is this reachable
   under Mode 2? `ws.resolveClientID(r)` resolves a per-browser-instance
   cookie: two windows in the same Chrome (same incognito) share
   `clientID`; two windows in different browsers do not. The spec's
   assumption is "one `clientID` per browser instance" — true today,
   assumed to remain true. If a future client_id assignment strategy
   changes, this spec needs revisiting.
2. **Two windows share a chat; one closes mid-query.** The surviving
   window's Next.js app may treat the partner's close as a
   `session_conflict` UI flash because `clientID` recently changed in
   the recent-workspaces tracker. Documented for the WebUI team; not
   a backend change.
3. **Cross-window query mid-flight.** If the user starts a query in
   window A and another query in window B at the same time, the
   agents are per-`clientID`. Within a single browser instance
   (same `clientID`) this would serialize through the existing
   `active_queries` counter (one clientID = one active query today).
   Across `clientID`s (different browsers), each window's query
   runs independently. Documented for the WebUI team.
4. **`active_ws_count_by_user` metric:** connection-level (the
   registry `Count()`) or window-level? Today they're equivalent; if
   a single window with two tabs is counted as one window or two
   matters for the operator's mental model. Default: count both as
   separate "windows"; name it `ws_count_per_user` (not
   `window_count_per_user`).
5. **`ws.setClientPaused` per-connection semantics.** See "Fresh-connection
   unpause" in §Design. Currently per-clientID (intended); preserve.
6. **`SetClientPaused` vs. `setClientPaused`.** There are two
   similarly-named methods. Confirm at implementation time which is
   the one called in `websocket_handler.go:172`.
7. **Daemon restart mid-session.** Sessions are in-memory; a restart
   loses them. Pre-existing behavior; not changed here. Spell out in
   `README.md` so users know.
8. **Recent workspaces tracker.** SP-116 Phase 4 makes it user-global
   (`~/.sprout/recent_workspaces.json`). Two Mode-2 windows in
   different workspaces will clobber each other's recents on every
   WS roundtrip. Two responses are reasonable: (a) per-clientID
   recents, (b) document the user-global behavior. Out of scope for
   SP-118; choose at implementation time and document.
9. **Slice vs. `sync.Map[conn]*conn` for the registry.** Slice first;
   switch if benchmarks warrant.

## Why this is its own spec, not a SP-116 phase

SP-116 is about **config + instance registry** isolation (the right
things for separate processes / workspaces / cwd-bound sessions). SP-118
is about **WebSocket session isolation inside a single daemon process**.
The two touch different layers: SP-116 touches `cmd/root.go`,
`pkg/configuration/`, `pkg/agent_tools/background_process_manager.go`;
SP-118 touches `pkg/webui/websocket_handler.go`, `pkg/webui/server.go`,
the connection registry. Cross-reference each other; do not merge.
