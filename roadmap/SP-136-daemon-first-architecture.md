# SP-136 — Daemon-First Architecture: Shared Process Model

## Problem

Every `sprout` CLI invocation is a standalone OS process that independently
loads the 155 MB ONNX model, opens its own embedding store, and spawns its own
inference threads. With N sprout processes on one machine:

| Resource | Per-process | × 5 processes | Coordinated? |
|----------|-------------|---------------|-------------|
| Model weights | 155 MB | 775 MB | ❌ |
| ONNX threads | 2–8 | 10–40 | ⚠️ Mitigated by `thread_budget.go` (process-count division) |
| Inference permits | 2 | 10 | ❌ |
| **Index store writes** | 1 writer | **5 writers to same dir** | ❌ **Data corruption risk** |

The last row is a correctness bug: `HNSWStore` uses an in-process `sync.Mutex`
with no cross-process locking. Two processes writing to the same workspace
index directory (same slug → same path) corrupt the HNSW graph and records JSON.

The daemon (port 56000) already solves resource deduplication *within* one
process — the `sharedONNXByModel` cache loads the model once, the
`inferenceGate` coordinates concurrent sessions, and the `sharedManager`
registry deduplicates index writers. The missing piece is an IPC path for CLI
processes to use the daemon instead of doing everything themselves.

## Scope

Restructure the native execution model so the daemon (or a lazy-started
background service) becomes the default execution context for all sprout work.
The CLI becomes a thin client that connects to the daemon for embedding
operations, agent execution, and tool dispatch. This consolidates four native
execution paths (interactive CLI, non-interactive CLI, daemon, service) into
one, reducing code complexity and eliminating resource duplication.

WASM is orthogonal — it runs the agent in the browser with no Go backend. This
spec does not change the WASM path.

## Current Architecture

**Execution paths (native Go):**

1. **Interactive CLI** (`sprout`, `sprout agent "query"`) — standalone process,
   full agent + embedding stack, no IPC.
2. **Non-interactive CLI** (`sprout agent --json "query"`) — same, one-shot.
3. **Daemon** (`sprout agent -d`) — long-lived process serving WebUI on port
   56000, creates per-workspace agents on demand.
4. **Service** (`sprout service install`) — daemon managed by launchd/systemd.

**What the daemon already deduplicates (within one process):**
- `sharedONNXByModel` — one model load per (modelDir, model, dims).
- `inferenceGate` — 2-permit pool for concurrent ORT Run calls.
- `sharedManager` (`sharedManagers` registry) — one `EmbeddingManager` per
  (indexDir, workspaceRoot), refcounted.
- Per-workspace agent creation via `clientContext` / `chat_sessions`.

**What is NOT deduplicated (across processes):**
- Model weights, threads, inference permits, index store writes.

**Known daemon issues to fix before CLI depends on it:**
- Folder-scoped WebUI port assignment (5600x vs 56000) — coordination bug.
- Multi-workspace session isolation — needs hardening for concurrent CLI
  sessions across different workspaces.

## Plan

### Phase 0 — File-lock index writes (urgent correctness fix)

**Ship first, independent of the daemon work.** Prevents the data corruption
that exists today regardless of architecture changes.

- `flock` on `indexDir/.build.lock` before any `BuildIndex` or
  `UpdateFromGitDiff` call. If another process holds the lock, skip the build
  (the other process is already doing it).
- Read-only queries need no lock — the on-disk index is loaded into memory
  atomically by `HNSWStore.LoadAll`.
- Fallback: if `flock` is unavailable (WASM, restricted sandbox), proceed
  without locking (current behavior).
- **Gate:** `TestConcurrentBuilders` — two goroutines calling `BuildIndex` on
  the same index dir; verify no corruption.

### Phase 1 — Daemon multi-workspace hardening

Fix the coordination bugs that would make CLI-on-daemon unsafe.

- **Port assignment:** Investigate the 5600x vs 56000 issue. The daemon should
  serve all workspaces from one port (56000), routing internally by workspace
  root. Folder-scoped ports were a stopgap; eliminate or document the
  constraint.
- **Session isolation:** Verify that N concurrent CLI sessions across K
  workspaces get isolated agents, isolated embedding managers, and isolated
  stores. The `clientContext` and `chat_sessions` machinery exists for the
  WebUI; CLI sessions need the same isolation guarantees.
- **Graceful degradation:** If the daemon is unhealthy (OOM, stuck inference),
  connected CLI clients must detect this and fall back to in-process execution
  with a clear warning, not hang silently.
- **Gate:** Integration test — 5 concurrent agent sessions across 3 workspaces,
  all complete successfully; daemon RSS stays bounded.

### Phase 2 — Lazy daemon auto-start

When `sprout` starts and no daemon is detected, automatically start one in the
background and connect to it.

- **Detection:** Check for a health endpoint (`GET /health` on port 56000 or a
  Unix socket at `~/.local/share/sprout/daemon.sock`). If healthy, connect. If
  not, start a daemon process.
- **Startup race:** Use a PID file + `flock` on `daemon.pid` to elect a single
  starter. Losers wait for the winner's daemon to be healthy, then connect.
- **Lifecycle:** The daemon stays alive while it has active client connections.
  A shutdown timer (e.g. 60s after the last client disconnects) reaps idle
  daemons. New connections arriving during teardown cancel the timer.
- **Auth:** Unix socket with filesystem permissions (0600, owner-only). No
  network exposure.
- **Fallback:** If the daemon fails to start within N seconds (e.g. 10s), the
  CLI falls back to in-process execution. This preserves the "sprout always
  works" guarantee.
- **`SPROUT_DAEMON=0`:** Env-var escape hatch to force in-process execution.
- **Gate:** `sprout` in a clean environment starts the daemon, connects,
  executes a query, disconnects; daemon shuts down after timeout.

### Phase 3 — Embedding service via daemon socket

CLI processes route embedding operations through the daemon.

- **Protocol:** Minimal JSON-over-Unix-socket: `Embed(text) → []float32`,
  `Query(text, k, threshold) → results`, `BuildIndex(workspaceRoot) → stats`,
  `CheckDuplicates(filePath, content) → matches`.
- **`RemoteEmbeddingProvider`:** Implements `EmbeddingProvider`, proxies to
  the daemon socket. If the socket is unavailable, falls back to
  in-process ONNX.
- **The daemon owns:** The sole model copy, the sole writer to each
  workspace's index, coordinated inference permits.
- **Connection management:** Pool of socket connections (or multiplex over
  one). Reconnect on transient failure. The `inferenceGate` in the daemon
  coordinates; CLI clients just send requests.
- **Gate:** 3 CLI processes querying the same workspace — all get results
  from one model load, one index, zero corruption.

### Phase 4 — Full CLI-on-daemon (agent execution)

The CLI becomes a thin client for all agent work, not just embeddings.

- **Protocol expansion:** `Query(prompt) → stream`, `ExecuteTool(name, args)
  → result`, `ListSessions() / CreateSession() / SwitchSession()`.
- **The daemon owns:** Agent state, conversation history, tool dispatch,
  embedding index. The CLI is a presentation layer (terminal rendering,
  input handling, streaming output).
- **One-shot mode:** `sprout agent --json "query"` connects to the daemon,
  runs the query, prints the JSON result, disconnects.
- **Fallback retained:** If the daemon is unavailable and auto-start fails,
  fall back to the current in-process path. This is the safety net that makes
  the migration reversible.
- **`daemonMode` cleanup:** Once the CLI-on-daemon path is default, the
  `daemonMode` flag branches can be simplified — the distinction between
  "CLI that embeds the web server" and "daemon that serves the web server"
  collapses into one code path.
- **Gate:** Full CLI session over daemon — interactive mode, streaming,
  tools, subagents, git operations — indistinguishable from today's
  in-process CLI.

## Risks

1. **Crash blast radius** — a daemon crash kills all sessions. Mitigation:
   daemon panic recovery, session state persisted to disk so sessions can
   resume after a daemon restart.
2. **Socket protocol evolution** — version skew between CLI and daemon.
   Mitigation: protocol version negotiation at connect time.
3. **Lazy-start latency** — first CLI invocation pays daemon startup cost.
   Mitigation: background daemon start is async; CLI can proceed in-process
   while the daemon warms up, then switch over.
4. **Complexity budget** — this is the largest architecture change since the
   daemon was introduced. Each phase must ship independently and be
   revertable.
5. **Platform constraints** — Unix sockets need filesystem access; Windows
   uses named pipes. Service managers vary by platform.

## Out of scope

- WASM execution path (browser-side, no Go backend).
- Cloud/remote daemon (this spec is local-only; remote is a future extension).
- Changing the daemon's HTTP/WebSocket API for the WebUI (unchanged; the CLI
  uses a separate Unix-socket protocol).
- Eliminating the in-process fallback (it stays as the safety net).
