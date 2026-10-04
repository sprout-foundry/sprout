# SP-154 — Agent Task Benchmark

> **Status (2026-10-03):** Proposed.
> Depends on SP-148 (plans), SP-149 (verification as the pass
> criterion), SP-153 (starters). Feeds SP-153c stack skills and model
> recommendations in the provider catalog.

## Problem

Sprout recommends models and ships prompts, skills and robustness
features (SP-125 low-context mode, subagents, tool-call repair), but
nothing measures end-to-end task success:

- `cmd/llm_bench` measures local inference speed, not whether tasks get
  done.
- The workflow harness (`pkg/workflow`, `loop_gate.go`, `automate/*.json`)
  runs and gates agent loops but has no task suite or scoring.
- `pkg/agent/design_e2e_test.go` covers design rendering paths only.

Without numbers, model recommendations and prompt changes are judged by
anecdote, and regressions on a model show up as user reports.

## Design

### 154a. Task suite

Per starter (SP-153), tasks at three sizes: a small change, a new
feature, and a from-scratch app. Each task is:

- A plain-language request.
- A structured plan (SP-148) frozen as a fixture, so runs are comparable
  across models and over time.
- Acceptance criteria inside the plan. Pass/fail comes only from the
  SP-149 verification result, never from the model's own report.

### 154b. Runner

- Headless runs through the existing non-interactive agent path, one
  fresh copy of the starter per task.
- Records per task: acceptance pass/fail, repair attempts, turns, wall
  time, tokens and cost (existing cost tracking, per role via SP-150),
  language-guard mismatches (SP-152).
- Configurable model list; defaults to the provider catalog's
  `recommended_model` entries so the benchmark follows recommendations
  without code changes.
- Each task runs 3 times per model, so one lucky or unlucky run does not
  decide the result.

### 154c. Reports

- Markdown + JSON report per run comparing models and starters, with a
  pass rate per starter per model computed over the 3 runs.
- Products embedding sprout may set their own readiness bar on these
  pass rates (e.g. before offering a starter); the report exposes the
  per-starter numbers they need, and this spec sets no bar.
- Failure categories (build, test, page, interaction, stopped by rule)
  to guide skill and prompt fixes.
- Results for recommended models are published in the repository with
  the run date and sprout version.
- On demand; not part of `go test ./...` (network and cost).

## Acceptance criteria

- [ ] At least five tasks for the first starter, with frozen plans.
- [ ] Runner produces a report for two models end to end.
- [ ] Pass/fail derives only from verification (test: a scripted model
      claims success but fails a check → recorded as fail).
- [ ] Report includes cost and turns per task.
- [ ] Report includes a pass rate per starter per model over 3 runs.

## Non-goals

- No leaderboard service.
- No benchmarking of model speed or raw capability outside sprout tasks.

## Open questions

- Where published results live (docs page vs a results directory).
- Run cadence (per release vs on recommendation changes).
