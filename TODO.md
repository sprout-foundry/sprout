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

**SHIPPED 2026-07-06.** Verified against `pkg/agent/conversation.go:253
::resizeImageForVisionEmbed` (committed as `abf3f6ba feat(agent):
pre-resize images to 1568px for vision embedding` + `2326cd85 docs:
mark shipped`). Bilinear resample via `golang.org/x/image/draw`,
re-encoded as JPEG q85, called from `processImagesAsMultimodal` at
`conversation.go:577`. 5 integration tests in
`pkg/agent/conversation_embed_resize_integration_test.go` cover
2400×1800 PNG, 2000×1500 JPEG, extreme aspect ratios, and small-image
no-op paths. TODO's "1536px" cap is stale — actual cap is 1568px
(Anthropic "high" tier), better than the spec. `DownloadImage` keeps
its own `OptimizeImageData` path (`vision_image.go:137-238`) which
pre-dates this work; not regressed. **Outstanding follow-up**: wire
`VisionCapabilities()` into the cap so per-provider tuning replaces the
single 1568px ceiling. Tracked under D2 below.

#### SP-103-A9: Typed Errors in Vision

`classifyPDFProcessingErrorCode` and `strings.Contains(errMsg, ...)` in `vision_image.go:418-440` stringify typed errors to classify them. `pkg/errors/types.go::TypedError` exists with `CodeVision*` constants. Migrate to typed error wrapping at the source.

**Effort:** ~0.5 day. Replace string-matching with `errors.As` checks against `TypedError`.

**SHIPPED (image path) 2026-07-06.** `pkg/agent_tools/vision_typed_errors.go`
(committed as `e47280c7 feat(agent_tools): translate typed errors to
vision error codes`) introduces `classifyVisionResponseError` which
walks the error chain: `IsRemoteSizeExceededError` first, then
`errors.As(err, &te)` against `*agenterrors.TypedError` mapped through
`typedErrorToVisionCode`, then legacy `strings.Contains` fallback for
untyped errors. `applyClassifiedError` at the response-builder boundary
emits a richer message using `TypedError.Component + .Message` when
present. `vision_typed_errors_test.go` covers the typed→code mapping,
the remote-size sentinel, the legacy fallback, and the local-file
refinement branch.

**NOT shipped (PDF path).** `classifyPDFProcessingErrorCode` in
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

_Surfacing past sessions on demand (~1–2 days)._

**SHIPPED 2026-07-06.** All three phases landed at `cd528da3`
(SP-092-1: extract `Agent.Recall`), `e461c546` (SP-092-2: `/recall`
CLI command), `c22f2fc1` (SP-092-3: `/api/recall` endpoint +
`PastSessionsHint` sidebar). Verified on disk:

- `pkg/agent/semantic_recall.go:295` — `Agent.Recall(ctx, query,
  limit) ([]RecalledItem, error)` extracted; `InjectSemanticRecall`
  (line 325) now wraps `Recall` + `FormatSemanticRecall`.
- `pkg/agent_commands/recall_command.go` (4KB) — `RecallCommand`
  registered in `pkg/agent_commands/commands.go:111` alongside
  `/sessions`, `/rewind`. Accepts `<query>` + `--limit N` (default 5)
  + `--json`; empty-query → usage; zero-result → explicit message.
- `pkg/agent_commands/recall_command_test.go` (7.7KB) covers the
  flag matrix.
- `webui/src/components/PastSessionsHint.tsx` (3.3KB) — debounced
  300 ms input, mounted via the sidebar slot, dispatches
  `sprout:session-restored` on click.
- `webui/src/components/PastSessionsHint.test.tsx` (5.3KB) covers
  the search → click → restore flow.
- `pkg/webui/recall_api.go` (2.6KB) — `GET /api/recall?query=&limit=`
  endpoint, with `pkg/webui/recall_api_test.go` (6KB) integration
  coverage.

No genuine follow-ups remain. The spec body below is preserved
verbatim per sp-009 isolation rules (metadata-only update).

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
(~2–3 days)._

**SHIPPED 2026-07-06.** All three phases landed at `003b0a26`
(SP-093-1: `shell_approval.go` with `ShellProposal` + 9 classifiers),
`be521b02` (SP-093-2: `Agent.RequestShellApproval` + CLI per-part
picker), `1a6c0e12` (SP-093-3: `ShellApprovalRequestPayload` event +
`ShellApprovalPanel`). Verified on disk:

- `pkg/agent/shell_approval.go` (14KB) — `ShellProposal`,
  `SplitShellIntoParts`, 9-command classifier table
  (`rm | git_push | git_reset | kubectl | docker | chmod | chown |
  write_redirect | http_post | unknown`).
- `pkg/agent/shell_approval_test.go` (18KB) covers classification +
  approval broker flows.
- `pkg/agent/approval_broker.go:215-222` — broker diverts
  `shell_command` with multi-part high-risk to
  `RequestShellApproval`; single-part retains the existing 4-option
  prompt.
- `pkg/events/shell_approval.go` (3.6KB) —
  `EventTypeShellApprovalRequest` + `ShellApprovalRequestPayload`.
- `webui/src/components/ShellApprovalPanel.tsx` (9.4KB) — per-part
  checkboxes mirroring `EditApprovalPanel.tsx` shape.

No genuine follow-ups remain. The spec body below is preserved
verbatim per sp-009 isolation rules.

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

**Foundation + retry-with-backoff SHIPPED 2026-07-05 (per AUDIT-SHIP-2).**
Verified on disk:

- `pkg/agent/retry.go::ClassifyError` — exported and present.
- `seed/core/retry.go::doChatWithRetry` — present.
- `pkg/agent/seed_tool_registry.go:479::PublishRateLimited` —
  `EventTypeRateLimited` wired.

**Full tree NOT shipped.** `pkg/agent/errors.go` is 392 bytes with
just one sentinel (`errProviderStartupClosed`) — the ~250-line tree
called for by the spec (`RetryableError`, `RateLimitError`,
`AuthError`, `ContextCancelledError`, `InvalidInputError`,
`ToolError`, `ProviderError`, `FileSystemError`, `NetworkError`,
`WorkspaceError`, plus `Wrap()` helper) has not landed. The
`fmt.Errorf`-migration work (~512 sites across `pkg/agent_tools/*_handler.go`,
`pkg/agent/api_client*.go`, `pkg/agent/subagent_*.go`, etc.) is
genuine remaining work.

Spec body preserved verbatim per sp-009 isolation rules.

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

_Status reconciliation (~1.5 days)._

**SHIPPED 2026-07-06 at commit `81ec1f87 chore(todo): mark SP-096-1
through SP-096-14 as complete`.** All 14 spec-header fixes landed in
that single batch commit (per the `chore(roadmap): reconcile …`
series: `205fb580` SP-017 + SP-048; `55c997e1` SP-107 + SP-110 +
AUDIT-SHIP; plus the 12 headers fixed by the 2026-06-30 TODO
audit). Verified on disk:

```
$ grep -lE "Status.*Proposed" roadmap/SP-*.md
roadmap/SP-105-cli-interactive-panels.md
roadmap/SP-112-platform-parity.md
roadmap/SP-114-unify-command-execution.md
```

3 specs still show `Status: 📋 Proposed` — all three verified
genuinely open (no implementation on disk):

- **SP-105** (CLI interactive panels) — `SettingsCommand` /
  `UsageCommand` are registered (commands.go:153, 156) but the
  full panel UX called for by the spec is not built. Per the
  SP-105 → AUDIT-SHIP-3 audit, the spec is partial.
- **SP-112** (platform parity) — 1-day-old spec; no code.
- **SP-114** (unify command execution) — 1-day-old spec; no code.

Spec body preserved verbatim per sp-009 isolation rules.

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
| `pkg/agent/submanager_state.go` | 848 | Split: `submanager_state.go` (state machine + transitions) + `submanager_persist.go` (snapshot / restore) + `submanager_query.go` (status queries). |
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

_~2 weeks, 3 phases._

**SHIPPED 2026-07-06.** All three phases landed at `e2dd7276`
(SP-099-2: ADR-0007 + mutex rename), `5339d2dc` (mark SP-099-2),
`076c0ecf` (mark SP-099-3 race fixes); SP-099-1 (CI race by default)
predates that commit trail but is verified on disk:

- **Phase 1 (CI race default)** — `Makefile:82` defines
  `TEST_RACE ?= -race`; `test-unit` (line 88) consumes `$(TEST_RACE)`;
  the `test` target (line 168) is `test: test-unit` and so runs
  with `-race` by default. `.github/workflows/build.yml:85` runs
  `make test-coverage` which hardcodes `go test -race …`. The
  opt-out `test-unit-lowmem` target is documented as a CI-bypass
  only.
- **Phase 2 (Locking ADR)** — `docs/adr-0007-locking-strategy.md`
  (5.3KB) codifies `sync.Mutex` vs `sync.RWMutex` vs channels vs
  atomic with the existing mutexes classified. Mutex rename to
  `mu sync.Mutex` applied (per `e2dd7276` commit body).
- **Phase 3 (`-race` clean)** — Marked shipped at `076c0ecf`. The
  `go test -race ./...` runner is green on CI; specific race fixes
  are captured in the SP-099 history / `docs/sp-099-audit.md`.

No genuine follow-ups remain. Spec body preserved verbatim per
sp-009 isolation rules.

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

