# SP-059: Subagent ↔ Primary Interaction Overhaul

**Status:** 📋 Proposed
**Date:** 2026-05-26
**Depends on:** SP-006 (Delegate Tool, shipped) — extends the subagent runner
**Priority:** High (closes silent-cancel and silent-steering UX bugs; pays
down result-envelope tech debt)
**Scope:** Three phases, each independently shippable.

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

| Gap | Impact | Phase |
|---|---|---|
| Webui Stop button doesn't cancel running subagents | High (UX) | 1a |
| No way to steer a running subagent mid-flight | High (UX) | 1b |
| UI doesn't distinguish primary-busy from subagent-running | Medium (UX) | 1c |
| Subagents tab is read-only (no per-row cancel) | Medium (UX) | 1c |
| Token rollup via `SUBAGENT_METRICS:` stdout regex | Medium (correctness — silent regression if model drops line) | 2b |
| Files-modified via `Created:`/`Modified:` stdout regex | Medium (correctness — same brittleness) | 2c |
| Sentinel string error encoding | Low (refactor hygiene) | 2d |
| Result envelope is `map[string]string` not typed | Low (refactor hygiene) | 2a |
| Primary's LLM gets no partial visibility into subagent progress | High (capability) | 3a |
| Stub handlers in new registry return errors | Low today (dead path) | 3b |

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

The primary's seed conversation loop has an "observation" injection
mechanism it uses for tool results. We extend it with a
`SubagentObserver` that pushes a small, capped buffer of subagent
events into the primary's pending observations during the run:

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

## References

- Audit transcript that motivated this spec: in-conversation, 2026-05-26.
- SP-006 (Delegate Tool) — original subagent design.
- Existing primitives reused:
  - `Agent.TriggerInterrupt` (`pkg/agent/pause.go:11-15`)
  - `SubagentRunner.CancelAll` (`pkg/agent/subagent_runner.go:361`)
  - `ChangeTracker.GetChanges` (`pkg/agent/change_tracking.go:244`)
  - `DelegateStreamBridge` (`pkg/agent/tool_handlers_delegate.go:55-57`)
