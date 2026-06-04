# SP-065: WebUI Automations Panel

**Status:** 📋 Proposed
**Date:** 2026-06-04
**Depends on:** SP-064 (uses the same BPM Stop primitive and PID-file format for cross-process discovery)
**Priority:** Medium

## Background

Today the only way for a human to launch or monitor a `sprout automate` workflow is the CLI. The WebUI has no automation surface — no list of available workflows, no run button, no live progress, no cost monitor, no stop button. Agent-initiated `run_automate` calls do surface via the existing intent-confirmation prompt, but there is no place to **watch** the resulting run in the browser, even though the daemon already has the captured output and event stream.

The webui already has all the infrastructure needed for a control panel:

- The event bus carries every agent event and is read by the React app for streaming.
- The intent-confirmation prompt is plumbed through the security approval manager.
- The `BackgroundProcessManager` (CLI mode) and `TerminalManager` (webui mode) both track running sessions with stable IDs and captured stdout.
- `automate.Summarize` produces the same price-card / step / budget data the CLI overview already renders.
- Workflow runtime emits `[budget] WARNING` and `[budget] CAP HIT` lines and per-iteration heartbeats — these are stdout today, easy to also publish as structured events.

The missing piece is the React panel + the few REST/WS endpoints to drive it.

## Problem

For a user who runs autonomous workflows on every codebase, the CLI is fine for "launch and check in tomorrow." The webui gap costs in three concrete scenarios:

1. **Cross-machine monitoring** — kick off a long run on the workstation, watch it from a laptop in a meeting.
2. **Mid-run drift detection** — a budget bar ticking up in a browser tab is a passive cue. The CLI heartbeat requires you to be reading a specific terminal.
3. **Agent-launched runs need a home** — when an agent in a chat calls `run_automate`, the human approves, but then has no live view of the workflow that just started in the background. The chat moves on; the workflow runs invisible.

## Proposed Solution

A new "Automations" panel in the webui, backed by a small set of REST endpoints and WS events. SP-064's PID-file mechanism is the cross-process discovery substrate.

### Phase 1: Backend — REST endpoints

Add handlers under `pkg/webui/automations_handlers.go`:

- `GET /api/automate/workflows` — discover workflow JSON files in the project's `automate/` directory (reuses `automate.Discover`); returns name, description, requires_approval, parsed Summary (including price card + budget).
- `GET /api/automate/sessions[?status=running|exited|all]` — list automate sessions known to the BPM AND the PID-file directory (SP-064).
- `GET /api/automate/sessions/:id` — one session's details (status, current spend/cap, current step, elapsed, last output snippet).
- `POST /api/automate/run` — body `{"workflow": "<name>", "budget_usd_override": optional, "warn_at_override": optional, "heartbeat_seconds_override": optional}`. Goes through the same `run_automate` tool path (so `requires_approval: false` still bypasses; otherwise an intent prompt is sent through the existing webui approval channel). Returns the new session ID.
- `POST /api/automate/sessions/:id/stop` — invokes `BPM.Stop` from SP-064.
- `GET /api/automate/sessions/:id/output[?since=offset]` — paged read of captured output for cold-fetch fallback when WS is dropped.

All endpoints honor the existing webui auth and origin checks.

### Phase 2: Backend — WS events

Extend the event bus with three new event types:

- `automate.session_started` `{session_id, workflow, budget_usd, started_at}`
- `automate.budget_update` `{session_id, spent, limit, iteration, elapsed_seconds, threshold_crossed?}` — emitted by the workflow runtime's existing budget callbacks + heartbeat. Roughly every heartbeat interval, plus immediately on threshold crossings.
- `automate.output_chunk` `{session_id, content, seq}` — incremental captured-output deltas; the React side appends. Coalesce small writes (≥250ms or ≥4KB).
- `automate.session_ended` `{session_id, status, final_spent, final_iteration, error?}`.

Routing: only delivered to subscribers explicitly opted into automate events (so chat sessions don't get spammed).

### Phase 3: Frontend — panel skeleton

Add `webui/src/components/AutomationsPanel.tsx` and a sidebar entry. Three sections:

1. **Available workflows** — list of workflow files from `/api/automate/workflows`. Each row shows name, description, a "⚠ no approval" tag if `requires_approval: false`, and a "Run" button. Clicking Run opens a modal with the price card, budget, and inputs to override `budget_usd` / `heartbeat_seconds` for this run only; "Start" calls `POST /api/automate/run`.
2. **Running** — sessions with `status: running`. Each row: workflow name, **live budget bar** (`$X / $Y` with warning color past 50%, danger past 80%), current step / iteration, elapsed time, "Open" and "Stop" buttons.
3. **Recent** — recently-completed sessions (last N). Status badge (completed / fleet_budget_exceeded / failed / stopped), final spend, total iterations, duration.

### Phase 4: Frontend — session detail view

Clicking "Open" on a running or recent session pushes a detail view in the same panel:

- Header: workflow name, status, current spend/cap with bar, iteration, elapsed.
- Captured-output stream — appended via `automate.output_chunk` events, scrolls with auto-scroll lock when user scrolls up.
- Step timeline (when the workflow has `steps`) — visual list with checkmarks for completed agent + shell steps, current step highlighted.
- Budget event log — every threshold crossing and the cap-hit event, with timestamps.

### Phase 5: Frontend — chat session linkage

When an agent in a chat calls `run_automate` and the user approves, the chat shows a **link** to the running automation in the Automations panel:

> ▶ Started `validate.json` — [open in Automations panel](#)

Clicking the link switches the sidebar to Automations with the session selected. Closes the gap where agent-launched runs vanish into the background.

### Phase 6: Tests

- Unit: workflow discovery endpoint with mixed valid/invalid JSON; session list with mock BPM + PID-file fixture; run endpoint enforces intent confirmation for `requires_approval: true`.
- WS: subscribe, kick off mock run, assert event ordering (`session_started` → budget updates → `session_ended`).
- React: AutomationsPanel renders states (empty, running, recent), budget bar color transitions, intent-confirmation modal flow.
- Integration: real sprout daemon, launch a tiny shell-only workflow from the panel, watch it complete in the WS stream, verify status row updates without manual refresh.

### Phase 7: Docs

Add a brief WebUI section to the workflow-automation skill explaining the panel exists, and a "WebUI usage" section in `workflow_properties.md`. Out-of-skill, add a one-paragraph note to the project README.

## Out of Scope

- **Workflow authoring in the WebUI** — JSON editing for workflows. Use the existing editor or the workflow-automation skill in chat. The panel is for invocation, not authoring.
- **Per-workflow analytics** — avg cost, success rate, duration distribution over the last N runs. Useful eventually but premature.
- **Cron/schedule UI** — OS cron + `sprout automate run` already covers this.
- **Mobile-optimized layout** — the panel works on a phone-sized viewport but doesn't get bespoke mobile UX.
- **Stop-and-resume via the UI** — restarting from a checkpoint is its own scope (tracked separately if needed).
- **Real-time streaming of agent reasoning** for automate sessions — captured stdout is sufficient; the deep agent-event stream is a chat-session concern.

## Success Criteria

- Opening the webui shows an Automations panel listing every workflow in `automate/`.
- Clicking "Run" on a workflow shows the price card + budget, the user confirms (or skips when `requires_approval: false`), and the run starts.
- A running workflow's budget bar updates within 1 s of each LLM response.
- The output panel streams captured stdout with no >1 s lag in normal operation.
- Stopping via the panel terminates the run within 15 s (matches SP-064's grace period).
- When an agent in a chat calls `run_automate`, the chat shows an inline link to the panel, and clicking it focuses the new session.
- A `requires_approval: false` workflow can be launched from the panel WITHOUT a confirmation modal (mirrors the agent tool path).
- Two browser tabs open to the same daemon both see the same running sessions and both update live.
- `make build-all` and the existing webui tests pass.

## Effort Estimate

Rough sizing:

- Backend REST endpoints + workflow discovery glue: ~1 day
- Backend WS event plumbing (publishing from the budget callbacks + heartbeat + output capture): ~1 day
- Frontend panel skeleton (list + run modal + running rows): ~1.5 days
- Frontend session detail view (output stream + step timeline + budget event log): ~1.5 days
- Chat → automate link plumbing: ~half-day
- Tests (backend handlers, WS, React components, integration): ~1.5 days
- Polish / docs: ~half-day

Total: ~7-8 days of focused work. Highly dependent on SP-064 landing first.

## Open Questions

1. Where does the Automations panel live in the existing sidebar — same level as Chat and Editor, or a sub-pane of Chat? Probably top-level given the panel can outlive any chat session.
2. Should the run-modal allow editing more workflow JSON fields beyond budget/heartbeat (e.g. `max_iterations`, persona)? Tempting but risks scope creep; defer until a concrete request.
3. For the chat→automate link, do we render a static link, or a live widget showing budget/status inside the chat message itself? Static link is simpler; live widget is delightful but adds complexity. Start static.
4. The intent-confirmation prompt today routes through the security approval manager and renders as a modal. For automate runs launched FROM the panel (not from a chat), should the confirmation also appear as a panel-local modal, or keep using the same global manager? Reuse the global manager — one approval UX is better than two.
5. How long do we keep "Recent" sessions visible after they exit? Default 24h? User-configurable?
6. Multi-daemon scenario: with SP-060 (per-workspace daemons), each daemon has its own automate panel. Acceptable for v1.
