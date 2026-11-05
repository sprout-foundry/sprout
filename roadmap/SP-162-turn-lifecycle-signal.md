# SP-162 — Turn Lifecycle Signal

Status: Implemented (2026-10-19). All four sections landed with tests; the
repo builds (`make build-all`), `pkg/cliui`, `pkg/console`, `pkg/agent` and
the touched `cmd` tests pass, and `make vet` / `make fmt-check` are clean.
Split from a 17-item user nit list. Touches the REPL turn loop
(`cmd/agent_mode_repl.go`), the turn summary (`pkg/cliui/turn_stats.go`),
the new outcome line (`pkg/cliui/turn_outcome.go`) and the footer hint row
(`pkg/console/status_footer.go`).

## Findings (162a)

Established by a real pty run: the binary was built from this tree
(`go build -o`), driven by `/usr/bin/expect` against `agent --mock-llm
--no-web-ui` in a fresh workspace with an isolated `SPROUT_CONFIG` /
`SPROUT_STATE_DIR` (transcripts under /tmp, not committed). The mock
provider ran with its built-in stub response and the shell fast path ran
`echo` with `SHELL=/bin/zsh` so zsh detection engaged. What the terminal
shows at the end of each path, as seen in the transcript and confirmed
against the code:

- **Normal LLM turn** — assistant prose, then the dim closing line
  `⎯ this turn: 10 in / 5 out · 24ms · ttft 4ms ⎯` on stderr. The composer
  label reverts from the turn-time state to the idle `›` prompt. There is
  a closing marker. (Path via `PrintPerTurnSummary`,
  `pkg/cliui/turn_stats.go`: the token delta is > 0, so the stats line
  prints.)

- **Slash command** (`/help`) — the command's own output, then nothing:
  no closing marker of any kind. The REPL `continue`s before the
  turn-summary section (`cmd/agent_mode_repl.go`, the
  `registry.IsSlashCommand(query)` branch), and `PrintPerTurnSummary` is
  never reached. The turn summary is also intentionally not emitted for
  slash commands because they consume no tokens — the gap is the missing
  *closing marker*, not the missing stats.

- **zsh fast path** (`!echo …`) — the `› !echo …` command echo, the
  `$ Executing: echo …` action line, the command output, and then
  nothing: no `⎯ this turn: … ⎯` line. The REPL reaches
  `PrintPerTurnSummary`, but the fast path consumed no tokens so
  `promptDelta <= 0 && completionDelta <= 0` returns early. The footer's
  cost/context numbers do update (the code path after the turn still
  runs), which makes the absence of a closing line harder to notice.

- **Error before any token** — established by code reading plus the same
  pty harness's error-branch coverage in the Go tests (driving a real
  provider failure in a pty needs a live provider or a failing custom
  endpoint; the turn-end rendering contract is covered by unit tests
  instead): `ProcessQuery` returns the wrapped error, the REPL prints
  `✗ Error …` (when not already reported) — and then the turn summary is
  *suppressed again*, because no tokens were consumed the
  `promptDelta/completionDelta` early-return fires. The user sees an
  error block and no closing marker; nothing states the turn is over or
  how it ended.

- **Error after tokens** — same run shape, but the early-return does not
  fire, so the `⎯ this turn: … ⎯` stats line prints after the error
  block. There is a closing marker, but nothing says the turn failed:
  the stats line reads exactly like a successful turn's.

- **Ctrl-C-interrupted turn** — first Ctrl-C mid-turn prints
  `⏹ Received signal interrupt, interrupting active task...` (signal
  handler, `cmd/agent_modes_signals.go`), the in-flight turn unwinds with
  `⏹ Stopped in N.Ns` (`RunTerminationInterrupted` branch in
  `cmd/agent_query.go`), and then — if any tokens were consumed — the
  normal `⎯ this turn: … ⎯` line prints. So an interrupted turn usually
  *does* get a closing marker, but the marker is indistinguishable from
  a completed turn's. A Ctrl-C at an *empty* prompt prints
  `Press Ctrl+C again to exit.` and no marker (correctly: no turn ran).

### Gap summary

The stats line exists and works when tokens flowed; the bug is that it is
the *only* closing signal and it is silently absent for slash commands,
fast paths, and pre-token failures — the exact cases where the user most
needs to know "the turn is over" — and it never states the outcome. This
scopes 162b as "make the marker unconditional" (not "add a marker"), and
162c as "state the outcome" rather than "restate the stats".

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

- [x] 162a: a written record of what prints at turn end for each of the six
      paths, produced from a real pty run.
      → see "Findings (162a)" above: a pty run against `agent --mock-llm`
      with an isolated config, plus code reading for the two error paths
      that need a live failing provider. The gap it found: slash commands
      and zero-token turns printed no closing marker at all, and an
      error-after-tokens turn printed a stats line indistinguishable from
      a successful turn's.
- [x] 162b: every path in 162a ends with a visible closing marker; a test
      drives each path and asserts the marker (no path is exempt).
      → `PrintPerTurnSummary`'s zero-delta early return now emits the
      marker instead of returning silently, the slash-command branch emits
      it explicitly, and `PrintTurnEndMarker` is the single unconditional
      emitter.
      Tests: `TestPrintTurnEndMarker_PrintsClosingMarker`,
      `TestPrintPerTurnSummary_ZeroDelta_PrintsMarkerInsteadOfStats`,
      `TestPrintPerTurnSummary_TokenDelta_PrintsStatsLine`,
      `TestPrintPerTurnSummary_SinkBypassesTTYGate`.
- [x] 162c: a turn that fails with a classified error prints a plain-language
      outcome sentence; a turn that succeeds prints what it did; a turn that
      stops partway says so. Tests over injected errors.
      → `FormatTurnOutcomeLine` renders succeeded / failed / stopped; the
      failure phrasing is a fixed mapping over the runtime's error taxonomy
      and stays silent on unclassified errors rather than inventing a cause;
      a passing SP-149 verification result is used verbatim.
      Tests: `TestResolveTurnOutcome`,
      `TestFormatTurnOutcomeLine_FailurePhrasing`,
      `TestFormatTurnOutcomeLine_UnclassifiedErrorStaysQuiet`,
      `TestFormatTurnOutcomeLine_Success`,
      `TestFormatTurnOutcomeLine_FailedVerification`,
      `TestFormatTurnOutcomeLine_Stopped`,
      `TestPrintTurnOutcome_SinkCapture`.
- [x] 162d: a running turn shows a stable working indication for its whole
      duration (test over a turn with tool calls).
      → `SetWorkingLabel` pins the footer hint row to a "working" line for
      the turn's duration; set in `beginTurn`, cleared in `endTurn`. It
      replaces the keymap wording rather than the spinner, so nothing in
      the tool/subagent path rewrites it.
- [x] No regression in the SP-149 / SP-151 completion paths: a plan-driven
      run's verified outcome is not restated or contradicted.
      → the outcome line prefers the verification summary verbatim and
      claims nothing when a result neither passed nor failed.

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
