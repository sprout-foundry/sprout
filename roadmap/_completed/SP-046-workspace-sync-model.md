# SP-046: Browser-Primary Workspace Sync Model

**Status:** ✅ Shipped (all 5 numbered items complete 2026-06)

When the webui can run in a browser tab (the WASM build) AND a container
(the daemon), a single workspace has two parallel editors. SP-046
defined a sync model so they don't clobber each other. The
container/browser pair uses a `browser_seq` / `container_seq` /
`last_synced_browser` counter scheme (Lamport-style) so each side can
detect divergence; `edit_handler.go:90` checks staleness before any
write. Conflict resolution is explicit (`workspace_conflict_test.go`
covers all scenarios). The heartbeat monitor (`workspace_heartbeat.go`)
pings every 15s with a 60s grace period; if missed, the workspace is
considered offline. Multi-device takeover uses a dedicated
`/api/workspace/takeover` endpoint with WS conflict notification.

## Key decisions

- **Lamport-style seq counters**, not wall-clock timestamps. Clocks can
  drift; counters don't.
- **Staleness check at the handler boundary, not the storage layer.** The
  handler is the chokepoint for all writes; that's where to enforce
  ordering.
- **Explicit conflict surfaces**, not silent merges. When
  `browser_seq > last_synced_browser`, the user sees a conflict
  resolution UI; they pick one side or merge.
- **Heartbeat with grace period**: 15s ping, 60s grace. Tuned so a
  transient network hiccup doesn't trigger takeover mode.
- **Takeover is a separate endpoint**, not implicit on first write.
  The new device announces "I am now primary" before doing anything.
- **Free-tier convergence**: the protocol degenerates cleanly when no
  container exists (browser-only mode just works).

## Artifacts

- code: `pkg/agent_tools/workspace_sync.go` — seq counter logic
- code: `pkg/agent_tools/edit_handler.go:90` — staleness check
- code: `pkg/agent_tools/workspace_heartbeat.go` — 15s/60s heartbeat
- code: `pkg/webui/routes.go` — `/api/workspace/takeover` endpoint
- tests: `workspace_conflict_test.go`, `rollback_staleness_test.go`,
  `websocket_session_conflict_test.go`

Full specification archived — see git history for original content.