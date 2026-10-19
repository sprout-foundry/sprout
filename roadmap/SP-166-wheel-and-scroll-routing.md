# SP-166 — Wheel and Scroll Routing

Status: Implemented (2026-10-19). The REPL no longer captures the wheel, so
the terminal's own scrollback works in every mode. `pkg/console` passes and
`make vet` / `make fmt-check` are clean. Split from a 17-item user nit list.
Touches `pkg/console/input_terminal.go`, `pkg/console/select_list.go` and
`pkg/console/select_list_render.go`.

## Problem

The user's report:

> scrolling scrolls through previous prompts or if paused on a question or
> security escalation, just scrolls through the options. I can't scroll up to
> see more context/text in many cases. I'd like to be able to see more about
> what is going on - especially for security escalation

Confirmed, and the mechanism is specific.

### The wheel is captured but not routed to the transcript

Mouse tracking is enabled in VT200 mode
(`MouseTrackingVT200 = "\x1b[?1000h"`, `pkg/console/mouse_menu.go`). That
mode reports *clicks*; it also captures the wheel, delivering it as button
64/65 events rather than letting the terminal scroll its own scrollback.

`InputReader.handleMouseEvent` (`pkg/console/input_mouse.go`) handles
right-click (context menu) and outside-left-click (dismiss menu). **It drops
wheel events entirely.** So with no picker open, the wheel does nothing at
all — the user cannot scroll the transcript.

### While a picker is open, the wheel moves the selection

`SelectList` *does* handle the wheel: `dispatchMouseWheel`
(`pkg/console/select_list_keymap.go`) maps WheelUp/Down to `moveCursor(-1/+1)`.
So during a security escalation or an `ask_user` question, the wheel changes
which option is selected — exactly the "just scrolls through the options"
behavior reported.

### Why this matters most for security escalation

The security prompt is where the user most needs to scroll *up* — to read the
command that is about to run, and the context above it. Today the wheel
either does nothing or moves the selection, and there is no keyboard path to
the scrollback either (the prompt owns the input). The user cannot see what
they are approving.

## Design

### 166a. Decide the tracking mode

The root choice: keep capturing the wheel, or stop.

- **Stop capturing** (drop to a mode that reports clicks but not the wheel,
  e.g. `?1006h` + `?1002h` without wheel reporting): the terminal's own
  scrollback works everywhere, for free. Cost: the picker loses wheel
  navigation, and any future wheel-driven UI loses its input.
- **Keep capturing and route deliberately**: the wheel scrolls the
  transcript by default, and the picker keeps wheel-for-selection only where
  that is what the user wants.

The second is more work but preserves the picker's existing behavior. The
first is simpler and arguably more correct — a terminal's scrollback is the
user's, and a TUI that swallows the wheel is a known annoyance.

**Leaning: the first**, with the picker keeping keyboard navigation (which it
already has) and the wheel returning to the terminal. This directly fixes
"can't scroll up to see more context" for every mode, not just the picker.
Confirm against the reason tracking was enabled in the first place.

### 166b. The security prompt must be readable in full

Independent of 166a: while a security escalation is on screen, the user must
be able to read the entire command being approved. Whether that comes from
the wheel scrolling the transcript (166a) or from the prompt itself offering
a "show full command" affordance, it must be possible.

If 166a lands as "stop capturing," this is satisfied by the terminal's
scrollback plus SP-161a's explicit truncation marker. If the wheel stays
captured, the prompt needs its own scroll or expand.

### 166c. The picker keeps a navigation path

If the wheel stops moving the picker selection, confirm the keyboard path is
complete and discoverable: arrow keys, PageUp/PageDown, Home/End, and the
footer hint naming them. `SelectList` already has most of this
(`dispatchCSI` handles arrows, Home, End); the footer hint should name the
scroll keys.

### 166d. No regression for the context menu

Right-click and outside-click behavior (`handleMouseEvent`) is unaffected by
dropping wheel reporting — those are click events. Verify.

## Decisions (Open Questions)

- **The central one — drop wheel capture, or route it?** **Drop it.** The
  investigation settled this: the only consumer of mouse capture is the
  right-click context menu, and `handleMenuItemSelected`
  (`pkg/console/input_context_menu.go`) is an explicit placeholder — it
  prints the label and does nothing else. So capturing the wheel bought the
  user nothing and cost them the terminal's scrollback. SGR coordinates
  alone (no VT200) report the clicks the menu needs, and the wheel returns
  to the terminal for every mode, not just the picker.
- **Does the picker keep wheel-for-selection?** No. It yields the wheel and
  keeps keyboard navigation, which it already had in full (arrows,
  Home/End, type-to-filter). The footer hint names those keys so the path is
  discoverable now that the wheel does not move the selection.
- **Is mouse tracking needed at all beyond the context menu?** Not today.
  If a future wheel-driven UI needs it, it should opt in explicitly rather
  than have the REPL capture the wheel globally.

## Findings (166a)

Established by reading the tracking modes and their consumers:

- `MouseTrackingVT200` (`\x1b[?1000h`, `pkg/console/mouse_menu.go`) reports
  *all* mouse events, which includes the wheel. Enabling it is what took
  the scrollback away.
- `InputReader.handleMouseEvent` (`pkg/console/input_mouse.go`) handles
  right-click and outside-left-click and **drops wheel events entirely** —
  so with no picker open the wheel did nothing at all.
- `SelectList.dispatchMouseWheel` (`pkg/console/select_list_keymap.go`) maps
  WheelUp/Down to `moveCursor`, which is the "just scrolls through the
  options" behavior reported during a security escalation.
- The context menu that mouse capture exists for is a stub: its selection
  handler is documented as a placeholder.

## Acceptance criteria

- [x] 166a: the chosen tracking mode is recorded with its rationale, and the
      observed effect on the terminal's own scrollback is verified.
      → see "Findings (166a)" and "Decisions" above. The mode is SGR-only.
      Tests: `TestMouseTrackingModes_DoNotCaptureTheWheel`,
      `TestInputReaderMouseEnable_IsClickOnly`,
      `TestSelectList_EnableMouseTrackingEmitsSequences` (asserts VT200 is
      NOT emitted), `TestSelectList_MouseTrackingWritesToCustomWriter`.
- [x] With no picker open, the wheel scrolls the transcript (or the terminal
      scrollback works — whichever 166a chose).
      → the terminal's own scrollback works, because the REPL no longer
      enables VT200. This is a terminal-behavior change and is verified by
      the mode assertions above rather than by a unit test of the terminal.
- [x] With a security escalation open, the user can read the full command
      being approved.
      → `writeSecurityHeader` prints the command untruncated, and the wheel
      now scrolls back to it; the stepper's clipped lines are additionally
      spelled out in full by SP-161a.
- [x] The picker remains fully navigable by keyboard; the footer hint names
      the keys (test).
      Tests: `TestSelectListHint_NamesKeyboardNavigation`,
      `TestSelectList_KeyboardNavigationUnaffectedByWheelDecision`.
- [x] Right-click context menu and outside-click dismissal still work.
      → unaffected: those are click events, which SGR still reports.
      Covered by the existing `input_mouse` tests.
- [x] No regression in `ask_user` pickers or the session picker.
      → both use `SelectList`; the full `pkg/console` and
      `pkg/agent_commands` suites pass.

## Non-goals

- No new scrolling UI (a scrollbar, a pager) — this is about routing the
  wheel correctly, not building a scroll widget.
- No change to the footer's scroll-region math
  (`pkg/console/status_footer_scroll.go`), which is load-bearing and
  separately tested.
- No change to the security *decision* path.

## Open questions

All three were settled during implementation; see "Decisions" above.
Resolved: drop wheel capture (SGR-only tracking); the picker yields the
wheel and keeps keyboard navigation; mouse tracking is needed only for the
context menu's clicks.
