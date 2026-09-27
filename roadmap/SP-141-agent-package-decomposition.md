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
- **Phase 3 (2026-09-27): `pkg/agent/approvals` increment 1 — the LLM
  security-analyzer cluster landed.** The `security_analyzer*.go` trio
  (`security_analyzer.go`, `security_analyzer_chain.go`,
  `security_analyzer_cache.go`) moved into the new `pkg/agent/approvals`
  subpackage. The cluster was nearly self-contained: the LLM entrypoints
  (`AnalyzeChain`, `AnalyzeChainFallback`, `AnalyzeShellCommand`) took a
  concrete `*Agent` and reached in only two places (`agent.getClient()`,
  `agent.GetModel()`). The seam: the three entrypoints now take
  `(client api.ClientInterface, model string, …)` — the caller resolves the
  client + model (including the session-model override). A
  `pkg/agent/security_analyzer_forwarders.go` keeps the original
  `*Agent`-based signatures for every in-package call site
  (`approval_broker.go`, `agent_accessors.go`), so no caller changed.
  `SecurityAnalysis`, `SecurityAnalysisCache`, `Chain`, `ParseChain`,
  `ChainCacheKey`, `NormalizeChain`, `NewSecurityAnalysisCache`, and
  `MaxChainSubcommandsForBatchPrompt` are type aliases/forwarders into
  `approvals`; the chain/cache files moved as pure moves (only the
  `package` line + file-reference comment changed).
  - **Behavior-preservation detail:** the original `AnalyzeChain` ran the
    long-chain dispatch *before* the client-nil check, so a long chain with
    no client still returned a synthesized (best-effort) entry. The moved
    version preserves that ordering exactly (dispatch → then `client==nil`
    check). The `*Agent`-based nil guard (`agent == nil`) was dropped since
    the entrypoints are now client-injected; a `nil` client is still
    handled.
  - **Test split:** `security_analyzer_test.go` was 1873 lines and mixed the
    analyzer tests (lines 1–1397: 41 tests + 4 mock clients) with the
    file-access conformance tests (lines 1399–1873: `NonTmpTempDir`/
    `externalTempDir` + `TestClassifyFileAccess_Conformance` + the
    `TestClassifyFileAccess_*` / `TestStaticGateAutoApprove_*` battery). The
    analyzer half moved to `approvals/security_analyzer_test.go` (the mock
    clients implement `api.ClientInterface` directly, so the
    `&Agent{}`/`setClient` setup became `client.GetModel()` — no `*Agent`
    needed); the conformance half stayed in `pkg/agent/security_analyzer_test.go`.
    One deliberate, documented nuance (reviewed): the pre-move "nil agent"
    fast error is gone — a nil `*Agent` degrades to a nil client, so the
    fallback/long-chain path now synthesizes instead of erroring (unreachable
    in production; pinned by `TestAnalyzeChainFallback_NilClient_ReturnsSynthesized`).
  - **Verification:** `go build ./...` + `go vet` + `gofmt` clean;
    content-identity on all 4 moved production files (only the seam lines
    differ); `pkg/agent/approvals` suite green (41 tests); `pkg/agent`
    suite green in isolation (the `[state-leak]` failure is the
    documented environmental false positive — my own live orchestrator
    session journals into the real state dir during the run);
    `agent_tools`/`webui`/`console`/`utils` suites green. The WebUI JS
    `build-all` step OOMs on this machine (JavaScript heap limit),
    unrelated to this Go-only change.
  - **Remaining approvals work (future increments):** the
    approval-broker/allowlist/risk-orchestrator files
    (`approval_broker.go`, `approval_allowlist.go`, `risk_assessment.go`,
    `agent_security.go`, `agent_risk.go`, `risk_prompt.go`,
    `shell_approval*.go`, `tool_security*.go`, `edit_approval.go`,
    `security_circuit_breaker.go`, `seed_tool_security.go`,
    `submanager_*_security.go`) still carry `*Agent` methods and reach into
    `Agent` fields — they follow the `ChangeAgent`-seam pattern (define a
    narrow `ApprovalAgent` interface, move, forward) in a later increment.
    (The `path_tier.go`/`access_mode.go` "risk input" foundation landed in
    increment 2 above; `path_tier_integration_test.go`/
    `path_tier_gate_test.go` stayed as `applyFilesystemDecision` integration
    tests.)
- **Phase 3 (2026-09-27): `pkg/agent/approvals` increment 2 — the path-tier
  + access-mode foundation landed.** `path_tier.go` (the `PathTier`
  classifier: `ClassifyPathAccess`, the four `PathTier*` tiers,
  `NormalizePath`, `IsUnderPrefix`, `DetectHomeDir`) and `access_mode.go`
  (`AccessModeForTool`) moved into `pkg/agent/approvals`. Pure, agent-free:
  the two files had zero `*Agent` coupling (the classifier takes explicit
  `workspaceRoot`/`homeDir`/`cwd` args; `DetectHomeDir` is a test-override
  hook). They are the "risk inputs" named in the spec's `approvals`
  target. The forwarder `pkg/agent/path_tier_forwarders.go` keeps every
  in-package call site working via aliases + one-line forwarders:
  `PathTier` (type alias), `PathTierUnknown`/`Workspace`/`External`/
  `Sensitive` (const aliases), `ClassifyPathAccess`, lowercase
  `normalizePath`/`isUnderPrefix`/`accessModeForTool` (forwarded to the
  exported `approvals.NormalizePath`/`IsUnderPrefix`/`AccessModeForTool`),
  and `detectHomeDir` (a delegating `var` so the test-override hook keeps
  working for `risk_assessment.go`/`tool_security_paths.go`).
  - **Test split:** `path_tier_test.go` interleaved pure classifier tests
    (5, lines 1–143 ∪ 205–236) with two `AgentSecurityManager` tests
    (144–204, `NewAgentSecurityManager` — an `*Agent`-adjacent type that
    stays in `pkg/agent`). The 5 pure tests moved to
    `approvals/path_tier_test.go`; the 2 manager tests stay in
    `pkg/agent/path_tier_test.go`. (`path_tier_gate_test.go` +
    `path_tier_integration_test.go` are `applyFilesystemDecision`
    integration tests — they stayed.)
  - **Verification:** `go build ./...` + `go vet` + `gofmt` clean;
    content-identity on both moved production files (every line maps 1:1
    except the `package` line + the 4 intentional lowercase→exported
    renames); `pkg/agent/approvals` suite green.
- Phases 4–5 pending (`subagents`, `tools`).

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
