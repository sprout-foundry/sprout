# SP-119: Workspace-aware directory resolution in daemon-mode tools

> ✅ **Implemented** — 3 phases shipped 2026-07-15. Commits: `6608ecf3`
> (`automate.DirIn(workspaceDir)` helper + 4 call-site migrations across
> `pkg/agent/tool_handlers_automate.go` and
> `pkg/agent_tools/list_automate_workflows_handler.go`), `aa2d05a9`
> (TODO.md sync). Tests: `pkg/automate/discovery_test.go::TestDirIn`,
> `pkg/agent_tools/list_automate_workflows_handler_test.go`.

_Discovered 2026-07-14 while debugging a `run_automate` failure in a daemon-served
workspace._

## Why

Multiple tool handlers in the agent tool layer call `os.Getwd()` (or `automate.Dir()`,
which itself calls `os.Getwd()`) to resolve workspace-relative paths. In daemon
mode (`SPROUT_SERVICE=1`, `SPROUT_DAEMON=1`), the process CWD is the daemon root
(`/Users/alanp`), not the workspace. As a result, tool handlers in a daemon process
operate against the wrong directory.

Confirmed broken:

1. **`automate.Dir()`** at `pkg/automate/discovery.go:31-35` returns
   `os.Getwd() + "/automate"`. The CLI caller (`cmd/automate.go:164`) works because
   the user's shell CWD is the workspace. The agent-tool caller
   (`pkg/agent/tool_handlers_automate.go:126, 313, 371`) does NOT work: it runs
   inside the daemon process where `os.Getwd()` is the daemon root.

   Symptom: `run_automate todo-loop` from chat in a daemon-served workspace
   returns "no automate/ directory found." The WebUI HTTP API path
   (`pkg/webui/automations_api.go:71, 382, 401`) already does the right thing —
   it uses `ws.getAutomateDir(r)` which is workspace-rooted via
   `getWorkspaceRootForRequest(r)` (`pkg/webui/client_context_accessors.go:103-114`).
   Only the chat-invoked tool path is broken.

2. **Inconsistent with existing convention.** The codebase already has a
   blessed pattern at `pkg/filesystem/context.go:WithWorkspaceRoot(ctx, root)`
   + `WorkspaceRootFromContext(ctx)` which 14+ tool handlers use. And
   `pkg/agent/tool_handlers_automate.go:115-125` defines `SproutDirFromContext(ctx)`
   for the same purpose. Both are unused by the discovery path.

3. **Potential sibling issues.** While auditing, 25+ other `os.Getwd()` callsites
   were enumerated (`pkg/agent/persistence.go` × 7 for session scoping,
   `pkg/agent/skills.go:141` for skill paths, `pkg/agent_tools/shell_native.go`
   × 3 for shell cwd, etc.). These are all symptoms of the same architectural
   ambiguity — process CWD vs workspace CWD — but each one is a different fix.
   This spec covers the automate discovery bug; the broader audit is the
   surface area for follow-up work in SP-091.

## What to build

### Phase 1: `automate.DirIn(dir string) string` (10 lines)

In `pkg/automate/discovery.go`, add a workspace-aware resolver:

```go
// DirIn returns the automate directory inside the given workspace directory.
//
// Returns the CWD-based Dir() when workspaceDir is empty (or
// whitespace-only). In production, callers always pass a non-empty
// workspaceDir (a.workspaceRoot is populated by every Agent constructor
// via either NewAgentWithLayersInWorkspace or a Getwd() fallback).
// The empty-string fall-through exists primarily to keep DirIn
// naturally testable in isolation and to preserve a graceful
// fallback for callers that haven't yet been migrated from Dir()
// to DirIn — not as a CLI-vs-daemon dispatch path.
func DirIn(workspaceDir string) string {
    if strings.TrimSpace(workspaceDir) == "" {
        return Dir()
    }
    return filepath.Join(workspaceDir, "automate")
}
```

Companion test (`pkg/automate/discovery_test.go::TestDirIn`):

```go
func TestDirIn(t *testing.T) {
    t.Run("empty workspace falls back to cwd", func(t *testing.T) {
        got := DirIn("")
        want := Dir()
        if got != want { t.Errorf("DirIn(%q) = %q, want %q", "", got, want) }
    })
    t.Run("explicit workspace joins automate", func(t *testing.T) {
        got := DirIn("/tmp/foo")
        want := filepath.Join("/tmp/foo", "automate")
        if got != want { t.Errorf("DirIn(%q) = %q, want %q", "/tmp/foo", got, want) }
    })
    t.Run("whitespace-only workspace falls back", func(t *testing.T) {
        got := DirIn("   ")
        want := Dir()
        if got != want { t.Errorf("DirIn(%q) = %q, want %q", "   ", got, want) }
    })
}
```

### Phase 2: Wire agent-tool callers to `DirIn` (3 single-line edits)

In `pkg/agent/tool_handlers_automate.go`:

| Line | Current | After |
|------|---------|-------|
| 126  | `dir := automate.Dir()` in `handleRunAutomate` | `dir := automate.DirIn(a.GetWorkspaceRoot())` |
| 313  | `dir := automate.Dir()` in `handleListAutomateWorkflows` | `dir := automate.DirIn(a.GetWorkspaceRoot())` |
| 371  | `return WorkflowRequiresApprovalIn(automate.Dir(), ...)` | `return WorkflowRequiresApprovalIn(automate.DirIn(a.GetWorkspaceRoot()), ...)` |

`a.GetWorkspaceRoot()` returns `a.workspaceRoot` (set by
`NewAgentWithLayersInWorkspace` at `pkg/agent/agent_creation.go:318`).
It does NOT fall back to `os.Getwd()` — it returns `""` when unset.

**Behavioral note (caught during review):** these three sites use
`a.GetWorkspaceRoot()`, which returns the *workspace root* only —
not the agent's effective CWD after shell `cd`. Phase 3 below uses
`env.WorkspaceRoot`, which is populated from `agent.effectiveCwd()`
(`pkg/agent/tool_security.go:270`,
`pkg/agent/tool_definitions_handler.go:172`) and does track shell
`cd` via `shellCwdTracker` (`pkg/agent/agent_getters.go:284`).

Concretely: if a user `cd subdir` in chat and then invokes
`list_automate_workflows`, the registry handler (Phase 3) walks
`subdir/automate/`. If they then invoke `run_automate` via the
legacy tool path (Phase 2 sites), the legacy handler walks
`<workspace>/automate/` — *not* `subdir/automate/`.

This asymmetry is correct for `run_automate` (workflows are a
workspace-level concern), but worth flagging if we ever want
subdirectory-aware workflow discovery. Not unified in this spec to
keep the fix tightly scoped. Future work if desired.

### Phase 3: Wire the agent_tools registry handler

The interface-based registry path for `list_automate_workflows` lives
at `pkg/agent_tools/list_automate_workflows_handler.go:45` and is
also chat-invokable through the new tool registry
(`pkg/agent_tools/all.go:84`). Same fix, different accessor:

| Line | Current | After |
|------|---------|-------|
| 45   | `dir := automate.Dir()` | `dir := automate.DirIn(env.WorkspaceRoot)` |

`ToolEnv.WorkspaceRoot` is populated by the agent at dispatch time
from `agent.effectiveCwd()` (`pkg/agent/tool_security.go:270`,
`pkg/agent/tool_definitions_handler.go:172`). Empty string falls back to
`Dir()` (CWD-based). Update the handler's godoc comment
(lines 12-20 in the current file) to reflect the workspace-aware
behavior.

### Phase 4: Tests + verification

Add unit coverage:

- `pkg/automate/discovery_test.go::TestDirIn` — covers empty,
  whitespace-only, absolute, relative `workspaceDir` inputs
  (4 sub-tests, table-friendly but readable as separate `t.Run`
  blocks per existing convention in this package).
- `pkg/agent_tools/list_automate_workflows_handler_test.go` (new) —
  covers (1) `env.WorkspaceRoot` set to a workspace with
  `automate/foo.json` (workflow discovered), (2) empty workspace
  (empty-state hint), (3) `env.WorkspaceRoot = ""` (falls back to
  CWD via `automate.Dir()`).

**Test isolation requirement:** the third case (3) `os.Chdir`s
into a temp dir to exercise the CWD-fallback path. **`os.Chdir`
mutates process-global state, so that test MUST NOT call
`t.Parallel()`.** If it did, parallel runs of all three tests
would race on `Getwd()`/`Chdir` — one test's `chdir` would
overwrite another's CWD mid-flight, producing flaky CI failures.
Codebase has prior art for this at
`pkg/agent_tools/zero_coverage_test.go:275` and
`pkg/agent_tools/background_process_signal_unix_test.go:27, 52`;
the test should explicitly comment *why* it omits `t.Parallel()`.

## Why this scope and not broader

The same "CWD vs workspace" surface affects:

- `pkg/agent/persistence.go` × 7 (session scoping).
- `pkg/agent/skills.go:141` (`resolveSkillPath`).
- `pkg/agent_tools/shell_native.go` × 3 (shell CWD).
- **`pkg/agent_tools/background_process.go:171`** —
  `BackgroundProcessManager.Start` falls back to `os.Getwd()` when
  the caller passes `dir == ""`. Same bug class as
  `shell_native.go` but in the detached-process path: in daemon
  mode, a backgrounded shell starts in the daemon root. Closest
  sibling to the SP-119 fix; intentionally out of scope here, but
  the most likely next candidate if we expand the spec.
- `pkg/agent_commands/shell.go:269`, `pkg/agent_commands/transcript.go:85`,
  `pkg/agent_commands/review_context.go:240`.
- `pkg/configuration/config_skills.go:177` (skills dir).
- `pkg/webcontent/webcontent_cache.go:33`.
- `pkg/agent/workspace_sync.go:316`, `pkg/agent/change_tracking.go:660`,
  `pkg/agent/context_discovery.go:46`, `pkg/agent/session_info.go:16`,
  `pkg/agent/transcript_snapshot.go:121`, `pkg/agent/agent_history.go:192`,
  `pkg/agent/conversation.go:406`, `pkg/agent/workflow_runner.go:200`,
  `pkg/agent/resource_capture.go:52,117`, `pkg/agent/proactive_context.go:466`.
- `pkg/agent_tools/security_classifier.go:773`,
  `pkg/agent_tools/vision_utils.go:80`,
  `pkg/agent_tools/search_files_handler.go` (comment block).

Each of these is potentially broken in daemon mode. But each has different
context-availability:

- Tool handlers with `ctx context.Context`: switch to
  `filesystem.WorkspaceRootFromContext(ctx)`. Already wired 14+ places.
- `*Agent` method calls: switch to `a.GetWorkspaceRoot()` — already on the
  struct, just doesn't read it.
- Standalone package functions with no agent or context: require either a
  context injection at every call site, OR a `WorkspaceRoot` env-var fallback
  that the daemon populates from the active workspace.

These each deserve a separate decision. SP-119 fixes the most acute one
(`run_automate` from chat) and lands the discoverable helper so the rest of
the surface can be migrated incrementally without future work needing to
guess whether the daemon's CWD is "right" or "wrong."

## Acceptance

- `go build ./...` clean.
- `go test ./pkg/automate/...` includes the new `TestDirIn` cases.
- `go test ./pkg/agent_tools/... -run TestListAutomateWorkflowsHandler` passes.
- `go test -race ./pkg/agent/...` passes. No regressions in existing tool
  tests (the discovery directory is the only change).
- Manual repro 1: in a workspace served by `sprout service`, an
  agent-tool `run_automate todo-loop` invocation from chat finds
  `automate/todo-loop.json` in the workspace rather than reporting
  "no automate/ directory found."
- Manual repro 2: in a workspace served by `sprout service`, a chat
  invocation of `list_automate_workflows` returns the workflows
  under the workspace's `automate/` directory.
- Manual cross-check: `sprout automate list` from the workspace shell
  still works (CLI path is unchanged; falls back to `Dir()` which is
  correct when shell CWD is the workspace).

## Effort

~0.4 day. ~10 lines new code in `pkg/automate/discovery.go` + 4
single-line call-site updates in `pkg/agent/tool_handlers_automate.go`
+ 1 single-line call-site update in
`pkg/agent_tools/list_automate_workflows_handler.go` + 1 unit test
file (~140 lines) + 1 small clarification to the spec acceptance.

## Out of scope

- Migrating the other 25+ `os.Getwd()` callsites. Each is a separate fix.
- Changing `currentWorkspaceRoot()` to no longer fall back to `os.Getwd()`
  (might break tests). Future work.
- Adding a daemon-level `WORKSPACE_ROOT` env var injection. Could be a
  follow-up spec if the migration proves too invasive.
- **`WorkflowRequiresApproval` at
  `pkg/agent/tool_handlers_automate.go:307-308`** — verified zero
  callers in production (via `grep -rn "WorkflowRequiresApproval("
  --include="*.go" .`). CWD-based legacy CLI wrapper, not migrated
  to `DirIn` by this spec because nothing calls it. Candidate for
  deletion in a separate cleanup PR (no behavior change; one less
  public surface).
