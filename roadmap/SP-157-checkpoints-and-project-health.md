# SP-157 — Checkpoints and Project Health

> **Status (2026-10-03):** Proposed.
> Depends on SP-145 (Changes surface), SP-149 (verification), SP-153
> (formatter/linter/test config). Summaries use SP-150's `summarizer`
> role when configured.

## Problem

Projects get changed many times; sprout records the changes but does
little to keep the project healthy or make its history easy to use:

- **History:** `pkg/history` tracks every change and supports per-turn
  and per-file revert; `webui/src/components/chat/TurnChangesStrip.tsx`
  lists a turn's changes as file paths. There is no timeline across
  sessions, no summary of what a change did, and no "restore the project
  to how it was before this afternoon".
- **Maintainability:** code review (`pkg/codereview`), a tester persona
  and refactoring guidance in the system prompt exist, but nothing runs
  automatically: no format/lint after edits, no periodic health check.
- **Errors:** provider and model errors are classified
  (`core.ClassifyError`, used in `pkg/agent/seed_query_result.go`);
  build and runtime failures reach the user as raw tool output.

## Design

### 157a. Timeline and checkpoints

- A project timeline built on `pkg/history`: each entry is a change set
  with a short summary (template from the diff and plan scope IDs,
  optionally model-written via the `summarizer` role), plus deploys
  (SP-156).
- Checkpoints are created automatically at each passing verification
  (SP-149) and each deploy, and on demand.
- Restoring a checkpoint is one action and is itself a timeline entry;
  nothing is lost.
- Lives in the SP-145 Changes surface; file-level views remain.

### 157b. Quality after edits

Configurable (off by default in the CLI):

- Run the project's formatter and linter (starter manifest or project
  config, SP-153) after each turn that changed code; fix findings in the
  same turn.
- Require a test for new behavior (an acceptance item of kind `test` or
  a test added in the turn); verification enforces it when enabled.

### 157c. Project health check

On demand (`sprout health`) and optionally on a schedule: size and
complexity signals, duplicated code (SP-016 embedding index), outdated
dependencies, failing checks. Findings are proposed as small, separately
approvable fixes.

### 157d. Readable build and runtime errors

Extend classification to build and runtime failures with a small set of
categories (missing dependency, syntax error, type error, failing test,
app crashed on start), each with a short explanation template. The agent
attempts a fix before reporting; the explanation accompanies the raw
output rather than replacing it.

## Acceptance criteria

- [ ] Timeline renders change sets with summaries for a fixture history;
      restore creates a new entry and reverts files.
- [ ] Checkpoints created on passing verification and deploy events.
- [ ] Quality-after-edits run fixes a seeded lint violation (scripted
      test).
- [ ] `sprout health` reports fixture findings.
- [ ] Error classifier maps fixture build/runtime outputs to categories.

## Non-goals

- No automatic dependency upgrades without approval.
- No change to git branching workflows.

## Open questions

- Checkpoint retention for long-lived projects.
- Whether health checks run in the background by default.
