# SP-141: pkg/agent Package Decomposition

**Status:** In progress — phases 1–2 shipped 2026-09-26; phase 3 (`approvals`) complete (increments 1–6 landed 2026-09-27/29 — security-analyzer, path-tier, allowlist, risk vocabulary, broker behind `ApprovalAgent`, `ResolveToolRisk` behind `RiskAgent`); phase 4 (`subagents`) in progress (increment 1 — data foundation — landed 2026-09-29); phase 5 pending
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
    approval-broker/risk-orchestrator files
    (`approval_broker.go`, `risk_assessment.go`,
    `agent_security.go`, `agent_risk.go`, `risk_prompt.go`,
    `shell_approval*.go`, `tool_security*.go`, `edit_approval.go`,
    `security_circuit_breaker.go`, `seed_tool_security.go`,
    `submanager_*_security.go`) still carry `*Agent` methods and reach into
    `Agent` fields — they follow the `ChangeAgent`-seam pattern (define a
    narrow `ApprovalAgent` interface, move, forward) in a later increment.
    (`approval_allowlist.go` landed in increment 3 below.)
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
- **Phase 3 (2026-09-27): `pkg/agent/approvals` increment 3 — the
  shell-command allowlist + persistence landed.** The four pure allowlist
  functions from `pkg/agent/approval_allowlist.go` moved to
  `pkg/agent/approvals/allowlist.go`, parameterized on
  `*configuration.Config` (lookup) / `*configuration.Manager` (persistence)
  instead of `*Agent`: `IsShellCommandAllowlisted`,
  `PersistShellCommandAllowlist`, `PersistShellCommandPattern`,
  `PersistShellCommandAskPolicy`. `pkg/agent/approval_allowlist.go` is now
  a thin forwarder file: the four `*Agent` methods keep their exact
  signatures (the broker, the tool-security gates, and the seed-time
  security checks call them) and resolve the config/manager via
  `a.GetConfig()`/`a.GetConfigManager()`, preserving the pre-move
  `agent == nil` guard (the persist forwarders return
  `approvals.ErrNilAgent`, the pre-move "nil agent" Permission error).
  `ElevateSessionToPermissive` did NOT move — it mutates agent-local
  risk-profile state (`a.SetRiskProfileOverride`), which is out of scope
  for the config/manager seam.
  - **Import-safety note:** `pkg/agent/approvals` now imports
    `pkg/configuration` (for the `Config`/`Manager`/`CommandPolicies`
    types). This is cycle-free: `pkg/configuration` does not import the
    `pkg/agent` parent (the `pkg/agent → approvals` edge stays one-way).
  - **Tests:** new `approvals/allowlist_test.go` covers the moved logic on
    bare configs + a real isolated `configuration.NewTestManager`
    (persist→lookup round trips, idempotency, ask-policy rule shape,
    validation ordering: empty-input check precedes the nil-manager check).
    The existing `pkg/agent/approval_allowlist_test.go` now exercises the
    forwarders unchanged.
  - **Still `*Agent`-coupled (next increments, interface-seam work):**
    `approval_broker.go` (RequestApproval — 15+ surface items: config,
    event bus, security-approval mgr, unsafe flags, debug logger,
    interrupt ctx, webui-client check, workflow-approval marking).
    These follow the `changes.AgentView`-style narrow-interface seam.
- **Phase 3 (2026-09-28): `pkg/agent/approvals` increment 4 — the
  risk-assessment vocabulary + pure decision helpers landed.** The
  canonical risk vocabulary moved from `pkg/agent/risk_assessment.go` to
  `pkg/agent/approvals/risk_assessment.go`: `RiskSource` + the 10
  `RiskSource*` consts, the `RiskAssessment` struct, `AssessmentFromClassifier`,
  `AssessmentFromPersonaCascade`, `RiskAssessment.Combine` (was
  unexported `combine`), `MergeRiskSources` (was `mergeRiskSources`),
  `RiskAssessment.Explain`, `ResolveOldDecision` / `ResolveUnifiedDecision`
  (shadow-mode comparators), and `IsGitRebaseCommand`. The git-command
  gate detectors (`IsGitWriteCommand`, `IsGitStashCommand` — pure
  strings/shelltext classifiers) moved from `pkg/agent/tool_handlers.go`
  to `pkg/agent/approvals/git_command_gates.go`. `pkg/agent` keeps the
  `*Agent` orchestrator `ResolveToolRisk` (it reaches into
  `a.EvaluateOperationRisk`, `a.effectiveCwd`, `a.debug`,
  `a.isGitWriteAllowed`, `a.GetWorkspaceRoot` — interface-seam work for a
  later increment) plus `agent_risk.go` / `risk_prompt.go` (persona
  resolution, active risk profile, request-approval side effects).
  `pkg/agent/risk_assessment_forwarders.go` aliases the moved types/consts
  (`type RiskSource = approvals.RiskSource`, etc.) and forwards the
  unexported helper names, so every call site in `pkg/agent` (the
  tool-handler gates, the seed/tool-security shadow-mode comparisons) is
  unchanged.
  - **Method rename (the one non-pure-move edit):** `combine` → `Combine`
    (Go methods cannot be re-declared across packages); the 8 call sites
    in `ResolveToolRisk` and the pure tests update from `.combine(` to
    `.Combine(`.
  - **Import-safety note:** `approvals → agent_tools` for
    `SecurityResult` (the classifier input) is cycle-free —
    `pkg/agent_tools` does not import the `pkg/agent` parent (it already
    holds this edge from the security-analyzer files); `configuration`,
    `security`, `shelltext`, and `utils` likewise don't import the agent
    parent.
  - **Tests:** the 15 pure tests moved to
    `approvals/risk_assessment_test.go` (AssessmentFromClassifier/
    PersonaCascade mapping, Combine edge cases, Explain, MergeRiskSources,
    ResolveOldDecision/ResolveUnifiedDecision, the golden Phase-1 mapping
    block); the 30 `*Agent`-based tests (ResolveToolRisk battery,
    shadow-mode parity, TestRiskLevelRank, TestAccessModeForTool,
    TestConfigUnifiedRiskResolver_DefaultFalse) stay in
    `pkg/agent/risk_assessment_test.go` and exercise the forwarders.
  - **Still `*Agent`-coupled (next increment, interface-seam work):**
    the `ResolveToolRisk` orchestrator + `agent_risk.go`/`risk_prompt.go`
    persona/risk-profile methods. These follow the
    `changes.AgentView`-style narrow-interface seam (a `RiskAgent`
    interface over the ~10 exported-method surface).
- **Phase 3 (2026-09-28): `pkg/agent/approvals` increment 5 — the
  approval broker landed behind the `ApprovalAgent` seam.** The broker
  body (`RequestApproval`, ~420 lines: command-policy → allowlist →
  unsafe/elevated bypasses → optional LLM security analysis → WebUI/CLI
  interactive surfaces → permissive fallback) moved to
  `pkg/agent/approvals/broker.go` as a package function operating on the
  new `ApprovalAgent` interface (`approvals/approval_agent.go`, 23
  members), with `BrokerDecision` moved alongside. The `*Agent` method
  `RequestApproval` is now a one-line forwarder in
  `pkg/agent/approval_broker.go` (with `BrokerDecision` aliased). Two
  pure companions moved too: `EvaluateCommandPolicy` (from
  `pkg/agent/command_policy.go` → `approvals/command_policy.go`, the
  whole file + its tests) and `approvalDecisionFromCLIChoice` →
  `approvals/cli_choice.go` (`ApprovalDecisionFromCLIChoice`; a
  2-line forwarder stays in `risk_prompt.go` for the other callers).
  - **Seam design (the `changes.AgentView` pattern, applied to
    approvals):** most interface members are existing exported
    accessors (`GetConfig`, `IsShellCommandAllowlisted`, `GetUnsafeMode`,
    `IsSessionElevated`, `GetSecurityApprovalMgr`, `GetEventBus`, …);
    8 new seam accessors in `pkg/agent/approval_seam_accessors.go`
    expose the private surface — `Client` (getClient), `EffectiveCwd`
    (effectiveCwd), `DebugEnabled`/`DebugLogf` (debug/debugLog),
    `IsNonInteractive`, `GetSecurityAnalysisCache`, `LogSecurityDecision`
    (logSecurityDecision), `ApplyApprovalDecision`
    (applyApprovalDecision), and `ApproveShellCommandParts` (wraps
    NewShellProposal + RequestShellApproval, returning
    decisions/partIDs so the broker's all-parts-approved check stays
    faithful). `InterruptCtx` and `GetModel` were already exported. The
    import arrow is one-way: `pkg/agent → approvals`.
  - **The one non-mechanical edit:** the shell-per-part picker block now
    calls `a.ApproveShellCommandParts(pickerCtx, cmd)` (returns
    decisions + part IDs) instead of constructing the `ShellProposal`
    locally and walking `proposal.Parts` — the all-approved loop walks
    the returned part IDs, same semantics.
  - **Verification:** content-identity confirmed (normalized-diff of the
    moved body vs. the original = only the package/import lines + the
    mechanical renames). Content-identity + go build + go vet clean;
    approvals suite + the broker/allowlist/adapter test batteries green.
- **Phase 3 (2026-09-29): `pkg/agent/approvals` increment 6 — the
  `ResolveToolRisk` orchestrator landed behind the `RiskAgent` seam.**
  The risk resolver (classifier → persona cascade → git gates →
  workspace security policy → filesystem path tiers) moved to
  `pkg/agent/approvals/risk_resolver.go` as a package function operating
  on the new `RiskAgent` interface (`approvals/risk_agent.go`, 11
  members): `GetWorkspaceRoot`, `HasPasswordPrompter`,
  `IsFolderSessionAllowed`, `GetConfig`, `EvaluateOperationRisk`
  (all pre-existing exported methods) plus the seam accessors
  `IsGitWriteAllowed`, `EffectiveCwd`, `HomeDir`, `DebugEnabled`,
  `DebugLogf`. `pkg/agent/risk_assessment.go` is now a thin forwarder
  file (the `*Agent` method delegates to `approvals.ResolveToolRisk`);
  `agent_risk.go` and `risk_prompt.go` stay — they own the state the
  seam reads (persona/risk-profile/subagent fields, the elevation
  flag), and the seam pattern keeps state on the owner.
  - **Seam accessors (new file `pkg/agent/risk_seam_accessors.go`):**
    `IsGitWriteAllowed()` delegates to the existing private
    `isGitWriteAllowed()` (persona git-write capability); `HomeDir()`
    routes through the existing `detectHomeDir` test-override hook (a
    package-level `var = approvals.DetectHomeDir`) so pre-move test
    overrides keep working. `EffectiveCwd`/`DebugEnabled`/`DebugLogf`
    come from `approval_seam_accessors.go` (increment 5). A
    `var _ approvals.RiskAgent = (*Agent)(nil)` compile-time assertion
    guards the seam.
  - **Nil-agent preservation:** the pre-move body was nil-`*Agent`-safe
    (skipped every agent-coupled input, returned the classifier-only
    assessment). A nil `*Agent` boxed into the `RiskAgent` interface is
    non-nil, so the guard lives in the forwarder: `a == nil` →
    `assessmentFromClassifier(ClassifyToolCallWithWorkspace(tool, args,
    ""))`, byte-identical to the pre-move nil path.
    `TestResolveToolRisk_NilAgent` passes unchanged.
  - **Verification:** content-identity confirmed (normalized-diff of
    the moved body vs. the original = 0 lines beyond the renames);
    build + vet + gofmt clean; approvals suite ok; the 37-test risk
    battery (ResolveToolRisk incl. nil-agent, git gates, path tiers,
    workspace policy, shadow-mode parity) all green.
- **Phase 4 (2026-09-29): `pkg/agent/subagents` increment 1 — the
  subagent data foundation landed.** The wire/result types
  (`SubagentStatus` + the six status consts, `FileChange`,
  `SubagentRunMetrics`, `SubagentReturn`, `ProgressEntry`,
  `SubagentError`, `SubagentOptions`, `SharedState`, `SubagentResult`,
  `SubagentProgressEntry`, `SubagentTask`, `SubagentMetrics`), the pure
  helpers (`IsOutputComplete`, `ProgressLogCap`), the terminal display
  (`PrintSubagentStart`/`PrintParallelSubagentStart`/
  `PrintSubagentDone` + `compactCount`/`plural`/the stat suffix), the
  spawn-time `AppendSubagentPreamble`, and the process-wide
  active-subagent counter + `BuildSubagentPrefix` moved to
  `pkg/agent/subagents` (types.go, display.go, preamble.go,
  lifecycle.go). `pkg/agent/subagent_forwarders.go` re-exports the types
  as aliases and forwards the six lowercase call-site names so the tool
  handlers, the task runner, and the workflow wiring are unchanged.
  `subagent_types.go` keeps only `SubagentRunner`/`runningSubagent`
  (they hold `*Agent` fields directly — the runner seam is the next
  increment); `subagent_lifecycle.go` keeps the runner construction,
  `Metrics()`, and the lifecycle-event publishers. The three pure test
  files moved with their code.
  - **Import-safety note:** `subagents` imports `changes` (for
    `changes.TrackedFileChange` on `SubagentResult.FileChanges`),
    `console` (display glyphs), `agent_tools`, `configuration`,
    `embedding`, `events` — none of which import the `pkg/agent` parent,
    so the `pkg/agent → subagents` edge stays one-way (no cycle through
    the forwarder).
  - **Verification:** content-identity confirmed (normalized-diff of
    every moved block and test file vs. the originals = 0 lines beyond
    the rename set); build + vet + gofmt clean; the `subagents` suite
    green; the `pkg/agent` subagent batteries (display/prefix/output-
    complete) still pass through the forwarders.
- Phases 4–5 continue (`subagents` runner seam next, then `tools`).

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
   > **Progress (2026-09-26):** Shipped (phase 1). The loop logic, config
   > types, gate parsing, heartbeat, and TODO-file helpers moved to
   > `pkg/agent/workflow/loop.go`; construction stays in
   > `pkg/agent/workflow_wiring.go` behind the `workflow.LoopAgent` seam
   > (an interface over the exported-method surface the loop uses;
   > `*Agent` satisfies it via the `workflowLoopAgent` adapter — the
   > unexported-field construction cannot leave the package). Import
   > arrow is one-way: `pkg/agent` → `pkg/agent/workflow`. Exported
   > surface added: `workflow.RunTodoLoop`, `ParseWorkflowFile`,
   > `GenerateWorkflowSessionID`, `StartWorkflowHeartbeat`, config/result
   > types, `LoopAgent`. The automate tool handler now calls
   > `workflow.ParseWorkflowFile`/`GenerateWorkflowSessionID` and the
   > package-local `RunWorkflowLoopInProcess` wrapper. Tests green
   > (Automate/Workflow/Loop set; pkg/agent_tools full suite); the 26
   > pkg/agent failures are the known encrypted-age-keys environment
   > issue, none workflow-related.
2. `pkg/agent/changes` — self-contained change-tracking cluster with its own test suite.
   > **Progress (2026-09-26):** Shipped (phase 2). The change-tracking
   > cluster (change_tracking*.go, transcript manifest, atomic_write —
   > 13 non-test files) moved to pkg/agent/changes with its tests;
   > pkg/agent keeps changes_seam.go: type aliases (ChangeTracker,
   > TrackedFileChange, …), the changesAgentView adapter (an AgentView
   > interface over the exported-method surface; *Agent adapts through
   > a.changesView()), and construction forwarders. Import arrow is
   > one-way: pkg/agent → changes. Test-visible accessors added on the
   > tracker (SetView, SetRevisionIDForTest, TrackerSessionID,
   > ShellCachePrimed, ApplyShellWalkConfig passthrough already
   > exported); helper-level tests (determineWriteOperation,
   > resolveAbsPath) moved next to the code they test. Gates: build,
   > vet, fmt, lint, lint-go-new 0 issues; pkg/agent/changes + agent_tools
   > suites green; pkg/agent's 26 failures are the known encrypted-keys
   > env issue (identical list to the phase-1 baseline).
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
