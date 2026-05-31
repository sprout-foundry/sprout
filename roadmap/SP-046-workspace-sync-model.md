# SP-046: Browser-Primary Workspace Sync Model

**Status:** 📋 Proposed
**Priority:** High (blocks paid-tier UX work)

## 1. Decision Summary

The paid sprout tier serves users a workspace through two synchronized
filesystems: an **OPFS replica in the browser** (always-available, fast)
and a **Docker container working directory** on the platform (where
agent tool calls actually run). The browser side is the **primary**
authority for user-typed edits; the container side is the primary
authority for agent-generated edits. Both sides converge via a sync
protocol described below.

This design exists because:

- A cold container resume on every paid-tier session would be a UX cliff.
  Browser-side persistence lets returning users feel instant.
- Single-box self-hosted beta makes idle container reaping mandatory; the
  browser cache lets us reap aggressively without UX pain.
- Free-tier users (no container at all) get the same UX shape, just
  without the second replica — code paths converge.

## 2. Sync Transport

| Direction | Transport | Notes |
|---|---|---|
| Container → Browser | WebSocket patch stream | Each tool-call write emits one patch event. Reuses the same WS for heartbeat. |
| Browser → Container | HTTP POST per op | Browser queues outbound ops in OPFS, flushes when WS is up. |
| Heartbeat | WebSocket ping every 15s | See §5. |

The protocol is file-level (patch granularity = whole-file or
RFC-6902-style JSON patches for small edits), not byte-level. We are NOT
building real-time collaborative editing here — we are reconciling two
asynchronous writers (user, agent) that rarely touch the same file at
the same instant.

## 3. Conflict Semantics

Each file in OPFS carries a metadata blob:

```json
{
  "browser_seq":           42,
  "container_seq":         17,
  "last_synced_browser":   42,
  "last_synced_container": 17,
  "modified_at":           "2026-05-20T10:32:00Z"
}
```

Rules:

1. User edit in the browser bumps `browser_seq`. Queues an outbound op.
2. Container write to the same file emits a patch event with
   `container_seq` bumped.
3. On receiving a patch from the container, if
   `browser_seq > last_synced_browser`, the browser has unsynced edits.
   The browser writes the container's patch as a sibling file
   `<path>.theirs` and surfaces a git-style conflict marker UI in the
   editor.
4. The agent's `write_file` tool wrapper refuses the write if
   `browser_seq > last_synced_browser` for that path, returning a tool
   error: `"user has unsynced edits to <path>, ask before overwriting"`.
   The agent must explicitly acknowledge this in its reasoning and
   either back off, ask the user, or wait.
5. Server reboots / browser crashes don't lose state — both sides hold
   their sequence in OPFS / container disk.

No CRDT. File-level last-writer-wins-with-conflict-detection is enough
for the use cases sprout actually has.

## 4. Long-Running Jobs

The tab must stay open while a long-running job (anything > a few
seconds) is in progress. The model:

- Browser sends a heartbeat ping over the WS every 15 seconds.
- Container monitors the heartbeat. If missed for 60s, the container
  considers the session abandoned and terminates the running job.
- The 60s grace covers transient network blips, sleep/wake cycles
  shorter than a minute, and similar. Doesn't cover "user closed laptop
  and walked away."
- UI surfaces this constraint at job-start: "this command takes a few
  minutes; keep this tab open or it'll cancel."

Future work: a "detach and resume later" mode for very long jobs becomes
a paid-tier-plus feature (SP-XXX). Not in scope for beta.

## 5. Multi-Device

**Single active session per user.** On opening sprout on a second
device, the user gets:

> Sprout is already open on another device. Take over here?
>
> [Yes, take over]  [Cancel]

Taking over: the first device's WebSocket is closed, the user gets
"This session moved to another device" overlay in the first browser.

This is deliberately less convenient than continuous multi-device sync.
The simpler model lets us ship beta without a real sync service, and
the upgrade path is clear (paid+ tier adds a server-side sync layer,
makes browsers thin caches over it). The beta model also avoids the
"split-brain editing" class of bugs by construction.

## 6. First-Load Latency

A returning user on a new device pays the cold-hydrate cost
(container → browser via WebSocket, typically MB to hundreds of MB of
repo bytes). UI shows a one-time progress bar with the realistic
estimate. Subsequent loads on the same device are instant from OPFS.

We do not promise instant cold-start. We promise instant warm-start.
Marketing copy should match.

## 7. Agent Staleness Rule

The agent's `write_file` tool wrapper enforces a re-read invariant:

```
BEFORE write_file(path, ...):
  IF file's browser_seq has changed since the agent last read it
  OR file's last-modified is within the last 30 seconds
  OR the agent has not called read_file(path) this turn:
    REFUSE with "must call read_file({path}) first; the file may be stale"
```

This protects against the failure mode where:
1. Agent reads X at T=0
2. User edits X in the browser at T=1 (browser-local, not yet synced)
3. Agent decides to write X at T=2 (based on stale T=0 read)

The 30s "recent modification" window is intentionally conservative — it
catches user edits that have been queued but not yet acknowledged by
the container.

Cheap check (a few hash comparisons), cheap to enforce in the tool
handler layer, expensive to debug after the fact if missing.

## 8. Free Tier Convergence

Free-tier users have no container. The above protocol degenerates
cleanly:

- "Container → Browser" sync is a no-op (no container)
- Browser is the sole authority for everything
- Agent operations happen via WASM-side tool handlers writing directly
  to OPFS (not through a `write_file` patch stream)
- The staleness rule still applies inside the agent's own loop, where
  the user can edit between the agent's `read_file` and `write_file`
- Multi-device for free users: single-active-session, same as paid

Free → paid upgrade is a one-time hydrate: contents of OPFS push to the
new container, the container becomes the authoritative replica going
forward.

## 9. Out of Scope

- Real-time collaborative editing (multi-user on one workspace)
- Background long-running jobs that survive tab close
- Continuous multi-device sync
- Cross-workspace search / federation
- Operational transform / CRDT-level merge

All of these are interesting and all of these can wait until after
paid-tier has revenue.

## 10. Failure Modes & Recovery

| Scenario | Behavior |
|---|---|
| Container dies mid-session | WS drops; browser shows "reconnecting" with backoff; on reconnect, browser sends its `browser_seq` for every file; container reconciles |
| Browser crashes | OPFS persists; on reload, browser sends `(browser_seq, last_synced)` per file; container resends any patches with `container_seq > last_synced_container` |
| Container persistent volume corrupted | Fall back to git clone + replay browser's unsynced ops |
| OPFS evicted by browser (free tier) | First-load latency, user warned ahead of time via `navigator.storage.persist()` permission prompt at first session |
| OPFS evicted by browser (paid tier) | Re-hydrate from container; expected cost is bandwidth, not data loss |

## 11. Open Questions

1. **Patch format**: whole-file replace vs RFC-6902 JSON patches vs
   diff/patch (binary). Recommendation: whole-file for the first pass,
   measure, optimize if files routinely exceed 100KB.
2. **Patch ordering guarantees**: do we need strict FIFO per file, or
   per workspace? Per-file is sufficient given the staleness rule;
   document it.
3. **Backpressure**: what happens if the browser is slow to acknowledge
   patches and the agent is generating them rapidly? Standard answer:
   apply browser-side, queue server-side, surface lag in the UI.
   Bounded queue prevents unbounded memory growth.

These don't block the design; they're implementation details that get
nailed down in the platform repo.
