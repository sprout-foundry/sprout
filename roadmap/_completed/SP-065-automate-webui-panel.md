# SP-065: WebUI Automations Panel

**Status:** ✅ Implemented (2026-06-04; REST endpoints, WS event stream, React panel)

The WebUI had no surface to discover, launch, or monitor `sprout automate` workflows — everything was CLI-only. This spec added REST endpoints for workflow discovery and session management, WebSocket events for live budget/output streaming, and a React AutomationsPanel with workflow list, running sessions with live budget bars, and a session detail view with output stream and step timeline. Agent-launched runs now show an inline link back to the panel.

## Key decisions

- REST endpoints reuse `automate.Discover` and the BPM's existing session tracking
- WS events use explicit opt-in subscription to avoid spamming chat sessions
- Output chunks are coalesced (≥250ms or ≥4KB) before publishing over WS
- Panel lives at top-level in sidebar (not nested under Chat) since runs can outlive chat sessions
- Run modal reuses the global security approval manager for intent confirmation

## Artifacts

- code: `pkg/webui/automations_api.go` — REST handlers (workflows, sessions, run, stop, output)
- code: `webui/src/components/AutomationsPanel.tsx` — React panel with workflow list, running rows, detail view
- tests: `pkg/webui/automations_api_test.go` — handler tests with mock BPM + PID-file fixtures
- tests: `pkg/webui/automations_integration_test.go` — end-to-end workflow launch and completion
- tests: `pkg/webui/websocket_automate_subscription_test.go` — WS event ordering tests

Full specification archived — see git history for original content.
