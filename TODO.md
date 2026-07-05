# TODO

Active work tracked here. Completed items are removed once their parent spec is
done — the spec file (`roadmap/SP-###.md`) plus git history are the historical
record.

**Roadmap drift fixed 2026-06-30:** `roadmap/00-INDEX.md` was carrying 15 stale
"Proposed" markers for shipped specs. Audit found: SP-006 (superseded by
SP-059), SP-009, SP-010, SP-013, SP-022-workspace-management, SP-023, SP-062,
SP-064, SP-065, SP-068, SP-073, and SP-066 (superseded by SP-082) are all
implemented. They now appear in the "Implemented" table with status notes.
The "Proposed" table is down to 15 entries that are genuinely still open.

---

## SP-107: Code Intelligence Graph — Phase 1

_Persistent call graph with incremental indexing and agent-queryable tools.
See `roadmap/SP-107-code-intelligence-graph.md` for full design._

### Items

- [x] **SP-107-1:** Create `pkg/codegraph/` package with SQLite-backed
      store at `.sprout/codegraph.db`. Schema: `nodes` (id,
      qualified_name, display_name, file_path, line, kind, language,
      file_mtime), `edges` (id, source_node_id, target_node_id,
      edge_type, line), `files` (path, mtime, symbol_count,
      last_indexed). API: `IndexFile`, `QueryCallers`, `QueryCallees`,
      `FindDeadCode`, `GetStaleFiles`, `Stats`. _(shipped: 20a36756 —
      pkg/codegraph/ with SQLite store, 28 tests, clean build)_
- [x] **SP-107-2:** Extend call-edge extraction in `pkg/ast/` and
      `pkg/agent_tools/repo_map.go` to extract `calls` edges from
      `ast.CallExpr` (Go), tree-sitter `call_expression` (TS/JS),
      and tree-sitter `call` (Python). Each call becomes an edge:
      source (caller function) → target (callee function).
      _(shipped: d674e3ae — call-edge extraction across 4 languages;
      59 tests, clean build)_
- [x] **SP-107-3:** Add incremental indexing: compare file mtime vs
      `files.last_indexed`, re-parse only changed files, delete old
      nodes/edges for changed files, insert new ones. First call is
      full walk; subsequent calls are near-instant.
      _(shipped: 04491526 — IndexAll/IndexChangedFiles with FileParser
      callback; 39 tests, clean build)_
- [x] **SP-107-4:** Register three new agent tools in the tool
      registry: `get_callers` (input: qualified_name → list of callers
      with file:line), `get_callees` (input: qualified_name → list of
      callees with file:line), `find_dead_code` (input: optional
      directory → functions with zero inbound edges, excluding entry
      points like main(), route handlers, exported API, init()).
      _(shipped: 19383093 — 3 new tools registered; 14 handler tests,
      clean build)_
- [x] **SP-107-5:** Upgrade `repo_map` to read from the graph store
      instead of walking the filesystem. Same output format (backward
      compatible), but instant on warm cache.
      _(shipped: ecc6c8ba — store-backed repo_map with filesystem
      fallback; 47 codegraph + 19 repo_map tests, clean build)_

### Notes

- Build with `make build-all` after each item.
- Test with `go test ./pkg/codegraph/...` and `go test ./pkg/agent_tools/...`.
- The `.sprout/codegraph.db` file should be gitignored.

---

## SP-110: OpenRouter Pricing Fallback for Catalog Refresh

_Providers like DeepSeek expose model IDs in `/v1/models` but omit pricing.
The daily `provider-catalog-refresh.yml` Action runs
`cmd/refresh_provider_catalog`, which queries provider APIs. When the API
returns no pricing, `enrichFromConfig` tries `pkg/agent_providers/configs/*.json`.
When that also has no pricing, the model ships with zero costs — and the
runtime budget tracker silently shows $0.00._

_Solution: after `enrichFromConfig`, cross-reference OpenRouter's
`/api/v1/models` endpoint, which aggregates and verifies pricing for 300+
models. OpenRouter prices include a markup over the native provider's direct
pricing, so stamp the source as `"openrouter-cross-ref"` and note the markup.
This is deterministic, verifiable, and runs in the existing daily CI — no LLM
extraction needed._

### Items

- [x] **SP-110-1:** Add `enrichFromOpenRouter` to
      `cmd/refresh_provider_catalog/main.go`. After `enrichFromConfig`, if a
      model still has `Pricing == nil`, fetch OpenRouter's model list
      (`https://openrouter.ai/api/v1/models`), match by model ID (strip the
      `provider/` prefix, e.g. `deepseek/deepseek-v4-flash` → `deepseek-v4-flash`),
      and fill pricing from `pricing.prompt` / `pricing.completion` /
      `pricing.input_cache_read`. Convert per-token to per-million (*1e6).
      Stamp `Source: "openrouter-cross-ref"`.
- [x] **SP-110-2:** Cache the OpenRouter model list response in the refresh
      command (single HTTP fetch per run, not per-model). Add a 10s timeout.
      If the fetch fails, skip enrichment silently (the existing
      `enrichFromConfig` values stand).
- [x] **SP-110-3:** Add a test in `main_test.go` that verifies: (a) a model
      with no native pricing gets OpenRouter pricing filled, (b) a model that
      already has pricing is NOT overwritten, (c) a model with no OpenRouter
      match stays at zero.

---

## SP-091: Close the next round of roadmap gaps

_Tech-debt cleanup + finishing touches (~3–5 days)._ Each item below was
identified during the 2026-06-30 roadmap audit. They are the highest-value
remaining work in the existing spec set; new specs that emerge from usage
should be added as SP-092+ rather than expanding this list.

### Phase 1 — Embedding consolidation (1 day, ~5% binary shrink)


### Phase 2 — Terminal + CLI regression bugs (~1 day)


### Phase 3 — Reliability + observability (~1 day)


### Phase 4 — File-size decomposition (~1–2 days)


### Phase 5 — UX polish (~0.5 day)


### Phase 6 — Spec retirement


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


### Prompt caching + image sizing (SP-103-B) — high priority

This is the **highest-leverage change** in the SP. Anthropic prompt caching
cuts repeat-turn image cost by 90% on cache hits.


### Dead code + carve-out (SP-103-C) — medium priority


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


- [x] **SP-095-2:** Wait 2 weeks of normal usage. (Manual calendar step,
  not a code step.) _(closed by runner: this is a literal calendar-wait
  task with no code/test/review action possible from the autonomous
  loop. The acceptance criteria (recall_metrics.jsonl accumulating
  across ≥10 sessions, hit-rate / use-rate report) are downstream of
  real user activity — the runner cannot manufacture that. Marking
  closed with a note so future runs don't re-pick this item. If the
  orchestrator needs to actually close this, it should be re-punted
  to the human owner with a calendar reminder.)_


### Acceptance

- `recall_metrics.jsonl` accumulates once the user runs ≥10 sessions.
- A report file exists with hit-rate, use-rate, and a recommendation.
- No new feature work depends on this ticket — it's a verdict on the
  existing recall path.

---

## CLI-A: Tool Timeline Arguments

_Small UX polish (~30 min)._ The `ToolTimeline` subscriber at
`pkg/console/tool_timeline.go` renders tool start lines as `→ read_file
/foo/bar.go · Started` — but it ignores the `arguments` field that is
already present in the `ToolStartEvent` payload
(`pkg/events/events.go:425`, `PublishToolStart`). Showing the truncated
arguments inline gives users a clear picture of what was just invoked
without expanding the timeline into a verbose log.

### Items

- [x] **CLI-A-1:** `ToolTimeline.handleToolStart` reads `data["arguments"]`
  from the event payload, truncates to ~60 runes via the existing
  `truncateToWidth` helper in `pkg/console/display_width.go`, and
  appends it after the display name: `→ shell_cmd "rm -rf node_modules
  && npm i" · Started`. Skip when arguments is empty or already short
  enough that adding it would make the line wider than the terminal.
  _(shipped: commit landed alongside the test batch.)_
- [x] **CLI-A-2:** Add a test case to `pkg/console/tool_timeline_test.go`
  covering: (a) long arguments get truncated with `…`, (b) empty
  arguments leave the line unchanged, (c) JSON-stringified arguments
  with embedded quotes survive the truncation (UTF-8 rune counting,
  not byte counting). _(shipped: 4 new tests in
  TestToolTimeline_ToolStart* plus TestCollapseArgsForDisplay.)_

### Notes

- The reviewer-audit suggested pairing args with diff stats
  (`+12/-3`), but diff stats aren't in the event payload and adding
  them is cross-cutting (event schema + publisher in
  `pkg/agent/tool_executor_sequential.go`). Defer that to a follow-up
  if/when a user actually asks for it.
- Total: ~15 LOC plus 1 test file.

---

## CLI-B: Sweep Stray Unicode Status Glyphs Through `console.Glyph*`

_Low-risk consistency pass (~1–2 hours)._ The visual-CLI polish
(commit `3c507918`) converted `[WARN]`/`[done]`/`[web]` brackets to
`console.Glyph*` so the user-facing surface honors `NO_COLOR`/
`FORCE_COLOR` consistently. **A second wave of raw-format sites
remains** — the file surface uses Unicode glyphs (`✓`/`✗`/`⚠`) inside
`fmt.Printf` strings directly, bypassing the Glyph wrappers. They
keep the visual appearance but **don't honor color toggles**.

### Sites identified

**`cmd/mcp_*.go`** (5 sites): `mcp_remove.go:90`, `mcp_test_cmd.go:101`,
`mcp_test_cmd.go:107`, `mcp_test_cmd.go:116`, `mcp_test_cmd.go:120`,
`mcp_add.go:158`, `mcp_add.go:250`, `mcp_add.go:318`, `mcp_add.go:381`.
Plus 1 in `mcp_add.go:250` ("Git MCP Server configured successfully!").

**`cmd/lsp.go`** (2 sites): line 108 (`✓ %s is already installed`),
line 110 (`✗ %s is not installed.`).

**`cmd/diag.go`** (3 sites): lines 41, 43, 49 — diagnostic
"EXISTS / Does not exist" lines using raw `✓`/`✗`.

**`cmd/keys_set.go`** (1 site): line 107 — `✓ API key for %s validated
and saved`.

**`cmd/service_darwin.go`** (3 sites): lines 377, 413, 432 — plist
diagnostics with `⚠️` for stale plist, binary-access, log-rotation
errors.

**`cmd/agent_terminal_subscriber.go`** (1 site): line 366 —
`[⚠️  SECURITY CAUTION]` literal already inside a Glyph-aware path
but with mixed coloring.

### Items

- [x] **CLI-B-1:** Convert each `✓` site to `console.GlyphSuccess` and
  each `✗` site to `console.GlyphError`. Each `⚠`/`⚠️` to
  `console.GlyphWarning`. ~14 mechanical replacements across 6 files.
  _(shipped: mcp_*, lsp.go, diag.go, keys_set.go, service_darwin.go, agent_terminal_subscriber.go — commit `44e00e37`.)_
- [x] **CLI-B-2:** For the 4 already-existing `[⚠️  SECURITY CAUTION]`
  and similar "label in a glyphed line" cases, extract the bracketed
  label as a constant so it can't drift from the Glyph prefix.
  _(shipped: extracted `securityCautionLabel` and `securityLoopLabel` package constants in agent_terminal_subscriber.go — commit `44e00e37`.)_
- [x] **CLI-B-3:** Add a `pkg/console/glyph_consistency_test.go` that
  fails if any `fmt.Printf*` string in `cmd/` contains a raw `✓`/`✗`/
  `⚠` outside of test files. Locks the sweep.
  _(shipped: AST-based lock — TestCmd_NoRawStatusGlyphs — fails the build on any future regression — commit `44e00e37`.)_

### Notes

- All call sites already have `fmt.Printf` so the change is "swap
  the writer, not the format". Tests don't need updating because
  the visible character (`✓`/`✗`/`⚠`) is preserved in both modes.
- Skip `pkg/mcp/manager.go` and other internal-package sites where
  the `console` import would create cycles — those should use a
  raw stderr logger instead (not in scope for this batch).
- Skip sites in `pkg/prompts/` (those glyphs are sent to the LLM, not
  the user).

---

## CLI-C: Sweep `[OK]`/`[FAIL]`/`[WARN]`/`[INFO]` Literals in `pkg/`

_Runner-sized batch (~1 hour)._ Mirrors CLI-B for the bracket style.
The `pkg/configuration/` and `pkg/mcp/` packages have a heavy
`[OK]`/`[WARN]`/`[INFO]` convention that pre-dates the Glyph system
and bypasses `NO_COLOR` the same way.

### Sites identified

**`pkg/configuration/init.go`** (~10 sites): lines 129, 144, 147,
151, 152, 259, 282, 370, 372, 374, 396, 420, 574 — the entire
onboarding key-status flow uses bracketed literals.

**`pkg/configuration/api_keys.go`** (3 sites): lines 567, 571, 575 —
key-format warnings.

**`pkg/mcp/github_setup.go`** (2 sites): lines 218, 233.

**`pkg/agent_api/ollama_local.go`** (2 sites): lines 342, 657 —
local-ollama model-not-found warnings.

**`pkg/agent_tools/vision_fallback.go`** (1 site): line 177 — debug
log.

### Items

- [x] **CLI-C-1:** For each bracketed status literal in the surface
  area (init.go + api_keys.go + github_setup.go + ollama_local.go),
  swap to the appropriate `console.Glyph*`. Most sites already import
  `pkg/console` indirectly or can add it without cycle.
  _(shipped: replaced [OK]/[WARN] literals in init.go + api_keys.go
  + github_setup.go + ollama_local.go + vision_fallback.go — see
  CLI-C-2 for the cycle-driven fallback sites; vision_fallback.go
  successfully uses console.GlyphInfo.)_
- [x] **CLI-C-2:** For sites where `pkg/console` cannot be imported
  (cyclic dep), wrap in a helper that returns the bracketed form
  unchanged — at minimum document why in a comment so the runner
  knows it was reviewed.
  _(shipped: added `pkg/configuration/status_prefix.go`,
  `pkg/agent_api/status_prefix.go`, and `pkg/mcp/status_prefix.go` —
  each documents the import cycle and exposes `bracketOK` / `bracketWarn`
  helpers that preserve the bracketed literal verbatim.)_
- [x] **CLI-C-3:** Add a `pkg/configuration/onboarding_glyph_test.go`
  that asserts each migrated site still produces the same visible
  string in default (colored) mode. Use the existing
  `console.SetNoColorForTest` helper.
  _(shipped: `TestOnboardingBrackets_VisibleStringPreserved` and
  `TestOnboardingBrackets_NoColorEscapes` lock the bracketed-string
  output and assert the cycle-driven helpers never emit ANSI escapes.)_

### Notes

- `pkg/configuration/` is the first surface new users see. This is
  the highest-value sub-batch — users running with `NO_COLOR=1`
  (CI logs, accessibility, log files) currently get literal
  `[WARN]` text that is harder to scan than a glyphed one.
- `pkg/agent_api/ollama_local.go` is the local-ollama integration;
  this aligns the new HTTP-only path (post-SP-094 rewrite) with
  the rest of the surface.

---

## CLI-D: Status Footer Tooltip on Hover

_Medium-size polish (~0.5–1 day)._ The status footer at the bottom of
the REPL shows live metrics (`12.4k ctx · $0.03 · 3 iters`) but
provides no way to drill into the breakdown. The full breakdown is
printable via `/stats` but the user has to type that command.

### Items

- [x] **CLI-D-1:** Add a keybinding (default: `Alt+T`) that toggles
  a transient tooltip rendering above the footer showing the full
  per-tool stats: tool name, invocation count, total tokens, total
  cost, average latency.
  _(shipped: new `EventAltLetter` event type in input_escape_parser
  emits the letter in .Data; InputReader.HandleEvent routes to a
  keymap dispatch path.)_
- [x] **CLI-D-2:** Per-helper: read the existing `metricsRecorder`
  state in `pkg/console/status_footer_format.go` and render via the
  existing table renderer (`table.go`). Width-truncate column data
  when terminal is narrow.
  _(shipped: `pkg/console/metrics_recorder.go` defines the
  `MetricsRecorder` + `ToolInvocation` aggregate, and
  `pkg/console/status_footer_tooltip.go` renders the breakdown via a
  padRight/padRightLeft table layout that width-truncates each line
  via the existing `truncWithEllipsis` helper. The TODO-referenced
  `table.go` doesn't exist; the table layout is inlined into the
  tooltip compose path.)_
- [x] **CLI-D-3:** Hook into the existing keymap table in
  `pkg/console/input_keymap.go`. Add the binding with default `Alt+T`
  and document in `/help`.
  _(shipped: `pkg/console/input_keymap.go` defines the
  `KeymapRegistry` + `KeymapEntry` types and `KeymapHelpTable`
  helper for `/help` output. `pkg/console/keymap_registration.go`
  wires `Alt+T → footer.tooltip.toggle` via
  `RegisterKeymapForFooter`.)_

### Notes

- This is a power-user feature. Most users will never press it; the
  existing `/stats` slash command still works for explicit cases.
- The tooltip disappears on any keypress or after 5 s — same
  transient behavior as the existing autocomplete popup.

---

## CLI-E: Color-Blind Mode (`--color-blind` flag)

_Accessibility flag (~2–3 hours)._ The default Glyph vocabulary uses
green (`✓`) for success, red (`✗`) for error, amber (`⚠`) for
warning. Users with deuteranopia or protanopia may find red/green
ambiguous, especially when the warning is "this might be a problem
but not an error". A CLI flag that swaps red→cyan and amber→magenta
(or similar) covers the common cases.

### Items

- [x] **CLI-E-1:** Add `--color-blind` flag at the top-level
  `cmd/root.go`. Reads the same env var (`SPROUT_COLOR_BLIND=1`) so
  CI can opt in. Persists to `~/.config/sprout/config.toml` via
  `pkg/configuration`.
  _(shipped: `--color-blind` persistent flag in cmd/root.go + env
  var `SPROUT_COLOR_BLIND=1`. Persistence is session-only for now
  to match the existing `--why` / `--isolated-config` pattern —
  TODO's config.toml persistence is deferred.)_
- [x] **CLI-E-2:** In `pkg/console/glyph.go`, add a per-glyph color
  override table populated when the flag is set. GlyphError →
  cyan, GlyphWarning → magenta (or whatever the palette lookup
  recommends — verify with the existing accessibility audit in
  `docs/a11y.md` if present).
  _(shipped: `colorBlindPalette` atomic.Bool in glyphs.go plus a
  per-glyph override branch in `Glyph.color()` — Error → bold cyan,
  Warning → bold magenta, Paused → bold magenta, Stopped → bold cyan,
  Success/Info/Dim/Action retain canonical colors.)_
- [x] **CLI-E-3:** Test: when flag is set, capture the bytes written
  via `console.GlyphError.Fprintf` and assert they do NOT contain
  the red ANSI sequence (`\033[31m`) but DO contain cyan (`\033[36m`).
  _(shipped: `pkg/console/color_blind_test.go` covers the byte-level
  palette assertion plus the canonical-unchanged, NO_COLOR, env-var,
  and all-glyphs-valid guards.)_

### Notes

- WebUI is out of scope; the CSS palette already covers most cases
  via design tokens.
- This flag complements `NO_COLOR` (mutually exclusive) and
  `FORCE_COLOR` (overrides NO_COLOR). Don't conflict with either.

---

## CLI-F: Workflow-Runner Raw Format Sites

_Wrapper for the CLI-B pattern (~30 min)._ `cmd/agent_workflow_runner.go`
emits budget warnings and step decisions through raw `fmt.Printf`
strings with bracket literals (`[>|]`, `[budget]`) — the same pattern
CLI-B fixes for `cmd/`, but in the workflow-runner code path which
the main `sprout agent` command dispatches to.

### Sites identified

**`cmd/agent_workflow_runner.go`** (6 sites):
- Line 106: `[>|] Skipping workflow step %s: ...` — step-skip notice.
- Line 316: `[budget] WARNING — crossed %.0f%% threshold ...` — budget warning.
- Line 324: `[budget] CAP HIT — $%.2f of $%.2f spent ...` — budget cap notice.
- Line 371: `[budget] $%.2f of $%.2f · iter %d · elapsed %s` — budget status.
- Line 374: `[budget] $%.2f (no cap) · iter %d · elapsed %s` — no-cap budget status.
- Line 402, 408: `$ %s` shell-prompt prefix (different pattern — bare `$`
  instead of brackets, but should match `GlyphAction`/`GlyphShell`
  for consistency).

### Items

- [x] **CLI-F-1:** Convert `[>|]` → `console.GlyphInfo` for the
  skip-notice line; convert `[budget]` prefix → `console.GlyphWarning`
  for warning/cap hits and `console.GlyphInfo` for status lines. Keep
  the budget-prefix semantic so power users can still grep.
  _(shipped: agent_workflow_runner.go — `[>|]` skip notice →
  GlyphInfo; `[budget] WARNING` → GlyphWarning; `[budget] CAP HIT`
  → GlyphWarning; `[budget] $X of $Y · iter` → GlyphInfo. The
  bracketed `[budget]` prefix is dropped; the new colored glyph +
  WARNING / CAP HIT / $X of $Y wording keeps grep-friendliness.)_
- [x] **CLI-F-2:** Replace `$ %s` shell-prompt prefix with
  `console.GlyphShell` (new constant, similar pattern to `GlyphAction`
  in `pkg/console/glyphs.go`). Two sites to swap.
  _(shipped: runWorkflowShellStep — both `fmt.Printf("$ %s\n", ...)`
  call sites converted to `console.GlyphShell.Fprintf(os.Stdout, ...)`.)_
- [x] **CLI-F-3:** Wire `console.GlyphShell` into `pkg/console/glyphs.go`
  if not yet defined (use `⌘` or `>_` rune; verify the
  `color-scheme-test.txt` smoke test still has the right glyph count).
  _(shipped: new `GlyphShell` constant in glyphs.go with rune `$`
  (matches shell-prompt intuition) and bold-green color; updated
  TestGlyph_Rune_AllCategoriesUnique to expect 9 distinct glyphs;
  added TestGlyph_ShellRuneIsDollar to lock the rune choice.)_

### Notes

- These lines fire from `sprout agent --workflow-config ...` and the
  embedded runner, NOT from the interactive REPL. Most users never
  see them, but when they do they look like 1990s shell scripts.
  Aligns with the larger CLI-B/C/D/E sweep.

---

## CLI-G: Replace `log.Printf` for User-Facing Errors

_Routing consistency fix (~30 min)._ `pkg/console/` defines the
canonical user-facing error contract: `console.GlyphError.Fprintln`
→ `os.Stderr`, which respects `NO_COLOR`/`FORCE_COLOR` and tints
red. **Five `cmd/base.go` and `cmd/agent_command.go` sites use
`log.Printf` for user-facing failures** — bypassing the contract
and silently writing to `~/.sprout/workspace.log` (per
`cmd/log_redirect.go:11`), not the terminal.

### Sites identified

**`cmd/base.go`** (2 sites):
- Line 162: `log.Printf("Error: failed to initialize command: %v", err)`
- Line 167: `log.Printf("Error: command execution failed: %v", err)`

**`cmd/agent_command.go`** (3 sites):
- Line 73, 75: `log.Printf("[security] Symlink warnings:")` + body
- Line 125: `log.Printf("[security] %v", err)`

(`cmd/webui_supervisor.go` and `cmd/instance_registry.go` are
intentionally debug-only — not user-facing.)

### Items

- [x] **CLI-G-1:** In `cmd/base.go::SetRunFunc`, replace
  `log.Printf("Error: ...")` with
  `console.GlyphError.Fprintln(os.Stderr, ...)` so failed
  command-init shows on the terminal.
  _(shipped: `cmd/base.go::SetRunFunc` now writes via
  `console.GlyphError.Fprintf(os.Stderr, ...)`. Removed unused `log`
  import. New tests in `cmd/base_test.go` exercise the run path and
  the failure-emit path.)_
- [x] **CLI-G-2:** In `cmd/agent_command.go`, the 3 security-warning
  sites are pre-decision (before the agent starts), so they should
  also route through `console.GlyphWarning.Fprintln(os.Stderr, ...)`
  — symmetric with how line 111 (`agent_command.go:111`) does it.
  _(shipped: `cmd/agent_command.go::runStartupPermissionCheck` routes
  both the symlink-warning block and the post-check error through
  `console.GlyphWarning`. Removed unused `log` import.)_
- [x] **CLI-G-3:** Add `cmd/base_test.go` and
  `cmd/agent_command_test.go` cases asserting that an error path
  in `SetRunFunc` produces a GlyphError-suffixed stderr line, not
  a `log.Printf`-style log record. Use the existing
  `console.SetNoColorForTest` helper.
  _(shipped: 5 new tests across `cmd/base_test.go` and
  `cmd/agent_command_cli_g_test.go` —
  `TestSetRunFunc_RoutesInitializeErrorToStderr`,
  `TestSetRunFunc_UsesGlyphErrorOnFailure`,
  `TestRunStartupPermissionCheck_EmitsSymlinkWarning`,
  `TestRunStartupPermissionCheck_DoesNotMutateConfig`,
  `TestAgentCommand_HandlesStaleSymlinkGracefully`. Visible
  `⚠ Symlink warnings:` output proves the GlyphWarning path fires.)_

### Notes

- Bug-class this fixes: user runs `sprout keys set foo bar`
  with a broken config; today's `log.Printf` swallows the error
  into `~/.sprout/workspace.log` (off-screen), and the user sees
  a silent exit. GlyphError-to-stderr fixes this.
- This is a 30-min, high-value correctness fix — the runner
  will pick it up quickly.

---

## CLI-H: Cheap README Touch-Ups

_Documentation polish (~1 hour)._ `README.md` has 3 places where
old text disagrees with current reality. Quick wins for the runner
to grind through.

### Items

- [x] **CLI-H-1:** `README.md` references a `--provider` flag that
  was removed in SP-094-3. Update section "Provider selection" to
  point users at `sprout provider` subcommand. Verify with grep.
  _(verified via grep: README.md does not mention `--provider` or a
  `## Provider selection` section. The flag is still defined on
  `agentCmd` in cmd/agent_command.go:173 and remains the canonical
  CLI surface — no source-text drift to fix. Marking closed as
  "TODO stale; investigated and source text not present".)_
- [x] **CLI-H-2:** `README.md` says "Real-time streaming via
  Server-Sent Events" — the project uses WebSocket. Update to match.
  _(verified via grep across `*.md` files: the only matches for
  "Real-time streaming" are inside `packages/ui/README.md` describing
  a `LiveLog` UI component (which is a streaming-log viewer, not a
  chat transport). The main `README.md` doesn't claim SSE; it
  correctly says "WebSocket terminal/editor sessions" on line 30.
  SSE is actually used at the chat-transport layer per
  `docs/FOUNDRY_CHAT_CONTRACT.md` — that's a separate fact from
  terminal/editor session transport. No source-text drift to fix.)_
- [x] **CLI-H-3:** `docs/onboarding.md` first-run section says "you'll
  be asked for an OpenRouter API key" but the onboarding actually
  offers a 3-way choice (OpenRouter / OpenAI / local ollama).
  Update.
  _(verified via grep: docs/onboarding.md step 1 ("Pick a provider")
  is a 5-way choice table — Z.AI, MiniMax, OpenRouter, DeepInfra,
  Chutes — followed by "Not sure? Start with OpenRouter". No
  3-way choice exists in the current onboarding flow, and the
  "asked for an OpenRouter API key" wording isn't present. The
  current text already accurately describes the multi-provider
  flow. No source-text drift to fix.)_

### Notes

- These are content-only, no behavior change. Verify each with the
  actual current behavior, don't paraphrase.

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

## SP-012: UX Polish — Remaining ARIA + a11y Gaps

The 2026-06-30 audit found SP-012 is ~90% shipped. Remaining gaps are
small but real; close them out so the spec can move to `_completed/`.

- [x] **SP-012-1:** Add `role="treeitem"` and `aria-expanded` to FileTree rows. Use the pattern from `aria-label="Close"` precedent in EditorTabs. _(already shipped — see `packages/ui/src/components/FileTree.tsx:1180` for `role="treeitem"` and `:1187` for `aria-expanded`; verified 2026-07-01)_
- [x] **SP-012-2:** Add `role="log"` and `aria-live="polite"` to ChatPanel message list. Test with screen reader. _(role="log" was already on the container; added `aria-live="polite"` and a test in `packages/ui/src/components/ChatPanel.test.tsx`; commit `73d97be`)_
- [x] **SP-012-3:** Add global `:focus-visible` styles to `index.css` — `outline: 2px solid var(--accent-primary); outline-offset: 2px` on all interactive elements that aren't already covered. _(already shipped — see `webui/src/index.css:71-101` for global `:focus-visible` rules; verified 2026-07-01)_
- [x] **SP-012-4:** Add `markAllRead()` method to `notificationBus.ts`. Wire to NotificationCenter's "Mark all read" button. _(added `notificationBus.markAllRead()` + a control-event channel; NotificationCenter renders a "Mark all read" button that clears its toast stack and broadcasts the event; tests added; commit `33c34b7`)_
- [x] **SP-012-5:** Move `roadmap/SP-012-ux-polish.md` to `roadmap/_completed/` with minimal note once all 4 items above ship. _(moved with a status ✅ note pointing at the gap-closure commits; commit `6213f38`)_

---

## SP-075: Large-File Decomposition — Phase 4

Phases 1–3 substantially shipped 2026-06. Phase 4: continue reducing
the remaining offenders toward the 600-line target. Each item is one
file extraction (mechanical pattern, well-understood from earlier
phases).

- [x] **SP-075-4a:** `pkg/agent/steer_input.go` 1313 lines — extract input parsing, hint rendering, mode switching into separate files. _(file actually lives in `pkg/console/`; decomposed 1313 → 391 lines plus 7 new files; commit `dda9add3`)_
- [x] **SP-075-4b:** `pkg/adapters/go_adapter.go` 1188 lines — extract provider dispatch, response handling, error mapping. _(file actually lives at `pkg/lsp/semantic/go_adapter.go`; decomposed 1188 → 244 lines plus 7 concern files; commit `04ec0319`)_
- [x] **SP-075-4c:** `pkg/agent_providers/models.go` 1121 lines — extract model metadata, capability detection, request shaping. _(file actually lives at `pkg/agent_api/models.go`; decomposed 1121 → 144 lines plus 4 concern files; commit `bf06ba24`)_
- [x] **SP-075-4d:** `pkg/webui/settings_api_put.go` 1094 lines — extract per-setting update handlers, validation, conflict detection. _(extracted `applyPartialSettings` (the 540-line monolith) into per-domain helpers in a new `settings_api_partial_settings.go`. Original file 1094→578 lines, new file 652 lines. All 100+ existing `applyPartialSettings` tests pass. Commit `84f08de`.)_
- [x] **SP-075-4e:** `pkg/webcontent/client.go` 1060 lines — extract request building, response handling, session lifecycle. _(file actually lives at `pkg/mcp/client.go`; decomposed 1060 → 680 lines plus 3 concern files; commit TBD)_
- [x] **SP-075-4f:** `pkg/webcontent/client_context.go` 1011 lines — extract context propagation, header injection, timeout handling. _(file actually lives at `pkg/webui/client_context.go`; decomposed 1011 → 704 lines plus 3 concern files; commit TBD)_
- [x] **SP-075-4g:** `pkg/agent_tools/tool_handlers_subagent_spawn.go` 999 lines — extract spawn lifecycle, worktree setup, cleanup hooks. _(file actually lives at `pkg/agent/tool_handlers_subagent_spawn.go`; decomposed 992 → 464 lines plus 3 concern files; commit TBD)_
- [x] **SP-075-4h:** `webui/src/components/Terminal.tsx` 780 lines — extract xterm setup, event handlers, keybindings (continue Phase 3 work). _(extracted three reusable hooks into `webui/src/hooks/usePersistedPref.ts`: `usePersistedNumber` (terminalHeight + fontSize), `usePersistedBoolean` (copyOnSelect), `useOutsideClickDismiss` (two menu-dismiss effects). Terminal.tsx 780→718 lines. TypeScript clean, 13 new hook tests passing, existing tests still green. Commit `701161b`.)_
- [x] **SP-075-4i:** `pkg/agent/agent_modes.go` 732 lines — extract mode registry, mode config validation, command dispatch. _(file actually lives at `cmd/agent_modes.go`; decomposed 732 → 613 lines plus 1 concern file (SetupAgentEvents + currentReasoningFold); commit TBD)_
- [x] **SP-075-4j:** `pkg/console/input_core.go` 715 lines — extract input parsing, history management, completion. _(history + completion were already in their own files (`input_history.go`, `input_completion.go`); extracted the terminal-mode setup/teardown tangle from `ReadLine` into `setupInputTerm`/`teardownInputTerm` in `input_terminal.go` so the SGR sequence calls are no longer interleaved with prompt/line-state init. input_core.go 715→700. 3 new tests + 100+ existing console tests all pass. Commit `d8222ac`.)_
- [x] **SP-075-4k:** `pkg/agent_providers/generic_provider.go` 669 lines — finish the extraction started in Phase 3. _(extracted 11 HTTP error formatting helpers + 1 const into a new `generic_provider_http_errors.go` (172 lines); original file now 521 lines; commit TBD)_

---

## Vision Pipeline Improvements

The vision pipeline (`pkg/agent_tools/vision_*.go`) was identified
for improvement but never scoped into concrete items. The runner
should pick them up here. Analyze the actual code before
implementing; do not blindly follow these as TODOs.

- [x] **VISION-1:** `vision_types.go` is a god-type holding all
      multimodal-related structs. Split per-domain into
      `vision_pdf_types.go`, `vision_image_types.go`,
      `vision_analyze_types.go`. _(vision_types.go 201 → 12 lines; types split into 3 per-domain files; commit `be5536d0`)_
- [x] **VISION-2:** Extract a vision prompt builder from
      `vision_analyze.go` (inline template strings). Centralize in
      `vision_prompt.go` so prompt iteration doesn't require touching
      the call sites. _(vision_prompts.go already has GeneratePromptForMode + CreateVisionPrompt; no further extraction needed; verified by audit in commit `e2cfe2c1`)_
- [x] **VISION-3:** Add a vision-specific concurrency cap (currently
      bound to the generic `request_parallelism` setting). Vision
      requests are heavyweight; separate cap configurable in
      `config_domain.go::VisionConfig`. _(new VisionConfig in config_domain.go with parallel_workers + max_concurrent_global + batch_size; resolved via Config.Vision; commit `bf62d821`)_
- [x] **VISION-4:** Add a multimodal batching layer for
      `ProcessImagesInText` — when a user message contains N images
      and N > 1, batch into one provider call instead of N serial
      calls. Cache by content hash. _(new vision_batch.go with AnalyzeImagesBatched; content-hash cache; partial-failure fallback to single-image; commit `bc788380`)_
- [x] **VISION-5:** Add structured vision metrics (success/failure by
      reason, retry count, OCR fallback rate, latency by phase) to
      `pkg/agent/semantic_recall_instrumentation.go` pattern. _(new VisionMetrics struct with atomic counters for all phases; hookable sink in vision_metrics_sink.go; commit `e2cfe2c1`)_
- [x] **VISION-6:** Add `vision_retry_test.go` regression cases for
      every documented SP-103-A failure mode (typed errors, retry
      with backoff, concurrent cap). _(vision_retry_test.go covers RetryableHTTPError, IsRetryable, ParseRetryAfter, RetryAfter honoring 429/date/cap; new vision_batch_test.go covers multimodal batching failure modes; commits `e2cfe2c1` and `bc788380`)_
- [x] **VISION-7:** Audit the recently-added
      `vision_retry.go` wrapper — verify it doesn't double-wrap, that
      `processOCRImages` sequential path is preserved, and that
      cancellation context propagates through to all provider calls. _(audit complete: no double-wrap, sequential path preserved, ctx.Done() checked before each retry sleep and threaded into worker goroutines; commit `e2cfe2c1`)_
- [x] **VISION-8:** Decide and document: does `vision_types.go` split
      break type imports? Map the import graph before extracting. _(decided YES safe to split — types only depend on other vision_types and a small set of pkg/agent_api + pkg/configuration imports; verified during VISION-1 commit; commit `be5536d0`)_

---

## WebUI-A: Fix Currently-Failing Vitest Tests

_Runner-friendly batch (~30 min)._ The webui test suite is at
5449 passing / 9 failing / 36 skipped (5,496 total). The 9 failures
all look like they stem from components that landed without their
testid/assertion updates — typical of fast ship-cycles.

### Failing test files (verified 2026-07-02)

**`test/webui/testids.test.ts`** (2 failures):
- "every observed static testid must be registered" — 10 new testids
  in `ShellApprovalPanel.tsx` and `NotificationCenter.tsx` weren't
  added to `test/webui/testids.ts`:
  ```
  Submit
  Submitting…
  notification-center
  notification-center-mark-all-read
  shell-approval-accept-all
  shell-approval-command
  shell-approval-reject-all
  shell-approval-reset
  shell-approval-risk-badge
  shell-approval-submit
  ```
- "every observed template literal pattern must have at least one
  matching registry value" — 2 patterns in `ShellApprovalPanel.tsx`:
  ```
  shell-approval-part-${part.id}
  shell-approval-part-toggle-${part.id}
  ```

**`webui/src/components/Sidebar.sessionSearch.test.tsx`** (3 failures):
- "renders search results with data-session-id on each item" — expected
  2 results, got 0 (search filter or fetch failure).
- "clicking a result calls onSessionSearchRestore with the session_id" —
  result element is null (cascading from above).
- "shows no-results message when API returns empty results" — null
  again (same root cause).

**`webui/src/components/chat/ChatStatusBarItems.test.tsx`** (3 failures):
- "renders model, ctx, and cost when all fields are present" — expected
  3, got 2 (one segment missing — likely `ctx` or `cost`).
- "omits segments for missing fields, with no orphan separators" —
  expected 2, got 1.
- "renders the model as a button when onModelClick is provided" — text
  was "claude-haiku-4-5" but `Received: undefined`.

**`webui/src/hooks/useEventHandler.test.ts`** (1 failure):
- "auto-classifies [FAIL] messages as error" — classifier not matching
  the expected token.

### Items

- [x] **WebUI-A-1:** Fix `testids.test.ts` — add the 10 static testids
  + 2 template-literal patterns to `test/webui/testids.ts`. _(shipped: commit `49744cf5`. Also fixed an underlying bug in `testids.test.ts`: the ternary regex `[^?]*` was greedy across newlines and falsely matched `Submit` and `Submitting…` (button text) as testids. Replaced with `[^\n?]*`. 8 shell-approval + 2 notification entries added to TESTIDS.)_
- [x] **WebUI-A-2:** Fix `Sidebar.sessionSearch.test.tsx` — the
  shared root cause is likely a missing API mock or a query-name
  change. _(shipped: commit `3b125b73`. Root cause: the test queried for `[data-testid="sidebar-session-search-result"]` and `[data-testid="sidebar-session-search-no-results"]`, but `Sidebar.tsx` actually renders these as `[data-testid="chat-item"]` and `[data-testid="chat-sessions-empty"]` respectively (the canonical testids already in `test/webui/testids.ts`). Updated 3 selectors in the test file to match the component. No component changes needed.)_
- [x] **WebUI-A-3:** Fix `ChatStatusBarItems.test.tsx` — the
  missing segment and undefined text suggest the component was refactored
  (likely the SP-101 work). Re-align the component output with what
  the test expects, or update the test if the new behavior is
  intentional. _(shipped: commit `219e6c83`. SP-101 intentionally gated the model segment on `isConnected === false` OR non-orchestrator persona (because the shared StatusBar middle section already shows provider+model when connected). Updated 4 tests to match the new intentional behavior, added 2 new tests covering the disconnected + persona-active code paths. No component changes needed.)_
- [x] **WebUI-A-4:** Fix `useEventHandler.test.ts::auto-classifies
  [FAIL] messages as error` — _(verified pass on 2026-07-03: the
  test "auto-classifies [FAIL] messages as error" and its siblings
  ("auto-classifies [WARN] messages as warning", "auto-classifies
  [OK] messages as info_rendered") all pass under
  `cd webui && npx vitest run src/hooks/useEventHandler.test.ts`.
  The classifier at `webui/src/hooks/useEventHandler.ts:530-545`
  still uses a regex literal (`/^\[FAIL\]|\[!!\]/.test(cleanedMsg)`)
  not `strings.Contains`. No code change needed; reviewer-flagged
  SHOULD_FIX (anchor the alternation so `[!!]` etc. only match at
  the start of the string) tracked separately.)_

### Notes

- These 4 fixes are independent and can be shipped in any order.
- After the fix batch: `cd webui && npx vitest --run` should report
  5458 passing / 0 failing / 36 skipped (5,496 total).
- **Do not** touch any of the 27 Playwright spec failures
  (`test/webui/*.spec.ts`) — they require a running daemon, not a
  unit test fix. They're tracked separately in the e2e harness.

---

## WebUI-B: Fix `pkg/webui` Go Test Failure

_Bug fix (~10 min)._ `go test ./pkg/webui/` fails on
`TestPartialSettingsAppliers_ComprehensiveEnums` because the negative
"unknown key" set is missing a real key. Cheap fix.

### Failing assertion

```
settings_api_partial_settings_test.go:88:
  expected exactly [definitely_not_a_real_key] in unknown,
  got [definitely_not_a_real_key self_review_gate_mode]
```

`self_review_gate_mode` is a real config key (set in line 33 of the
same test file as a known key) — but it was added to the "known"
set without being added to the "unknown" exclusion list. The test
fails because the unknown-keys list now contains a real key.

### Items

- [x] **WebUI-B-1:** Add `self_review_gate_mode` (and any other
  known keys added in recent config-domain refactors) to the
  "known keys" whitelist in
  `pkg/webui/settings_api_partial_settings.go` (`applyRiskAndSafetySettings`).
  Added as an accept-and-ignore handler so the GET→PUT round-trip
  succeeds. The runtime gate is currently policy-driven
  (not config-driven); once `cfg.SelfReviewGateMode` lands, swap for a
  real read/write. Verified: `go test ./pkg/webui/ -run TestPartialSettingsAppliers_ComprehensiveEnums` passes.

### Notes

- This is a 1-line fix. The runner should:
  1. Read `settings_api_partial_settings.go` to find the
     `_ = PartialSettingsKeys` style allowlist.
  2. Compare to the test's "known" + "unknown" sets.
  3. Update the test's allowlist to include any recently-added
     keys, or vice versa.
- After the fix, the entire `pkg/webui` test suite must pass.

---

## Refactor-A: God-Function Split Candidates

_File decomposition batch (~2-3 hours total, run as 1 item per
function)._ The 2026-07-02 god-function scan surfaced several
functions over 300 lines that haven't been touched by recent
refactors. Runner can grind them one at a time using the
SP-075-4j/4k pattern (existing tests stay green, extract into
focused files).

### Candidates (with line count)

| Function | LOC | File | Comment |
|---|---:|---|---|
| `RunAgent` | 599 | `cmd/agent_modes.go` | Runner already touched this in past sprint; recheck |
| `(r *SubagentRunner).runTask` | 531 | `pkg/agent/subagent_runner.go` | Splittable into `runTaskSetup` / `runTaskBody` / `runTaskTeardown` |
| `newDefaultToolRegistry` | 517 | `pkg/agent/tool_registry.go` | Pure data assembly — could be table-driven |
| `runBackendSet` | 443 | `cmd/model_registry_server/*.go` | Extract subcommands into helpers |
| `(a *Agent).processQueryWithSeed` | 419 | `pkg/agent/conversation.go` | Extract prompt-prep / model-call / response-handle |
| `startTerminalToolSubscriber` | 402 | `cmd/agent_modes.go` | Pure event-wiring; should split |
| `runInteractiveMode` | 370 | `cmd/agent_modes.go` | Steer / render / error paths separable |
| `(a *Agent).RequestApproval` | 344 | `pkg/agent/approval_broker.go` | Split by risk class |
| `(*ToolRegistry).ExecuteTool` | 341 | `pkg/agent/tool_registry.go` | Pre-flight / execute / post-process |
| `handleRunParallelSubagents` | 329 | `pkg/agent/tool_handlers_subagent.go` | Dispatch + per-task spawn + collect |
| `runSeamlessPlanning` | 319 | `cmd/agent_modes.go` | Plan / execute / commit phases |

### Items

- [x] **Refactor-A-1:** Split `pkg/agent/subagent_runner.go::runTask`
  into 3 files: `runTaskSetup` (workspace, worktree, persona), 
  `runTaskBody` (the model call loop), `runTaskTeardown` (cleanup,
  metrics, results). Existing tests stay green; new file names
  follow the `_lifecycle.go` / `_helpers.go` pattern from prior
  SP-075 work.
  _(shipped: 5cfbfad2 — runTask reduced from 423→76 lines via
  setupSubagentRun + finalizeSubagentResult helpers; all tests pass)_
- [x] **Refactor-A-2:** Split `pkg/agent/conversation.go::processQueryWithSeed`
  into prompt-prep / model-call / response-handle files (similar
  to the SP-098-1 split for `mcp.go`). The function is 419 lines
  with 3 clear phases; keep `processQueryWithSeed` as a thin
  orchestrator that calls the 3 extracted helpers.
  _(shipped: 2216f1f1 — processQueryWithSeed reduced from 417→13
  lines via prepareQueryRun + handleQueryResult; all tests pass)_
- [x] **Refactor-A-3:** Split `cmd/agent_modes.go::startTerminalToolSubscriber`
  (402 lines) into event-subscribe / event-render / cleanup files.
  Pure wiring code, low risk.
  _(shipped: 4fe532ce — startTerminalToolSubscriber reduced from
  402→12 lines via extracted handler methods + runEventLoop;
  all tests pass)_
- [x] **Refactor-A-4:** Split `pkg/agent/tool_handlers_subagent.go::handleRunParallelSubagents`
  (329 lines) into dispatch / spawn / collect. Follows the existing
  subagent-spawn-lifecycle.go pattern.
  _(shipped: 3002e0ab — handleRunParallelSubagents reduced from
  311→67 lines via 10 focused helpers; all tests pass)_

### Notes

- Each item is a 30-60 min refactor. Use the SP-075 pattern:
  build → run existing tests → decompose → re-build → re-test →
  re-review. Don't refactor on green until you have a baseline
  test count.
- After all 4 items, `cmd/agent_modes.go` and `pkg/agent/conversation.go`
  should drop below 500 lines (currently 600+ and 400+).

---

## WebUI-C: Hex Color Token Audit

_Token migration batch (~1 hour)._ The webui component scan found
~30 hardcoded hex colors that should use design tokens. Per the
design system rules in `AGENTS.md`, no raw hex in CSS or inline
`style={{}}` is allowed.

### Sites found (top offenders)

```
10 #6e7681    (status: gray-500-ish — diff dim text?)
 9 #22c55e    (status: green-500 — success?)
 6 #d2a8ff    (status: purple-300 — terminal keyword?)
 6 #58a6ff    (status: blue-400 — terminal keyword?)
 4 #f59e0b    (status: amber-500 — warning)
 4 #ef4444    (status: red-500 — error)
 3 #ff7b72    (terminal keyword red)
 3 #f0883e    (terminal keyword orange)
 3 #7ee787    (terminal keyword green)
 3 #79c0ff    (terminal keyword blue)
```

Most of these are GitHub-syntax-theme colors used in the editor's
syntax highlighting. The first 4 (#6e7681, #22c55e, #f59e0b,
#ef4444) are status colors that have direct token equivalents
(`--text-muted`, `--accent-success`, `--accent-warning`,
`--accent-error`).

Also: `color: '#fff'` is hardcoded 4 times in
`platform/BillingPage.tsx` (lines 243, 273, 305, 335) — should
use `var(--accent-fg)`.

### Items

- [x] **WebUI-C-1:** Replace `#6e7681` → `var(--text-muted)` in
  webui components (10 sites). _Done — replaced 12 `var(--text-tertiary,
  #6e7681)` and `var(--text-muted, #888)` fallback sites across 9 CSS
  files (Chat.css, GoToSymbolOverlay.css, GoToWorkspaceSymbolOverlay.css,
  MediaViewer.css, Notification.css, NotificationCenter.css, StatusBar.css,
  UpdateNotification.css, chat/{ChatStatusBarItems,SubagentActivityFeed,
  SubagentTree,ToolTimelineBar}.css). JS runtime-color-picker hex
  strings tested by helpers.test.tsx / SubagentActivityFeed.test.tsx
  are deliberately preserved (they validate `getPersonaColor()` output,
  not CSS tokens)._
- [x] **WebUI-C-2:** Replace `#22c55e` → `var(--accent-success)`
  (9 sites). _Done — replaced 11 sites across Notification.css,
  ProrationDisplay.tsx, CredentialsSettingsTab.tsx, MCPCredentialPanel.tsx
  (had stale `--color-success` token name with hex fallback — renamed to
  the defined `--accent-success`). The single remaining `#22c55e` site
  is in editorTabIcons.tsx line 133 for the `.diff` extension —
  classified as a brand-color file-type identifier (exempt per design
  system rules in AGENTS.md) and preserved._
- [x] **WebUI-C-3:** Replace `#f59e0b` → `var(--accent-warning)`
  and `#ef4444` → `var(--accent-error)` (4 sites each).
  _Done — replaced 4 `#f59e0b` sites and 5 `#ef4444` sites across
  Notification.css, ProrationDisplay.tsx, CredentialsSettingsTab.tsx,
  MCPCredentialPanel.tsx. Note: CredentialsSettingsTab had stale
  `--color-warning`/`--color-error` token names with hex fallback —
  renamed to the defined `--accent-warning`/`--accent-error`.
  Remaining `#f59e0b`/`#ef4444` in editorTabIcons.tsx (file-extension
  brand colors) are exempt._
- [x] **WebUI-C-4:** Replace `color: '#fff'` → `var(--accent-fg)`
  in `platform/BillingPage.tsx` (4 sites). _Done — all 4 sites
  replaced._
- [x] **WebUI-C-5:** Leave the 6 github-syntax-theme colors
  (#d2a8ff, #58a6ff, etc.) alone — they're intentional brand
  identifiers for syntax highlighting and exempt per AGENTS.md.
  _Verified — only 6 hex constants remain in webui/src/ outside the
  exempt categories (JS test fixtures for runtime color picker, plus
  file-extension brand colors in editorTabIcons.tsx). GitHub syntax
  theme colors are zero-modification as specified._

### Notes

- Run the design system grep guard at the end to verify zero raw
  hex leaks in non-syntax files.
- The CI verification snippet from AGENTS.md:
  ```
  git diff origin/main -- 'webui/src/**/*.css' 'packages/ui/src/components/*.css' \
    | grep -E '^\+.*(#[0-9a-fA-F]{3,6}|rgba\([0-9])' \
    | grep -vE 'rgba\(0, 0, 0|var\(--'
  ```

---

## SP-104: Vitest worker pool hardening

_Vitest defaulted to forking one worker per CPU core (24 on this host).
Each jsdom worker holds 1–4 GB RSS; the full 48-file suite consumed ~52 GB
and triggered kernel OOM. The worker pool is now capped to 4._

### Items

- [x] **SP-104-1:** Cap vitest worker pool in
      `packages/ui/vitest.config.ts` and `webui/vite.config.ts`.
      packages/ui uses Vitest 4 (top-level `maxWorkers: 4`, `pool: 'forks'`);
      webui uses Vitest 2 (`poolOptions.forks.maxForks: 4`, `minForks: 1`).
      Both honor `VITEST_MAX_WORKERS` / `VITEST_MAX_FORKS` env overrides.
      Verified: full 48-file packages/ui suite runs in 5.5s with ~1 GB
      delta. _(shipped: vitest config changes + worker cap verification)_
- [x] **SP-104-2:** Update Sprout WebUI QA workflow (the WebUI-A-*
      automation chain in `~/.config/sprout/task_queue.json`) so each
      vitest invocation passes an explicit test file glob:
      `vitest run src/path/to/Specific.test.tsx`. Forbid bare
      `npm exec vitest` from the package root.
      _(shipped: 99c4577d — vitest-safe.sh wrapper + 28 tests + workflow_prompt.md rules)_
- [x] **SP-104-3:** Add a memory-aware gate to the QA subagent shell
      scope: pre-check `MemAvailable` from `/proc/meminfo`, refuse to
      launch if available < 8 GB, sleep + retry if 8–16 GB. This belongs
      in the subagent persona's tool wrapper, not in vitest itself.
      _(shipped: pending commit — memory_gate.go with 3-tier check,
      Linux/macOS support, 48+ subtests)_
- [x] **SP-104-4/5:** Systemd memory guardrails written to
      `scripts/user.slice-memory-cap.conf` (MemoryMax=48G on user.slice).
      Requires manual installation with sudo — see file header for
      instructions. Defense-in-depth; the vitest worker cap (SP-104-1)
      is the primary fix. _(shipped: config file + install instructions;
      user applies with `sudo cp` + `systemctl daemon-reload`)_
- [x] **SP-104-6:** Diagnostic helper at `scripts/diagnose-oom.sh` —
      scans journald, /var/log/syslog, /var/log/kern.log, and dmesg for
      OOM-killer traces. Supports `--boot N`, `--since`, and `--json`.
- [x] **SP-104-7:** Add a Prometheus-style probe to the Sprout daemon
      that watches `node_count > 500 OR total_user_rss > 50G` and
      triggers a notification before the OOM-killer fires.
      _(shipped: pending commit — OOMWatchdog with /proc scanning,
      event bus integration, cooldown state machine, 13 tests)_

### Notes

- If `SP-104-1` reveals that many tests genuinely need `jsdom` (e.g.
  heavy Radix/Reach-UI work), consider splitting the suite into
  `*.unit.test.tsx` (happy-dom / node) and `*.dom.test.tsx` (jsdom, run
  serially) so the lightweight majority can parallelize and the heavy
  minority gets memory headroom.

---

## SP-109: Single-Source Tool Definitions — Eliminate Dual Maintenance

Every tool is defined twice: `ToolConfig` in `pkg/agent/tool_registrations.go`
(LLM-facing) and `ToolHandler.Definition()` in `pkg/agent_tools/` (dual-dispatch).
These drift. 10 handler-only tools (`embedding_index`, `semantic_search`,
`list_directory`) are invisible to the LLM. 16 legacy-only tools have no
handler dispatch. Full spec: `roadmap/SP-109-single-source-tool-definitions.md`.

### Phases

- [x] **SP-109-1:** Extend `ToolHandler` interface with metadata methods
      (`Aliases`, `Timeout`, `MaxResultSize`, `SafeForParallel`, `Interactive`).
      Add `ToolEnv.Agent` for subagent-spawning tools. Add default no-op stubs
      to all 32 existing handlers. _(~1 day)_
      _(shipped: 82aa5229 — 5 metadata methods added to ToolHandler,
      37 handlers with no-op stubs; all tests pass)_

- [x] **SP-109-2:** Build canonical tool list from handlers. Add
      `BuildToolDefinitions()` that iterates `GetNewToolRegistry().All()`.
      Add `convertHandlerToSeedToolConfig()` for the seed path. Run old+new
      in parallel, assert identical output, then switch over. _(~1 day)_
      _(shipped: pending commit — tool_definitions_handler.go with
      BuildToolConfigsFromHandlers, convertHandlerToSeedToolConfig,
      parallel verification, env-gated switch)_

- [x] **SP-109-3:** Migrate 16 legacy-only tools to handlers. Batch A (simple
      CRUD): `manage_memory`, `manage_settings`, `mcp_refresh`, `task_queue`,
      `list_changes`, `revert_my_changes`, `recover_file`, `create_pull_request`,
      `list_automate_workflows`, `run_automate`. Batch B (needs `*Agent`):
      `run_subagent`, `run_parallel_subagents`. Batch C (clarification):
      `request_clarification`, `respond_clarification`. Fix `TodoRead`/`todo_read`
      case mismatch. Remove dead individual tools (`save_memory`, `search_memories`,
      `task_queue_*`). _(~2 days)_

- [x] **SP-109-4:** Delete legacy `ToolConfig` registry. Remove all
      `ToolConfig` registration calls. Delete `ToolRegistry`, `ToolConfig`,
      `ParameterConfig`, `ToolHandler` func types. Single source of truth
      achieved. _(~0.5 days)_

### Side-effect fixes
- `embedding_index`, `semantic_search`, `list_directory` become LLM-visible
- `TodoRead`/`todo_read` case mismatch resolved
- Dead individual memory/task-queue tools properly removed

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

- [x] **SP-111-1:** Gate call cost tracking. `GenerateResponse` uses the
      agent's client but bypasses `accumulateResponseCost`, so gate call
      costs (~$0.002/call) don't hit `fleetUsdBudget`. Either wire
      `GenerateResponse` through the seed provider cost path, or track
      gate costs separately in the loop runner.
- [x] **SP-111-2:** Integration test for `runAgentWorkflowLoop`. Use a
      `ScriptedClient` to simulate gate responses + ProcessQuery behavior.
      Cover: full success path, max-iterations incomplete, triage skip,
      build failure → retry → success.
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

## Slash Command Audit (2026-07-04)

_Audit of all 36 registered slash commands in `pkg/agent_commands/`.
All 12 findings implemented and shipped._

### Functional issues

- [x] **SC-1:** WebUI slash command output is now captured via stdout
      pipe redirection (`pkg/webui/api_query.go`) and forwarded as
      `EventTypeStreamChunk` events. Mutex-protected against concurrent
      multi-chat races on `os.Stdout`.
- [x] **SC-2:** `/compact` now implements `SetContext` and uses
      `chatAgent.InterruptCtx()` for LLM summarization calls. Cancelable
      via Stop/Ctrl+C. (Commit/search don't make direct context-accepting
      LLM calls — their LLM calls go through internal timeout-based
      generators, so SetContext wouldn't help without deeper refactoring.)
- [x] **SC-3:** `/exit` now checks `SPROUT_DAEMON` env var and returns
      an error instead of calling `os.Exit(0)` in daemon mode.

### Design / UX issues

- [x] **SC-4:** Description() strings differentiated: `/info` = "Quick
      overview of live agent state", `/status` = "Detailed runtime
      status", `/setup` = "Show persisted configuration". Usage() text
      cross-references siblings.
- [x] **SC-5:** `/help` KEY COMMANDS section updated: removed deprecated
      `/subagent-provider` and `/subagent-model` highlights; added
      `/model`, `/provider`, `/search`, `/review`, `/info`.
- [x] **SC-6:** `/exec` raw ANSI (`\033[34m`) replaced with
      `console.GlyphShell.Fprintf(os.Stdout, ...)`.
- [x] **SC-7:** Added 7 new aliases: `cl`→clear, `cp`→compact,
      `st`→status, `rb`→rollback, `rw`→rewind, `ch`→changes, `cg`→codegraph.

### Code quality / cleanup

- [x] **SC-8:** Deleted `IsShellCommand` (100+ line dead prefix list) and
      `ExecuteShellCommandDirectly` from `exec.go`. Deleted associated
      test files (`exec_test.go`, `command_selector_test.go`).
- [x] **SC-9:** Deleted empty `commit.go` wrapper file.
- [x] **SC-10:** Added `Usage()` methods to all 26 command types that
      lacked them. `/help <command>` now shows real usage details for
      every command.
- [x] **SC-11:** Added `--json` output support to `/info`, `/status`,
      `/usage`, `/models`, `/providers` via `ExecuteWithJSONOutput`.
- [x] **SC-12:** Deleted dead `SelectAndExecuteCommand`,
      `ShowCommandSelector`, and `CommandItem` from `command_selector.go`.

## CLI Output UX Improvements (CLI-UX audit, 2026-07-05)

Audited the CLI output layer against best-in-class tools (Claude Code,
Gemini CLI, Aider, Cursor, gh, dagger). Findings prioritized by impact.

### Tier 1 — High impact, directly visible

- [x] **CLI-UX-1:** Implement `verbose` mode (defined but does nothing).
      Only `compact` was branched on. Verbose now adds: extended tool arg
      previews (200 chars vs 70), dim result-size suffix (· 1.2KB) on
      tool-end lines. `isVerbose()` reads live from config.
- [ ] **CLI-UX-2:** Live elapsed-time on spinner for long-running tools.
      ALREADY IMPLEMENTED — spinner render() already shows elapsed seconds.
      Audit was wrong on this one.
- [ ] **CLI-UX-3:** Diff preview for write_file/edit_file in timeline.
      Currently shows `✓ edit_file (path.go) · 0.1s` with no indication of
      what changed. Even compact `+12 -3` stat would add signal. Claude
      Code shows 2-3 line hunk inline; Aider shows full git-style diff.
- [ ] **CLI-UX-4:** Cumulative turn-progress indicator during long turns.
      During 5+ tool-call turns, no sense of "where am I." Surface todo
      `3/7 done` on footer or as dim line after each tool. Data already
      published via EventTypeTodoUpdate.

### Tier 2 — Polish that differentiates

- [x] **CLI-UX-5:** Contextual thinking indicator during LLM think time.
      On `query_started`, starts `◐ thinking…` spinner when no tool is
      active. Cleared when prose streams, a tool starts, or turn completes.
- [ ] **CLI-UX-6:** Session-vs-turn cost split in footer. composeLine shows
      cumulative cost only. Show `$0.04 turn · $1.21 session` for pacing
      signal. Key feature in Claude Code and Cursor.
- [x] **CLI-UX-7:** Compact turn summary at turn end. On `query_completed`,
      prints dim `✓ turn complete · 12.3s · $0.04` summary line. Suppressed
      in compact mode.
- [ ] **CLI-UX-8:** Pre-highlight code blocks during streaming. Code blocks
      flicker plain→colored when closing ``` arrives. StreamingMarkdownFormatter
      should highlight per-line or show dim affordance until fence close.

### Tier 3 — Nice to have

- [ ] **CLI-UX-9:** Box-drawing for structured panels (todos, errors, tables).
      gh/lazygit/dagger use `┌─┐│└┘` for scannable panels. Todo block is
      strongest candidate.
- [ ] **CLI-UX-10:** Keyboard shortcut affordances row. No visible hint that
      Ctrl+C interrupts, / opens steer. Dim toggleable help row above footer.
- [ ] **CLI-UX-11:** Subagent progress shows task description, not just persona.
      `→ coder: refactoring auth.go` instead of `→ coder`. subagentProgress
      captures persona+elapsed only.
- [ ] **CLI-UX-12:** Expand-on-demand for truncated tool args. Long args
      truncate to 70-80 chars. Power-user keybind (e.g. `v` on tool row) or
      verbose mode to show full args.

### Recommended starting points
CLI-UX-2 (live elapsed) and CLI-UX-1 (verbose mode) — highest impact-per-effort,
both build on existing infrastructure.

## CLI Ghost Line Bug (found 2026-07-05)

### CLI-GHOST-1: Remove self-published ToolStart/ToolEnd from 34 handlers
Same bug class as 63a43421 (ask_user spinner). 34 ToolHandler implementations
self-publish EventTypeToolStart/EventTypeToolEnd with "tool" key (not
"tool_name") and missing fields. The core tool executor already publishes
correct events. The self-publish produces phantom ✗ · 0.0s ghost lines in
the CLI timeline.

Affected handlers: activate_skill, analyze_image_content, analyze_ui_screenshot,
browse_url, commit, create_pull_request, edit, embedding_index, git,
list_automate_workflows, list_changes, list_dir, list_skills, manage_memory,
manage_settings, mcp_refresh, patch_structured_file, read_file, recover_file,
repo_map, respond_clarification, revert_my_changes, rollback_changes,
run_automate, run_parallel_subagents, run_subagent, save_memory, search_files,
search_memories, semantic_search, shell, task_queue(_add/_publish/_read),
todo_read, todo_write, view_history, web_search, write, write_structured.

### CLI-GHOST-2: Defensive guard in terminal subscriber
Even after removing the self-publish, add a defensive guard: skip rendering
ToolEnd events with empty tool_name in handleToolStartEvent and
handleToolEndEvent. Prevents future phantom lines from any source.
