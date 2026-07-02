# SP-070: Agent Completion Notifications — Tell the User When It's Their Turn

**Status:** ✅ Implemented (2026-06-14; CLI bell, browser notifications, input_required event)

When an agent finishes a long turn or blocks on an approval, users had no out-of-band signal if they weren't looking at the terminal or browser tab. This spec wired the existing turn-completion and approval signals to terminal bells, OS notifications, and browser Notification API alerts — all opt-in and config-gated. The highest-value signal is `input_required`, which catches an agent silently stuck waiting for a human.

## Key decisions

- Terminal bell defaults ON for interactive TTY, suppressed under `--skip-prompt`/non-TTY
- OS notifications default OFF everywhere (opt-in with one-time hint)
- Browser notifications fire only when `document.hidden` (tab backgrounded)
- Minimum turn duration of 10s before notifying to avoid spam on quick turns
- All three channels share a single `NotificationsConfig` struct in config

## Artifacts

- code: `pkg/notify/notify.go` — cross-platform OS notification helper (shells out to platform tool)
- code: `pkg/notify/notify_test.go` — backend selection + no-op when tool absent
- code: `webui/src/services/desktopNotify.ts` — Notification API wrapper with permission flow
- code: `cmd/agent_modes.go` — bell/OS notify on turn end + blocked prompt
- code: `pkg/console/security_prompt.go` — bell on blocking approval
- tests: `webui/src/hooks/useWebSocketEventHandler.ts` — fires on query_completed / input_required when hidden

Full specification archived — see git history for original content.
