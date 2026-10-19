# SP-166 — Wheel and Scroll Routing

Status: Proposed (2026-10-19). Split from a 17-item user nit list. Touches
`pkg/console/mouse_menu.go`, `pkg/console/input_mouse.go`,
`pkg/console/select_list*.go`, and the footer's scroll region
(`pkg/console/status_footer_scroll.go`).

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

## Acceptance criteria

- [ ] 166a: the chosen tracking mode is recorded with its rationale, and the
      observed effect on the terminal's own scrollback is verified in a real
      terminal (not just a unit test — this is a terminal-behavior change).
- [ ] With no picker open, the wheel scrolls the transcript (or the terminal
      scrollback works — whichever 166a chose).
- [ ] With a security escalation open, the user can read the full command
      being approved (test / manual verification).
- [ ] The picker remains fully navigable by keyboard; the footer hint names
      the keys (test).
- [ ] Right-click context menu and outside-click dismissal still work (test).
- [ ] No regression in `ask_user` pickers or the session picker.

## Non-goals

- No new scrolling UI (a scrollbar, a pager) — this is about routing the
  wheel correctly, not building a scroll widget.
- No change to the footer's scroll-region math
  (`pkg/console/status_footer_scroll.go`), which is load-bearing and
  separately tested.
- No change to the security *decision* path.

## Open questions

- **The central one**: drop wheel capture, or route it? This determines the
  whole shape of the work. The user's complaint is best served by not
  capturing; the picker's wheel navigation is the cost. Get a decision before
  implementing.
- Whether the picker should keep wheel-for-selection when it is open (a
  "modal" interpretation: while a picker owns input, the wheel belongs to
  it) or always yield to the transcript.
- Whether mouse tracking is needed at all beyond the context menu; if the
  menu is the only consumer, a narrower mode may suffice.
