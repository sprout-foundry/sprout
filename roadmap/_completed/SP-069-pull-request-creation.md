# SP-069: Pull Request Creation — Close the "agent did the work, now what?" Gap

**Status:** ✅ Implemented (2026-06-14; agent tool, sprout pr CLI, WebUI button)

The agent could branch, edit, commit, and push — but had no first-class way to open a pull request. This spec added PR creation backed by the GitHub REST API (with `gh` CLI fallback), exposed as an agent tool (`create_pull_request`), a CLI command (`sprout pr`), and a WebUI button. The `--output-json` task contract now includes `pull_request_url` for Foundry integration. Token resolution prefers credential store, then `GH_TOKEN`, then `gh`.

## Key decisions

- Resolution order: GitHub REST API → `gh pr create` shell-out → structured error with exact command
- Auto-pushes the head branch if it has no upstream (gated by git-write policy)
- Agent tool is gated as a git-write operation (persona must hold `git_write`)
- Body auto-synthesized from branch commit messages + change manifest when not provided
- GitHub-only for v1; resolver designed to accept additional forges later

## Artifacts

- code: `pkg/git/pull_request.go` — API-first PR creation with gh fallback
- code: `pkg/git/pull_request_test.go` — unit tests with mock transport + mock gh
- code: `cmd/pr.go` — `sprout pr` CLI command
- code: `webui/src/services/api/gitApi.ts` — PR API call
- code: `webui/src/components/GitSidebarPanel.tsx` — Create PR button + dialog
- code: `pkg/agent/tool_registrations.go` — create_pull_request tool registration

Full specification archived — see git history for original content.
