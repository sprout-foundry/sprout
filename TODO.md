# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

**Status of related specs:** SP-063 (`computer_user` persona) is **partially implemented** — its core shipped; remaining work (panic key 4g, destructive-app denylist 4h) is tracked in `roadmap/SP-063-computer-use-persona.md`, not here. SP-073 (`cooperative cancellation`) shipped 2026-06-26 — all three phases green (TODO(SP-034-1c) markers cleared); further work would be new tickets, not this list.

## SP-078: Steer-Panel UX Parity — Wrap-Aware Rendering, Tab Completion
_Spec: `roadmap/SP-078-steer-panel-ux-parity.md` (status: 📋 Proposed; Phases 1–3 shipped)_

UX parity (Medium): the pinned steer-input panel (`pkg/console/steer_input.go`, 1428 LOC) lacks the wrap-aware render path and Tab completion that the regular `InputReader` has across `pkg/console/input_*.go` (~6300 LOC across 18 files). User-visible: long single-line steers overflow horizontally, the caret lands off-column on wrapped multi-line steers, and there's no slash-command completion mid-turn (Tab is reserved for STEER↔QUEUE mode toggle). All five prior steer fixes (`e830d113`, `8f501bd3`, `6714f690`, `eb441143`, `ac75f0ed`) remain green.

### Phase 4 — close-out

- [ ] SP-078-4: `grep -rn "TODO(SP-078)" pkg/console/` is empty; `make build-all` + `go test ./...` green; add a recording-style screenshot or `browse_url` snapshot of a wrapped steer so future regressions surface at review time.

## SP-066: Never-Ending Context — Phase 3d tie-breaker + calibration
_Spec: `roadmap/SP-066-never-ending-context.md` (status: ✅ Substantially Shipped; Phase 3d ⏸ deferred)_

Foundation (Low-Medium): Phases 1–3 are shipped except 3d (embedding-driven rollup clustering) and an adjacent calibration question about `rollupTriggerCount + recentTurnsToPreserve = 30` being set higher than real workloads exercise. Both are tagged "don't pick up without first revisiting whether rollups even fire."

- [x] SP-066-A: First, run the adjacent calibration experiment — drop `rollupTriggerCount + recentTurnsToPreserve` from 30 → 20 (or 15) in `pkg/agent/rollup.go`, observe whether real-world sessions routinely cross the new threshold. Acceptance: telemetry from `rollup.go` shows rollups firing on at least one non-test session in the developer's local `~/.sprout/sessions/` corpus; the call site change is one constant. _Verified shipped: `rollupSourceCount = 15` + `recentTurnsToPreserve = 5` = 20 (already at the spec's "20 or 15" target); rollup wiring unchanged._
- [ ] SP-066-3d: If and only if calibration shows rollups are now routine, implement `Embeddings as a rollup tie-breaker` — (a) cluster N per-turn checkpoints by embedding similarity before summarizing (tighter per-cluster summaries than one monolithic LLM call); (b) detect topic shifts as natural rollup boundaries via sharp similarity drops between turns. Acceptance: rollups emit per-cluster summaries when topic shifts are detected; existing rollup tests still green; `go test ./pkg/agent/...` passes.

## SP-056: CLI Reasoning Fold — Collapsed Thinking Indicator
_Spec: `roadmap/SP-056-cli-reasoning-fold.md` (status: 📋 Proposed)_

UX (Low-Medium, ~1 day): CLI has only two reasoning display modes — hidden (silence) or full (dim wall of CoT). Add a third `fold` mode: a single pinned `⋯ thinking · N tokens · T elapsed` line that updates in place every ~100ms during the thinking phase, then resolves to `⋯ thought for 1.2k tokens · 3.4s` when assistant text begins. Built on the existing `OutputRouter` + `ActivityIndicator`.

- [ ] SP-056-1: New `pkg/console/reasoning_fold.go` — `ReasoningFold` struct (`indicator *ActivityIndicator`, `startedAt`, `tokenEstimate`, `active`, `mu`). Methods: `Start()` (begin tracking + spawn updating line), `Chunk(text string)` (ingest one chunk, update count), `Resolve()` (emit summary, clear pinned line). Token estimate uses byte/4 heuristic (UX feel, not billing accuracy).
- [ ] SP-056-2: Extend `pkg/console/activity_indicator.go` — add `SetStatic(line string)` that pins a non-animated line to the same row (for fold mode without a spinning frame).
- [ ] SP-056-3: Replace binary `--show-reasoning` flag with `--reasoning=<mode>` (`hidden` (default), `fold` (new), `full` (was `--show-reasoning=true`)). Keep `--show-reasoning` as back-compat alias for `--reasoning=full`.
- [ ] SP-056-4: `pkg/agent/output_router.go` — add `SetReasoningCallback(fn func(chunk string))` parallel to `EnableStreaming`; CLI plumbs fold updates without changing the WebUI event-bus contract.
- [ ] SP-056-5: `cmd/agent_modes.go::SetupAgentEvents` — wire `fold.Chunk(chunk)` on reasoning events when `reasoningMode == ReasoningFold`; on first assistant stream chunk, call `fold.Resolve()` before falling through to the existing stream-chunk path.
- [ ] SP-056-6: Edge cases — (a) resolve on first tool event when reasoning ends with no assistant text; (b) each burst gets its own resolved line for multi-burst sequences; (c) `NO_COLOR`/non-TTY degrades to single Fprintln per chunk burst + summary at end; (d) Ctrl+C interrupt resolves to `⋯ thinking interrupted (N tokens)` instead of orphan "thinking" line.
- [ ] SP-056-7: Tests — Start/Chunk/Resolve lifecycle, NO_COLOR degradation, interrupt path, multi-burst sequences. Acceptance: with `--reasoning=fold` (or once it's default), reasoning-heavy turns show a live-updating progress line during the thinking phase; resolved summary stays in scrollback; fold line never clobbers tool-spinner rows or assistant streaming.

## Automation-Process: Workflow TODO Processor Issues (3 issues from workflow diagnostics)
_Inline diagnosis (handled directly by orchestrator, NOT delegated to workflow): during a workflow diagnostic run we observed (1) failing webui tests, (2) the workflow-automation skill lacks details of the actual coordinated flow, (3) subagent provider/model sometimes diverges from `subagent_overrides`. All three fixed in-place by the orchestrator this session._

- [x] SP-AUTO-1: Fix two failing webui tests `TestAutomateSessionsAll_DispatchEmptyPathToList` and `TestAutomateIntegration_FullWorkflow` — both decode bare arrays from API responses that actually return wrapped objects (`{"workflows":[...]}`, `{"sessions":[...]}`); updated test decode to use the wrapped shape. Acceptance: `go test ./pkg/webui/...` green. _Fixed: tests now use the wrapped-envelope decode pattern that the other tests in the same file already use._
- [x] SP-AUTO-2: Expand `pkg/skills/library/workflow-automation/SKILL.md` with the canonical coordinated flow (coordinator → orchestrator → leaf workers) — added three sections: "The Coordinated Flow — How a Workflow Actually Runs" (persona chain + separation-of-concerns matrix), "subagent_overrides — The Resolution Chain" (4-level resolution order + silent-skip cases + debugging), "Reading the Canonical Example — automate/workflow.json" (field-by-field walkthrough). _Done._
- [x] SP-AUTO-3: Diagnose & fix subagent model/provider divergence — empirically confirmed via a live workflow run that the persona-keyed override chain works correctly (`subagent [orchestrator|coder|tester] starting · ai-worker/qwen3.6-27b` matched the workflow JSON's `subagent_overrides`). Two silent-divergence paths fixed: (a) added INFO log lines in `applyWorkflowSubagentOverrides` for every skip case (unknown persona, disabled persona, empty override) and every successful apply; (b) added `pickSubagentDefault` helper + global-default seeding in `applyWorkflowRuntimeOverrides` so no-persona `run_subagent` calls inside a workflow pick up a workflow-appropriate model instead of inheriting the coordinator's primary model. Also added no-persona spawn observability in `tool_handlers_subagent_spawn.go`. New test `TestPickSubagentDefault` with 7 sub-tests. _Done._

## SP-WASM: Pre-existing WASM build break (incidental fix)
_Not part of the original 3 issues, but blocked `make build-all` verification required by AGENTS.md. Pre-existing bug from commit `92e8fa07` (feat: background-process orphan cleanup) — not caused by these changes._

- [x] SP-WASM-1: Fix WASM build break in `pkg/agent/agent_creation.go:136` — referenced `tools.GetBackgroundOutputBaseDir()` / `tools.CleanupOrphanedBackgroundProcesses()` which live in a `!js`-only file. Extracted the orphan-cleanup block into build-tagged helpers (`background_cleanup_desktop.go` for `!js`, `background_cleanup_wasm.go` as no-op stub for `js`). `make build-all` now passes.
