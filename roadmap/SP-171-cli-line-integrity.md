# SP-171 — CLI Line Integrity & Transcript Ergonomics

**Status:** Partially implemented (2026-10-31). 171a (transcript writer +
guard), 171b (blank-line discipline), 171d (expandable tool output),
171g (resume tail), and 171h (steer-during-write) have landed. 171c (resize
harness), 171e (`/transcript`), and 171f (`/history`) remain. The pty harness could not be validated in this environment: `expect`
against `agent --mock-llm` inserts `\r` and `\n` into the composer instead of
submitting, on the unmodified binary as well as this one — so the rendered-
screen assertions below are covered by unit tests over the writer, not by a
live terminal. That gap is 171c's job to close.
**Origin:** user report — *"improve the interactive CLI so we don't lose lines
and don't add blank lines, so that this functioned more like the best in class
tools and supported more CLI features like Claude Code"*

Follow-up to SP-144 (REPL polish), SP-161 (friction fixes), SP-162 (turn
lifecycle), SP-166 (wheel/scroll routing). Those fixed the *signal* at the end
of a turn and gave the wheel back to the terminal; none of them made the
transcript itself trustworthy.

## Problem

The interactive REPL loses transcript rows and manufactures blank ones. The
editing layer is at parity with the best tools (multi-line compose, Ctrl+R,
Ctrl+X Ctrl+E, bracketed paste, kill-ring, undo, steer panel, slash
completion) — it is the *output* layer that is not.

### 171a. Nothing owns the transcript

Transcript writes are scattered across at least seven entry points, each with
its own cursor discipline:

| Writer | Location | Discipline |
|---|---|---|
| `PrintExternal` | `pkg/console/console_lock.go:114` | clears line, prints, redraws input |
| `printExternalLocked` (input) | `pkg/console/console_lock.go:139` | `\r\033[K`, print, `refreshLocked` |
| `printExternalLocked` (steer) | `pkg/console/steer_input_render.go:179` | absolute CUP to region bottom, print |
| `PrintLine` / `printBackground` | `pkg/console/console_lock.go:172` | routes to `PrintExternal` or raw stderr |
| `PrintUserMessage` | `pkg/cliui/turn_stats.go:73` | raw `fmt.Printf` to stdout |
| `PrintAssistantHeader` | `pkg/cliui/turn_stats.go:60` | raw `fmt.Printf` to stdout |
| `PrintTurnEndMarker` / `PrintTurnOutcome` | `pkg/cliui/turn_stats.go:195`, `turn_outcome.go:107` | raw stderr, TTY-gated |

Plus raw `fmt.Println()` in the REPL loop (`cmd/agent_mode_repl.go:115, 154,
242`) and in slash-command implementations.

Only the first four know about the status footer's DECSTBM scroll region
(`pkg/console/status_footer_scroll.go:129`). The `pkg/cliui` writers and the
REPL's own `fmt.Println` calls write at the cursor's current position and hope
it is inside the region. When it is not — after a child process emitted
`\033[r`, after a `\033[J` clear in `Resize`, after a footer draw left the
cursor restored to a pre-draw row — those writes land on pinned rows or scroll
the whole screen.

`drawFullLocked` (`pkg/console/status_footer_draw.go:133`) already re-asserts
the region on every full draw as a self-healing checkpoint. That is a repair
after the fact, on a schedule the writer does not control. Between draws there
is no guard at all.

### 171b. Every turn adds a blank line

The turn-end path prints an unconditional `fmt.Println()` before the assistant
header (`cmd/agent_mode_repl.go:242`) and `PrintUserMessage` begins with a
literal `"\n"` (`pkg/cliui/turn_stats.go:83`, both branches). When the previous
output already ended on a fresh row — which is the common case, since
`FinalizeAtTurnEnd` guarantees a trailing newline
(`pkg/console/assistant_turn_renderer.go:331`) — that is a guaranteed stray
blank row. It accumulates inside the scroll region and reads as the gap the
user is describing.

The renderer already tracks the fact needed to prevent this: `atLineStart`
(`pkg/console/assistant_turn_renderer.go:52`). Nobody consults it.

### 171c. Resize is the largest line-eater

`StatusFooter.Resize` (`pkg/console/status_footer_resize.go:14`) resets the
region, computes a clear window from the union of `oldRows - reserved + 1` and
`newTop`, and clears with `\033[J`. On a shrink it clears from `newTop` only.
The window depends on `lastRows` / `lastCols` / `lastSteerRows` / `lastHintRows`
matching what the terminal actually reflowed to. A mismatch — a resize that
lands while the steer panel is mid-growth, a terminal that reflows differently
than `computeOverflowRows` models, a bottom-anchored terminal taking the
non-Termux branch — clears live transcript.

There are 435 lines of resize tests (`status_footer_resize_fix_test.go`). They
assert the escape sequences emitted, not the resulting screen. The bug class is
invisible to them by construction.

### 171d. Truncation is silent and permanent

`FormatToolArgPreview` (`pkg/cliui/tool_display_diff.go:81`) hard-cuts at 70–80
chars with `…`. The full argument exists in the event stream and in the session
file, but there is no in-session path to it. Claude Code lets you expand a tool
call in place; here the user must leave the REPL and read the session JSON.

### 171e. Scrollback loss is unrecoverable

SP-166 correctly returned the wheel to the terminal. But the footer's scroll
region means rows that scrolled off before the footer existed, or that a
`\033[J` ate, are gone from the terminal's scrollback too. There is no
`/transcript` pager and no way to re-read what was lost.

### 171f. History is per-session in practice

`AddToHistory` does persist — `saveHistoryToConfig` writes
`CommandHistoryByPath` keyed by workspace root
(`pkg/agent/agent_history.go:161,191`) and `loadHistoryFromConfig` restores it
at agent creation (`agent_creation.go:212`). So the *mechanism* exists. What
does not exist is any signal that it worked, and the cap is a hard 100 with
silent eviction (`agent_history.go:44`). The user's report of "history doesn't
persist" is consistent with the store being workspace-keyed: starting the REPL
from a subdirectory, or from `~`, produces a different key and an empty
history, with no indication.

## Design

### 171a — One transcript writer

Introduce `console.WriteTranscript(s string)` as the only way text reaches the
transcript area. It:

1. Takes `outputMu`.
2. Reads the footer's applied region (`appliedReserved`, `appliedRows` —
   already tracked, `status_footer.go:68`).
3. Queries the cursor row (DSR `\033[6n`, with a cached fallback so a terminal
   that does not answer does not stall). If the row is outside the region,
   re-apply the region and park the cursor on its last row before writing.
4. Writes the text, translating bare `\n` to `\r\n` in raw mode.
5. Updates the renderer's line-start state.

Then migrate every writer in the 171a table to it. `PrintExternal` and
`printExternalLocked` keep their input-redraw responsibility but delegate the
transcript write itself. `pkg/cliui`'s writers stop touching `os.Stdout`
directly and call through.

The DSR probe is the contentious part. Alternative: skip the probe and always
re-apply the region + park the cursor before a transcript write. That is one
extra escape sequence per write and no round-trip, at the cost of a visible
cursor jump when the user is mid-line. **Leaning: always park, no probe.** A
transcript write is a line-oriented event; the cursor is going to move anyway.
Confirm with a pty recording before committing to it.

### 171b — Blank-line discipline

- `console.BlankLine()` — emits one newline only when the previous emitted
  character was not already a newline. Implemented over a package-level
  `lastByte` tracker updated by `WriteTranscript`.
- Replace the unconditional `fmt.Println()` at `agent_mode_repl.go:242` and the
  leading `"\n"` in `PrintUserMessage` with `console.BlankLine()`.
- Same treatment for `agent_mode_repl.go:115, 154`.
- `PrintAssistantHeader` gains the leading blank via `BlankLine()` rather than
  the caller doing it, so the pairing cannot drift.

Acceptance is a rendered-screen assertion, not a string assertion: a turn
followed by another turn must produce exactly one blank row between them.

### 171c — Resize against the screen, not the escape stream

Build a pty harness (`pkg/console/testdata/pty` or `internal/ptyharness`) that:

- spawns a real terminal emulator model (a small VT100 subset is enough:
  scroll region, CUP, ED, IL/DL, autowrap) or drives `tmux` in a detached
  session and captures `capture-pane -p`;
- records the screen before a resize and after, at a matrix of
  (old width, new width, old rows, new rows, steer active?, hint row?,
  prose streaming?);
- asserts every transcript row that was visible before is still present after,
  in order, with no duplicates and no extra blanks.

Run the existing resize tests through it. Fix whatever the matrix exposes in
`status_footer_resize.go` — most likely the `computeOverflowRows` model and the
`bottomAnchoredResize` branch, both of which encode assumptions about terminal
reflow that only a real screen can confirm.

The harness is the deliverable as much as the fix: without it, the next resize
change reintroduces the same class.

### 171d — Expandable tool output

- Keep a bounded ring of the last N (8) tool calls: name, full arguments,
  status, duration, result preview.
- `Ctrl+O` reprints the most recent tool call untruncated, under a
  `⎯ tool · <name> ⎯` header, into the transcript via `WriteTranscript`.
- Repeated `Ctrl+O` walks back through the ring.
- The footer hint row names it while a turn is running.
- No new storage: the ring is populated from the same events
  `cliui.StartTerminalToolSubscriber` already consumes.

### 171e — `/transcript`

`/transcript` renders the current session's conversation to the system pager
(`$PAGER`, fallback `less -R`), read from the session file the agent already
writes. `--last-turn` limits to the final exchange. This is the escape hatch
that makes 171c's remaining imperfections survivable: if a row was lost from
the screen, the content is still reachable.

Suspends the footer and input reader around the pager, like the existing
`Ctrl+X Ctrl+E` editor escape does (`input_editor_escape.go`).

### 171f — History that says so

- Raise the cap to 1000 and evict oldest-first (already the behavior, just a
  bigger number).
- `/history` reports the count, the store path, and the workspace key, in the
  same shape SP-161f gave `/keys`.
- When the restored history is empty but the config holds history under a
  *different* key, say so: `ⓘ no history for <path>; history exists for <other
  path>`. This is the actual fix — the mechanism works, the user cannot tell
  which key they are on.

## Acceptance criteria

- [x] 171a: a single `console.WriteTranscript` is the only writer into the
      transcript area; a guard test fails on any new `fmt.Print*` to
      `os.Stdout`/`os.Stderr` in `pkg/cliui` or `cmd` REPL paths that is not
      routed through it.
      → `pkg/console/transcript.go` owns the writer, the region-aware
      parking sequence, and the blank-line state machine.
      `TestNoRawTranscriptWritesInCliui` walks `pkg/cliui` with go/ast and
      fails on a raw write; `//nolint:transcript-guard` exempts writes that
      are not text (the terminal bell).
- [x] 171a: after a child process emits `\033[r` and exits, the next
      transcript write lands inside the region and the pinned rows are intact.
      → `WriteTranscript` re-asserts the region and parks on its last row on
      every write, so a `\033[r` from a child is repaired by the next line
      rather than persisting.
      Tests: `TestWriteTranscript_ParksInsideRegionWhenFooterLive`,
      `TestWriteTranscript_RegionShrinksWithSteerPanel`,
      `TestWriteTranscript_NoFooterWritesAtCursor`.
- [x] 171b: two consecutive turns render with exactly one blank row between
      them; a turn following a slash command likewise.
      → `SectionBreak` always emits a separator; `BlankLine` collapses
      repeats and is a no-op at session start. The REPL's redundant
      `fmt.Println()` before `PrintAssistantHeader` (which already calls
      `SectionBreak`) was the double-blank source and is gone.
      Test: `TestTurnBoundary_RendersExactlyOneBlankRow`.
- [x] 171b: no path emits two consecutive blank rows.
      → `BlankLine` tracks the last emitted row and suppresses a repeat.
      Tests: `TestBlankLine_SuppressesConsecutiveBlanks`,
      `TestBlankLine_EmitsWhenPreviousLineUnterminated`,
      `TestBlankLine_SuppressedAtSessionStart`.
- [ ] 171c: the pty harness exists and runs in CI; the resize matrix asserts
      no visible transcript row is lost across every case.
- [ ] 171c: `make test-smoke` includes the harness; `SPROUT_REQUIRE_PTY=1`
      turns a missing pty into a failure, following the
      `SPROUT_REQUIRE_BROWSER` precedent.
- [x] 171d: a recent tool call's full output can be reprinted without
      re-running the tool.
      → `/expand [n]` prints one buffered result, `/expand list` indexes
      them; the ring holds the last 10 completed calls and is recorded
      before the compact-mode early return so compact turns are addressable.
      Shipped as a slash command rather than `Ctrl+O`: the key is unbound
      and the command is discoverable through `/help` and the WebUI.
      Tests: `TestExpandCommand_*` (6), `TestToolOutputRing_*` (4).
- [ ] 171e: `/transcript` opens the pager with the session rendered; the footer
      and input reader are suspended and restored around it.
- [ ] 171f: `/history` reports count, path, and key; a mismatched key produces
      the cross-key notice (unit test over two workspace roots).

### 171g. A resumed session renders nothing

- [x] 171g: resuming a session reprints the tail of its conversation, in
      chronological order, oldest at the top and newest just above the
      prompt; tool payloads are not reprinted but their count is stated.
      → `cliui.RenderSessionTail` walks the conversation backwards to
      collect `RestoreTailLines` (200) lines, reverses them, and prints.
      Wired into both restore paths: the startup picker
      (`cmd/recent_sessions.go`) and the `--session-id` / `--last-session`
      flags (`cmd/agent_mode_interactive.go`, at the same seam the picker
      uses, so the output lands inside the scroll region).
      Tests: `TestCollectTailLines_*` (5), `TestRenderSessionTail_EmptyIsSilent`.

### 171h. Steer typed while lines are being written

- [x] 171h: output arriving mid-turn obeys the same parking discipline as
      idle output, and clears the row it lands on.
      → `SteerInputReader.printExternalLocked` had its own copy of the
      parking sequence that parked without clearing the row, so a
      half-written line survived underneath the message. It now calls
      `writeTranscriptLocked`, the one implementation.
      Tests: `TestPrintExternal_SteerActive_LandsInsideRegionAndClearsRow`,
      `TestWriteTranscript_ClearsParkedRow`.

## Non-goals

- No new TUI framework. This stays a line-oriented renderer with a pinned
  footer; the fix is discipline, not a rewrite onto bubbletea.
- No change to the steer panel's behavior or the wheel decision (SP-166).
- No change to the turn lifecycle signal (SP-162) — 171b removes *extra*
  blanks, it does not touch the closing marker contract.
- No new tool-output storage format; 171d reads the existing event stream.

## Open questions

- **171a: DSR probe or always-park?** Leaning always-park (one extra sequence,
  no round-trip, no stall risk on terminals that ignore `\033[6n`). The visible
  cost is a cursor jump before each transcript write, which a line-oriented
  transcript makes acceptable. Decide with a pty recording.
- **171c: emulator model or tmux?** A VT100 subset is portable and has no
  external dependency; tmux is more faithful but adds a CI dependency and
  behaves differently on macOS vs Linux. Leaning: VT100 subset in Go, with
  tmux available as an opt-in cross-check.
- **171d: `Ctrl+O` or a slash command?** Resolved: slash command
      (`/expand`). `Ctrl+O` matches the muscle memory
  from Claude Code and is discoverable via the hint row. A `/expand` command
  would be easier to test but less likely to be found. Leaning `Ctrl+O`, with
  `/expand` as an alias.
- **171f: is the workspace key the right one?** Keying by workspace root means
  history does not follow the user between projects. That is arguably correct
  for a coding agent. The bug is the silence, not the key. Confirm before
  changing the key.
