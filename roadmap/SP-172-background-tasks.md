# SP-172 — Background Tasks: `/task` Fan-Out into Named Worktrees

> **Status (2026-11-01):** In progress — 172a (worktree + branch naming),
> 172b (task agent run), 172c (`/task` command surface), 172d (`/tasks`
> picker), 172e (`/task merge`). The persistent task pane (172f) is
> explicitly out of scope; see "Non-goals".

## Problem

Working on several aspects of one project at once means either stashing and
switching, or running several checkouts by hand. Sprout already has the
pieces — git worktrees, a background task registry, a durable completion
notification that wakes an idle primary, and an interactive picker — but
nothing connects them for the common case:

> "I'm on main. I want three of these going at once, and I want to be told
> when each is done."

Today the closest thing is `run_subagent` with `isolation: "worktree"`. That
path is built for a different job and does not serve this one:

- `createSubagent` gives the subagent a minimal prompt
  (`subagent_creation.go`), not the primary's system prompt or the
  workspace's `AGENTS.md` conventions. A delegated feature task does not
  know the repo's build commands or conventions.
- Subagent budgets are persona-sized (`effectiveBudget`); a real feature
  task hits the wrap-up grace and returns partial work.
- `finishIsolatedRun` brings changes back as a **patch applied to the
  primary's working tree**, staged and uncommitted. When the primary has
  moved (which is the point of working in parallel) the patch is refused
  and parked.
- `createIsolatedWorkspace` is `--detach` at HEAD plus a seeded baseline
  commit: no branch, no name, and it seeds the primary's uncommitted state.
- Worktrees live under the state dir, not the workspace.

So the ask is a first-class task: a named branch, a worktree under
`.sprout/`, a full agent (not a subagent) running the task to completion,
and a notification that tells the primary what to merge.

## Goals

1. `/task <description>` starts a background task in one command.
2. The branch name follows the workspace's `AGENTS.md` conventions, chosen
   by a single LLM call against the default model.
3. The worktree lives at `<workspace>/.sprout/tasks/<slug>` on branch
   `<type>/<slug>`, created from the current branch.
4. The task runs a full agent (primary system prompt + conventions, primary
   budget) and commits its work on the branch when it finishes.
5. Completion notifies the primary through the existing wakeup queue, with
   a merge instruction the primary can act on.
6. `/tasks` lists tasks alongside subagent background tasks, and selecting
   one shows live progress and recent output.
7. `/task merge <id>` merges the branch back.

## Non-goals

- **A persistent task pane** (a keybinding that swaps the main view for a
  task's output). The CLI has no pane/alt-screen model; building one is a
  separate subsystem with its own terminal-resync risk. `/tasks` selection
  covers the need; revisit if it proves insufficient.
- **Cross-machine tasks.** Tasks are in-process goroutines of the primary
  sprout process. They die with it.
- **Task-to-task communication.** Tasks do not see each other's branches.
- **WebUI parity.** The WebUI reads the same registry through `/tasks`;
  no dedicated UI in this spec.

## Design

### 172a. Worktree creation and branch naming

`pkg/agent/tasks_worktree.go`

- `taskBaseDir(workspaceRoot)` → `<workspace>/.sprout/tasks`. Created on
  demand; `tasks/` is added to the generated `.sprout/.gitignore` (the file
  `EnsureWorkspaceConfigDir` ships) so worktrees never show up as untracked
  files in the workspace.
- `createTaskWorktree(ctx, repoRoot, workspaceRoot, branch, slug)` runs
  `git worktree add -b <branch> <path> <base>`. Unlike
  `createIsolatedWorkspace` it does **not** seed uncommitted changes: a task
  starts from a committed base so its branch is mergeable on its own.
  It refuses when the base is detached HEAD, and it refuses when the slug's
  path already exists.
- `suggestTaskBranch(ctx, a, description)` makes exactly one LLM call
  (`Agent.GenerateResponse`, no tools) with a compact prompt: the task
  description, the conventions section of the workspace's `AGENTS.md`, and
  the existing branch names. It asks for one line: `<type>/<slug>`.
  `parseTaskBranchName` validates the reply — Conventional Commit type
  prefix, lowercase kebab slug, `git check-ref-format --branch` clean, length
  capped — and falls back to a deterministic slug when the model's reply is
  unusable. The fallback is not an error: a bad LLM reply must never block
  the task.
- `readWorkspaceConventions(workspaceRoot)` returns the `AGENTS.md` text
  (capped) or "" when absent.

### 172b. The task agent

`pkg/agent/tasks_agent.go`

`newTaskAgent(a *Agent, worktreePath string) (*Agent, error)` builds a full
agent in the worktree:

- Client, provider, model: the primary's, resolved through the primary's
  config manager.
- System prompt: the primary's base system prompt, so the task agent has the
  same conventions section and tool guidance the primary has.
- Workspace root: the worktree path. The change tracker, file tools, and
  shell cwd all root there for free.
- Budget: the primary's `maxIterations` and the task timeout, not a persona
  budget.
- Role: stamped `task` so spend is attributed to tasks, not the coder role.
- Shared managers: the primary's MCP manager and terminal manager (process
  scoped, correct to share). A **fresh** todo manager and approval manager —
  a task has no human to answer an approval prompt, so it runs with
  `SetWorkflowRun(true)` and fails fast instead of blocking.
- Event metadata: `chat_id` = task ID, so the CLI can route a task's events
  to its own output view.

### 172c. The task runner and `/task`

`pkg/agent/tasks_runner.go`, `pkg/agent_commands/task.go`

`Agent.StartTask(ctx, StartTaskOptions) (*TaskInfo, error)`:

1. Resolve the repo root and base branch; validate.
2. Name the branch (172a).
3. Create the worktree (172a).
4. Register a `task` background task in the existing registry
   (`registerBackground("task", …)`), so `/tasks`, `/tasks <id>`,
   `/tasks stop <id>` and the wakeup notification all work unchanged.
5. Goroutine: build the task agent (172b), `ProcessQuery(description)`,
   then `git add -A && git commit` in the worktree when the run finished
   cleanly and changed files. The worktree and branch are **kept** — unlike
   `finishIsolatedRun`, which removes them.
6. `finishBackground(t, status, summary)` — the existing notification path,
   with a `NotifTask` kind whose wakeup text names the branch and the merge
   command.

`/task <description>` is the command surface. `/task` with no args prints
usage. It is not safe during steer (it starts long work and mutates the
repo's worktree list).

### 172d. `/tasks` picker

`pkg/agent_commands/tasks.go` gains an interactive path: `/tasks` with no
arguments and a TTY opens a `console.SelectList` of background tasks
(subagent and task kinds alike); selecting one prints its detail. Without a
TTY it keeps printing the plain list, so scripted and WebUI use is
unchanged.

`describeRunningTask` already reports step/tool/token progress and the last
lines of output for a running subagent. A task's own run is registered as a
running subagent by the same machinery, so the detail view works without a
second progress path.

### 172e. `/task merge <id>`

Merges the task's branch into the primary's current branch with
`git merge --no-ff`, refusing when the primary's working tree is dirty or
the merge would conflict (it runs `git merge --no-commit --no-ff` first and
aborts on failure, so a conflict leaves the primary untouched). On success
the worktree is removed and the branch is left in place.

## Configuration

New settings under `tasks`:

| Key | Default | Meaning |
|---|---|---|
| `tasks.enabled` | `true` | Allow `/task`. |
| `tasks.base_branch` | `""` | Branch to create from; empty means the current branch. |
| `tasks.max_concurrent` | `3` | Refuse a new task beyond this many running. |
| `tasks.timeout_minutes` | `90` | Per-task wall clock. |

## Safety

- A task never writes to the primary's working tree. Its only shared state
  is the git object database (via the worktree) and the branch namespace.
- Starting a task requires a clean-enough repo: a detached HEAD is refused
  (there is no branch to merge back into).
- The task agent runs non-interactive (`SetWorkflowRun(true)`): no approval
  prompt can block it, and a risky operation fails instead of waiting.
- `/task merge` is the only operation that touches the primary's tree, and
  it aborts cleanly on conflict.
- Tasks die with the process. `/tasks` reports them as failed after a
  restart rather than pretending they are running.

## Tests

- Worktree creation in a temp repo: branch exists, path under
  `.sprout/tasks`, base commit correct, refuses detached HEAD, refuses an
  existing path.
- Branch naming: valid reply accepted; invalid reply falls back to the
  deterministic slug; `check-ref-format` rejects are caught.
- Task agent construction: workspace root is the worktree, role is `task`,
  workflow-run flag set, todo manager is not shared.
- Runner: a scripted task run commits on the branch and leaves the worktree;
  a failed run leaves the worktree uncommitted and reports failure.
- Notification content names the branch and the merge command.
- `/task merge`: clean merge succeeds; dirty tree refused; conflicting
  branch leaves the primary untouched.
