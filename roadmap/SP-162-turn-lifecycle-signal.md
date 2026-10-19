# SP-162 — Turn Lifecycle Signal

Status: Proposed (2026-10-19). Split from a 17-item user nit list. Depends on
nothing; touches the REPL turn loop (`cmd/agent_mode_repl.go`), the turn
summary (`pkg/cliui/turn_stats.go`) and the pinned composer
(`pkg/console/composer.go`).

## Problem

The user cannot tell, at a glance, whether a turn is still running or has
finished — and when it finishes they often cannot tell whether it succeeded.
Two complaints, one root cause: the turn has no reliable closing marker.

### The working/done indicator is not predictable

While a turn runs, the pinned composer shows `steer › ` or `queue › `
(`ComposerPrefix`, `pkg/console/composer.go`) and the spinner shows
`Thinking · <model>`. At turn end the composer label disappears
(`ClearSteerLine`, `pkg/console/status_footer_steer_region.go`). That is the
only transition signal, and the user reports it is easy to miss — "once or
twice I've seen the steering go away."

### The turn summary is silently suppressed

`PrintPerTurnSummary` (`pkg/cliui/turn_stats.go`) returns early when
`promptDelta <= 0 && completionDelta <= 0`. So the `⎯ this turn: … ⎯` line —
the thing that reads as "this turn is over" — is absent for:

- fast-path turns (zsh command execution, slash commands),
- a turn that consumed no tokens,
- a turn that failed before any token was produced.

The user is left with no closing separator and no way to tell the turn ended.

### Failures can end with no conclusion at all

`ProcessQuery` errors are printed only when `!isReported(err)`
(`cmd/agent_mode_repl.go`). A turn that gives up inside the agent loop (model
network difficulty after retries, budget exhaustion, repeated identical
failures) can return `nil` with a final reply that says nothing about the
outcome. The user's ask is explicit:

> sometimes no conclusion written out, so not clear if things actually
> succeeded or not. If something failed, we should be clear on that: "had
> network difficulty with the model, giving up". And if things were
> successful, that should also be communicated: "Implemented x, y, and z;
> all tests pass, …". I still end up prompting with a "where are we at?"
> fairly often.

The user notes this may be partially improved in a recent build — so the
first step is to establish what the current behavior actually is, not to
assume the bug is still fully present.

## Design

### 162a. Establish current behavior first

Before changing anything, drive the REPL in a pty against `--mock-llm` and
record what actually prints at turn end for each path:

- a normal LLM turn,
- a fast-path (zsh) turn,
- a slash command,
- a turn that errors before any token,
- a turn that errors after tokens,
- a Ctrl-C-interrupted turn.

The spec's later sections are written against the *gaps this finds*, not
against the report alone. If the summary already prints in some of these
cases, say so and scope accordingly.

### 162b. An unconditional turn-end marker

Every turn ends with a visible closing marker, regardless of token count,
fast path, or error. The `⎯ this turn: … ⎯` line stays as-is when tokens were
consumed; when they were not, a shorter closing marker still prints. The
marker is the contract: *if you see it, the turn is over.*

This is deliberately unconditional. A marker that appears only sometimes is
the bug being fixed.

### 162c. An explicit outcome line

Where 162b says "the turn ended," 162c says "and here is how it went." Three
outcomes, each stated plainly:

- **Succeeded**: what was done, in one line — the shape the user asked for
  ("Implemented x, y, and z; all tests pass").
- **Failed**: why, in the user's words — "had network difficulty with the
  model, giving up" — not a raw exception.
- **Partial / stopped**: what completed and what did not.

The runtime already classifies errors (`pkg/errclass`, `pkg/errors`); 162c
maps a classified error to a plain sentence. It does not invent detail.

Relationship to SP-149 (Verified Done) and SP-151 (Progress Events): those
already produce a structured completion event and a verified-outcome summary
for *plan-driven* runs. 162c is the REPL's own closing line for every turn,
plan or not. Where a verification result exists, 162c uses it rather than
restating from the model's text.

### 162d. The working indicator is unambiguous

While a turn runs, the footer states that work is in progress in a way that
does not depend on the user noticing the composer label appear and disappear.
The existing spinner is the mechanism; the gap is that it is not tied to the
turn's *end* in a way the user can rely on. 162b's marker is the primary fix;
162d covers the running half — a stable "working" indication for the whole
turn, including during tool execution and subagent runs where the spinner
text currently changes or stops.

## Acceptance criteria

- [ ] 162a: a written record of what prints at turn end for each of the six
      paths, produced from a real pty run.
- [ ] 162b: every path in 162a ends with a visible closing marker; a test
      drives each path and asserts the marker (no path is exempt).
- [ ] 162c: a turn that fails with a classified error prints a plain-language
      outcome sentence; a turn that succeeds prints what it did; a turn that
      stops partway says so. Tests over injected errors.
- [ ] 162d: a running turn shows a stable working indication for its whole
      duration (test over a turn with tool calls).
- [ ] No regression in the SP-149 / SP-151 completion paths: a plan-driven
      run's verified outcome is not restated or contradicted.

## Non-goals

- No change to the model's own final reply text; 162c is runtime-emitted
  chrome, not a prompt change.
- No new event types — SP-151's schema is reused where it applies.
- No work on the *content* of session summaries (that is SP-163).

## Open questions

- Whether the closing marker and the outcome line are one line or two. Two is
  clearer; one is quieter. Leaning two, with the outcome line dim.
- Whether 162c's success line is derived from the turn's tool/file changes
  (deterministic) or from the model's own summary (richer, less reliable).
  Deterministic is the safer default; the model's text is already on screen.
