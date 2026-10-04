# SP-149 — Verified Done

> **Status (2026-10-03):** Proposed.
> Depends on SP-148 (acceptance criteria) and SP-153 (starter manifest
> commands). Reuses the browse path (`pkg/webcontent`) and the SP-140-4
> render→check pattern.

## Problem

"Done" is something the agent says, not something sprout checks:

- The system prompt (`pkg/agent/prompts/system_prompt.md`) tells the agent
  to build, run tests and show proof, and to stop after repeated identical
  failures. Nothing enforces it; a final reply can claim success with no
  evidence.
- The design workspace already has a closed loop for screens (render →
  critique → validate: `pkg/agent_tools/design_critique_*.go`,
  `design_validate_handler.go`). Application code has no equivalent:
  nothing starts the app and checks it.

Reviewing every claim by hand costs developers time, and unattended runs
(automation, CI, embedding environments) have no reviewer at all.

## Design

### 149a. Verification run

After a turn that changed application code, before the final reply, the
runtime (not the model) runs the acceptance checks from the active plan
(SP-148):

- `build`: the project's build command.
- `test`: the project's test command.
- `page`: start the app, open each listed route in the headless browser,
  confirm it renders without console errors.
- `interaction`: run the scripted browser steps, confirm the expected
  outcome.
- `manual`: listed in the result, never gated.

Without a plan, the build and test commands still run as a baseline.

### 149b. Where commands come from

Commands come only from the project's starter manifest
(`.sprout/starter.json`, SP-153) or explicit project configuration —
never from model output. The agent cannot change what "passing" means
mid-turn.

### 149c. Gate and repair loop

- A failing gated check is fed back to the agent as a structured
  verification report and the turn continues.
- Stopping rule: after N repair attempts on the same failing check
  (configurable, small default), stop.
- A final reply can report success only with a passing verification
  result attached. The result is recorded as a structured event
  (SP-151) with evidence: checks run, pass/fail, output excerpts,
  screenshot references for `page` and `interaction` checks.

### 149d. Honest failure

When the stopping rule fires, the final reply states plainly what passes,
what fails, and what was tried. Partial success is never reported as
success.

### 149e. Enablement

- Off by default in the CLI; enabled per project or globally in config.
- Embedding environments (hosted workspaces, automation) can enable it by
  default through the same setting.
- When enabled, `sprout agent` non-interactive runs exit non-zero if
  verification fails.

## Acceptance criteria

- [ ] Fixture project + plan: passing checks → final reply carries the
      verification result; a broken build → repair loop runs → stops at
      N with the failure report.
- [ ] A `page` check catches a route that renders with a console error.
- [ ] Commands are read only from the manifest/config (test: model output
      proposing a different command has no effect).
- [ ] Verification events emitted with evidence references.
- [ ] Disabled by default in the CLI; enabling it changes no other
      behavior.

## Non-goals

- No visual regression diffing.
- No load, security or performance testing.

## Open questions

- Default N for the stopping rule.
- Screenshot retention: `.sprout/` cache vs session history.
