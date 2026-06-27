# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

**Status of related specs:** SP-063 (`computer_user` persona) is **partially implemented** — its core shipped; remaining work (panic key 4g, destructive-app denylist 4h) is tracked in `roadmap/SP-063-computer-use-persona.md`, not here. SP-073 (`cooperative cancellation`) shipped 2026-06-26 — all three phases green (TODO(SP-034-1c) markers cleared); further work would be new tickets, not this list.

## SP-079: Migrate Stub Tool Handlers off the Legacy `*Agent` Path
_Spec: `roadmap/SP-079-migrate-stub-tool-handlers.md` (status: 📋 Spec; new ticket from a TODO cluster identified 2026-06-27)_

LLM usability (Medium-High, ~1–2 weeks): 5 handlers in the new `pkg/agent_tools/*_handler.go` path (`analyze_image_content`, `analyze_ui_screenshot`, `browse_url`, `activate_skill`, `web_search`) are stubs that return `requires full *Agent refactoring` errors. SP-074 explicitly deferred "every handler's rewrite" as out of scope; this finishes that deferred work.

- [ ] SP-079-1: Extend `ToolEnv` with `VisionProcessor *VisionProcessor`, `WebBrowser WebBrowser`, `SkillLoader SkillLoader`, `SearchEngine SearchEngine` fields; populate them in the dispatch shim (`pkg/agent/tool_definitions.go`).
- [ ] SP-079-2: Rewrite `analyze_image_content` and `analyze_ui_screenshot` handlers to call `env.VisionProcessor.AnalyzeImage(ctx, path, prompt)`.
- [ ] SP-079-3: Rewrite `browse_url` handler to call `env.WebBrowser.BrowseURL(ctx, url, opts)`.
- [ ] SP-079-4: Rewrite `activate_skill` handler to call `env.SkillLoader.Load(ctx, skillID)` and format instructions.
- [ ] SP-079-5: Rewrite `web_search` handler to call `env.SearchEngine.Search(ctx, query)`.
- [ ] SP-079-6: Add 4 conformance tests in `pkg/agent_tools/new_tools_conformance_test.go` (one per handler) that compare output against the legacy handler for the same args.
- [ ] SP-079-7: Once all 5 are migrated, remove the "thin wrappers … pending full refactoring" caveat from `pkg/agent_tools/handler.go:43`. Acceptance: `grep -rn "requires full.*Agent refactoring" pkg/agent_tools/` returns nothing; all 5 conformance tests pass; `make build-all` clean; legacy handler tests still green.

## SP-080: Type the Unknown-Tool Error in ToolRegistry
_Spec: `roadmap/SP-080-type-unknown-tool-error.md` (status: 📋 Spec; residual tech debt)_

Cleanup (Low, ~half a day): `pkg/agent/tool_executor_sequential.go:166` falls back to `strings.Contains(err.Error(), "unknown tool")` because the new registry returns a plain error rather than the typed `agenterrors.NewInvalidInputError` that already exists.

- [ ] SP-080-1: In `pkg/agent_tools/registry.go::ExecuteTool`, replace the unknown-tool `fmt.Errorf("unknown tool: %s", name)` with `agenterrors.NewInvalidInputError("unknown tool: "+name, nil)`.
- [ ] SP-080-2: Remove the `strings.Contains(err.Error(), "unknown tool")` branch from `pkg/agent/tool_executor_sequential.go:166`; rely solely on `agenterrors.IsInvalidInput(err)`.
- [ ] SP-080-3: Update tests in `pkg/agent_tools/new_tools_conformance_test.go` and `pkg/agent_tools/registry_integration_test.go` to assert `agenterrors.IsInvalidInput` rather than substring matching.
- [ ] SP-080-4: Acceptance: `grep -rn '"unknown tool"' pkg/agent_tools/ pkg/agent/` returns nothing in non-test code; `go test ./pkg/agent_tools/... ./pkg/agent/...` green.

## SP-081: Delete the Dead `pkg/tools/global.go` Executor
_Spec: `roadmap/SP-081-delete-dead-global-executor.md` (status: 📋 Spec; cleanup)_

Cleanup (Low, ~1 hour): `pkg/tools/global.go` exposes `InitializeGlobalExecutor` / `GetGlobalExecutor` / `ExecuteWithGlobal` with **zero non-test callers** (verified via `grep`). The init function carries a misleading "TODO: Make this configurable based on security settings" comment that has no consumer.

- [ ] SP-081-1: Audit callers (`grep -rn "tools\.GetGlobalExecutor\|tools\.InitializeGlobalExecutor\|tools\.ExecuteWithGlobal" pkg/ cmd/`); confirm zero non-test callers.
- [ ] SP-081-2: Delete `pkg/tools/global.go`.
- [ ] SP-081-3: Delete `pkg/tools/executor_behavior_test.go` if its only purpose was the global executor; otherwise prune the global-executor cases and keep the rest.
- [ ] SP-081-4: Acceptance: file does not exist; grep returns zero matches; `go build ./...` clean; `go test ./...` green.

## SP-082: Preserve Key Insertion Order in Structured File Tools
_Spec: `roadmap/SP-082-preserve-structured-file-key-order.md` (status: 📋 Spec; supersedes original `roadmap/SP-066-structured-file-key-order.md` with concrete plan)_

UX / diff hygiene (Medium, ~half a week): `write_structured_file` and `patch_structured_file` use `map[string]interface{}` internally, which `encoding/json` and `gopkg.in/yaml.v2` both emit in alphabetical order. `package.json` and similar convention-driven formats lose readability, and every patch produces a full re-sort diff even for 1-field changes.

- [ ] SP-082-1: Replace `map[string]interface{}` with `*yaml.Node` (gopkg.in/yaml.v3) as the internal representation in `pkg/agent_tools/write_structured_file.go` and `pkg/agent_tools/patch_structured_file.go`.
- [ ] SP-082-2: Update `patch_structured_file` to read the original file as a `*yaml.Node` and apply patches via an adapter (`yamlNodeFromMap` / `mapFromYamlNode`).
- [ ] SP-082-3: Add round-trip tests in `pkg/agent_tools/write_structured_handler_test.go` and `pkg/agent_tools/patch_structured_handler_test.go` — JSON, YAML, and `package.json`-style ordering.
- [ ] SP-082-4: Update the tool definitions to note that key order is preserved. Acceptance: a `package.json` written via the tool retains the LLM-supplied key order; a 1-field patch produces a 1-line diff; `go test ./pkg/agent_tools/...` green; existing patch tests still pass.

## SP-069: PR Creation — credential-store lookup for GitHub token
_Spec: `roadmap/SP-069-pull-request-creation.md` (status: ✅ Implemented; one residual TODO)_

Follow-up (Low, ~half a day): the PR-creation flow's REST-API branch (`pkg/git/pull_request.go:171`) only checks `GH_TOKEN` env var, not the credential store. The spec explicitly documents "credential store → `GH_TOKEN` → `gh`" as the resolution order, and `cmd/github_setup_prompt.go` already prompts to configure `gh`/token — the token just isn't consulted from where the user stored it.

- [x] SP-069-cred: In `pkg/git/pull_request.go`, before the `GH_TOKEN` lookup, check `pkg/credentials` for a `github` provider entry (resolution order per spec §Open Questions: credential store → `GH_TOKEN` → `gh`). Update `cmd/github_setup_prompt.go`'s existing flow to write to the credential store on success so the next PR doesn't need `GH_TOKEN` in env. Add a unit test covering all three branches. Acceptance: a user who runs `sprout setup github` once can open subsequent PRs without exporting `GH_TOKEN`; the spec's resolution order is honored; existing PR tests still pass. _Shipped in `bb37657a` — resolution order honored via `resolveGitHubCredential` hook (default `credentials.GetFromActiveBackend("github")`); `cmd/github_setup_prompt.go` now writes the PAT to the credential store after successful save (soft-fail on credential-store error). New tests: `TestCreatePullRequest_TokenResolutionOrder` (4 subtests) + `TestPromptGitHubMCPSetupIfNeeded_CredentialStore{Save,NoPATToStore,Error}`._

## SP-078: Steer-Panel UX Parity — Wrap-Aware Rendering, Tab Completion
_Spec: `roadmap/SP-078-steer-panel-ux-parity.md` (status: 📋 Proposed; Phases 1–3 shipped)_

UX parity (Medium): the pinned steer-input panel (`pkg/console/steer_input.go`, 1428 LOC) lacks the wrap-aware render path and Tab completion that the regular `InputReader` has across `pkg/console/input_*.go` (~6300 LOC across 18 files). User-visible: long single-line steers overflow horizontally, the caret lands off-column on wrapped multi-line steers, and there's no slash-command completion mid-turn (Tab is reserved for STEER↔QUEUE mode toggle). All five prior steer fixes (`e830d113`, `8f501bd3`, `6714f690`, `eb441143`, `ac75f0ed`) remain green.

### Phase 4 — close-out

- [x] SP-078-4: ✅ Closed — `grep -rn "TODO(SP-078)" pkg/console/` is empty; `make build-all` + `go test ./...` green; regression artifact added at `pkg/console/steer_wrap_snapshot_test.go` (10 subtests: narrow ASCII, wide CJK, combining chars, wrap boundary, overflow, empty, single line, hard line break, cursor at start, very narrow terminal).

## SP-066: Never-Ending Context — Phase 3d tie-breaker + calibration
_Spec: `roadmap/SP-066-never-ending-context.md` (status: ✅ Substantially Shipped; Phase 3d ⏸ deferred)_

Foundation (Low-Medium): Phases 1–3 are shipped except 3d (embedding-driven rollup clustering) and an adjacent calibration question about `rollupTriggerCount + recentTurnsToPreserve = 30` being set higher than real workloads exercise. Both are tagged "don't pick up without first revisiting whether rollups even fire."

- [x] SP-066-A: First, run the adjacent calibration experiment — drop `rollupTriggerCount + recentTurnsToPreserve` from 30 → 20 (or 15) in `pkg/agent/rollup.go`, observe whether real-world sessions routinely cross the new threshold. Acceptance: telemetry from `rollup.go` shows rollups firing on at least one non-test session in the developer's local `~/.sprout/sessions/` corpus; the call site change is one constant. _Verified shipped: `rollupSourceCount = 15` + `recentTurnsToPreserve = 5` = 20 (already at the spec's "20 or 15" target); rollup wiring unchanged._
- [x] SP-066-3d: ⏸ Gate fails — still dormant. Calibration telemetry audit (347 sessions, 4569 checkpoints) shows zero post-calibration real sessions crossed 20 Level-0 checkpoints. Longest post-cal real session peaked at 16. Zero post-cal rollups fired. Embedding tie-breaker deferred indefinitely until real workloads routinely exceed the threshold.

## SP-056: CLI Reasoning Fold — Collapsed Thinking Indicator
_Spec: `roadmap/SP-056-cli-reasoning-fold.md` (status: 📋 Proposed)_

UX (Low-Medium, ~1 day): CLI has only two reasoning display modes — hidden (silence) or full (dim wall of CoT). Add a third `fold` mode: a single pinned `⋯ thinking · N tokens · T elapsed` line that updates in place every ~100ms during the thinking phase, then resolves to `⋯ thought for 1.2k tokens · 3.4s` when assistant text begins. Built on the existing `OutputRouter` + `ActivityIndicator`.

- [x] SP-056-1: ✅ `pkg/console/reasoning_fold.go` shipped — `ReasoningFold` struct (`indicator *ActivityIndicator`, `startedAt`, `tokenEstimate`, `active`, `mu`, plus render state); `Start()`, `Chunk(text)`, `Resolve()`, `Interrupt()`, `IsActive()`. Token estimate uses `len(text)/4` per chunk; refresh ~100ms on TTY; non-TTY degraded to single Fprintln per burst + summary.
- [x] SP-056-2: ✅ `ActivityIndicator.SetStatic(line)` + `ClearStatic()` added — pins a non-animated line on the indicator row, no spinner frames; both no-op on non-TTY.
- [x] SP-056-3: ✅ New `--reasoning=<mode>` flag (`hidden` | `fold` | `full`) added in `cmd/agent_command.go`; legacy `--show-reasoning-terminal` (the actual flag in the codebase; the TODO said `--show-reasoning`) preserved as back-compat alias mapping to `full`. Validation rejects unknown values with allowed-list error.
- [x] SP-056-4: ✅ `OutputRouter.SetReasoningCallback(fn)` was already present (line 77); confirmed wiring: reasoning chunks go only to the callback when set, terminal stream is suppressed for reasoning content. Test coverage extended in `output_router_test.go`.
- [x] SP-056-5: ✅ `cmd/agent_modes.go::SetupAgentEvents` wires `fold.Chunk(chunk)` on the reasoning callback when `reasoningMode == "fold"`; `fold.Resolve()` is called on the first non-empty assistant stream chunk before the existing prose path. Fold instance lives in `currentReasoningFold` and is reset per turn.
- [x] SP-056-6: ✅ All four edge cases: (a) Resolve is idempotent and safe to call on tool events too; (b) multi-burst produces one resolved line per Start/Resolve cycle; (c) non-TTY / nil-indicator path emits a single Fprintln at Start + summary at Resolve; (d) `Interrupt()` emits `⋯ thinking interrupted (N tokens)` distinct summary, wired to the SIGINT path in `agent_modes.go:326`.
- [x] SP-056-7: ✅ Tests in `pkg/console/reasoning_fold_test.go` (8 tests): nil-indicator degraded mode, token estimate accumulation across chunks, idempotent Resolve, idempotent Interrupt, multi-burst, Interrupt distinct summary, active-indicator no-panic, active-indicator interrupt no-panic. All green.

## Automation-Process: Workflow TODO Processor Issues (3 issues from workflow diagnostics)
_Inline diagnosis (handled directly by orchestrator, NOT delegated to workflow): during a workflow diagnostic run we observed (1) failing webui tests, (2) the workflow-automation skill lacks details of the actual coordinated flow, (3) subagent provider/model sometimes diverges from `subagent_overrides`. All three fixed in-place by the orchestrator this session._

- [x] SP-AUTO-1: Fix two failing webui tests `TestAutomateSessionsAll_DispatchEmptyPathToList` and `TestAutomateIntegration_FullWorkflow` — both decode bare arrays from API responses that actually return wrapped objects (`{"workflows":[...]}`, `{"sessions":[...]}`); updated test decode to use the wrapped shape. Acceptance: `go test ./pkg/webui/...` green. _Fixed: tests now use the wrapped-envelope decode pattern that the other tests in the same file already use._
- [x] SP-AUTO-2: Expand `pkg/skills/library/workflow-automation/SKILL.md` with the canonical coordinated flow (coordinator → orchestrator → leaf workers) — added three sections: "The Coordinated Flow — How a Workflow Actually Runs" (persona chain + separation-of-concerns matrix), "subagent_overrides — The Resolution Chain" (4-level resolution order + silent-skip cases + debugging), "Reading the Canonical Example — automate/workflow.json" (field-by-field walkthrough). _Done._
- [x] SP-AUTO-3: Diagnose & fix subagent model/provider divergence — empirically confirmed via a live workflow run that the persona-keyed override chain works correctly (`subagent [orchestrator|coder|tester] starting · ai-worker/qwen3.6-27b` matched the workflow JSON's `subagent_overrides`). Two silent-divergence paths fixed: (a) added INFO log lines in `applyWorkflowSubagentOverrides` for every skip case (unknown persona, disabled persona, empty override) and every successful apply; (b) added `pickSubagentDefault` helper + global-default seeding in `applyWorkflowRuntimeOverrides` so no-persona `run_subagent` calls inside a workflow pick up a workflow-appropriate model instead of inheriting the coordinator's primary model. Also added no-persona spawn observability in `tool_handlers_subagent_spawn.go`. New test `TestPickSubagentDefault` with 7 sub-tests. _Done._

## SP-WASM: Pre-existing WASM build break (incidental fix)
_Not part of the original 3 issues, but blocked `make build-all` verification required by AGENTS.md. Pre-existing bug from commit `92e8fa07` (feat: background-process orphan cleanup) — not caused by these changes._

- [x] SP-WASM-1: Fix WASM build break in `pkg/agent/agent_creation.go:136` — referenced `tools.GetBackgroundOutputBaseDir()` / `tools.CleanupOrphanedBackgroundProcesses()` which live in a `!js`-only file. Extracted the orphan-cleanup block into build-tagged helpers (`background_cleanup_desktop.go` for `!js`, `background_cleanup_wasm.go` as no-op stub for `js`). `make build-all` now passes.
