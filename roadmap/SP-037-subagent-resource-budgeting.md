# SP-037: Subagent Resource Budgeting — Bounded Parallelism

**Status:** 📋 Proposed
**Date:** 2026-05-19
**Priority:** HIGH (resource exhaustion under adversarial or runaway agent plans)
**Depends on:** None (SP-023 in-process subagents landed; this constrains it)
**Related:** SP-026 (Executive Assistant — primary caller of parallel subagents), SP-023 (In-Process Subagents)

## Problem

`SubagentRunner.RunParallel` at `pkg/agent/subagent_runner.go:108` spawns one goroutine per task with **no upper bound, no queue, and no resource accounting.**

```go
func (r *SubagentRunner) RunParallel(ctx context.Context, tasks []SubagentTask, opts SubagentOptions) []*SubagentResult {
	// ...
	var wg sync.WaitGroup            // line 114
	// ...
	go func(idx int, t SubagentTask) {  // line 124 — N goroutines, N agents, N LLM clients
		defer wg.Done()
		// build Agent, run conversation loop, call LLM
	}(i, task)
```

Each spawned subagent constructs a full `Agent` struct (its own conversation state, sub-managers, MCP connections, LLM `ClientInterface`). The cost per subagent is **not small**: an HTTP connection pool, system prompt tokens, embedding-store handle, optional persona overrides. A misbehaving orchestrator that schedules 100 parallel subagents creates 100 LLM clients and 100 conversation contexts simultaneously.

### Concrete failure modes

1. **Provider rate-limit storms.** 100 concurrent requests to a provider with a 60 req/min limit produces an immediate 429 cascade — every subagent retries, multiplying the storm.
2. **Token cost runaway.** No aggregate budget across the fleet. An EA running a "research everything" plan can spend a workspace's monthly token allowance in seconds without any single subagent exceeding its individual budget.
3. **Goroutine + memory pressure.** Each Agent holds ~MB of conversation state and a few goroutines (LLM stream reader, MCP heartbeats, embedding writes). 100 parallel = 100s of MB + a few hundred goroutines.
4. **MCP connection multiplication.** Every subagent that uses MCP tools attempts to initialize its own MCP connection unless caching is shared. SP-028 Phase 2 made MCP init safer under contention, but the resource cost is still N×.
5. **Cascading depth.** Although depth is capped at 2 (SP-026 Phase A), a depth-1 orchestrator can still spawn N parallel subagents at depth 2. With N=20, that's 20 simultaneous tool-using agents — way past any human-meaningful parallelism.

### Test coverage gap

Subagent parallel tests (`pkg/agent/tool_handlers_subagent_parallel.go` and its tests) exercise ≤5 parallel tasks. The unbounded path has never been stressed in CI.

## Goals / Non-Goals

**Goals**
- A hard upper bound on concurrent in-flight subagents per process, configurable, with a safe default.
- A FIFO queue for excess tasks so `RunParallel(tasks=N)` still completes correctly when `N > limit`, just serially in batches.
- Aggregate token-budget enforcement across the subagent fleet for a single parent turn.
- Telemetry: active count, queued count, completed, failed, queued-wait time. Surfaced in the WebUI Subagents tab and runlogs.
- A stress test that exercises 50 queued tasks against a limit of 4 — runs to completion with zero leaked goroutines.

**Non-Goals**
- Per-provider rate-limit shaping (different problem; goes in agent_providers).
- Cross-process budget enforcement (single-process scope only).
- Reworking the subagent type model or persona dispatch.
- Distributed scheduling — this is in-process only.

## Current State

| Concern | File:Line | Issue |
|---------|-----------|-------|
| Unbounded goroutine fanout | `pkg/agent/subagent_runner.go:124` | `go func` per task; no semaphore |
| No queueing | `pkg/agent/subagent_runner.go:108-140` | All tasks spawn immediately or not at all |
| No aggregate token budget | `pkg/agent/subagent_runner.go:25` | `MaxTokens` is per-subagent only |
| No telemetry | `pkg/agent/subagent_runner.go` whole-file | No counters, no event emission |
| Stress test ceiling | `pkg/agent/tool_handlers_subagent_*_test.go` | Highest observed parallelism in tests: ~5 |
| Depth gate only | `pkg/agent/agent.go` (subagentDepth) | Depth caps nesting; does not cap breadth |

## Proposed Solution

### Track A — Bounded execution

A1. **Add `MaxConcurrentSubagents`** to `SubagentOptions` in `pkg/agent/subagent_runner.go:20-30`. Type `int`. Zero means "use config default". Negative means "unlimited (caller asserts they know)".

A2. **Add `MaxConcurrentSubagents` to global config** in `pkg/configuration/config.go` (SubagentConfig section). Default `4`. Cap at `16` regardless of user setting unless `unsafe_unbounded_subagents: true`.

A3. **Replace the unbounded `go func` in `RunParallel`** with a `chan struct{}` semaphore of capacity `MaxConcurrentSubagents`. Acquire before `go func`, release in `defer`.

A4. **Make `RunParallel` block-and-batch.** When `len(tasks) > limit`, the function still returns one `*SubagentResult` per task in original order; internally it batches. Callers do not need to change.

A5. **Cancellation cascade.** If the parent `ctx` is cancelled, drop queued (not-yet-started) tasks immediately with `Result{Cancelled: true}`; let in-flight subagents observe the same context.

### Track B — Aggregate token budget

B1. **Add `FleetTokenBudget int`** to `SubagentOptions`. Zero means "no fleet cap". Non-zero means: the sum of `result.TokensUsed` across all subagents for this `RunParallel` invocation must not exceed `FleetTokenBudget`.

B2. **Implement a budget tracker.** `type fleetBudget struct { sync.Mutex; remaining int; failed bool }`. Each subagent, after each LLM call, attempts to debit its delta from the shared budget. On overdraw, the budget marks itself failed and cancels the shared context; in-flight subagents observe cancellation and return.

B3. **Decision rule.** A subagent that has already consumed its individual quota at the moment the fleet budget hits zero keeps its accumulated work and returns `Result{Truncated: true, Reason: "fleet budget exhausted"}`. A subagent that has not yet started gets `Result{Cancelled: true}`.

B4. **Surface fleet budget in EA persona.** Default `FleetTokenBudget = 200_000` for EA-spawned RunParallel. Configurable via persona JSON.

### Track C — Telemetry

C1. **Per-runner counters.** Add to `SubagentRunner`:
```go
type SubagentRunnerMetrics struct {
    Active, Queued, Completed, Failed, Cancelled int64
    TotalQueuedWaitMS int64
}
```
All counters are `atomic.Int64`.

C2. **Emit subagent lifecycle events.** Hook into `pkg/events/` (already used elsewhere) to emit:
  - `subagent.queued{id, parent_id}`
  - `subagent.started{id, queued_wait_ms}`
  - `subagent.completed{id, tokens_used, duration_ms}`
  - `subagent.cancelled{id, reason}`

C3. **WebUI panel update.** Add a "Resource Usage" row to `webui/src/components/Subagents*Tab*` (or `packages/ui/.../SubagentsPanel`) showing live counts.

C4. **Runlog entries.** Write the same events to `~/.sprout/runlogs/*.jsonl` so post-mortems can reconstruct the fleet behavior.

### Track D — Stress test + regression

D1. **`TestSubagentRunner_BoundedConcurrency`** in `pkg/agent/subagent_runner_test.go` — submit 50 tasks against limit=4 using a stub LLM client that sleeps 100ms; assert max concurrent ≤ 4 throughout, all 50 complete, ordering is preserved in result slice.

D2. **`TestSubagentRunner_FleetBudgetCancels`** — submit 10 tasks with fleet budget = 5000 tokens, stub client returns 600 tokens per call; assert at least one task returns `Cancelled`, total tokens consumed ≤ fleet budget + one subagent's worth of overdraw tolerance.

D3. **`TestSubagentRunner_NoGoroutineLeak_AfterStress`** — runs the bounded concurrency stress test, then checks `goleak.VerifyNone(t)` and `runtime.NumGoroutine()` delta ≤ 2.

D4. **`TestSubagentRunner_ParentCancelDropsQueued`** — submit 20 tasks with limit=2, cancel parent context after 50ms; assert in-flight ≤ 2 finish (possibly cancelled), remaining 18 return `Cancelled` without ever starting.

## Implementation Phases

### Phase 1: Bounded execution

[ ] SP-037-1a: Add `MaxConcurrentSubagents` field to `SubagentOptions` (`pkg/agent/subagent_runner.go:20-30`).
[ ] SP-037-1b: Add `MaxConcurrentSubagents` to `SubagentConfig` in `pkg/configuration/config.go`. Default 4; document the cap.
[ ] SP-037-1c: Replace the unbounded `go func` at `pkg/agent/subagent_runner.go:124` with a semaphore-guarded spawn. Acquire before `go`, release in defer.
[ ] SP-037-1d: Verify `RunParallel(N>limit)` still returns `len(results) == len(tasks)` in original order. Add `TestSubagentRunner_OrderPreservedUnderBatching`.
[ ] SP-037-1e: Wire cancellation: parent ctx done drops queued tasks with `Cancelled: true`.

### Phase 2: Fleet token budget

[ ] SP-037-2a: Add `FleetTokenBudget int` to `SubagentOptions`; default 0 = unlimited.
[ ] SP-037-2b: Implement `fleetBudget` struct with atomic debit and overdraw-detection cancellation.
[ ] SP-037-2c: Hook budget debit into the per-subagent LLM-call wrapper (likely in `pkg/agent/conversation.go` or a new helper).
[ ] SP-037-2d: Add `FleetTokenBudget: 200000` default to `pkg/personas/configs/executive_assistant.json`. Confirm SP-035 Track A explicit-policy approach is followed.

### Phase 3: Telemetry

[ ] SP-037-3a: Add atomic counters to `SubagentRunner`; expose via `Metrics()` accessor.
[ ] SP-037-3b: Emit `subagent.queued/started/completed/cancelled` events through `pkg/events/`.
[ ] SP-037-3c: Add a Subagents resource-usage row to `webui/src/components/.../SubagentsTab.tsx` (or `packages/ui/.../SubagentsPanel`).
[ ] SP-037-3d: Write events to runlog (`pkg/logging/`).

### Phase 4: Stress + regression

[ ] SP-037-4a: Add `TestSubagentRunner_BoundedConcurrency` (50 tasks, limit=4).
[ ] SP-037-4b: Add `TestSubagentRunner_FleetBudgetCancels`.
[ ] SP-037-4c: Add `TestSubagentRunner_NoGoroutineLeak_AfterStress`.
[ ] SP-037-4d: Add `TestSubagentRunner_ParentCancelDropsQueued`.
[ ] SP-037-4e: Run `go test -race -run TestSubagentRunner -count=20 ./pkg/agent/` to verify stability.

### Phase 5: Documentation

[ ] SP-037-5a: Add a "Subagent resource model" section to `docs/AGENT_WORKFLOW.md` covering concurrency limit, fleet budget, telemetry, and how to read the WebUI Subagents tab.
[ ] SP-037-5b: Add a package-level doc comment to `pkg/agent/subagent_runner.go` documenting the semaphore + budget invariants.

## Success Criteria

| Metric | Target |
|--------|--------|
| `RunParallel(N=50, limit=4)` max concurrent goroutines | ≤ 4 (verified by counter sampling) |
| Fleet budget overdraw | ≤ 1 subagent's individual `MaxTokens` worth |
| Stress test `goleak.VerifyNone` | Passes |
| Stress test runs in row | 20 |
| WebUI surfacing live counts | Visible in Subagents tab |

## Files Reference

| File | Action |
|------|--------|
| `pkg/agent/subagent_runner.go` | Modify: add fields, semaphore, fleet budget, atomic counters |
| `pkg/configuration/config.go` | Modify: add `MaxConcurrentSubagents`, `UnsafeUnboundedSubagents`, `FleetTokenBudgetDefault` |
| `pkg/personas/configs/executive_assistant.json` | Modify: set `fleet_token_budget` and `max_concurrent_subagents` |
| `pkg/events/subagent_events.go` | Create: event type definitions for subagent lifecycle |
| `pkg/agent/subagent_runner_test.go` | Modify/Create: four new regression tests |
| `webui/src/components/.../SubagentsTab.tsx` | Modify: surface resource counts |
| `packages/ui/src/components/SubagentsPanel.tsx` | Modify: same (depending on canonical location post-SP-039) |
| `docs/AGENT_WORKFLOW.md` | Modify: new "Subagent resource model" section |

## Risks

- **Backpressure surfaces as latency.** With limit=4, a 100-task plan that previously "ran in parallel" now takes 25× longer per batch. Mitigation: the prior behavior was a lie (provider rate limits made it slower than it looked); document this so users tune `MaxConcurrentSubagents` based on their provider headroom.
- **Fleet budget premature exhaustion.** A poorly-sized budget kills useful work. Mitigation: default high (200k tokens), surface remaining budget in telemetry so users can size it from observed usage.
- **Cancellation races.** Cancelling in-flight subagents mid-LLM-call may leave the provider HTTP request running. Mitigation: this is the same `context` plumbing as SP-034 Phase 1 — depend on that work for proper request cancellation, or land a local fix here.
- **Event volume.** High-fanout plans emit many events. Mitigation: counters are atomic and cheap; events are batched per-100ms in the WebUI emitter to avoid UI thrash.
