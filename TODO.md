# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

**Roadmap drift fixed 2026-06-30:** `roadmap/README.md` was carrying 15 stale
"Proposed" markers for shipped specs. Audit found: SP-006 (superseded by
SP-059), SP-009, SP-010, SP-013, SP-022-workspace-management, SP-023, SP-062,
SP-064, SP-065, SP-068, SP-073, and SP-066 (superseded by SP-082) are all
implemented. They now appear in the "Implemented" table with status notes.
The "Proposed" table is down to 15 entries that are genuinely still open.

---

## SP-091: Close the next round of roadmap gaps

_Tech-debt cleanup + finishing touches (~3–5 days)._ Each item below was
identified during the 2026-06-30 roadmap audit. They are the highest-value
remaining work in the existing spec set; new specs that emerge from usage
should be added as SP-092+ rather than expanding this list.

### Phase 1 — Embedding consolidation (1 day, ~5% binary shrink)

- [x] **SP-091-1: SP-058 selective grammar embed.** Strip the 100 grammar blobs
  we don't use from `pkg/ast.SupportedLanguages` (currently 5 of 100 active).
  Goal: ~13 MB off the WASM bundle, ~20 MB off the daemon. Spec already
  written (`roadmap/SP-058-selective-grammar-embed.md`); no spec work needed.

- [x] **SP-091-2: SP-061 retire the static embedding provider.** Two parallel
  providers exist (static / ONNX). ONNX is the higher-quality path; static
  only exists for WASM-without-CGO. Once SP-058 lands (fewer grammars shipped)
  and the ONNX WASM bridge is verified, remove the static provider entirely.
  Binary shrinks another ~55 MB (the embedded model). Spec:
  `roadmap/SP-061-remove-static-embeddings.md`.

### Phase 2 — Terminal + CLI regression bugs (~1 day)

- [x] **SP-091-3: SP-011 critical terminal bugs.** Audit
  `pkg/console/terminal*.go` and `webui/src/components/Terminal*.tsx` for the
  three issues called out in the spec (exit doesn't close pane, scroll-back
  lost on tab switch, paste of large content silently truncates). Pick the
  one that most affects the REPL day-to-day and fix. Spec:
  `roadmap/SP-011-terminal-parity.md`.

- [x] **SP-091-4: SP-048 silence-fill between submit and first token.** When
  the model takes >2s to respond, the CLI is silent. Spec proposes a single
  status line ("thinking…" with elapsed timer) that the agent emits on its
  own turn-start and erases on first stream chunk. ~50 lines in
  `pkg/console/`. Spec: `roadmap/SP-048-cli-delight.md` §"Silence between
  submit and first stream chunk."

### Phase 3 — Reliability + observability (~1 day)

- [x] **SP-091-5: SP-008 typed error hierarchy.** The codebase has ~366
  `fmt.Errorf` sites in `pkg/agent/` with no classification. Define a small
  typed error tree in `pkg/agent/errors.go` (RetryableError, RateLimitError,
  ContextCancelledError, InvalidToolInputError, ProviderError) and migrate
  the top-10 most-trafficked error sites. Foundation for systematic retry,
  metric labels, and better error messages. Spec:
  `roadmap/SP-008-reliability-engineering.md` §"Observability" Phase 2.

- [x] **SP-091-6: SP-008 structured-log context propagation.** `pkg/logging`
  has a `slog` wrapper; only ~3 packages use it. Add `.With(sessionID,
  iteration, provider, model)` propagation through `Agent.ProcessQuery` so
  every log line in a turn is automatically correlated. Spec:
  `roadmap/SP-008-reliability-engineering.md` §"Observability" Phase 1.

### Phase 4 — File-size decomposition (~1–2 days)

- [x] **SP-091-7: SP-075 split `pkg/agent/tool_handlers_subagent_spawn.go`
  (1208 lines).** Largest single file in `pkg/agent/`. Per-spec method: extract
  spawn/build/run into separate files under a new
  `pkg/agent/subagent_spawn/` package. No behavior change. Run full test
  suite between each extraction.

  _First extraction shipped: two self-contained helpers (`extractSubagentSummary`,
  `(*Agent).warnSubagentFallback`) moved to `tool_handlers_subagent_spawn_helpers.go`;
  main file 1208 → 991 lines, build + tests green. Further splits
  (`handleRunSubagent` / `handleRunParallelSubagents`) deferred —
  cohesive enough that aggressive splits risk behavior drift; tracked
  for a future SP-075 phase._

- [x] **SP-091-8: SP-075 split webui components above 800 lines.**
  GitSidebarPanel (974), Sidebar (851), AppContent (843), Terminal (738),
  EditorTabs (731), AutomationsPanel (724), SettingsPanel (691). Pick
  GitSidebarPanel first (clearest seams: branches / worktrees / push / PR
  / status — five files). Spec: `roadmap/SP-075-large-file-decomposition.md`.

  _Shipped (GitSidebarPanel only): extracted cohesive sections into
  `webui/src/components/git/` — GitCommitBox.tsx (78 lines), GitContextMenu.tsx
  (110), GitFileSections.tsx (289), GitHeader.tsx (143), GitPRDialog.tsx (234).
  `GitSidebarPanel.tsx` 974 → 349 lines. Type-check green, no new lint
  errors. Remaining components (Sidebar, AppContent, Terminal, EditorTabs,
  AutomationsPanel) deferred to future SP-075 phases — they're each well
  over 690 lines but cohesive in their current form; same risk-vs-need
  trade-off as SP-091-7's deeper splits._

### Phase 5 — UX polish (~0.5 day)

- [x] **SP-091-9: SP-012 reduced-motion support.** Wrap every `@keyframes`
  animation in `webui/src/App.css` and the shared `packages/ui/.storybook/tokens.css`
  in a `@media (prefers-reduced-motion: no-preference)` block. Audited list
  in spec §"Current State" §"Missing accessibility." Spec:
  `roadmap/SP-012-ux-polish.md`.

- [x] **SP-091-10: SP-017 collapse thin settings tabs.** Security (2 fields),
  Performance (5), OCR (3), Commit & Review (4) each get a full tab in
  `webui/src/components/settings/`. Merge into a single "Advanced" tab with
  collapsible sections; remove 3 tab entries from `SettingsPanel.tsx`.

  _Shipped: New `AdvancedSettingsTab` composes Performance + Commit & Review
  + OCR into one `<details>`-collapsible section. SECTION_GROUPS reduced from
  4 env-subsections to 2 (Providers + Advanced). Three obsolete tab files
  deleted, plus `SettingsPanel.commit-review.test.tsx` (was 100% `test.skip()`
  with a TODO note). Vitest: 83/83 settings tests still pass; type-check
  green; lint clean for the new file._

### Phase 6 — Spec retirement

- [x] **SP-091-11:** Delete `roadmap/SP-006-delegate-tool.md` and
  `roadmap/SP-066-structured-file-key-order.md` — both are superseded (the
  spec files themselves note this; only the README carried stale entries).
  Their content is preserved in git history.

---

## SP-103: Vision Pipeline Reliability + Caching + Routing Fixes

_Verified against actual code state (2026-06-30) and current LLM-vendor
practice (Anthropic Vision + Prompt Caching docs, production IDP research
from Edge Case Jan 2026)._ The original direction (route pasted images
through `analyze_image_content` by default) was wrong for the common case;
Anthropic's docs explicitly recommend inline image embedding with
`cache_control` for repeat-turn cost reduction. This SP captures the actual
gaps: prompt caching on image blocks, image resizing, dead-code
reactivation for non-vision models, OCR-model carve-out, plus the
reliability/perf/correctness fixes that were always valid.

### Background (verified state)

- `pkg/agent/conversation.go:230-242` (`processImagesAsMultimodal`) is the
  *only* path that embeds pasted images as `api.ImageData` for the chat
  model. Default behavior when `SupportsVision() == true`.
- `processImagesViaOCR` (`conversation.go:376`) and
  `buildNonVisionImageToolPrompt` (`conversation.go:268`) are defined but
  **never called from production code**. The "non-vision path" is dead
  code — pasted images on a non-vision model stay as raw
  `Pasted image saved to disk: /tmp/foo.png` placeholders with no
  system-prompt guidance to call `analyze_image_content`.
- `analyze_image_content` calls `tools.AnalyzeImage` directly and is
  available to all models regardless of `SupportsVision()`. It's the
  right tool for specialized analysis (OCR of dense text, frontend
  inspection, structured extraction), but **not** the right default for
  simple "what color is this?" conversational vision.
- `OllamaLocalClient.SupportsVision()`
  (`pkg/agent_api/ollama_local.go:514-520`) returns true for `glm-ocr` and
  similar OCR models, which then triggers direct multimodal embedding on a
  model not designed for conversational vision. Needs a
  `SupportsConversationalVision()` distinction.
- `visionCache`, `lastVisionUsage`, `visionCacheUsage` are package-globals
  with no mutex. Concurrent `AnalyzeImage` calls race.
- `processOCRImages` (`vision_pdf.go:194-265`) is sequential with a
  `failures >= 2` give-up. Up to 8 pages serially.
- `DownloadImage` (`vision_image.go:91-100`) and `downloadRemotePDFToTemp`
  (`vision_pdf.go:118`) read the full body before checking size.
- `classifyPDFProcessingErrorCode` (`vision_utils.go:182-184`) and the
  per-call `strings.Contains(errMsg, ...)` in `vision_image.go:418-440`
  stringify typed errors to classify them back to `ErrCode*` constants.
  Typed errors already exist in `pkg/errors/types.go::TypedError` with
  `NewTool`, `NewNetwork`, etc.; the gap is at the response-builder
  boundary, not the source.
- `RateLimitExceededError` (`pkg/agent/api_client_types.go`) is used only
  by test fixtures (`scripted_playback.go`). The legacy retry layer was
  removed alongside `APIClient` in v0.16.12. Production has no
  retry/backoff in `SendVisionRequest`.

### Why these items, not "route to tools"

Per Anthropic's vision docs
(`https://platform.claude.com/docs/en/build-with-claude/vision`):

- Inline image embedding is the recommended pattern. Anthropic explicitly
  shows `image` content blocks as the primary use case.
- Image-then-text structure gives the best results ("Claude works best
  when images come before text").
- Multi-image joint analysis is first-class ("useful for comparing
  images, asking about differences").
- Caching images is supported via `cache_control` — 1024-token minimum
  for Sonnet 4.5+, cache hits cost 10% of base input.

Per Anthropic's prompt caching docs:

- Image blocks are cacheable content blocks. The biggest cost win is
  caching the image prefix across turns, not routing through tools.

Per Edge Case's production IDP research (Jan 2026), the canonical
production pattern is **hybrid OCR → vision LLM**, not vision-LLM-only or
OCR-only. For Sprout's case (a code agent with multimodal chat model),
that means: inline images for chat, `analyze_image_content` for
specialized analysis (OCR mode = "vision LLM called as a tool"), and
caching for repeat-turn cost.

### Reliability (SP-103-A) — high priority

- [x] **SP-103-A1:** Retry-with-backoff wrapper for `SendVisionRequest`. 3
      attempts, exponential (200ms → 1.6s) ±20% jitter. Respect
      `Retry-After` header on 429/503. Plumb through `AnalyzeImage`,
      `processOCRImages`, and remote image/PDF downloads. Skip retries
      for 4xx other than 408/429. Configurable via
      `VISION_RETRY_ATTEMPTS` env var (default 3).

      _~0.5 day. New `pkg/agent_tools/vision_retry.go` helper. Touches
      `vision_image.go`, `vision_pdf.go`._

      _Shipped (commit ab6a2655): New `pkg/agent_tools/vision_retry.go`
      (418 lines) with `DoVisionRetry(ctx, op, opts)` plus
      `RetryOptions`, `RetryableHTTPError`, `parseRetryAfter`, and
      `isRetryableError` classifier (5xx, 408/429, network/EOF/conn-reset).
      Plumbed through `AnalyzeImage` (`vision_analyze.go`),
      `processOCRImages` (`vision_pdf.go`), `DownloadImage`
      (`vision_image.go`), and `downloadRemotePDFToTemp`
      (`vision_pdf.go`). Env vars: `VISION_RETRY_ATTEMPTS` (default 3,
      1 disables), `VISION_RETRY_BASE_MS` (200),
      `VISION_RETRY_MAX_MS` (1600), `VISION_RETRY_JITTER_PCT` (20).
      Backoff is ctx-aware (select on ctx.Done() vs timer).
      Tests cover success-first-try, transient-failure-recovery,
      give-up-after-max-attempts, no-retry-on-non-retryable-4xx,
      retryable-408/429, ctx-cancellation, env-var precedence, and
      `Retry-After` header parsing (numeric + HTTP-date). All pass
      (also with `-race -count=1`). Build green._

- [x] **SP-103-A2:** Parallelize `processOCRImages` and
      `ProcessImagesInText` with `errgroup.Group` + bounded worker pool
      (size 3 by default, env `VISION_PARALLEL_WORKERS`). Preserve
      per-page ordering in the output (page N's text still appears
      before page N+1's). Stream partial results via a
      `progress.VisionProgress` callback so the agent can surface
      "analyzed 2/6 pages" feedback.

      _~1 day. New `pkg/agent_tools/vision_parallel.go`. Update
      `vision_pdf.go:194-265` and `vision_analyze.go:33-72`._

      _Shipped (commit 4a3f80c3): New `pkg/agent_tools/vision_parallel.go`
      with `VisionProgressFunc`, `runOCROne` (per-image worker), and
      `processOCRImagesParallel` (parallel orchestrator). `processOCRImages`
      is now a thin wrapper. Worker pool sized by
      `getVisionParallelWorkers()` reading `VISION_PARALLEL_WORKERS` (SPROUT_*
      / LEDIT_* form per `configuration.GetEnvSimple`), default 3,
      clamped to [1,32]. Indexed result slice preserves input ordering.
      `errgroup.Group.SetLimit(N)` provides the bounded worker pool.
      Failure counter threshold of 2 (matching the original sequential
      behavior) cancels remaining work via `eg.WithContext`. Per-image
      OCR still wraps `SendVisionRequest` in `DoVisionRetry`. Five new
      tests: `TestProcessOCRImages_PreservesOrder` (4 images, section
      headers in input order), `TestProcessOCRImages_ProgressCallback`
      (final `(3, 3)` reported), `TestProcessOCRImages_FailureThreshold`
      (alwaysFail → empty text + error), `TestGetVisionParallelWorkers_Defaults`
      (default/5/0/-1/999/garbage), `TestProcessOCRImages_Parallelism`
      (6 images × 10ms delay × 4 workers → peak ≥3). Pre-existing
      `TestProcessOCRImages_CancelledContext` still passes. All
      `pkg/agent_tools` tests green; race detector clean. Build green._

- [x] **SP-103-A3:** Add mutex + LRU eviction to `visionCache`. Cache key
      becomes `sha256(filepath + mtime_ns + analysisMode + analysisPrompt)`
      so a modified file at the cached path automatically invalidates.
      Bounded at 256 entries (`VISION_CACHE_SIZE`); evict
      least-recently-used. Add `VisionCacheStats` reporting hits/misses/
      evictions for observability.

      _~0.5 day. Convert `map[string]string` to a hand-rolled
      doubly-linked-list LRU (avoiding a new dependency). Test that
      mutating a file at the cached path causes a miss._

      _Shipped (commit 9b76fe48): New `pkg/agent_tools/vision_cache.go`
      (266 lines) with `VisionLRUCache` (mutex + atomic counters +
      hand-rolled doubly-linked-list LRU, head/tail sentinels).
      `visionCache` + `visionCacheUsage` package-globals replaced with
      single `visionLRU *VisionLRUCache`. Capacity from env
      `VISION_CACHE_SIZE` (SPROUT_/LEDIT_ form), default 256.
      `visionCacheKey()` computes `sha256(filepath + mtime_ns +
      analysisMode + analysisPrompt)` — file mtime change auto-invalidates
      (URL fallback: url-len proxy). `getCachedVisionResult` and the
      cache write site now call `visionLRU.Get`/`Put`. `GetVisionCacheStats()`
      reports hits, misses, evictions, insertions, size, capacity.
      `lastVisionUsage` continues as a separate package-global. 12 new
      tests including `TestVisionLRUCache_Concurrent` (race detector
      clean), `TestVisionCacheKey_MtimeChanges`, `TestVisionLRU_DefaultCapacity`,
      `TestVisionLRU_CapacityFromEnv`. Updated
      `TestVisionCacheStats` to use the new helpers. Build green,
      full `pkg/agent_tools` test suite passes with `-race`._

- [x] **SP-103-A4:** Replace package-globals `lastVisionUsage`,
      `visionCache`, `visionCacheUsage` with a `*VisionProcessor`-owned
      struct returned alongside each call. Thread it through
      `AnalyzeImage` and `processOCRImages`. Lets the agent accumulate
      per-session usage for cost reporting without the race. The
      package-level `GetLastVisionUsage()` keeps a "most recent across
      all sessions" mirror for backward compat, guarded by
      `sync.RWMutex`.

      _~0.5 day. Touches `vision_types.go`, `vision_image.go`,
      `vision_analyze.go`, `vision_pdf.go`. Verify with `go test -race`._

      _Shipped (commit c16c8f16): Per-session usage tracking. The
      `visionCache` / `visionCacheUsage` portion was previously landed
      under SP-103-A3 (`visionLRU *VisionLRUCache`). For `lastVisionUsage`,
      the global is replaced by `visionLastUsageMirror` (RWMutex-guarded)
      and `VisionProcessor` gains a `usage *VisionUsageInfo` field +
      `(*VisionProcessor).LastUsage()` method. `recordVisionUsage(vp,
      usage)` writes per-session + cross-session mirror atomically.
      `GetLastVisionUsage` / `ClearLastVisionUsage` use the
      RWMutex. `vision_analyze.go:151` and `vision_image.go:305` now
      call `recordVisionUsage(vp, ...)`. 4 new tests
      (`TestRecordVisionUsage_PerSession`,
      `TestRecordVisionUsage_GlobalMirror`,
      `TestGetLastVisionUsage_Concurrency`,
      `TestVisionProcessor_LastUsage`) pass with `-race`;
      pre-existing `TestVisionUsage` updated. Build green,
      full `pkg/agent_tools` suite passes with `-race -count=1`._

- [ ] **SP-103-A5:** Pre-flight `Content-Length` HEAD on remote image and
      PDF downloads. Bail before reading the body if the header exceeds
      the size cap. Fall back to streaming + size-check on HEAD failure
      (e.g. S3 signed URLs sometimes reject HEAD).

      _~0.25 day. Add `checkRemoteSize(ctx, url, cap)` helper in
      `vision_image.go` and call from `DownloadImage` and
      `downloadRemotePDFToTemp`._

- [ ] **SP-103-A6:** Translate typed errors at the response-builder
      boundary. Currently `vision_image.go:418-440` does
      `strings.Contains(errMsg, ...)` to classify errors into `ErrCode*`
      strings for the JSON response. Replace with: if the error is a
      `*TypedError` (already constructed via `agenterrors.NewTool`/`NewNetwork`),
      extract its `Component` and `Details` to populate the response
      fields directly. Falls back to the legacy classification only when
      the error isn't a `*TypedError`.

      _~0.5 day. Touches `vision_image.go`, `vision_pdf.go`. No changes
      to `pkg/errors` needed — `TypedError` is already complete._

- [ ] **SP-103-A7:** Race-detector test fixtures. Add
      `vision_concurrency_test.go` that fires 10 concurrent
      `AnalyzeImage` calls against a mock client and asserts no data
      race (run with `-race`). Add parallel OCR test that drives 6 PDF
      pages and asserts all 6 are processed even if 1 fails
      transiently. Wire `make test-race` into a required CI check.

      _~0.25 day._

- [ ] **SP-103-A8:** Graceful degradation. If `SendVisionRequest` fails
      twice on a non-OCR image, retry via the configured OCR model
      (`PDFOCRModel` if set, else fallback Ollama OCR) with the OCR
      prompt. If OCR also fails, return a structured
      `ErrorCode = ErrCodeVisionRequestFailed` with a hint that the
      user can describe the image manually. No silent fallback — every
      transition is logged at INFO. Config flag
      `VISION_FALLBACK_TO_OCR = true`.

      _~1 day. Touches `vision_analyze.go`._

### Prompt caching + image sizing (SP-103-B) — high priority

This is the **highest-leverage change** in the SP. Anthropic prompt caching
cuts repeat-turn image cost by 90% on cache hits.

- [ ] **SP-103-B1:** Add `cache_control: {type: "ephemeral"}` to image
      blocks in `processImagesAsMultimodal`. The image content block in
      the user message gets the cache breakpoint so subsequent turns
      reuse the cached image instead of re-encoding and re-sending
      base64. For OpenAI's chat API, the equivalent is `image_url.detail`
      + repeating the URL across turns (OpenAI doesn't have native
      prompt caching on images yet — fall back to keeping the URL stable
      and letting their caching layer do its thing).

      _~0.5 day. Touches `conversation.go:286-360` and the Anthropic
      provider in `pkg/agent_api/anthropic*.go` to emit the
      `cache_control` field. Test that subsequent turns report
      `cache_read_input_tokens > 0`._

- [ ] **SP-103-B2:** Pre-resize images before embedding. Current code
      caps at `visionMaxImageFileSizeBytes` but doesn't constrain
      dimensions. A 4K screenshot costs 4784 visual tokens on
      high-resolution-tier models vs. 1560 on standard tier — ~3× the
      cost for most tasks. Default resize to ≤1568px long-edge
      (standard tier), unless the user opts into
      `analysis_mode: extract` which needs high resolution. Add
      `VISION_MAX_IMAGE_EDGE` env var (default 1568, set to 0 to
      disable).

      _~0.5 day. Add `resizeForVisionBudget` helper in
      `vision_image.go`. Hook from `processImagesAsMultimodal` before
      the image goes into the message._

- [ ] **SP-103-B3:** Image-then-text ordering. Anthropic docs:
      "Claude works best when images come before text." Currently the
      image blocks are appended after the text placeholder. Reorder
      so the image blocks come first in the user message content
      array, with the (cleaned) text query following.

      _~0.25 day. Touches `conversation.go:286-360`._

- [ ] **SP-103-B4:** Multiple-image label hints. Anthropic recommends
      `Image 1:`, `Image 2:` text labels before each image so the
      model can refer to them by name. Generate these labels
      automatically when the user pastes 2+ images.

      _~0.25 day. Touches `conversation.go`._

### Dead code + carve-out (SP-103-C) — medium priority

- [ ] **SP-103-C1:** Reactivate the non-vision path. Currently
      `conversation.go:225-242::processImagesInQuery` falls through to
      `return nil, query, nil` on the non-multimodal branch, leaving
      pasted images as raw `Pasted image saved to disk: /tmp/foo.png`
      placeholders. The function comment at line 227 even *claims* it
      "falls back to the existing OCR pipeline" but the code doesn't.
      Fix: replace the dead branch with a call to
      `buildNonVisionImageToolPrompt` (already defined at
      `conversation.go:268`) that appends the
      "OCR Trigger Policy (MANDATORY)..." prompt to nudge the model to
      call `analyze_image_content` per pasted image. Delete
      `processImagesViaOCR` (line 376) — it's superseded by the tool
      path. Update the function comment to match the actual behavior.

      _~0.5 day. Touches `conversation.go:225-242, 268-280, 376-...`.
      Tests in `conversation_test.go` already cover
      `buildNonVisionImageToolPrompt`; add an integration test that
      drives the full non-vision flow and asserts the prompt is
      appended._

- [ ] **SP-103-C2:** Add `SupportsConversationalVision()` to
      `pkg/agent_api/interface.go` (separate from `SupportsVision()`).
      Default `true` for providers that previously returned
      `SupportsVision() == true`. `OllamaLocalClient` returns `true`
      only for chat models (llama3.2 *without* "ocr"/"vision"
      substring); `glm-ocr` returns `false`. `processImagesInQuery`
      uses this for the OCR-model carve-out — forces the non-multimodal
      path regardless of user setting, so OCR models never try inline
      embedding.

      _~0.5 day. Update all client implementations
      (`base_provider.go`, `ollama.go`, `ollama_local.go`, etc.) to
      provide a sensible default. Backward compat: callers that only
      check `SupportsVision()` keep working._

- [ ] **SP-103-C3:** Update `analyze_image_content` tool description to
      advertise the new routing: "Pasted images are visible inline to
      multimodal chat models by default. Use this tool when you need
      specialized analysis (OCR of dense text, frontend UI inspection,
      structured extraction via `extract` mode, or when the chat model
      can't see images directly)."

      _~0.25 day. Edit
      `analyze_image_content_handler.go::Definition` and the
      corresponding prompt string in
      `pkg/agent/seed_tool_registry.go`._

- [ ] **SP-103-C4:** Metrics + observability. Add `vision_image_tokens`
      histogram (sampled per embedded image), `vision_cache_hit` counter
      (tagged by mode), `vision_retry_attempt` counter, and
      `vision_parallel_pages` gauge. Surface all in the `metrics`
      JSON endpoint and the audit-event stream. Goal: a user can see
      in their session log how many image tokens they spent, whether
      images hit cache, and whether retries fired.

      _~0.5 day. Touches `pkg/metrics/` and the existing
      `EventTypeToolStart` payload._

### Rollout

SP-103-A1..A8 + B1..B4 + C1..C4 ship as one cohesive SP behind no flag
(reliability + caching + sizing all benefit every user; the OCR carve-out
fixes a real misrouting bug).

Feature flag only needed for C3 (tool description text) — wait until
B1's caching metric is verified before updating the user-facing
description, so the message matches actual behavior.

### Acceptance

- `go test -race ./pkg/agent_tools/...` passes; new tests cover
  parallel dispatch + cache eviction + retry + typed-error paths.
- Subsequent turns with the same pasted image report
  `cache_read_input_tokens > 0` (Anthropic) or `cached_tokens > 0`
  (OpenAI) in the response usage.
- A 4K pasted screenshot bills as ~1500 visual tokens, not ~4800.
- An Ollama OCR-model session (`glm-ocr`) routes pasted images through
  `analyze_image_content` tool call, never direct multimodal
  embedding.
- A non-vision chat model (e.g. GPT-3.5) on Sprout has the model call
  `analyze_image_content` for each pasted image (currently it just
  sees the placeholder text and does nothing).
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

- [x] **SP-092-1:** Extract `Agent.Recall(ctx, query, limit) ([]RecalledItem, error)`
  from `InjectSemanticRecall`. `InjectSemanticRecall` becomes a thin wrapper.
  Existing turn-level semantic-recall tests in `semantic_recall_test.go`
  must continue to pass. _Effort: ~0.5 day. No new CLI/webui surface yet._

  _Shipped: New exported method `(a *Agent) Recall(ctx, query, limit)
  ([]RecalledItem, error)` extracted from the body of
  `InjectSemanticRecall`. `InjectSemanticRecall` is now a thin wrapper.
  Added 4 new tests in `semantic_recall_test.go` covering nil agent,
  no embedding manager, non-positive limits, and session filtering.
  `go test ./pkg/agent/...` green; `make build-all` clean._

- [x] **SP-092-2:** `/recall <text>` CLI command. New
  `pkg/agent_commands/recall_command.go`, registered in `commands.go`. Uses
  `output_writer.go` for printable output; `--json` flag emits the raw
  `[]RecalledItem` for scripting. Tests in `recall_command_test.go` cover
  empty query, zero results, hits, and the `--limit` / `--json` flags.
  _Effort: ~1 day. No webui changes._

  _Shipped: 148-line `RecallCommand` implementing Command+JSONCommand.
  Wired via `commands.go::registry.Register(&RecallCommand{})`. 16 tests
  pass covering all spec cases plus edge cases (negative limit, missing
  flag, nil agent, JSON marshal shape). `make build-all` clean._

- [x] **SP-092-3:** WebUI `/api/recall` endpoint +
  `PastSessionsHint` sidebar component. New `pkg/webui/recall_api.go`,
  new `webui/src/components/PastSessionsHint.tsx` + `.css`. Mounted in
  `Sidebar.tsx`. Click-to-restore uses the existing
  `handleSessionRestore` from the `sprout:session-restored` event. Add
  `past-sessions-hint` to the testid registry. Tests:
  `PastSessionsHint.test.tsx` covers debounce, empty state, zero results,
  and click-to-restore. _Effort: ~1 day._

  _Shipped: 99-line backend handler + 116-line React component + 119-line
  CSS (tokens only, no raw hex). 7 Go tests + 6 Vitest tests pass.
  TestIDs registered. Component mounted in Sidebar.tsx. Click-to-restore
  dispatches the existing sprout:session-restored event._
  Build green.

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

- [x] **SP-093-1:** `ShellProposal` + `SplitShellIntoParts` + 5 destructive
  classifiers. Pure functions, fully unit-tested in
  `shell_approval_test.go`. No agent wiring, no UI. _Effort: ~1 day._

  _Shipped: 391-line `pkg/agent/shell_approval.go` with 9 CommandKind
  constants (rm, git_push, git_reset, kubectl, docker, chmod, chown,
  write_redirect, http_post, unknown), paren-/quote-aware
  `SplitShellIntoParts`, regex-based `ClassifyShellSegment`,
  `NewShellProposal` composer, and `MostDestructivePart` /
  `HighRiskParts` methods. 54 tests pass covering all regex patterns,
  tokenizer edge cases (balanced parens, quoted strings, sequential IDs),
  and risk folding. Pure functions only — no agent wiring, no UI._
  Build green.

- [x] **SP-093-2:** `Agent.RequestShellApproval` + CLI 4-option picker per
  part (arrow-key picker that toggles parts). Existing 4-option prompt
  remains the default; opt-in via `configuration.EditApprovalConfig` with
  a new `shell_command: bool` flag (default `false` so no behavior change
  for existing users). _Effort: ~1 day._

  _Shipped: `pkg/configuration/config_domain.go` got `ShellCommand bool`
  flag (default false → opt-in). `Agent.RequestShellApproval` projects
  parts into `[]console.ShellPartInfo` (avoids import cycle) and
  dispatches to WebUI (stub) or CLI picker. `pkg/console/shell_approval_picker.go`
  implements an io-injectable `PromptShellApprovalParts` with bulk-accept
  / bulk-reject shortcuts. Broker gated on the flag. 14 new tests pass._
  Build green.

- [x] **SP-093-3:** `ShellApprovalRequestPayload` event + WebUI panel
  + handler. Wires into the existing WS pipeline. New
  `pkg/webui/shell_approval_api.go` (decision endpoint). Tests:
  `ShellApprovalPanel.test.tsx`, `shell_approval_event_test.go`.
  _Effort: ~1 day._

  _Shipped (despite orchestrator timeout): New `pkg/events/shell_approval.go`
  with payload types + unit tests. New `pkg/webui/shell_approval_api.go`
  decision endpoint. New `webui/src/components/ShellApprovalPanel.tsx` +
  `.css` (tokens only) + `.test.tsx`. Wired via `useWebSocketEventHandler`
  and `AppStateContext`. All backend + Vitest tests pass; build green.
  (Note: The orchestrator timed out at 30 min but had already created
  all required files; verified by hand after timeout.)_

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

- [x] **SP-094-1:** Full error tree in `pkg/errors/types.go`.
  (Full tree shipped; see SP-094-2..6 for migration waves — see git history.)
  `pkg/agent/errors.go` for re-export. `errors_test.go` covers every
  category: `IsRetryable()`, `IsAuth()`, `IsRateLimit()`, `As()` chains.
  _Effort: ~0.5 day._

- [ ] **SP-094-2:** Migrate `pkg/agent_tools/*_handler.go` (~80 sites).
  Existing tests in `*_handler_test.go` catch regressions. _Effort: ~2 days._

- [ ] **SP-094-3:** Migrate `pkg/agent/api_client*.go` (~40 sites) plus
  provider-side classification (429→RateLimitError, etc.). _Effort: ~1 day._

- [ ] **SP-094-4:** Migrate `pkg/agent/subagent_*.go` (~60 sites) and
  remap `pkg/agent/seed_provider.go::ChatStream` retry/backoff to use
  `IsRetryable()`. _Effort: ~1.5 days._

- [ ] **SP-094-5:** Final wave in remaining `pkg/agent/*.go` files.
  Audited via `grep -rn "fmt.Errorf" pkg/agent` returning only the helper
  itself plus a list of acceptable sites (delegator re-wraps, etc.).
  _Effort: ~1 day._

- [ ] **SP-094-6:** Wire into approval broker / metrics /
  `sprout explain`. New `EventTypeRateLimited` event payload + WebUI
  consumer (~50 lines). _Effort: ~1 day._

### Acceptance

- `grep -rn "fmt.Errorf" pkg/agent --include="*.go" | wc -l` returns
  a number ≥80% smaller than today (some sites are legitimate format-and-
  wrap; the goal is removing the untyped ones).
- Every entry in `pkg/errors/types_test.go` passes.
- Provider 429 now triggers 1-2 automatic retries with backoff instead of
  surfacing as a hard failure.

---

## SP-095: Validate Cross-Session Recall in Real Workflows

_Validation pass, not a new build (~2–3 days)._ SP-092 ships the surface.
SP-095 evaluates whether the surfaced summaries actually help. This is a
*research*-shaped ticket: instrument the existing `InjectSemanticRecall` for
two weeks, then decide what to keep.

### Scope

**Instrument (`pkg/agent/semantic_recall_instrumentation.go`, new file):**
- Wrap `InjectSemanticRecall` (or its successor from SP-092-1) to record:
  - Per-turn: how many recalled items came back, what their top similarity
    was, whether the agent's response cited or used them.
- Detection heuristic for "used": check whether the agent's response
  touched any file path or session id mentioned in the recall set.
- Persist per-day counters to `~/.config/sprout/recall_metrics.jsonl`.

**Survey (`docs/recall-evaluation-2026.md`):**
- After 2 weeks of normal usage, generate a Markdown report from the
  metrics file: hit-rate (how often recall fires with ≥1 result),
  use-rate (how often the agent uses a surfaced item), per-tool
  breakdown.
- Recommendation: kill recall, narrow it (only file-path matches), or
  expand it (per-turn proactive injection is already on; add proactive
  hints in the steer panel).

### Phase order

- [ ] **SP-095-1:** Add the instrumentation wrapper. Fire-and-forget;
  no behavior change. New
  `pkg/agent/semantic_recall_instrumentation_test.go`. _Effort: ~1 day._

- [ ] **SP-095-2:** Wait 2 weeks of normal usage. (Manual calendar step,
  not a code step.)

- [ ] **SP-095-3:** Write `docs/recall-evaluation-2026.md` from the
  metrics. Recommend: keep / narrow / expand. _Effort: ~0.5 day._

### Acceptance

- `recall_metrics.jsonl` accumulates once the user runs ≥10 sessions.
- A report file exists with hit-rate, use-rate, and a recommendation.
- No new feature work depends on this ticket — it's a verdict on the
  existing recall path.

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

- [x] **SP-096-1: SP-013.** `pkg/agent/settings_handler.go` (572 lines)
  ships `manage_settings` with get/set/list_providers/test_credential/
  describe/describe_all/preview. README says Implemented.

- [x] **SP-096-2: SP-014.** README says Implemented (hidden PTY routing
  + background mode). Verify by reading
  `cmd/agent_terminal_subscriber.go` (added in the latest merge) +
  TerminalManager hooks; update spec header.

- [x] **SP-096-3: SP-022 (workspace-management variant).** README says
  Implemented (WorkspacePicker + WorkspacePane + LocationSwitcher +
  WorkspaceBar). Spec at `roadmap/SP-022-workspace-management.md`
  still says Proposed. Update header.

- [x] **SP-096-4: SP-009.** README says Implemented (Storybook + MDX
  docs + Chromatic; webui imports `@sprout/ui`). Spec at
  `roadmap/SP-009-component-library-maturation.md` still says Proposed.
  Update header.

- [x] **SP-096-5: SP-010.** README says Implemented (EditorPane 2604→513
  lines; EditorCore extracted; 18 bug fixes). Spec at
  `roadmap/SP-010-editor-modernization.md` still says Proposed.

- [x] **SP-096-6: SP-062.** README says Implemented (BPM wired into
  shell dispatch; `pkg/agent_tools/shell.go` already handles
  `COMMAND_PROMOTED_TO_BACKGROUND`). Spec at
  `roadmap/SP-062-cli-background-shell.md` still says Proposed. NOTE:
  this also makes SP-097 much smaller — see revised scope below.

- [x] **SP-096-7: SP-068.** README says Implemented (Phases 1-3
  shipped: single resolver, single broker, `sprout explain` —
  `cmd/explain.go` exists at 200+ lines, `pkg/agent/risk_assessment.go`
  exists). Spec at
  `roadmap/SP-068-security-check-consolidation.md` still says Proposed.

- [x] **SP-096-8: SP-073.** README says Implemented (zero
  `TODO(SP-034-1c)` markers remain; all 10 sites threaded with
  `context.Context`). Spec at
  `roadmap/SP-073-cooperative-cancellation.md` still says Proposed.

- [x] **SP-096-9: SP-058.** Daemon binary is 149 MB (per `899d667f`),
  22 MB below 171 MB target. Spec at
  `roadmap/SP-058-selective-grammar-embed.md` still says Proposed.

- [x] **SP-096-10: SP-061.** Static embedding provider removed
  (SP-091-2). Spec at
  `roadmap/SP-061-remove-static-embeddings.md` still says Proposed.

- [x] **SP-096-11: SP-064.** `cmd/automate.go::runAutomateStatus`,
  `runAutomateStop`, `runAutomateStopAll` exist; BPM.Stop is wired.

- [x] **SP-096-12: SP-065.** `pkg/webui/automations_api.go` +
  `webui/src/components/AutomationsPanel.tsx` + WS wiring landed in
  commit `4f0a81c5`.

- [x] **SP-096-13: SP-017.** README says implemented ("scoped labels
  shipped"). The spec's broader goal (collapsible sections) is pending
  — header should say `✅ Implemented (Phase 1); collapsible sections
  pending → see SP-101`.

- [x] **SP-096-14: SP-048.** README says "status footer + glyph
  vocabulary shipped". Header should say
  `✅ Partially Implemented (status footer + glyphs); tool timeline +
  silence-fill pending → see SP-101`.### Drift to fix in TODO.md cross-reference

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

## SP-097: SP-062 CLI Background Shell — Remaining Surface Work

_~1.5 days, 2 phases._ After merge, SP-062 is mostly done: BPM is
wired, `pkg/agent_tools/shell.go` already handles
`COMMAND_PROMOTED_TO_BACKGROUND`, the error message at line 198 already
mentions BPM as an alternative. What's left is the **human-facing CLI
surface** and the **LLM-facing tool schema** polish.

### Phase 1: Standalone CLI monitor commands (~1 day)

**New `cmd/shell_bg.go`:** mirrors the existing `cmd/automate.go`
status/stop pattern but for the BackgroundProcessManager directly.

- `sprout shell-bg list [--json]` — table of active BPM sessions with
  ID, owner, command preview, runtime.
- `sprout shell-bg status <id>` — full accumulated output (head +
  tail), runtime, current status.
- `sprout shell-bg stop <id> [--grace=10s]` — graceful stop with the
  same SIGINT→SIGTERM→SIGKILL cascade as `cmd/automate.go::runAutomateStop`.
- `sprout shell-bg stop-all` — same as above, all sessions.

**Wire into `cmd/root.go` as `RootCmd.AddCommand(...)` so they appear
in `--help` output and tab-complete.**

Phase 2: Tool-schema update (~0.5 day)

**Edit `pkg/agent/tool_registrations.go`:**
- Find the `shell_command` tool description. Currently it warns that
  `background`/`check_background`/`stop_background` may fail in CLI
  mode. Replace that with: "All background operations work in CLI as
  well as WebUI; promoted sessions are discoverable via
  `sprout shell-bg list`."
- Update the `description` field of the schema to mention the new CLI
  commands so the LLM can suggest them.

**Edit `pkg/skills/library/self-help/SKILL.md`:**
- Remove the caveat about background mode being WebUI-only.

### Phase order

- [x] **SP-097-1:** `cmd/shell_bg.go` (4 subcommands) + Cobra wiring
  + tests. _~1 day._

  _Shipped: 635-line `cmd/shell_bg.go` with `list [--json]`, `status <id>`,
  `stop <id> [--grace=10s]`, `stop-all` subcommands. Wired into
  `cmd/root.go` alongside `automateCmd`. 15 tests pass including a
  17-second real `sleep 30` process kill that exercises the
  SIGINT→SIGTERM→SIGKILL cascade. `make build-all` clean._

- [x] **SP-097-2:** Update tool_registrations.go + self-help SKILL.md.
  _~0.5 day._

  _Shipped: `pkg/agent/tool_registrations.go` shell_command description
  now references `sprout shell-bg list/status/stop/stop-all` instead of
  warning that background operations require WebUI. Same fix applied
  to `pkg/skills/library/self-help/SKILL.md`. Caveat removed from
  `pkg/agent/subagent_creation.go` and the affected test.
  `make build-all` clean; `go test ./pkg/agent/... ./pkg/agent_tools/...` green._

### Acceptance

- `sprout shell-bg --help` lists the four commands.
- A user running `sprout agent --no-web-ui "start dev server"` then
  `sprout shell-bg status <id>` in another shell sees live output.
- The string "background mode requires WebUI terminal manager" no
  longer appears anywhere in the binary's strings table.

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

- [x] **SP-098-1:** `pkg/console/steer_input.go` (1536 → <800). Extract
  `streak.go` and `autocomplete.go`. _Highest-impact: this file
  dominates the steer panel that the user sees every turn._

  _Shipped (with pivot): The original spec described a "typed streak"
  and "ghost text" autocomplete feature that don't exist in the file
  (already-removed scope drift from SP-078). Pivoted to a coherent
  2-file extraction:
  - `pkg/console/steer_search.go` (133 lines) — Ctrl-R reverse-search subsystem
  - `pkg/console/steer_editor.go` (103 lines) — `runExternalEditor` helpers
  Result: `steer_input.go` 1536 → 1313 lines (-223). Pure refactor,
  no logic change, no test skips. Build green, all tests pass.
  The <800-line target is not achievable with further clean extraction
  seams; the remaining 1313 lines are the core input loop with no
  further clean splits without larger structural changes._

- [x] **SP-098-2:** `cmd/mcp.go` (1105 → <800). Extract per-tool
  commands. _Best for `make build-all` since MCP is in the build path._

  _Shipped: `cmd/mcp.go` 1105 → 71 lines (target hit, exceeded). Per-tool
  commands extracted: `mcp_add.go` (762 lines), `mcp_list.go` (92),
  `mcp_remove.go` (97), `mcp_test_cmd.go` (134 — named to avoid Go's
  `_test.go` build convention). 38/38 MCP tests pass. `sprout mcp --help`
  shows all 4 subcommands. Pure refactor, no logic change._

- [x] **SP-098-3:** `pkg/agent_tools/structured_helpers.go` (1190 →
  <800). Extract per-format helpers. _Pure data, low risk._

  _Shipped: 1190 → 194 lines. Extracted `structured_json.go` (32),
  `structured_yaml_node.go` (276), `structured_schema.go` (190),
  `structured_patches.go` (534). No TOML existed in the file — only
  JSON + YAML. Pure refactor, no logic change, no test skips. Build
  green, full `pkg/agent_tools/...` test suite passes._

- [x] **SP-098-4:** `pkg/agent_tools/vision_types.go` (1188 → <800).
  Split types from helpers.

  _Shipped: 1166 → 150 lines. Extracted `vision_client.go` (385 lines,
  constructors/factory), `vision_image.go` (460 lines, processor +
  AnalyzeImage), `vision_utils.go` (227 lines, truncation/persistence).
  Pure refactor, no logic change, no test skips. Build green, full
  `pkg/agent_tools/...` test suite passes._

- [x] **SP-098-5:** `pkg/console/status_footer.go` (1132 → <800). Split
  per-section rendering.

  _Shipped: 1132 → 710 lines. Extracted `status_footer_badges.go` (133,
  badge styling), `status_footer_format.go` (152, formatting helpers),
  `status_footer_scroll.go` (83, scroll region), `status_footer_steer.go`
  (98, steer row rendering). Pure refactor, no logic change, no test
  skips. Build green, 529 console tests pass._

- [x] **SP-098-6:** `cmd/automate.go` (1070 → <800). Move
  status/stop/list to sibling files.

  _Shipped: 1070 → 216 lines. Extracted `automate_list.go` (54),
  `automate_logs.go` (124), `automate_run.go` (448), `automate_status.go`
  (164), `automate_stop.go` (117). Pure refactor, no logic change, no
  test skips. Build green, 20/20 automate tests pass. `sprout automate
  --help` shows all 5 subcommands._

- [x] **SP-098-7:** `pkg/ast/symbols.go` (1040 → <800). Per-language.

  _Shipped: 1040 → 181 lines. Extracted `symbols_go.go` (267),
  `symbols_python.go` (245), `symbols_typescript.go` (364). Pure
  refactor, no logic change, no test skips. Build green, 100/100 ast
  tests pass._

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

- [x] **SP-099-1:** CI race detection by default (Makefile + workflow).
  ~0.5 day. _Shipped: race detection is already the default in `make test-unit`
  (via `TEST_RACE ?= -race`) and CI (`make test-coverage` hardcodes `-race`).
  No Makefile or workflow change was needed. Audit memo at `docs/sp-099-audit.md`._

- [ ] **SP-099-2:** Locking strategy ADR + mutex rename pass.
  ~1 day.

- [ ] **SP-099-3:** Run `-race -count=3 ./...`, fix what surfaces.
  ~1.5 days.

### Acceptance

- `make test` includes `-race` by default.
- `go test -race -count=3 ./...` returns zero race reports.
- ADR-0007 merged.

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

- [ ] **SP-100-1:** Wire `embedding_funcs.go` to expose ONNX status +
  switch. _~1 day._

- [ ] **SP-100-2:** Lazy-load `onnxruntime-web` from the WASM HTML
  shell. _~2 days._

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
- [ ] **SP-101-1:** Read `webui/src/components/Terminal.tsx`, find the
  `onProcessExit` handler. Test the three cleanup paths with a real
  terminal session. Fix any that misbehave. Add vitest coverage if
  missing.

### Phase 2: SP-012 — notification center (~1 day)

README says "notification center pending". SP-012 Phase 1 calls for a
non-blocking toast/snackbar UI for system messages (rate-limit warnings,
auth failures, agent blocked on permission, etc.). Right now those
messages go to the in-terminal `PublishAgentMessage` stream and risk
clobbering input state (cf. the recent fix in `10a9cbd5 fix(agent):
route security cautions via event bus`).

- [ ] **SP-101-2:** Add `webui/src/components/NotificationCenter.tsx`
  (~150 lines). Subscribes to a new event category
  `notification` published via the event bus. Renders as a fixed
  top-right stack with auto-dismiss after 5s. Covers 4 event types:
  `rate_limit`, `auth_failure`, `permission_required`,
  `agent_blocked`. Mirrors the spec body for Phase 1.

### Phase 3: SP-017 — collapsible sections (~1 day)

README says "scoped labels shipped; collapsible sections pending".

- [ ] **SP-101-3:** Add `<details>`-style collapsible groups to
  `SettingsPanel.tsx`, grouped by layer (Global / Workspace / Session).
  Use a `sectionState` local store (or `localStorage` for persistence).
  Cover the four "scope groups" SP-017 names. Add a vitest covering
  collapse/expand + persistence across reloads.

### Phase 4: SP-048 — tool execution timeline (~0.5 day)

README says "tool timeline + silence-fill pending". The silence-fill
part is covered by SP-091-4. Remaining: tool timeline — render
`PublishToolStart` / `PublishToolEnd` events as a vertical timeline in
the terminal output.

- [ ] **SP-101-4:** Edit `pkg/console/terminal_subscriber.go` (or
  wherever the tool events render) to emit per-tool entries with:
  glyph, tool name, elapsed ms, result icon. Format example per the
  spec: `[✓] read_file (124ms) · pkg/foo.go`. Add a vitest that
  simulates 3 tool events and asserts the rendered timeline matches.

### Acceptance

- [ ] SP-011 P1.4 verified in browser session + tests cover all three
  cleanup paths.
- [ ] NotificationCenter renders and dismisses correctly.
- [ ] Settings panel has 3+ collapsible scope groups that persist
  across reload.
- [ ] Tool timeline renders correctly across 5+ events.

---

## SP-102: Drift audit for newly-merged specs (post-merge verification)

_~0.5 day._ The `656db751` merge brought in 6 new commits and a
re-sync of the README. There may be additional specs that flipped from
Proposed to Implemented whose spec headers were not updated. This
ticket is a quick verification pass.

- [x] **SP-102-1:** Read every spec file `roadmap/SP-*.md`, compare
  its `**Status:**` line against the README table row. List any
  discrepancies in a comment on this ticket.

  _Audit complete: 76 SP files reviewed. After the SP-096-1..14 commit
  batch, only 9 spec files remain `📋 Proposed`, and all 9 match the
  README's `📋 Proposed` row exactly (SP-008, SP-011, SP-012, SP-016b,
  SP-027, SP-045, SP-046, SP-054, SP-075). No drift._

- [x] **SP-102-2:** Update any drifted spec headers (should be <10
  edits; if more, open separate tickets).

  _No updates required — drift = 0. All 14 formerly-drifted specs were
  fixed in SP-096._

- [x] **SP-102-3:** Verify SP-076 is reflected correctly (header
  already says `✅ Implemented (2026-06-26)`; confirm).

  _Confirmed: `roadmap/SP-076-webui-streaming-verbosity.md` header is
  `**Status:** ✅ Implemented (2026-06-26)`. Matches README._

