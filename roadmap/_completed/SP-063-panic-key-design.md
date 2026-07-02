# SP-063-4g: Panic Key — Emergency Stop for Computer-Use Action Loops

**Status:** ✅ Implemented (SP-063 computer_use gate 4g, shipped 2026-06-30; OS-level chord detection best-effort)

This sub-doc settles the design question: "what should sprout do if the
computer-use agent gets stuck in an action loop — clicking the same button,
typing into the wrong field, dragging a window across the screen — and the
user needs to abort right now?" The answer: a panic-key chord
(`Ctrl+Shift+Escape` by default, configurable via
`ComputerUseConfig.PanicKeyChord`) immediately halts the action loop. The
chord sets a halted flag on a `PanicableBackend` decorator that wraps the
subprocess backend, kills any in-flight subprocess tree via process-group
`SIGKILL`, cancels the agent's `interruptCtx` (reusing the existing WebUI-Stop
path), and records `panic_key_triggered` / `panic_key_triggered_duplicate` /
`panic_key_reset` audit events. After a halt the user must re-consent to
computer use (`computerUseSessionApproved` is reset) — the panic key is
a real break, not a pause.

The OS-level chord watcher is best-effort: macOS uses CGEventTap (when
permissions allow), Linux uses XRecord (X11 only, no-op on Wayland/headless),
other platforms fall through to polling. The WebUI Stop button remains the
primary halt path; the OS chord watcher is a redundant safety net for users
who don't have the WebUI focused.

## Key decisions

- **Reuse the existing `interruptCtx` cancellation path** rather than building
  a new halt mechanism. The WebUI Stop button already does this; panic key
  is just an alternative trigger.
- **Process-group `SIGKILL`** so the entire subprocess tree (the action
  process, anything it spawned) dies atomically. No orphans left clicking.
- **Re-consent after halt** — `computerUseSessionApproved` is reset, so the
  user must re-acknowledge before computer use resumes. A panic is a real
  break.
- **OS-level chord is best-effort, not required.** Wayland doesn't allow
  synthetic input monitoring by design; headless runs have no display.
  Documented limitations; WebUI Stop remains the canonical halt.
- **Default chord is `Ctrl+Shift+Escape`** — distinct from common shortcuts,
  unlikely to conflict with the agent's own keystrokes during an action loop.

## Artifacts

- code: `pkg/agent_tools/computer_use/panic_key.go` — `PanicableBackend` decorator
- code: `pkg/agent_tools/computer_use/panic_key_chord.go` (cross-platform dispatcher)
- code: `pkg/agent_tools/computer_use/panic_key_chord_darwin.go`, `panic_key_chord_linux.go`, `panic_key_chord_other.go`
- code: `pkg/agent_tools/computer_use/process_group_unix.go`, `process_group_other.go`
- tests: `pkg/agent_tools/computer_use/panic_key_test.go`, `panic_key_chord_test.go`

Full specification archived — see git history for original content.