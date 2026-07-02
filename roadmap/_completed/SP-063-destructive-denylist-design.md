# SP-063-4h: Destructive-App Denylist — Pre-Click Gate for Computer-Use Actions

**Status:** ✅ Implemented (SP-063 computer_use gate 4h, shipped 2026-06-30)

This sub-doc settles the design question: "what should sprout do when
computer-use mode is about to click on a destructive application (Mail, Disk
Utility, a banking app, a password manager, an incognito browser window)?"
The answer: a pre-action gate fires before any `mouse_click`, `keyboard_press`,
`scroll`, or `mouse_drag` whose foreground target matches a curated denylist.
The gate reuses the existing `checkComputerUseSessionOptIn` approval cascade
(WebUI dialog or CLI fallback), so users get one consistent prompt experience.
A per-session `computerUseAppAllowlist` short-circuits the gate after the
first "Allow once." "Always allow this app" persists an `"allow": true` entry
to `~/.config/sprout/computer_use_denylist_overrides.json` so future sessions
don't re-prompt for the same app. The hand-curated list ships in
`pkg/agent_tools/computer_use/denylist.json` (21 macOS + 22 Linux entries
across financial/system/destructive/password_manager categories); users can
append new apps or remove existing ones via the override file.

## Key decisions

- **Per-action gate, not per-session.** Catching the destructive app *before*
  the click matters; a per-session opt-in is too coarse — once a user has
  opted in to computer use, they're not necessarily opting in to clicking on
  Mail.
- **Reuse `checkComputerUseSessionOptIn`.** A new approval cascade would be
  one more thing to maintain; the existing one already handles WebUI +
  CLI fallback correctly.
- **Hand-curated list, not ML.** An ML classifier would have to run on every
  click; a hand-curated JSON lookup is microseconds. The 43-entry default
  list catches the common destructive cases; users extend via override file.
- **Foreground detection, not window-title regex.** On macOS, `osascript`
  asks the OS for the frontmost app bundle ID; on Linux X11, `xdotool` +
  `wmctrl` extract the active window's WM_CLASS. Wayland/headless is a
  no-op (returns "unknown"), which means the gate is best-effort on those
  platforms — explicitly documented as such.
- **Screenshot and Wait skip the gate.** Reading the screen and idle waits
  are read-only operations; no destructive intent.
- **Power-user opt-out** via `ComputerUseConfig.DestructiveAppGate = false`,
  but default is on.

## Artifacts

- code: `pkg/agent_tools/computer_use/audit.go` — `PreActionHook` on `auditingBackend`
- code: `pkg/agent_tools/computer_use/denylist.go` — `Loader.IsDestructiveApp`
- code: `pkg/agent_tools/computer_use/foreground.go` — OS-specific foreground detection
- config: `pkg/agent_tools/computer_use/denylist.json` — 43 default entries
- code: `pkg/agent/destructive_app_prompter.go` — agent-side prompter
- tests: `pkg/agent_tools/computer_use/denylist_allow_test.go`,
  `pkg/agent/destructive_app_test.go`

Full specification archived — see git history for original content.