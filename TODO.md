# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

**Status of related specs:** SP-063 (`computer_user` persona) is **partially implemented** — its core shipped; remaining work (panic key 4g, destructive-app denylist 4h) is tracked in `roadmap/SP-063-computer-use-persona.md`, not here. SP-073 (`cooperative cancellation`) shipped 2026-06-26 — all three phases green (TODO(SP-034-1c) markers cleared); further work would be new tickets, not this list.

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
