# SP-110: Background Completion Injection & Auto-Resume

**Status:** 🔵 Proposed — Design complete. 3 phases.

When the agent starts a background task (`shell_command --bg` or `run_automate`),
it must actively poll to discover the result. Each poll burns a full LLM
round-trip. The `run_automate` tool description promises automatic completion
injection but the implementation couldn't deliver it: the completion goroutine
fired `InjectInputContext` into a channel whose reader was dead by the time
most workflows finished.

This spec replaces the broken channel-injection model with a durable
notification queue, adds completion notification for all background shell
commands, and introduces opt-in auto-resume so the agent can wake itself
when background work finishes.

## Goal

1. All background completions are durably queued.
2. Shell background commands get completion notification.
3. Opt-in auto-resume with per-session budget controls.
4. Interrupt safety — no unattended burn loops.

## Phases

### Phase 1: Notification Queue
- `pkg/agent/notifications.go` — Notification type, QueueNotification, DrainNotifications, wakeup budget methods
- Agent struct fields: pendingNotifications, notifMu, wakeupTokensConsumed, wakeupResumeCount, wakeupDisabled, wakeupMu
- Drain at turn start in ProcessQueryWithContinuity

### Phase 2: Wire Completion Callbacks
- Fix run_automate: QueueNotification instead of InjectInputContext; context.Background() goroutine
- Shell bg: wakeup_timeout parameter, startWakeupWatcher method, BackgroundNotifier interface
- Tool descriptions updated

### Phase 3: Auto-Resume
- WakeupConfig in configuration (enabled, max_tokens_per_session, max_resumes_per_session)
- Daemon poller (wakeup_poller.go) — 2s ticker, all gates checked
- Interrupt safety: DisableWakeup on TriggerInterrupt, EnableWakeupIfDisabled on manual message
- Web UI controls in Settings → Agent → General

## Deliverables

| File | Change |
|---|---|
| pkg/agent/notifications.go | NEW |
| pkg/agent/agent.go | Fields added |
| pkg/agent/conversation.go | Drain + re-enable |
| pkg/agent/pause.go | DisableWakeup |
| pkg/agent/tool_handlers_automate.go | QueueNotification + context.Background() |
| pkg/agent/tool_security.go | Notifier wired |
| pkg/agent_tools/handler.go | BackgroundNotifier interface + ToolEnv.Notifier |
| pkg/agent_tools/shell_handler.go | wakeup_timeout + startWakeupWatcher |
| pkg/agent/tool_registrations.go | wakeup_timeout param + run_automate desc fix |
| pkg/configuration/config.go | WakeupConfig type |
| pkg/webui/server_lifecycle.go | startWakeupPoller |
| pkg/webui/wakeup_poller.go | NEW |
| pkg/webui/settings_api_partial_settings.go | applyWakeupSettings |
| webui/src/services/api/types/settings.ts | wakeup type |
| webui/src/components/settings/AgentBehaviorSettingsTab.tsx | Wakeup UI |
| webui/src/components/SettingsPanel.tsx | renderNumberInput prop |
