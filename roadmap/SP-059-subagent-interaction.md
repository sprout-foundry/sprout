# SP-059: Subagent ↔ Primary Interaction Overhaul + Delegate Retirement

**Status:** ✅ Implemented (Phases 1–6 complete; delegate tool retired; audited 2026-06-27)
**Date:** 2026-05-26 (original) · 2026-05-31 (amended) · 2026-06-27 (audit reconciliation)
**Depends on:** SP-006 (Delegate Tool, shipped — being retired by this spec)
**Priority:** High (closes silent-cancel and silent-steering UX bugs; pays
down result-envelope tech debt; collapses the dual delegate/subagent
architecture into a single mechanism)
**Scope:** Six phases. Phases 1–2 are largely shipped. Phases 3–6 are the
remaining work: port the few delegate-only features worth keeping, then
delete the delegate tool entirely.

> **Audit reconciliation (2026-06-27):** The "❌ Pending" markers on
> phases 4/5/6 in the residual table below were stale — they predate
> commit `0ee1a3e3` ("refactor(agent): remove delegate tool in favor of
> subagent system") which actually shipped all three phases. The status
> header already said "Phases 1–6 complete" but the table contradicted
> it. This audit verified the shipping state by file:line evidence and
> flipped the markers; the detailed per-phase evidence and the
> porting-feature review are in `roadmap/SP-059-6a-review.md`. If you
> see another "❌ Pending" appear in this table in the future, treat it
> as suspect and verify with `git log --oneline -- pkg/agent/delegate_*`
> before picking it up — the four dropped capabilities (async, freeform
> role, per-call tools allowlist, `FollowUpMessages`) are non-goals by
> design.

## Problem

The subagent system shipped under SP-006 works for the happy path, but the
seams between the primary agent, running subagents, and the user are thin
and in places non-existent. Three classes of issue:

1. **Cancellation doesn't propagate.** The webui Stop button calls
   `clientAgent.TriggerInterrupt()` on the primary agent only
   (`pkg/webui/api_query.go:512`). The primary is blocked inside
   `runner.Run` and the subagent's `ProcessQuery` (`pkg/agent/conversation.go:19`)
   reads only the *subagent's* `interruptCtx` — not `runCtx`. The runner
   already has `CancelSubagent` and `CancelAll`
   (`pkg/agent/subagent_runner.go:351-371`) but no caller in `pkg/webui`
   invokes them. Net effect: clicking Stop while a subagent is running
   does nothing visible until the subagent finishes on its own.

2. **No mid-flight steering of subagents.** Users typing during subagent
   execution have their messages routed to the primary's
   `inputInjectionChan` (`pkg/agent/input.go:92`), which the primary won't
   drain until the subagent returns. The subagent has its own
   `inputInjectionChan` allocated at `pkg/agent/subagent_runner.go:672` but
   there is no API path to reach it. The UI input box gives no signal that
   the user is typing into a buffer that won't be read.

3. **Result envelope is brittle.** The primary's tool-result is a
   `map[string]string` (`pkg/agent/tool_handlers_subagent.go:785-870`) with:
   - **Token rollup via regex scraping** of `SUBAGENT_METRICS:` stdout
     lines (`tool_handlers_subagent.go:247-265`). Silent regression if a
     model strips the line. The structured `SubagentResult.TokensUsed`
     value at `subagent_runner.go:576` is computed but unused for parent
     rollup.
   - **Files modified via regex scraping** for `Created:`/`Modified:`/
     `Wrote` prefixes (`tool_handlers_subagent.go:218-230`). No structured
     manifest, no diff, even though `ChangeTracker.GetChanges()` already
     produces a `[]TrackedFileChange` (`pkg/agent/change_tracking.go:244`).
   - **Error states encoded as sentinel string prefixes** —
     `SUBAGENT_SECURITY_ERROR`, `SUBAGENT_TOKEN_BUDGET_EXCEEDED`,
     `SUBAGENT_FAILED` (`tool_handlers_subagent.go:888,948,976`). Fragile
     for callers; impossible to discriminate by code.
   - **No partial visibility for the primary's LLM.** Subagent output
     streams to the UI (batched activity events) but the primary's LLM
     sees nothing until the final tool-return. The richer `delegate` tool
     already implements `DelegateStreamBridge`
     (`pkg/agent/tool_handlers_delegate.go:55-57`) for the same problem —
     two parallel architectures for the same concept.
   - **Stub handlers in the new tool registry**
     (`pkg/agent_tools/run_subagent_handler.go:55-59`,
     `pkg/agent_tools/run_parallel_subagents_handler.go:63-66`) return
     hardcoded errors. If dispatch ever shifts to the new registry first
     for these tools, every call breaks.

## Current State

### What Works (do not regress)

| Mechanism | File:Line | Notes |
|---|---|---|
| Per-persona depth ceiling | `pkg/agent/agent_getters.go:506-511` | Default 2 for EA-root, 1 otherwise; `SubagentMaxDepth` config override |
| Persona-self-spawn block | `pkg/agent/tool_handlers_subagent.go:687-690` | Plus EA-spawn-authority gating |
| Lifecycle events (`queued`/`started`/`completed`/`cancelled`) | `pkg/agent/subagent_runner.go:173-198` | With queue-wait metrics |
| Activity events batched at 50/batch | `pkg/agent/tool_handlers_subagent.go:22` | Drives Subagents tab |
| Per-persona tool allowlist | `pkg/agent/conversation.go:97-100` | Enforced inside subagent |
| Fleet-wide token budget | `pkg/agent/subagent_runner.go:230,272-283` | Atomic counter; checked before each task starts |
| Critical security re-prompt path | `pkg/agent/tool_handlers_subagent.go:540-571` | Subagent approval-request → primary's event bus |
| `CancelSubagent` / `CancelAll` runner API | `pkg/agent/subagent_runner.go:351-371` | Exists but unused externally |
| `ChangeTracker.GetChanges()` | `pkg/agent/change_tracking.go:244` | Structured `[]TrackedFileChange`, ready to consume |
| `SubagentResult.TokensUsed` / `.Cost` | `pkg/agent/subagent_runner.go:576-588` | Computed from `subAgent.state.GetTotalTokens()` |

### What's Actually Missing

| Gap | Impact | Phase | Status |
|---|---|---|---|
| Webui Stop button doesn't cancel running subagents | High (UX) | 1a | ✅ Shipped (`api_query.go:537` calls `runner.CancelAll`) |
| No way to steer a running subagent mid-flight | High (UX) | 1b | ✅ Shipped (`InjectInputIntoActive` at `subagent_runner.go:409`) |
| UI doesn't distinguish primary-busy from subagent-running | Medium (UX) | 1c | ❓ Verify; may be covered by SP-051 |
| Subagents tab is read-only (no per-row cancel) | Medium (UX) | 1c | ❓ Verify; may be covered by SP-051 |
| Token rollup via `SUBAGENT_METRICS:` stdout regex | Medium (correctness) | 2b | ✅ Shipped (`SubagentRunMetrics`) |
| Files-modified via `Created:`/`Modified:` stdout regex | Medium (correctness) | 2c | ✅ Shipped (`FilesModified []FileChange`) |
| Sentinel string error encoding | Low (refactor hygiene) | 2d | ✅ Shipped (`SubagentStatus` + `SubagentError`) |
| Result envelope is `map[string]string` not typed | Low (refactor hygiene) | 2a | ✅ Shipped (`SubagentReturn`) |
| Primary's LLM gets no partial visibility into subagent progress | High (capability) | 3a | ✅ Shipped — `ProgressLog` in `SubagentReturn` carries the full timeline; mid-flight injection N/A for sync subagents |
| Stub handlers in new registry return errors | Low today (dead path) | 3b | ✅ Shipped (handler files deleted) |
| Subagents can't request user clarification mid-run | Medium (capability) | 4 | ✅ Shipped — `subagent_creation.go:114-117` shares `parent.clarificationManager`; tests at `subagent_runner_test.go:1399-1403` assert the child receives the parent's manager by pointer |
| `SubagentReturn` missing useful `DelegateResult` fields (e.g. `Iterations`) | Low (parity for delegate removal) | 5 | ✅ Shipped — `SubagentReturn.Iterations` at `subagent_types.go:53`, `SubagentRunMetrics.Iterations` at `subagent_types.go:190` |
| `delegate` + `delegate_status` tools duplicate `run_subagent` with weaker operational tooling | High (architectural debt) | 6 | ✅ Shipped — all 8 delegate files deleted (commit `0ee1a3e3`); review at `roadmap/SP-059-6a-review.md` confirms no live consumers or needed delegate features missing from `run_subagent` |

## Proposed Solution

Three phases. Each ships independently.

### Phase 1: Cancellation + steering — close the silent-input UX bugs

**Goal:** When the user clicks Stop, running subagents actually stop.
When the user types a steering message while a subagent is running, it
reaches the subagent (or the primary, with a clear UI signal of which).

#### 1a. Stop button cancels running subagents

`handleAPIQueryStop` (`pkg/webui/api_query.go:506-520`) currently calls
`clientAgent.TriggerInterrupt()` on the primary. After the existing
`TriggerInterrupt()` call, also call `clientAgent.GetSubagentRunner().CancelAll()`.

Two follow-up details:

1. **`Agent.ProcessQuery` doesn't observe `runCtx`.** The runner's
   `runCtx` (`subagent_runner.go:387-394`) is the right cancel signal,
   but `ProcessQuery(prompt)` (`pkg/agent/conversation.go:19`) doesn't
   take a context. We have two viable options:
   - **Option A (chosen):** Have `CancelSubagent` invoke
     `subAgent.TriggerInterrupt()` on the subagent's own interrupt
     mechanism. The seed-conversation loop already honors that path
     (`pkg/agent/pause.go:11-15`). Minimal blast radius; same mechanism
     the primary already uses.
   - Option B: Change `ProcessQuery` to take `context.Context`.
     Touches every caller; defer to a separate refactor.

   This spec proposes Option A.

2. **Per-subagent cancel** also exposed via a new endpoint
   `POST /api/subagent/{id}/cancel`. UI uses it in 1c.

**Files touched:**
- `pkg/webui/api_query.go` — extend `handleAPIQueryStop`
- `pkg/webui/routes.go` — new `/api/subagent/{id}/cancel` route
- `pkg/webui/api_subagent.go` (new) — per-ID cancel handler
- `pkg/agent/subagent_runner.go` — `CancelSubagent` / `CancelAll` call
  `subAgent.TriggerInterrupt()` instead of (or in addition to) cancelling
  `runCtx`

#### 1b. Mid-flight steering to running subagent

When a subagent is the active executor, steering messages route to its
`inputInjectionChan` instead of the primary's.

The runner already tracks active subagents in `r.active sync.Map` keyed
by `taskID` with `*runningSubagent` values
(`subagent_runner.go:96, 499-500`). Expose a method
`InjectInputIntoActive(input string) (delivered bool, target string)` that
finds the deepest (most-recently-started) active subagent and pushes the
input into its `inputInjectionChan`. Returns `delivered=false` when no
active subagent exists — caller falls back to the primary.

`handleAPIQuerySteer` (`pkg/webui/api_query.go:413-475`) checks for an
active subagent first:

```go
if delivered, target := runner.InjectInputIntoActive(query.Query); delivered {
  writeJSON(w, http.StatusAccepted, map[string]any{
    "accepted": true,
    "mode":     "steer",
    "target":   target, // "subagent:<id>"
  })
  return
}
// fall through to existing primary-injection path
```

The response distinguishes target so the UI can label it (1c).

**Files touched:**
- `pkg/agent/subagent_runner.go` — new `InjectInputIntoActive` method
- `pkg/webui/api_query.go` — route to subagent if active
- `pkg/agent/seed_integration.go` — verify subagent seed loop already
  drains `inputInjectionChan` the same way the primary does (it does;
  same code path at line 777-819)

#### 1c. UI: per-row cancel + busy-state distinction

Two surfaces:

1. **Subagents context panel** (`SubagentsTab.tsx`, `SubagentTree.tsx`):
   each row in `running` state shows a small Stop icon → `POST /api/subagent/{id}/cancel`.

2. **Chat footer / input box** (`ChatFooter.tsx:78`): when a subagent is
   the active executor, show a distinct status pill:
   - "Primary thinking" (existing)
   - **"Subagent running — your next message will steer it"** (new)

   Source the busy state from the existing `hasSubagentActivity` signal
   already in the footer's render condition. The new copy makes the
   routing target clear so users don't think the input is stuck.

**Files touched:**
- `webui/src/components/chat/SubagentsTab.tsx` — Stop button per row
- `webui/src/components/chat/SubagentTree.tsx` — same
- `webui/src/components/chat/ChatFooter.tsx` — subagent-routing copy
- `webui/src/services/api/subagentApi.ts` (new) — `cancelSubagent(id)`

### Phase 2: Structured result envelope — typed return, real manifest, real metrics

**Goal:** Replace `map[string]string` + sentinel strings + regex stdout
scraping with typed structures that the primary's LLM (and any future
caller) can rely on.

#### 2a. Typed `SubagentReturn` envelope

New struct in `pkg/agent`:

```go
type SubagentReturn struct {
  Output         string             `json:"output"`              // final assistant message
  Summary        string             `json:"summary,omitempty"`   // first-paragraph excerpt
  FilesModified  []FileChange       `json:"files_modified,omitempty"`
  ToolCalls      []SubagentToolCall `json:"tool_calls,omitempty"`
  Metrics        SubagentMetrics    `json:"metrics"`
  Status         SubagentStatus     `json:"status"` // "completed" | "timed_out" | "budget_exceeded" | "security_blocked" | "cancelled" | "failed"
  ErrorReason    string             `json:"error_reason,omitempty"`
  ElapsedSeconds float64            `json:"elapsed_seconds"`
  WorkingDir     string             `json:"working_dir,omitempty"`
}

type FileChange struct {
  Path string `json:"path"`
  Op   string `json:"op"` // "created" | "modified" | "deleted"
}

type SubagentMetrics struct {
  TokensUsed int64   `json:"tokens_used"`
  Cost       float64 `json:"cost"`
  ToolCalls  int     `json:"tool_calls"`
}
```

JSON-marshaled as the primary's tool result, replacing the current
`map[string]string`. The LLM sees the same JSON shape it sees today
plus structured `files_modified` and `metrics`. Existing keys
(`stdout`, `exit_code`, etc.) are retained or mapped through a legacy
shim only if regression-testing turns up an LLM that relied on the
exact prior key names — preferred path is to migrate cleanly with one
shipped breaking change in the spec.

**Files touched:**
- `pkg/agent/subagent_types.go` (new) — typed envelope
- `pkg/agent/tool_handlers_subagent.go` — populate `SubagentReturn`
  instead of `map[string]string`

#### 2b. Drop the `SUBAGENT_METRICS:` stdout scrape

In `tool_handlers_subagent.go:247-265`, replace the regex parse of
`SUBAGENT_METRICS:` stdout lines with direct reads from
`SubagentResult.TokensUsed` and `.Cost` (already computed at
`subagent_runner.go:576-588`).

The print-side that emits `SUBAGENT_METRICS:` from inside the subagent
can stay as a debug aid but its value is no longer consulted by the
parent.

**Files touched:**
- `pkg/agent/tool_handlers_subagent.go` — delete the scrape branch,
  fill `SubagentReturn.Metrics` from `SubagentResult` directly

#### 2c. Structured file-change manifest from ChangeTracker

`ChangeTracker.GetChanges()` already returns `[]TrackedFileChange`
(`pkg/agent/change_tracking.go:244`). When the subagent is built in
`createSubagent`, the parent's ChangeTracker (if enabled) gets a
*child* tracker scoped to the subagent run. After the subagent
returns, the child's `GetChanges()` is copied into
`SubagentReturn.FilesModified` and merged into the parent tracker.

This replaces the regex scrape at `tool_handlers_subagent.go:218-230`.
The stdout `Created:`/`Modified:` lines stay (they're human-readable
output) but the parent no longer regex-parses them.

Two cases to handle:
- **Parent tracker disabled:** subagent runs without tracking;
  `FilesModified` is `nil` and the consumer treats absence as "not
  reported," not "no changes." Same semantics as today.
- **Parent tracker enabled:** child tracker scoped, merged on return,
  manifest populated.

**Files touched:**
- `pkg/agent/change_tracking.go` — add `Fork(parent *ChangeTracker)` and
  `Merge(child *ChangeTracker)` helpers (or use existing API if the
  shape already supports it)
- `pkg/agent/subagent_runner.go:createSubagent` — wire child tracker
- `pkg/agent/tool_handlers_subagent.go` — populate
  `SubagentReturn.FilesModified` from child's `GetChanges()`

#### 2d. Typed errors instead of sentinel strings

Replace `SUBAGENT_SECURITY_ERROR`, `SUBAGENT_TOKEN_BUDGET_EXCEEDED`,
`SUBAGENT_FAILED` string prefixes with a `SubagentStatus` enum
(included in 2a's struct) and a typed error value:

```go
type SubagentError struct {
  Status SubagentStatus // "security_blocked" | "budget_exceeded" | "failed" | "timed_out"
  Reason string
}
```

The JSON output already carries `status` and `error_reason`; the typed
error is for in-process callers.

**Files touched:**
- `pkg/agent/subagent_types.go` — `SubagentStatus` + `SubagentError`
- `pkg/agent/tool_handlers_subagent.go:888,948,976` — return typed
  errors instead of `SUBAGENT_*` prefixed strings

### Phase 3: Streaming into primary's LLM context + dead-stub cleanup

**Goal:** The primary's LLM can react to subagent progress, not just the
final tool-return. Bring `run_subagent` up to the `delegate` tool's
streaming-bridge pattern.

#### 3a. Stream subagent activity into the primary's LLM context

**Resolution (amended 2026-05-31):** This was originally framed around
mid-flight injection between primary LLM steps. With async subagents
dropped in Phase 6, the primary is sync-blocked on the `run_subagent`
tool call — there are no intervening LLM steps to inject into. The
practical answer is the already-shipped `ProgressLog` field on
`SubagentReturn` (`subagent_types.go:96-100`), populated by an
event-bus subscription in `subagent_runner.go:507-547`. When the
sync tool call returns, the primary's LLM sees the full chronological
activity timeline (spawn / output / complete entries, capped at 50)
attached to the result envelope. Goal met without the
`SubagentObserver` machinery below.

The original design follows for historical reference:

```
Subagent <persona:id> [t+12s]: read_file pkg/auth.go
Subagent <persona:id> [t+18s]: write_file pkg/auth_test.go
Subagent <persona:id> [t+25s]: ran 4 tests, all pass
```

Mechanism:

- New `SubagentObserver` interface with `Observe(taskID, event)`.
- `subagent_runner.go` publishes `subagent_activity` events (already
  exists at `tool_handlers_subagent.go:76-161`); the observer
  subscribes to the parent's event bus filtered by parent ID.
- The seed integration drains the observer's buffer between LLM
  steps and prepends it as a "subagent_progress" tool observation.

This mirrors `DelegateStreamBridge` (`tool_handlers_delegate.go:55-57`),
which already does the same thing for async delegates. The plan is to
extract the bridge into a shared `pkg/agent/streambridge.go` and have
both `run_subagent` and `delegate` use it.

**Open question:** what fraction of subagent activity is useful for
the primary's LLM vs noise? Initial scope: tool name + path + final
result line per tool call. Phase 3 ships with a fixed batch size (10
events / 2 seconds, whichever first) and tunable in the same Memory
settings panel that already houses context controls.

**Files touched:**
- `pkg/agent/streambridge.go` (new) — extracted from
  `tool_handlers_delegate.go`
- `pkg/agent/subagent_runner.go` — wire observer into the runner
- `pkg/agent/tool_handlers_subagent.go` — use shared bridge
- `pkg/agent/tool_handlers_delegate.go` — switch to shared bridge
- `pkg/agent/seed_integration.go` — drain observer buffer between
  LLM steps

#### 3b. Remove or finish the stub new-registry handlers

`pkg/agent_tools/run_subagent_handler.go:55-59` and
`pkg/agent_tools/run_parallel_subagents_handler.go:63-66` return hardcoded
errors. Two options:

- **Delete them** if the seed registry will remain canonical for
  subagent tools. Removes a footgun if dispatch ever reorders.
- **Finish them** by routing through a `SubagentRunner` accessor on
  `ToolEnv` (parallel to how SP-058 added `EmbeddingMgr` to `ToolEnv`).

Initial choice: **delete**. The dual-dispatch path already prefers the
new registry (per `tool_executor_sequential.go:142-143`), so finishing
the stubs is a non-trivial refactor (the new-registry tools don't have
`*Agent` access). Removing them keeps the seed registry as the
canonical home for subagent tools until a future SP picks up the
migration.

**Files touched:**
- Delete `pkg/agent_tools/run_subagent_handler.go` + its test
- Delete `pkg/agent_tools/run_parallel_subagents_handler.go` + its test
- `pkg/agent_tools/all.go` — remove their registrations

### Phase 4: Subagents can request user clarification mid-run

**Goal:** Subagents reach the same `clarificationManager` the primary
already shares with delegates. Today, `createSubagent` does not copy
`parent.clarificationManager`, so a subagent that hits an ambiguous
input has no path back to the user. `delegate_factory.go:111-113`
already demonstrates the one-line wiring.

**Files touched:**
- `pkg/agent/subagent_runner.go` — in `createSubagent`, copy
  `parent.clarificationManager` onto the child agent struct (same as
  delegate's pattern).
- Test: extend an existing subagent runner test to assert the field is
  populated.

### Phase 5: Reconcile `SubagentReturn` with delegate's return fields

**Goal:** Before deleting `delegate`, copy any genuinely useful fields
from `DelegateResult` into `SubagentReturn` so callers don't lose
information.

**Audit candidates:**
- `Iterations int` — `DelegateResult` has it; `SubagentReturn` doesn't.
  Worth adding — it's a useful budget-pressure signal for the primary's
  LLM.
- `Summary string` — both have it (already present in `SubagentReturn`).
- `FilesChanged []string` — `SubagentReturn.FilesModified` is richer
  (carries op). No change.
- `ToolsCalled []ToolCallRecord` — partially mirrored by
  `ProgressLog`. The richer per-call record is delegate-only; skip
  unless a real consumer materializes.
- `ExitStatus string` (`"completed" | "max_iterations" | "error"`) —
  `SubagentStatus` already covers this. Map `max_iterations` →
  `SubagentStatusBudgetExceeded` semantically (it's the iteration
  budget rather than the token budget, but the meaning is the same
  class of terminal state — "couldn't finish within limits").

**Files touched:**
- `pkg/agent/subagent_types.go` — add `Iterations int` to
  `SubagentReturn` and `SubagentRunMetrics`.
- `pkg/agent/subagent_runner.go` / `tool_handlers_subagent.go` —
  populate it from `subAgent.state.GetIterations()` or equivalent.

### Phase 6: Delete the `delegate` tool entirely

**Goal:** Collapse the dual delegate/subagent architecture. After
Phases 3–5, the only delegate-only capabilities that haven't been
ported are: async execution + `delegate_status`, freeform `role`
strings, optional `tools` allowlist per-call, and `FollowUpMessages`
pre-scheduled injection. We are intentionally dropping all four:

- **Async + `delegate_status`** — research (Claude Code's `Task` tool,
  Anthropic Agent SDK guidance, common pattern across coding agents)
  shows async subagent execution is not a documented best practice;
  the LLM control loop does not benefit from non-blocking child
  execution. Parallelism is already covered by `run_parallel_subagents`.
- **Freeform `role` string** — was a bug. Personas must be registered
  configurations, not ad-hoc strings, so per-persona allowlists and
  spawn-authority gating apply uniformly.
- **Per-call `tools` allowlist** — superseded by the per-persona
  allowlist (`conversation.go:97-100`). One source of truth.
- **`FollowUpMessages` pre-scheduled injection** — interactive
  steering via Phase 1b's `InjectInputIntoActive` covers the live
  case; no production caller uses pre-scheduled follow-ups today.

**Pre-flight:**

```bash
grep -rn '"delegate"\|"delegate_status"\|DelegateConfig\|DelegateResult\|delegateDepth' \
  --include="*.go" --include="*.ts" --include="*.tsx" \
  --include="*.json" --include="*.md" .
```

Anything beyond the known delete-set (the files listed below + their
tests + spec mentions) is a live consumer that must migrate to
`run_subagent` first.

**Files deleted:**
- `pkg/agent/delegate_factory.go` + `delegate_factory_test.go`
- `pkg/agent/delegate_types.go` + `delegate_types_test.go`
- `pkg/agent/delegate_stream.go` + `delegate_stream_test.go` (logic
  preserved in `pkg/agent/streambridge.go` from Phase 3a, scoped to
  subagents only)
- `pkg/agent/delegate_nesting.go` + `delegate_nesting_test.go`
- `pkg/agent/async_delegate_tracker.go` + `async_delegate_tracker_test.go`
- `pkg/agent/tool_handlers_delegate.go` + `tool_handlers_delegate_test.go`
  + `tool_handlers_delegate_async_test.go` + `delegate_followup_test.go`
- `pkg/agent/tool_handlers_delegate_status.go` + `tool_handlers_delegate_status_test.go`

**Files modified:**
- `pkg/agent/tool_registrations.go` — remove `delegate` and
  `delegate_status` registrations (lines 236, 256).
- `pkg/agent/tool_registrations_test.go` — drop delegate entries from
  the expected tool list and parameter-shape tests.
- `pkg/agent_api/tools.go` — remove delegate registrations (lines
  605, 673).
- `pkg/agent/agent.go` (or wherever `Agent` is declared) — remove
  `delegateDepth`, `delegateID` fields; remove `clarificationManager`
  share path through the delegate factory (subagent path replaces it).
- Anywhere `SPROUT_MAX_DELEGATE_DEPTH` is read — remove the env-var
  handling.
- `webui/` — grep for `delegate_spawn` / `delegate_activity` /
  `delegate_complete` / `delegate_tool` event consumers; migrate to
  the subagent equivalents (these events already exist for subagents).
- `roadmap/SP-006-delegate-tool.md` — collapse to stub, mark
  superseded by SP-059 (mirrors how SP-023 was archived).

**Verification:**
- `make build-all`
- `go test ./...`
- Manual: spin up the CLI, confirm tool listing no longer advertises
  `delegate` / `delegate_status`, confirm `run_subagent` still works.

## Test plan

Per phase. Each phase is independently shippable; tests guard the seam.

### Phase 1
- Click Stop while a subagent is running → subagent terminates within
  the existing 5s leak window (`subagent_runner.go:545-558`); `cancelled`
  lifecycle event fires.
- Type a steer message while a subagent is running → `/api/query/steer`
  returns `target: "subagent:<id>"`; the message appears in the
  subagent's input.
- Type a steer message while no subagent is running → existing primary
  injection path, no behavior change.
- UI: footer pill text changes between primary and subagent states.
- UI: Stop icon in Subagents tab triggers `/api/subagent/{id}/cancel`.

### Phase 2
- New result JSON has the new fields; existing fields still present (or
  documented as removed if we drop the legacy shim).
- Token rollup matches the value from `SubagentResult.TokensUsed`
  (regression check: synthesize a scenario where the subagent's stdout
  omits `SUBAGENT_METRICS:` — parent still gets correct tokens).
- File-change manifest contains the same paths that
  `ChangeTracker.GetChanges()` reports, in the same order.
- Typed `SubagentError` round-trips through the tool-result JSON as
  `status`/`error_reason`.

### Phase 3
- Primary's seed loop sees `subagent_progress` observations during a
  subagent run (assert by inspecting the message list before the final
  tool-result lands).
- Buffer-size and interval limits hold under a chatty subagent
  (no unbounded growth).
- Shared `streambridge.go` services both `run_subagent` and `delegate`
  with identical event shapes.

## Non-goals (deferred)

- **Filesystem write coordination for parallel subagents.** The CLAUDE.md
  "don't use parallel" rule is structural; building a coordination layer
  is a separate spec. Phase 1 simply makes serial subagents cancellable
  and steerable.
- **Full migration of subagent tools to the new `pkg/agent_tools`
  registry.** Phase 3b deletes the stubs rather than finishing them;
  a future SP can pick up the migration if the dispatch order ever
  changes.
- **Per-subagent UI deep-dive (open in pane, view full conversation).**
  The current Subagents tab shows lifecycle + activity events; a richer
  drill-down is a separate UX project.
- **Async/fire-and-forget subagent execution.** Explicitly dropped in
  Phase 6 rather than ported from `delegate`. Sync subagents +
  `run_parallel_subagents` cover the real use cases; async added
  coordination overhead with no model-driven benefit.
- **Pre-scheduled follow-up messages for subagents.** Delegate's
  `FollowUpMessages` is not being ported. Phase 1b's interactive
  steering covers live cases; pre-scheduling can be revisited if a
  concrete need appears.

## References

- Audit transcript that motivated this spec: in-conversation, 2026-05-26.
- SP-006 (Delegate Tool) — original subagent design.
- Existing primitives reused:
  - `Agent.TriggerInterrupt` (`pkg/agent/pause.go:11-15`)
  - `SubagentRunner.CancelAll` (`pkg/agent/subagent_runner.go:361`)
  - `ChangeTracker.GetChanges` (`pkg/agent/change_tracking.go:244`)
  - `DelegateStreamBridge` (`pkg/agent/tool_handlers_delegate.go:55-57`)
