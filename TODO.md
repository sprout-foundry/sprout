# TODO

Active work tracked here. Completed items are removed once their parent spec is moved to ✅ Implemented in `roadmap/README.md` — the spec file itself is the historical record.

## SP-064: Automate CLI — Status, Stop, Logs
_Spec: `roadmap/SP-064-automate-cli-monitoring.md`_

### Phase 1: BPM Stop primitive
- [x] SP-064-1a: Add `(*BackgroundProcessManager).Stop(sessionID string, grace time.Duration) error` in `pkg/agent_tools/background_process.go`. SIGINT → grace → SIGTERM → 5s → SIGKILL. Updates status to `exited`. No-op on already-exited sessions.
- [x] SP-064-1b: Wire `BPM.Stop` into the `shell_command(stop_background=…)` tool path in `pkg/agent_tools/shell_handler.go` so CLI mode reaches parity with the WebUI TerminalManager.
- [x] SP-064-1c: Revert the "stop_background not available for automate sessions in CLI mode" caveat in `pkg/skills/library/workflow-automation/SKILL.md`.
- [x] SP-064-1d: Unit tests — signal sequencing on a controlled sleep subprocess (mock or real with very short grace periods), no-op on exited, error on unknown session.

### Phase 2: Session-kind tagging
- [x] SP-064-2a: Add `Kind string` field to BPM `Process` struct, default `"shell"`.
- [x] SP-064-2b: Set `Kind = "automate"` in `pkg/agent/tool_handlers_automate.go` `handleRunAutomate` BPM `Start` call.
- [x] SP-064-2c: Set `Kind = "automate"` in `cmd/automate.go` `runWorkflowByPath` — but this path uses `exec.Command` not BPM; either move CLI launches through BPM or write the same `kind=automate` marker to the PID file (Phase 3) and treat that as the source of truth for CLI-launched runs.

### Phase 3: Cross-process discovery (PID files)
- [x] SP-064-3a: On every workflow launch (CLI or agent tool), write `.sprout/automate/<session_id>.json` containing `{workflow, pid, started_at, output_file_path, budget_usd?, kind: "automate"}`.
- [x] SP-064-3b: Remove the PID file on clean shutdown (workflow process exit handler).
- [x] SP-064-3c: Stale-PID sweep at startup of any `sprout automate *` subcommand — `kill -0` each PID, remove files whose process is dead.
- [x] SP-064-3d: Document the PID-file schema in `roadmap/SP-064-automate-cli-monitoring.md` so SP-065's webui consumer doesn't drift.

### Phase 4: status / stop / logs subcommands
- [x] SP-064-4a: `cmd/automate.go` — add `automateStatusCmd` (`sprout automate status [--all] [--json]`). Reads PID files + BPM in-memory state, prints table.
- [x] SP-064-4b: `cmd/automate.go` — add `automateStopCmd` (`sprout automate stop <session_id>` or `--all`). Calls `Stop` (or sends signals directly when only the PID file is known).
- [x] SP-064-4c: `cmd/automate.go` — add `automateLogsCmd` (`sprout automate logs <session_id> [-f] [-n N]`). Reads the captured output file; `-f` polls at 500ms ticks.
- [x] SP-064-4d: Add subcommands to `automateCmd.AddCommand` and update help text.

### Phase 5: Tests + docs
- [x] SP-064-5a: Integration test — launch a sleep-based workflow, status shows it, stop kills it, status reflects exit, output file persists.
- [x] SP-064-5b: Cross-process test — launch from terminal A (real subprocess), assert `sprout automate status` from a separate process sees it via the PID file.
- [x] SP-064-5c: Update `workflow_properties.md` with a "Monitoring a running workflow" section.
- [x] SP-064-5d: Run `make build-all` and the full automate test suite; verify green.

## SP-065: WebUI Automations Panel
_Spec: `roadmap/SP-065-automate-webui-panel.md`_
_Blocked by: SP-064 (Phases 1–3 are prerequisites for cross-process session discovery)_

### Phase 1: Backend REST
- [x] SP-065-1a: `pkg/webui/automations_handlers.go` — `GET /api/automate/workflows` reuses `automate.Discover` + `automate.Summarize`.
- [x] SP-065-1b: `GET /api/automate/sessions` and `GET /api/automate/sessions/:id` — read BPM + PID files (SP-064-3a).
- [x] SP-065-1c: `POST /api/automate/run` — body validation, optional overrides, dispatches through the `run_automate` tool path so `requires_approval` and the security gate are honored.
- [x] SP-065-1d: `POST /api/automate/sessions/:id/stop` — calls `BPM.Stop`.
- [x] SP-065-1e: `GET /api/automate/sessions/:id/output?since=offset` — paged output read for WS-drop fallback.
- [x] SP-065-1f: Wire endpoints into the existing webui router with auth/origin checks.

### Phase 2: Backend WS events
- [x] SP-065-2a: Define event types in `pkg/events/`: `automate.session_started`, `automate.budget_update`, `automate.output_chunk`, `automate.session_ended`.
- [x] SP-065-2b: Publish `session_started` / `session_ended` from `handleRunAutomate` and CLI launch.
- [x] SP-065-2c: Publish `budget_update` from the existing budget warning + exceeded callbacks AND from the heartbeat tick in `cmd/agent_workflow.go`.
- [ ] SP-065-2d: Tee captured-output writes through a `automate.output_chunk` publisher with coalescing (≥250ms or ≥4KB).
- [ ] SP-065-2e: Subscription opt-in so chat sessions don't see automate events by default.

### Phase 3: Frontend panel
- [x] SP-065-3a: `webui/src/components/AutomationsPanel.tsx` — three sections (Available / Running / Recent). Wire to REST endpoints + WS subscription.
- [x] SP-065-3b: Add Automations entry to sidebar nav.
- [x] SP-065-3c: Run modal — shows price card + budget, allows per-run budget/heartbeat override, calls `POST /api/automate/run`.
- [x] SP-065-3d: Budget bar component with 50%/80% color transitions.
- [x] SP-065-3e: Running-row Stop button → `POST stop` with confirmation dialog.

### Phase 4: Session detail view
- [x] SP-065-4a: Detail panel route — header with status/budget/iteration/elapsed.
- [x] SP-065-4b: Captured-output stream component, auto-scroll-lock on user scroll-up.
- [x] SP-065-4c: Step timeline when `steps` exists — checkmarks for completed, highlight for current.
- [x] SP-065-4d: Budget event log — threshold crossings + cap-hit timestamps.

### Phase 5: Chat ↔ automate linkage
- [x] SP-065-5a: When `run_automate` succeeds in a chat, emit an inline chat message containing a link to the Automations panel with the new session id.
- [x] SP-065-5b: Sidebar nav handler — clicking the link switches to Automations and focuses the session.

### Phase 6: Tests
- [ ] SP-065-6a: Handler unit tests — workflow discovery, run with requires_approval=true triggers intent prompt, run with requires_approval=false skips, stop terminates.
- [ ] SP-065-6b: WS event ordering test — start → updates → end.
- [ ] SP-065-6c: React component tests — AutomationsPanel renders empty / running / recent states; budget bar color transitions; intent confirmation modal flow.
- [ ] SP-065-6d: Integration test against a real daemon with a shell-only workflow.

### Phase 7: Docs
- [ ] SP-065-7a: Add a "WebUI usage" section to `workflow_properties.md`.
- [ ] SP-065-7b: Add a WebUI paragraph to `SKILL.md` explaining the panel exists and how it relates to the agent tool path.
- [ ] SP-065-7c: One-paragraph README mention.
