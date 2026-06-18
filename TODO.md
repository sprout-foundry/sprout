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

- [x] SP-069-1: Create `pkg/git/pull_request.go` with `CreatePullRequest(ctx, repoDir, req)`. Resolution order: GitHub REST API (token from credential store / `GH_TOKEN`) → `gh pr create` → structured error with the exact `gh` command. Derive owner/repo from `git remote` (reuse `pkg/webcontent/github.go` parsing); auto-push the head branch if it has no upstream (git-write gated); synthesize body from commits + `ChangeTracker` manifest when empty. Add `pull_request_test.go` (mock transport + mock `gh`). Acceptance: `go test ./pkg/git/...` passes; PR opens against a test remote and returns the URL.
- [x] SP-069-2: Register the `create_pull_request` agent tool in `pkg/agent/tool_registrations.go` with a handler in `pkg/agent/tool_handlers_shell.go`, gated as a **git-write** op (SP-049 — persona must hold `git_write`, blocked headless without authorization). Tool description instructs the model to call it after pushing a feature branch with a real title/body. Acceptance: `make build-all` passes; the tool listing includes `create_pull_request`; the gate blocks when the persona lacks `git_write`.
- [x] SP-069-3: Add `cmd/pr.go` — `sprout pr [--title --body --base --draft --web]`; generate title/body from commits and confirm via `$EDITOR` unless `--skip-prompt`. Add `pull_request_url` to the `--output-json` payload in `cmd/agent_result.go`. Acceptance: `sprout pr` on a pushed branch prints the PR URL; `--output-json` includes `pull_request_url`.
- [x] SP-069-4: WebUI — `POST /api/git/pull-request` handler in `pkg/webui/`, a "Create Pull Request" button + dialog in `webui/src/components/GitPanel.tsx` (enabled when the branch is ahead of base), API call in `webui/src/services/api/gitApi.ts`, and a clickable PR-link toast on success. Acceptance: `make build-all` passes; creating a PR from the git panel returns a clickable link.

## SP-070: Agent Completion Notifications
_Spec: `roadmap/SP-070-completion-notifications.md`_

Gap (High priority, low effort): notifications are in-app only — no CLI bell, no browser notification when the user has looked away. Completion signals already exist (`FinalizeAtTurnEnd()`, `query_completed`).

- [x] SP-070-1: Add `NotificationsConfig` (`cli_bell`, `os_notify`, `browser`, `min_seconds`) to `pkg/configuration/config.go`. Create `pkg/notify/notify.go` — cross-platform OS notification via subprocess (`osascript` / `notify-send` / PowerShell toast), no-op when the tool is absent. Add `notify_test.go`. Acceptance: backend selection picks the right tool per OS and no-ops cleanly when absent; `go test ./pkg/notify/...` passes.
- [x] SP-070-2: CLI wiring in `cmd/agent_modes.go` — emit terminal bell (`\a`) + optional OS notify on turn completion (around `FinalizeAtTurnEnd()`) and on a blocking approval (`pkg/console/security_prompt.go`), only when the turn exceeded `min_seconds` and the session is interactive (suppressed under `--skip-prompt`/non-TTY). Acceptance: a >10s interactive turn rings the bell on completion and on a blocking prompt; non-interactive runs are silent.
- [x] SP-070-3: Add an `input_required` event in `pkg/events/events.go` so "agent is blocked on the human" is an explicit, notifiable signal on both surfaces. Acceptance: the event fires when the agent blocks on a prompt/approval; CLI and webui can subscribe.
- [x] SP-070-4: WebUI — `webui/src/services/desktopNotify.ts` (`Notification` API + permission flow); fire on `query_completed` and `input_required` in `useWebSocketEventHandler.ts` **only when `document.hidden`**; clicking focuses the tab + chat. Add a settings toggle + "Test" button; record in `notificationBus` history. Acceptance: a backgrounded tab raises a browser notification on completion and input-required; clicking focuses the tab.

## SP-071: Conversation Rewind & Edit-and-Resend
_Spec: `roadmap/SP-071-conversation-rewind.md`_

Gap (Medium-High): files can be reverted but the conversation can't be rewound — no "jump back to turn N, fix the prompt, re-run."

- [x] SP-071-1: Create `pkg/agent/rewind.go` — `(*Agent).Rewind(opts)` truncating `messages[]` at a `TurnCheckpoint` boundary, dropping orphaned checkpoints, and (default) reverting the discarded turns' file changes via `ChangeTracker` in reverse order; skip files modified outside the agent; snapshot first so rewind is itself undoable; leave embeddings untouched (SP-066 invariant). Add `rewind_test.go`. Acceptance: `/rewind` to a turn truncates history and reverts later file edits; files changed outside the agent are reported, not clobbered; `go test ./pkg/agent/...` passes.
- [x] SP-071-2: Add `/rewind` — `pkg/agent_commands/rewind_command.go` registered in `commands.go`: no-arg interactive turn picker (built on `select_list`, SP-057) and `/rewind N`; print the `RewindResult` and drop the user at a fresh prompt after turn N. Acceptance: `/rewind 3` rewinds to turn 3 and reports turns discarded + files reverted; `/rewind` with no arg shows a picker.
- [x] SP-071-3: WebUI — `POST /api/query/rewind { to_turn, revert_files }`; add "Edit & resend from here" to `webui/src/components/ChatMessageContextMenu.tsx` with a discard-confirmation, prompt pre-fill, resubmit, and collapse (not delete) of the discarded branch. Acceptance: `make build-all` passes; editing & resending a prior message rewinds and resubmits; the discarded branch is collapsed, not lost.

## SP-072: Per-Hunk Diff Approval — Approve-Before-Apply
_Spec: `roadmap/SP-072-diff-approval.md`_

Gap (Medium): the agent applies edits immediately; there is no opt-in approve-before-apply / per-hunk accept-reject.

- [x] SP-072-1: Create `pkg/agent/edit_approval.go` — `EditProposal`/`EditDecision`, a unified-diff hunk splitter, partial apply (original + accepted hunks only), and a broker call reusing the SP-049/SP-068 approval delivery (timeout + webui↔CLI fallback). The tool result states exactly which hunks applied/rejected. Add `edit_approval_test.go`. Acceptance: hunk split + partial apply + reject-all are covered by tests; `go test ./pkg/agent/...` passes.
- [x] SP-072-2: Add `EditApprovalConfig` (`mode: off|all|paths`, `paths` globs) to `pkg/configuration/config.go`; route `write_file`/`edit_file`/`patch_structured_file` in `pkg/agent/tool_handlers_file.go` through approval when active; treat non-interactive runs (`--skip-prompt`/automate/daemon) as approve-all (no hangs). Acceptance: `mode: off` (default) preserves current behavior; `mode: all` gates every write; `mode: paths` gates only matching globs.
- [x] SP-072-3: CLI review UI in `pkg/console` — colored unified diff with a per-hunk `[a]ccept / [r]eject / [s]elect / [e]dit ($EDITOR)` prompt (built on the SP-057 prompt/select primitives). Acceptance: a gated write renders the diff and applies only accepted hunks; rejected hunks never touch the working tree.
- [x] SP-072-4: WebUI — `webui/src/components/EditApprovalPanel.tsx` (per-hunk Accept/Reject, Apply selected / Reject all), `POST /api/edits/{id}/decision`, wired into the tool-event stream and triggering an `input_required` notification (SP-070). Acceptance: `make build-all` passes; a pending edit blocks visibly and applies only accepted hunks.

## SP-073: Cooperative Cancellation — Stop Actually Aborts
_Spec: `roadmap/SP-073-cooperative-cancellation.md`_

Tech debt / UX (Medium-High): ~8 pipelines pass `context.Background()` to the provider, so Ctrl+C / Stop can't abort them (10 `TODO(SP-034-1c)` markers). `a.interruptCtx` is the ready cancellation source.

- [x] SP-073-1: Phase 1 — add a leading `ctx context.Context` param to the leaf functions in `pkg/spec/extractor.go` + `validator.go`, `pkg/codereview/prompts.go`, `pkg/agent_tools/vision_pdf.go` + `vision_analyze.go`, `pkg/agent_commands/commit_review.go` + `shell.go`, and forward it to `SendChatRequest`/`SendVisionRequest` instead of `context.Background()`. Acceptance: `make build-all` passes; those calls use the passed ctx.
- [x] SP-073-2: Phase 2 — callers pass a real cancellation context (tool `Execute(ctx,…)` for handler-driven paths; `a.interruptCtx` for agent methods, incl. the `GenerateResponse` signature note at `agent_getters.go:633`). Acceptance: `grep -rn "context.Background()" ` in the affected pipelines' provider calls returns nothing; `make build-all` passes.
- [x] SP-073-3: Phase 3 — verify Ctrl+C / Stop aborts a multi-page PDF OCR and a commit review within ~1s (clean "aborted" result, no crash); delete all 10 `TODO(SP-034-1c)` markers. Acceptance: `grep -rn "TODO(SP-034-1c)" pkg/` is empty; manual abort confirmed.

## SP-074: Finish the Tool-Registry Migration (retire SP-038 shim)
_Spec: `roadmap/SP-074-tool-registry-migration.md`_

Tech debt (Medium): the dual registry has 3 `TODO(SP-038)` leftovers — `ForPersona` is a stub returning all tools, `ToolEnv.OutputWriter` is hardcoded to `os.Stdout`, and `ToolEnv.ApprovalManager` is `nil`.

- [x] SP-074-1: Implement real per-persona filtering — `(*ToolRegistry).ForPersona(allowlist []string) map[string]ToolHandler` in `pkg/agent_tools/registry.go` (empty allowlist → all tools), with tests for allowlist / empty / unknown-tool. Acceptance: `ForPersona` returns exactly the allowlisted tools; `go test ./pkg/agent_tools/...` passes.
- [x] SP-074-2: Route tool output through the agent — add an `io.Writer` accessor backed by `PrintLine`/`PrintLineAsync` (`OutputRouter`) and set `env.OutputWriter` to it in `pkg/agent/tool_security.go` (keep `os.Stdout` only as the nil-agent fallback). Acceptance: a streaming new-interface tool's output appears in the WebUI, not just stdout.
- [x] SP-074-3: Add a `tools.ApprovalManager` adapter over the agent's `security.ApprovalManager` (signature translation) and set `env.ApprovalManager`. Acceptance: a migrated tool can request approval via the env and it surfaces through the normal CLI/WebUI prompt.
- [x] SP-074-4: Collapse the dual-dispatch fallback for migrated tools (keep only for documented legacy func-style handlers); remove the `TODO(SP-038)` markers. Acceptance: `grep -rn "TODO(SP-038)" pkg/` is empty; existing persona tool-gating is regression-tested unchanged; `make build-all` + `go test ./...` green.

## SP-075: Large-File Decomposition
_Spec: `roadmap/SP-075-large-file-decomposition.md`_

Maintainability (Low-Medium): 20+ files exceed 800 lines (config.go 2833, agent_modes.go 2344, …) vs the 500-line target. Pure, incremental file-level extraction — build + tests green after each step.

- [x] SP-075-1: Phase 1 (config + cmd) — split `pkg/configuration/config.go` (per-domain `config_*.go` for the nested structs + `Resolve()`s), `cmd/agent_modes.go` (per-mode files), `cmd/agent_workflow.go`. Acceptance: each targeted file < ~600 lines or documented exception; `make build-all` + `go test ./...` green; no API diff beyond moves.
- [x] SP-075-2: Phase 2 (agent core) — `pkg/agent/tool_handlers_subagent.go`, `seed_integration.go`, `subagent_runner.go`, `change_tracking_shell.go`. Acceptance: as above, per file.
- [x] SP-075-3: Phase 3 (providers + web) — `pkg/agent_providers/generic_provider.go` ✅ (split into 5 files), `pkg/webcontent/browser_rod.go` (split into 4 files, 1398→max 587), `pkg/wasmshell/commands.go` (split into 4 files, 1633→368), `pkg/console/input_core.go` (1264→706, extracted editing/escape-parser/search), `webui/src/components/Terminal.tsx` (1320→700, extracted `useTerminalPanes` hook). Acceptance: all packages build+test green; no file >750 lines.

## SP-045: WASM Build Feature Parity — distribution tail
_Spec: `roadmap/SP-045-wasm-feature-parity.md` §4–§5 (Tiers 1/2a/2b already shipped)_

- [x] SP-045-1: Strip the WASM binary — add `-ldflags="-s -w"` in `scripts/build-wasm.sh`; confirm the size drop. Acceptance: WASM binary is measurably smaller; still loads.
- [x] SP-045-2: Spike `tinygo` for `cmd/wasm` — assess stdlib/`pkg/agent` compatibility; document go/no-go. Acceptance: written go/no-go with the blocking incompatibilities (if any).
- [x] SP-045-3: Split into a small shell-only WASM module + a lazy-loaded `embedding.wasm` (loads on first semantic search). Acceptance: casual page load no longer pulls the embedding module.
- [x] SP-045-4: Build-matrix hygiene — tag `pkg/webui/terminal_*.go` (creack/pty importers) `!js` and sweep `//go:build !windows` that wrongly include `js`. Acceptance: `GOOS=js GOARCH=wasm go build ./...` is clean.

## SP-015: Cloud Platform Integration — remaining follow-ups
_Spec: `roadmap/SP-015-cloud-platform.md` (R1/R3/R5 already complete)_

- [ ] SP-015-R4: Add a CI check that fails if Foundry's Service-Worker route table diverges from `dist/endpoint-manifest.json`; evaluate a shared `@sprout/endpoint-registry` package. Acceptance: a manifest/route-table divergence fails CI.
- [ ] SP-015-R6: Define the canonical dist-bundle layout the browser IDE expects (`index.html`, `static/js/`, `wasm/`) and make `scripts/build-webui-dist.mjs` produce exactly that. Acceptance: the build output matches the documented layout.
- [ ] SP-015-R7: Chat-translation robustness — edge-case tests (empty query, missing `chat_id`, steer, stop), document the Foundry chat contract, and consider extracting the `{query}`→`{messages,stream}` translation into a module shared with Foundry's `chat-bridge.ts`. Acceptance: edge-case tests pass; the contract is documented.
