# SP-028: Test Suite Stabilization — Deadlock Resolution & CI Hardening

**Status:** 📋 Proposed
**Date:** 2026-05-18
**Priority:** CRITICAL (blocks reliable regression detection)
**Depends on:** None
**Related:** SP-008 (Reliability Engineering — long-term concurrency audit)

## Problem

`go test ./...` cannot complete reliably. Two packages — `pkg/agent` and `pkg/webui` — hang past the 60s timeout, so CI cannot tell us when concurrency or terminal-session regressions ship. SP-008 plans a broader concurrency audit; this spec is the narrow, urgent fix that gets the suite green again so SP-008 has a working baseline to build on.

### Concrete failure sites

1. **MCP initialization deadlock under contention.** `pkg/agent/mcp_concurrency_test.go:264-296` (`TestMCPConcurrency_StressTest`) spawns 200 goroutines × 10 iterations all calling `agent.getMCPTools()`. That path invokes `AgentMCPManager.LockInit()` (`pkg/agent/submanager_mcp.go:73`), which is also called transitively from `pkg/agent/mcp.go:162` and `pkg/agent/mcp.go:184`. Under 2000-way contention the test never returns. Symptom: 60s timeout with all goroutines parked on the init mutex.

2. **WebUI PTY reader goroutine leak.** `pkg/webui/terminal_create.go:146-175` runs an infinite `for { pty.Read(buf) }` loop. The only exits are `pty == nil` or read error. Tests that create a terminal session and finish without closing the PTY leave that goroutine blocked in `pty.Read()` forever. The `pkg/webui` package test times out at 60s with PTY readers and HTTP/2 transport goroutines still alive in the dump.

## Goals / Non-Goals

**Goals**
- `go test -race ./pkg/agent/... ./pkg/webui/...` finishes within 90s without deadlocks or leaked goroutines.
- The MCP stress test exercises real contention without livelock.
- WebUI tests release every PTY they create.
- CI fails loudly on goroutine-count regressions rather than silently timing out.

**Non-Goals**
- The full concurrency / locking audit and channel-conversion work in SP-008 — that remains separate.
- Refactoring `AgentMCPManager` beyond what is required to remove the deadlock.
- Rewriting the WebUI terminal layer (covered by SP-011 / SP-014).

## Current State

| Area | File | Issue |
|------|------|-------|
| MCP init lock | `pkg/agent/submanager_mcp.go:73` (`LockInit`) | Re-entered transitively from `mcp.go:162`/`:184` under high contention; no observed timeout, no fast-path for "already initialized" check before lock |
| MCP stress test | `pkg/agent/mcp_concurrency_test.go:264` | 200×10 = 2000 concurrent init attempts; no `t.Deadline` guard |
| WebUI PTY reader | `pkg/webui/terminal_create.go:146-175` | `pty.Read()` has no read deadline; goroutine cannot be cancelled from outside; test cleanup not enforced |
| Default test target | `Makefile` | `make test` does not run `-race`; only `make test-race` does, and that uses `-short` which skips this suite |
| CI race step | `.github/workflows/build.yml` | Uses `-short`, hiding the very tests that would catch this |

## Proposed Solution

### Track A — Fix the MCP init deadlock

1. **Add a double-checked fast path to `getMCPTools`.** Before calling `LockInit()`, read the initialization flag under an `RWMutex.RLock()`. Only take the write lock if init is still pending. This collapses 2000 concurrent init attempts to a single one plus 1999 RLock readers — the common case.
2. **Make `LockInit` re-entrant-safe (or prove it doesn't need to be).** Audit every transitive caller in `pkg/agent/mcp.go` and confirm none holds another lock that `LockInit` itself eventually tries to take. Document the lock order in `submanager_mcp.go`.
3. **Bound the stress test.** Change `TestMCPConcurrency_StressTest` to either (a) call a real `Stop()`/`Close()` between phases, or (b) skip when `testing.Short()` and run nightly only. The current numbers (200×10) test the *test*, not the production lock contention. 32×50 with proper cleanup is more representative.
4. **Add a `t.Cleanup(func() { agent.Shutdown() })`** so leaked goroutines don't pollute later tests.

### Track B — Fix the WebUI PTY goroutine leak

1. **Give the reader an exit signal.** Add a `done chan struct{}` to the terminal session and select on it alongside the read. Use `pty.SetDeadline()` (or a periodic short read with deadline) so the goroutine periodically rechecks `done`.
2. **Enforce close in tests.** Add `t.Cleanup(session.Close)` everywhere a terminal session is created in tests.
3. **Add a goroutine-leak detector** to `pkg/webui` tests using `goleak.VerifyNone(t)` in `TestMain` so leaks fail the test instead of hanging it.

### Track C — CI hardening

1. **Move `-race` into the default `make test` target** for `pkg/agent` and `pkg/webui`. The wider migration to `-race` everywhere stays in SP-008.
2. **Drop `-short` from the race step in `.github/workflows/build.yml`** for these two packages so the contention tests actually run.
3. **Add a per-package test timeout of 90s** so a future hang fails CI in 90s instead of running until the global job timeout.
4. **Add `-count=1`** to defeat the test cache so flaky-when-cold deadlocks are caught.

## Implementation Phases

### Phase 1: Unblock CI (Day 1-2)

- [ ] Add `t.Cleanup` + `goleak` to the two failing test packages so the *symptom* (silent hang) becomes a fast, loud failure.
- [ ] Add the 90s per-package timeout + `-count=1` to `Makefile` and `.github/workflows/build.yml`.

### Phase 2: Fix the deadlock (Day 2-4)

- [ ] Add the RWMutex fast path to `getMCPTools` in `pkg/agent/mcp.go`.
- [ ] Audit `LockInit` callers and document lock order in `pkg/agent/submanager_mcp.go`.
- [ ] Reduce `TestMCPConcurrency_StressTest` to 32×50 with `t.Cleanup`.
- [ ] Verify with `go test -race -run TestMCPConcurrency -count=20 ./pkg/agent/`.

### Phase 3: Fix the PTY leak (Day 3-5)

- [ ] Add `done` channel + cancellable read loop to `pkg/webui/terminal_create.go`.
- [ ] Wire `t.Cleanup(session.Close)` into the WebUI test helpers.
- [ ] Confirm `goleak` reports zero leaks across `go test -race -count=5 ./pkg/webui/`.

### Phase 4: Sustain (Week 2)

- [ ] Add `concurrency_test.go`-style regression cases pinning the new invariants.
- [ ] Document the lock order and PTY lifecycle in package-level doc comments.

## Success Criteria

| Metric | Target |
|--------|--------|
| `go test -race -timeout=90s ./pkg/agent/...` | Pass 20× in a row |
| `go test -race -timeout=90s ./pkg/webui/...` | Pass 20× in a row |
| `goleak.VerifyNone` failures | 0 |
| CI total test time (race step) | Within +20% of current short run |
| Stress test goroutines | 32 × 50 (with cleanup) instead of 200 × 10 (without) |

## Files Reference

| File | Action |
|------|--------|
| `pkg/agent/submanager_mcp.go` | Modify: document lock order; verify no re-entrant takers |
| `pkg/agent/mcp.go` | Modify: add RWMutex fast path before `LockInit()` (lines 162, 184) |
| `pkg/agent/mcp_concurrency_test.go` | Modify: reduce stress factors; add `t.Cleanup`; line 264 |
| `pkg/webui/terminal_create.go` | Modify: cancellable read loop with `done` channel (lines 146-175) |
| `pkg/webui/terminal_session.go` | Modify: expose `Close()`, signal `done` |
| `pkg/webui/*_test.go` | Modify: `t.Cleanup(session.Close)` everywhere |
| `pkg/agent/concurrency_test.go` | Create: regression pin (new file from SP-008's Track A3) |
| `pkg/webui/main_test.go` | Create: `TestMain` with `goleak.VerifyNone` |
| `Makefile` | Modify: `-race -count=1 -timeout=90s` on `pkg/agent` and `pkg/webui` in default test target |
| `.github/workflows/build.yml` | Modify: drop `-short` from race step for these packages |
| `go.mod` | Modify: add `go.uber.org/goleak` test dependency |

## Risks

- **Fast-path may mask a real bug.** If `LockInit` *needs* to be called every time for state-refresh reasons, a fast path will skip it. Mitigation: read the function carefully, and if init has side effects beyond a flag-set, keep them but extract the idempotent guard.
- **Goleak false positives.** Background goroutines from `pkg/logging` or `pkg/history` may leak intentionally for the process lifetime. Mitigation: use `goleak.IgnoreTopFunction(...)` for known long-lived workers.
- **PTY deadline support.** `creack/pty` may not implement `SetDeadline` on all platforms. Mitigation: fall back to a polling read with a short blocking duration.
