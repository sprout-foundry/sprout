# SP-064: Automate CLI — Status, Stop, Logs

**Status:** ✅ Implemented (2026-06-04; sprout automate status/stop/logs commands)

The `sprout automate` feature lacked monitoring and control commands — once a workflow started, users had no way to check status, stop runs, or tail logs from a different terminal. This spec shipped three CLI subcommands (`status`, `stop`, `logs`) backed by a BPM `Stop` primitive and cross-process PID-file persistence. The agent tool path's `stop_background` now works in CLI mode, matching WebUI parity.

## Key decisions

- Added a `kind` field to the BPM `Process` struct (default `"shell"`, set to `"automate"` for workflow sessions) instead of relying on naming conventions — naming conventions silently break during refactors.
- Cross-process persistence uses per-session PID files in `.sprout/automate/<session_id>.json` rather than a shared BPM state file — simpler to debug and matches the existing `workflow_state.json` pattern.
- BPM `Stop` sends SIGINT with a 10-second grace period, escalating to SIGTERM then SIGKILL — matches standard process termination semantics.
- CLI-launched sessions use `cli-automate-<16-hex>` IDs; agent-launched sessions use `bg-<prefix>-<8-hex>` — consumers filter on `kind == "automate"` regardless of ID format.
- Stale PID files are swept at the start of every `sprout automate *` subcommand via `SweepStaleSessions()`.

## Artifacts

- code: `cmd/automate.go` — status/stop/logs CLI subcommands
- code: `pkg/agent_tools/background_process.go` — BPM `Stop` primitive and `kind` field
- code: `pkg/automate/pid_file.go` — cross-process PID-file persistence and stale sweep
- code: `pkg/webui/automations_api.go` — WebUI automation API endpoints
- tests: `cmd/automate_test.go` — CLI subcommand tests

Full specification archived — see git history for original content.
