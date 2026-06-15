# TODO

Active work tracked here. Completed items are removed once their parent spec is moved to ✅ Implemented in `roadmap/README.md` — the spec file itself is the historical record.

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

## SP-063: Real `computer_user` Persona — Mouse/Keyboard/Screenshot Agent
_Spec: `roadmap/SP-063-computer-use-persona.md`_

Core landed 2026-06-15 (build-verified + unit-tested). Remaining work is the WebUI surface, two interactive safety gates, and a real-display integration smoke test.

- [x] SP-063-1: Tool surface — 7 tools (`take_screenshot`, `mouse_click`, `mouse_drag`, `keyboard_type`, `keyboard_press`, `scroll`, `wait`) + Anthropic `computer_20241022` translation. (`handlers.go`, `anthropic.go`, `registry.go`)
- [x] SP-063-2: Platform backends — macOS (`cliclick`/`screencapture`) + Linux-X11 (`xdotool`/`scrot`|`import`), Wayland/other rejected with a reason, region crop in-process. (`backend_subprocess.go`, `backend_select.go`)
- [x] SP-063-4 (core): Safety — off-by-default `ComputerUseConfig`, JSONL audit log (typed text recorded as length only), action-rate cap. (`config.go`, `audit.go`, `safety.go`)
- [x] SP-063-5: Persona prompt `computer_user.md` (screenshot→describe→propose→act→verify; pause-before-Send/Delete/Pay/password).
- [x] SP-063-6: Allowlist — `computer_user` persona `allowed_tools` + dispatch-layer guard rejecting the tools for any other persona; gated registration at agent creation. (`pkg/personas/configs/computer_user.json`, `computer_use_registration.go`, `tool_security.go`)
- [x] SP-063-3b: Vision capability enforcement — `checkComputerUseActivation` refuses `computer_user` on a non-vision provider (`a.client.SupportsVision()`), checked in `ApplyPersona`. Also gates on enabled-flag, platform support, and top-level-only (no subagent).
- [ ] SP-063-4b: Interactive safety gates (remaining) — first-use per-session opt-in prompt ("Allow agent to control your computer?"), a global Ctrl+C+Esc panic key that halts within 500ms, a destructive-app denylist heuristic (Mail/Banking/Disk Utility/Trash) requiring per-action confirmation, and a precise `--skip-prompt`/daemon block. (Enabled-flag/platform/subagent/vision activation gates already shipped in SP-063-3b.)
- [ ] SP-063-7: WebUI settings — "Computer Use (Experimental)" section: master toggle, per-workspace auto-approve allowlist, audit-log location, and a "Test connection" button (`CheckPlatformSupport` + one screenshot round-trip).
- [ ] SP-063-8b: Integration smoke test — Xvfb + a controlled tkinter app on Linux/X11; assert the agent can click a known button and the app state changes. (Unit tests with the mock backend already ship.)

## SP-059: Subagent ↔ Primary Interaction Overhaul + Delegate Retirement
_Spec: `roadmap/SP-059-subagent-interaction.md`_

Phases 1–3 are shipped. Phase 6 (delete the `delegate` tool) is the remaining high-priority work.

### Phase 6: Delete the `delegate` tool entirely
- [x] SP-059-6a: Port useful delegate-only features to `run_subagent`. Check `pkg/agent/tool_handlers_delegate.go` for any features that `run_subagent` lacks (e.g. `FollowUpMessages` — confirm it's intentionally dropped per SP-059 non-goals). If nothing needs porting, document that and move on. Acceptance: a review of delegate features confirms nothing needed is missing from run_subagent.
- [x] SP-059-6b: Delete the `delegate` tool registration. Remove `delegate` and `delegate_status` registrations from `pkg/agent/tool_registrations.go` and `pkg/agent_api/tools.go`. Delete the delegate handler files (`pkg/agent/tool_handlers_delegate*.go`). Acceptance: `make build-all` passes; the tool listing no longer includes `delegate` or `delegate_status`.
- [x] SP-059-6c: Remove delegate-related agent fields. Remove `delegateDepth`, `delegateID` fields and `SPROUT_MAX_DELEGATE_DEPTH` env-var handling from `pkg/agent/agent.go` and related code. Acceptance: `make build-all` passes; `grep -r delegateDepth pkg/` returns nothing.
- [x] SP-059-6d: Migrate webui delegate event consumers. In `webui/`, find any consumers of `delegate_spawn`/`delegate_activity`/`delegate_complete`/`delegate_tool` events and migrate them to the subagent equivalents (these events already exist for subagents). Acceptance: `make build-all` passes; no references to `delegate_*` events remain in webui code.
- [x] SP-059-6e: Phase 6 tests and verification. Run `make build-all` and `go test ./...` — both must pass. Verify `run_subagent` still works via manual or automated test. Update `roadmap/SP-006-delegate-tool.md` to mark as superseded by SP-059. Acceptance: all tests pass; SP-006 spec marked superseded.

### Phase 4: Subagents can request user clarification mid-run
- [x] SP-059-4: Wire clarificationManager into createSubagent. Currently subagents cannot call `request_clarification`. Wire the primary's `clarificationManager` into the subagent creation path (`pkg/agent/subagent_runner.go::createSubagent`) so subagents can request clarification from the user mid-run. Acceptance: a subagent calling `request_clarification` surfaces the question to the user via the existing primary agent event bus path.

## SP-060: Desktop App — Per-Workspace Server Mode
_Spec: `roadmap/SP-060-desktop-serve.md`_

Phase A (auth token + TCP random port) and Phase B (Unix domain socket) are largely implemented. Verify and close out.

- [x] SP-060-A: Verify Phase A and Phase B completeness. Run the existing `desktop/backend_test.js` tests. Verify: (1) `generateSecret()` produces a 256-bit hex token, (2) `SPROUT_AUTH_TOKEN` env var is passed to the child process, (3) the HTTP proxy injects `Authorization: Bearer <token>` headers, (4) `--bind-socket` flag works in Go and the Electron proxy forwards to the socket, (5) Windows falls back to TCP+auth. If all pass, update the spec status to ✅ and move SP-060 in `roadmap/README.md` from "In Progress" to "Implemented". Acceptance: `node desktop/backend_test.js` passes; spec and README updated.

## SP-068: Security Check Consolidation
_Spec: `roadmap/SP-068-security-check-consolidation.md`_

Refactor to unify the two risk vocabularies (static classifier SAFE/CAUTION/DANGEROUS + persona cascade Low/Medium/High/Critical) into one scale, one resolver, one broker.

### Phase 1: One risk scale (non-behavioral mapping)
- [x] SP-068-1a: Create `RiskAssessment` type. In `pkg/agent_tools/` (or `pkg/agent/`), create a `RiskAssessment` struct with fields: `Level` (Low/Medium/High/Critical), `Sources` ([]RiskSource — classifier, persona, git-gate, fs-tier, workspace-policy), `Reason` (human-readable string), `IsHardBlock` (bool). This is a pure type addition with no behavior change. Acceptance: type compiles; `go vet` clean; `go build ./...` passes.

### Phase 2: One resolver (collapse the two gates)
- [x] SP-068-2a: Implement `ResolveToolRisk` function. Create `ResolveToolRisk(toolName, args, agent) RiskAssessment` that runs all security inputs (static classifier, persona cascade, git-gate, fs-tier, workspace policy) and returns the most restrictive result. Ship behind a `unified_risk_resolver` config flag (default off). Add shadow-mode logging comparing old-vs-new decisions. Acceptance: with flag off, behavior is unchanged. With flag on, the resolver returns a single decision; shadow-mode log shows zero decision changes except eliminated duplicate prompts on a test corpus.

### Phase 3: One broker + "explain" diagnostic
- [x] SP-068-3a: Add `sprout explain` CLI subcommand. Add an `explain` subcommand that takes a command string and prints the full `RiskAssessment`: canonical level, every contributing source, and the exact rule that set the level. Complements SP-049's `sprout audit tail`. Acceptance: `sprout explain 'git reset --hard HEAD~5'` prints level `High/Critical` with annotated contributing sources.

## SP-066: Structured File Key Order Preservation
_Spec: `roadmap/SP-066-structured-file-key-order.md`_

- [x] SP-066-keyorder: Preserve insertion key order in structured file tools. Update `write_structured_file` and `patch_structured_file` to preserve the key order the LLM sends, rather than alphabetically sorting keys. Use an ordered map implementation (e.g. `OrderedMap` with slice + map) instead of `map[string]interface{}` for JSON/YAML serialization. Acceptance: `write_structured_file` with keys `{name, version, directory}` produces a file with keys in that order, not alphabetical. `patch_structured_file` preserves original key order on re-serialization. Existing tests pass.

## SP-015: Cloud Platform Integration
_Spec: `roadmap/SP-015-cloud-platform.md`_

- [x] SP-015-R1: Add WASM interception in CloudAdapter. The `CloudAdapter` class in `webui/src/services/cloudAdapter.ts` classifies 17 endpoints as `wasm-local` but does NOT intercept them — they fall through to `fetch()` → 404. Add WASM interception: check `isWasmLocal()` and route to WASM shell methods instead of `fetch()`. The Service Worker path already does this correctly. Acceptance: file operations work through the CloudAdapter path in cloud mode; `cloudAdapter.test.ts` and `cloudAdapter.integration.test.ts` pass.
- [x] SP-015-R3: Audit component-level feature flag adoption. Audit webui components that reference SSH, instances, local terminal, or settings. Ensure they use `supports*` flags from `webui/src/config/mode.ts` so local-only features are hidden in cloud mode. Acceptance: no local-only UI elements appear when running in cloud mode.
- [x] SP-015-R5: Verify WebSocket routing across all three patterns. Three WebSocket patterns exist: (1) transparent reverse proxy, (2) JSON-over-websocket tunnel, (3) no WebSocket (browser IDE uses SSE + MessageChannel). Verify the webui's WebSocket client handles all three. Acceptance: terminal sessions work in all three deployment environments.

## SP-061: Remove Static Embedding Provider
_Spec: `roadmap/SP-061-remove-static-embeddings.md`_

Tech-debt reduction: remove the static embedding provider and consolidate on ONNX.

- [x] SP-061-1: Delete static provider files. Remove `pkg/embedding/static_*.go` files (static provider implementation). Update imports and references throughout `pkg/embedding/manager.go`. Acceptance: `go build ./...` succeeds with no references to deleted files.
- [x] SP-061-2: Update WASM embedding and memory code. Update `cmd/wasm/embedding_funcs.go`, `pkg/agent/memory_embedding.go`, and `pkg/agent/memory_search_handler.go` to remove static provider code paths. Acceptance: `go build -tags wasm ./cmd/wasm/` succeeds; WASM build works without static model.
- [x] SP-061-3: Update tests and build system. Remove or update static-provider tests in `pkg/embedding/`. Ensure `go test ./pkg/embedding/...` and `go test ./pkg/agent/...` pass. Update any build scripts that reference static model paths. Acceptance: all tests pass; semantic search via ONNX provider returns correct results.
