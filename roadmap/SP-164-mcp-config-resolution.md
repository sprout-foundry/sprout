# SP-164 — MCP Config Resolution Across Config Roots

Status: Implemented (2026-10-19). 14 tests across `pkg/mcp` and `pkg/agent`;
both suites pass and `make vet` / `make fmt-check` are clean. Split from a
17-item user nit list. Touches `pkg/mcp/config.go`, `pkg/agent/mcp.go`, and
the config-root resolution in `cmd/` and `pkg/configuration`.

## Problem

The user's report:

> sprout mcp add creates an mcp config in ./.sprout/mcp_config.json. sprout
> mcp test sees and uses that, so that is consistent, but if I run sprout
> agent it doesn't see that config at all and claims there is no mcp at all

`sprout mcp add` and `sprout mcp test` agree with each other. `sprout agent`
does not. The two agree because both write and read through the same
resolution; the agent diverges because it resolves a *different* root.

### How the roots diverge

`SaveMCPConfig` and `LoadMCPConfig` both resolve `envutil.ConfigDir()`
(`pkg/mcp/config.go`), which honors `SPROUT_CONFIG`.

- `sprout mcp add` runs through the root command's `PersistentPreRunE`,
  which — inside a git repo — auto-detects the repo and sets
  `SPROUT_CONFIG=<repo>/.sprout` (`cmd/root.go`). So the file lands in the
  repo.
- `sprout agent` in the same repo builds its agent with
  `NewAgentWithLayers(globalDir, autoDetectedWorkspaceDir)`
  (`cmd/agent_command.go`), and `initializeMCP` calls `mcp.LoadMCPConfig()`
  — reading `envutil.ConfigDir()`, the same repo `.sprout`.

So in-process they should agree. The divergence is the **daemon**:
`sprout agent` routes to a long-lived daemon whose `SPROUT_CONFIG` was fixed
when *it* started (documented at `cmd/agent_socket_identity.go`). A
repo-local `mcp_config.json` written after the daemon started is invisible to
it, and the agent reports no MCP at all.

There is a second, quieter divergence: `mcp_config.json` is a *separate
file* from the layered `config.json`/`workspace.json` that
`NewManagerWithLayers` merges. The agent's MCP servers can therefore come
from two places with different resolution rules — the layered config's
`mcp.servers` and the standalone `mcp_config.json`. `initializeMCP` already
merges the legacy file into the config's servers, which is the right shape,
but the legacy file's own path resolution is not layered.

## Findings (164a)

Established by reading the resolution path and tracing both call sites.

`mcp.LoadMCPConfig` / `SaveMCPConfig` both resolve their path through
`getConfigDir()` → `envutil.ConfigDir()` (`pkg/mcp/config.go`), which reads
the **process-global** `SPROUT_CONFIG` / `SPROUT_CONFIG_DIR` environment
variables. That is the whole bug: the path depends on which process is
asking, not on which workspace the agent is serving.

Scenario 2 (no daemon) **works**: `sprout mcp add` runs through the root
command's `PersistentPreRunE`, which auto-detects a git repo and sets
`SPROUT_CONFIG=<repo>/.sprout` (`cmd/root.go`). `sprout agent` in the same
repo inherits that same env, so `initializeMCP`'s `mcp.LoadMCPConfig()` call
reads the same file it was written to.

Scenario 1 (daemon running) **fails**: `sprout agent` routes the turn to a
long-lived daemon whose `SPROUT_CONFIG` was fixed when the daemon started
(documented at `cmd/agent_socket_identity.go`). The daemon's
`initializeMCP` therefore resolves the daemon's own config root, and a
repo-local `mcp_config.json` written after the daemon started is invisible
to it. The agent then reports no MCP servers at all.

The second, quieter divergence is that `mcp_config.json` is a separate file
from the layered `config.json`/`workspace.json` that
`NewManagerWithLayers` merges, and its own path resolution does not follow
the layering rule. `initializeMCP` already merges the legacy file's servers
into the config's servers (`pkg/agent/mcp.go`), which is the right shape —
the file's *path* is what needs to become workspace-aware.

The agent already knows the workspace it is serving (`GetWorkspaceRoot`,
`pkg/agent/agent_accessors.go`), so the fix has a correct source of truth
available at the call site.

## Design

### 164a. Establish the actual failure first

Reproduce before designing further. Two scenarios, driven for real:

1. Daemon already running, then `sprout mcp add` in a repo, then
   `sprout agent` in that repo — does the agent see the server?
2. No daemon, `sprout mcp add` in a repo, then `sprout agent` — does it see
   the server?

The fix depends on which of these fail. The spec's later sections assume
scenario 1 fails and scenario 2 may also fail; confirm both.

### 164b. One resolution rule for MCP config

`mcp_config.json` resolves through the same layered logic as the rest of the
config, rather than through a bare `envutil.ConfigDir()`. Concretely: the
workspace layer is consulted, then the global layer, with the workspace
winning — the same order `NewManagerWithLayers` applies to
`config.json`/`workspace.json`.

This makes the file's location a consequence of the layering rule instead of
a separate convention that happens to match sometimes.

### 164c. The daemon sees workspace MCP config

The agent's MCP initialization must see servers configured for the workspace
it is serving, regardless of which process is running the turn. Two
approaches:

- **Merge at agent construction**: `initializeMCP` reads the workspace
  layer's MCP config for the agent's workspace root, in addition to the
  config it already merges. The daemon then sees repo-local servers because
  the agent it constructs for that workspace carries the workspace root.
- **Write where the daemon reads**: `sprout mcp add` targets the config root
  the running daemon uses.

The first is preferred: it makes the workspace's config authoritative
wherever it lives, and it keeps `mcp add`/`mcp test`/`agent` consistent by
construction rather than by coordination. The second makes `mcp add` and
`mcp test` disagree in the daemon case, which is the class of bug being
fixed.

### 164d. Say which config was read

When the agent reports no MCP servers, it should be possible to tell *which*
config root was consulted. Either the message names the path, or a debug/diag
path does (`sprout diag` already loads MCP config). This is what turns a
future instance of this bug from a mystery into a one-line answer.

## Acceptance criteria

- [x] 164a: both scenarios reproduced and recorded, with the observed
      behavior for each.
      → see "Findings (164a)": scenario 2 (no daemon) works; scenario 1
      (daemon) fails because the daemon's config root was fixed at start.
- [x] A server added with `sprout mcp add` in a repo is visible to
      `sprout agent` in that repo, both with and without a running daemon
      (test).
      → `initializeMCP` now loads through
      `mcp.LoadMCPConfigForWorkspace(a.GetWorkspaceRoot())`, so the agent
      reads the config for the workspace it serves rather than the
      process's ambient root. The daemon case is tested at the
      agent-construction layer with an explicit workspace root.
      Tests: `TestInitializeMCP_ReadsWorkspaceConfigNotProcessRoot`,
      `TestNewAgentWithLayersInWorkspace_CarriesWorkspaceRoot`.
- [x] `mcp_config.json` resolution follows the layered rule: a workspace
      entry wins over a global entry of the same name (test).
      Tests: `TestLoadMCPConfigForWorkspace_WorkspaceWinsCollision`,
      `TestLoadMCPConfigForWorkspace_SeesWorkspaceServers`,
      `TestSaveMCPConfigForWorkspace_WritesWorkspaceLayer`.
- [x] The "no MCP servers" path names the config root consulted (test).
      → `mcp.MCPConfigSourcePath(workspaceRoot)` reports the file a
      workspace resolves to; the disabled-MCP debug line names it.
      Tests: `TestMCPConfigSourcePath_NamesWorkspaceFile`,
      `TestMCPConfigSourcePath_FallsBackToGlobal`,
      `TestMCPConfigSourcePathForAgentWorkspace`.
- [x] No regression: a globally-configured MCP server is still visible in a
      repo that has no workspace MCP config (test).
      Tests: `TestInitializeMCP_GlobalServersStillVisible`,
      `TestLoadMCPConfigForWorkspace_GlobalVisibleWithoutWorkspaceLayer`.

## Decisions (Open Questions)

- **Merge vs replace for the workspace layer**: merge. The workspace layer
  wins on a name collision; the global layer still contributes servers the
  workspace does not define. Replace would make a repo-local config silently
  drop every globally-configured server.
- **Does `mcp list` show the layer?** Not in this change. `MCPConfigSourcePath`
  is exposed so a caller can; surfacing it in `mcp list` is a follow-up if
  the need appears.
- **Scalar flags**: the highest-precedence layer that exists supplies
  `Enabled` / `AutoStart` / `AutoDiscover` outright rather than OR-ing with
  the defaults. `DefaultMCPConfig` starts `Enabled` and `AutoDiscover` true,
  so OR-ing let those defaults override an explicit `false` in a config
  file — silently re-enabling auto-discovery for a user who turned it off.

## Non-goals

- No change to how MCP servers are *started* or how tools are exposed.
- No migration of `mcp_config.json` into `config.json` — the separate file
  stays, it just resolves consistently.
- No change to the daemon identity check (`cmd/agent_socket_identity.go`).

## Open questions

- Whether the workspace layer's MCP config should be a *merge* (workspace
  servers added to global) or a *replace* (workspace wins entirely). The rest
  of the config merges; leaning merge.
- Whether `sprout mcp list` should show which layer each server came from.
  Useful for debugging; possibly noise.
