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

`/task show <id>` (and the bare `/task <id>` form) shows one task. The ID
shape (`bg-task-<n>`) is matched explicitly rather than "does this resolve
to a task", so a one-word description can never be swallowed as a detail
request.

The task record and the background-task registry entry share one ID, so
`/tasks stop <id>` stops a task through the existing path. Because that
path only touches the background entry, a stop also closes the task record
(`markStopped`) — otherwise the record would stay `running` forever and
`/task merge` would refuse it while the concurrency slot stayed occupied.

### 172d. `/tasks` picker

`pkg/agent_commands/tasks.go` gains an interactive path: `/tasks` with no
arguments and a TTY opens a `console.SelectList` of background tasks
(subagent, review, and task kinds alike); selecting one prints its detail.
Without a TTY it keeps printing the plain list, so scripted and WebUI use is
unchanged.

`describeRunningTask` already reports step/tool/token progress and the last
lines of output for a running subagent. A task's own run is registered as a
running subagent by the same machinery, so the detail view works without a
second progress path.

The task registry is process-wide and a record holds a full agent, so it
prunes finished records past `maxRetainedTasks` (mirroring the background
registry's cap) and never prunes a running one. Its lock guards the records'
mutable fields, not just the map: the runner goroutine writes status,
output and commit while `/tasks` reads them on another goroutine. The
concurrency limit reserves a slot under that lock before the LLM naming
call and the worktree creation, so the limit holds under concurrent
`/task` invocations.

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
- The task agent runs as a workflow run (`SetWorkflowRun(true)`) so it can
  never take the primary's terminal mid-turn, plus a task-run flag
  (`SetTaskRun(true)`) that keeps the **WebUI approval surface**. A task is
  long, user-initiated work, so it must be able to ask a question or request
  approval rather than failing every gated operation outright. It shares the
  primary's approval and ask-user managers, which resolve by request ID
  against one pending map, so a task's dialog and the primary's never
  collide and an answer reaches the agent that asked. With no browser
  attached the request fails fast with a message naming the surface, instead
  of blocking for the approval timeout.
- The task agent is constructed at depth 1 (a delegated run), not depth 0.
  A task runs concurrently with the primary's own turn, so it must not run
  the per-turn quality and verification hooks (they read per-agent turn
  state the primary is using) or publish progress-completion events under
  an empty run id (which a consumer reads as an extra "run finished" for
  the primary's run). Delegation still works: the default depth limit is 1.
- A task gets its own session id (`task_<id>`) so its conversation state
  saves to its own file instead of over the primary's.
- `/task merge` is the only operation that touches the primary's tree, and
  it aborts cleanly on conflict. It runs the repo's hooks: the merge commit
  lands on the user's own branch, so it is the one commit in the feature a
  human would have made. The task's own commit bypasses hooks, because the
  worktree is unattended and a prompting hook would hang it.
- Task worktrees live inside the workspace, so `.sprout/.gitignore` must
  cover `tasks/` or `git status` is permanently dirty and `/task merge`
  (which refuses a dirty tree) could never run. `ensureTaskGitignore`
  appends to a user-written file and leaves a sprout-generated one alone,
  because the generator refreshes by exact content match.
- Tasks die with the process, but their records do not. Each workspace's
  task list is persisted under the state dir (`tasks/<workspace-key>.json`).
  On load, a record saved as `running` becomes `interrupted` — never
  `running` — because a task cannot outlive its process and the concurrency
  limit and `/task merge` would otherwise act on a lie. An interrupted task
  does not hold a slot, and `/task merge` still works for it: the branch and
  worktree are on disk, so its committed work is recoverable.

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
- Persistence: a saved record round-trips; a record saved as running loads as
  interrupted with a finish time; an interrupted record holds no concurrency
  slot; a corrupt state file is ignored rather than fatal; two workspaces do
  not share a task list.
- Prompts: a task keeps the WebUI approval surface and shares the primary's
  approval manager; with no browser attached its `ask_user` fails fast
  instead of blocking.
