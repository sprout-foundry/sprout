# SP-022: Workspace Management & Project Detection

**Status:** ✅ Implemented (`WorkspacePicker` + `WorkspacePane` + `LocationSwitcher` + `WorkspaceBar`)

Sprout was originally single-workspace (the daemon served whatever directory
it was launched from). Multi-project users had to run multiple daemons, one
per repo, and switch by killing and restarting. SP-022 added first-class
workspace switching: a sidebar `WorkspacePicker` lets the user browse and
switch active workspaces; `WorkspacePane` shows the active workspace's state
(current branch, recent files, last-used agent); `LocationSwitcher` is the
WebUI header-level switcher; `WorkspaceBar` is the persistent indicator. The
daemon hot-reloads when the workspace changes and re-roots the agent's CWD.
Under the hood, `pkg/agent_tools/workspace_sync.go` keeps the file-watcher
state coherent across workspace changes (no dangling watchers, no orphan
embeds), and a workspace heartbeat verifies the directory is reachable before
serving it.

## Key decisions

- **Hot-swap, not restart.** Switching workspaces does not restart the
  daemon. The agent's CWD changes; the watcher tree re-roots.
- **One active workspace per chat** to keep the agent's mental model clean.
  Multi-workspace queries are an explicit "compare workspace A and B" tool,
  not a default capability.
- **Workspace heartbeat before serving.** If the workspace directory is
  unreachable (deleted, network share unmounted), the daemon refuses to
  serve rather than crashing on first file access.
- **Embeddings are workspace-scoped** — switching workspaces doesn't drag
  the previous workspace's embedding index along. Each workspace has its
  own `embedding_index` directory under `~/.sprout/workspaces/<id>/`.
- **UI components live in webui**, not `@sprout/ui` — they're
  sprout-application-specific (workspace picker needs daemon access),
  not general design-system primitives.

## Artifacts

- code: `webui/src/components/WorkspacePicker.tsx`, `LocationSwitcher.tsx`
- code: `pkg/agent_tools/workspace_sync.go`, `workspace_heartbeat.go`
- code: `pkg/webui/workspace_api.go` — workspace switch REST endpoints
- tests: `pkg/agent_tools/workspace_sync_test.go`, `workspace_heartbeat_test.go`
- companion: SP-046 (browser-primary sync model) — pending

Full specification archived — see git history for original content.