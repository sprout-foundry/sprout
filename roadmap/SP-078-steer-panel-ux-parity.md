# SP-078: Steer-Panel UX Parity — Wrap-Aware Rendering, Tab Completion

**Status:** 📋 Proposed
**Date:** 2026-06-26
**Depends on:** SP-055 (CLI pinned input — shipped), SP-048 (CLI Delight — established the StatusFooter pattern)
**Priority:** Medium — user-visible CLI regression vs the regular REPL prompt and a recurring source of "the steer panel feels half-baked" reports.
**Effort Estimate:** ~3–5 days end-to-end, split into 4 phases (see below)

## Problem

SP-055 shipped the pinned steer-input panel as a focused 1,428-line file
(`pkg/console/steer_input.go`) and intentionally diverged from the
`InputReader` architecture (`pkg/console/input_core.go` + ~6,300 lines
across 18 sibling files) to keep turn-time input lightweight. That
decision kept the steer reader fast and predictable, but over the last
two months the regular prompt gained a number of UX features that the
steer panel never absorbed:

| Capability | In `InputReader` | In `SteerInputReader` | User-visible consequence |
|---|---|---|---|
| **Width-aware wrap rendering** with cursor row/col math | ✅ `input_render.go` (`wrappedGeometry`, `cursorLineIndex`, `cursorColumnOffset`, `writeWithHardBreaks`, `Refresh`) | ❌ Single-line call to `footer.SetSteerLineWithCursor` (`steer_input.go:1138`); `splitSteerLines` does a naive `\n` split (`status_footer.go:616`) with no terminal-width awareness | A long single-line steer overflows horizontally and wraps off the screen. A multi-line steer (Alt/Shift+Enter) renders fine *up to `maxSteerRows=6`* (`status_footer.go:310`) then clips with a leading `…` — fine for hard breaks, but the cursor (a byte offset into the full string) lands at the wrong visual column on wrapped rows. |
| **Tab completion** | ✅ `input_completion.go` (`SetCompleter`, `CompletionProvider`, `handleTabCompletion`) | ❌ Tab is reserved for the **STEER ↔ QUEUE mode toggle** (`steer_input.go:74`) | No slash/file/path completion while steering mid-turn — users have to wait for the turn to finish to get REPL completion back. |
| **Mouse support** | ✅ `input_mouse.go` (`handleMouseEvent`, right-click context menu) | ❌ Explicitly swallowed (`steer_input.go:643`: *"Mouse events are not supported in steer mode — swallow."*) | Acceptable for now: the steer panel is a single pinned row, so right-clicking on conversation content above it doesn't have an obvious target. See *Out of Scope*. |
| **Context menu** | ✅ `input_context_menu.go` | ❌ None | Same rationale as mouse — out of scope. |
| **Undo / redo / syntax highlight** | ❌ | ❌ | Not in `InputReader` either; not a regression. |

The result: **the steer panel feels noticeably rougher than the REPL prompt**
even though it was purpose-built and has gotten five fixes since launch
(`a8013938` reader suspend during webui approvals, `ac75f0ed` readline
bindings, `eb441143` Ctrl-R/Ctrl-X-Ctrl-E/SIGWINCH, `8f501bd3` PrintExternal
mid-turn routing, `e830d113` security-caution destruction). Each fix
narrowed a specific break, but none closed the **fundamental parity gap**
that drove the original "half-baked" perception.

This was confirmed during a 2026-06-26 audit: the user reported "I thought
we did it, but it is still not good," and `git log --all --grep="steer"`
shows **no reverted or deleted steer work** — the gap is unfinished, not
lost.

### Concrete reproductions

1. **Horizontal overflow.** On an 80-col terminal, type a steer message
   longer than ~70 chars. The line spills past the right margin and any
   right-side footer chrome (e.g., the activity indicator's elapsed-time
   counter) overlaps it. `splitSteerLines` returns 1 line and the footer
   writes it verbatim.
2. **Cursor lands off-column on hard-wrapped multi-line steers.** Type a
   3-line steer (Alt+Enter ×2) where line 2 is long enough to soft-wrap.
   Move the cursor with arrow keys: `steerCursor` is a byte offset into
   `f.steerLine` (`status_footer.go:540`), so the cursor-line picker walks
   `\n`-boundaries correctly but cannot map a byte offset within line 2 to
   a visual column after a soft-wrap.
3. **No completion mid-turn.** Run a slow query, type `/mo` in the steer
   panel, press Tab: the panel toggles to QUEUE mode instead of cycling
   slash-command candidates. To get completion, the user has to wait for
   the turn to end and edit at the REPL prompt — exactly the moment the
   steer panel was supposed to *improve*.

## Proposed Solution

Four phases. Each phase is independently shippable; earlier phases fix the
most-visible regressions, later phases close the rest.

### Phase 1: Wrap-aware render path for the steer panel

Replace `steer_input.go`'s `renderLine()` single-row pin with a wrap-aware
path that mirrors `input_render.go`'s `wrappedGeometry`. The footer
already accepts multi-line strings and per-line cursor placement
(`SetSteerLineWithCursor`, `status_footer.go:402`); the gap is that the
*input side* never builds a wrapped, cursor-aware multi-line string.

Concretely:

- Extract `wrappedGeometry`, `cursorLineIndex`, `cursorColumnOffset`,
  `writeWithHardBreaks`, and `visibleRuneWidth` from `input_render.go`
  into `pkg/console/wrap.go` (no behavior change).
- In `steer_input.go`, before calling `SetSteerLineWithCursor`, run the
  buffer through `writeWithHardBreaks(buffer, cols-promptWidth)` to
  produce hard-wrapped lines, then build a `wrappedGeometry`-derived
  `(lineIndex, colOnLine)` for `r.cursorPos` and forward both to the
  footer.
- Extend `StatusFooter.SetSteerLineWithCursor` (or add a sibling
  `SetSteerLineWrapped`) to accept `(text string, lines []string, lineIdx, colOnLine int)`
  so the footer can render a caret on the correct visual row at the
  correct column even after soft wraps. The cursor walks visually (not
  by raw byte offset) for steer panels that have wrapped.
- Add `steer_input_wrap_test.go` mirroring `input_search_test.go`'s
  width-variation cases: narrow terminal forces more wraps, cursor
  positions land correctly, blank-line rules don't get crossed.

**Risk:** `maxSteerRows=6` is a hard cap. With wrapping, a 60-line paste
would consume 60 visible rows. Clamp the *visible* cap to e.g. 8 rows,
but ensure the caret is always on a visible row by scrolling the wrapped
output (similar to `splitSteerLines`'s existing `…` overflow, but
applied to wrapped lines not hard breaks).

### Phase 2: Tab completion on a different binding (preserve the mode toggle)

`Tab` is already taken by the STEER ↔ QUEUE mode toggle and is documented
in `agent_mode_utils.go:101`. Reassigning it would be a breaking change
to users who rely on it. Two viable options:

- **Option A (recommended):** Add a new `completionCycle` path triggered
  by `Ctrl+]` (or `Shift+Tab`). Move `CompletionProvider`, the cycle
  state, and `handleTabCompletion` into `pkg/console/completion.go` so
  both readers share one implementation; expose
  `(*SteerInputReader).SetCompleter(c CompletionProvider)` mirroring
  `(*InputReader).SetCompleter`. Wire the slash-command registry
  (`cmd/agent_mode_utils.go`'s `/commands`) as the steer completer.
- **Option B:** Add a `TabVariant` config (`tab_mode_toggle`, `tab_completion`,
  `tab_cycle`). Default stays `tab_mode_toggle` for backward compat;
  users opt into completion.

Pick **A**. The single most-asked-for feature in the recent CLI feedback
("the steer panel is missing command completion") is solved without
breaking muscle memory.

### Phase 3: Soft-wrap cursor position fix for the *existing* multi-line path

Independent of Phase 1's full wrap-aware rewrite: harden the
`steerCursor → (lineIndex, byteColWithinLine)` mapping in
`status_footer.go:540` so it correctly handles the case where a `\n`-
separated line is wider than the terminal (and the user has wrapped
within it via the terminal's auto-wrap). Today the byteCol is computed
naively; refactor to also call `visibleRuneWidth(lineText[:byteCol])` so
the caret lands at the right visual column even on over-wide lines.

This is a one-day fix that lands immediate value while Phase 1 is in
flight.

### Phase 4: Remove `TODO(SP-078)` markers; add regression coverage

- `grep -rn "TODO(SP-078)" pkg/console/` returns nothing.
- Add a `TestStatusFooter_SteerPanel_Wrap` table-driven test covering
  narrow / wide terminals, ASCII / CJK / combining-char content, and
  cursor positions that land mid-wrap.
- Add a `TestSteerInput_Completion_*` family exercising the new
  completion binding through real `SteerInputReader` cycles.
- Add a recording-style screenshot test (or `browse_url` snapshot
  against a live `make run` instance) so a future "wrap regression" is
  caught by a human at review time.

## Success Criteria

- **Wrap behavior:** a 200-char steer message in an 80-col terminal wraps
  to 3 visual rows; the caret sits at the correct (line, col) for every
  cursor position tested; the footer chrome does not overlap the steer
  text.
- **Cursor parity:** every cursor position reachable by arrow keys /
  Ctrl-A/E / Ctrl-B/F / mouse-click lands on the same visual cell as it
  would in the REPL prompt with identical buffer content.
- **Completion:** typing `/mo` + the new completion binding cycles
  through `/model`, `/mode`, etc., both between turns and mid-turn; an
  empty candidate set is a silent no-op (matches `InputReader`).
- **No regression:** all existing `steer_input_test.go` and
  `status_footer_test.go` cases pass unchanged; the five prior steer
  fixes (`e830d113`, `8f501bd3`, `6714f690`, `eb441143`, `ac75f0ed`)
  remain green.
- **Build:** `make build-all` clean; `go test ./pkg/console/...` green.

## Out of Scope

- **Mouse support / context menu in steer.** The steer panel is a single
  pinned row above the conversation area; right-clicking on conversation
  content to get a "Copy / Re-send / Cancel" menu is plausible future
  work but doesn't fix the perceived "rough" UX and is a WebUI-style
  feature on a TTY — high implementation cost for limited benefit.
- **Syntax highlighting / undo-redo.** Neither exists in `InputReader`
  today; not a steer-panel regression.
- **Auto-suggestions (fish-style greyed completions).** Not present in
  either reader; tracked separately if requested.
- **Touch / iTerm2 / Kitty-specific protocol extensions.** Stay
  portable; revisit if a specific gap is reported.

## Open Questions

1. **Phase 1 cap.** Should `maxSteerRows` grow (e.g., 8 or 10) once
   wrapping is in play, or stay at 6 to preserve terminal real estate?
   Recommendation: stay at 6; the soft-scroll via leading `…` already
   handles longer content, and the conversation area above is more
   valuable than 2 extra input rows.
2. **Phase 2 binding.** `Ctrl+]` is rare in readline but conflicts with
   nothing in our current keymap. Alternatives: `Shift+Tab` (reversed
   Tab, common for "previous completion"), `Alt+/`. Recommend polling
   in the spec's TODO items before implementation.
3. **Phase 1 ordering with the webui steer parity work.** The webui
   steer box (`webui/src/components/SteerPanel/...`) has its own
   rendering path and is *not* affected by this spec — its wrap math
   already lives in CSS. No cross-repo coordination required.

## Notes

- No reverted work exists. `git log --all --diff-filter=D -- 'pkg/console/*steer*'`
  is empty; the seven "feat/refactor(console): ... steer" commits on
  `main` (`22ec94f3`, `ac75f0ed`, `cfb53479`, `eb441143`, `6714f690`,
  `a07b3c61`, `8f501bd3`) all remain present; the most recent fix
  (`e830d113`) landed 2026-06-26.
- The feature gap is structural (single-file reader vs. decomposed
  reader), not missing code. Resolving it via Phase 1+2 closes the
  parity story and avoids re-decomposing `SteerInputReader` into 18
  sibling files for a feature that's only active mid-turn.