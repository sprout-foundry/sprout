# SP-141: pkg/agent Package Decomposition

**Status:** In Progress — Phases 1–2 landed 2026-09-27
**Created:** 2026-09-19
**Origin:** 2026-09-19 codebase evaluation — `pkg/agent` had grown to 238
non-test files / ~51K LOC in a single package, the largest concentration in
the repo. This spec plans the split; it does not schedule it.

## Progress

- **Phase 1 (2026-09-27): `pkg/agent/workflow` landed.** The in-process
  TODO-loop runner (loop driver, config parsing, gate/triage types +
  parsers, outcome classification, TODO-file ops, session ID, budget
  heartbeat) moved to a new `pkg/agent/workflow` subpackage behind a narrow
  `Agent`/`Budget`/`HeartbeatReporter` interface, so the subpackage does not
  import `pkg/agent` (no cycle). `RunWorkflowLoopInProcess` stays in
  `pkg/agent` as the construction entry point (it needs unexported `Agent`
  fields) and now forwards to `workflow.RunLoop`; `WorkflowResult` is a type
  alias to `workflow.Result`; `handleRunAutomate` calls
  `workflow.ParseFile`/`NewSessionID`. Pure move, no behavior change.
- **Dead-code warm-up (2026-09-27):** removed zero-reference `pkg/agent`
  code: the `tool_call_format.go` island (`formatToolCall` +
  `formatTruncateString` + `summarizeTodoWriteArgs` +
  `maxToolArgDisplayLength` — entry `formatToolCall` had no callers, its
  helpers were reachable only through it) and the single-arg
  `isSystemPath` (zero refs; the live variant is
  `isSystemPathWithOriginal`). Verified zero references repo-wide before
  deletion; the `find_dead_code` graph's high-confidence list was
  over-flagging (several entries had live test-only or cross-package
  callers), so deletion used repo-wide grep as ground truth.
- **Phase 2 (2026-09-27): `pkg/agent/changes` landed.** The change-tracking
  cluster (the 12 `change_tracking*.go` production files) moved to a new
  `pkg/agent/changes` subpackage. Unlike the workflow phase, this cluster was
  **not** self-contained: `ChangeTracker` holds an `agent *Agent` field and
  reaches into three unexported spots (`ct.agent.workspaceRoot`,
  `ct.agent.eventBus.Publish` ×2, `ct.agent.Logger()`), and its white-box test
  suite constructs `&Agent{...}`. So the phase landed in two verifiable
  increments, per the spec's "narrow interface seam" prerequisite:
  - **Step A (in-package, zero moves):** introduced a `ChangeAgent` interface
    + `DebugLogger` sub-interface the tracker now depends on instead of the
    concrete `*Agent`. `*Agent` satisfies it structurally, so the 3.9K-line
    white-box suite compiled unchanged. `ct.agent.workspaceRoot` →
    `GetWorkspaceRoot()` (equivalent: the field is only written via the
    trimming setter); the two `eventBus.Publish` sites now route through a new
    nil-guarded, undecorated `Agent.PublishRawEvent` (preserves the exact
    pre-seam publish semantics); `ct.agent.Logger()` → a `DebugLogger()` seam
    (the concrete `*AgentLogger` return type can't cross a package boundary).
  - **Step B (the physical move):** relocated the 12 files into
    `pkg/agent/changes/` behind type aliases in `pkg/agent`
    (`ChangeTracker`, `TrackedFileChange`, `TrackedBulkItem`,
    `CheckpointFileChange`) + a `NewChangeTracker` forwarder, so
    `webui`/`agent_tools`/`cmd` and all in-package callers are untouched.
    `CheckpointFileChange` traveled with the cluster (its only in-cluster user
    is `CollectFileChangesForCheckpoint`; `turn_checkpoints`/`rollup` resolve
    it via the alias). The white-box test files moved to `changes` with a
    minimal `fakeAgent` double in place of the unreferenceable `*Agent`
    (import cycle); the genuinely agent-integration tests (agent handler +
    `NewTestAgent` + `agent.changeTracker`) stayed in `pkg/agent` behind
    exported testutil seams. Pure move, no behavior change.
  - **Scope:** the `transcript_snapshot*.go` trio and `atomic_write.go`
    stayed in `pkg/agent` this phase — the snapshot trio carries `*Agent`
    methods (`BuildTranscriptSnapshot`/`CaptureTranscriptSnapshot`) and
    `writeFileAtomic` serves `persistence_message.go`; both can follow in a
    later increment if the cluster's remaining entanglement warrants it.
- Phases 3–5 pending (`approvals`, `subagents`, `tools`).

## Problem

`pkg/agent` is a god package. Everything the in-process agent does lives in
one Go namespace: query lifecycle, tool handlers, provider wiring, workflow
runner, shell/session management, embeddings, seeding, subagent execution,
change tracking, and security analysis.

Consequences:

- Any internal change risks conflicting with unrelated in-flight work
  (merge friction across the whole team).
- Import-level coupling is invisible: files reach into shared package state
  freely, so dependency direction can only be enforced by convention.
- Compile times for the package scale with the whole surface, not the
  changed part.
- The 500-line file convention is hardest to hold here (14 files exceed it,
  led by `tool_handlers_file.go` at ~820).

## Current shape (2026-09-19)

Cluster (approximate file groups in `pkg/agent`):

| Cluster | Representative files | Notes |
|---|---|---|
| Core lifecycle | agent.go, agent_runtime.go, agent_lifecycle.go, agent_state.go, agent_events.go | shared by everything; moves last |
| Query + streaming | seed_query.go, conversation.go, api_client_types.go | |
| Tool handlers | tool_handlers_{file,shell,changes,structured,subagent_spawn}.go | ~680-820 lines each |
| Provider wiring | agent_provider.go, seed_provider.go, agent_providers glue | |
| Shell / sessions | agent_shell.go, shell session mgmt, background cleanup | build-tag split (js/wasm) |
| Subagents | submanagers.go, subagent_runner.go, subagent_task.go | |
| Change tracking | change_tracking*.go, transcript_snapshot.go, atomic_write.go | own test suite (~4K lines) |
| Security / risk | security_analyzer.go, risk_assessment.go, approval_*.go | |
| Workflow | workflow_runner.go | has own TODO-loop entry point |
| Embeddings / recall | agent_embedding.go, semantic_recall.go, turn embedding | |
| Settings | settings_defs.go, settings_handler.go | |

Build-tag surface: `agent_tool_wiring_js.go` / `agent_tool_wiring_nonjs.go`,
`background_cleanup_{desktop,wasm}.go` — any extraction must keep tag pairs
together.

## Target shape

Subpackages under `pkg/agent/`, extracted incrementally (one cluster per
change, `make build-all` + that cluster's tests green between steps):

```
pkg/agent/            core lifecycle + query + interfaces consumed elsewhere
pkg/agent/tools       tool handler implementations (handler funcs stay
                      registered through the existing registry seam)
pkg/agent/subagents   subagent manager, runner, task
pkg/agent/changes     change tracking + transcript snapshots
pkg/agent/approvals   approval broker, allowlists, risk assessment inputs
pkg/agent/workflow    workflow runner (near-standalone today)
```

Rules of engagement:

1. **No behavior changes.** Each extraction is a pure move; factories and
   registries in `pkg/agent` keep registering the same tools.
2. **Direction of imports:** extracted packages may import `pkg/agent`
   core types via a narrow `pkg/agent/agentapi` (or `pkg/types`) seam if
   needed; `pkg/agent` core must NOT import extracted packages except for
   registration wiring — flip any violation before merging.
3. **Exported surface stays compatible** for external consumers (`cmd`,
   `pkg/webui`, `pkg/console`, `pkg/agent_tools`, foundry integration).
   Prefer moving unexported implementations and keeping forwarding
   declarations temporarily; delete forwarders in the same phase they
   become unused.
4. **Test files move with their code.** `newTestAgent(t)` /
   `createTestAgentWithTempConfig(t)` helpers stay in `pkg/agent` and are
   imported by subpackage tests via an exported testutil seam if needed.
5. **Build-tag files move as complete pairs** (js/non-js, wasm/desktop).

## Suggested phase order (highest cohesion first)

1. `pkg/agent/workflow` — smallest blast radius, own entry point (`RunWorkflowLoopInProcess`).
2. `pkg/agent/changes` — change-tracking cluster with its own test suite
   (less self-contained than the table implies: the tracker held an
   `agent *Agent` field; landed 2026-09-27 behind the `ChangeAgent` seam).
3. `pkg/agent/approvals` — approval broker + allowlists + risk inputs.
4. `pkg/agent/subagents` — submanagers/runner/task cluster.
5. `pkg/agent/tools` — the five big tool_handlers files (largest; do last, possibly split by handler family).

Each phase is scoped for a single focused session (repo convention:
1-4 hours). Phases are independent after #1 lands the agentapi seam.

## Out of scope

- `pkg/console` and `pkg/webui` splits (separate evaluation items).
- Any tool/schema/contract changes — registration names, tool names, and
  event payloads are frozen by foundry integration tests.

## Acceptance criteria

- [ ] `pkg/agent` non-test files ≤ 150 and every file ≤ 500 lines
- [ ] `go list ./pkg/agent/...` shows the six packages; no import cycles
- [ ] `go test ./pkg/agent/...` green after each phase (targeted runs; machine is resource-constrained)
- [ ] `make build-all` green after each phase
- [ ] No new exported symbols beyond the agentapi seam
