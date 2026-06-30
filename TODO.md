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

- [ ] **SP-092-1:** Extract `Agent.Recall(ctx, query, limit) ([]RecalledItem, error)`
  from `InjectSemanticRecall`. `InjectSemanticRecall` becomes a thin wrapper.
  Existing turn-level semantic-recall tests in `semantic_recall_test.go`
  must continue to pass. _Effort: ~0.5 day. No new CLI/webui surface yet._

- [ ] **SP-092-2:** `/recall <text>` CLI command. New
  `pkg/agent_commands/recall_command.go`, registered in `commands.go`. Uses
  `output_writer.go` for printable output; `--json` flag emits the raw
  `[]RecalledItem` for scripting. Tests in `recall_command_test.go` cover
  empty query, zero results, hits, and the `--limit` / `--json` flags.
  _Effort: ~1 day. No webui changes._

- [ ] **SP-092-3:** WebUI `/api/recall` endpoint +
  `PastSessionsHint` sidebar component. New `pkg/webui/recall_api.go`,
  new `webui/src/components/PastSessionsHint.tsx` + `.css`. Mounted in
  `Sidebar.tsx`. Click-to-restore uses the existing
  `handleSessionRestore` from the `sprout:session-restored` event. Add
  `past-sessions-hint` to the testid registry. Tests:
  `PastSessionsHint.test.tsx` covers debounce, empty state, zero results,
  and click-to-restore. _Effort: ~1 day._

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

- [ ] **SP-093-1:** `ShellProposal` + `SplitShellIntoParts` + 5 destructive
  classifiers. Pure functions, fully unit-tested in
  `shell_approval_test.go`. No agent wiring, no UI. _Effort: ~1 day._

- [ ] **SP-093-2:** `Agent.RequestShellApproval` + CLI 4-option picker per
  part (arrow-key picker that toggles parts). Existing 4-option prompt
  remains the default; opt-in via `configuration.EditApprovalConfig` with
  a new `shell_command: bool` flag (default `false` so no behavior change
  for existing users). _Effort: ~1 day._

- [ ] **SP-093-3:** `ShellApprovalRequestPayload` event + WebUI panel
  + handler. Wires into the existing WS pipeline. New
  `pkg/webui/shell_approval_api.go` (decision endpoint). Tests:
  `ShellApprovalPanel.test.tsx`, `shell_approval_event_test.go`.
  _Effort: ~1 day._

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

- [ ] **SP-094-1:** Full error tree in `pkg/errors/types.go`. Mirror to
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
  with CGO-only handlers stubbed (already partly done per SP-058 / SP-061).
- **Subagent webui panel** — there's an active conversation indicator but
  no per-subagent detail view; SP-051 shipped depth in CLI but not WebUI.
- **Multi-workspace sprout** daemon — feature requested twice in the past
  month.