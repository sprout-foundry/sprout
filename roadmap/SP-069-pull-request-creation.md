# SP-069: Pull Request Creation — Close the "agent did the work, now what?" Gap

**Status:** 📋 Proposed
**Date:** 2026-06-14
**Depends on:** SP-049 (Shell Permission Overhaul — git-write gating), SP-004 (Security)
**Priority:** High
**Effort Estimate:** ~3-4 days (3 phases)

## Problem

The agent can take a task end-to-end — branch, edit, commit, and **push** (the
`git` tool at `pkg/agent_tools/git_handler.go` supports
`add/commit/push/pull/checkout/stash`) — but then it stops. There is no
first-class way to **open a pull request**. `pkg/webcontent/github.go` only
*parses* GitHub URLs for `browse_url`; it does not create anything.

So the single most natural next step after "the agent finished a feature on a
branch" — open the PR so a human can review and merge — requires the user to
drop to a terminal and run `gh pr create` by hand, or go to the GitHub web UI.
For a product whose sister platform (`sprout-foundry`) runs agent tasks in the
cloud, "task complete → here is the PR link" is table stakes, not a nicety.

## Current State

| Capability | Where | Status |
|---|---|---|
| Commit | `git` tool / `commit` tool / `/commit` | ✅ |
| Push branch | `git` tool `push` action | ✅ |
| Parse GitHub URLs | `pkg/webcontent/github.go` | ✅ (read-only) |
| GitHub auth setup | `cmd/github_setup_prompt.go` | ✅ (prompts to configure `gh`/token) |
| **Create a PR** | — | ❌ **missing** |
| PR status / link surfaced in UI | — | ❌ missing |

The `--output-json` task contract (consumed by Foundry) returns
`files_modified` + `git_diff` but **no PR URL**, so even when a cloud task
pushes a branch, the platform has nothing to link the user to.

## Proposed Solution

Add a first-class PR-creation capability with three surfaces (agent tool, CLI
command, webui button) backed by one shared implementation that prefers the
GitHub API and falls back to the `gh` CLI.

### Phase 1: Backend PR creation

**New file:** `pkg/git/pull_request.go`

```go
type PullRequestRequest struct {
    Title      string
    Body       string
    Base       string // target branch; default = repo default branch
    Head       string // source branch; default = current branch
    Draft      bool
    Reviewers  []string
}

type PullRequestResult struct {
    URL    string
    Number int
    State  string // "open"
}

// CreatePullRequest opens a PR. Resolution order:
//   1. GitHub REST API (token from credential store / GH_TOKEN)
//   2. `gh pr create` shell-out when gh is installed and authed
//   3. structured error with the exact `gh` command the user can run
func CreatePullRequest(ctx context.Context, repoDir string, req PullRequestRequest) (*PullRequestResult, error)
```

- Remote/owner/repo derived from `git remote get-url origin`, reusing the
  parsing in `pkg/webcontent/github.go`.
- Auto-push the head branch if it has no upstream (gated by the same git-write
  policy as `push`).
- Body generation: when `Body` is empty, synthesize from the branch's commit
  messages + the change manifest (reuse `ChangeTracker`).

### Phase 2: Agent tool + CLI

- **Agent tool** `create_pull_request` registered in
  `pkg/agent/tool_registrations.go`, handler alongside `handleGitOperation`
  in `pkg/agent/tool_handlers_shell.go`. Gated as a **git-write** operation
  (persona must hold `git_write`; same gate as `commit`/`push`, per SP-049).
  The tool description instructs the model to call it after pushing a feature
  branch, and to write a real title/body (not placeholders).
- **CLI** `sprout pr [--title …] [--body …] [--base …] [--draft] [--web]`
  in `cmd/pr.go`. With no flags, it generates title/body from commits and
  opens an editor for confirmation (unless `--skip-prompt`).
- **Task contract:** add `pull_request_url` to the `--output-json` payload in
  `cmd/agent_result.go` so Foundry can surface it.

### Phase 3: WebUI

- "Create Pull Request" button in the git panel
  (`webui/src/components/GitPanel.tsx`) enabled when the current branch is
  ahead of its base. Opens a small dialog (title, body prefilled from commits,
  base selector, draft toggle), `POST /api/git/pull-request`.
- New endpoint in `pkg/webui/` git API handlers.
- On success, toast with a clickable PR link (and feed it into the
  notification system from SP-070).

## Files Reference

| File | Action |
|------|--------|
| `pkg/git/pull_request.go` | **New** — API-first PR creation with `gh` fallback |
| `pkg/git/pull_request_test.go` | **New** — unit tests (mock transport + mock `gh`) |
| `pkg/agent/tool_registrations.go` | Modify — register `create_pull_request` |
| `pkg/agent/tool_handlers_shell.go` | Modify — handler + git-write gate |
| `cmd/pr.go` | **New** — `sprout pr` command |
| `cmd/agent_result.go` | Modify — add `pull_request_url` to `--output-json` |
| `pkg/webui/api_git*.go` | Modify — `POST /api/git/pull-request` |
| `webui/src/components/GitPanel.tsx` | Modify — Create PR button + dialog |
| `webui/src/services/api/gitApi.ts` | Modify — PR API call |

## Success Criteria

- `sprout pr` on a pushed branch opens a PR and prints the URL.
- The agent, after pushing a feature branch, can call `create_pull_request`
  and return the PR link in its final message.
- A Foundry task that pushes a branch surfaces `pull_request_url` in
  `--output-json`.
- No token configured → a clear, actionable error with the exact `gh` command,
  not a stack trace.
- PR creation respects git-write gating (blocked when the persona lacks
  `git_write` or in headless mode without authorization).

## Out of Scope

- Non-GitHub forges (GitLab/Bitbucket/Gitea) — design the resolver so a
  second backend can be added, but ship GitHub only.
- PR review/merge from inside sprout (commenting, approving, merging).
- Auto-linking issues / project boards.

## Open Questions

1. Token source precedence: credential store vs `GH_TOKEN` env vs `gh` config?
   Recommendation: credential store → `GH_TOKEN` → `gh`.
2. Should the agent open PRs as **draft by default** for safety? Likely yes in
   autonomous/automate runs, no in interactive use.
