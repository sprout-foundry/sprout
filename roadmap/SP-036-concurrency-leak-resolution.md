# SP-036: Concurrency Leak Resolution — Removing the goleak Allowlist

**Status:** 📋 Proposed
**Date:** 2026-05-19
**Priority:** HIGH (production reliability — long-running daemons accumulate goroutines)
**Depends on:** SP-028 (Test Suite Stabilization) — landed; this is the follow-on cleanup
**Related:** SP-008 (Reliability Engineering), SP-032 (Daemon Mode Hardening — shutdown path), SP-014 (Agent Terminal Sessions)

## Problem

SP-028 unblocked CI by adding `goleak.VerifyNone` to `pkg/webui/main_test.go` and `pkg/agent/main_test.go`. To get the suite green, four real leaks were silenced via `IgnoreTopFunction` / `IgnoreAnyFunction` rather than fixed at the source. The allowlist is now **the regression detector for goroutine leaks**, and it is currently masking production-relevant leaks:

### Concrete leaks being masked

1. **`pkg/webui.(*fileWatcher).start.func1`** — `pkg/webui/main_test.go:19`. Fsnotify-based file watcher launched from the webui; the goroutine has no exit channel and is never joined on shutdown. Each watcher creation in tests leaves one orphaned goroutine plus a held file descriptor.
2. **`pkg/lsp/proxy.(*Manager).cleanupLoop`** — `pkg/webui/main_test.go:20`. Periodic ticker-driven cleanup; the loop runs on `time.Ticker` with no `case <-done` arm. Manager.Shutdown does not signal it.
3. **`pkg/webui.(*TerminalManager).ExecuteCommandAndWait`** + `.func1` — `pkg/webui/main_test.go:21-22`. The synchronous one-shot exec helper spawns a watcher goroutine via `os/exec.(*Cmd).watchCtx` that survives past the call return when the test does not drain stdout/stderr or when the process exits via a path the watcher does not see.
4. **`fsnotify.(*shared).sendEvent`** — both files. Shared fsnotify worker; may be a genuine library limitation (one goroutine per process, lifetime-of-binary), but no one has verified that — it is currently allowlisted on faith.
5. **`os/exec.(*Cmd).watchCtx`** — both files. Spawned by `exec.CommandContext` when the context has cancellation; leaks when the `Cmd` is not `Wait()`-ed or the context lives longer than the process.

### Why the allowlist is the wrong place to leave these

- `IgnoreTopFunction("…fileWatcher.start.func1")` matches **every** instance of a leaked fileWatcher, not just the known-safe ones. A future code path that leaks a *second* watcher passes silently.
- The allowlist hides the cost: each leaked goroutine retains its goroutine stack (~8KB), any captured closure state, and any held resources (file descriptors, network connections). A daemon that creates 10,000 terminal sessions over a week accumulates ~80MB of pure goroutine waste plus FD pressure.
- SP-028's own "Risks" section called this out: *"Goleak false positives. Background goroutines from pkg/logging or pkg/history may leak intentionally for the process lifetime. Mitigation: use goleak.IgnoreTopFunction(...) for known long-lived workers."* The mitigation was applied to **non-intentional** leaks. That promise has not been kept.

## Goals / Non-Goals

**Goals**
- Every `IgnoreTopFunction` / `IgnoreAnyFunction` in `pkg/webui/main_test.go` and `pkg/agent/main_test.go` is either (a) removed because the leak is fixed, or (b) retained with a `// REASON:` comment proving it is fundamental to a third-party library and not our code.
- A scoped `Shutdown()` call on `TerminalManager`, `fileWatcher`, and LSP `proxy.Manager` returns within 5s with zero residual goroutines.
- A regression test pins each fix so re-introducing the leak fails CI loudly.

**Non-Goals**
- Replacing fsnotify or refactoring the LSP proxy architecture (covered by SP-008 / SP-014).
- Fixing leaks outside `pkg/webui` and `pkg/agent` (e.g., test-only helpers).
- Eliminating `IgnoreTopFunction("syscall.Syscall")` and `internal/poll.runtime_pollWait` — those are runtime-level and genuinely fundamental.

## Current State

| Allowlist entry | File:Line | Verdict |
|-----------------|-----------|---------|
| `pkg/webui.(*fileWatcher).start.func1` | `pkg/webui/main_test.go:19` | Fixable — add `done` chan, signal from `Stop()` |
| `pkg/lsp/proxy.(*Manager).cleanupLoop` | `pkg/webui/main_test.go:20` | Fixable — add cancellation via `context.Context` on `NewManager` |
| `pkg/webui.(*TerminalManager).ExecuteCommandAndWait` | `pkg/webui/main_test.go:21-22` | Fixable — ensure `Wait()` is always called and `watchCtx` joins |
| `os/exec.(*Cmd).watchCtx` (AnyFunction) | both files:20 / :28 | Symptom of leaked `Cmd`s — should drop after #3 is fixed |
| `fsnotify.(*shared).sendEvent` (AnyFunction) | both files:19 / :27 | Investigate — likely intentional library-lifetime; if so, document with rationale and keep |
| `internal/poll.runtime_pollWait` (AnyFunction) | both files:21 / :29 | Fundamental — keep with comment |
| `syscall.Syscall` (TopFunction) | both files:27 / :35 | Fundamental — keep with comment |

## Proposed Solution

### Track A — Fix `fileWatcher` shutdown (`pkg/webui`)

A1. Locate the fileWatcher struct definition (`grep -n "type fileWatcher" pkg/webui/`). Add a `done chan struct{}` field and a `Stop()` method that closes it idempotently (via `sync.Once`).

A2. In `start()` (the goroutine entry point currently leaking as `func1`), wrap the fsnotify event loop in a `for { select { case <-done: return; case ev := <-watcher.Events: … } }`. Ensure the underlying `*fsnotify.Watcher` is `.Close()`-d in the `done` arm.

A3. Find every caller of `fileWatcher.start` and ensure they hold a reference and call `Stop()` in their own shutdown path. `grep -rn "fileWatcher{" pkg/webui/`.

A4. Add `t.Cleanup(func() { fw.Stop() })` to any test that directly instantiates a `fileWatcher`.

A5. Remove `goleak.IgnoreTopFunction("github.com/sprout-foundry/sprout/pkg/webui.(*fileWatcher).start.func1")` from `pkg/webui/main_test.go:19`. CI must stay green.

### Track B — Cancel `proxy.Manager.cleanupLoop` (`pkg/lsp/proxy`)

B1. Locate `cleanupLoop` (`grep -n "cleanupLoop" pkg/lsp/proxy/`). Confirm whether the `Manager` already has a `context.Context` or `done` channel — if so, this is a one-line fix (add the `select` arm). If not, plumb a `context.Context` into `NewManager`.

B2. Add a `Shutdown(ctx context.Context) error` method on `proxy.Manager` that signals the loop, waits up to the context deadline, and returns. Ensure idempotent (multiple calls are no-ops).

B3. Wire `Shutdown` into the webui `ReactWebServer` lifecycle (`pkg/webui/server_lifecycle.go`) alongside the existing `terminalManager` shutdown.

B4. Remove `goleak.IgnoreTopFunction("…/pkg/lsp/proxy.(*Manager).cleanupLoop")` from `pkg/webui/main_test.go:20`.

### Track C — Drain `ExecuteCommandAndWait` (`pkg/webui`)

C1. Read `pkg/webui/TerminalManager.ExecuteCommandAndWait` (find it: `grep -rn "ExecuteCommandAndWait" pkg/webui/`). Identify the missing-Wait path. Hypothesis: stdout/stderr pipes are not fully drained before `cmd.Wait()`, leaving `watchCtx` blocked on a closed-but-not-drained pipe.

C2. Convert the function to use `exec.CommandContext` with a derived context that is cancelled in `defer` *before* `Wait()` returns — guarantees `watchCtx` exits.

C3. Use `io.Copy` to drain to a `bytes.Buffer` in goroutines that themselves join with `errgroup.Group` before `cmd.Wait()`.

C4. Add a test `TestExecuteCommandAndWait_NoGoroutineLeak` in `pkg/webui/terminal_*_test.go` that runs the helper 100 times in a loop and asserts `runtime.NumGoroutine()` stays bounded.

C5. Remove the two `ExecuteCommandAndWait` entries from `pkg/webui/main_test.go:21-22` and the `os/exec.(*Cmd).watchCtx` ignore at line 28 (if no other leak source remains).

### Track D — Document or escalate the fsnotify shared worker

D1. Trace where `fsnotify.(*shared).sendEvent` originates: read fsnotify v1.9 source. Determine whether `sendEvent` is one-per-`Watcher` or one-per-process. If one-per-process, document with a `// REASON:` comment in both `main_test.go` files (~3 lines of justification, linking to the upstream source).

D2. If one-per-`Watcher`, this is the same root cause as Track A — fixing the fileWatcher Close() will drop it. Remove the allowlist entry and verify.

### Track E — Regression pinning

E1. Add `TestNoNewGoroutineLeaks_Webui` and `TestNoNewGoroutineLeaks_Agent` that:
  - Snapshot goroutines at start.
  - Run a representative workload (create + close fileWatcher, start + stop LSP manager, exec a command via TerminalManager).
  - Assert delta <= a small constant (e.g., 2 for thread-local runtime workers).

E2. Add a `make test-leak` target that runs `go test -race -count=10` on `pkg/webui` and `pkg/agent` with verbose goleak output — useful for local verification before pushing.

## Implementation Phases

### Phase 1: Investigation
- [ ] SP-036-1a: Read each leaking goroutine's source. Confirm root cause for each of the 4 allowlist entries.
- [ ] SP-036-1b: Decide per-entry: fix vs. document vs. defer to upstream. Write the verdict into this spec.

### Phase 2: Track A — fileWatcher
- [ ] SP-036-2a: Add `done chan struct{}` + `sync.Once`-guarded `Stop()` to `fileWatcher` in `pkg/webui/`.
- [ ] SP-036-2b: Convert `start()` event loop to `select` on `done` + fsnotify events; `.Close()` the watcher in the done arm.
- [ ] SP-036-2c: Audit every `fileWatcher{…}` instantiation site for `Stop()` call in shutdown path.
- [ ] SP-036-2d: Add `t.Cleanup` to tests that create watchers directly.
- [ ] SP-036-2e: Remove `pkg/webui/main_test.go:19` allowlist entry. Verify with `go test -race -count=5 ./pkg/webui/`.

### Phase 3: Track B — LSP proxy
- [ ] SP-036-3a: Plumb `context.Context` into `pkg/lsp/proxy.NewManager` (or use existing if present).
- [ ] SP-036-3b: Add `select` on `ctx.Done()` in `cleanupLoop`.
- [ ] SP-036-3c: Add idempotent `Shutdown(ctx)` method; wire into `pkg/webui/server_lifecycle.go`.
- [ ] SP-036-3d: Remove `pkg/webui/main_test.go:20` allowlist entry.

### Phase 4: Track C — ExecuteCommandAndWait
- [ ] SP-036-4a: Refactor `ExecuteCommandAndWait` to use `exec.CommandContext` + `errgroup` for pipe draining.
- [ ] SP-036-4b: Add `TestExecuteCommandAndWait_NoGoroutineLeak` (100 iterations, NumGoroutine bound).
- [ ] SP-036-4c: Remove the two `ExecuteCommandAndWait` allowlist entries (`pkg/webui/main_test.go:21-22`) and the corresponding `watchCtx` AnyFunction ignore if no longer needed.

### Phase 5: Track D — fsnotify investigation
- [ ] SP-036-5a: Trace `fsnotify.(*shared).sendEvent` in upstream v1.9 source; confirm scope (per-Watcher vs per-process).
- [ ] SP-036-5b: Either remove the AnyFunction allowlist or replace it with a `// REASON: fsnotify v1.9 maintains a process-lifetime worker — see <upstream link>` comment.

### Phase 6: Track E — regression pinning + docs
- [ ] SP-036-6a: Add `TestNoNewGoroutineLeaks_*` tests in both packages.
- [ ] SP-036-6b: Add `make test-leak` Makefile target.
- [ ] SP-036-6c: Add a package-level doc comment to `pkg/webui/terminal_create.go` and `pkg/webui/file_watcher.go` (or wherever fileWatcher lives) documenting the shutdown contract.

## Success Criteria

| Metric | Target |
|--------|--------|
| `IgnoreTopFunction` entries for our-code goroutines in `pkg/webui/main_test.go` | 0 |
| `IgnoreTopFunction` entries for our-code goroutines in `pkg/agent/main_test.go` | 0 |
| Remaining allowlist entries | Only `syscall.Syscall`, `internal/poll.runtime_pollWait`, and at most one fsnotify entry — each with a `// REASON:` comment |
| `make test-leak` | Passes 10× in a row on both packages |
| `runtime.NumGoroutine()` delta in regression tests | ≤ 2 after representative workload |

## Files Reference

| File | Action |
|------|--------|
| `pkg/webui/main_test.go` | Modify: remove lines 19-22, 28 allowlist entries after fixes land |
| `pkg/agent/main_test.go` | Modify: remove line 20 (`watchCtx`) after Track C |
| `pkg/webui/file_watcher.go` (or equivalent) | Modify: add `done` channel + `Stop()` |
| `pkg/lsp/proxy/manager.go` (or equivalent) | Modify: add context plumbing + `Shutdown()` |
| `pkg/webui/terminal_*.go` | Modify: refactor `ExecuteCommandAndWait` for clean exec lifecycle |
| `pkg/webui/server_lifecycle.go` | Modify: wire LSP manager shutdown alongside terminal manager |
| `pkg/webui/leak_regression_test.go` | Create: representative-workload leak tests |
| `pkg/agent/leak_regression_test.go` | Create: same for agent package |
| `Makefile` | Modify: add `test-leak` target |

## Risks

- **Stop() races during init.** If `Stop()` is called before `start()` begins, the goroutine may miss the `done` signal. Mitigation: use `sync.Once` and ensure `start()` checks `done` first thing in the loop.
- **Test flakes from real cancellation.** Cancelling exec contexts mid-flight may surface as new test failures elsewhere. Mitigation: land each Track independently with its own CI run.
- **Upstream fsnotify behavior may change.** A future fsnotify version may eliminate the shared worker, breaking the allowlist comment. Mitigation: the comment includes the version (`fsnotify v1.9`) so a `go.mod` bump triggers re-investigation.
- **Hidden second leak.** Removing the `IgnoreTopFunction` for fileWatcher may surface a *different* fileWatcher caller leaking. Mitigation: that's the whole point — make it loud, fix it, repeat.
