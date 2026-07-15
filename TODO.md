# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

---

## SP-116: Multi-Instance Isolation

_Spec: `roadmap/SP-116-multi-instance-isolation.md`. 🔵 Proposed — not yet started._

### Phase 1: Auto-isolate config per workspace (~0.5 day)

Auto-detect when `sprout agent` is run in a git repo (`.git` in cwd or
ancestors) and auto-bootstrap `.sprout/config.json` by cloning from the
global config on first run. This makes `--isolated-config` the default
for all repo-backed directories.

**Files:** `cmd/root.go` (~50 lines in `PersistentPreRunE`) + maybe
`pkg/configuration/isolated_config.go` (handle empty source gracefully).

### Phase 2: Per-instance background processes (~0.5 day)

Scope `/tmp/sprout-bg/` to `<configDir>/bg-processes/` so `sprout
shell-bg list` only shows sessions from the current workspace.

**Files:** `pkg/agent_tools/background_process_manager.go`,
`cmd/shell_bg.go`.

### Phase 3: Workspace config overrides (~1-2 days, stretch)

Allow `.sprout/config.json` to override specific settings (model
preference, subagent provider, persona) while inheriting
providers/credentials from the global config.

**Files:** `pkg/configuration/config_load_save.go`,
`pkg/configuration/config_paths.go`.

### Phase 4: Daemon service hardening (launch priority)

The `sprout service` daemon on port 56000 is the primary launch target (desktop
is deferred). The daemon must serve multiple workspaces through a single WebUI.
Already implemented:
- `NewAgentWithLayersInWorkspace` creates per-workspace agents
- `WorkspacePicker` UI for switching workspaces
- `RecentWorkspace` tracking (`~/.sprout/recent_workspaces.json`)
- `SPROUT_SERVICE=1` guard to skip auto-isolation for system daemon
- Graceful startup without provider (WebUI onboarding)

**Remaining**: Service manager robustness testing on macOS (launchd) and Linux
(systemd). Verify `sprout service install/start/stop/status` end-to-end.

### Acceptance

- Start two sprout instances in different git repos; each gets its own
  `.sprout/config.json`, `instances.json`, and port.
- `sprout shell-bg list` only shows sessions from the current workspace.
- `sprout agent` in a non-git directory still uses `~/.config/sprout/`
  (backward compat).
- Desktop app multi-workspace remains fully functional.

---

## SP-118: Daemon Multi-Window Session Isolation

_Spec: `roadmap/SP-118-daemon-multi-window-sessions.md`. 🔵 Proposed — not yet started._

Discovered while debugging a 13-minute UI hang in a two-window daemon
session (Windows A on the sprout repo + Window B SSH'd into another
box, both against the same `sprout service` daemon). Root cause: the
WS single-active policy currently enforced for *all* modes via
`activeWSByUserID sync.Map` (`pkg/webui/server.go:56`,
`websocket_handler.go:93-156`). Two browser windows under one user
account trigger `session_conflict`; the second sits in
`waitForTakeover` for up to 10s (`websocket_handler.go:798-826`)
then drops; the first window's socket is closed by the daemon,
and the first window's UI appears frozen even though the agent
keeps running.

The mislabel is real: the policy is attributed to "SP-046" in ~15
`[SP-046]` log lines (`websocket_handler.go:107, 120, 137, 729,
811, 816, 821, 836, 850, 860, 868, 879, 889, 905, 909, 914, 924,
939, 943, 953, 959, 963, 969, 973`) and in the
`pkg/webui/multi_device_takeover.go:7-12` doc comment that
references a non-existent "SP-046-5" sub-section. SP-046 is the
unrelated browser-primary workspace-sync spec
(`roadmap/_completed/SP-046-workspace-sync-model.md`).

Spec covers: split WS session policy into Mode 1 (`sprout agent`,
keeps current single-active enforcement byte-identical) and Mode 2
(`sprout service`, supports N parallel windows). Read spec
before implementing; non-trivial design decisions on
`agentEnforceSingleSession` field, registry shape, and per-session
panic cleanup.

### Phase 1: Registry + dispatch skeleton + log tag sweep

- [ ] **SP-118-1:** Add `agentEnforceSingleSession bool` field to
      `ReactWebServer` in `pkg/webui/server.go` (near `serviceMode` at
      `:76`). Set to `true` only by `sprout agent`. Add new
      `UserConnections` type in `pkg/webui/multi_connection_registry.go`
      with `Add`, `Remove`, `Count`, `ForEach`. Use lazy-allocated
      per-user `sync.RWMutex`. Existing `activeWSByUserID sync.Map`
      registry kept *temporarily* for the Mode-1 path; the Mode-2 path
      uses `UserConnections`. Cutover in Phase 2. Dispatch in
      `handleWebSocket`: `if ws.agentEnforceSingleSession {
      handleWebSocket_Agent(...) } else { handleWebSocket_Daemon(...) }`.
      Mode-2 body is a stub for this phase. Rename `cleanupAfterPanic`
      → `cleanupAfterPanicAgent`. Add `cleanupAfterPanicSession` as a
      thin wrapper that delegates to the existing logic for now. Sweep
      all 24 `[SP-046]` log lines (line numbers above) to
      `[SP-118-Mode1]`. Update type comment on `activeWSConn`
      (`websocket_handler.go:14-16`). Update
      `pkg/webui/multi_device_takeover.go:7-12` doc comment to
      reference SP-118 instead of non-existent SP-046-5.

      **Files:** `pkg/webui/server.go`, `pkg/webui/websocket_handler.go`
      (renames only — no logic change yet),
      `pkg/webui/multi_connection_registry.go` (new), plus 24
      single-line log tag edits, plus the takeover doc comment.

      **Acceptance:** `go test -race ./pkg/webui/...` byte-identical
      before/after. Existing `websocket_session_conflict_test.go` and
      `multi_tab_fanout_test.go` continue to pass with one new line in
      each test setup: `srv.agentEnforceSingleSession = true`.

### Phase 2: Wire `handleWebSocket_Daemon` behind flag

- [ ] **SP-118-2:** Implement `handleWebSocket_Daemon` body: connect
      registers into `UserConnections`, no conflict gate, no
      `waitForTakeover`. Same `chatSubscribers.Subscribe` + reattach
      flow as Mode 1. Wire `cleanupAfterPanicSession` to:
      remove conn from `UserConnections`; clear only this session's
      chat state (not all chats for clientID); clear cached agents
      for `clientID` only if `Count(userID) <= 1`. Add new config
      setting `daemon_multi_session` in
      `pkg/configuration/config_load_save.go`. Read at handler entry.
      Dispatch uses both `agentEnforceSingleSession` AND the flag
      value: `(agentEnforceSingleSession == false) && setting == true`
      → Mode 2; else Mode 1. Default OFF. First PR ships with the
      flag off. Second PR flips default to ON.

      **Files:** `pkg/webui/websocket_handler.go` (Mode-2 body),
      `pkg/webui/websocket_handler_daemon.go` (new, optional
      decomposition), `pkg/configuration/config_load_save.go`,
      `pkg/configuration/config_get.go`, settings UI.

      **Acceptance:** With `daemon_multi_session=false` (default),
      all existing tests pass byte-identically. With
      `daemon_multi_session=true` (override), Mode-2 tests
      (introduced in Phase 3) pass.

### Phase 3: New Mode-2 tests

- [ ] **SP-118-3a:** Add `pkg/webui/multi_connection_registry_test.go`
      (new) — unit tests for `UserConnections`: concurrent adds under
      one user, remove-by-pointer, count invariants, empty-slice
      cleanup after Remove.

- [ ] **SP-118-3b:** Add `pkg/webui/daemon_session_isolation_test.go`
      (new) — integration. Open two `gorilla/websocket` connections,
      each with distinct `clientID`. Publish a `stream_chunk` with
      `client_id=A`, `chat_id=X`. Assert the subscriber for chat X
      (connection B) receives it; assert a non-subscriber (third
      connection) does not.

- [ ] **SP-118-3c:** Add `pkg/webui/cleanup_after_panic_modes_test.go`
      (new) — trigger `cleanupAfterPanicSession` on connection A of
      `clientID=client-1` with a second connection B at the same
      clientID. Assert connection B's chat state is preserved
      (`Count(userID) > 1`).

- [ ] **SP-118-3d:** Verify `go test -race ./pkg/webui/...` passes
      with `agentEnforceSingleSession=true` AND
      `agentEnforceSingleSession=false`.

### Phase 4: Flip default to ON behind flag

- [ ] **SP-118-4:** Change `daemon_multi_session` default from `false`
      to `true` in `pkg/configuration/config_load_save.go`. Land as a
      separate commit in a separate PR. Watch metrics for one release
      cycle: `active_ws_count_by_user`, `panic_cleanup_scope_metric`,
      `chat_subscribers_count`. Rollback path:
      `sprout config set daemon_multi_session=false` is enough — no
      re-deploy.

### Phase 5: Metrics + diagnostics

- [ ] **SP-118-5:** Update `pkg/webui/api_diagnostics.go` to expose
      `active_ws_count_by_user` and the effective
      `daemon_multi_session` value via `sprout diagnose`. Add a
      runtime metric `ws_count_per_user` (counted at the connection
      registry level; one window with two tabs = two conns).

### Phase 6: Documentation + UI hint

- [ ] **SP-118-6:** Update `README.md` to document the new behavior
      ("daemon supports N parallel windows; `sprout agent` keeps
      single-active semantics"). Update the WebUI settings panel
      (`webui/src/components/settings/`) to show the current
      `daemon_multi_session` setting and link to the spec. Update
      `pkg/webui/multi_device_takeover.go:7-12` to mark the file as
      deprecated; open a follow-up issue to delete it (explicitly
      out of scope here).

### Acceptance (per spec)

- `go test -race ./pkg/webui/...` passes with both
  `agentEnforceSingleSession` flag values.
- New `daemon_session_isolation_test.go` passes: two windows
  under one user on the daemon each receive their own events.
- New `cleanup_after_panic_modes_test.go` passes: panic in
  window A does not invalidate window B.
- Existing `websocket_session_conflict_test.go` and
  `multi_tab_fanout_test.go` produce identical pass/fail
  before and after the refactor.
- Manual smoke: three browser windows on a real daemon, each
  with a distinct chat, each receiving independent events.

### Cross-refs

- SP-116 (Multi-Instance Isolation, in-flight). Different
  layer (config/instance registry vs WS session). Stacks
  naturally.
- SP-046 (archived, unrelated). Stays archived and
  untouched. The `[SP-046]` log tag sweep and the
  `multi_device_takeover.go` doc comment update are *not*
  part of "fixing SP-046" — they're cleaning up misattributed
  references in code under SP-118's logic.

---

## SP-119: Workspace-aware directory resolution in daemon-mode tools

_Spec: `roadmap/SP-119-workspace-aware-directory-resolution.md`. 🔵 Proposed — not yet started._

Discovered 2026-07-14 while debugging a `run_automate` failure in a
daemon-served workspace. Root cause: `pkg/automate.Dir()` (and the agent
tool handlers that call it) use `os.Getwd()` to resolve `automate/` —
correct when the user's shell CWD is the workspace (CLI usage), but wrong
in daemon mode where `os.Getwd()` is the daemon root (`/Users/alanp` per
`SPROUT_DAEMON_ROOT=1`). The WebUI HTTP API path
(`pkg/webui/automations_api.go`) already uses `ws.getAutomateDir(r)` which
is workspace-rooted — only the chat-invoked tool path is broken.

The same surface (`os.Getwd()` for workspace-relative paths) recurs in
~25+ other callsites across `pkg/agent/persistence.go`, `pkg/agent/skills.go`,
`pkg/agent_tools/shell_native.go`, etc. Each is a separate decision; this
spec fixes the most acute case and ships a discoverable helper so the
rest can migrate incrementally.

### Phase 1: `automate.DirIn` helper (~0.25 day)

- [ ] **SP-119-1:** Add `automate.DirIn(workspaceDir string) string`
      to `pkg/automate/discovery.go`. Returns
      `filepath.Join(workspaceDir, "automate")` when `workspaceDir` is
      non-empty; falls back to `Dir()` (CWD-based) when empty so the CLI
      behavior is unchanged. Update the godoc on `Dir()` to flag the
      daemon-mode caveat and point to SP-119 for the workspace-aware path.
      Add `TestDirIn` to `pkg/automate/discovery_test.go` covering:
      empty (falls back to cwd), whitespace-only workspace (falls back),
      absolute path, relative path. Acceptance:
      `go test ./pkg/automate/...` passes with the new cases.

- [ ] **SP-119-2:** Wire the three agent-tool callers in
      `pkg/agent/tool_handlers_automate.go` to `DirIn(a.GetWorkspaceRoot())`:

      - Line 124 (`handleRunAutomate`): `dir := automate.DirIn(a.GetWorkspaceRoot())`
      - Line 313 (`handleListAutomateWorkflows`): `dir := automate.DirIn(a.GetWorkspaceRoot())`
      - Line 365 (`workflowRequiresApproval`): `return WorkflowRequiresApprovalIn(automate.DirIn(a.GetWorkspaceRoot()), workflowName)`

      `a.GetWorkspaceRoot()` returns `a.workspaceRoot` (set by
      `NewAgentWithLayersInWorkspace` at `pkg/agent/agent_creation.go:318`).
      It does NOT fall back to `os.Getwd()` — when unset it returns `""`,
      which `DirIn` resolves to `Dir()` (CWD-based) — correct for CLI
      where the shell CWD IS the workspace.

- [ ] **SP-119-3:** Wire the interface-based registry handler at
      `pkg/agent_tools/list_automate_workflows_handler.go:45`. Same bug
      (chat-invokable through `pkg/agent_tools/all.go:84`), different
      accessor: `dir := automate.DirIn(env.WorkspaceRoot)`. Update the
      handler's godoc to reflect the workspace-aware behavior. Add
      `pkg/agent_tools/list_automate_workflows_handler_test.go` (3 cases:
      workspace-set with workflow, empty workspace, empty WorkspaceRoot
      fallback to CWD).

      Acceptance for the whole spec:
      - `go build ./...` clean.
      - `go test -race ./pkg/automate/...` no regressions.
      - `go test ./pkg/agent_tools/... -run TestListAutomateWorkflowsHandler` passes.
      - Manual repro 1: in a workspace served by `sprout service`,
        `run_automate todo-loop` from chat finds
        `automate/todo-loop.json` in the workspace (vs. "no
        automate/ directory found").
      - Manual repro 2: chat `list_automate_workflows` returns
        the workspace's workflows.
      - Manual cross-check: `sprout automate list` from the workspace
        shell still works (unchanged CLI path).

### Out of scope (follow-ups under SP-091 / individual specs)

The broader CWD-vs-workspace surface (≈25 callsites) is queued for a
follow-up audit. See SP-119 spec "Why this scope and not broader" for
the full list. Each is a separate decision based on whether the caller
has `ctx` (use `filesystem.WorkspaceRootFromContext(ctx)`), `*Agent`
(use `a.GetWorkspaceRoot()`), or no agent context at all (env var or
explicit injection).

---

## SP-091: Close the next round of roadmap gaps

_Tech-debt cleanup + finishing touches (~3–5 days)._ Each item below was
identified during the 2026-06-30 roadmap audit. They are the highest-value
remaining work in the existing spec set; new specs that emerge from usage
should be added as SP-092+ rather than expanding this list.

_Phases to be filled in as the runner picks them up._


---

## CLI-UX + WebUI Gap Audit (2026-07-05)

_Verified against code state on disk. Items below are concrete gaps
discovered during the audit. NO inline shipping from the audit pass —
queued for the runner per sp-009 isolation rules._

### Already-shipped-but-listed (cleanup)

These are already in the codebase; the TODO entries need their status
headers updated, but no new code work is required:

- [x] **AUDIT-SHIP-1:** Update `roadmap/SP-101-*.md` header for the
      four phases that audit confirmed shipped:
      - SP-101-Phase 1 (Terminal `handleProcessExit` 3-path logic)
      - SP-101-Phase 2 (NotificationCenter toast stack via `@sprout/ui`)
      - SP-101-Phase 4 (ToolTimelineBar WebUI + OutputRouter CLI)
      Mark each spec `**Status:** ✅ Implemented` in the spec header.
      _~0.25 day, metadata only._
      **SHIPPED 2026-07-05.** Investigation showed `roadmap/SP-101-*.md`
      does not exist — SP-101 is a placeholder spec referenced from
      SP-017 and SP-048 as the deferred-workstream name, never written
      as its own document. Each phase is independently tracked via
      commits (Phase 3 just shipped as `e7229794` Collapsible
      component). No spec header to update; the TODO entries and
      commit references ARE the historical record for SP-101.

- [x] **AUDIT-SHIP-2:** SP-094 partial. The retry-with-backoff
      foundation + `EventTypeRateLimited` event publishing
      (`pkg/agent/retry.go::ClassifyError` + `seed/core/retry.go
      ::doChatWithRetry` + `pkg/agent/seed_tool_registry.go:479
      ::PublishRateLimited`) is shipped. Update TODO.md's SP-094
      "Phase order" section to reflect this. The remaining ~512
      `fmt.Errorf` migration is still real work.
      **SHIPPED 2026-07-05.** Verified the three listed sites:
      - `pkg/agent/retry.go::ClassifyError` — present and exported.
      - `seed/core/retry.go::doChatWithRetry` — present.
      - `pkg/agent/seed_tool_registry.go:479::PublishRateLimited`
        — present (EventTypeRateLimited publishing wired).
      SP-094 "Phase order" section was already empty (the runner
      collapses empty sub-headers). Retry/backoff foundation is
      marked shipped via this TODO entry; the typed-error migration
      (~512 sites) remains genuine work tracked under the SP-094
      "Scope" section.

- [x] **AUDIT-SHIP-3:** SP-102 spec-status drift audit. The 6
      commits in `656db751` were re-audited this session and 3 of
      the 4 newly-merged specs that needed header updates have
      already been updated in subsequent commits. Remaining drift
      is captured by AUDIT-SHIP-1.
      **SHIPPED 2026-07-05.** This session extended the audit to
      all 5 specs still marked 🔵 Proposed:
      - SP-105 (CLI interactive panels) — verified NOT shipped
        (no `/settings` or `/usage` slash commands exist); status
        stays Proposed.
      - SP-107 (code intelligence graph) — verified shipped:
        auto-build trigger in `pkg/agent_tools/codegraph_handler.go:60`,
        embedding_index integration at `pkg/agent_tools/embedding_index_handler.go:267`,
        qualified-name edge bug fixed in `repo_map.go:ToCodegraphSymbols`.
        Status flipped to ✅ Implemented. Stale audit doc
        `SP-107-code-intelligence-graph-audit.md` deleted (its
        "feature is unreachable" verdict was correct as of 2026-07-04
        but the wiring was added between then and now).
      - SP-110 (background completion auto-resume) — verified
        Partially Implemented (Phases 1+2 shipped, Phase 3 auto-resume
        daemon poller missing). Status flipped to 🟡 Partially
        Implemented with rationale.
      - SP-112 (platform parity) — verified Not shipped (spec is
        1 day old, no implementation). Status stays Proposed.
      - SP-114 (unify command execution) — verified Not shipped
        (spec is 1 day old, no implementation). Status stays Proposed.

### Genuinely open (real code work)

- [x] **AUDIT-GAP-1:** SP-101-Phase 3 collapsible sections. Only the
      reasoning-block `<details>` in `ChatPanel.tsx:211` exists.
      No general-purpose `Collapsible` component in
      `packages/ui/src/components/`. Spec calls for collapsible
      sidebar sections, agent detail panels, terminal output
      blocks. _~1 day. Build a `Collapsible` component in
      `@sprout/ui`, then wire into the 2-3 most-used surfaces
      (Sidebar, Agent detail panel, ChatPanel tool output)._
      **SHIPPED 2026-07-05 at commit `e7229794`.** New
      `Collapsible` component at `packages/ui/src/components/
      Collapsible.tsx` (22 tests, 8 stories); migrated
      MessageItem, AdvancedSettingsTab (3 sections), EditApprovalPanel,
      ReviewWorkspaceTab (2 sections), and ChatPanel.

- [x] **AUDIT-GAP-2:** SP-103-D3 `VisionCapabilities` per-provider.
      Only the binary `SupportsVision()` flag exists on the
      `ProviderClient` interface (`pkg/agent_api/interface.go:30`).
      No `MaxImageBytes` / `MaxImageCount` / `MaxDimensions` /
      `DetailTiers` struct. Required to drive SP-103-B2 resize
      (currently picks a single 1536px cap regardless of provider)
      and SP-103-D2 batch splitting (needs to know each provider's
      image-count limit). _~0.5 day. Define struct in
      `pkg/agent_api/interface.go`; populate from model metadata
      for Anthropic (hard-coded table), OpenAI (from `image_url
      .detail` accepted values), Ollama local (from model
      manifest)._ **SHIPPED 2026-07-05 at commit `368f6dd0`.**

- [x] **AUDIT-GAP-3:** SP-098 stale audit table. The 2026-06-30
      file-over-800-lines table is stale — 11 of 13 originally
      listed files (`pkg/console/steer_input.go`, `pkg/lsp/semantic
      /go_adapter.go`, `pkg/agent_tools/structured_helpers.go`,
      etc.) have been split/renamed/removed since. Today's
      offenders (29 files >800 lines: `pkg/console/markdown_
      formatter.go` 1217, `pkg/configuration/config_risk_subagent
      .go` 1035, `pkg/webui/websocket_handler.go` 1008, etc.) are
      a different set. Refresh the SP-098 audit table to current
      state. _~0.25 day. Re-run `wc -l pkg/**/*.go` excluding
      tests, sort descending, take top 25, write the new table
      into TODO.md SP-098 section._
      **SHIPPED 2026-07-05.** Refreshed the SP-098 "Current
      state" table with the actual current top-25 ≥800-line
      files (29 total at or above 800), each with a concrete
      split recommendation. Most pre-2026-06-30 worst offenders
      have been split since the original spec was written
      (steer_input.go, structured_helpers.go, go_adapter.go,
      vision_types.go, status_footer.go are all gone from the
      list).

- [x] **AUDIT-GAP-4:** SP-099 + SP-100 still genuinely open. The
      audit confirms neither has shipped:
      - SP-099 (concurrency hardening, ~2 weeks, 3 phases)
      - SP-100 (WASM Tier 2a onnxruntime-web bridge, ~3 days)
      No code action from this audit; they remain properly
      queued at their existing TODO entries.


---

## SP-103: Vision Pipeline Reliability + Caching + Routing Fixes

_Most items completed by subsequent work. Verified against code state 2026-07-05._

### Remaining Work

#### SP-103-B2: Image Resizing

4K screenshots bill as ~4800 visual tokens. No resize logic exists. Providers have `max_image_width`/`max_image_height` or detail-tier settings (`low`/`high`/`auto` on OpenAI, `low`/`high` on Anthropic). Resize oversized images before embedding to cap token cost.

**What to build:**
- Add `resizeImageToMax(dim image.Dimensions, maxW, maxH int) []byte` using an existing image library (or a minimal Go implementation)
- Call it in `DownloadImage` and when preparing `ImageData` for `processImagesAsMultimodal`
- Cap at 1536px on the longest side (Anthropic's default for "auto" detail)

**Effort:** ~0.5 day. New helper in `vision_image.go` or `vision_utils.go`.

#### SP-103-A9: Typed Errors in Vision

`classifyPDFProcessingErrorCode` and `strings.Contains(errMsg, ...)` in `vision_image.go:418-440` stringify typed errors to classify them. `pkg/errors/types.go::TypedError` exists with `CodeVision*` constants. Migrate to typed error wrapping at the source.

**Effort:** ~0.5 day. Replace string-matching with `errors.As` checks against `TypedError`.

**Image path shipped (`e47280c7`); PDF path open.** `classifyPDFProcessingErrorCode` in
`pkg/agent_tools/vision_utils.go:160-196` still uses 6 `strings.Contains`
groups to map "download pdf / status 404 / stat pdf file / ocr request /
missing %pdf header / not a valid pdf" patterns. **Genuine follow-up**:
migrate the PDF classifier to typed errors too — narrow scope (~0.25
day) since `IsRemoteSizeExceededError` already covers the size-cap case
and most callers of PDF processing already wrap with typed errors. No
spec change; just mechanical migration.

#### SP-103-D1: Inline-Image Cost into Budget Tracker

When `processImagesAsMultimodal` embeds images into the chat message, per-image `image_tokens` / `cache_read_input_tokens` come back in the provider's chat response but are dropped before reaching `BudgetTracker.Deduct`. Bridge them so users see actual vision cost.

**Effort:** ~1.5 days. Touches `conversation.go`, provider response structs (`Anthropic`/`OpenAI`), and `pkg/budget/budget.go`.

#### SP-103-D2: Batch Splitting with Fallback

When a user pastes N images and the provider's vision context window is exceeded, the inline path fails with 400. Add automatic batch splitting: try inline; on vision-context overflow, split — keep first K images inline, call `analyze_image_content` for the rest.

**Effort:** ~1 day. New `vision_batch_split.go` helper.

#### SP-103-D3: Provider Vision-Capability Tables

`SupportsVision()` is a binary flag. Add a `VisionCapabilities` struct per provider (max image bytes, max image count, max dimensions, detail tiers). Use it to drive resize (B2) and batch splitting (D2).

**Effort:** ~0.5 day. New types in `pkg/agent_api/interface.go`, populated from model metadata.

### Rollout

B2 and A9 are quick wins (1 day combined). D1, D2, D3 are follow-ups that add value but aren't blocking.

### Acceptance

- `go test -race ./pkg/agent_tools/...` passes.
- A 4K pasted screenshot bills as ~1500 visual tokens (not ~4800).
- `classifyPDFProcessingErrorCode` uses `errors.As(*TypedError)` instead of `strings.Contains`.
- `make test-race` is a required CI check.

## SP-092: Persistent Recall via `/recall` and Cross-Turn Hints

_All three phases shipped (`cd528da3`, `e461c546`, `c22f2fc1`). See git
history for the spec body; archived to `roadmap/_completed/` once the
runner finishes moving spec files._

## SP-093: Edit Approval for Destructive Shell Commands

_All three phases shipped (`003b0a26`, `be521b02`, `1a6c0e12`). See git
history for the spec body; archived to `roadmap/_completed/` once the
runner finishes moving spec files._

---

## SP-094: Typed Error Hierarchy in `pkg/agent`

_Full migration of ~512 `fmt.Errorf` sites to typed errors (~1 week)._

**Foundation + retry/backoff shipped (`e2dd7276` etc., per AUDIT-SHIP-2).
Full tree NOT shipped.** `pkg/agent/errors.go` is 392 bytes with
just one sentinel (`errProviderStartupClosed`) — the ~250-line tree
called for by the spec (`RetryableError`, `RateLimitError`,
`AuthError`, `ContextCancelledError`, `InvalidInputError`,
`ToolError`, `ProviderError`, `FileSystemError`, `NetworkError`,
`WorkspaceError`, plus `Wrap()` helper) has not landed. The
`fmt.Errorf`-migration work (~512 sites across
`pkg/agent_tools/*_handler.go`, `pkg/agent/api_client*.go`,
`pkg/agent/subagent_*.go`, etc.) is genuine remaining work.

### Scope

**Define the full tree in `pkg/agent/errors.go` (~250 lines new):**
- `AgentError` (already exists in `pkg/errors/types.go` — extend, not
  duplicate).
- Categories: `RetryableError`, `RateLimitError`, `AuthError`,
  `ContextCancelledError`, `InvalidInputError`, `ToolError`,
  `ProviderError`, `FileSystemError`, `NetworkError`, `WorkspaceError`.
- Each implements `Error()`, `Unwrap()`, and `IsRetryable()` (bool).
- `Wrap(base error, msg string) error` helper that returns the right typed
  wrapper based on `errors.As`.

**Migrate sites in waves, each with tests:**
1. Tool handlers — `pkg/agent_tools/*_handler.go` (~80 sites). Each becomes
   `return errors.Wrap(err, InvalidInputError, "doing X")` etc.
2. Provider clients — `pkg/agent/api_client*.go` (~40 sites).
   `RateLimitError` for HTTP 429, `AuthError` for 401/403, `NetworkError`
   for transient connect failures.
3. Subagent + delegator — `pkg/agent/subagent_*.go` (~60 sites).
4. Remaining `pkg/agent/*.go` files (~330 sites).

**Wire classification into the broker:**
- `pkg/agent/approval_broker.go` — when an error is wrapped
  `ProviderError+RateLimitError`, trigger exponential backoff before
  propagating to the user.
- `pkg/agent/metrics.go` — emit a label per error category so the
  cost/status footer can show "rate-limited, retrying…" distinct from
  "provider error".
- `pkg/agent/seed_provider.go::ChatStream` — when the streaming
  response yields `RateLimitError`, surface "rate-limited, retrying"
  to the WebUI event bus with a new `EventTypeRateLimited` event.

**`sprout explain <hash>` integration (SP-068):**
- The existing explain flag should now classify errors via the typed
  hierarchy: `RateLimitError` → "retry-safe (n retries so far)",
  `AuthError` → "re-auth required", etc. instead of raw stack traces.

### Phase order


### Pre-SP-095 cleanup (test isolation + subagent routing)


### Acceptance

- `grep -rn "fmt.Errorf" pkg/agent --include="*.go" | wc -l` returns
  a number ≥80% smaller than today (some sites are legitimate format-and-
  wrap; the goal is removing the untyped ones).
- Every entry in `pkg/errors/types_test.go` passes.
- Provider 429 now triggers 1-2 automatic retries with backoff instead of
  surfacing as a hard failure.

---

## Things to consider after SP-091 → SP-095 ship

- **WASM stub-tools** — running the WASM build against `pkg/agent_tools/`
  with CGO-only handlers stubbed (grammar embed + static-embed removal
  shipped per SP-058/SP-061; remaining work is handler-stub coverage).
- **Subagent webui panel** — there's an active conversation indicator but
  no per-subagent detail view; SP-051 shipped depth in CLI but not WebUI.
- **Multi-workspace sprout** daemon — feature requested twice in the past
  month.

---

## SP-096: Roadmap status sync (full audit + 14 spec-header fixes)

_All 14 spec-header fixes shipped at commit `81ec1f87`. Three specs
remain genuinely Proposed (SP-105, SP-112, SP-114) — all 1-day-old
specs with no implementation. See git history for the original spec
body; archived to `roadmap/_completed/` once the runner finishes
moving spec files._

---


## SP-098: SP-075 Large-File Decomposition — Second Pass

_~1 week, 5-7 phases._ Most of SP-075's original worst offenders were
already split since the spec was written (2026-06-15): `cmd/agent_modes.go`
2344 → 732 lines, `pkg/configuration/config.go` 2833 → 388, etc. But a
new batch of files has grown large. The runner should split these in
priority order.

### Current state (2026-07-05 audit — refresh)

| File | Lines | Recommendation |
|---|---|---|
| `pkg/console/markdown_formatter.go` | 1217 | Split: `markdown_table.go` (table rendering, ~400 lines: formatMarkdownLine / flushTable / parseTableRow / renderTable / clampColumnWidths / padCell / truncateDisplay) + `markdown_highlight.go` (syntax highlighting, ~200 lines: highlightGo / Python / Bash / JSON / YAML / JavaScript / TypeScript / Generic). Keep core Format() loop in place. |
| `pkg/configuration/config_risk_subagent.go` | 1035 | Split into `risk_heredoc.go` (Heredoc/quote stripping, lines 22-170), `risk_profile.go` (DefaultAutoApproveRules / IsValidRiskProfile / ResolveRiskProfileRules, lines 171-371), `risk_classify.go` (critical-operation detection + categorizeCommand / isReadOnlyCmd / isBuildTestCmd, lines 372-1000). |
| `pkg/webui/websocket_handler.go` | 1008 | Split: `websocket_conn.go` (handleWebSocket + connection lifecycle: waitForTakeover / evictExistingConnection / cleanupAfterPanic, lines 34-830) + `websocket_message.go` (handleWebSocketMessage + handleSyncRecoverMessage + shouldForwardEventToConnection, lines 427-980). |
| `pkg/configuration/manager.go` | 949 | Split: `manager_load.go` (loadConfigSilently / NewManager* constructors / LoadConfigWithLayers, lines 28-348) + `manager_save.go` (saveConfigLocked / saveConfigDirectLocked / SaveConfig / SaveAPIKeys / Reload, lines 375-600) + `manager_provider.go` (GetProvider / SetProvider / GetModelForProvider, lines 601+). |
| `pkg/agent_api/ollama_local.go` | 940 | Split per-feature: `ollama_models.go` (model listing / pulling / manifest parsing), `ollama_chat.go` (ChatCompletion streaming), `ollama_embed.go` (embeddings endpoint). |
| `pkg/agent/seed_tool_registry.go` | 926 | Per SP-109-2/3 split: tool descriptions by domain (file / shell / subagent / search / vision / network) into separate `tool_registry_*.go` files. |
| `pkg/webui/chat_sessions_api.go` | 920 | Split: `chat_sessions_api.go` (CRUD: list / get / create / delete / patch) + `chat_sessions_messages.go` (message pagination / append / search) + `chat_sessions_search.go` (full-text search / filter). |
| `pkg/filediscovery/filediscovery.go` | 897 | Split by phase: `filediscovery_walk.go` (filesystem walker), `filediscovery_filter.go` (gitignore / extension / size filters), `filediscovery_index.go` (in-memory index + query). |
| `pkg/agent/agent_getters.go` | 886 | Split: most getters are simple field accesses (~400 lines), but the heavy ones (currentSession / state snapshot / persona lookup) deserve their own files: `agent_session_getters.go` + `agent_state_getters.go`. |
| `pkg/agent/tool_security.go` | 873 | Split: `tool_security_policy.go` (policy evaluation: IsAllowed / IsRestricted / RiskClassification) + `tool_security_paths.go` (path normalization + sandbox checks) + `tool_security_audit.go` (audit log emission). |
| `pkg/webui/ssh_launch.go` | 867 | Split: `ssh_launch_config.go` (config struct + validation) + `ssh_launch_exec.go` (subprocess spawn + connection tracking) + `ssh_launch_api.go` (HTTP handlers / status). |
| `pkg/providerregistry/registry.go` | 865 | Split: `registry_models.go` (model metadata + capabilities) + `registry_providers.go` (provider registration + lookup) + `registry_aliases.go` (alias resolution). |
| `pkg/credentials/encrypt.go` | 861 | Split: `encrypt_aes.go` (AES-GCM encrypt / decrypt) + `encrypt_keyring.go` (keyring backend integration) + `encrypt_migrate.go` (legacy plaintext → encrypted migration). |
| `pkg/events/events.go` | 857 | Split: `events_types.go` (event type / payload definitions) + `events_bus.go` (publish / subscribe / once) + `events_filter.go` (filter / throttle / dedupe). |
| `pkg/embedding/manager.go` | 853 | Split: `embedding_models.go` (model registry + capability lookup) + `embedding_batch.go` (batch embedding + queue) + `embedding_cache.go` (LRU + persistence). |
| `pkg/agent/change_tracking.go` | 850 | Per SP-077 split: `change_tracking_record.go` (record change) + `change_tracking_revert.go` (revert / recover) + `change_tracking_persist.go` (disk persistence + snapshot management). |
| `pkg/agent_tools/background_process.go` | 848 | Split: `background_process.go` (lifecycle: start / stop / status) + `background_process_log.go` (log streaming + truncation) + `background_process_pty.go` (PTY allocation + signal forwarding). |
| `pkg/agent/submanager_state.go` | 848 | Split: `submanager_state.go` (state machine + transitions) + `submanager_persist.go` (snapshot / restore) + `submanager_query.go` (status queries). NOTE: also listed in **StateManager interface refactor** below — the file is large AND the StateManager interface (28 sub-interfaces) and concrete `AgentStateManager` (all-in-one struct) need to be split into focused sub-managers (security / output / mcp) that wrap smaller interfaces. That refactor is a separate, ~2-week effort. |
| `cmd/mcp_add.go` | 847 | Split per-tool: `mcp_add.go` (add command) + `mcp_list.go` (list) + `mcp_remove.go` (remove) — already partially split, this file may consolidate; check. |
| `pkg/history/changetracker.go` | 843 | Already split some helpers; remaining bulk is per-action methods. Split: `changetracker_record.go` (Record*) + `changetracker_revert.go` (Revert / handleRevisionRollback + staleness guard per SP-077) + `changetracker_persist.go` (disk write / sweepCommittedSnapshots). |
| `pkg/agent/persistence.go` | 843 | Split: `persistence_session.go` (session save / load) + `persistence_message.go` (message append / truncate) + `persistence_index.go` (full-text index). |
| `pkg/webui/settings_api_mcp.go` | 841 | Split: `settings_api_mcp.go` (list / get / set MCP servers) + `settings_api_mcp_test.go` (connection test endpoint) + `settings_api_mcp_oauth.go` (OAuth flow if present). |
| `pkg/console/select_list.go` | 840 | Already partially split; remaining bulk is per-prompt-mode logic. Split: `select_list.go` (core SelectList type + Render) + `select_list_filter.go` (filter / search / fuzzy match) + `select_list_keymap.go` (keybindings — already exists as `input_keymap.go`). |
| `pkg/agent_tools/security_classifier.go` | 834 | Split: `security_classifier.go` (command classification: shell patterns) + `security_classifier_path.go` (path traversal / sensitive dir checks) + `security_classifier_shell_patterns.go` (pattern table — may already exist; verify). |
| `pkg/agent/scripted_playback.go` | 832 | Split: `scripted_playback.go` (test playback engine) + `scripted_record.go` (record session to script) + `scripted_assert.go` (assertion framework). |

Total: 25 files ≥800 lines. Additional files 800-797 lines are borderline
(`pkg/agent_tools/repo_map.go` 801, `pkg/agent/workspace_sync.go` 797);
these can be folded into the same phase work but are not strictly above
the 800-line threshold. The pre-2026-06-30 worst offenders from the
original SP-075 list (`steer_input.go` 1536, `go_adapter.go` 1188,
`structured_helpers.go` 1190, etc.) are no longer in the top 25 — most
have been split by SP-075 / Refactor-A work.

### Phase order (each ~0.5 day)


### Acceptance

- Every targeted file ends under 800 lines.
- `go build ./...` clean after each extraction (per AGENTS.md
  refactoring protocol).
- All existing tests in each split file's package continue to pass.

---

## SP-099: SP-008 Track A — Concurrency Hardening

_All three phases shipped (`e2dd7276` + `076c0ecf`). `Makefile:80` has
`TEST_RACE ?= -race`; `docs/adr-0007-locking-strategy.md` exists;
`go test -race ./...` is green on CI. See git history for the spec
body; archived to `roadmap/_completed/` once the runner finishes
moving spec files._


---

## SP-100: SP-045 WASM Parity — Tier 2a (onnxruntime-web bridge)

_~3 days, 2 phases._

**NOT shipped (verified open 2026-07-06).** The two artifacts called
for by the spec do not exist:

- `cmd/wasm/embedding_funcs.go` — **missing**. `cmd/wasm/` contains
  `embedding_support.go`, `chat_funcs.go`, `llm_funcs.go`, etc.,
  but no `embedding_funcs.go`. There is no
  `switchEmbeddingBackend` / `embeddingBackendStatus` /
  `embeddingModel = "gemma-300m"` symbol on disk.
- `webui/public/wasm/onnxruntime-web-loader.js` — **missing**.
  Only `webui/public/wasm/sprout.wasm` exists in that directory.

This is genuine remaining work — Tier 1 + Tier 2 (per SP-045 §6) are
shipped; Tier 2a (the onnxruntime-web bridge surfaced into the
WASM JS API) is still open. Spec body preserved verbatim per
sp-009 isolation rules.

### Scope

Phase 1: surface the existing bridge as `SproutWasm.embedding*`.

**Edit `cmd/wasm/embedding_funcs.go`:**
- Add `SproutWasm.embeddingModel = "gemma-300m"` constant + load
  helper that resolves the right asset path.
- Add `SproutWasm.switchEmbeddingBackend(name string)` — switches
  between `static` and `onnx-web`.
- Add `SproutWasm.embeddingBackendStatus() { backend, model,
  dimensions, ready }`.

Phase 2: lazy-load the onnxruntime-web bundle.

**New `webui/public/wasm/onnxruntime-web-loader.js` (~80 lines):**
- Detects the active backend; only injects `<script src=onnxruntime-web>`
  if `onnx` is selected.
- Caches the promise so the second call reuses the resolved runtime.
- Falls back to static with a console warning if the network blocks the
  script.

### Phase order


### Acceptance

- `SproutWasm.switchEmbeddingBackend("onnx")` resolves, fires the
  lazy-load, and the next `searchSemantic` call uses ONNX vectors.
- Default remains `static` so existing WASM users see no change.
- A test asserts the loader is not fetched when backend is `static`.

---

## SP-101: Partial-spec gap fills (SP-011, SP-012, SP-017, SP-048)

_~3 days, 4 phases._

**SHIPPED 2026-07-06.** All four phases landed:

- **Phase 1 (SP-011 P1.4 terminal exit-pane)** — `2882f4db
  fix(terminal): delay last-session restart by 1.5s and cover exit
  paths`. `webui/src/components/Terminal.tsx:372 ::handleProcessExit`
  implements the 3-path logic (paths 1 & 3 immediate; path 2 — only
  pane AND only session — defers via `exitRestartTimerRef` 1500ms
  before `handlePaneExit`).
- **Phase 2 (SP-012 notification center)** — `efc46320 feat(webui):
  add notification center with top-right stack and auto-dismiss`
  and `33c34b70 feat(notifications): add markAllRead control event`.
  `webui/src/components/NotificationCenter.tsx` mounted in
  `webui/src/components/StatusBar.tsx:8`; `packages/ui/src/
  services/notificationBus.ts` decouples publishers from the toast
  stack.
- **Phase 3 (SP-017 collapsible)** — `e7229794 feat(ui): add
  Collapsible component and migrate 5 sites` (also captured in
  AUDIT-GAP-1 above).
- **Phase 4 (SP-048 tool timeline)** — `webui/src/components/chat/
  ToolTimelineBar.tsx` + `ToolTimelineBar.css` + test file; CLI side
  is `pkg/agent/seed_tool_registry.go`'s `OutputRouter
  .RouteToolCompletion` (also confirmed by reconciliation commit
  `205fb580`).

Per the cleanup commit `6018cf3c chore(todo): mark sp-101 acceptance
items complete`, the acceptance list has been verified against
current code state. Spec body preserved verbatim per sp-009
isolation rules.

### Phase 1: SP-011 — terminal exit-pane handling polish (~0.5 day)

The Phase 1 fixes (P1.1 `onProcessExit`, P1.2 per-pane session model,
P1.3 split-mode button cleanup) are largely shipped. What's pending is
**P1.4** — the parent Terminal's cleanup logic when `onProcessExit`
fires (auto-close secondary split pane; auto-create a fresh session
after 1.5s if last; close tab + switch to next if multi-tab). Currently
the callback exists in TerminalPane but Terminal.tsx may not handle all
the cases.

**Verify and finish:**

### Phase 2: SP-012 — notification center (~1 day)

README says "notification center pending". SP-012 Phase 1 calls for a
non-blocking toast/snackbar UI for system messages (rate-limit warnings,
auth failures, agent blocked on permission, etc.). Right now those
messages go to the in-terminal `PublishAgentMessage` stream and risk
clobbering input state (cf. the recent fix in `10a9cbd5 fix(agent):
route security cautions via event bus`).


### Phase 3: SP-017 — collapsible sections (~1 day)

README says "scoped labels shipped; collapsible sections pending".


### Phase 4: SP-048 — tool execution timeline (~0.5 day)

README says "tool timeline + silence-fill pending". The silence-fill
part is covered by SP-091-4. Remaining: tool timeline — render
`PublishToolStart` / `PublishToolEnd` events as a vertical timeline in
the terminal output.


### Acceptance


---

## SP-102: Drift audit for newly-merged specs (post-merge verification)

_~0.5 day._

**SHIPPED 2026-07-06 (per AUDIT-SHIP-3).** The audit pass expanded
beyond the original `656db751` re-sync and re-audited all 5 specs
that were still marked 🔵 Proposed:

- SP-105 — verified NOT shipped (partial: SettingsCommand +
  UsageCommand are registered but the full panel UX is not built).
  Stays Proposed.
- SP-107 — verified shipped; status flipped to ✅ Implemented
  (`codegraph_handler.go:60` auto-build trigger,
  `embedding_index_handler.go:267` integration,
  `repo_map.go:ToCodegraphSymbols` edge fix). Stale audit doc
  `SP-107-code-intelligence-graph-audit.md` deleted.
- SP-110 — verified Partially Implemented (Phases 1+2 shipped,
  Phase 3 auto-resume daemon poller missing).
- SP-112 — verified NOT shipped (1-day-old spec). Stays Proposed.
- SP-114 — verified NOT shipped (1-day-old spec). Stays Proposed.

No further drift to fix. The remaining "Proposed" set (SP-105,
SP-112, SP-114) is the genuinely-open backlog.

---

## Things to consider after SP-091 → SP-095 ship
- Acceptance: `get_callers`/`get_callees` return correct results for
  Go code in this repo; `find_dead_code` runs in < 100ms; `repo_map`
  output is unchanged but returns in < 50ms on warm cache.

---

## SP-111: TODO Loop Workflow — Hardening

_The native TODO loop workflow (`automate/todo-loop.json`) is functional but
has known gaps. These are follow-ups, not blockers._

**SHIPPED 2026-07-06.** Both listed items were already marked
`[x]` (AUDIT-GAP-4 commit `7acc5b7e` for SP-111-3; commit `ed9c3260`
for SP-111-4). No further items exist in the section — items 1 + 2
were retired by the original scope. Spec body preserved for git
history continuity per sp-009 isolation rules.

### Items

- [x] **SP-111-3:** Checkpoint/resume for crash recovery. The steps workflow
      persists state via `persistWorkflowCheckpoint`. The loop should do
      the same — save the current TODO line number so a restarted run
      picks up where it left off instead of starting over.
      **SHIPPED 2026-07-05.** `persistLoopCheckpoint`/`loadLoopCheckpoint`/
      `removeLoopCheckpoint` in `cmd/agent_workflow_runtime.go` already
      wired into `runAgentWorkflowLoop` for checkpoint/resume across
      budget interrupts, context cancellation, and item completion.
- [x] **SP-111-4:** Fix `run_automate` BPM process detachment. The
      `BackgroundProcessManager` uses `Setpgid` but processes still die
      when the agent tool call completes. Investigate whether stdin
      inheritance or session group teardown is the cause. `nohup` works
      as a workaround.
      **SHIPPED 2026-07-05 at commit `fe885faa`.** Root cause: Go
      1.24+ Linux seccomp blocks `setsid(2)` from child processes,
      forcing the BPM fallback to Setpgid-only. Children stayed in
      the parent's session and received SIGHUP from terminal
      teardown. Fix: `signal.Ignore(syscall.SIGHUP)` in the parent
      guarded by `sync.Once`, called only on the Setpgid fallback
      path. Children inherit the SIG_IGN disposition at fork time
      (same mechanism as `nohup`). The Setsid path is unchanged
      because a new session already isolates the child.
      Implementation in `pkg/agent_tools/background_process_signal_unix.go`;
      6 new tests in `background_process_signal_unix_test.go`.

---

## SP-115: StateManager interface refactor (28 sub-interfaces → focused sub-managers)

_Surfaced from the 2026-07-08 audit (not from the agent's "what's next?" prompt — the audit passed). Surface-only per `audit-surface-vs-implement` memory: do not ship inline from the audit pass; the runner owns the implementation surface._

_~2 weeks, 3 phases. Big refactor — see "Why" below._

### Why

The `StateManager` interface in `pkg/agent/submanager_state.go:14` already
decomposes into **28 sub-interfaces** (e.g. `SecurityStateProvider`,
`MCPSubManagerState`, `OutputManagerState`, `PlanStateManager`, etc.) —
good surface design. But the concrete `AgentStateManager` struct
(`submanager_state.go:46`, ~700 lines) **implements all 28 in one
monolithic struct**. The mismatch means:

- Every field on `AgentStateManager` is reachable from any of the 28
  interfaces, defeating interface-based separation of concerns.
- ~625 callsites across 64 files depend on `*StateManager` (or one of
  the sub-interfaces, which is satisfied by the same struct). Changing
  the struct's internal layout ripples through the entire call graph.
- Tests like `submanager_state_new_test.go` (541 lines) and
  `submanager_state_session_test.go` (166 lines) all build the whole
  struct, even when only one sub-interface is exercised.

### What to build

Split `AgentStateManager` into **focused sub-managers** that each own
one logical domain and **wrap** smaller sub-interfaces. Each
sub-manager is a struct with only the fields it actually needs.

**Phase 1: extract sub-manager structs (~1 week)**
- `SecuritySubManager` — wraps `SecurityStateProvider`. Owns the
  security/circuit-breaker/policy state. Includes the security tool
  approval/denial state.
- `OutputSubManager` — wraps `OutputManagerState`. Owns the agent
  output stream, the silence-fill timer, the message-routing table.
- `MCPSubManager` — wraps `MCPSubManagerState`. Owns the MCP server
  registry, the per-server tool index, the transport-failure counter.
- `PlanSubManager` — wraps `PlanStateManager`. Owns the plan steps,
  the todo list, the plan approval state.
- `SessionSubManager` — wraps session/conversation state. Owns the
  message buffer, the turn counter, the persistence scope.
- `AgentStateManager` — becomes a **facade** holding the
  sub-managers and forwarding calls. Existing 28 interfaces continue
  to work; the facade just routes to the right sub-manager.

**Phase 2: migrate callsites (~0.5 week)**
- Audit each of the 64 callers. For each one:
  - If it only needs ONE sub-interface (e.g. only security state), change
    its `*StateManager` field to the specific sub-manager type.
  - If it needs multiple sub-interfaces, keep `*StateManager` (the
    facade satisfies them all).
- For tests that only exercise one sub-interface, change the test's
  setup to build only the relevant sub-manager.
- Estimated breakdown: ~250 callsites need to switch to a focused
  sub-manager; ~375 keep the facade. Migration is mechanical
  (`s.state.X` → `s.security.X` etc.).

**Phase 3: document and verify (~0.5 week)**
- Update `submanager_state.go` docstring to list the sub-managers and
  which interface each implements.
- Add an `AgentStateManager` godoc that says "facade; prefer the
  focused sub-manager if you only need one domain."
- Run `go test -race ./...` and `go test -count=1 ./pkg/agent/...`.
  No regressions; the existing 28 sub-interfaces all still work.

### Sub-managers to keep on the facade (do NOT extract)

A few state domains are tightly coupled and would be artificial to
split:
- The agent's `Status` (idle / running / blocked) and the security
  circuit-breaker's status need to flip together (a "blocked on
  security" status is one combined state, not two). Keep on facade.
- The persistence scope (which user/session this state belongs to) is
  read by every sub-manager. Keep on facade.

### Phase order

- [ ] **SP-115-1:** Extract `SecuritySubManager`, `OutputSubManager`,
      `MCPSubManager`, `PlanSubManager`, `SessionSubManager`. Make
      `AgentStateManager` a facade that holds them and delegates.
      `go build ./...` and `go test ./pkg/agent/...` clean. ~1 week.
- [ ] **SP-115-2:** Migrate callsites. Grep
      `*StateManager`/`s.state.` and rewrite each to use the focused
      sub-manager where the callsite only needs one domain. ~0.5 week.
- [ ] **SP-115-3:** Update docstrings, run `go test -race`, verify no
      regressions in 28-interface callers. ~0.5 week.

### Acceptance

- `AgentStateManager` is a thin facade: contains only the 5
  sub-manager fields + the 2 "keep on facade" fields (status,
  persistence scope). Total < 100 lines.
- Each sub-manager is its own struct in `submanager_state_*.go` with
  focused tests (`submanager_state_<name>_test.go`).
- 28 sub-interfaces in `submanager_state.go` unchanged — the
  refactor is internal.
- All existing tests pass; the runner can ship a focused
  sub-manager change in 1 file instead of touching the 700-line
  monolith.

---

## AUDIT-UF-2026-07-12: User Flows & Functionality Audit

**Date**: 2026-07-12 | **Scope**: End-to-end user flows, broken steps, missing
error handling, UX gaps | **Method**: Subagent trace of CLI, WebUI, WASM,
MCP, subagent, auth, billing, task, and mode flows.

### 🔴 Critical (Flow is broken)

✅C1:** `browse_url` always errors in browser/WASM mode.
      `nopRenderer` returns "browser rendering not available" with no
      client-side fallback. Cloud agent cannot inspect web pages.
      **Files**: `pkg/webcontent/browser_none.go:166-188`,
      `pkg/agent_tools/browse_url_handler.go:58-60`

✅C2:** `run_automate` + `create_pull_request` wired in WASM
      but call host-only code (no `!js` build guard). Either errors
      opaquely or hangs when invoked in browser.
      **Files**: `pkg/agent/agent_tool_wiring.go:55-62`,
      `pkg/agent/tool_handlers_automate.go:119`

✅C3:** Chat sessions don't persist across page loads in
      cloud/browser mode. `/api/sessions` returns `[]`, `/api/sessions/restore`
      returns error. Refresh loses entire conversation.
      **Files**: `webui/src/services/cloudEndpointRegistry/endpoints/synthetic.ts:224-236`,
      `webui/src/hooks/useAppInitialization.ts:246,259`

### 🟠 High (Major UX gap)

✅H1:** Four guided MCP setup functions (GitHub, Playwright,
      Chrome DevTools, git) are dead code — never dispatched from
      `runMCPAdd`. Users only get generic template picker.
      **Files**: `cmd/mcp_add.go:196,295,532,629` (defs); dispatcher only
      uses `setupCustomMCPServer`

✅H2:** Browser git operations are unreachable
      (`supportsGit=false` never flips) AND incomplete (unstage/reset return
      fake success). ~800 lines of dead code. Missing ops: pull, pull-request,
      discard, revert, commit-message.
      **Files**: `webui/src/services/browserGit.ts:337-379`,
      `webui/src/config/mode.ts:66`, `webui/src/services/cloudAdapter.ts:37`

✅H3:** Subagents have no default timeout. A stuck/hung
      subagent blocks the primary indefinitely. Only cancellation is
      parent context — no per-subagent deadline.
      **Files**: `pkg/agent/subagent_task.go:50-55`,
      `pkg/agent/subagent_runners.go`

✅H4:** Screenshot path has no VFS/image-attach plumbing in
      WASM — even if browser existed, file goes nowhere and model never
      receives the image.
      **Files**: `pkg/agent_tools/browse_url_handler.go:65-70`,
      `pkg/webcontent/browse.go:33-43`,
      `pkg/agent_tools/browse_url_handler_image_nonjs.go`

✅H5:** `/rewind` interactive prompt uses raw
      `bufio.NewReader(os.Stdin)` which fights the REPL terminal state
      (cooked mode, scroll regions, status footer).
      **Files**: `pkg/agent_commands/rewind_command.go:62-72`

### 🟡 Medium (Minor gap)

✅M1:** PR dialog collects no "head" branch field — always
      uses current branch. API contract supports `head` but UI omits it.
      **Files**: `webui/src/components/git/GitPRDialog.tsx`,
      `webui/src/services/api/gitApi.ts:232-252`

✅M2:** `handleSendMessage` decrements `activeRequestsRef` only
      in `catch`, not on success. Under flaky WS conditions, next message
      may be misrouted to steer branch.
      **Files**: `webui/src/hooks/useMessageSending.ts:96-129`

✅M3:** Semantic search silently returns `[]` in cloud mode
      with no "unavailable" signal. User assumes no matches.
      **Files**: `webui/src/services/cloudEndpointRegistry/endpoints/synthetic.ts:133-135`,
      `webui/src/components/SearchView.tsx:389`

✅M4:** Embedding-index POST toggle has no synthetic handler
      in cloud mode. Falls through to backend proxy → 401/404. Silently
      no-ops.
      **Files**: `webui/src/components/ChatView.tsx:273-289`,
      `webui/src/services/cloudEndpointRegistry/endpoints/synthetic.ts:160-165`

✅M5:** MCP "Test Connection" exists in CLI but missing from
      WebUI settings. Misconfigured servers discovered only mid-turn.
      **Files**: `webui/src/components/settings/MCPSettingsTab.tsx`

✅M6:** CLI startup config failure exits with WebUI-only
      remediation hint. Pure-CLI users get no `keys set` guidance.
      **Files**: `cmd/root.go:146-156`

✅M7:** Parallel-subagent-disabled error surfaced as tool
      failure, not user guidance. User sees cryptic failure.
      **Files**: `pkg/agent/tool_handlers_subagent_spawn.go:418-422`

### 🟢 Low (Polish)

✅L1:** Onboarding manual provider entry uses raw `bufio`
      stdin (empty-catalog fallback). Rare but terminal-state-inconsistent.
      **Files**: `cmd/onboarding.go:184-199`

✅L2:** First-run hint reprints forever if `~/.sprout/state.json`
      is unwritable. No diagnostic.
      **Files**: `cmd/first_run_hint.go:55`

✅L3:** Embedding-index toggle failure logs to `console.error`
      only — no toast, no state rollback.
      **Files**: `webui/src/components/ChatView.tsx:283-288`

✅L4:** "No git in browser mode" empty state in Sidebar is
      unreachable dead code (tab is filtered out).
      **Files**: `webui/src/components/Sidebar.tsx:313-318`

✅L5:** GitHub PAT "add to shell profile" prompt only prints
      instructions, doesn't edit the file.
      **Files**: `cmd/mcp_add.go:446-456`

### Cross-cutting observations

1. **Cloud/Mode C is the weakest surface.** Nearly every "not available
   in browser mode" stub silently returns empty rather than surfacing
   unavailability (C1, C3, M3, M4, L3).
2. **Browser git subsystem (~800 lines) is effectively dead** because
   `supportsGit=false` is never toggled (H2, L4).
3. **Terminal-state discipline is inconsistent** — codebase has
   infrastructure for REPL-safe prompts but `/rewind` and onboarding
   fallback bypass it (H5, L1).

