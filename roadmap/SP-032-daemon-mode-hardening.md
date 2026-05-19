# SP-032: Daemon Mode Hardening

**Status:** 📋 Proposed
**Date:** 2026-05-18
**Priority:** CRITICAL (daemon ships in-tree; ops gaps affect every installed user)
**Depends on:** None
**Related:** SP-014 (Agent Terminal Sessions — PTY routing), SP-028 (test stabilization — shares the PTY shutdown fix), SP-030 (repo hygiene — Phase 4 de-scoped here)

## Problem

Sprout ships a system-service mode (`sprout service install/start/stop/uninstall`) on macOS (launchd) and Linux (systemd). The install/uninstall surface is well-structured — user-mode units, atomic env-file writes, a defensive binary-name check at install time. **The in-process shutdown path, however, is incomplete in three CRITICAL ways**, plus there are several HIGH-severity ops gaps (no auth-token enforcement, no crash backoff on launchd, no migration for users with the predecessor `ledit` service still installed).

When `systemctl stop sprout` runs today, the agent, every MCP server subprocess, and every active PTY session are abandoned — systemd eventually SIGKILLs the process group, but the daemon's own cleanup hooks never fire. Embeddings aren't flushed; the changelog may not be synced; users see "zombie" `bash`/`zsh`/`gopls` processes in `ps` afterwards.

## What's already good (don't change)

For context — these are correct and don't need touching:

- User-mode service (`cmd/service_linux.go` emits `WantedBy=default.target`; not a system service)
- Atomic service.env write/replace pattern (`cmd/service_env.go:76–114`)
- Defensive binary-name guard at install: `cmd/service_linux.go` rejects install when `filepath.Base(binaryPath) != "sprout"`
- HTTP bound to `127.0.0.1` by default
- 1s heartbeat / 4s stale threshold in webui supervisor (`cmd/webui_supervisor.go:17-18`)

## Current State (verified)

### CRITICAL gaps

| # | Where | What's missing |
|---|-------|----------------|
| C1 | `pkg/webui/server_lifecycle.go:126` `Shutdown()` | Never calls `ws.terminalManager.CloseAllSessions()` — the function exists at `pkg/webui/terminal_lifecycle.go:65` but no production caller. PTYs leak on daemon stop. Same root cause as SP-028's test-hang. |
| C2 | `cmd/agent_modes.go:447-460` graceful shutdown block | Calls `webServer.Shutdown()` and `webUISup.cleanupHostRecordIfOwned()` but never `chatAgent.Shutdown()`. The method exists at `pkg/agent/agent_lifecycle.go:10`. Agent MCP children, LSP processes, async output workers all leak. |
| C3 | `cmd/service_linux.go` systemd unit template | Sets `Restart=on-failure`, `RestartSec=5`. Missing `TimeoutStopSec=` (defaults to 90s — too long for an unattended daemon) and `KillMode=` (defaults to `control-group`, which will SIGKILL children after the timeout instead of routing them through the agent's cleanup). |

### HIGH gaps

| # | Where | What's missing |
|---|-------|----------------|
| H1 | `cmd/service_darwin.go:77` launchd plist | `KeepAlive=true` with only `ThrottleInterval=30`. A deterministic panic restarts every 30s indefinitely (no `SuccessfulExit` qualifier, no max-retries, no exponential backoff). |
| H2 | `pkg/webui/server.go:161-162` | Auth token is read from env and **silently disabled** when unset. The middleware (`server_lifecycle.go:48`) only attaches when `authToken != ""`. Safe on `127.0.0.1`, catastrophic if combined with `SPROUT_BIND_ADDR=0.0.0.0`. No warning at install time. |
| H3 | `cmd/service_*.go` install paths | No detection of a pre-existing `ledit`-named service unit (`com.ledit.daemon` plist or `ledit.service` systemd unit) from the predecessor tool. The binary-name guard prevents a wrong-binary install, but doesn't disable an old unit that's still pointing at an old binary. |

### MEDIUM / LOW gaps

| # | Where | What |
|---|-------|------|
| M1 | `cmd/service_darwin.go:35-36` | `~/.sprout/logs/daemon.{stdout,stderr}.log` grow unbounded. `lumberjack` is already a dependency. Linux is fine (journald). |
| M2 | `cmd/service_darwin.go:220`, `cmd/service_linux.go:125` | `Uninstall()` is silent — no "active sessions detected, continue?" prompt. In-flight conversations are lost. |
| M3 | `cmd/agent_modes.go:240` | `signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)` — no `SIGHUP` handler, so config reload requires full restart. |
| L1 | `docs/` | No `docs/SERVICE.md`. Feature discoverable only via `sprout service --help`. |
| L2 | `cmd/service_other.go` | Windows Service Manager unimplemented. Out of scope for this spec — see SP-007 or a future SP. |

## Goals / Non-Goals

**Goals**
- Daemon stop is graceful: agent, MCP subprocesses, PTYs, async workers all complete shutdown within a bounded window.
- A user upgrading from the predecessor `ledit` service is moved cleanly to `sprout-daemon`.
- The daemon's HTTP API cannot be exposed unauthenticated by accident.
- A panicking daemon backs off instead of hot-looping.
- Operators have a discoverable install + troubleshoot guide.

**Non-Goals**
- Windows Service support (SP-014 territory, or future spec).
- Rewriting the launchd / systemd integration. We extend, we don't replace.
- Changing the user-mode design choice (daemon runs as the invoking user).
- Live config-reload semantics beyond a SIGHUP that re-reads the config file (no hot-swap of the running agent).

## Proposed Solution

### Track A — Graceful shutdown (CRITICAL)

#### A1: Add `Shutdown()` call chain
- `cmd/agent_modes.go:447-460` — extend the graceful-shutdown block to call, in order:
  1. `chatAgent.Shutdown()` (closes MCP children, flushes embeddings/checkpoints, stops async workers)
  2. `webServer.Shutdown()` (existing; now closes PTYs first — see A2)
  3. `webUISup.cleanupHostRecordIfOwned()` (existing)
- Bound each step with its own context deadline (5s, 5s, 1s). Log which step exceeded its budget.

#### A2: Wire `CloseAllSessions()` into web-server `Shutdown()`
- `pkg/webui/server_lifecycle.go:126` — inside `Shutdown()`, call `ws.terminalManager.CloseAllSessions()` *before* `ws.server.Shutdown(ctx)` so PTY children are reaped before the HTTP listener closes.
- **This is the same fix as SP-028 Phase 3** — coordinate so it lands once. SP-028's `done`-channel changes to the PTY read loop (`pkg/webui/terminal_create.go:146-175`) are a prerequisite: `CloseAllSessions()` won't unblock readers until those channels exist.

#### A3: Tighten the systemd unit
- `cmd/service_linux.go` — add to the `[Service]` block:
  ```
  TimeoutStopSec=15
  KillMode=mixed
  KillSignal=SIGTERM
  ```
  `KillMode=mixed` sends SIGTERM to the main process only, then SIGKILL to the whole control group after the timeout. This gives the agent a clean window to flush before children are force-killed.

### Track B — Security & migration (HIGH)

#### B1: Enforce auth when bind address is non-local
- `pkg/webui/server.go:161` — read both `SPROUT_AUTH_TOKEN` and `SPROUT_BIND_ADDR`. If bind is non-`127.0.0.1`/`localhost` and token is empty, refuse to start with a clear error: "Refusing to start: SPROUT_BIND_ADDR=%s requires SPROUT_AUTH_TOKEN to be set."
- `sprout service install` should also warn (not fail) when it detects `SPROUT_BIND_ADDR` in the env file pointing somewhere non-local, regardless of token presence.

#### B2: Detect & uninstall legacy `ledit` service on install
- New helper `detectLegacyService()` in `cmd/service.go` (cross-platform). On Darwin, checks for `~/Library/LaunchAgents/com.ledit.*.plist`. On Linux, checks for `~/.config/systemd/user/ledit*.service`.
- If found during `sprout service install`: print a clear notice, prompt for confirmation (`-y` flag bypasses), then `launchctl bootout` / `systemctl --user disable && rm` the old unit.
- Document the behaviour in `sprout service install --help`.

#### B3: Launchd crash backoff
- `cmd/service_darwin.go:77` — replace bare `KeepAlive=true` with the dictionary form that enables `SuccessfulExit=false` (only restart on non-zero exit) and add `ExponentialBackoff=true` (a `launchd` 12.0+ option; if older macOS needs supporting, fall back to a wrapper script with `sleep $((2**attempt))`).

### Track C — Operability (MEDIUM/LOW)

- **C1**: Wrap Darwin daemon log files in `lumberjack.Logger` (`pkg/logging` already imports it). 10MB max, 5 backups.
- **C2**: `sprout service uninstall` — before tearing down, ask the daemon via its HTTP API "any active sessions?" If yes, print warning + count; require `-y` to proceed.
- **C3**: Add SIGHUP handler in `cmd/agent_modes.go:240` that triggers `configuration.Reload()`. Scope is limited to config-on-disk re-read; running tools/agents are unaffected.
- **C4**: Author `docs/SERVICE.md`: install, start, stop, uninstall, troubleshoot, log locations, env-file structure, security model (user-uid, 127.0.0.1 default, auth-token requirement for non-local).

## Cross-spec coordination

- **SP-028 Phase 3** owns the PTY *test* infrastructure (`done` channels, `goleak`, cancellable reads) — A2 here owns the *production* call site. Land SP-028 Phase 3 first; A2 is a one-line follow-up.
- **SP-030 Phase 4 is de-scoped** by this spec. The audit confirmed the live install path already uses `sprout-daemon` / `com.sprout.daemon` and the binary-name check refuses non-sprout binaries. The remaining concern (detecting pre-existing `ledit` units) moves to B2 here. SP-030 4a/4b can be deleted.

## Implementation Phases

### Phase 1: Graceful shutdown (CRITICAL — 1-2 days)

- [ ] **SP-032-1a**: Add `chatAgent.Shutdown()` call to `cmd/agent_modes.go:447-460` with bounded context.
- [ ] **SP-032-1b**: Wire `terminalManager.CloseAllSessions()` into `pkg/webui/server_lifecycle.go:126` `Shutdown()`. **Blocked by SP-028 Phase 3** (needs cancellable PTY read).
- [ ] **SP-032-1c**: Add `TimeoutStopSec=15`, `KillMode=mixed`, `KillSignal=SIGTERM` to the systemd unit template in `cmd/service_linux.go`.
- [ ] **SP-032-1d**: Manual verification — run `sprout service install` + `start`, open a web terminal, kick off an agent query, run `systemctl --user stop sprout`. Confirm no orphan `bash`/`gopls`/MCP processes via `pgrep`.

### Phase 2: Security & migration (HIGH — 2-3 days)

- [ ] **SP-032-2a**: Bind-address vs auth-token check at startup in `pkg/webui/server.go:161`. Refuse to start when non-local bind + empty token.
- [ ] **SP-032-2b**: Legacy `ledit` service detection in `cmd/service.go`; integrate into `Install()` flow on both platforms.
- [ ] **SP-032-2c**: Launchd crash backoff in `cmd/service_darwin.go:77`. Use the `KeepAlive` dictionary form with `SuccessfulExit=false`; document `ExponentialBackoff` requirement (macOS 12+).

### Phase 3: Operability (MEDIUM/LOW — 1-2 days)

- [ ] **SP-032-3a**: `lumberjack.Logger` for `~/.sprout/logs/daemon.{stdout,stderr}.log` on Darwin (10MB × 5).
- [ ] **SP-032-3b**: Pre-uninstall active-session check + warning prompt. Add `-y` / `--yes` to both Darwin and Linux `Uninstall()` paths.
- [ ] **SP-032-3c**: SIGHUP handler → `configuration.Reload()`. Wire into the signal handler at `cmd/agent_modes.go:240`.
- [ ] **SP-032-3d**: Write `docs/SERVICE.md` (install/start/stop/uninstall/troubleshoot + security model section).

### Phase 4: Test fixtures (cleanup — 1 day)

- [ ] **SP-032-4a**: Update `cmd/service_darwin_test.go` and `cmd/service_linux_test.go` — replace `/usr/local/bin/ledit` test fixtures with `/usr/local/bin/sprout` so tests cover the actual binary-name guard.

## Success Criteria

| Metric | Target |
|--------|--------|
| `systemctl --user stop sprout` followed by `pgrep -f sprout` | Returns empty (no orphans) within 15s |
| Daemon panics in test harness | Restart attempts back off after 3 failures within 60s |
| `SPROUT_BIND_ADDR=0.0.0.0` without `SPROUT_AUTH_TOKEN` | Daemon refuses to start, prints clear error |
| Old `ledit` daemon present at install | Detected, prompted, removed before new install proceeds |
| Darwin daemon log files | Rotate at 10MB, ≤ 5 backups |
| `sprout service uninstall` with active sessions | Prompts for confirmation; `-y` bypasses |
| SIGHUP to daemon | Reloads config without dropping connections |
| `docs/SERVICE.md` | Exists, covers all 4 lifecycle commands + security model |

## Risks

- **PTY close may deadlock on a tight read loop.** Mitigation: SP-028 Phase 3 must land first so reads are cancellable via `done` channel. Without that, `CloseAllSessions()` blocks waiting for read returns.
- **`KillMode=mixed` changes systemd semantics for users with custom configs.** Mitigation: document the change in `docs/SERVICE.md`; users with extensions of the unit file can override.
- **macOS `ExponentialBackoff` not available pre-12.** Mitigation: feature-detect or document the minimum macOS version. Sprout's other Darwin dependencies (Electron 37) already require recent macOS.
- **Detecting legacy `ledit` service may false-positive** if a user has unrelated services starting with `ledit`. Mitigation: match the full `com.ledit.daemon` / `ledit.service` label, not a prefix.
- **Bounded shutdown deadlines may be too short** under heavy embedding-flush load. Mitigation: tune via config; start with 5s/5s/1s and adjust based on real runs.

## Files Reference

| File | Action |
|------|--------|
| `cmd/agent_modes.go` | Modify: graceful shutdown block at 447-460 (call `chatAgent.Shutdown`); signal handler at 240 (add SIGHUP) |
| `pkg/webui/server_lifecycle.go` | Modify: `Shutdown()` at line 126 — call `terminalManager.CloseAllSessions()` |
| `pkg/webui/server.go` | Modify: bind-address vs auth-token check at line 161 |
| `cmd/service_linux.go` | Modify: systemd unit template — `TimeoutStopSec`, `KillMode`, `KillSignal` |
| `cmd/service_darwin.go` | Modify: plist `KeepAlive` dictionary form (line 77); add `lumberjack` log wrapping; uninstall prompt |
| `cmd/service.go` | Modify: legacy-service detection helper; `Install()` flow integration; `--yes` flag for uninstall |
| `cmd/service_darwin_test.go`, `cmd/service_linux_test.go` | Modify: replace `ledit` test fixtures with `sprout` |
| `docs/SERVICE.md` | Create: end-user install/troubleshoot guide |
| `pkg/logging/logger.go` | Reuse: existing `lumberjack` integration for Darwin daemon logs |
