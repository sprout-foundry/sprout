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

- [ ] **SP-091-7: SP-075 split `pkg/agent/tool_handlers_subagent_spawn.go`
  (1208 lines).** Largest single file in `pkg/agent/`. Per-spec method: extract
  spawn/build/run into separate files under a new
  `pkg/agent/subagent_spawn/` package. No behavior change. Run full test
  suite between each extraction.

- [ ] **SP-091-8: SP-075 split webui components above 800 lines.**
  GitSidebarPanel (974), Sidebar (851), AppContent (843), Terminal (738),
  EditorTabs (731), AutomationsPanel (724), SettingsPanel (691). Pick
  GitSidebarPanel first (clearest seams: branches / worktrees / push / PR
  / status — five files). Spec: `roadmap/SP-075-large-file-decomposition.md`.

### Phase 5 — UX polish (~0.5 day)

- [ ] **SP-091-9: SP-012 reduced-motion support.** Wrap every `@keyframes`
  animation in `webui/src/App.css` and the shared `packages/ui/.storybook/tokens.css`
  in a `@media (prefers-reduced-motion: no-preference)` block. Audited list
  in spec §"Current State" §"Missing accessibility." Spec:
  `roadmap/SP-012-ux-polish.md`.

- [ ] **SP-091-10: SP-017 collapse thin settings tabs.** Security (2 fields),
  Performance (5), OCR (3), Commit & Review (4) each get a full tab in
  `webui/src/components/settings/`. Merge into a single "Advanced" tab with
  collapsible sections; remove 3 tab entries from `SettingsPanel.tsx`.

### Phase 6 — Spec retirement

- [x] **SP-091-11:** Delete `roadmap/SP-006-delegate-tool.md` and
  `roadmap/SP-066-structured-file-key-order.md` — both are superseded (the
  spec files themselves note this; only the README carried stale entries).
  Their content is preserved in git history.

---

## Things to consider next (not on this list yet)

These are real ideas but not yet formalized into a spec. Open one (SP-092+)
when ready to start:

- **Persistent conversation recall** — `/recall <phrase>` to surface past
  session turns via the embedded summary store (already built per SP-066
  embedding store; just needs a CLI surface).
- **Edit approval for shell commands** — SP-072 covers file edits; no
  equivalent for `git push`, `rm -rf`, etc. Pattern: same broker, different
  payload type.
- **Test the cross-session recall UX in real workflows** — the embedded
  recall exists but no validation that the surfaced summaries actually help.
- **Audit all `pkg/agent` `fmt.Errorf` for the typed-error migration** — SP-091-5
  is a starter, but the full migration touches ~366 sites.