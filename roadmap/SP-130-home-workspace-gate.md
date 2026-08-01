# SP-130 — Home-Directory Workspace Gate

## Problem

When the sprout daemon runs as a system service (launchd/systemd), the plist
bakes `WorkingDirectory=$HOME` and `SPROUT_DAEMON_ROOT=$HOME`. The webui then
inherits home as the workspace root. Two compounding bugs follow:

1. **`IsProjectDirectory($HOME)` returns true** because install creates
   `~/.sprout` (weight-90 project marker in `project_detect.go`). So
   `handleAPIWorkspaceGet` sets `needs_workspace_selection = false` and the
   webui opens silently with home as the workspace — no picker.

2. **`FindProjectsInDirectory(daemonRoot=home, 2)`** is called on every
   `getWorkspace()` request when `!isProject` (it isn't, normally, but
   downstream features and the suggestion path still walk it). It
   recursively reads `~/Music`, `~/Library`, `~/Pictures` — tripping macOS
   TCC `kTCCServiceMediaLibrary` ("sprout would like to access Apple Music…").

With workspace root = home, **all paths under `~` are `PathTierWorkspace`**
(`path_tier.go:50-55`) — no approval prompts, no session-allowlist friction.
The agent's shell/file tools freely walk `~/Music`, `~/Library`, etc.

## Goals

1. **Never silently default the workspace to the home directory.** The webui
   must gate on an explicit directory selection before chat/files/editor work.
2. **Stop recursive home-directory inspection at startup / page-load.** Project
   suggestions move from the `getWorkspace` hot path to on-demand (picker open).
3. **Explicit home selection = intentional.** The user may pick home
   deliberately; consent is persisted so it doesn't re-prompt every launch.
4. **Don't regress SSH, cloud, or worktree modes.** The gate is local-daemon
   only.

## Negative-Consequence Audit

Risks identified during pre-implementation review, with mitigations:

### A. Test breakage — `ws.workspaceRoot = daemonRoot`

Three test files set `ws.workspaceRoot = daemonRoot` using a tempdir:
`api_agent_sessions_test.go:29`, `automations_api_test.go:38`,
`changes_api_test.go:52`.

**Risk**: Gate logic that flags `workspaceRoot == daemonRoot` as "home" would
break these (tempdir is not home).

**Mitigation**: The gate checks `workspaceRoot == homeDir` (actual `os.UserHomeDir()`
or `user.Current().HomeDir`), NOT `workspaceRoot == daemonRoot`. A tempdir
daemonRoot never matches home. Tests unaffected.

### B. Worktree session switches bypass `setClientWorkspaceRoot`

`chat_sessions_worktree_api.go:237` sets `ctx.WorkspaceRoot` directly (bypassing
the normal setter that would run the gate).

**Risk**: A worktree under `~/dev/repo/.git/worktrees/...` would trigger the
gate mid-session, blocking a valid worktree switch.

**Mitigation**: The gate fires on **initial workspace selection** and on
**explicit `setWorkspace` API calls**, not on internal worktree switches.
Worktree switches already validate `.git` presence and are user-initiated via
the worktree UI. Add a guard: `isHomeWorkspace()` is only checked in
`handleAPIWorkspaceGet` (read path, for the frontend gate state) and
`handleAPIWorkspaceSet` (the explicit selection path). Worktree switches go
through neither.

### C. SSH remote sessions

SSH connects pick a remote workspace path via `SSHWorkspacePickerDialog` /
`window.SPROUT_INITIAL_WORKSPACE`.

**Risk**: Gate could block a valid SSH workspace that happens to resolve to the
remote `$HOME`.

**Mitigation**: The gate checks the **local daemon's** home dir, not the remote
home. SSH workspaces have `clientCtx.SSHHostAlias != ""` — the gate skips
clients with an active SSH context. Only local-daemon workspaces are gated.

### D. Cloud mode

Cloud mode has `supportsWorkspaceSwitching = false` and a single virtual FS.

**Risk**: Gate would block all cloud sessions.

**Mitigation**: Frontend gate is gated on `supportsWorkspaceSwitching` (same as
the existing WorkspacePicker). Cloud mode is unaffected.

### E. Existing users silently running in home

Anyone whose daemon was installed before this change has been running with home
as workspace.

**Risk**: After upgrade, the webui suddenly forces a picker — confusing.

**Mitigation**: This is the **intended fix**. The picker screen explains why:
"Sprout is running in your home directory, which gives the agent access to all
your files. Select a project folder to limit its scope." One-time friction that
fixes a real security/privacy problem (TCC prompts, unscoped file access).

### F. `GetMostRecentWorkspace()` as initial root

Phase 1 proposes seeding the daemon's initial workspace from the most recent
workspace record.

**Risk**: The most recent workspace could be deleted, outside daemonRoot, or
stale.

**Mitigation**: Validate before use: `os.Stat` + `isWithinWorkspace(path,
daemonRoot)`. Fall through to "no workspace selected" (gate active) if invalid.

### G. Removing `FindProjectsInDirectory` from `getWorkspace`

The suggestion list powers the WorkspacePicker UI.

**Risk**: Removing it makes the picker show no suggestions.

**Mitigation**: Move the call to a separate lazy endpoint or invoke it only
when the picker modal opens (frontend triggers a fetch), not on every
`getWorkspace()` poll. The picker UX is unchanged; only the timing moves.

### H. CLI interactive mode parity

Interactive `sprout` launched from `~` inherits home as CWD.

**Risk**: Blocking the CLI loop in home would surprise users who `cd ~ && sprout`.

**Mitigation**: CLI path is **Phase 4 (optional)** and uses a warning + prompt,
not a hard block. The daemon/webui is the primary vector for the TCC issue;
CLI users already chose to run from home interactively.

## Implementation Phases

### Phase 1 — Backend gate (Go, `pkg/webui/`)

**1.1 Home detection helper** (`workspace_gate.go`, new file)

```go
// isHomeWorkspace reports whether the workspace root resolves to the user's
// home directory. Uses the same resolution chain as server.go's daemonRoot
// logic (SPROUT_DAEMON_ROOT → $HOME → user.Current) for consistency.
func isHomeWorkspace(workspaceRoot string) bool
```

Symlink-safe: `filepath.EvalSymlinks` on both sides before comparing.

**1.2 Consent store** (`workspace_gate.go`)

JSON at `~/.sprout/workspace_consent.json`:
```json
{"home_workspace": {"consented_at": "2026-07-31T20:00:00Z"}}
```

Functions: `loadHomeWorkspaceConsent() bool`, `recordHomeWorkspaceConsent() error`.

**1.3 `handleAPIWorkspaceGet` changes** (`api_workspace.go`)

- Add `workspace_is_home` (bool) and `home_dir` (string) to response.
- Override the project-marker check: set `needs_workspace_selection = true`
  when `isHomeWorkspace(workspaceRoot) && !hasHomeConsent()` — **even if**
  `IsProjectDirectory` returns true (the `~/.sprout` marker false positive).
- Remove `FindProjectsInDirectory(daemonRoot, 2)` from this path. Suggestions
  move to a separate endpoint or are computed only when `needs_workspace_selection`
  is true (lazy).

**1.4 `handleAPIWorkspaceSet` consent flow** (`api_workspace.go`)

Accept an optional `consent_home` field in the POST body. When the target path
resolves to home:
- If `consent_home == true`: persist consent, allow.
- If `consent_home == false` / absent: reject with a structured error
  (`code: "home_workspace_requires_consent"`) so the frontend can show the
  warning UI.

**1.5 `setClientWorkspaceRoot` defense-in-depth** (`client_context.go`)

Reject a target resolving to home unless consent is on record. Belt-and-suspenders
behind the API-layer check.

**1.6 Server startup initial workspace** (`server.go`)

In service mode, instead of `workspaceRoot = daemonRoot`:
- Try `GetMostRecentWorkspace()`; validate it exists and is within daemonRoot.
- If valid and not home (or home + consented): use it.
- Otherwise: leave workspace unset → frontend gate fires.

### Phase 2 — Frontend hard gate (`webui/src/`)

**2.1 `useWorkspace.ts`**

- Consume `workspace_is_home` / `home_dir` from backend response.
- Delete the broken `extractHomeDir` heuristic.
- Add `homeConsented` state (derived from `needs_workspace_selection` — if
  false and `workspace_is_home`, consent exists).

**2.2 Blocking gate modal** (new component, e.g. `WorkspaceGateModal.tsx`)

- Renders as a **full-screen overlay** (not a tab) when
  `needs_workspace_selection && supportsWorkspaceSwitching`.
- Chat input, file tree, editor — all unreachable until selection.
- Reuses `WorkspacePicker` for project/recent list and the
  `sprout:open-workspace-switcher` event for the browse popover.
- Explicit "Use my home directory" button with warning text:
  > "Running Sprout in your home directory gives the agent unrestricted access
  > to all your files and may trigger macOS permission prompts for protected
  > folders like Music and Photos. Select a project folder instead unless you
  > understand the risks."
- The button sends `POST /api/workspace` with `consent_home: true`.

**2.3 `WelcomeTab.tsx`**

The existing `needs_workspace_selection` → `WorkspacePickerView` path is
superseded by the modal gate for the home case. For non-home non-project
directories (e.g. an empty temp dir), keep the existing tab-based picker.

### Phase 3 — Install/startup hygiene (`pkg/service/`)

**3.1 Plist/unit comments** (`darwin.go`, `linux.go`)

Update the `SPROUT_DAEMON_ROOT` comment to reflect that it scopes **storage**
(sessions, config, browse-root) — not the workspace. The workspace is now
selected at runtime.

**3.2 `Diagnose()` stale-plist check** (`darwin.go:373`)

The warning text ("workspace browser may start in the wrong directory") is
still accurate for missing `SPROUT_DAEMON_ROOT`, but the remediation message
should note that re-installing also picks up the new gate behavior.

### Phase 4 — CLI interactive parity (optional, lower priority)

**4.1** `cmd/agent_modes.go`: when entering interactive mode and
`effectiveCwd() == home`, print a warning + prompt for confirmation (not a hard
block). Gate behind `--skip-prompt` (skip warning in non-interactive).

## Testing

### Go tests

- `workspace_gate_test.go`: `isHomeWorkspace` (home, non-home, symlinked home,
  empty), consent load/save/expire, `setClientWorkspaceRoot` home rejection
  with/without consent.
- `api_workspace_test.go`: `handleAPIWorkspaceGet` returns
  `needs_workspace_selection=true` when workspace is home (even with `.sprout`
  marker), `workspace_is_home=true`, no `FindProjectsInDirectory` call.
- `server_test.go`: service-mode initial workspace = recent workspace (valid),
  recent workspace (invalid) → empty, home + consent → allowed.
- Existing tests (`api_agent_sessions_test.go`, `automations_api_test.go`,
  `changes_api_test.go`): verify tempdir daemonRoot is NOT flagged as home.

### TypeScript tests

- `WorkspaceGateModal.test.tsx`: renders when `needs_workspace_selection &&
  supportsWorkspaceSwitching`; does not render in cloud mode; "Use home"
  button sends consent; chat/files unreachable while open.
- `useWorkspace.test.ts`: consumes `workspace_is_home`; no `extractHomeDir`.

### Manual verification

1. `sprout service install` on macOS → open webui → picker must appear, not
   home with a chat ready to go.
2. Pick a project → reload → persists (recent workspace).
3. Type `~` in workspace switcher → blocked with structured error.
4. Click "Use home directory anyway" → consent persisted → reload → no gate.
5. `log stream --predicate 'subsystem == "com.apple.TCC"'` → no media-library
   prompts during webui load (the `FindProjectsInDirectory` walk is gone).

## Out of scope

- Changing macOS TCC attribution mechanics (OS-level behavior).
- The Fly.io remote Workspaces page (`WorkspacesPage.tsx`) — unrelated.
- Enforcing the gate in cloud/WASM mode (`supportsWorkspaceSwitching = false`).
- Blocking the agent from `cd ~` mid-session (that's `PathTierSensitive` when
  CWD is outside home — already handled by `path_tier.go`).
