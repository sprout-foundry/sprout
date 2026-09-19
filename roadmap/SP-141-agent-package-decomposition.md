# SP-141: pkg/agent Package Decomposition

**Status:** Proposed
**Created:** 2026-09-19
**Origin:** 2026-09-19 codebase evaluation — `pkg/agent` had grown to 238
non-test files / ~51K LOC in a single package, the largest concentration in
the repo. This spec plans the split; it does not schedule it.

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
2. `pkg/agent/changes` — self-contained change-tracking cluster with its own test suite.
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
