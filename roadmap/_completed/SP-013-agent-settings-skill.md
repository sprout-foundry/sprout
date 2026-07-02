# SP-013: Agent Settings Management Tool

**Status:** ✅ Implemented (`manage_settings` tool registered)

The agent needed an in-conversation way to read and modify its own configuration
(model, provider, OCR toggle, commit provider, etc.) instead of bouncing the user
out to the Settings panel or a manual config edit. The `manage_settings` tool
exposes `get`/`set`/`list_providers`/`test_credential`/`describe`/`preview`
operations against the existing `configuration.Config`. Design choices: no skill
wrapper (settings are occasional, not per-conversation); self-documenting
returns (invalid keys/values return actionable guidance); useful-over-terse
confirmations on every `set` so the agent can act without a follow-up call.

## Key decisions

- **No skill.** Skills load context on every turn; settings management is an
  occasional request, so the tool is self-documenting instead.
- **Self-documenting returns** — invalid keys/values/operations return
  guidance, not errors. The agent learns by calling.
- **`test_credential` reuses the existing API ping path** rather than parsing
  keys, so it returns the same failure modes the real call would.
- **`describe`/`preview` are non-mutating** — agents can call them without
  committing to a change.
- **Validation lives at the configuration layer**, not the handler, so the
  Settings UI and the agent see the same rules.

## Artifacts

- code: `pkg/agent/settings_handler.go` — handler
- code: `pkg/agent/tool_registrations.go` — `manage_settings` registration
- tests: `pkg/agent/settings_handler_test.go`
- companion: `cmd/agent_commands/manage_settings.go` (CLI parity)

Full specification archived — see git history for original content.