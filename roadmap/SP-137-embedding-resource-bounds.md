# SP-137 — Embedding Index Resource Bounds & Gate Consistency

Status: ✅ Implemented (2026-08-16) — Phases 1–3 shipped

## Problem

The embedding subsystem can consume host memory without bound during index
builds, and the experimental opt-in gate is enforced inconsistently across
acquisition paths.

Three gaps:

1. **Gate inconsistency.** `EmbeddingIndexConfig.Experimental` must be set
   alongside `Enabled` for the index to activate (see
   `pkg/configuration/config_domain.go`). `RestoreEmbeddingIndex` enforces
   this, but two acquisition paths construct managers without consulting the
   workspace's stored preference:
   - `cmd/embedding_socket.go` — the daemon's embedding service acquires a
     manager with an empty `EmbeddingIndexConfig` for any workspace a client
     names, so a socket request can activate an index the user never opted
     into for that workspace.
   - `pkg/agent_tools/embedding_index_handler.go` — `pickEmbeddingMgr`
     acquires the shared manager whenever the agent doesn't own one, without
     checking `IsEnabled()`.
2. **Unbounded build memory.** `IndexManager.BuildIndex` caps Go heap via
   `debug.SetMemoryLimit`, but the embedding provider's native allocations
   are invisible to that limit. On large trees the process can grow until
   the kernel OOM killer fires — and the OOM killer's choice of victim is
   not limited to the process that caused the pressure.
3. **OOM victim priority.** A long-lived background daemon that holds
   significant memory is treated as a preferred OOM survivor by default
   (`oom_score_adj` 0), so when memory runs out the kernel kills other
   processes first. A background helper should sacrifice itself before
   user-facing processes do.

## Non-Goals

- Changing the gate semantics (both flags, or the default-off behavior).
- End-to-end host memory management (earlyoom etc.) — host-side tooling is
  the operator's choice.
- Renaming or moving any of the affected packages.

## Design

### Phase 1 — Gate-consistent manager acquisition

- `cmd/embedding_socket.go`: the daemon's `Acquire` callback reads the
  workspace config via the shared `configuration.WorkspaceEmbeddingIndexEnabled`
  helper and returns a distinguishable error ("not enabled for this
  workspace") unless the workspace opted in (`enabled && experimental`), or
  `SPROUT_EXPERIMENTAL_EMBEDDINGS=1` default-on applies. Workspace-scoped op
  clients (query / build / check_duplicates) surface that error to the
  caller; inference ops (meta / embed / embed_batch) carry no workspace root
  and keep their in-process fallback behavior.
- `pkg/agent_tools/embedding_index_handler.go`: `build`/`update` operations
  return a status message ("index not enabled for this workspace; enable
  via /index") when the merged config (`cfg.IsEnabled()`) or the workspace
  config file (`configuration.WorkspaceEmbeddingIndexEnabled`) is not opted
  in, instead of acquiring a manager. `status` remains available without a
  manager.

### Phase 2 — MemoryAvailable floor during builds

- New helper in `pkg/embedding` (build-tagged linux/darwin/other, tests
  injectable): reads `MemAvailable` from `/proc/meminfo` (linux) or
  `vm.stats.vm.v_free_count` equivalent (darwin); other platforms report
  "unknown" and the check is skipped.
- The shared embedding loop — used by `BuildIndex`, `UpdateFile`, and
  `UpdateFromGitDiff` — checks the floor at batch boundaries (and once
  before the loop starts). Below the floor the loop stops and returns the
  floor error alongside any partial records. `BuildIndex` treats the halt
  as a clean stop and keeps the partial results already checkpointed;
  `UpdateFile` / `UpdateFromGitDiff` abort with the floor error so callers
  surface the condition. Default floor: conservative absolute value (e.g.
  1 GiB) chosen so that hitting it on a healthy host indicates a runaway
  build, not normal variance.
- The floor is not configurable in v1; revisit if it misfires.

### Phase 3 — Daemon OOM sacrifice

- On startup, a daemon that was auto-started (`SPROUT_DAEMON_AUTOSTARTED=1`)
  raises its own `oom_score_adj` (via `/proc/self/oom_score_adj`, wrapped so
  non-linux builds no-op) to mark itself a preferred victim. Explicitly
  started daemons (`sprout agent -d`) keep the default — a deliberate
  foreground-adjacent start should not self-sacrifice silently.
- Value: modest positive adjustment (e.g. +200, not the 1000 maximum) —
  preferred victim, but still outranking throwaway container processes at
  1000.

## Testing

- Phase 1: unit tests for the socket `Acquire` gate (enabled+experimental →
  manager; otherwise nil) using temp workspace configs; handler test for
  build/update returning the not-enabled message.
- Phase 2: build-loop test injecting a fake meminfo reader that reports
  below-floor after N batches; assert partial flush and the specific error.
- Phase 3: unit test asserting the oom_score_adj writer is invoked on the
  autostart path and not on the explicit path (fs abstraction or function
  injection).

## Rollout / Compatibility

- Behavior change: socket clients and tool calls that previously
  activated an un-enabled workspace index now get an error/status. This is
  the intended gate enforcement. Workspace-scoped op clients (query / build /
  check_duplicates) receive a distinguishable "not enabled" error they
  surface to the caller; inference ops (meta / embed / embed_batch) carry no
  workspace root and keep their in-process fallback path.
- No config migration required — the gate flags already exist.

## References

- `pkg/configuration/config_domain.go` — `EmbeddingIndexConfig.Experimental`
- `pkg/agent/agent_embedding.go` — `RestoreEmbeddingIndex` (reference gate
  decode)
- `pkg/daemon/embedding_server.go` — socket service
- `pkg/embedding/index.go` — build loop and batch boundaries
