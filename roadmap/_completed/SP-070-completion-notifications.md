# SP-070: Agent Completion Notifications — Tell the User When It's Their Turn

**Status:** ✅ Implemented
**Date:** 2026-06-14
**Depends on:** SP-067 (event injection plumbing), SP-012 (notification center / `notificationBus`)
**Priority:** High
**Effort Estimate:** ~2-3 days (low effort, high value)

## Problem

Agent turns are long. Users start a task, switch to another window or browser
tab, and then have **no idea when the agent finishes** — or, worse, when it
**stops mid-run waiting for an approval**. Today notifications are **in-app
only**:

- **CLI:** no terminal bell (`\a`/BEL) on turn completion or on a blocking
  security prompt. Confirmed: no BEL emission anywhere in `pkg/console/`.
- **WebUI:** no browser `Notification` API usage anywhere in `webui/src/`.
  The `notificationBus` / notification center (SP-012) only renders toasts
  inside the app, which a backgrounded tab never shows.

The completion signal itself already exists — the CLI calls
`turnRenderer.FinalizeAtTurnEnd()` (`cmd/agent_modes.go:1205`) and the webui
receives a `query_completed` event (`useWebSocketEventHandler.ts:882`). This
spec wires those existing signals to **out-of-band** notifications so a user
who has looked away gets pulled back exactly when they're needed.

## Current State

| Surface | Completion signal present | User-visible when not looking |
|---|---|---|
| CLI | `FinalizeAtTurnEnd()` | ❌ none |
| CLI (blocked on approval) | security prompt rendered | ❌ none (silent wait) |
| WebUI | `query_completed` event | ❌ in-app toast only |
| WebUI (blocked on approval) | approval event | ❌ in-app only |

## Proposed Solution

Three notification channels, all **opt-in** and config-gated, driven by events
that already fire. Two trigger conditions: **turn completed** and **input
required** (blocked on a prompt/approval).

### Phase 1: CLI terminal bell + OS notification

- Emit a terminal bell (`\a`) on turn completion and on a blocking prompt,
  gated by a new config `notifications.cli_bell` (default **on** for
  interactive TTY, suppressed under `--skip-prompt`/non-TTY/`NO_COLOR`-style
  quiet modes).
- Optional OS notification via a tiny `pkg/notify` helper that shells out to
  the platform tool when present: `osascript -e 'display notification …'`
  (macOS), `notify-send` (Linux), `powershell` toast (Windows). Gated by
  `notifications.os_notify` (default **off**; enabled with a one-time hint).
- Only fire when the turn ran longer than a threshold
  (`notifications.min_seconds`, default 10s) so quick turns don't spam.

**Wire-in:** `cmd/agent_modes.go` around `FinalizeAtTurnEnd()` and at the
security-prompt entry points (`pkg/console/security_prompt.go`).

### Phase 2: WebUI browser notifications

- New `webui/src/services/desktopNotify.ts`: thin wrapper over the
  `Notification` API with a permission request flow.
- Fire a browser notification **only when `document.hidden`** (tab
  backgrounded) on `query_completed` and on an approval-required event,
  consumed in `useWebSocketEventHandler.ts`. Clicking the notification focuses
  the tab and the relevant chat session.
- Settings toggle in the notification/UX settings: "Notify me when the agent
  finishes or needs input" + a "Test" button. Reuse `notificationBus` history
  so the event is also recorded in the in-app center.

### Phase 3: New event for "input required"

- Add an `input_required` event (or reuse the existing approval-request event
  shape) in `pkg/events/` so both surfaces have a single, explicit signal for
  "agent is blocked on the human." This is the highest-value notification —
  an unattended agent stuck on a prompt is pure wasted wall-clock.

### Configuration

```go
type NotificationsConfig struct {
    CLIBell    bool `json:"cli_bell,omitempty"`     // default: true (interactive TTY)
    OSNotify   bool `json:"os_notify,omitempty"`    // default: false
    Browser    bool `json:"browser,omitempty"`      // default: false (opt-in via permission)
    MinSeconds int  `json:"min_seconds,omitempty"`  // default: 10
}
```

## Files Reference

| File | Action |
|------|--------|
| `pkg/notify/notify.go` | **New** — cross-platform OS notification helper (subprocess) |
| `pkg/notify/notify_test.go` | **New** — backend selection + no-op when tool absent |
| `cmd/agent_modes.go` | Modify — bell/OS notify on turn end + blocked prompt |
| `pkg/console/security_prompt.go` | Modify — bell on blocking approval |
| `pkg/configuration/config.go` | Modify — add `NotificationsConfig` |
| `pkg/events/events.go` | Modify — add `input_required` event |
| `webui/src/services/desktopNotify.ts` | **New** — `Notification` API wrapper |
| `webui/src/hooks/useWebSocketEventHandler.ts` | Modify — fire on `query_completed` / `input_required` when `document.hidden` |
| `webui/src/components/settings/` | Modify — notification preferences + Test button |

## Success Criteria

- CLI rings the terminal bell when a >10s turn finishes and when it blocks on
  an approval (suppressed in non-interactive mode).
- A backgrounded webui tab raises a browser notification on completion and on
  input-required; clicking it focuses the tab + chat.
- All channels are opt-in/configurable; defaults are non-annoying (bell on for
  TTY, OS/browser off until enabled).
- Quick turns (<`min_seconds`) do not notify.

## Out of Scope

- Push notifications to phones / email (Foundry platform concern).
- Per-session granular notification rules.
- Sound themes beyond the terminal bell.

## Open Questions

1. Should `os_notify` default on for macOS (where it's least intrusive) but
   off elsewhere? Lean: off everywhere until the user opts in once.
2. For Foundry cloud tasks, completion notification is a platform concern
   (the `--output-json` result + `pull_request_url` from SP-069) — confirm we
   don't double-notify.
