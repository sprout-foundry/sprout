# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

---

## SP-123: User Command Policies

**Status:** ✅ Shipped — Phases 1–3 all implemented (2026-07-16).
_Spec: `roadmap/SP-123-user-command-policies.md`._

- Phase 1: Config types + EvaluateCommandPolicy engine + RequestApproval wiring + migration
- Phase 2: CommandPolicyEditor UI (Allow/Ask/Deny) + "Always ask" in CLI + WebUI approval
- Phase 3: E2E tests + settings API round-trip fix

---

## SP-122: Security Classifier — Chained Command Handling

**Status:** ✅ Shipped — Phase 1 + Phase 2 both implemented.

- Phase 1 (nested safe-path matching): `safeRmRfComponents` set in `shell_patterns.go` + `isSafeRmRfComponent` helper. Nested paths like `rm -rf internal/api/webui/dist/sprout-webui` classify SAFE because `dist` is a known build artifact. Path traversal (`../`) and absolute system paths rejected.
- Phase 2 (chained command splitting): `classifyChainedCommand` in `security_classifier.go` splits on `&&`, `||`, `;`, `|` (quote-aware), classifies each subcommand independently, and returns the max risk. Safe portions of a chain don't elevate to DANGEROUS.

**Priority**: High — blocks safe dev workflows (vendoring, build cycles)

### Problem

When a shell command chains a destructive operation (e.g. `rm -rf`)
with subsequent safe operations (e.g. `mkdir`, `cp`), the classifier
blocks the ENTIRE pipeline, even when the `rm -rf` targets a
legitimate build/vendoring path.

### Concrete example (2026-07-15)

```bash
rm -rf internal/api/webui/dist/sprout-webui && \
  mkdir -p internal/api/webui/dist && \
  cp -r ../sprout/webui/dist/* internal/api/webui/dist/sprout-webui/
```

This is a safe vendoring operation (clear build artifacts, copy fresh
build). The classifier blocked it because `rm -rf internal/api/webui/...`
is not in the `safeRmRfPrefixes` whitelist — only top-level dirs like
`dist/`, `node_modules/`, etc. are whitelisted. The `cp -r` and `mkdir`
commands are perfectly safe but got blocked by association.

### Root cause (two compounding issues)

1. **`safeRmRfPrefixes` is too narrow.** It only matches top-level
   build artifact directories (`node_modules/`, `dist/`, `build/`).
   Nested project paths like `internal/api/webui/dist/sprout-webui`
   or `platform/webui/dist/` are not whitelisted and trigger
   `directory_deletion` → DANGEROUS → hard block.

2. **Chained command classification is all-or-nothing.** When a
   command has `&&`-chained subcommands, the classifier evaluates
   the full command string as a single unit. If ANY part is
   DANGEROUS, the whole thing is blocked. There's no mechanism to:
   - Split on `&&` / `||` / `;` and evaluate each subcommand independently
   - Allow the safe portions while prompting for the dangerous portion
   - Whitelist specific workspace-relative paths

### Phase 1: Expand safe-path matching

- [x] **SP-122-1a:** Add pattern matching for `rm -rf` against paths
      ending in known build output dirs (e.g. `*/dist/*`, `*/build/*`,
      `*/node_modules/*`). Use a glob/regex approach instead of exact
      prefix matching so nested paths are covered.
      **Files**: `pkg/agent_tools/shell_patterns.go`
      **Acceptance**: `rm -rf internal/api/webui/dist/sprout-webui`
      classifies as SAFE; `rm -rf internal/api/` still DANGEROUS.
      **SHIPPED.** `safeRmRfComponents` set + `isSafeRmRfComponent` helper.

- [x] **SP-122-1b:** Add tests for nested-path safety matching.
      **Files**: `pkg/agent_tools/security_classifier_test.go`
      **Acceptance**: Tests cover top-level, nested, and traversal
      (`../`) paths.
      **SHIPPED.** `TestIsSafeRmRfPrefix_NestedPaths` (46 cases),
      `TestIsSafeRmRfPrefix_NestedPathsClassifiedSafe` (5 cases),
      `TestIsSafeRmRfComponent` (23 cases),
      `TestIsSafeRmRfPrefixBackwardCompatibility` (16 cases),
      `TestSafeRmRfTraversalEscape` (6 cases).

### Phase 2: Split chained commands for classification

- [x] **SP-122-2a:** When a command contains `&&`, `||`, or `;`, split
      into subcommands and classify each independently. The overall risk
      is the MAX of the subcommand risks, but the prompt/block decision
      should be per-subcommand where feasible.
      **Files**: `pkg/agent_tools/security_classifier.go`
      **Acceptance**: A command like `cp -r x y && rm -rf dist/ && echo done`
      classifies the `cp` and `echo` as SAFE and only flags the `rm -rf`.
      **SHIPPED.** `classifyChainedCommand` splits on `&&`/`||`/`;`/`|`
      (quote-aware), classifies each subcommand via `classifySingleCommand`,
      returns max risk. The vendoring example from the original bug report
      (`rm -rf internal/api/webui/dist/sprout-webui && mkdir -p ... && cp -r ...`)
      classifies as SAFE.

- [x] **SP-122-2b:** Tests for chained command splitting.
      **Files**: `pkg/agent_tools/security_classifier_test.go`
      **Acceptance**: Tests cover `&&`, `||`, `;`, pipes, and mixed chains.
      **SHIPPED.** `TestClassifyChainedCommand` (25 cases covering `&&`,
      `||`, `;`, `|`, mixed chains, quote handling, and the vendoring
      example from the TODO). Known gap: `xargs rm -rf` after a pipe
      classifies as CAUTION (xargs prefix not matched by rm patterns) —
      tracked as a minor follow-up, not a security bypass since CAUTION
      still prompts.

### Key files

- `pkg/agent_tools/security_classifier.go` — main classifier
- `pkg/agent_tools/shell_patterns.go` — `safeRmRfPrefixes` map
- `pkg/agent_tools/shell_utils.go` — `getShellCommandRiskType`
- `pkg/agent_tools/shell_handler.go` — where block decision is enforced

---

## SP-116: Multi-Instance Isolation

_Spec: `roadmap/_completed/SP-116-multi-instance-isolation.md`. ✅ Implemented — Phases 1–4 shipped 2026-07-15 (`ac4d72e6`, `ef47144d`, `c7c4047b`, `99991ba2`, `c0602add`). Auto-detect in `cmd/root.go::detectGitRepo`, scoped background processes, layered config (`agent.NewAgentWithLayers`). The spec is archived; the test coverage is in `cmd/root_test.go` and `pkg/configuration/isolated_config_test.go`. Daemon-side skip-via-`SPROUT_SERVICE=1` guard ensures system services stay global._

## SP-118: Daemon Multi-Window Session Isolation

_Spec: `roadmap/_completed/SP-118-daemon-multi-window-sessions.md`. ✅ Implemented — Phases 1–5 shipped 2026-07-15; Phase 6 partial. `agentEnforceSingleSession` dispatch + `UserConnections` registry lets the daemon (`sprout service`) accept N parallel browser windows per user while `sprout agent` keeps single-active semantics byte-identical. `daemon_multi_session` flag defaulted ON with `sprout config set daemon_multi_session=false` rollback path. `/api/ws-metrics` exposes `active_ws_count_by_user` and `ws_count_per_user`. Phase 6 (README + Settings UI) intentionally deferred per AGENTS.md "no documentation" rule; TODO.md sync landed and `multi_device_takeover.go` deprecation comment shipped in Phase 1. Commits: `629fd42b`, `16f55278`, `2a07a9ba`, `4cf55ddc`, `81f46b7c`, `6b013bce`._

## SP-119: Workspace-aware directory resolution in daemon-mode tools

_Spec: `roadmap/_completed/SP-119-workspace-aware-directory-resolution.md`. ✅ Implemented — 3 phases shipped 2026-07-15. `automate.DirIn(workspaceDir)` helper threads workspace context through agent-tool paths (`handleRunAutomate`, `handleListAutomateWorkflows`, `workflowRequiresApproval`) and the interface-based registry handler (`list_automate_workflows_handler.go`). Tests: `TestDirIn` (4 cases) + `TestListAutomateWorkflowsHandler` (3 cases). Out-of-scope ~25 CWD-vs-workspace callsites tracked under SP-091. Commits: `6608ecf3`, `aa2d05a9`._

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
        ✅ Implemented (all 3 phases shipped at `6d31e17a`).
        Status flipped from 🟡 Partially Implemented to ✅ Implemented.
        Phase 3 deliverables (`pkg/webui/wakeup_poller.go`,
        `pkg/agent/pause.go::DisableWakeup`,
        `pkg/configuration/config.go::WakeupConfig`, Settings UI
        toggle) all landed in `6d31e17a` — the previous "Phase 3
        NOT shipped" verdict from the 2026-07-05 audit was stale,
        caught by the 2026-07-15 doc-drift sweep (`93b722f6`).
        TODO.md sync completed in the same sweep (entries at
        line 176 + line 666 updated).
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

_2 of 6 sub-items remain open (D1, D2). B2 + A9 shipped by subsequent work;
B2 verified at `abf3f6ba`, A9 image-path verified at `e47280c7`. Code state
audit performed 2026-07-15._

### Remaining Work

#### SP-103-B2: Image Resizing

**SHIPPED 2026-07-15** at `abf3f6ba feat(agent): pre-resize images to 1568px for
vision embedding`. Implementation diverges from the original spec name
(`resizeImageForVisionEmbed` instead of `resizeImageToMax`) and target
(`1568px` is Anthropic's recommended value, not the spec's "1536"), but
achieves the spec's goal. Wired into `readImageAsImageData` which feeds
`processImagesAsMultimodal` — images are resized before being attached as
inline multimodal content, capping the long edge at `visionEmbedMaxEdgePx =
1568` using bilinear interpolation, re-encoded as JPEG quality 85.
Pass-through for unsupported formats (webp/avif) so the agent doesn't lose
those images entirely. Tests: `conversation_test.go` (5 unit tests:
no-op small, no-op exact, downscale to cap, format pass-through, error
tolerance) + `conversation_embed_resize_integration_test.go` (4
integration tests covering extreme aspect ratios + end-to-end
`readImageAsImageData` pipeline).

The TODO's original "Call it in `DownloadImage`" item is not necessary:
`DownloadImage` returns raw bytes that flow into `OptimizeImageData` for
the `analyze_image_content` tool path, which already has its own
`visionMaxDimension = 4096px` resize. The 1568px pre-resize is a
multimodal-path concern; the tool path keeps the larger cap so
`analyze_image_content` users can still see high-detail screenshots.

**Genuine follow-up** for the broader vision resize story: SP-103-D3
(per-provider `VisionCapabilities` table). The current code picks a
single 1568px cap regardless of provider — OpenAI's `low`/`high` detail
tiers, Anthropic's `low`/`high`, and Gemini's different size policies
all need their own optimal caps. The `VisionCapabilities` struct was
added at `pkg/agent_api/interface.go` (per SP-103-D3 referenced in
AUDIT-GAP-2); populating per-provider values is the remaining work.

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

- [x] **SP-103-A9b:** PDF-path typed-error migration. `classifyPDFProcessingErrorCode`
      in `pkg/agent_tools/vision_utils.go:152-188` now prefers typed errors via
      `errors.As(*TypedError)` → `typedErrorToVisionCode`, falling back to the
      strings.Contains logic (now `legacyClassifyPDFError`) when no typed error
      is in the chain. Failure sites in `vision_pdf.go` are wrapped with
      `agenterrors.NewNotFound`/`NewNetwork`/`NewValidation` so typed errors
      propagate through `fmt.Errorf("...: %w", ...)` chains. Tests:
      `vision_types_pdf_error_test.go` got 13 new typed cases (NotFound,
      Network, Validation, Timeout, Tool, wrapped-chain variants, legacy
      fallback, nil). `vision_utils.go:13` and `vision_pdf.go:18` import
      `agenterrors`. Build clean; `go test ./pkg/agent_tools/...` 42.5s pass.

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

**SHIPPED 2026-07-15.** Phase 1 (Go surface) and Phase 2 (TS loader)
both landed. The shipped artifacts diverge from the original spec
paths — the onnxruntime-web loader lives in `webui/src/services/
onnxruntimeWebLoader.ts` (the modern TS service location per
project conventions, not `webui/public/wasm/onnxruntime-web-loader.js`
as the spec prescribed). The bridge globals `__sproutONNX`,
`__sproutLoadOnnxRuntime`, and the new `__sproutSwitchEmbeddingBackend`
+ `__sproutEmbeddingModel` are installed by `webui/src/services/
embeddingBackendController.ts` on app boot.

The Go-side control surface that the spec prescribed
(`SproutWasm.embeddingModel`, `SproutWasm.switchEmbeddingBackend`,
`SproutWasm.embeddingBackendStatus`) is wired in `cmd/wasm/
embedding_funcs.go` and merged into the shell WASM module's
apiSurface via `cmd/wasm/main.go`. All three are pure delegation:
the actual install/uninstall of `__sproutONNX` is owned by the
host page (TypeScript), since the underlying `BrowserONNXProvider`
is TypeScript-only.

### Phase 1: Go-side control surface (`cmd/wasm/embedding_funcs.go`)

- `embeddingJSFuncs()` returns the three JS-callable functions and
  is merged into `apiSurface` in `cmd/wasm/main.go`. Defaults to
  "static" backend with `EmbeddingModelDefault = "gemma-300m"`.
- `embeddingModelFunc` reads `globalThis.__sproutEmbeddingModel`
  first (host-page override), falls back to the default.
- `switchEmbeddingBackendFunc` validates the name (`"static"` or
  `"onnx-web"`), rejects unknown names with a JS Error, then calls
  the host-side helper `globalThis.__sproutSwitchEmbeddingBackend`.
- `embeddingBackendStatusFunc` returns
  `{backend, model, dimensions, ready}` based on whether
  `__sproutONNX` is installed. Always returns an object.

Pure-Go helpers covered by `embedding_funcs_test.go` (3 tests:
default model name, backend name constants, registry surface).

### Phase 2: Host-side controller (`webui/src/services/embeddingBackendController.ts`)

- `installEmbeddingBackendController()` installs
  `globalThis.__sproutSwitchEmbeddingBackend` (delegates to
  `switchEmbeddingBackend`) and `globalThis.__sproutEmbeddingModel`
  (the default model name).
- `switchEmbeddingBackend(name)` uninstalls the bridge for "static"
  or calls `installSproutONNXBridge()` for "onnx-web". Idempotent.
- `embeddingBackendStatus()` mirrors the Go-side contract.
- `teardownEmbeddingBackend()` removes all globals + closes the
  underlying BrowserONNXProvider.
- Wired into `webui/src/services/wasmShell.ts` boot sequence
  immediately after `installSproutONNXBridge()`.

Augments the existing `SproutWasmAPI` interface in `wasmShell.ts`
via a `declare module` block rather than redeclaring `window.SproutWasm`
— the canonical interface covers ~30 entries (init, executeCommand,
extractSymbols, runAgent, etc.) and shadowing it would lose them all.

11 vitest tests cover: install side-effects, default state, idempotent
switching, static↔onnx-web round-trip, error on unknown names, status
reporting for both backends, and the WASM-callable helper surface.

### Acceptance

- `SproutWasm.switchEmbeddingBackend("onnx-web")` returns "onnx-web",
  triggers lazy-load of onnxruntime-web, and installs `__sproutONNX`.
- Default backend remains "static" so existing WASM users see no change.
- Tests assert the onnxruntime-web script is NOT injected when the
  backend is "static" (covered by `onnxruntimeWebLoader.test.ts`).

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
- SP-110 — ✅ Implemented (all 3 phases shipped at `6d31e17a`).
  Doc-drift caught in the 2026-07-15 sweep; status flipped from
  🟡 Partially Implemented to ✅ Implemented.
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

---


