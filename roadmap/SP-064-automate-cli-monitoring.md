# SP-064: Automate CLI — Status, Stop, Logs

**Status:** ✅ Implemented (sprout automate status/stop/logs)
**Date:** 2026-06-04
**Depends on:** none (extends the existing `cmd/automate.go` surface and the BPM)
**Priority:** Medium

## Background

The `sprout automate` feature now has hard cost caps, soft warnings, a runtime heartbeat, and a price-card overview, but once a workflow starts the only way to monitor or control it is to keep the terminal in the foreground. There is no equivalent of `sprout automate status`, no clean stop command, no log tail. If the original terminal closes or the user moves to another machine, the only recovery paths are reading `.sprout/workflow_state.json` directly or `ps | grep | kill`.

The `BackgroundProcessManager` (`pkg/agent_tools/background_process.go`) already tracks every running workflow as a session with stable IDs, captured stdout/stderr, and process handles. It is exposed to the agent loop via `shell_command(check_background=…, wait_seconds=…)`, but there is no CLI surface that lets a human poll the same state.

`StopBackgroundSession` is implemented for the WebUI's `TerminalManager` but **not** for the CLI's `BackgroundProcessManager`. The agent tool description for `run_automate` had to call this out as a CLI limitation (SKILL.md update in the previous round).

## Problem

For a user who runs autonomous workflows on every codebase, the absence of CLI monitor/control commands turns "kicked off a 4-hour run" into "I either keep this terminal open the whole time or I have no way to inspect or stop it." This is friction that compounds across every run.

Concretely:

- No way to ask "what's running right now?" from a fresh terminal.
- No way to stop a running workflow without OS-level signal hunting.
- No way to tail the captured output of a running workflow that started elsewhere.
- No way to inspect a recently-completed workflow's final output without scrolling the original terminal.

## Proposed Solution

Add three CLI subcommands under `sprout automate`, backed by a stop primitive added to the BPM.

### Phase 1: BPM `Stop` primitive

Extend `pkg/agent_tools/background_process.go` with `(*BackgroundProcessManager).Stop(sessionID) error`. Semantics:

- Send SIGINT to the process group (matching the workflow's `os/exec` invocation in `runWorkflowByPath`).
- On a configurable grace period (default 10 s), escalate to SIGTERM, then SIGKILL.
- Update the session's status to `exited` so a subsequent `CheckOutput` reflects the new state.
- Returns nil on a session that's already exited.

The existing `TerminalManager.StopBackgroundSession` covers the WebUI; this fills the parity gap. Wire it through the `shell_command(stop_background=…)` tool path so the same call works in both modes — the skill's mention of "stop_background is not available for automate sessions in CLI mode" can be reverted.

### Phase 2: `sprout automate status`

`sprout automate status [--all] [--json]`

Lists active background sessions that were launched via `sprout automate`. Output a table:

```
SESSION                 WORKFLOW            STATUS    SPENT/CAP        ITER  ELAPSED
bg-automate-7f3a91     validate.json       running   $1.20 / $5.00    23    4m12s
bg-automate-1c0e2d     full_autonomous     exited    $7.85 / $10.00   142   58m04s
```

Distinguishing automate sessions from generic background shell sessions requires a marker on the BPM session. Two options:

1. Naming convention — prefix all `run_automate`-launched sessions with `bg-automate-` and filter on prefix.
2. Metadata field — extend BPM `Process` struct with `kind string` and tag automate sessions explicitly.

Prefer **(2)** — naming conventions silently break when refactored. Add `kind` to the BPM, default `"shell"`, set `"automate"` from `handleRunAutomate` and from `runWorkflowByPath`'s CLI path.

`--all` includes exited sessions still in BPM memory (BPM already retains them with a configurable TTL). `--json` emits the same data structured for piping.

### Phase 3: `sprout automate stop`

`sprout automate stop <session_id>` or `sprout automate stop --all`.

Resolves the session via BPM (must be `kind=automate`), calls `Stop()`, prints the final captured output snippet (last N lines).

`--all` stops every active automate session — useful when the user kicks off a run, walks away, and wants to nuke everything before catching a flight.

### Phase 4: `sprout automate logs`

`sprout automate logs <session_id> [-f] [-n N]`

Prints the captured output of a session. `-f` tails the running session's stdout/stderr by reading the BPM's output file with a small polling interval (matches the existing pattern in `CheckBackgroundOutputWait`). `-n` shows only the last N lines.

For exited sessions, just prints whatever's still buffered (or in `.sprout/workflow_events.jsonl` if we extend the events writer to also capture stdout; deferred).

### Phase 5: Cross-session persistence

The BPM is per-process. If you run `sprout automate run X` from terminal A, then open terminal B and run `sprout automate status`, terminal B's BPM is empty — there's no shared state.

Two reasonable approaches:

1. **PID file per session** in `.sprout/automate/<session_id>.json` containing the workflow path, PID, started_at, output_file_path. `sprout automate status` reads the directory; `sprout automate stop` reads the PID and sends signals directly (no BPM dependency for cross-process operations).
2. **Persistent BPM state** in `.sprout/bpm.json` reconciled at startup. More invasive.

Prefer **(1)** — simpler, easier to debug ("just look in `.sprout/automate/`"), and matches the existing `.sprout/workflow_state.json` pattern.

A nightly cleanup sweep removes stale entries whose PID no longer exists.

#### PID-file schema

Each session writes `.sprout/automate/<session_id>.json`. The schema is defined in `pkg/automate/pid_file.go` (`AutomateSessionInfo`):

```json
{
  "workflow": "full_autonomous.json",
  "pid": 12345,
  "started_at": "2026-06-04T22:00:00Z",
  "output_file_path": "/tmp/sprout-bg/bg-automate-abc123.output",
  "budget_usd": 5.0,
  "kind": "automate"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `workflow` | string | yes | Basename of the workflow JSON file |
| `pid` | int | yes | OS process ID of the sprout agent subprocess |
| `started_at` | RFC3339 | yes | Timestamp when the process was launched |
| `output_file_path` | string | no | Path to captured stdout/stderr temp file (agent-launched sessions only; CLI sessions pipe directly to the terminal) |
| `budget_usd` | float | no | Per-run USD budget cap (omitted when no budget is set) |
| `kind` | string | yes | Always `"automate"` — used by consumers to filter automate sessions from other PID-file consumers |

**Lifecycle:**
1. Written immediately after `cmd.Start()` succeeds (CLI path) or after BPM `StartWithKind` returns (agent path).
2. Removed on clean process exit via `defer RemoveSessionFile(...)`.
3. Stale files (PID no longer alive) are swept at the start of every `sprout automate *` subcommand via `SweepStaleSessions()`.

**Session ID format:**
- CLI-launched: `cli-automate-<16-hex-chars>` (crypto/rand)
- Agent-launched: `bg-<sanitized-prefix>-<8-hex-chars>` (BPM-generated)

Consumers (SP-065 WebUI panel, `sprout automate status`) should read the directory with `ListSessionFiles()` and check `kind == "automate"` to filter.

### Phase 6: Tests + docs

- Unit: BPM Stop primitive (mock process, signal sequencing, grace-period escalation).
- Integration: launch a sleep-based workflow, status shows it, stop kills it, status shows it gone.
- Cross-process: launch from terminal A, status from terminal B sees it (via PID file).
- Update `SKILL.md` to drop the "stop_background not available" caveat once the WebUI/CLI parity lands.
- Update `workflow_properties.md` with a "Monitoring a running workflow" section.

## Out of Scope

- Persisted run history beyond the current `.sprout/workflow_events.jsonl` (deferred to a future spec; if needed, the event log already has the data).
- Per-workflow analytics (avg cost, avg duration).
- Re-attaching to a running session for interactive control (the workflow is autonomous; we don't reopen its stdin).
- Cron/schedule integration (use OS cron; `sprout automate run` already works there).

## Success Criteria

- `sprout automate status` lists every running automate session across all terminals on the machine.
- `sprout automate stop <id>` terminates a running session within 15 s (10 s SIGINT grace + 5 s SIGTERM grace) and reflects the new state in `status`.
- `sprout automate logs <id> -f` streams output of a running session started from a different terminal.
- `sprout automate stop --all` cleanly stops every running automate session.
- `.sprout/automate/<session_id>.json` files are written on launch and removed on clean shutdown; stale entries are detected and cleaned up.
- The agent tool path's `shell_command(stop_background=<automate session id>)` works in CLI mode (parity with WebUI).
- `make build-all` and the existing automate tests still pass.

## Effort Estimate

Rough sizing:

- BPM `Stop` primitive + tests: ~half-day
- Status/stop/logs commands wired to BPM: ~half-day
- Cross-process PID file approach + cleanup sweep: ~half-day
- Tests + docs + skill cleanup: ~half-day

Total: ~2 days of focused work, plus an audit pass.

## Open Questions

1. Should `sprout automate status` also list sessions launched from the agent tool path (run_automate)? Yes — they're the same kind. But the session ID surfaces from the BPM which is per-process — only the daemon's BPM sees them unless the PID-file approach covers daemon-launched runs too. Probably needs both terminal-launched AND daemon-launched runs writing PID files.
2. How long do we retain stale automate PID files? 7 days? Until the next clean reboot?
3. For `sprout automate logs -f`, what polling cadence balances responsiveness with file-read overhead? 500 ms matches the existing `CheckBackgroundOutputWait` tick; reasonable.
4. Do we want `sprout automate restart <session_id>` for the common "it failed near the end, run it again" case? Probably yes, but defer to a follow-up — needs to interact with the orchestration checkpoint/resume mechanism.
