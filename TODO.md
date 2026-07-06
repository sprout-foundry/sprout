# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

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

- [ ] **AUDIT-SHIP-1:** Update `roadmap/SP-101-*.md` header for the
      four phases that audit confirmed shipped:
      - SP-101-Phase 1 (Terminal `handleProcessExit` 3-path logic)
      - SP-101-Phase 2 (NotificationCenter toast stack via `@sprout/ui`)
      - SP-101-Phase 4 (ToolTimelineBar WebUI + OutputRouter CLI)
      Mark each spec `**Status:** ✅ Implemented` in the spec header.
      _~0.25 day, metadata only._

- [ ] **AUDIT-SHIP-2:** SP-094 partial. The retry-with-backoff
      foundation + `EventTypeRateLimited` event publishing
      (`pkg/agent/retry.go::ClassifyError` + `seed/core/retry.go
      ::doChatWithRetry` + `pkg/agent/seed_tool_registry.go:479
      ::PublishRateLimited`) is shipped. Update TODO.md's SP-094
      "Phase order" section to reflect this. The remaining ~512
      `fmt.Errorf` migration is still real work.

- [ ] **AUDIT-SHIP-3:** SP-102 spec-status drift audit. The 6
      commits in `656db751` were re-audited this session and 3 of
      the 4 newly-merged specs that needed header updates have
      already been updated in subsequent commits. Remaining drift
      is captured by AUDIT-SHIP-1.

### Genuinely open (real code work)

- [ ] **AUDIT-GAP-1:** SP-101-Phase 3 collapsible sections. Only the
      reasoning-block `<details>` in `ChatPanel.tsx:211` exists.
      No general-purpose `Collapsible` component in
      `packages/ui/src/components/`. Spec calls for collapsible
      sidebar sections, agent detail panels, terminal output
      blocks. _~1 day. Build a `Collapsible` component in
      `@sprout/ui`, then wire into the 2-3 most-used surfaces
      (Sidebar, Agent detail panel, ChatPanel tool output)._

- [ ] **AUDIT-GAP-2:** SP-103-D3 `VisionCapabilities` per-provider.
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

- [ ] **AUDIT-GAP-3:** SP-098 stale audit table. The 2026-06-30
      file-over-800-lines table is stale — 11 of 13 originally
      listed files (`pkg/console/steer_input.go`, `pkg/lsp/semantic
      /go_adapter.go`, `pkg/agent_tools/structured_helpers.go`,
      etc.) have been split/renamed/removed since. Today's
      offenders (27 files >800 lines: `pkg/console/markdown_
      formatter.go` 1217, `pkg/configuration/config_risk_subagent
      .go` 1035, `pkg/webui/websocket_handler.go` 1008, etc.) are
      a different set. Refresh the SP-098 audit table to current
      state. _~0.25 day. Re-run `wc -l pkg/**/*.go` excluding
      tests, sort descending, take top 25, write the new table
      into TODO.md SP-098 section._

- [ ] **AUDIT-GAP-4:** SP-099 + SP-100 still genuinely open. The
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

_Surfacing past sessions on demand (~1–2 days)._ All the backend work is
already shipped: `pkg/agent/semantic_recall.go` exposes
`Agent.InjectSemanticRecall(ctx, query)`, `pkg/agent/memory_embedding.go` and
`pkg/embedding/conversation_store.go` persist per-checkpoint summaries, and
`seed_query.go::InjectSemanticRecall` is already wired into the conversation
loop. What's missing is the proactive surface — a CLI command and a webUI
"past-session hints" panel for when the user is *not* in a turn.

### Scope

**CLI surface (`pkg/agent_commands/recall_command.go`, new file):**
- `Register` in `pkg/agent_commands/commands.go` alongside the existing
  `/sessions`, `/rewind`, etc.
- Accept `<free-text query>` plus `--limit N` (default 5) and `--json`.
- Call `Agent.Recall(ctx, query, limit)` — needs to be added to
  `pkg/agent/semantic_recall.go` by extracting the body of
  `InjectSemanticRecall` up to the format-and-append step.
- Output: existing `FormatSemanticRecall(items, maxChars)` rendered through
  the CLI OutputWriter with a header indicating session + turn + similarity.
- Handle empty query (print usage) and zero results ("No prior sessions
  match 'foo'.") explicitly.
- Honor the steer-panel pause hooks (same pattern as the existing
  `pkg/commands/sessions_cmd.go`).

**WebUI surface (`webui/src/components/PastSessionsHint.tsx`, new file):**
- Sidebar component, mounted in `Sidebar.tsx` below `chat-sessions-empty`.
- Reads from a new `GET /api/recall?query=<text>&limit=5` endpoint
  (`pkg/webui/recall_api.go`) that calls the same `Agent.Recall` path.
- Debounced 300 ms text input → fetch → render cards with session id, turn,
  similarity %, and a 1-line content preview.
- Click on a card → dispatch `sprout:session-restored` with the session_id
  (the handler at `webui/src/components/AppContent.tsx` already accepts this
  event via `handleSessionRestore`).

**Decompose `InjectSemanticRecall` so the recall logic is reusable:**
- Current signature: `func (a *Agent) InjectSemanticRecall(ctx context.Context, query string)`
- New signature: `func (a *Agent) Recall(ctx context.Context, query string, limit int) ([]RecalledItem, error)`
- `InjectSemanticRecall` becomes: `items, err := a.Recall(...); if err == nil && len(items) > 0 { append(format(items)) }`
- Same gating constants (recency decay, maxChars by token budget) are reused.

### Phase order (each is independently shippable)


### Acceptance

- `go test ./...` passes; `make build-all` clean.
- `webui/src/components/PastSessionsHint.test.tsx` and
  `pkg/agent_commands/recall_command_test.go` are added.
- A user with ≥3 historical sessions can:
  1. Type `/recall OpenAI auth` in the CLI and see the matching sessions.
  2. Type a query in the webui sidebar and click a result to restore it.
- No regression in existing `semantic_recall_test.go` cases.

---

## SP-093: Edit Approval for Destructive Shell Commands

_Per-command approval for `rm -rf`, `git push --force`, `kubectl delete`
(~2–3 days)._ SP-072 covers per-hunk diff approval for file edits, but
shell approval is monolithic: the user gets one prompt with four options
(`Deny`, `Approve once`, `Approve always`, `Elevate`) and can only choose
binary outcomes. A multi-command pipeline like
`rm -rf foo && git push --force` either runs entirely or not at all.

The pattern already exists: `pkg/agent/edit_approval.go` (624 lines) does
this for files. We mirror it for shell commands.

### Scope

**New `pkg/agent/shell_approval.go` (~400 lines):**
- `ShellProposal` type: `{ command string, parts []ShellPart, riskLevel RiskLevel }`.
- `ShellPart` type: `{ id string, text string, kind CommandKind, semantic string }`
  where `CommandKind` is one of `rm | git_push | git_reset | kubectl |
  docker | chmod | chown | write_redirect | http_post | unknown`.
- `SplitShellIntoParts(cmd string) []ShellPart` — tokenizes on `&&`, `||`,
  `;`, `|`, balanced-paren-aware. Classification: extends existing
  `pkg/agent_tools/security_classifier.go::ClassifyToolCall` with a
  per-segment classifier (`ClassifyShellSegment`) that maps to one of
  `rm | git_push | git_reset | kubectl | docker | chmod | chown |
  write_redirect | http_post | unknown` based on a small destructive-regex
  table (`rm -rf`, `git push --force`, `git reset --hard`, `kubectl
  delete`, `docker rm`, `chmod 7`).
- `Agent.RequestShellApproval(ctx, p ShellProposal)` — same broker pattern
  as `edit_approval.go`: WebUI first (publish `shell_approval_request`
  event), fallback to CLI (renderer with per-part checkboxes).
- `Agent.RespondToShellApproval(requestID string, decisions map[string]bool)`.

**New event type `pkg/events/shell_approval.go`:**
- `EventTypeShellApprovalRequest = "shell_approval_request"`
- `ShellApprovalRequestPayload{ requestID, command, parts[], unifiedView }`
  — mirrors `EditApprovalRequestEvent` shape so the WebUI panel can mirror
  EditApprovalPanel.tsx.
- Already supported by `pkg/webui/websocket_outbound_registry.go` (just add
  the new event type to the registry; it pattern-matches existing entries).

**Wire into `pkg/agent/approval_broker.go`:**
- In `RequestApproval`, when `toolName == "shell_command"` and the
  classification yields ≥2 high-risk parts, divert from
  `AskForApprovalWithOptions` to `RequestShellApproval`.
- Single-part shell commands: keep existing 4-option prompt (no regression).
- Pair-to-pipe commands (`a | b`) and risk-bounded commands: existing path.

**WebUI panel `webui/src/components/ShellApprovalPanel.tsx`:**
- Mirrors `EditApprovalPanel.tsx` (same data-testid shape, same diff viewer
  pattern). Each part is a checkbox: ✓ approved, ✗ rejected.
- Accept-all / reject-all shortcut keys.

### Phase order


### Acceptance

- `go test ./pkg/agent/... ./pkg/webui/... ./pkg/agent_commands/...`
  passes; `make build-all` clean.
- A user can `/approve-shell false` (or whatever the flag is named) to
  opt out; default behavior unchanged.
- A user with opt-in sees per-part checkboxes for `rm -rf foo &&
  git push --force`, can approve `rm` and reject `git push` and have
  exactly that outcome.
- The existing 4-option prompt still appears for single-part commands and
  for users who haven't opted in.

---

## SP-094: Typed Error Hierarchy in `pkg/agent`

_Full migration of ~512 `fmt.Errorf` sites to typed errors (~1 week)._
SP-091-5 covers the foundation (define types, migrate top-10 sites). SP-094
finishes the job: every tool handler and provider client returns a typed
error; the broker / metrics layer can classify retry vs. fail-fast without
string-matching.

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

_Status reconciliation (~1.5 days)._ After merging origin/main (commit
`656db751`), the README is authoritative and shows many more specs as
✅ Implemented than the spec headers themselves admit. The 2026-06-30
TODO audit fixed 12; this ticket finishes the remaining **14 spec
headers** so the automate runner knows what's actually open. No code
work — pure metadata.

### How to do each item

Open `roadmap/SP-###.md`, change `**Status:** 📋 Proposed` →
`**Status:** ✅ Implemented (<one-line summary>)`. Use the README's
phrasing as a guide. Commit with
`chore(roadmap): mark SP-### as shipped`. Each is independently
committable.

### Specs to fix (in priority order)


After running SP-096-1..14, also update the "Things to consider after
SP-091 → SP-095 ship" section at the bottom of TODO.md to remove
"Storybook" (SP-009 done), "Component library" (SP-009 done), and any
other items whose backing spec is now flipped.

### Acceptance

- `grep -lE "Status.*Proposed" roadmap/SP-*.md | wc -l` returns only the
  genuinely-open specs (SP-008, SP-011 [Phase 1 mostly done], SP-012
  [partial], SP-016b, SP-027, SP-045, SP-046, SP-054, SP-075).
- Each updated spec header matches what the README shows.
- All commits are pure metadata changes; `git diff --stat` shows no code
  lines added.

---


## SP-098: SP-075 Large-File Decomposition — Second Pass

_~1 week, 5-7 phases._ Most of SP-075's original worst offenders were
already split since the spec was written (2026-06-15): `cmd/agent_modes.go`
2344 → 732 lines, `pkg/configuration/config.go` 2833 → 388, etc. But a
new batch of files has grown large. The runner should split these in
priority order.

### Current state (2026-06-30 audit)

| File | Lines | Recommendation |
|---|---|---|
| `pkg/console/steer_input.go` | 1536 | Extract `streak.go` (typed streak + persistence) and `autocomplete.go` (tab completion + ghost text) — keep core loop in place. |
| `pkg/agent_tools/structured_helpers.go` | 1190 | Split JSON / YAML / TOML helpers into `structured_json.go` etc. by format. |
| `pkg/lsp/semantic/go_adapter.go` | 1188 | Extract per-symbol-kind scanners (struct, interface, func) into separate files. |
| `pkg/agent_tools/vision_types.go` | 1188 | Split type defs vs. prompt-builder helpers. |
| `pkg/console/status_footer.go` | 1132 | Extract token / cost / model-status into focused files. |
| `cmd/mcp.go` | 1105 | Extract per-tool MCP command surfaces. |
| `cmd/automate.go` | 1070 | Extract status / stop / list sub-commands. |
| `pkg/mcp/client.go` | 1060 | Extract transport / protocol / resource-readers. |
| `pkg/ast/symbols.go` | 1040 | Split per-language scanners. |
| `pkg/agent/tool_handlers_subagent_spawn.go` | 990 | Per-spec: spawn vs. build vs. run. |
| `pkg/agent/seed_tool_registry.go` | 982 | Group tool descriptions by domain. |
| `webui/src/components/Sidebar.tsx` | 851 | Extract section components. |
| `webui/src/components/AppContent.tsx` | 843 | Extract chat vs. editor vs. terminal layouts. |
| `webui/src/components/PaneLayoutManager.tsx` | 732 | Extract per-pane logic. |

### Phase order (each ~0.5 day)


### Acceptance

- Every targeted file ends under 800 lines.
- `go build ./...` clean after each extraction (per AGENTS.md
  refactoring protocol).
- All existing tests in each split file's package continue to pass.

---

## SP-099: SP-008 Track A — Concurrency Hardening

_~2 weeks, 3 phases._ Track B (typed errors) is fully covered by SP-094.
Track A has 4 open phases from SP-008 that have never been scoped into
real tickets. This ticket scopes them.

### Scope

**Phase 1: CI race detection by default.**
- Edit `Makefile` `test` target to include `-race` (not just `test-race`).
- Audit which `-short` skips disable race coverage; remove them from the
  default path or add a separate `test-race-short` target.
- Add a step to `.github/workflows/build.yml` that runs `go test -race
  ./...` on every PR.

**Phase 2: Locking audit + ADR.**
- New `docs/adr-0007-locking-strategy.md` codifying: when to use
  `sync.Mutex` vs `sync.RWMutex` vs channels vs atomic, with the 25
  existing mutexes classified under one of these patterns.
- Per-spec pattern: rename to `mu sync.Mutex` (drop the domain prefix)
  everywhere except where the prefix encodes ownership semantics.

**Phase 3: `-race` clean.**
- Run `go test -race ./...` with `-count=3` to flush flaky races.
- File and fix every race report (expected: a handful of test fixtures
  + 1-2 real races in event publishing).

### Phase order


---

## SP-100: SP-045 WASM Parity — Tier 2a (onnxruntime-web bridge)

_~3 days, 2 phases._ Tier 1 and Tier 2 are done per SP-045 §6.
Tier 2a (onnxruntime-web bridge in the browser) is the next concrete
unblocking piece. Currently WASM users only get the static-provider
embeddings; this brings ONNX quality to the browser.

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

_~3 days, 4 phases._ After merging origin/main, the README reports
several specs as `Partially Implemented` — the foundational pieces are
shipped but specific pending phases remain. The automate runner can
close the gaps as a single batch.

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

_~0.5 day._ The `656db751` merge brought in 6 new commits and a
re-sync of the README. There may be additional specs that flipped from
Proposed to Implemented whose spec headers were not updated. This
ticket is a quick verification pass.

---

## Things to consider after SP-091 → SP-095 ship
- Acceptance: `get_callers`/`get_callees` return correct results for
  Go code in this repo; `find_dead_code` runs in < 100ms; `repo_map`
  output is unchanged but returns in < 50ms on warm cache.

---

## SP-111: TODO Loop Workflow — Hardening

_The native TODO loop workflow (`automate/todo-loop.json`) is functional but
has known gaps. These are follow-ups, not blockers._

### Items

- [ ] **SP-111-3:** Checkpoint/resume for crash recovery. The steps workflow
      persists state via `persistWorkflowCheckpoint`. The loop should do
      the same — save the current TODO line number so a restarted run
      picks up where it left off instead of starting over.
- [ ] **SP-111-4:** Fix `run_automate` BPM process detachment. The
      `BackgroundProcessManager` uses `Setpgid` but processes still die
      when the agent tool call completes. Investigate whether stdin
      inheritance or session group teardown is the cause. `nohup` works
      as a workaround.

---

