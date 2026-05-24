# SP-055: CLI Pinned Input — Always-On Steering Panel

**Status:** 📋 Proposed
**Date:** 2026-05-24
**Depends on:** SP-048 (CLI Delight — established the status footer pattern this spec borrows from), `pkg/agent/seed_integration.go` steer-bridge (already landed; makes the agent-side mechanism functional), seed v0.x `InjectInput` API (already integrated)
**Priority:** Medium-High — closes the largest remaining CLI ergonomics gap. Users on the CLI today have to wait for a turn to finish before they can redirect the agent; webui users got mid-turn steering via the floating input box. Until this lands the CLI feels "fire and forget" while the webui feels live.
**Effort Estimate:** ~3–5 days end-to-end, split into 3 layered phases

## Problem

The CLI is a strict REPL: `inputReader.ReadLine()` → `ProcessQuery()` → repeat.
While `ProcessQuery` is running (potentially minutes for a long multi-tool
task), there is no input reader active — keystrokes go nowhere. Users have
three options today:

1. **Wait.** Let the turn finish naturally, then prompt again. Loses the
   chance to steer early when the wrong direction is obvious.
2. **Ctrl+C.** Triggers `chatAgent.TriggerInterrupt()` (wired in
   `cmd/agent_modes.go:306-319`, propagates into the LLM HTTP request via
   `interruptCtx`). Stops the turn, but the user then types a brand-new
   prompt — they lose the in-progress work.
3. **Open the webui.** The `/api/query/steer` endpoint takes a message and
   feeds it into the same `inputInjectionChan` the CLI could be using.
   Mid-turn steering already works there.

(3) is a workaround that requires the user to leave the terminal and
context-switch to a browser. For terminal-native users this is friction.

The agent-side mechanism is already there: the steer-bridge goroutine in
`seed_integration.go` forwards `inputInjectionChan` messages to seed's
`InjectInput()` API. Seed consumes injected messages at iteration
boundaries (between tool batches, before the assistant decides to
terminate the turn). So the missing piece is purely UX: a way for CLI
users to type *while a turn is in flight*, without breaking the existing
ReadLine flow.

## Current State

### What works today

- **The agent-side steer mechanism** end-to-end: webui `/api/query/steer`
  → `clientAgent.InjectInputContext` → `sprout.inputInjectionChan`
  → forwarder goroutine → `seedAgent.InjectInput` → seed loop consumes
  at the next iteration boundary.
- **Ctrl+C interrupt** during a turn: cancels `interruptCtx`, aborts the
  in-flight HTTP request, returns control to the REPL loop. Visible
  feedback (`[||] Received signal interrupt, interrupting active task...`)
  is already printed.
- **The status footer** (`pkg/console/status_footer.go`) demonstrates
  pinned-bottom rendering using terminal scroll regions (`DECSTBM`
  escape) so streaming content scrolls above a frozen footer line. This
  is the rendering primitive the new input panel will reuse.
- **The streaming line cap** (`pkg/console/line_cap.go`) ensures the
  scrolling region above the input doesn't get blown out by minified
  content.

### What's missing

- A second input reader instance dedicated to "steer mode". Today's
  `console.InputReader` assumes it owns stdin exclusively (it calls
  `term.MakeRaw` and waits in a blocking read).
- Coordination between two input modes in one terminal session
  (idle = primary prompt, running = steer prompt).
- A visual treatment that makes the pinned input distinguishable from
  the conversation transcript above it.

## Proposed Solution

### Architecture

```
┌──────────────────────────────────────────────────────────────┐
│ [conversation scrolls here]                                  │
│                                                              │
│ [chart] read_file (Chat.css) · 0.1s                         │
│ [tool] Searching for "color-mix" in 47 files...             │
│ Sure, I'll start by looking at the existing tokens...        │
│                                                              │
│ [chart] grep_files (--accent-) · 0.4s ─────────────────── ↑ scroll region (DECSTBM)
├──────────────────────────────────────────────────────────────┤── pinned line: status footer (existing)
│ glm-5.1 · 8.4k/200k ctx · $0.0042 · ~/dev/sprout-foundry    │
├──────────────────────────────────────────────────────────────┤── new pinned line: steer input
│ steer › Make sure you check the storybook tokens too|       │
└──────────────────────────────────────────────────────────────┘
```

**Two pinned lines at the bottom**, both inside the terminal's scroll-
region exclusion (set via `\033[<top>;<bottom>r` DECSTBM). Streaming
output scrolls in the top region; the footer + steer input never move.

### Phase 1 — Steer InputReader (foundational)

New type: `console.SteerInputReader` in `pkg/console/steer_input.go`.

- **Doesn't own stdin globally** — it requests stdin only while the
  primary `InputReader` is idle (i.e., between `ReadLine` calls). The
  primary reader continues to own stdin during idle phases; the steer
  reader takes over from the moment `ProcessQuery` starts until it
  returns.
- **Renders into a single pinned line** at row `LINES - 1` (above the
  status footer at `LINES`). On terminal resize, both pinned lines
  reflow via the existing `SIGWINCH` handler in `status_footer.go`.
- **Submit on Enter** → calls `Agent.InjectInputContext(text)` →
  clears the line → shows brief `[STEER → text…]` ack in the scroll
  region for 1.5s before fading. The agent-side bridge does the rest.
- **Escape** → empty the line, return focus to the streaming output
  (no scroll-region change; just stops capturing keystrokes locally).
- **Ctrl+C** retains its existing meaning (interrupt the turn).

### Phase 2 — Unified Input Coordinator

New type: `cmd.inputCoordinator` (or similar). Owns the transition
between modes:

```
state: idle → running → idle
idle:    primary InputReader active (full readline UX — history,
         completion, etc.)
running: steer SteerInputReader active (single-line capture, sends
         to InjectInputContext on Enter)
```

The coordinator is owned by `runChatMode` and toggles state around
each `ProcessQuery` call. Existing readline state (history, prompt
prefix updates) lives in the primary reader and is untouched.

### Phase 3 — Optional polish (separable)

- **Visual mode indicator**: prefix the pinned line with a colored
  glyph that reflects mode (`›` idle vs `⇄` steering).
- **Steer history**: separate from main command history. Up/down in
  the steer reader recalls only prior steer messages.
- **Inline indicator** in the scroll region when a steer fires (so
  the user can see *what they sent* even after the input clears —
  not just `[STEER →]` flash).
- **"Done queue" mode**: hold Shift-Enter to queue a message for the
  *next user-prompted turn* instead of mid-turn steering. The default
  remains mid-turn steer per the user's preference; "done queue" is a
  fallback when the user knows the next instruction belongs to a fresh
  turn.

## Out of Scope

- **Multi-line steer input.** v1 is single-line. Power users who want
  to compose a multi-line steer can still open the webui.
- **Tab completion in the steer reader.** The primary reader's completer
  is overkill for one-line steering messages.
- **Steer message persistence.** Steers are transient by design (they
  feed into the LLM's next iteration as a user message; they're already
  recorded in conversation history that way). No separate "steer log".
- **Mid-turn steering for non-interactive runs** (`--query`, pipes,
  workflow mode). The pinned input is a TTY-only feature; non-TTY runs
  continue to use the existing webui / API steer path.

## Success Criteria

1. **A user typing while a turn is in flight** sees their keystrokes
   accumulate in the pinned input line. Pressing Enter sends the message
   to the agent and shows visible acknowledgement. Within one model-
   iteration boundary (typically <30s), the agent acts on the new
   guidance.
2. **The streaming output continues uninterrupted** during steer typing.
   No corruption, no race-condition flicker, no lost output bytes.
3. **Ctrl+C still interrupts the turn**; the pinned input does not
   capture or consume Ctrl+C.
4. **Idle prompt experience is unchanged.** When `ProcessQuery` returns,
   the steer reader yields back to the primary reader; history,
   completion, and slash commands all work as before.
5. **Terminal resize works** while a turn is in flight — both pinned
   lines reflow to the new width without dropping captured keystrokes.
6. **Non-TTY runs are unaffected** — piped stdin, CI mode, `--query`
   one-shot, etc. continue to behave exactly as today.

## Implementation Notes

- The existing `pkg/console/status_footer.go` is the model for pinned
  rendering. It uses `\033[s` / `\033[u` save/restore cursor + DECSTBM
  scroll-region escapes. The new steer panel will share these primitives
  — probably exporting a shared `pinnedRegion` helper to manage the
  growing collection (footer, steer, maybe more in the future).
- The steer InputReader does NOT need `term.MakeRaw` if we use a simple
  byte-at-a-time read with `os.Stdin.Read(buf[:1])` and decide based on
  the byte. Avoids fighting the primary reader's MakeRaw/Restore cycle.
- Stale stdin bytes between modes: when `ProcessQuery` returns, the
  primary reader's `ReadLine` re-enters raw mode. Any bytes still in
  the steer reader's buffer (e.g., a half-typed steer the user didn't
  submit) should be discarded — they're meaningless as a fresh primary
  prompt.
- For the brief `[STEER → …]` ack in the scroll region, reuse the
  per-tool render path so the visual treatment matches existing
  "captured event" lines.

## Dependencies on Already-Landed Work

- **Steer bridge** (this session): `pkg/agent/seed_integration.go`
  forwards `sprout.inputInjectionChan` → `seedAgent.InjectInput`. Without
  this, the pinned input UI would queue messages into a dead channel.
- **Line cap** (this session): `pkg/console/line_cap.go` keeps the
  scrolling region above the pinned input from being overrun by
  minified content. Already wired into `SetupAgentEvents`.
- **Ctrl+C interrupt**: already wired end-to-end (`cmd/agent_modes.go:306-319`
  → `chatAgent.TriggerInterrupt()` → `interruptCtx` cancel → HTTP request
  abort). The pinned input must not break this.
