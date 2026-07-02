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


- [ ] **SP-095-2:** Wait 2 weeks of normal usage. (Manual calendar step,
  not a code step.)


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
- [ ] **SP-075-4g:** `pkg/agent_tools/tool_handlers_subagent_spawn.go` 999 lines — extract spawn lifecycle, worktree setup, cleanup hooks.
- [x] **SP-075-4h:** `webui/src/components/Terminal.tsx` 780 lines — extract xterm setup, event handlers, keybindings (continue Phase 3 work). _(extracted three reusable hooks into `webui/src/hooks/usePersistedPref.ts`: `usePersistedNumber` (terminalHeight + fontSize), `usePersistedBoolean` (copyOnSelect), `useOutsideClickDismiss` (two menu-dismiss effects). Terminal.tsx 780→718 lines. TypeScript clean, 13 new hook tests passing, existing tests still green. Commit `701161b`.)_
- [ ] **SP-075-4i:** `pkg/agent/agent_modes.go` 732 lines — extract mode registry, mode config validation, command dispatch.
- [x] **SP-075-4j:** `pkg/console/input_core.go` 715 lines — extract input parsing, history management, completion. _(history + completion were already in their own files (`input_history.go`, `input_completion.go`); extracted the terminal-mode setup/teardown tangle from `ReadLine` into `setupInputTerm`/`teardownInputTerm` in `input_terminal.go` so the SGR sequence calls are no longer interleaved with prompt/line-state init. input_core.go 715→700. 3 new tests + 100+ existing console tests all pass. Commit `d8222ac`.)_
- [ ] **SP-075-4k:** `pkg/agent_providers/generic_provider.go` 669 lines — finish the extraction started in Phase 3.

---

## Vision Pipeline Improvements

The vision pipeline (`pkg/agent_tools/vision_*.go`) was identified
for improvement but never scoped into concrete items. The runner
should pick them up here. Analyze the actual code before
implementing; do not blindly follow these as TODOs.

- [ ] **VISION-1:** `vision_types.go` is a god-type holding all
      multimodal-related structs. Split per-domain into
      `vision_pdf_types.go`, `vision_image_types.go`,
      `vision_analyze_types.go`.
- [ ] **VISION-2:** Extract a vision prompt builder from
      `vision_analyze.go` (inline template strings). Centralize in
      `vision_prompt.go` so prompt iteration doesn't require touching
      the call sites.
- [ ] **VISION-3:** Add a vision-specific concurrency cap (currently
      bound to the generic `request_parallelism` setting). Vision
      requests are heavyweight; separate cap configurable in
      `config_domain.go::VisionConfig`.
- [ ] **VISION-4:** Add a multimodal batching layer for
      `ProcessImagesInText` — when a user message contains N images
      and N > 1, batch into one provider call instead of N serial
      calls. Cache by content hash.
- [ ] **VISION-5:** Add structured vision metrics (success/failure by
      reason, retry count, OCR fallback rate, latency by phase) to
      `pkg/agent/semantic_recall_instrumentation.go` pattern.
- [ ] **VISION-6:** Add `vision_retry_test.go` regression cases for
      every documented SP-103-A failure mode (typed errors, retry
      with backoff, concurrent cap).
- [ ] **VISION-7:** Audit the recently-added
      `vision_retry.go` wrapper — verify it doesn't double-wrap, that
      `processOCRImages` sequential path is preserved, and that
      cancellation context propagates through to all provider calls.
- [ ] **VISION-8:** Decide and document: does `vision_types.go` split
      break type imports? Map the import graph before extracting.


