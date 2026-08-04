# SP-133 — Config / State / Cache / Secret Separation

## Problem

`~/.sprout` is a single directory holding four categories of data with four
different lifecycles, portability requirements, and blast radii:

| Category | Examples in `~/.sprout` today |
|---|---|
| **Config** (user-authored, portable, shareable) | `config.json`, `mcp_config.json`, `providers/`, `roles/` |
| **State** (machine-generated, per-install, not portable) | `changes/`, `revisions/`, `sessions/`, `runlogs/`, `transcripts/`, `logs/`, `instances.json`, `recent_workspaces.json`, `state.json`, `webui_host.json`, `ssh_sessions.json`, `shell_outputs/`, `workspace.log` |
| **Cache** (disposable, regenerable) | `search_cache/`, `url_cache/`, `lastRequest.json`, `lastResponse.json`, `error_request_*.json` dumps |

Several of these grow without bound — `changes/`, `url_cache/`, `search_cache/`,
and the `error_request_*.json` dumps have no eviction policy, so on a
long-running install the directory is dominated by regenerable data sitting
beside the credential files.
| **Secrets** (sensitive, never synced/logged/backed-up casually) | `api_keys.json`, `key.age`, `keyring_providers.json`, `api_keys.mode`, `backend.mode` |

Four consequences follow:

1. **`~/.sprout` doubles as a workspace config directory.** `ConfigDirName` is
   `.sprout`, and `$HOME` is a directory — so when the workspace root is home,
   the user-level state directory *is* the workspace config directory. This
   caused the SP-132-adjacent incident where a global `embedding_index.enabled`
   was read as a per-workspace opt-in and the installed service began indexing
   the user's entire home directory at startup, with no client connected.

2. **Two directories both claim to be "global config."**
   `configuration.GetConfigDir()` resolves to `~/.config/sprout` (XDG), while
   `cmd/diag.go:37` prints `~/.sprout/config.json` as `globalConfigPath`. Both
   files can exist simultaneously, both carrying an `embedding_index` block,
   and nothing reconciles them.

3. **Dual-role directories.** `changes/` and `revisions/` resolve from
   `configDir` in `pkg/history/data_access.go:107` and from
   `<workspace>/.sprout/...` in `data_access.go:607`, `cmd/history.go:174,206`,
   and `pkg/training/file_changes.go:169`. At home these are the same path.

4. **Secrets sit beside disposable junk.** `key.age` and `api_keys.json` share a
   directory with debug dumps, request/response captures, and logs. "Never sync
   this, never ship this in a support bundle" is not enforceable by path.

## Prior art in this repo

Two related fixes have landed and this spec builds on them — it does not repeat
them:

- **SP-132-adjacent (workspace config filename split).** The workspace layer is
  now `<ws>/.sprout/workspace.json` (`configuration.WorkspaceConfigFileName`),
  read with a legacy `config.json` fallback that is **disabled at `$HOME`** —
  because at home the legacy file is the user's global config, not a workspace
  config. `ResolveWorkspaceConfigFile` is the single place that decides.
- **`.sprout` project-marker down-weight.** 90 → 40, below the single-marker
  threshold, because sprout creates `.sprout` in every directory it runs in.
  The home gate was simultaneously decoupled from project markers and now keys
  on consent alone.

Those two changes made the *config* collision impossible by construction. This
spec addresses the remaining three: the two-global-dirs ambiguity, the
dual-role state directories, and the secrets/cache co-location.

## Goals

1. **One home per category.** Config, state, cache, and secrets each resolve to
   a distinct root, overridable independently by env var.
2. **One directory is "global config," unambiguously.** Delete the second
   claimant; make `diag` report the real one.
3. **Every per-directory artifact has exactly one resolution rule.** No path is
   reachable from both `configDir` and `<workspace>/.sprout`.
4. **Secrets are isolated by path**, so "don't sync / don't bundle / restrict
   mode" is enforceable mechanically rather than by remembering.
5. **Layer precedence is inspectable.** A user (or an agent debugging a
   support issue) can ask which layer supplied a value and get a file path.
6. **Shared vs personal split at both scopes**, so a committed workspace config
   and a machine-local override are different files.

## Non-goals

- Backwards compatibility with the current layout. The project is early alpha;
  a clean break with a one-shot migration is preferred over a compatibility
  matrix. (Exception: **credentials**, see Phase 4.)
- Changing the layered-config *semantics* (global → workspace → session → env).
  Only the locations and the shared/personal split change.
- Multi-root workspaces or a folder-level scope. Three layers plus env is the
  right shape for this tool; see "What we are not copying".

## Target layout

```
~/.config/sprout/              $SPROUT_CONFIG_DIR → $XDG_CONFIG_HOME/sprout → ~/.config/sprout
  config.json                  user config, portable, shareable
  config.local.json            user config, machine-specific (gitignore-able, never synced)
  mcp.json                     split by concern
  providers/  roles/
  credentials/                 mode 0700
    api_keys.json  key.age  keyring_providers.json  api_keys.mode  backend.mode

~/.local/state/sprout/         $SPROUT_STATE_DIR → $XDG_STATE_HOME/sprout
  logs/  sessions/  transcripts/  runlogs/  shell_outputs/
  instances.json  recent_workspaces.json  state.json
  webui_host.json  ssh_sessions.json  workspace_consent.json  service.env

~/.local/share/sprout/         $SPROUT_DATA_DIR → $XDG_DATA_HOME/sprout
  embeddings/  models/

~/.cache/sprout/               $SPROUT_CACHE_DIR → $XDG_CACHE_HOME/sprout
  search_cache/  url_cache/  webcontent/  grammars/
  diagnostics/                 lastRequest.json, lastResponse.json, error_request_*.json

<workspace>/.sprout/
  workspace.json               committed — shared project settings
  workspace.local.json         gitignored — personal overrides
  security-policy.json         committed
  .gitignore                   shipped: *.local.json + state dirs below
  changes/  revisions/  runlogs/  bg-processes/   per-workspace state, gitignored
```

`$SPROUT_CONFIG` (the existing single override) continues to work as an alias
for `$SPROUT_CONFIG_DIR` so test isolation and `configuration.NewTestManager`
keep functioning unchanged.

## Design decisions

**XDG on macOS too, not `~/Library/Application Support/sprout`.** VS Code uses
Application Support because it is a GUI app; sprout is CLI-first and already
resolves `~/.config/sprout` through `XDG_CONFIG_HOME` in
`pkg/envutil/env.go:43`. Moving to Application Support now would be the larger
breaking change and would surprise the CLI audience.

**`changes/` and `revisions/` become workspace-local only.** They are per-repo
artifacts; users expect `rm -rf project` to take its history with it, and
keeping them out of the user dir stops `~/.sprout/changes` accumulating
snapshots from every workspace ever opened. This resolves the dual-role
ambiguity in favour of the workspace, and
`pkg/history/data_access.go:107` loses its `configDir` branch.

**Credentials get their own directory** rather than staying loose in the config
dir. Mirrors the `ant` CLI (`~/.config/anthropic/` splits `configs/` from
`credentials/`) and makes the support-bundle exclusion (`api_support_bundle.go`)
a one-line path rule instead of a filename denylist.

**Shared/personal split uses a `.local.json` sibling**, matching the convention
users already know from Claude Code (`settings.json` / `settings.local.json`)
and VS Code. Same schema, higher precedence, never committed.

**`~/.sprout` is deleted, not repurposed.** Leaving it as a state dir would
preserve the `$HOME`-is-a-workspace aliasing: `<ws>/.sprout` at `$HOME` would
still be the user state dir. Removing it entirely is what closes the class.

## Negative-Consequence Audit

### A. Credential loss during the move

`key.age` is machine encryption material and `api_keys.json` is encrypted
against it. A bug that moves one without the other, or that half-writes either,
is unrecoverable — the user must re-enter every provider key.

**Mitigation**: credentials move **last, in their own phase, behind their own
marker file** (`credentials_migrated`), copying rather than renaming, verifying
a successful decrypt of at least one key before removing the source. Precedent
exists: `~/.sprout/.config_api_keys_migrated` from a prior migration.

### B. Test isolation depends on `SPROUT_CONFIG`

`configuration.NewTestManager` sets `SPROUT_CONFIG` + `HOME` and asserts the
real config file is untouched. `pkg/webui`'s `TestMain` similarly redirects
recent-workspaces and session state.

**Risk**: new roots (`SPROUT_STATE_DIR`, `SPROUT_CACHE_DIR`,
`SPROUT_DATA_DIR`) that ignore the test env would write to the developer's
real directories. This is not hypothetical — it is the failure mode that
`recent_workspaces.json` hit before `initRecentWorkspaces` stopped
re-deriving its path on every server construction.

**Mitigation**: every new resolver honours its env override *and* falls back
through `HOME`, so `t.Setenv("HOME", tmp)` isolates all four. `NewTestManager`
sets all four roots. Extend the existing leak detectors in
`pkg/webui/main_test.go` to cover the state and cache roots.

### C. Service plists bake `SPROUT_DAEMON_ROOT=$HOME`

`pkg/service/darwin.go:56` writes `SPROUT_DAEMON_ROOT` and `WorkingDirectory`
into the launchd plist at install time. An installed service carries the old
assumption until reinstalled.

**Mitigation**: `daemonRoot` stays `$HOME` — it is the *workspace browse
boundary*, which is a separate concept from the config/state roots and does not
move. No plist change required, no reinstall needed. Call this out in the spec
so a future reader does not "helpfully" repoint it.

### D. sprout-foundry consumes the daemon

`../sprout-foundry` pins a `SPROUT_VERSION` and runs the binary in Docker
images, and `docs/INTEGRATION_CONTRACT_ANALYSIS.md` documents the contract.
Docker images that mount or seed `~/.sprout` would silently get an empty config.

**Mitigation**: audit `../sprout-foundry` for `.sprout` path references before
Phase 1 lands; bump `VERSION`, update `COMPATIBILITY.md`, and run
`make test-integration` as part of the phase's exit criteria (per AGENTS.md).

### E. WASM build has no home directory

`pkg/webui` and others carry `//go:build !js` guards; `envutil.GetConfigDir`
has a Termux fallback path.

**Mitigation**: new resolvers live behind the same build tags as their
consumers, and each has an explicit fallback chain ending in a
deterministic path rather than an error. No resolver may panic on a missing
`$HOME`.

### F. Support bundle and diagnostics

`api_support_bundle.go` walks the config dir. After the split it would collect
the config dir *including* `credentials/` unless updated.

**Mitigation**: Phase 4 adds an explicit `credentials/` exclusion with a test
asserting no bundle entry has that path prefix.

### G. Cache growth is currently invisible

`url_cache/` and the `error_request_*.json` dumps accumulate with no eviction.
Moving them to `~/.cache/sprout` makes them *look* disposable without making
them *be* bounded.

**Mitigation**: out of scope for this spec, but Phase 3 adds a TODO and a
`sprout diag --disk` line item so the growth is at least observable.

## Implementation Phases

### Phase 1 — Root resolvers + config/secrets split (`pkg/envutil`, `pkg/configuration`)

- Add `envutil.ConfigDir() / StateDir() / DataDir() / CacheDir()`, each honouring
  `$SPROUT_{CONFIG,STATE,DATA,CACHE}_DIR` → XDG → `$HOME`-relative default.
  `$SPROUT_CONFIG` remains an alias for the config root.
- Add `configuration.CredentialsDir()` = `<config>/credentials`, created 0700.
- Add `config.local.json` as a user-scope layer between global and workspace.
- Delete the `~/.sprout` claimant: `cmd/diag.go:37` reports `ConfigDir()`.
- **Exit**: `go test ./pkg/configuration/ ./pkg/envutil/`, `make build-all`.

### Phase 2 — Move state and data off `~/.sprout`

Repoint, in order: `setupDaemonLogging` (logs), `recent_workspaces.go`,
`workspace_gate.go` (consent), `instances_api.go:496`, `pkg/mcp/config.go:90`,
`pkg/search/session_index.go:66`, `pkg/webui/cost_tracking.go:62`,
`cmd/audit.go:25`, `pkg/webcontent/webcontent_cache.go:24`.

- `pkg/embedding/embedding_models.go:202` and `agent_creation.go:249` →
  `DataDir()/embeddings`.
- **Exit**: no non-test occurrence of `filepath.Join(home, ".sprout")` remains;
  add a CI grep guard asserting that.

### Phase 3 — Resolve the dual-role directories

- `pkg/history/data_access.go:107` loses its `configDir` branch;
  `changes/`/`revisions/`/`runlogs/` resolve from the workspace only.
- Ship a `<ws>/.sprout/.gitignore` on workspace-dir creation covering
  `*.local.json`, `changes/`, `revisions/`, `runlogs/`, `bg-processes/`.
- Diagnostics dumps (`lastRequest.json`, `error_request_*.json`) → `CacheDir()/diagnostics`.
- **Exit**: `go test ./pkg/history/ ./cmd/`; manual `sprout history` in a repo.

### Phase 4 — Credentials (isolated, last)

- Move `api_keys.json`, `key.age`, `keyring_providers.json`, `*.mode` to
  `CredentialsDir()`, copy-verify-then-remove behind a marker file.
- Exclude `credentials/` from the support bundle.
- **Exit**: a test that round-trips an encrypted key through the new location,
  plus a support-bundle test asserting exclusion.

### Phase 5 — `sprout config get <key> --show-origin`

Print the winning layer and the absolute file that supplied it, plus the
shadowed layers. Covers the "which of the four layers won?" question that is
currently unanswerable, and the env-var-silently-overrides-config trap.

### Phase 6 — One-shot migration

Detect a legacy `~/.sprout`, move each category to its new root, write
`migrated_sp133` into the state dir, and print a one-line summary. Leave the
legacy directory in place (empty of moved content) rather than deleting it, so
a failed migration is diagnosable.

## Testing

### Go tests

- `envutil`: each root honours its env override, falls back through XDG, then
  `$HOME`; none panics when `$HOME` is unset.
- `configuration`: `config.local.json` overrides `config.json` and is
  overridden by the workspace layer; `CredentialsDir()` is 0700.
- `history`: `changes/`/`revisions/` resolve from the workspace and never from
  the config dir, including when the workspace is `$HOME`.
- Migration: a fixture legacy `~/.sprout` lands each file in the right root and
  is idempotent on a second run.
- Leak detectors in `pkg/webui/main_test.go` extended to the state root.

### Manual verification

1. Fresh install → `~/.sprout` never created.
2. Upgrade over a populated legacy dir → keys still decrypt, recent workspaces
   preserved, `sprout diag` reports the new paths.
3. `sprout config get embedding_index.enabled --show-origin` names the layer.
4. Service-mode daemon idle in `$HOME` with nothing connected → no indexing, no
   TCC prompts (the SP-132 regression check).
5. `cd ../sprout-foundry && make test-integration` green.

## Out of scope

- Cache eviction / size bounds for `url_cache/` and the diagnostics dumps
  (observability only in this spec — see audit item G).
- Moving `daemonRoot` off `$HOME`; it is the workspace browse boundary, not a
  config root (audit item C).
- Multi-root workspaces, a folder-level config scope, or profile support.
- Migrating existing committed `<ws>/.sprout/config.json` files — the legacy
  read-fallback already handles those and is deliberately retained.

## What we are not copying

- **VS Code's five-level chain** (default/user/remote/workspace/folder). Folder
  level exists to serve multi-root workspaces, which sprout does not have.
- **A settings file at the home root outside its own directory** (Claude Code's
  `~/.claude.json`). That is legacy shape, not design — everything belongs
  under a category root.
