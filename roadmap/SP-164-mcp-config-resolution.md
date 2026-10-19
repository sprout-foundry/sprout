# SP-164 — MCP Config Resolution Across Config Roots

Status: Proposed (2026-10-19). Split from a 17-item user nit list. Touches
`pkg/mcp/config.go`, `pkg/agent/mcp.go`, and the config-root resolution in
`cmd/` and `pkg/configuration`.

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

- [ ] 164a: both scenarios reproduced and recorded, with the observed
      behavior for each.
- [ ] A server added with `sprout mcp add` in a repo is visible to
      `sprout agent` in that repo, both with and without a running daemon
      (test).
- [ ] `mcp_config.json` resolution follows the layered rule: a workspace
      entry wins over a global entry of the same name (test).
- [ ] The "no MCP servers" path names the config root consulted (test).
- [ ] No regression: a globally-configured MCP server is still visible in a
      repo that has no workspace MCP config (test).

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
