# SP-116: Multi-Instance Isolation

> ✅ **Implemented** — Phases 1–4 shipped 2026-07-15.
> Commits: `ac4d72e6` (Phase 1), `ef47144d` (Phase 2), `c7c4047b` (Phase 3),
> `99991ba2` (service-daemon guard), `c0602add` (daemon-side bootstrap).

## Summary

Sprout needs to support two distinct multi-instance modes:

1. **Daemon mode** (`sprout agent --daemon`): Multiple instances, each with
   independent cwd, fully isolated config and state. This is what the desktop
   app creates for each workspace window.

2. **Interactive CLI mode** (`sprout agent`): One instance per cwd, with a
   unique port, where the CLI and WebUI share the same session state and the
   user can swap between them seamlessly.

## Current State Assessment

### What already works

| Capability | Status | Notes |
|---|---|---|
| `--isolated-config` | ✅ Exists | Sets `SPROUT_CONFIG` to `.sprout/` in cwd. Clones main config on first use. Desktop app uses this for every workspace. |
| Per-instance port assignment | ✅ Exists | Interactive mode scans from 56001 for free ports. Daemon mode uses 56000 or explicit `--web-port`. |
| Instance heartbeat registry | ✅ Exists | `instances.json` in config dir. Uses `flock` for concurrent writes. Tracks PID, port, cwd, session. |
| CLI↔WebUI session sharing | ✅ Exists | `sharedWebServer` + `SetEventMetadata(client_id:"default")` in non-daemon mode. `showWebUIHandoffOnce` suppresses CLI output when browser connected. |
| Workspace config path | ✅ Defined | `GetWorkspaceConfigPath()` returns `.sprout/config.json`. Only `IsWorkspaceConfigPresent()` consumes it. |
| Background process manager | ✅ Exists | `/tmp/sprout-bg/` with session tracking. CLI `sprout shell-bg` commands. |
| Multi-workspace daemon | ✅ Exists | `NewAgentWithLayersInWorkspace` creates per-workspace agents in daemon WebUI. `WorkspacePicker` for switching. |
| Recent workspace tracking | ✅ Exists | `~/.sprout/recent_workspaces.json` tracks last 10 workspaces with markers. |
| macOS/Linux service manager | ✅ Exists | `sprout service install/start/stop/status` via launchd (macOS) and systemd (Linux).

### What's broken for multi-instance

#### 🔴 Critical — blocks the core workflow

**1. Interactive CLI mode uses global config, not isolated**

When you run `sprout agent` in `project-a/`, it uses `~/.config/sprout/`. If
you also run `sprout agent` in `project-b/`, both share the same config dir →
`instances.json` is shared → the instance list mixes workspaces. The web server
starts fine (dynamic port), but the config is global.

**Impact**: Provider changes in one workspace affect others. Instance registry
is cluttered. No per-workspace state isolation.

**2. No `--isolated-config` auto-detection**

The desktop app explicitly passes `--isolated-config`. CLI users must remember
to type `sprout --isolated-config agent` every time. The workspace config file
(`.sprout/config.json`) exists but goes unused as an auto-detection signal.

**Impact**: Default CLI workflow is not isolated. User must opt in every time.

#### 🟡 Medium — degrades the experience

**3. Background processes share a global directory**

`/tmp/sprout-bg/` is shared across all instances. `sprout shell-bg list` shows
sessions from all workspaces.

**Impact**: User sees noise from unrelated workspaces.

**4. Daemon web UI supervisor uses global `webui_host.json`**

The `webUISupervisor` (daemon mode, no explicit port) writes to
`getConfigDir()/webui_host.json`. With `--isolated-config`, this goes to
`.sprout/webui_host.json` — correct. Without `--isolated-config`, this goes to
`~/.config/sprout/webui_host.json` — shared.

**Impact**: Without isolation, only one daemon can own the web UI role.

**5. Session resume / state scoping**

`maybeOfferSessionResume` in interactive mode reads session history. Need to
verify it's scoped to the isolated config dir.

#### 🟢 Low — nice-to-have

**6. Workspace-level config overrides**

Providers are user-level (API keys are personal). But some settings (model
preference, subagent provider, persona config) could reasonably differ per
workspace. `GetWorkspaceConfigPath()` exists but only `IsWorkspaceConfigPresent()`
consumes it.

**Impact**: No way to say "use claude-opus for this repo, but gemini for
that one."

## Design

### Principle: Config dir follows cwd

The config directory should be:
- **`$cwd/.sprout/`** when the workspace has been initialized (`.sprout/config.json`
  exists, or auto-bootstrapped on first `sprout agent`).
- **`~/.config/sprout/`** as fallback (legacy, no workspace init).

This eliminates `--isolated-config` as a flag the user must remember and makes
it the default behavior.

### Phase 1: Make isolated config the default

> ✅ **Shipped** — `ac4d72e6 feat: auto-detect git repos for per-workspace isolated config`.
> Auto-detection lives in `cmd/root.go::detectGitRepo` and the
> `PersistentPreRunE` guard (honors explicit `--isolated-config`, `SPROUT_SERVICE=1`,
> and `CI`/`GITHUB_ACTIONS` to skip). Tests: `cmd/root_test.go`.

**1a. Auto-detect workspace config in `PersistentPreRunE`**

In `cmd/root.go`, detect when we're in a git repo and auto-bootstrap:

```
If --isolated-config is explicitly passed → use it (existing behavior).
Else if a .git directory exists in cwd or ancestors → auto-isolate.
  - If .sprout/config.json already exists → use it.
  - If not → bootstrap (clone from global config) → use it.
Else → use global ~/.config/sprout/ (legacy behavior).
```

**Recommendation: git-detection trigger.** Repos get automatic isolation.
Random directories don't. This matches user expectation: "in a project, my
sprout settings should stay with the project."

**1b. Remove `--isolated-config` from desktop app**

The desktop app currently passes `--isolated-config` explicitly. After this
change, it becomes a no-op (auto-detected). Keep passing it for backward compat
initially; remove in a follow-up after verifying auto-detection works in desktop
context.

### Phase 2: Per-instance background processes

> ✅ **Shipped** — `ef47144d feat: scope background processes to config dir`.
> `BackgroundProcessManager` now resolves its base directory from the active
> config dir (`.sprout/bg-processes/` for isolated, `~/.config/sprout/bg-processes/`
> for global) instead of the global `/tmp/sprout-bg/`. `sprout shell-bg`
> reuses the same config resolution.

**2a. Scope BPM directory per config dir**

Change the background process directory from `/tmp/sprout-bg/` to
`<configDir>/bg-processes/` (i.e., `.sprout/bg-processes/` for isolated,
`~/.config/sprout/bg-processes/` for global).

**2b. Scope `sprout shell-bg` to the current config**

The `shell-bg` command should use the same config resolution as `sprout agent`
to find the right BPM directory.

### Phase 3: Workspace config overrides (stretch)

> ✅ **Shipped** — `c7c4047b feat: layered config for auto-detected workspaces`.
> `agent.NewAgentWithLayers(globalDir, workspaceDir, …)` merges the global
> config with the auto-detected workspace's `.sprout/config.json`; providers
> still resolve from the global layer. Wired in `cmd/agent_command.go` via
> `autoDetectedWorkspaceDir`.

Allow `.sprout/config.json` to override specific settings while inheriting
providers/credentials from the global config.

Merge strategy: workspace config values take precedence, falling back to global
config values. Providers are always resolved from the global config (API keys
are personal, not per-project).

### Phase 4: Daemon service hardening (launch priority)

> ✅ **Shipped** — `99991ba2 fix: skip auto-isolation for system service daemon`
> and `c0602add feat: auto-bootstrap workspace config when daemon opens a git repo`.

The `sprout service` daemon on port 56000 is the primary way users interact with
sprout (desktop app is deferred). The daemon must:

- **Skip auto-isolation**: System daemon should always use global config
  (`~/.config/sprout/`). Done — `SPROUT_SERVICE=1` guard in `PersistentPreRunE`.
- **Multi-workspace via WebUI**: The daemon WebUI already supports per-workspace
  agents via `NewAgentWithLayersInWorkspace` + `WorkspacePicker`. Each workspace
  gets its own layered config (global providers + workspace overrides).
- **Service manager robustness**: `sprout service install/start/stop/status`
  works correctly on macOS (launchd) and Linux (systemd).
- **Graceful startup without provider**: When no LLM provider is configured,
  the daemon keeps the WebUI running for onboarding (already implemented).

## Files to change

### Phase 1

| File | Change | Risk |
|---|---|---|
| `cmd/root.go` | Auto-detect workspace config when `.git` exists in cwd or ancestors | Medium — changes default behavior for all `sprout` commands |
| `cmd/agent_modes.go` | No changes needed (already uses `getConfigDir()`) | Low |
| `cmd/common.go` | No changes needed (already uses `getConfigDir()`) | Low |
| `pkg/configuration/isolated_config.go` | Handle empty source config gracefully (no global config exists yet) | Low |
| `desktop/backend.js` | Remove `--isolated-config` after verifying auto-detection | Low |
| `cmd/root_test.go` | New tests for auto-detection logic | Low |

### Phase 2

| File | Change | Risk |
|---|---|---|
| `pkg/agent_tools/background_process_manager.go` | Base dir from config dir, not `/tmp/sprout-bg/` | Medium — changes file paths |
| `cmd/shell_bg.go` | Use `getConfigDir()` for BPM directory resolution | Low |

### Phase 3 (stretch)

| File | Change | Risk |
|---|---|---|
| `pkg/configuration/config_load_save.go` | Load workspace config, merge with global | High — config loading is a hot path |
| `pkg/configuration/config_paths.go` | Add `LoadWorkspaceConfig()` helper | Low |

## What intentionally does NOT change

- **Provider credential storage**: Stays in global config. API keys are
  personal, not per-project. (Phase 3 layers workspace overrides on top.)
- **Web UI supervisor**: Works correctly with isolated config. No changes
  needed to the leader-election logic itself.
- **Port assignment logic**: Works correctly. Dynamic ports for interactive,
  explicit or 56000 for daemon. No changes needed.
- **CLI↔WebUI session sharing**: Works correctly in interactive mode via
  `sharedWebServer`. No changes needed.

## Testing strategy

1. **Unit tests**: Auto-detection logic in `root.go` — test `.git` detection,
   ancestor traversal, missing `.git`, explicit `--isolated-config` override.

2. **Integration test**: Start two sprout instances in different directories
   with `.git`. Verify:
   - Each has its own `.sprout/config.json`
   - Each has its own `instances.json`
   - Each gets a unique port
   - `sprout shell-bg list` only shows sessions from the current workspace

3. **Desktop smoke test**: Launch two workspace windows. Verify they operate
   independently — different providers, different sessions, no cross-talk.

4. **Backward compat**: Verify `sprout agent` in a non-repo directory still
   uses `~/.config/sprout/` (global config).

## Out of scope

- Multi-user sprout daemon (service mode already handles one system-level daemon)
- Network-shared config directories
- Container/cloud deployment isolation (Docker handles this via volume mounts)
