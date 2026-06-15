# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

The items below are the active, proposed product-gap work surfaced by the
2026-06-14 gap analysis. **SP-063** (`computer_user` persona) is **partially
implemented** — its core shipped; remaining work is tracked in
`roadmap/SP-063-computer-use-persona.md`, not here.

## SP-069: Pull Request Creation
_Spec: `roadmap/SP-069-pull-request-creation.md`_

Gap (High priority): the agent can commit and push a branch but has no way to open a PR — the most natural next step. `pkg/webcontent/github.go` only parses GitHub URLs.

- [ ] SP-069-1: Create `pkg/git/pull_request.go` with `CreatePullRequest(ctx, repoDir, req)`. Resolution order: GitHub REST API (token from credential store / `GH_TOKEN`) → `gh pr create` → structured error with the exact `gh` command. Derive owner/repo from `git remote` (reuse `pkg/webcontent/github.go` parsing); auto-push the head branch if it has no upstream (git-write gated); synthesize body from commits + `ChangeTracker` manifest when empty. Add `pull_request_test.go` (mock transport + mock `gh`). Acceptance: `go test ./pkg/git/...` passes; PR opens against a test remote and returns the URL.
- [ ] SP-069-2: Register the `create_pull_request` agent tool in `pkg/agent/tool_registrations.go` with a handler in `pkg/agent/tool_handlers_shell.go`, gated as a **git-write** op (SP-049 — persona must hold `git_write`, blocked headless without authorization). Tool description instructs the model to call it after pushing a feature branch with a real title/body. Acceptance: `make build-all` passes; the tool listing includes `create_pull_request`; the gate blocks when the persona lacks `git_write`.
- [ ] SP-069-3: Add `cmd/pr.go` — `sprout pr [--title --body --base --draft --web]`; generate title/body from commits and confirm via `$EDITOR` unless `--skip-prompt`. Add `pull_request_url` to the `--output-json` payload in `cmd/agent_result.go`. Acceptance: `sprout pr` on a pushed branch prints the PR URL; `--output-json` includes `pull_request_url`.
- [ ] SP-069-4: WebUI — `POST /api/git/pull-request` handler in `pkg/webui/`, a "Create Pull Request" button + dialog in `webui/src/components/GitPanel.tsx` (enabled when the branch is ahead of base), API call in `webui/src/services/api/gitApi.ts`, and a clickable PR-link toast on success. Acceptance: `make build-all` passes; creating a PR from the git panel returns a clickable link.

## SP-070: Agent Completion Notifications
_Spec: `roadmap/SP-070-completion-notifications.md`_

Gap (High priority, low effort): notifications are in-app only — no CLI bell, no browser notification when the user has looked away. Completion signals already exist (`FinalizeAtTurnEnd()`, `query_completed`).

- [ ] SP-070-1: Add `NotificationsConfig` (`cli_bell`, `os_notify`, `browser`, `min_seconds`) to `pkg/configuration/config.go`. Create `pkg/notify/notify.go` — cross-platform OS notification via subprocess (`osascript` / `notify-send` / PowerShell toast), no-op when the tool is absent. Add `notify_test.go`. Acceptance: backend selection picks the right tool per OS and no-ops cleanly when absent; `go test ./pkg/notify/...` passes.
- [ ] SP-070-2: CLI wiring in `cmd/agent_modes.go` — emit terminal bell (`\a`) + optional OS notify on turn completion (around `FinalizeAtTurnEnd()`) and on a blocking approval (`pkg/console/security_prompt.go`), only when the turn exceeded `min_seconds` and the session is interactive (suppressed under `--skip-prompt`/non-TTY). Acceptance: a >10s interactive turn rings the bell on completion and on a blocking prompt; non-interactive runs are silent.
- [ ] SP-070-3: Add an `input_required` event in `pkg/events/events.go` so "agent is blocked on the human" is an explicit, notifiable signal on both surfaces. Acceptance: the event fires when the agent blocks on a prompt/approval; CLI and webui can subscribe.
- [ ] SP-070-4: WebUI — `webui/src/services/desktopNotify.ts` (`Notification` API + permission flow); fire on `query_completed` and `input_required` in `useWebSocketEventHandler.ts` **only when `document.hidden`**; clicking focuses the tab + chat. Add a settings toggle + "Test" button; record in `notificationBus` history. Acceptance: a backgrounded tab raises a browser notification on completion and input-required; clicking focuses the tab.

## SP-071: Conversation Rewind & Edit-and-Resend
_Spec: `roadmap/SP-071-conversation-rewind.md`_

Gap (Medium-High): files can be reverted but the conversation can't be rewound — no "jump back to turn N, fix the prompt, re-run."

- [ ] SP-071-1: Create `pkg/agent/rewind.go` — `(*Agent).Rewind(opts)` truncating `messages[]` at a `TurnCheckpoint` boundary, dropping orphaned checkpoints, and (default) reverting the discarded turns' file changes via `ChangeTracker` in reverse order; skip files modified outside the agent; snapshot first so rewind is itself undoable; leave embeddings untouched (SP-066 invariant). Add `rewind_test.go`. Acceptance: `/rewind` to a turn truncates history and reverts later file edits; files changed outside the agent are reported, not clobbered; `go test ./pkg/agent/...` passes.
- [ ] SP-071-2: Add `/rewind` — `pkg/agent_commands/rewind_command.go` registered in `commands.go`: no-arg interactive turn picker (built on `select_list`, SP-057) and `/rewind N`; print the `RewindResult` and drop the user at a fresh prompt after turn N. Acceptance: `/rewind 3` rewinds to turn 3 and reports turns discarded + files reverted; `/rewind` with no arg shows a picker.
- [ ] SP-071-3: WebUI — `POST /api/query/rewind { to_turn, revert_files }`; add "Edit & resend from here" to `webui/src/components/ChatMessageContextMenu.tsx` with a discard-confirmation, prompt pre-fill, resubmit, and collapse (not delete) of the discarded branch. Acceptance: `make build-all` passes; editing & resending a prior message rewinds and resubmits; the discarded branch is collapsed, not lost.

## SP-072: Per-Hunk Diff Approval — Approve-Before-Apply
_Spec: `roadmap/SP-072-diff-approval.md`_

Gap (Medium): the agent applies edits immediately; there is no opt-in approve-before-apply / per-hunk accept-reject.

- [ ] SP-072-1: Create `pkg/agent/edit_approval.go` — `EditProposal`/`EditDecision`, a unified-diff hunk splitter, partial apply (original + accepted hunks only), and a broker call reusing the SP-049/SP-068 approval delivery (timeout + webui↔CLI fallback). The tool result states exactly which hunks applied/rejected. Add `edit_approval_test.go`. Acceptance: hunk split + partial apply + reject-all are covered by tests; `go test ./pkg/agent/...` passes.
- [ ] SP-072-2: Add `EditApprovalConfig` (`mode: off|all|paths`, `paths` globs) to `pkg/configuration/config.go`; route `write_file`/`edit_file`/`patch_structured_file` in `pkg/agent/tool_handlers_file.go` through approval when active; treat non-interactive runs (`--skip-prompt`/automate/daemon) as approve-all (no hangs). Acceptance: `mode: off` (default) preserves current behavior; `mode: all` gates every write; `mode: paths` gates only matching globs.
- [ ] SP-072-3: CLI review UI in `pkg/console` — colored unified diff with a per-hunk `[a]ccept / [r]eject / [s]elect / [e]dit ($EDITOR)` prompt (built on the SP-057 prompt/select primitives). Acceptance: a gated write renders the diff and applies only accepted hunks; rejected hunks never touch the working tree.
- [ ] SP-072-4: WebUI — `webui/src/components/EditApprovalPanel.tsx` (per-hunk Accept/Reject, Apply selected / Reject all), `POST /api/edits/{id}/decision`, wired into the tool-event stream and triggering an `input_required` notification (SP-070). Acceptance: `make build-all` passes; a pending edit blocks visibly and applies only accepted hunks.
