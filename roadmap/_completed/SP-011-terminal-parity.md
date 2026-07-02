# SP-011: Terminal Parity & Bug Fixes

**Status:** ✅ Shipped (all 3 phases complete 2026-06)

The webui `Terminal.tsx` had drifted from native-terminal parity in three
ways: (1) `pty_exit` signals weren't handled so panes went black on shell
exit; (2) the pane/session model was a 1:1 string when multiple sessions
per pane was expected; (3) the file was 1320 lines with dead code paths.
Phase 1 fixed the exit handling (1.5s delay before clearing), the
per-pane session model, and pruned the file. Phase 2 polished CSS for
exited panes, fixed zoom, and removed an unused `Terminal` component
from `@sprout/ui`. Phase 3 added the missing features: terminal search,
clickable file paths, copy-on-select, scrollback persistence. Terminal.tsx
now 780 lines with `useTerminalPanes.ts` extracting the multi-session
state machine.

## Key decisions

- **Per-pane session model, not per-pane shell.** A pane can hold multiple
  sessions (one active, others backgrounded/closed); the `pane.sessions`
  array models this directly.
- **1.5s delay before clearing a `pty_exit`** so the user can read the
  exit message before the pane blanks.
- **Extract `useTerminalPanes` hook** rather than splitting the component
  — the state machine was the largest source of complexity.
- **Drop unused Terminal component from `@sprout/ui`** (it was a
  different API surface; webui's Terminal.tsx is the real one).
- **Scrollback persistence via xterm's buffer serialization**, not custom
  storage. Standard library feature, no reinvention.

## Artifacts

- code: `webui/src/components/Terminal.tsx` — 780 lines (was 1320)
- code: `webui/src/hooks/useTerminalPanes.ts` — multi-session state
- code: `webui/src/components/TerminalPane.tsx` — single-pane component
- tests: `webui/src/components/Terminal.test.tsx`

Full specification archived — see git history for original content.