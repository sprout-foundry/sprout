## Multi-Agent Orchestration: Ready-State TODO

This document tracks the remaining work to bring the multi-agent orchestration flow to a reliable “ready” state.

### High-priority (implement now)

1) Core executor loop
- Replace single-pass step execution with a progress-making loop:
  - While pending steps exist, enqueue any step whose `depends_on` are all `completed` and execute them.
  - If no step is runnable and pending remain, detect deadlock and fail with a clear message listing unmet deps.

2) Honor process settings
- Parallel execution: when `settings.parallel_execution` is true, run independent steps concurrently using a bounded pool.
- Timeouts: enforce per-step `timeout` (fallback to `settings.step_timeout`).
- Retries: retry failed steps up to `step.retries` (fallback to `settings.max_retries`) with backoff.
- Stop on failure: use `settings.stop_on_failure` globally; keep agent-specific overrides only if explicitly intended in the future.

3) Validation stage
- Execute `validation.build_command`, `validation.test_command`, `validation.lint_command`, and `validation.custom_checks`.
- If `validation.required` is true, fail the process on any validation error.

4) Minimal state persistence and resume
- Persist orchestration state (plan status, agent statuses, step results) to `.ledit/orchestration_state.json` after significant changes.
- On start, if a compatible state file exists, load it and resume from the last known state.

### Medium-priority

5) Result wiring
- Populate `StepResult.Files`, `StepResult.Output`, and `StepResult.Logs`.
- Integrate with `pkg/changetracker` to report modified/created files per step.

6) Budget and cost accuracy
- Use pricing helpers to compute real costs per model with prompt/completion split; update `AgentStatus` accurately.

7) Dependency handling & validation
- Implement a robust topological sort for graph planning and early validation for orphaned `depends_on`.

8) Persistence & resume polish
- Add explicit CLI `--resume` and `--state <path>` flags; include compatibility checks (same goal, agents, steps).

### Nice-to-have

9) Tooling integration
- Allow agent turns to leverage tools via `ToolExecutor` where useful, gated by config.

10) CLI/UX improvements
- Live progress table (agent, step, status, tokens, cost) and concise failure summary.


