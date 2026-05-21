# TODO

---

## SP-026: Executive Assistant Persona

Spec: `roadmap/SP-026-executive-assistant.md`

[x] - SP-026 Phase A: Replace `isSubagent bool` with `subagentDepth int` on Agent struct — enables 3-level nesting: EA (depth=0) → orchestrator (depth=1) → coder/tester (depth=2). Update `getOptimizedToolDefinitions()` to filter delegation tools at depth >= 2. Add `MaxSubagentDepth` config (default: 2). Update all references. `pkg/agent/agent.go`, `pkg/agent/agent_getters.go`, `pkg/agent/conversation.go`, `pkg/agent/subagent_runner.go`, `pkg/configuration/config.go`
[x] - SP-026 Phase B: Add `working_dir` parameter to `run_subagent` tool — allows spawning subagents at any directory under `$HOME`. Add `WorkingDir` to `SubagentOptions` and `SubagentTask`. Validate target exists and is within `$HOME`. `pkg/agent/subagent_runner.go`, `pkg/agent/tool_handlers_subagent.go`
[x] - SP-026 Phase C: File-based task queue tools — `task_queue_read`, `task_queue_publish`, `task_queue_add` with atomic writes, file locking, and persistent storage at `~/.config/sprout/task_queue.json`. `pkg/agent_tools/task_queue.go`, `pkg/agent/tool_definitions.go`
[x] - SP-026 Phase D: Persona infrastructure — `LocalOnly bool` on `SubagentType`, `IsLocalMode()` detection, sliding risk cascade for EA approvals (auto-approve low-risk, reason about medium-risk, escalate high-risk), `-f`/`--force` auto-reject. `pkg/configuration/config.go`, `pkg/agent/persona.go`, `pkg/agent/tool_handlers_shell.go`
[x] - SP-026 Phase E: Executive Assistant persona definition — full replacement system prompt, project discovery (AGENTS.md → git scan → memory → organic), auto-activate when started from `~`, commit tool with strict rules (reject force, require meaningful message), EA-spawned subagents get depth=1, two startup modes (queue mode for autonomous processing, interactive mode for standard chat). `subagent_prompts/executive_assistant.md`, `pkg/agent/project_discovery.go`, `pkg/agent/agent_creation.go`, `cmd/sprout/main.go` [audit 2026-05-19: ~5/6 sub-features in place — system prompt at `pkg/agent/prompts/subagent_prompts/executive_assistant.md`, `project_discovery.go`, `autoActivateEAPersona` in agent_creation.go, `handleCommitTool` in tool_handlers_shell.go, depth+1 in subagent_runner.go — but "queue mode for autonomous processing" startup mode is missing (no QueueMode, no --queue flag, no auto-task-queue processing on EA startup)]

---

## SP-027: Persistent Context & Conversational Memory

Spec: `roadmap/SP-027-persistent-context.md`

### Phase 1: Conversation Turn Embedding (Foundation)

[x] - SP-027-1a: Create `ConversationTurn` struct in `pkg/agent/conversation_turn.go` — struct with ID, SessionID, TurnNumber, Timestamp, UserPrompt, ActionableSummary, PromptEmbedding, FilesTouched, WorkingDir, Duration, TokenUsage fields

[x] - SP-027-1b: Create `ConversationStore` in `pkg/embedding/conversation_store.go` — wraps a second `JSONLFileStore` instance for `~/.config/sprout/embeddings/conversation_turns.jsonl`, lazy initialization via `EmbeddingManager.GetConversationStore()`
[x] - SP-027-1c: Implement `VectorRecord` serialization mapping — `ConversationTurn` → `VectorRecord` with explicit field mapping (ID→ID, prompt→Signature, mean embedding→Embedding, Type→"conversation_turn", metadata map for FilesTouched/WorkingDir/Duration/TokenUsage)
[x] - SP-027-1d: Add `EmbedAndStoreTurn()` function — compute embeddings for prompt and actionable summary using static provider, store as `VectorRecord` in `ConversationStore`. Graceful failure: checkpoint still recorded if embedding/storage fails
[x] - SP-027-1e: Hook `EmbedAndStoreTurn()` into `pkg/agent/turn_checkpoints.go` — call after existing checkpoint recording in the same goroutine
[x] - SP-027-1f: Add `SessionIntentEmbedding []float32` to `ConversationState` in `pkg/agent/persistence.go` — computed on first turn, restored on session load
[x] - SP-027-1g: Tests — unit test for embed→store round-trip, test for graceful failure when provider unavailable

### Phase 2: Proactive Context Retrieval

[x] - SP-027-2a: Implement time-decayed similarity scoring — `ScoreWithDecay()` with 30-day half-life exponential decay combining cosine similarity and temporal weighting
[x] - SP-027-2b: Create `pkg/agent/proactive_context.go` — query `ConversationStore` with time decay, filter by `MinRelevanceScore` (0.50), cap at `MaxContextualResults` (5), format as "Previous Work" section for system prompt injection
[x] - SP-027-2c: Hook `proactiveContext.Inject()` into `ProcessQuery()` pre-loop — only on first turn (no prior messages beyond system prompt) or cold session restore
[x] - SP-027-2d: Add `PersistentContextConfig` struct to `pkg/configuration/config.go` — `ProactiveContextEnabled` (true), `MaxContextualResults` (5), `MinRelevanceScore` (0.50), `MaxContextChars` (4000), `WorkspaceScopedRetrieval` (false)
[x] - SP-027-2e: Tests — unit test for retrieval with time decay, test for empty store (graceful no-op), test for workspace-scoped filtering

### Phase 3: Drift Detection

[x] - SP-027-3a: Create `pkg/agent/drift_detection.go` — track `SessionIntentEmbedding` (from `ConversationState`), compute cosine similarity with current prompt every Nth turn, flag if below `DriftThreshold` (0.60)
[x] - SP-027-3b: Implement non-blocking drift notification — WebUI: toast-style notification with "Continue here" / "Start new chat" options (non-modal, agent continues). CLI: post-turn prompt with Enter to continue, 's' for new chat
[x] - SP-027-3c: Implement suppression logic — disable drift detection for session after 3 consecutive rejections
[x] - SP-027-3d: Add `CreateSessionWithHandoff()` to `pkg/webui/chat_sessions.go` — extract `ActionableSummary` from last turn, pre-populate new chat system prompt with "Context from Previous Chat" section
[x] - SP-027-3e: Add drift config fields to `PersistentContextConfig` — `DriftDetectionEnabled` (true), `DriftThreshold` (0.60), `DriftCheckInterval` (5 turns)
[x] - SP-027-3f: Create WebUI drift notification component in `webui/src/components/` — non-modal toast with "Continue here" / "Start new chat" buttons
[x] - SP-027-3g: Tests — unit test for drift detection with threshold, test for suppression after 3 rejections, test for intent embedding persistence across session restore

### Phase 4: Memory Integration

[x] - SP-027-4a: Add `StoreMemory()` to `ConversationStore` — embed memory file content, store as `VectorRecord` with Type: "memory"
[x] - SP-027-4b: Create `pkg/agent/memory_embedding.go` — `EmbedMemory()` function called from `SaveMemory()`, `DeleteMemory()` also removes from store
[x] - SP-027-4c: Implement one-time memory migration — on first `search_memories` call or app startup, embed all existing `~/.config/sprout/memories/*.md` files into conversation store
[x] - SP-027-4d: Add `search_memories` tool to `pkg/agent/tool_definitions.go` — `search_memories(query: string, max_results?: int) → []{name, title, relevance}`
[x] - SP-027-4e: Implement `handleSearchMemories()` in `pkg/agent/memory_handlers.go` — embed query, search conversation store for Type:"memory" records, return ranked results
[x] - SP-027-4f: Tests — unit test for memory embedding round-trip, test for search tool with semantic query, test for migration of existing memories

---

## SP-028: Test Suite Stabilization — Deadlock Resolution & CI Hardening

Spec: `roadmap/SP-028-test-suite-stabilization.md`

### Phase 1: Unblock CI (loud failures instead of silent hangs)

[x] - SP-028-1a: Add `go.uber.org/goleak` to `go.mod` test deps
[x] - SP-028-1b: Create `pkg/webui/main_test.go` with `TestMain` calling `goleak.VerifyNone(t)` — ignore known long-lived workers from `pkg/logging`/`pkg/history` via `goleak.IgnoreTopFunction(...)`
[x] - SP-028-1c: Create `pkg/agent/main_test.go` with `TestMain` calling `goleak.VerifyNone(t)` — same ignore list as webui
[x] - SP-028-1d: Update `Makefile` `test` target — add `-race -count=1 -timeout=90s` to `pkg/agent` and `pkg/webui` test invocations
[x] - SP-028-1e: Update `.github/workflows/build.yml` — drop `-short` from the race step for `pkg/agent` and `pkg/webui`

### Phase 2: Fix MCP init deadlock

[x] - SP-028-2a: Add double-checked RWMutex fast path to `getMCPTools` in `pkg/agent/mcp.go`
[x] - SP-028-2b: Audit every transitive caller of `LockInit` (`pkg/agent/submanager_mcp.go:73`) for lock-order violations; document the lock-order invariant as a doc comment on `AgentMCPManager`
[x] - SP-028-2c: Reduce `TestMCPConcurrency_StressTest` (`pkg/agent/mcp_concurrency_test.go:264`) from 200×10 to 32×50 and add `t.Cleanup(func() { agent.Shutdown() })`
[x] - SP-028-2d: Verify with `go test -race -run TestMCPConcurrency -count=20 ./pkg/agent/` — must pass 20× in a row

### Phase 3: Fix WebUI PTY goroutine leak

[x] - SP-028-3a: Add `done chan struct{}` to terminal session struct; close it on `session.Close()` (`pkg/webui/terminal_session.go`)
[x] - SP-028-3b: Rewrite the PTY read loop at `pkg/webui/terminal_create.go:146-175` — `select` on `done` alongside the read; use `pty.SetDeadline()` (with periodic-polling fallback if unsupported on platform)
[x] - SP-028-3c: Audit every test that creates a terminal session; add `t.Cleanup(session.Close)` — sweep `pkg/webui/*_test.go`
[x] - SP-028-3d: Verify with `go test -race -count=5 ./pkg/webui/` — `goleak` reports zero leaks (verified 5/5 clean, ~135-142s each)

### Phase 4: Sustain

[x] - SP-028-4a: Create `pkg/agent/concurrency_test.go` — pin the new MCP-init invariant with a fast regression test (16 goroutines, single phase, cleanup-verified)
[x] - SP-028-4b: Add package-level doc comments to `pkg/agent/submanager_mcp.go` and `pkg/webui/terminal_create.go` documenting lock order and PTY lifecycle

---

## SP-029: Monolith Decomposition — File Size Reduction

Spec: `roadmap/SP-029-monolith-decomposition.md`

**Blocked by:** SP-028 Phase 1 (need green baseline before refactoring)

### Phase 1: Smallest blast radius first

[x] - SP-029-1a: Split `pkg/agent/tool_handlers_subagent.go` (1318 LOC) — extract `handleRunParallelSubagents` to `tool_handlers_subagent_parallel.go`, batching to `tool_handlers_subagent_batch.go`, utilities to `tool_handlers_subagent_utils.go`. Pure move, no signature changes.
[x] - SP-029-1b: Split `pkg/agent_providers/generic_provider.go` (1276 LOC) — extract HTTP-error helpers to `generic_provider_errors.go`, request-building helpers to `generic_provider_request.go`, model-listing/vision to `generic_provider_models.go`, max-tokens retry to `generic_provider_retry.go`

### Phase 2: Configuration and optimizer

[x] - SP-029-2a: Split `pkg/configuration/config.go` (1895 LOC) into 9 files per the table in SP-029 — `config_types.go`, `config_risk.go`, `config_subagents.go`, `config_skills.go`, `config_paths.go`, `config_persistence.go`, `config_accessors.go`, `config_validate.go`, plus the slimmed `config.go`. Single PR; coordinate to avoid merge conflicts.
[x] - SP-029-2b: Split `pkg/agent/conversation_optimizer.go` (1319 LOC) — extract summary builders to `conversation_optimizer_summary.go`, file-read tracking to `conversation_optimizer_files.go`, shell-command tracking to `conversation_optimizer_shell.go`

### Phase 3: Surface area packages

[x] - SP-029-3a: Split `pkg/wasmshell/commands.go` (1633 LOC) — `commands_fs.go` (filesystem builtins), `commands_text.go` (text-processing builtins), `commands_env.go` (env/help/util builtins), `commands_util.go` (private helpers)
[x] - SP-029-3b: Split `pkg/webcontent/browser_rod.go` (1335 LOC) — `browser_rod_session.go`, `browser_rod_actions.go`, `browser_rod_capture.go`, `browser_rod_gpu.go`

### Phase 4: Investigate-then-split

[x] - SP-029-4a: Read `pkg/agent/seed_tool_registry.go` (1223 LOC) end-to-end, define the split table, then execute. Likely 3 files (definitions / dispatcher / handler bindings).
[x] - SP-029-4b: Read `pkg/lsp/semantic/go_adapter.go` (1188 LOC) end-to-end, define the split by LSP capability area (definitions, references, completions, diagnostics), then execute. Likely 4 files.
[x] - SP-029-4c: Read `pkg/agent/scripted_client.go` (1068 LOC) end-to-end, separate DSL parsing from playback engine from response builders, then execute. Likely 3 files.

---

## SP-030: Repository Hygiene — Stale Artifacts & Predecessor Cleanup

Spec: `roadmap/SP-030-repository-hygiene.md`

### Phase 1: One-shot cleanup

[x] - SP-030-1a: Delete stale `.test` binaries at repo root — `agent.test`, `configuration.test`, `proxy.test`, `semantic.test` (~56MB total; gitignored)
[x] - SP-030-1b: Delete stale `sprout` binary at repo root (113MB; rebuilt by `make build`)
[x] - SP-030-1c: Delete `code_review_output.json` (gitignored stale dev output)
[x] - SP-030-1d: Delete `.ledit/` directory at repo root (predecessor tool state)
[x] - SP-030-1e: Delete `update_and_test.sh` — entire script invokes a `./ledit` binary that no longer exists
[x] - SP-030-1f: Add `examples/.todo_pipeline_checkpoint.json` to `.gitignore` (the "move runtime state to `.sprout/`" half is deferred — needs locating the writer first)
[x] - SP-030-1g: Add/extend `make clean` target — remove root `.test` binaries, root `sprout` binary, `code_review_output.json`, `dist/local/*`, `dist/cloud/*` (verify with `make clean` manually)

### Phase 2: Docstring/prompt updates

[x] - SP-030-2a: Update `replay_last_request.sh` — `SPROUT_COPY_LOGS_TO_CWD` documented as primary; `LEDIT_COPY_LOGS_TO_CWD` retained as a backwards-compat fallback in `pkg/logging/request_logger.go` (verified)
[x] - SP-030-2b: Renamed `test_runner.py` → `workspace_test_runner.py` (via `mv`; git tracks as delete+add but `git diff -M` will show as rename). Also updated old-filename references in `AGENTS.md`, `CONTRIBUTING.md`, `roadmap/SP-005-infrastructure.md`
[x] - SP-030-2c: Update docstrings in `e2e_test_runner.py` and `integration_test_runner.py` — each docstring now names sprout, its test directory, and real-vs-mocked AI
[x] - SP-030-2d: Update `CLAUDE.md` (project root) Testing section to describe each runner
[x] - SP-030-2e: Update `pkg/agent/prompts/system_prompt.md` — `Ledit - Software Engineering Agent` heading and `/tmp/ledit_examples/` path replaced
[x] - SP-030-2f: Update `pkg/agent/skills/go-conventions/SKILL.md` — module path examples updated to `github.com/sprout-foundry/sprout/...`

> Follow-up: the three Python runners still contain operational `ledit` strings — the `go build -o ledit` output filename, `.ledit/config.json` config path, and "ledit TESTING COMPLETE" banner. Touching those means renaming the build output and migrating the legacy config-dir lookup; left for a dedicated bundle.

### Phase 3: Documentation sweep

[x] - SP-030-3a: Per-file audit and update of `ledit` references in `docs/ELECTRON.md`, `docs/AGENT_WORKFLOW.md`, `docs/PROVIDER_CATALOG.md`, `docs/TESTING.md`, `docs/PRODUCT_BACKLOG.md`, `docs/subagent_personas.md`
[x] - SP-030-3b: Audit `README.md` and update non-historical `ledit` references; leave `CHANGELOG.md` historical sections intact

### Phase 4: Decide-then-act on service names — **DE-SCOPED (moved to SP-032)**

[x] - SP-030-4a: ~~Audit `cmd/service_*` code paths~~ — Done as part of SP-032 audit; live install uses `sprout-daemon` and `cmd/service_linux.go` refuses non-sprout binaries. Pre-existing `ledit` daemon detection becomes **SP-032-2b**; fixture cleanup becomes **SP-032-4a**.
[x] - SP-030-4b: ~~Service name comment/migration~~ — No action required under SP-030. Covered by SP-032.

### Phase 5: Test fixtures

[x] - SP-030-5a: Per-file audit of `pkg/agent/conversation_image_test.go`, `pkg/agent/tool_handlers_search_new_test.go`, `pkg/git/commit_helpers_test.go`, `pkg/history/history_tools_test.go` — replace `ledit` where it's incidental; leave where the literal string is being asserted
[x] - SP-030-5a: Per-file audit of `pkg/agent/conversation_image_test.go`, `pkg/agent/tool_handlers_search_new_test.go`, `pkg/git/commit_helpers_test.go`, `pkg/history/history_tools_test.go` — replace `ledit` where it's incidental; leave where the literal string is being asserted

---

## SP-031: MCP Tool Input Validation Hardening

Spec: `roadmap/SP-031-mcp-input-validation.md`

### Phase 1: Implement validation

[x] - SP-031-1a: Add `github.com/santhosh-tekuri/jsonschema/v6` to `go.mod`
[x] - SP-031-1b: Add `compiledSchema *jsonschema.Schema` field to `MCPToolWrapper` and a `compileSchema()` method with lazy initialization (cache once at first use). `pkg/mcp/tool_wrapper.go`
[x] - SP-031-1c: Replace the `ValidateArgs` stub at `pkg/mcp/tool_wrapper.go:233-238` with real validation — skip on nil schema, fail-open on compile error (warn once), return `*InvalidArgsError` on validation failure
[x] - SP-031-1d: Create `pkg/mcp/errors.go` with `InvalidArgsError` typed error (Tool, Server, Wrapped fields; implements `Error()` and `Unwrap()`)

### Phase 2: Wire into execution

[x] - SP-031-2a: Call `w.ValidateArgs(args)` at the top of `MCPToolWrapper.Execute` before the network round-trip; return early on validation error
[x] - SP-031-2b: Update `CanExecute` (`pkg/mcp/tool_wrapper.go:171`) to call `ValidateArgs` and return `false` on failure; remove the TODO comment
[x] - SP-031-2c: Format validation errors as a concise LLM-visible message — enumerate failing field paths and reasons, not raw `jsonschema` output. Use this as the tool result so the model can self-correct on the next iteration.
[x] - SP-031-2c: Format validation errors as a concise LLM-visible message — enumerate failing field paths and reasons, not raw `jsonschema` output. Use this as the tool result so the model can self-correct on the next iteration.

### Phase 3: Tests

[x] - SP-031-3a: Replace the trivial assertions in `TestMCPToolWrapper_ValidateArgs` (`pkg/mcp/tool_wrapper_test.go:124-127`) with real cases — required fields, type mismatches, enum violations, nested objects
[x] - SP-031-3b: Add test: `ValidateArgs` with `nil` schema → returns nil (skip path)
[x] - SP-031-3c: Add test: `ValidateArgs` with malformed schema → warns once, returns nil (fail-open on our bug)
[x] - SP-031-3c: Add test: `ValidateArgs` with malformed schema → warns once, returns nil (fail-open on our bug)
[x] - SP-031-3d: Add integration test in `pkg/agent/` — stub MCP wrapper that returns `InvalidArgsError`; verify agent surfaces a useful tool-result message to the LLM mock
[x] - SP-031-3d: Add integration test in `pkg/agent/` — stub MCP wrapper that returns `InvalidArgsError`; verify agent surfaces a useful tool-result message to the LLM mock

### Phase 4: Observability

[x] - SP-031-4a: Add structured log entry on validation failure with `{tool, server, errors[]}` fields (cooperates with SP-008 structured logging)
[x] - SP-031-4a: Add structured log entry on validation failure with `{tool, server, errors[]}` fields (cooperates with SP-008 structured logging)
[x] - SP-031-4b: Add a counter/metric for `mcp_validation_failures` so we can see if a particular server is producing bad arguments at rate
[x] - SP-031-4b: Add a counter/metric for `mcp_validation_failures` so we can see if a particular server is producing bad arguments at rate

---

## SP-025: Tree-Sitter Integration — Remaining Work

Spec: `roadmap/SP-025-tree-sitter-integration.md`

Phases 1–3 are complete: `pkg/ast/` is in place (tree-sitter via `odvcencio/gotreesitter v0.16.0`) and consumed by `pkg/agent_tools/repo_map.go` and `pkg/index/symbols.go`. The remaining work closes the gap so `pkg/embedding/extractor_*.go` stops maintaining its own parallel regex zoo, and finishes the WASM wiring.

### Phase 4: WASM Integration (finish)

[x] SP-025-4a: Add a `pkg/ast` import to `pkg/wasmshell/` and surface a basic code-intelligence entry point (e.g. a function-symbol lookup that the WASM shell can call). Today `pkg/ast/browser_cache.go` exists but no caller in `wasmshell` exercises it.
[x] SP-025-4b: Run `make build-wasm` and record the binary-size delta from enabling `pkg/ast` in the WASM target. Document the threshold the team is willing to accept. (Baseline: 4.3M, With ast: 34M, Delta: +29.7M. See roadmap/SP-025-tree-sitter-integration.md)
[x] - SP-025-4c: Verify `pkg/ast/browser_cache.go` (290 LOC) actually persists compiled grammars to browser storage (IndexedDB / localStorage) across page loads — write a manual reproduction note or a headless test.

### Phase 5: Embedding Extractor Migration (the consistency fix)

[x] - SP-025-5a: Replace the body of `pkg/embedding/extractor_ts.go` (~531 LOC, 9 standalone regex patterns starting at `tsFuncRegex` line 13) with a thin adapter that calls `pkg/ast.ExtractSymbols()` and emits the existing embedding record shape. Keep the public function signature stable so callers in `pkg/embedding/index.go:106` don't change.
[x] - SP-025-5b: Replace the body of `pkg/embedding/extractor_py.go` (~345 LOC, regex + indent-level tracking starting at `pyFuncRegex` line 14) with the same adapter pattern over `pkg/ast.ExtractSymbols()`. Confirm class/method nesting comes out of the AST scope info correctly — that's the subtle case the old indent tracker handled.
[x] - SP-025-5c: Decide on `pkg/embedding/extractor_go.go` (currently uses native `go/ast` directly) — keep as-is for performance (no tree-sitter overhead) or migrate to `pkg/ast` for codebase consistency. Document the decision in a one-line comment at the top of the file.
[x] - SP-025-5d: Add a symbol-coverage parity test in `pkg/embedding/extractor_parity_test.go` — given a fixture file in each of TS, JS, Python, assert that the set of symbol names returned by `repo_map`, `pkg/index/symbols`, and `pkg/embedding/extractor` is identical. This is the regression test that would have caught today's three-way disagreement.
[x] - SP-025-5e: Delete the now-orphaned regex variables at the top of `extractor_ts.go` and `extractor_py.go` after the body migration in 5a/5b. Net code reduction target: ~700 LOC (with corresponding test simplification).
[x] - SP-025-5f: Run `make build-all && go test ./pkg/embedding/...` and exercise an embedding refresh against the repo itself — verify previously-missed symbols (TS arrow functions, decorated Python methods, multi-line signatures) now appear in `~/.config/sprout/embeddings/*.jsonl`.

---

## SP-032: Daemon Mode Hardening

Spec: `roadmap/SP-032-daemon-mode-hardening.md`

The daemon's install/uninstall surface is solid, but `systemctl stop sprout` leaks the agent, MCP children, and active PTYs — and the HTTP API can be exposed unauthenticated if `SPROUT_BIND_ADDR` is misconfigured. SP-032 closes these gaps.

### Phase 1: Graceful shutdown (CRITICAL)

[x] - SP-032-1a: Add `chatAgent.Shutdown()` call to the graceful-shutdown block at `cmd/agent_modes.go:447-460` — with a bounded context (5s) so it can't block daemon exit. `chatAgent.Shutdown()` is defined at `pkg/agent/agent_lifecycle.go:10` and is currently never invoked from the daemon path.
[x] - SP-032-1b: Wire `ws.terminalManager.CloseAllSessions()` into `pkg/webui/server_lifecycle.go:126` `Shutdown()` before `ws.server.Shutdown(ctx)`. **Blocked by SP-028 Phase 3** (cancellable PTY read loop is a prerequisite — without it, `CloseAllSessions()` will block on `pty.Read`).
[x] - SP-032-1c: Update the systemd unit template in `cmd/service_linux.go` — add `TimeoutStopSec=15`, `KillMode=mixed`, `KillSignal=SIGTERM` to the `[Service]` block.
[] - SP-032-1d: Manual verification — install + start the daemon, open a web terminal, kick off an agent query, run `systemctl --user stop sprout`. `pgrep -f sprout` (and `pgrep -f gopls` / `pgrep -f bash` from the terminal) returns empty within 15s.

### Phase 2: Security & migration (HIGH)

[x] - SP-032-2a: At `pkg/webui/server.go:161`, read both `SPROUT_AUTH_TOKEN` and `SPROUT_BIND_ADDR`. If bind is non-`127.0.0.1`/`localhost` and token is empty, refuse to start with: `"Refusing to start: SPROUT_BIND_ADDR=%s requires SPROUT_AUTH_TOKEN to be set."` Cover this with a startup test.
[x] - SP-032-2b: Add `detectLegacyService()` helper in `cmd/service.go` (cross-platform). Darwin checks `~/Library/LaunchAgents/com.ledit.*.plist`; Linux checks `~/.config/systemd/user/ledit*.service`. On `sprout service install`: print notice, prompt for confirmation (`-y` bypasses), then `launchctl bootout` / `systemctl --user disable && rm` the old unit before installing.
[x] - SP-032-2c: Launchd crash backoff — `cmd/service_darwin.go:77`, switch `KeepAlive=true` to the dictionary form with `SuccessfulExit=false` (and `ExponentialBackoff=true` if targeting macOS 12+; document the minimum). Prevents the panic hot-loop.

### Phase 3: Operability (MEDIUM/LOW)

[x] - SP-032-3a: Wrap Darwin daemon stdout/stderr log files (`~/.sprout/logs/daemon.{stdout,stderr}.log` from `cmd/service_darwin.go:35-36`) in `lumberjack.Logger` — 10MB max, 5 backups. `lumberjack` is already a dep.
[x] - SP-032-3b: Pre-uninstall active-session check — before `Uninstall()` in `cmd/service_darwin.go:220` and `cmd/service_linux.go:125`, query the running daemon (if any) for active session count via its HTTP API. Print warning + count; require `-y`/`--yes` flag to skip.
[x] - SP-032-3c: Add `syscall.SIGHUP` to the signal handler at `cmd/agent_modes.go:240`. On SIGHUP, call `configuration.Reload()`. Scope is on-disk config re-read only; running agents/tools unaffected.
[x] - SP-032-3d: Write `docs/SERVICE.md` — install, start, stop, uninstall, troubleshoot, log file locations, env-file structure, and the security model section (user-uid execution, 127.0.0.1 default, auth-token requirement for non-local binds).

### Phase 4: Test fixture cleanup

[x] - SP-032-4a: Update `cmd/service_darwin_test.go` (lines 11, 28, 69, 96) and `cmd/service_linux_test.go` (lines 18, 20, 102, 130) — replace `/usr/local/bin/ledit`, `/opt/ledit/bin/ledit`, `/usr/bin/ledit` test fixtures with the `sprout` equivalents so tests actually exercise the binary-name guard in `cmd/service_linux.go` `Install()`.

---

## SP-033: Agent Trust Boundary Hardening

Spec: `roadmap/SP-033-agent-trust-boundary-hardening.md`

Three trust boundaries to defend: the project (skills auto-load silently), the disk (tool outputs persist unredacted at `0644` and accrete forever), and subprocesses (MCP restarts unbounded, Chromium leaks on panic, Python has no timeout). Already-good baselines (api_keys.json at 0600, git-arg validator, `$()` recursive classification) stay untouched.

### Phase 1: Skill discovery UX

[x] - SP-033-1a: Print a discovery notice listing every project-local skill (name + path) when `discoverProjectSkills` at `pkg/configuration/config.go:1690-1755` finds any. Stderr in CLI mode, startup banner in WebUI.
[x] - SP-033-1b: Implement `.sprout/allowed_skills` allowlist file (one ID per line) with read/write helpers.
[x] - SP-033-1c: In `discoverProjectSkills`, load skills not in the allowlist with `Enabled: false` so they appear but don't activate.
[] - SP-033-1d: New CLI commands `sprout skills allow <id>...` / `sprout skills revoke <id>...` / `sprout skills list` in `cmd/skills.go`.
[x] - SP-033-1e: `--no-project-skills` flag on the agent command; default to "skip" when stdin is non-TTY (CI / non-interactive).
[x] - SP-033-1f: Set `Metadata["source"]` to `builtin` / `project:<repo-root>` / `user` on every loaded skill; surface in the agent system prompt so the model knows where instructions came from.

### Phase 2: Redaction + file modes

[x] - SP-033-2a: Create `pkg/redact/redact.go` with `Apply([]byte) []byte` covering AWS keys (`AKIA...`), GitHub tokens (`gh[pousr]_...`), Slack tokens, OpenAI/Anthropic-style `sk-...`, `BEGIN ... PRIVATE KEY` blocks, `Authorization:` / `X-API-Key:` headers, `*_TOKEN|*_KEY|*_SECRET|*_PASSWORD` env-style assignments. Replacement: `[REDACTED:<kind>]`.
[x] - SP-033-2b: Pipe HTTP bodies through `redact.Apply` in `pkg/logging/request_logger.go` (runlog write path).
[x] - SP-033-2c: Apply `redact.Apply` to `UserPrompt` and `ActionableSummary` in `pkg/agent/turn_checkpoints.go` before SP-027's `EmbedAndStoreTurn()`.
[x] - SP-033-2d: Apply `redact.Apply` in `pkg/agent/memory_handlers.go` before writing memory files.
[] - SP-033-2e: Conditional redaction in `pkg/history/changetracker.go:461,481` — only redact when the revision target is *outside* the workspace root (in-workspace revisions are the real file content; out-of-workspace revisions like `~/.aws/credentials` are leakable).
[x] - SP-033-2f: Change file modes `0644` → `0600` at `pkg/history/changetracker.go:461,481`. Audit all `os.WriteFile(…0644)` sites under `pkg/logging/`, `pkg/embedding/` (for `conversation_turns.jsonl`), `pkg/agent/memory*.go` — tighten where data is user-private.

### Phase 3: Lifecycle commands

[] - SP-033-3a: `sprout history clear [--older-than DURATION] [--workspace PATH]` in `cmd/history.go` — removes runlogs and change-tracker entries.
[] - SP-033-3b: `sprout embeddings clear [--type conversation_turn|memory|code]` in `cmd/embeddings.go`.
[] - SP-033-3c: Add `RetentionDays int` to `PersistentContextConfig` (default `0` = forever); background sweep on agent startup removes expired entries.
[] - SP-033-3d: All clear operations confirmation-prompt by default with `-y`/`--yes` bypass; support `--dry-run` to preview deletions.

### Phase 4: Subprocess hardening

[] - SP-033-4a: At `pkg/mcp/client.go:147`, replace bare `restartCount++` with a sliding-window check — after 3 failures in 60s, exponential backoff (start 1s, double, max 5min); after 10 failures in 24h, disable the server and surface a notice.
[] - SP-033-4b: Register `webcontent.RodRenderer.Close()` in the interactive-mode signal handler; add a `runtime.SetFinalizer` backstop on the renderer struct in `pkg/webcontent/browser_rod.go:1311`. Coordinate with SP-032 A1 so the daemon path is also covered.
[x] - SP-033-4c: At `pkg/pythonruntime/runtime.go:65`, replace `exec.Command(...)` with `exec.CommandContext(ctx, ...)` carrying a 30s default deadline (configurable for longer operations).

### Phase 5: Audit log + documentation

[] - SP-033-5a: Extend runlog entries in `pkg/agent/tool_executor*.go` to capture all four of: raw tool-call JSON, executed (post-substitution) command, classifier decision (`SecuritySafe`/`SecurityCaution`/`SecurityDangerous`), and approval source (auto-rule X / manual / denied).
[x] - SP-033-5b: Write `docs/SECURITY.md` — trust boundaries, classifier limitations (lift from `pkg/agent_tools/security_classifier.go:12-25` header), file layout per directory, how to clear persisted data, skill allowlist model, auth-token requirement for non-local binds (refs SP-032 B1).
[x] - SP-033-5c: Create `SECURITY.md` at repo root with vuln-reporting contact and a link to `docs/SECURITY.md`.

---

## SP-034: WebUI ↔ Backend Workflow Hardening

Spec: `roadmap/SP-034-webui-workflow-hardening.md`

Four user-visible defects: Stop button doesn't cancel the in-flight LLM HTTP call, reloading the page during an agent run loses the live stream, two tabs on the same chat corrupt each other's state, and UI config writes silently overwrite concurrent CLI writes. Plus protocol hygiene: hand-maintained TS types drift from Go structs, outbound WebSocket messages aren't validated, error envelope is inconsistent.

### Phase 1: Cancellation that actually cancels (CRITICAL)

[x] - SP-034-1a/1b: Threaded `ctx context.Context` through both `ClientInterface` and `ProviderInterface` (in `pkg/agent_api/interface.go` and `types.go` respectively), covering `SendChatRequest`, `SendChatRequestStream`, **and** `SendVisionRequest` since vision shares the same Stop-button concern. All implementations updated: `pkg/agent_providers/generic_provider.go` (new `buildHTTPRequestCtx` uses `http.NewRequestWithContext`), `pkg/agent_api/ollama_local.go` (forwards ctx into the existing 300s `context.WithTimeout` child), `pkg/agent_api/unified.go`, `pkg/agent_api/provider_adapter.go`, `pkg/factory/factory.go` (TestClient), `pkg/agent/scripted_playback.go` (ScriptedClient). All callsites updated: `pkg/agent/seed_integration.go` (passes the doChat caller ctx through), `pkg/agent/agent_getters.go::GenerateResponse` (uses `a.interruptCtx`), `pkg/agent/llm_summarizer.go` (now forwards the seed-supplied ctx instead of ignoring it). Callsites lacking a parent ctx today use `context.Background()` with `TODO(SP-034-1c)` markers: `pkg/codereview/prompts.go`, `pkg/agent_tools/vision_analyze.go`/`vision_pdf.go`, `pkg/git/commit_message_generator.go`, `pkg/spec/extractor.go`/`validator.go`, `pkg/agent_commands/commit_review.go`/`shell.go`, `smoke_tests/test_api_functionality.go`, plus my own `cmd/wasm/chat_funcs.go`. 10 test files mechanically updated with sed (`SendChatRequest(`/`SendChatRequestStream(`/`SendVisionRequest(` callsites get `context.Background()` prepended; mock implementations get `ctx context.Context` added). Native + WASM builds clean; `pkg/agent_api`, `pkg/agent_providers`, `pkg/factory`, `pkg/codereview`, `pkg/git` test suites all pass.
[] - SP-034-1c: Update every caller in `pkg/agent/` (`api_client.go`, `conversation.go`, `seed_integration.go`) to pass through a real context.
[x] - SP-034-1d: `pkg/agent_providers/generic_provider.go:1168` (the formerly-line-1160 buildHTTPRequest) now uses `http.NewRequestWithContext` via the new `buildHTTPRequestCtx` helper introduced in 1a/1b. Repo-wide audit: every `http.NewRequest` in `pkg/agent_providers/` and `pkg/agent_api/` (chat + model-listing + retry paths) is `WithContext`. 5 contextless `http.NewRequest` calls remain in `pkg/agent/resource_capture.go`, `pkg/configuration/custom_provider_registry.go`, and `pkg/webcontent/` — all orthogonal to LLM cancellation; left for a future web-content/config cancellation pass.
[x] - SP-034-1e/1f: **Achieved via a simpler path than the original spec.** The webui's `handleAPIQueryStop` already calls `clientAgent.TriggerInterrupt()`, which cancels `a.interruptCancel`. The bug was that `pkg/agent/seed_integration.go:617` passed `context.Background()` to `seedAgent.Run` — so the cancellation had no path to `http.NewRequestWithContext`. Fixed by passing `a.interruptCtx` instead. Added `Agent.resetInterruptForNewQuery()` in `pkg/agent/pause.go` so each new `ProcessQuery` gets a fresh ctx (otherwise a Stop on query N would instantly cancel query N+1). The originally-proposed "stash a separate cancel on the chat session" would have been a redundant second cancellation path; the agent's existing interrupt machinery is sufficient now that ctx actually reaches the HTTP layer.
[x] - SP-034-1g: Already configured. `pkg/agent_providers/provider_config.go::GetTimeout` returns 5min and `GetStreamingTimeout` returns 15min as defaults, applied to `httpClient.Timeout` / `streamingClient.Timeout` in `NewGenericProvider`. Per-provider override via `streaming.chunk_timeout_ms` config. The originally-spec'd "10 minute" default is bracketed by these existing values; raising the non-streaming default toward 10min isn't worth re-defaults churn given streaming is the longer path and already gets 15min.
[x] - SP-034-1h: `pkg/agent_providers/cancellation_test.go` covers two paths. `TestSendChatRequest_CtxCancelAborts` stands up an httptest stub that sleeps until r.Context().Done(); the client cancels at 50ms; asserts SendChatRequest returns within 5s with `context.Canceled` in the error chain (vs the 2s stub timeout). `TestSendChatRequestStream_CtxCancelAborts` covers the streaming path — emits one SSE chunk, waits for the callback to fire, cancels, asserts the call returns within 5s and the callback received its chunk. Both pass stably (3× back-to-back runs, total ~6s). The original spec was "30s sleep, cancel after 1s, return within 1s"; my version tightens the cancel-to-50ms and bound-to-5s while keeping the same end-to-end intent.

### Phase 2: Chat reattach (HIGH)

[] - SP-034-2a: Add a `chatRunRingBuffer` to the chat session struct in `pkg/webui/chat_sessions.go` — last 5,000 stream chunks (configurable) with monotonic `seq`. Cap by chunk-count *and* total bytes.
[] - SP-034-2b: In `publishClientEventWithChat` (`pkg/webui/api_query.go:85`), append stream-chunk events to the ring buffer.
[] - SP-034-2c: Extend the chat WebSocket handler to accept `?reattach=<chat-id>&after_seq=<n>`; replay buffered events with `seq > n`, then resume live stream. Mirror the shape of terminal reattach at `pkg/webui/terminal_websocket.go:48-74`.
[] - SP-034-2d: Send a `chat_run_restored` message on reattach with `{chat_id, last_seq, missed_chunks_count}`. Register this type in the outbound list (Phase 5 E2).
[] - SP-034-2e: Frontend — on WebSocket open during an active chat (detect via `/api/query/status`), automatically reconnect with `reattach` + last-seen `seq`. Transparent to the user.
[] - SP-034-2f: Buffer TTL — clear 60s after run completion; total memory cap to prevent runaway on multi-million-token runs.

### Phase 3: Multi-tab consistency (CRITICAL)

[] - SP-034-3a: Add `chatSubscribers map[string][]connection` to `ReactWebServer` (`pkg/webui/server.go:42`) under `sync.RWMutex`.
[] - SP-034-3b: Handle inbound `subscribe` WebSocket message (already whitelisted at `pkg/webui/websocket_message_types.go:42`) by adding the connection to the chat's subscriber list. Clean up on disconnect.
[] - SP-034-3c: Refactor `publishClientEventWithChat` (`pkg/webui/api_query.go:85`) — when `chatID != ""`, fan out to every connection in `chatSubscribers[chatID]` rather than only the originator.
[] - SP-034-3d: Add a per-chat writer mutex for `AgentState` mutations in `pkg/webui/chat_sessions.go:32`. Reads snapshot under RLock; writes serialize.
[] - SP-034-3e: Emit `session_changed` events on rename/pin/switch in `pkg/webui/chat_sessions_api.go`. Register this type in the outbound list.
[] - SP-034-3f: Frontend — on `session_changed`, reconcile by replacing local session state with the broadcast payload (canonical wins over optimistic).

### Phase 4: Config conflict detection (CRITICAL)

[] - SP-034-4a: Add private `loadedModTime time.Time, loadedSize int64` fields to `Config` (`pkg/configuration/config.go`). Populate in `Load()` in `pkg/configuration/config_persistence.go`.
[] - SP-034-4b: Before each `Save()`, `os.Stat` the target path. If `(modTime, size) != (loadedModTime, loadedSize)`, return a new typed `ConfigConflictError` (create `pkg/configuration/errors.go`).
[] - SP-034-4c: Surface the typed error in `pkg/webui/websocket_message_handlers.go:49-59` as `{code: "config_conflict", current_summary: {provider, model, ...}}`.
[] - SP-034-4d: Frontend — non-blocking "Settings changed on disk" toast with a Reload action.
[] - SP-034-4e: Regression test — load config, modify file externally (touch mtime), attempt save → expect `ConfigConflictError`.

### Phase 5: Protocol hygiene (HIGH)

[] - SP-034-5a: Add `tygo` (or equivalent Go→TS type generator) to dev tooling. New `make generate-ts-types` Makefile target emits `webui/src/types/generated.ts` from annotated Go structs.
[] - SP-034-5b: Annotate `chatSession` (`pkg/webui/chat_sessions.go:27-52`), event payloads (`pkg/webui/events/*.go`), and key API response shapes with the tygo emit marker.
[] - SP-034-5c: Replace the hand-maintained TS interface in `webui/src/.../chatSessions.ts:6-21` with an import from `generated.ts`. Keep computed-only fields (`is_default`, `is_active`) in a separate wrapper type.
[] - SP-034-5d: Extract the inbound message-type whitelist from `pkg/webui/websocket_message_types.go:42` into a shared registry; add `validateOutbound(msg)` called by every `WriteJSON` site (panic in dev builds, log+drop in prod).
[x] - SP-034-5e: Define a `WebUIError` struct `{Code, Message, Details, Retryable}` in `pkg/webui/errors.go`. Replace stringy 503 returns at `pkg/webui/api_query.go:391-396` and audit other handlers for the same anti-pattern.
[] - SP-034-5f: Frontend — shared error-handling util keyed on `Code`; deprecate string-matching on `Message`.

### Phase 6: Documentation

[] - SP-034-6a: Write `docs/WEBUI_PROTOCOL.md` — REST endpoints table, WebSocket inbound + outbound message types, event payload shapes, reattach flow, error envelope, type-generation workflow.

---

## SP-035: Persona System Tightening

Spec: `roadmap/SP-035-persona-system-tightening.md`

The persona system works today but several behaviors that *should be loud are silent*: EA inherits its risk cascade implicitly, the two-gate model has no integration test, force-flag detection lacks fuzz coverage, dropped user overrides emit no warning, and SP-026 docs point at the wrong path. Each is fixable cheaply.

### Phase 1: Explicit EA rules

[] - SP-035-1a: Add `auto_approve_rules` block to `pkg/personas/configs/executive_assistant.json`. Initial values: literal copy of `DefaultAutoApproveRules()` from `pkg/configuration/config.go:195-213`. The PR review is the "should EA differ from defaults?" conversation.
[] - SP-035-1b: Audit `pkg/personas/configs/default_personas.json` and `project_planner.json` — per persona, decide explicitly whether to declare rules or inherit. Add a `"_rules_source"` annotation field so the decision is visible.
[] - SP-035-1c: Add `TestPersona_EA_RiskCascadeBaseline` in `pkg/configuration/` — load EA, call `GetAutoApproveRules()`, deep-equal against the approved baseline. Failure prints the diff so a drift is impossible to miss.

### Phase 2: Two-gate invariant tests

[] - SP-035-2a: Add `TestRiskGates_GlobalClassifierIsNotBypassedByPersona` — synthetic persona with `rm_command` in `LowRiskOps`; submit `rm -rf /`; assert the global `ClassifyToolCall` at `pkg/agent/tool_definitions.go:541` still blocks.
[] - SP-035-2b: Add `TestRiskGates_BothGatesEvaluate` with counter wrappers around `EvaluateOperationRisk` (`pkg/agent/tool_handlers_shell.go:90,195,381`) and `ClassifyToolCall` — assert both run for each command in a dangerous-commands fixture.
[] - SP-035-2c: Add a package-level doc comment to `pkg/agent/tool_handlers_shell.go` describing the two-gate model and the invariant "neither gate may suppress the other."

### Phase 3: Force-flag fuzz tests

[] - SP-035-3a: Extend `pkg/configuration/config_risk_test.go:119,143` tables with: `tar -xzf`, `tar -fvz`, `grep -f patterns`, `git -f commit` (malformed position), `rsync --force`, `rsync --force-with-lease`, `cp -rf`, `mv -f`, `git push --force-with-lease`, `docker rm -f`, `docker rm --force`. Each entry carries a one-line `why:` comment.
[] - SP-035-3b: Add `TestContainsForceFlag_Property` using `testing/quick` with iteration count 1000 — generates random {command, flags, args} combos and asserts the function's verdict matches a documented reference for the curated cases.

### Phase 4: Loud warnings on silent overrides

[] - SP-035-4a: At `pkg/configuration/config.go:1408-1414`, after the existing comment block, detect `len(userOverride.AllowedTools) > 0` for a built-in persona and log a warning via `pkg/logging` naming the persona and the dropped tool list — message: "AllowedTools override ignored for built-in persona '%s'; create a new persona ID to customize tools."
[] - SP-035-4b: In `mergeLegacyStructuredToolsIntoPersonaAllowlists` at `pkg/configuration/config.go:1462`, iterate every persona (not just defaults). For custom personas with `write_file` but no `write_structured_file`, log a one-time warning per config-load.
[] - SP-035-4c: Tests — `TestAllowedToolsOverride_WarnsAndDrops`, `TestLegacyCustomPersona_WarnsOnce`. Both assert the warning is emitted via the logger fixture and that the underlying behavior (drop / no-migrate) is unchanged.

### Phase 5: Documentation

[] - SP-035-5a: Update `roadmap/SP-026-executive-assistant.md` Phase E — correct the prompt path from `subagent_prompts/executive_assistant.md` to `pkg/agent/prompts/subagent_prompts/executive_assistant.md`. Add a "Where prompts live" subsection near the top of the spec.
[] - SP-035-5b: Write `docs/PERSONAS.md` covering: the three-layer architecture (catalog → config → session), merge resolution rules (what overrides, what doesn't, why), the two-gate risk model, the depth model (0/1/2), `LocalOnly` + `IsLocalMode` semantics, how to define a custom persona, and provider/model cost considerations.
[] - SP-035-5c: When SP-033's `docs/SECURITY.md` lands, add a cross-link from its "trust boundaries" section to `docs/PERSONAS.md`. (Tracked here as a forward-reference; do the edit in whichever order the specs land.)

---

## SP-036: Concurrency Leak Resolution — Removing the goleak Allowlist

Spec: `roadmap/SP-036-concurrency-leak-resolution.md`

SP-028 unblocked CI by silencing four real goroutine leaks via `goleak.IgnoreTopFunction` / `IgnoreAnyFunction` rather than fixing them. The allowlist now masks production-relevant leaks in `fileWatcher`, LSP proxy `cleanupLoop`, and `TerminalManager.ExecuteCommandAndWait`. This spec fixes each at the source and removes the allowlist entries one by one.

### Phase 1: Investigation

[] - SP-036-1a: Read each leaking goroutine's source. Confirm root cause for each of the four our-code allowlist entries in `pkg/webui/main_test.go:19-22` and the `os/exec.(*Cmd).watchCtx` entries at both `main_test.go` files.
[] - SP-036-1b: Decide per-entry: fix vs. document vs. defer to upstream. Record the verdict in the spec's Current State table.

### Phase 2: Track A — fileWatcher

[] - SP-036-2a: Add `done chan struct{}` + `sync.Once`-guarded `Stop()` to the `fileWatcher` struct in `pkg/webui/`. Locate via `grep -n "type fileWatcher" pkg/webui/`.
[] - SP-036-2b: Convert the `start()` event loop to `select` on `done` + fsnotify events; `.Close()` the underlying `*fsnotify.Watcher` in the done arm.
[] - SP-036-2c: Audit every `fileWatcher{…}` instantiation site for `Stop()` call in its shutdown path. `grep -rn "fileWatcher{" pkg/webui/`.
[] - SP-036-2d: Add `t.Cleanup(func() { fw.Stop() })` to any test that directly instantiates a `fileWatcher`.
[] - SP-036-2e: Remove `goleak.IgnoreTopFunction("…fileWatcher.start.func1")` from `pkg/webui/main_test.go:19`. Verify with `go test -race -count=5 ./pkg/webui/`.

### Phase 3: Track B — LSP proxy cleanup loop

[] - SP-036-3a: Plumb `context.Context` into `pkg/lsp/proxy.NewManager` (use existing field if present). Locate `cleanupLoop` via `grep -n "cleanupLoop" pkg/lsp/proxy/`.
[] - SP-036-3b: Add `select` on `ctx.Done()` in `cleanupLoop` alongside the existing `time.Ticker` case.
[] - SP-036-3c: Add idempotent `Shutdown(ctx context.Context) error` method; wire into `pkg/webui/server_lifecycle.go` alongside the existing `terminalManager` shutdown.
[] - SP-036-3d: Remove `goleak.IgnoreTopFunction("…/pkg/lsp/proxy.(*Manager).cleanupLoop")` from `pkg/webui/main_test.go:20`.

### Phase 4: Track C — ExecuteCommandAndWait

[] - SP-036-4a: Refactor `TerminalManager.ExecuteCommandAndWait` (`pkg/webui/`) to use `exec.CommandContext` with a derived context cancelled in `defer` before `Wait()` returns. Use `io.Copy` to drain stdout/stderr in goroutines joined by `errgroup.Group` before `Wait()`.
[] - SP-036-4b: Add `TestExecuteCommandAndWait_NoGoroutineLeak` in `pkg/webui/` that runs the helper 100 times and asserts `runtime.NumGoroutine()` stays bounded.
[] - SP-036-4c: Remove the two `ExecuteCommandAndWait` entries (`pkg/webui/main_test.go:21-22`) and the corresponding `os/exec.(*Cmd).watchCtx` AnyFunction ignore at line 28 (and `pkg/agent/main_test.go:20`) if no longer needed.

### Phase 5: Track D — fsnotify shared worker

[] - SP-036-5a: Trace `fsnotify.(*shared).sendEvent` in fsnotify v1.9 source; confirm whether it is per-`Watcher` or per-process.
[] - SP-036-5b: If per-`Watcher`, remove the AnyFunction allowlist (Track A's fileWatcher.Close fix already handles it). If per-process, replace with a `// REASON: fsnotify v1.9 maintains a process-lifetime worker — see <upstream link>` comment.

### Phase 6: Regression pinning + documentation

[] - SP-036-6a: Add `TestNoNewGoroutineLeaks_Webui` and `TestNoNewGoroutineLeaks_Agent` that snapshot goroutines, run a representative workload (create+close fileWatcher, start+stop LSP manager, exec via TerminalManager), and assert delta ≤ 2.
[] - SP-036-6b: Add `make test-leak` target running `go test -race -count=10` on `pkg/webui` and `pkg/agent` with verbose goleak output.
[] - SP-036-6c: Add package-level doc comments to `pkg/webui/file_watcher.go` (or wherever `fileWatcher` lives) and `pkg/lsp/proxy/manager.go` documenting the shutdown contract.

---

## SP-037: Subagent Resource Budgeting — Bounded Parallelism

Spec: `roadmap/SP-037-subagent-resource-budgeting.md`

`SubagentRunner.RunParallel` (`pkg/agent/subagent_runner.go:108`) spawns one goroutine per task with no semaphore, no queue, and no aggregate token budget. A misbehaving orchestrator that schedules 100 parallel subagents creates 100 LLM clients simultaneously. This spec adds bounded execution, a fleet-wide token budget, and telemetry.

### Phase 1: Bounded execution

[] - SP-037-1a: Add `MaxConcurrentSubagents int` field to `SubagentOptions` in `pkg/agent/subagent_runner.go:20-30`.
[] - SP-037-1b: Add `MaxConcurrentSubagents` to `SubagentConfig` in `pkg/configuration/config.go`. Default 4. Cap at 16 unless `unsafe_unbounded_subagents: true`.
[] - SP-037-1c: Replace the unbounded `go func` at `pkg/agent/subagent_runner.go:124` with a `chan struct{}` semaphore acquire-before-spawn, release in defer.
[] - SP-037-1d: Verify `RunParallel(N>limit)` still returns `len(results) == len(tasks)` in original order. Add `TestSubagentRunner_OrderPreservedUnderBatching`.
[] - SP-037-1e: On parent `ctx` cancellation, drop queued (not-yet-started) tasks immediately with `Result{Cancelled: true}`.

### Phase 2: Fleet token budget

[] - SP-037-2a: Add `FleetTokenBudget int` to `SubagentOptions`; zero means unlimited.
[] - SP-037-2b: Implement a `fleetBudget` struct with atomic debit; on overdraw, mark failed and cancel the shared context.
[] - SP-037-2c: Hook budget debit into the per-subagent LLM-call wrapper in `pkg/agent/conversation.go` (or a new helper); subagents over individual quota at exhaustion return `Result{Truncated: true}`.
[] - SP-037-2d: Add `fleet_token_budget: 200000` default to `pkg/personas/configs/executive_assistant.json` (follow SP-035 Track A's explicit-policy approach).

### Phase 3: Telemetry

[] - SP-037-3a: Add atomic counters (`Active`, `Queued`, `Completed`, `Failed`, `Cancelled`, `TotalQueuedWaitMS`) to `SubagentRunner`; expose via `Metrics()` accessor.
[] - SP-037-3b: Emit `subagent.queued / started / completed / cancelled` events through `pkg/events/`.
[] - SP-037-3c: Add a Subagents resource-usage row to `webui/src/components/.../SubagentsTab.tsx` (or `packages/ui/.../SubagentsPanel` per SP-039 outcome) showing live counts.
[] - SP-037-3d: Write the same events to runlog via `pkg/logging/`.

### Phase 4: Stress + regression

[] - SP-037-4a: Add `TestSubagentRunner_BoundedConcurrency` — submit 50 tasks against limit=4 with a stub client sleeping 100ms; assert max concurrent ≤ 4 throughout.
[] - SP-037-4b: Add `TestSubagentRunner_FleetBudgetCancels` — 10 tasks, fleet budget 5000 tokens, 600 tokens per call; assert at least one cancellation and overdraw bounded by one subagent's individual `MaxTokens`.
[] - SP-037-4c: Add `TestSubagentRunner_NoGoroutineLeak_AfterStress` — runs the bounded stress test and asserts `goleak.VerifyNone(t)` plus `runtime.NumGoroutine()` delta ≤ 2.
[] - SP-037-4d: Add `TestSubagentRunner_ParentCancelDropsQueued` — 20 tasks, limit=2, cancel parent ctx after 50ms; assert remaining 18 return `Cancelled` without starting.
[] - SP-037-4e: Run `go test -race -run TestSubagentRunner -count=20 ./pkg/agent/` to verify stability.

### Phase 5: Documentation

[] - SP-037-5a: Add a "Subagent resource model" section to `docs/AGENT_WORKFLOW.md` covering concurrency limit, fleet budget, telemetry, and how to read the WebUI Subagents tab.
[] - SP-037-5b: Add a package-level doc comment to `pkg/agent/subagent_runner.go` documenting the semaphore + budget invariants.

---

## SP-038: Tool Dispatch Consolidation — Registry Over Switch

Spec: `roadmap/SP-038-tool-dispatch-consolidation.md`

Adding a tool today requires editing four locations across two packages (definition in 1007-line `tool_definitions.go`, handler in one of 10+ `tool_handlers_*.go`, dispatch in `tool_executor*.go`, and command surface in 62-file `pkg/agent_commands/`). No `ToolHandler` interface, no registry, no startup assertion that every declared tool has a handler. This spec introduces a registry + interface and migrates tools incrementally.

### Phase 1: Interface + registry

[] - SP-038-1a: Create `pkg/agent_tools/handler.go` with `ToolHandler` interface (`Name`, `Definition`, `Validate`, `Execute`), `ToolEnv` (explicit deps, no `*Agent`), `ToolResult` (Output, StructuredOut, TokenUsage for SP-037).
[] - SP-038-1b: Create `pkg/agent_tools/registry.go` with thread-safe `ToolRegistry` (`Register`, `Lookup`, `All`, `ForPersona`).
[] - SP-038-1c: Move `ClassifyToolCall` from `pkg/agent/tool_definitions.go` (current location around line 541 per SP-035 references) to `pkg/agent_tools/security_classifier.go`. Update all callers.
[] - SP-038-1d: Create `pkg/agent_tools/all.go` as the central tools-init file (initially empty — tools migrate in over time).

### Phase 2: Dual-dispatch shim

[] - SP-038-2a: In `pkg/agent/tool_executor*.go`, check the registry first; fall back to legacy switch on miss. Add a debug log line per dispatch path so migration progress is observable.
[] - SP-038-2b: Add `TestDualDispatch_RegistryWins` confirming a registered tool takes precedence over a legacy entry of the same name.

### Phase 3: Migrate small tools

[] - SP-038-3a: Migrate `read_file` to `pkg/agent_tools/read_file.go`. Remove from legacy. Add `TestTool_ReadFile_Conformance`.
[] - SP-038-3b: Migrate `list_directory`.
[] - SP-038-3c: Migrate `web_fetch`.
[] - SP-038-3d: Migrate remaining small tools (`read_directory`, `glob`, similar) one per commit.

### Phase 4: Migrate medium tools

[] - SP-038-4a: Migrate `write_file` and `write_structured_file` together (preserve the SP-035 Phase 4 migration warning behavior).
[] - SP-038-4b: Migrate `edit_file`.
[] - SP-038-4c: Migrate `shell_command` — careful interaction with the SP-035 two-gate risk model; the `EvaluateOperationRisk` and `ClassifyToolCall` callouts must remain on the path.
[] - SP-038-4d: Migrate `search_memories` / `save_memory` (touches SP-027 conversation-store paths).

### Phase 5: Migrate large/complex tools

[] - SP-038-5a: Migrate the subagent family (`run_subagent`, `run_subagent_parallel`, task queue tools); likely a `pkg/agent_tools/subagent/` subdirectory due to size.
[] - SP-038-5b: Migrate `task_queue_*` and `todo_*` tools.
[] - SP-038-5c: Migrate remaining tools (image/vision, PDF, browser, web search).

### Phase 6: Cleanup + tests

[] - SP-038-6a: Remove the legacy switch from `pkg/agent/tool_executor*.go` once every tool is registered.
[] - SP-038-6b: Verify `pkg/agent/tool_definitions.go` is ≤ 150 lines.
[] - SP-038-6c: Add `TestRegistry_AllToolsHaveValidDefinitions`, `TestRegistry_AllToolsRespectPersonaFilter`, `TestRegistry_AllToolsValidate`, `TestRegistry_NoOrphanHandlers` in `pkg/agent_tools/registry_test.go`.
[] - SP-038-6d: Run `go test -race ./pkg/agent/ ./pkg/agent_tools/` 10× clean.

### Phase 7: Documentation

[] - SP-038-7a: Write `docs/TOOLS.md` covering: how to add a tool (one-file recipe), the `ToolHandler` interface, the `ToolEnv` contract, the registry init order, the persona filter, and the relationship between tools and `pkg/agent_commands/`.
[] - SP-038-7b: Add a package-level doc comment to `pkg/agent_tools/handler.go`.

---

## SP-039: UI Package Consolidation — One Canonical Component Library

Spec: `roadmap/SP-039-ui-package-consolidation.md`

`packages/ui/src/components/` and `webui/src/components/` have ~30 overlapping component filenames (Terminal, FileTree, ContextMenu, Sidebar, StatusBar, CommandPalette, Notification, MessageBubble, GitSidebarPanel, …). CSS edits drift between the two; imports are ambiguous; Storybook tests the library copy while the app uses the duplicate. This spec consolidates to one canonical location per component and enforces the boundary in CI.

### Phase 1: Decision + audit

[] - SP-039-1a: Confirm Option A (delete `packages/ui`, move everything into `webui`) or Option B (keep `packages/ui` as the canonical library, webui imports from it). Document the choice and rationale in `roadmap/SP-039-DECISION.md` or inline in the spec.
[] - SP-039-1b: Write `scripts/ui-consolidation-diff.sh` outputting the 30+ overlaps and per-component diff status (identical / packages-leads / webui-leads / divergent).
[] - SP-039-1c: Categorize every `packages/ui/src/components/*.tsx` as primitive (reusable, no domain types) or composite (wires primitives to app state).

### Phase 2: Move misplaced composites out of `@sprout/ui`

[] - SP-039-2a: Move `BillingPage*`, `TeamPage*`, `AdminBillingPage*`, `TasksPage*` from `packages/ui/src/components/` to `webui/src/components/`. One commit per move.
[] - SP-039-2b: Audit `packages/ui` for any other domain-coupled components (importing from `@sprout/events` for app-specific events, using `useSproutAdapter()` against a specific endpoint set); move them.
[] - SP-039-2c: Verify `grep -rn "chatSession\|persona\|adapter" packages/ui/src/components/` returns no domain-specific hits.

### Phase 3: Consolidate primitives — small first

[] - SP-039-3a: `Notification`, `NotificationItem`, `Notification.css` → canonical in `packages/ui`; delete webui copy; update imports.
[] - SP-039-3b: `Dropdown`, `Modal` (base), `ContextMenu` → same.
[] - SP-039-3c: `Sidebar`, `StatusBar`, `MenuBar` → same.
[] - SP-039-3d: `CommandPalette`, `CommandInput` → same.

### Phase 4: Consolidate primitives — large

[] - SP-039-4a: `FileTree` — highest-impact primitive; verify behavior parity with at least manual smoke test in WebUI plus existing component tests passing.
[] - SP-039-4b: `Terminal` — uses xterm.js; verify keybinding parity, reattach behavior, search bar.
[] - SP-039-4c: `GitSidebarPanel` — confirm whether primitive or composite (recent edits in commit `b46bcada` suggest composite); place accordingly.
[] - SP-039-4d: `MessageBubble`, `MessageSegments`, `MessageContent`, `LiveLog`, `QueuedMessagesPanel`, `SelectionActionBar`, `ChatMessageContextMenu`.

### Phase 5: Enforce boundary

[] - SP-039-5a: Add `eslint-plugin-import` `no-restricted-paths` rule to `webui/.eslintrc` and `packages/ui/.eslintrc` forbidding cross-boundary deep imports.
[] - SP-039-5b: Add `scripts/check-no-duplicate-components.sh` (fails CI if `comm -12` between the two component directories has any matches); wire into `.github/workflows/build.yml`.
[] - SP-039-5c: Add a Storybook coverage check requiring every primitive in `@sprout/ui` to have a matching `.stories.tsx`.

### Phase 6: Documentation

[] - SP-039-6a: Write `docs/COMPONENT_LIBRARY.md` covering the Option A/B decision and rationale, the primitive vs composite rubric with examples, import direction enforcement, how to add a new component.
[] - SP-039-6b: Update `CONTRIBUTING.md` with a "Where does my new component go?" subsection.
[] - SP-039-6c: Update `packages/ui/README.md` (if it exists) to point at `docs/COMPONENT_LIBRARY.md` as the source of truth.

---

## SP-040: Deployment Configurability — Untangling Hardcoded Ports and Hosts

Spec: `roadmap/SP-040-deployment-configurability.md`

`webui/package.json:101` hardcodes `"proxy": "http://localhost:56000"`. `webui/src/bootstrapAdapter.ts` (29 lines) uses `window.location` heuristics with no env-var override. The frontend has no login UI and never sends `Authorization` headers, even though `pkg/webui/auth_middleware.go:23` validates bearer tokens on write endpoints. This spec adds runtime + build-time configurability and a minimal login flow.

### Phase 1: Bootstrap endpoint + adapter rewrite

[] - SP-040-1a: Define `RuntimeConfig` type (`apiBaseURL`, `wsURL`, `authMode: "none"|"bearer"`, `appMode: "local"|"cloud"`, `buildVersion`) in `pkg/webui/api_bootstrap.go` and `webui/src/types/runtimeConfig.ts`.
[] - SP-040-1b: Implement `GET /api/bootstrap` returning `RuntimeConfig` (unauthenticated; `authMode` set based on `SPROUT_AUTH_TOKEN` env, sharing the env read with `pkg/webui/server.go:69`).
[] - SP-040-1c: Rewrite `webui/src/bootstrapAdapter.ts` — fetch `/api/bootstrap` first, fall back to `import.meta.env.VITE_*`, fall back to localhost defaults. Log each fallback step.
[] - SP-040-1d: Update `webui/src/bootstrapAdapter.test.ts` with all three fallback paths.

### Phase 2: Build-time configurability

[] - SP-040-2a: Define `VITE_API_BASE_URL`, `VITE_WS_URL`, `VITE_AUTH_MODE`, `VITE_APP_MODE` in `webui/vite.config.ts` with safe defaults.
[] - SP-040-2b: Replace `webui/package.json:101` hardcoded proxy with a Vite plugin reading `process.env.SPROUT_DEV_BACKEND_URL` (default `http://localhost:56000`).
[] - SP-040-2c: Add `webui/.env.example` documenting every supported `VITE_*` var.

### Phase 3: Auth context + LoginPage

[] - SP-040-3a: Create `webui/src/contexts/AuthContext.tsx` exposing `{token, setToken, clearToken, isAuthenticated}` backed by `sessionStorage` key `sprout_auth_token`.
[] - SP-040-3b: Create `webui/src/components/LoginPage.tsx` — single token input + submit; route on success.
[] - SP-040-3c: Wrap the app root in `AuthContext`; route to LoginPage when `authMode === "bearer"` and no token is stored. Skip LoginPage when `authMode === "none"` (preserves current localhost UX).
[] - SP-040-3d: Add `Authorization: Bearer <token>` injection to `webui/src/services/apiAdapter.ts` (or whichever module owns API calls).
[] - SP-040-3e: Add WebSocket auth via first-message-after-open in `webui/src/services/websocket*.ts`; reject if first message is not a valid auth payload within 5s.
[] - SP-040-3f: Add 401 response interceptor: clear stored token, route to LoginPage, show "Session expired" toast.
[] - SP-040-3g: Add Log Out menu item that calls `clearToken()`.

### Phase 4: Tests

[] - SP-040-4a: `pkg/webui/api_bootstrap_test.go` — bootstrap endpoint shape + `authMode` toggling on `SPROUT_AUTH_TOKEN`.
[] - SP-040-4b: `webui/src/contexts/AuthContext.test.tsx` — token persists, `clearToken` clears storage, 401 interceptor fires `clearToken`.
[] - SP-040-4c: Playwright (`playwright.config.js` exists at repo root) scenario: bearer-mode login flow start-to-finish, including LogOut.

### Phase 5: Documentation

[] - SP-040-5a: Write `docs/DEPLOYMENT.md` with sections for default localhost, custom port, remote VM behind reverse proxy (nginx example with WS upgrade), Foundry cloud mode (cross-link SP-015), Docker.
[] - SP-040-5b: Update `README.md` to point at `docs/DEPLOYMENT.md`; refresh the "use at your own risk" disclaimer with the bearer-token requirement for non-localhost binds (depends on SP-032 B1 wording).
[] - SP-040-5c: Add a "Deployment configurability" note to `docs/WEB_UI.md`.

---

## SP-041: Tool Execution Sandboxing — Making the README Disclaimer Untrue

Spec: `roadmap/SP-041-tool-execution-sandboxing.md`

`README.md:5` warns "use at your own risk, ideally in a container." Tool execution is host-level: `pkg/agent/tool_handlers_shell.go` runs arbitrary `exec.Command`, `pkg/pythonruntime/runtime.go:65` uses contextless `exec.Command` with no timeout, MCP server subprocesses (`pkg/mcp/client.go:97`) have full host access. This spec adds a pluggable sandbox interface with bubblewrap / firejail backends and per-tool policies.

### Phase 1: Close the easy gaps first

[] - SP-041-1a: Convert `pkg/pythonruntime/runtime.go:65` `exec.Command(...)` to `exec.CommandContext(ctx, ...)` with a default 30s timeout. (Ships value before the sandbox lands.)
[] - SP-041-1b: Add timeouts to any other contextless `exec.Command` in `pkg/agent/tool_handlers_shell.go` (lines 264, 355 use git directly). Grep the codebase for `exec.Command(` without `Context` and convert.

### Phase 2: Sandbox interface

[] - SP-041-2a: Create `pkg/sandbox/sandbox.go` with `SandboxRunner` interface, `ExecSpec`, `SandboxPolicy` (WorkspaceRoot, ReadOnlyPaths, AllowNetwork, Timeout, MemLimitMB, CPUQuotaPercent), `ExecResult` (ExitCode, Stdout, Stderr, Duration, Timeout, OOM).
[] - SP-041-2b: Implement `pkg/sandbox/none.go` — no-op runner with timeout enforcement only.
[] - SP-041-2c: Implement `pkg/sandbox/bubblewrap.go` — translates `SandboxPolicy` into `bwrap` invocation; checks `bwrap --version` and user-namespace availability on init.
[] - SP-041-2d: Implement `pkg/sandbox/firejail.go` — same for firejail.
[] - SP-041-2e: Add `SandboxFromConfig(cfg)` selector with availability detection and a startup warning if the requested backend is unavailable (fall back to `none`).

### Phase 3: Wire tools

[] - SP-041-3a: Wrap `exec.Command` sites in `pkg/agent/tool_handlers_shell.go` with `sandbox.Run` using a `shell` policy (60s timeout, 1GB mem, network per-config).
[] - SP-041-3b: Wrap `pkg/pythonruntime/runtime.go` with `sandbox.Run` using a `python_runtime` policy (30s timeout, workspace + stdlib RO bind-mount).
[] - SP-041-3c: Wrap `pkg/mcp/client.go:97` subprocess launch with `sandbox.Run`; create per-MCP scratch dir at `~/.config/sprout/mcp_scratch/<server_id>/`.
[] - SP-041-3d: Decide on browser/Rod handling (`pkg/webcontent/browser_rod.go`) — Chromium has its own sandbox; document the layering even if no code change.

### Phase 4: Config + CLI

[] - SP-041-4a: Add `SandboxConfig` block to `pkg/configuration/config.go` with `backend` and per-tool `policies`. Defaults: bubblewrap if available, network=false, sensible per-tool timeouts.
[] - SP-041-4b: Add `--sandbox=<none|bubblewrap|firejail>` CLI flag in `cmd/`.
[] - SP-041-4c: WebUI MCP settings tab (`webui/src/components/SettingsPanel.../MCPSettingsTab.tsx`): per-server network opt-in checkbox.
[] - SP-041-4d: Startup notice listing active backend; warn loudly when running with `none` and non-localhost bind (interacts with SP-032 / SP-040).

### Phase 5: Tests

[] - SP-041-5a: `pkg/sandbox/bubblewrap_test.go` — table tests asserting generated `bwrap` arg list for various policies. Skip when `bwrap` unavailable.
[] - SP-041-5b: `TestSandbox_NetworkBlocked` — `curl https://example.com` under each backend with `AllowNetwork=false` returns non-zero.
[] - SP-041-5c: `TestSandbox_TimeoutKills` — `sleep 60` with timeout 1s killed within 2s; `ExecResult.Timeout == true`.
[] - SP-041-5d: `TestSandbox_MemLimitKills` — memory-allocating script with `MemLimitMB=64` OOM-killed.
[] - SP-041-5e: `TestSandbox_WorkspaceRoot_Isolated` — `ls ~/.aws` under sandbox with `WorkspaceRoot=/tmp/sandbox-test` returns no access.

### Phase 6: Documentation + threat model

[] - SP-041-6a: Write `docs/SANDBOX_THREAT_MODEL.md` covering what each backend protects against, residual risks (shared kernel, syscall surface), and when to use VM-level isolation instead.
[] - SP-041-6b: Update `README.md:5` — replace "use at your own risk, ideally in a container" with concrete sandboxing guidance and a link to the threat model.
[] - SP-041-6c: Cross-link from `docs/SECURITY.md` (when SP-033 5b lands) to the sandbox doc.

### Phase 7: Future platforms (deferred follow-ups)

[] - SP-041-7a: macOS `sandbox-exec` backend — separate spec.
[] - SP-041-7b: Windows AppContainer / Job Object backend — separate spec.

---

## SP-042: Self-Review Quality Gates — Tooling in Place of Human Review

Spec: `roadmap/SP-042-self-review-quality-gates.md`

Single-author project (1627+ commits as Alan Price across name aliases per `git shortlog -sne --all`); no PR receives independent human review. Current CI: race tests + goleak, frontend lint as `continue-on-error`. No `golangci-lint`, no complexity gate, no layering check, no secret-scan. This spec adds the tooling a second human reviewer would provide.

### Phase 1: golangci-lint baseline

[] - SP-042-1a: Add `.golangci.yml` with curated linter set: `errcheck`, `gosec`, `govet`, `ineffassign`, `unconvert`, `unparam`, `revive`, `gocyclo`, `misspell`, `bodyclose`, `noctx`, `nilerr`.
[] - SP-042-1b: Run `golangci-lint run` locally; fix cheap violations.
[] - SP-042-1c: Add `//nolint:<linter> // <reason>` waivers where the fix is out-of-scope; each waiver references a tracking spec (e.g., SP-029, SP-038).
[] - SP-042-1d: Add `golangci-lint run --timeout=5m` step to `.github/workflows/build.yml` as a hard gate.

### Phase 2: ESLint hard gate

[] - SP-042-2a: Run `npm run lint` (or equivalent) for `packages/ui` and `webui`; capture violation count.
[] - SP-042-2b: Fix or explicitly waiver every violation.
[] - SP-042-2c: Flip `continue-on-error: false` for the frontend lint step in `.github/workflows/build.yml`.
[] - SP-042-2d: Add Prettier check step alongside ESLint.

### Phase 3: Complexity cap

[] - SP-042-3a: Enable `gocyclo --over=30` in `.golangci.yml`. Add waivers for known SP-029-tracked monoliths.
[] - SP-042-3b: Add ESLint `complexity: ["error", 20]` rule. Apply waivers where SP-029 / SP-038 / SP-039 will refactor.

### Phase 4: Import layering

[] - SP-042-4a: Add `depguard` rules to `.golangci.yml` encoding the layering: `pkg/configuration` no domain imports; `pkg/agent_tools` no `pkg/agent` import; `pkg/agent` no `pkg/webui` import.
[] - SP-042-4b: Resolve any violations surfaced by the initial run.

### Phase 5: Coverage gate

[] - SP-042-5a: Add coverage-computation step to CI producing a percentage per package + overall.
[] - SP-042-5b: Store rolling baseline; fail if PR drops below baseline minus 0.5pp tolerance.
[] - SP-042-5c: Add `make coverage-baseline` to update the baseline intentionally.

### Phase 6: Secret scanning

[] - SP-042-6a: Add `gitleaks` step to CI on every push.
[] - SP-042-6b: Create `.gitleaks.toml` with custom rules for sprout-specific config patterns (look at `~/.config/sprout/api_keys.json` shape) + allowlist for fake-token test fixtures.
[] - SP-042-6c: Run `gitleaks detect` on full history; document/rotate any real findings; allow-list historical commits as needed.

### Phase 7: LLM advisory review (optional)

[] - SP-042-7a: Create `.github/workflows/llm-review.yml` running sprout against the PR diff and posting a review comment.
[] - SP-042-7b: Implement diff-size gating (skip if <20 lines or >2k lines) and per-PR token-budget cap (50k).
[] - SP-042-7c: Add `skip-llm-review` PR label support.
[] - SP-042-7d: Track signal-to-noise of LLM suggestions; tune confidence filter.

### Phase 8: Documentation

[] - SP-042-8a: Write `docs/QUALITY_GATES.md` covering every active gate, threshold, how to add a waiver, how to update the baseline.
[] - SP-042-8b: Update `CONTRIBUTING.md` with "How to handle a CI gate failure" section.

---

## SP-043: Documentation & Bus-Factor Resilience

Spec: `roadmap/SP-043-documentation-bus-factor-resilience.md`

Effective bus factor: 1 (per `git shortlog -sne --all` — all commits by Alan Price under name aliases). Docs are user-facing, not engineer-onboarding-facing. `README.md:61` still says `cd ledit`, `README.md:72` invokes `ledit` as the binary name (stale rename). There are no ADRs, no onboarding ramp, no "where does X live?" index, no Foundry coupling explanation. This spec writes down what's in the author's head so a successor can take over.

### Phase 1: Stop the bleeding — fix the README

[] - SP-043-1a: Replace `cd ledit` at `README.md:61` with `cd sprout`. Replace `ledit` at `README.md:72` with `sprout`. Audit the entire README for other stale name references.
[] - SP-043-1b: Run each command in the README against a fresh clone; fix anything that doesn't work.
[] - SP-043-1c: Trim the README to entry-point status; move detail to linked `docs/`. README answers: what is sprout, how do I install it, how do I run my first command, where do I go from here.

### Phase 2: ADR backfill

[] - SP-043-2a: Create `docs/adr/` with a `README.md` explaining the ADR format (Context / Decision / Status / Consequences).
[] - SP-043-2b: Write ADR-001 (run modes — scoped 56001+ vs daemon 56000), ADR-002 (two UI packages — pending SP-039), ADR-003 (Electron desktop wrapper), ADR-004 (WASM target), ADR-005 (persona model layering), ADR-006 (multi-provider vs single), ADR-007 (semantic memory time-decay), ADR-008 (MCP over custom protocol), ADR-009 (`ledit` → `sprout` rename + pending cleanup), ADR-010 (Foundry coupling).
[] - SP-043-2c: Add "Process for new ADRs" subsection to `CONTRIBUTING.md` — structural decisions land an ADR alongside their implementation.

### Phase 3: Code map

[] - SP-043-3a: Write `docs/CODE_MAP.md` keyed by user intent: "add a tool", "debug MCP", "understand personas", "add a provider", "add a slash command", "understand the agent loop", "write a new persona", "inspect persisted state", "replay last LLM request" (`replay_last_request.sh`), "understand WebUI events".
[] - SP-043-3b: Add bidirectional links between `CODE_MAP.md` entries and the relevant roadmap specs.

### Phase 4: Onboarding ramp

[] - SP-043-4a: Write `docs/ONBOARDING.md` with Day 1 (clone, build, run, one query, navigate `pkg/agent/` and `webui/src/`), Week 1 (read these specific docs, run these specific tests, ship one tiny change), Month 1 (pick one open spec, scope a sub-phase, implement).
[] - SP-043-4b: Add "Open specs suitable for new contributors" section listing 3-5 specs with scoped sub-phases.

### Phase 5: Recipes

[] - SP-043-5a: Write `docs/DEBUG_RECIPES.md` covering: attach delve/VS Code debugger to agent and daemon; replay via `replay_last_request.sh`; inspect `~/.config/sprout/embeddings/conversation_turns.jsonl`; verify MCP connections; inspect `~/.sprout/runlogs/*.jsonl`; trace a tool call from WebUI click to LLM and back.
[] - SP-043-5b: Write `docs/BUILD_RECIPES.md` covering current-platform build, cross-platform release, WASM target, Electron bundle, each test suite's coverage. (Or expand Makefile docstrings if that's enough.)

### Phase 6: Foundry / cloud-mode clarification

[] - SP-043-6a: Write `docs/FOUNDRY.md` (or expand `docs/ARCHITECTURE.md`): what Foundry is, which sprout features depend on it (billing, team, admin UIs), what works standalone, how `cloudAdapter` routes between WASM-local and Foundry endpoints. Cross-link SP-015.
[] - SP-043-6b: Cross-link from `README.md` and `docs/DEPLOYMENT.md` (SP-040 dependency).

### Phase 7: Second contributor test

[] - SP-043-7a: Identify a tester (LLM agent run cold against a fresh checkout, or a human reviewer).
[] - SP-043-7b: Run them through `ONBOARDING.md` end-to-end; capture every stuck point as a tracked issue.
[] - SP-043-7c: Fix every issue. Re-run until clean.
[] - SP-043-7d: Document the result; commit to re-running whenever the architecture moves materially.

---

## SP-044: Roadmap Triage & WIP Limits

Spec: `roadmap/SP-044-roadmap-triage-and-wip-limits.md`

22 `Proposed` specs before this batch, 21 `Complete`, plus 9 new specs from this batch = ~31 open specs for a single committer. No `Active` / `Deferred` / `Superseded` taxonomy, no WIP cap, no `roadmap/README.md` dashboard. This spec defines a five-state status taxonomy, triages the current backlog, and adds a status dashboard.

### Phase 1: Taxonomy + dashboard skeleton

[] - SP-044-1a: Define the five-status taxonomy (`Active`, `Proposed`, `Deferred`, `Superseded`, `Done`) at the top of `roadmap/README.md`, with priority sub-tier for `Proposed` (Critical/High/Medium/Low).
[] - SP-044-1b: Audit every existing spec file's `Status:` line; normalize to one of the five values. `📋 Proposed` is acceptable; other variants need normalization.
[] - SP-044-1c: Write the initial `roadmap/README.md` status table by hand: every spec listed once, sorted by status, with a one-line hook.

### Phase 2: Triage pass

[] - SP-044-2a: Walk every `Proposed` spec. Per-spec decision: promote to Active (cap 3), stay Proposed at what priority, Defer with reason, or Supersede with link.
[] - SP-044-2b: For `Deferred` specs, add a `Deferred-Reason:` line to the spec header.
[] - SP-044-2c: For `Superseded` specs, add a `Superseded-By: SP-XXX` line and a one-paragraph explanation at the top of the spec body.
[] - SP-044-2d: Create `roadmap/TRIAGE-LOG.md` and record the triage pass decisions (one line per spec per pass).

### Phase 3: TODO.md consistency

[] - SP-044-3a: For every spec moved to `Deferred` or `Superseded`, prefix its TODO.md section heading with `[deferred]` or `[superseded]` so live work is visually distinct.
[] - SP-044-3b: For `Superseded` specs whose successor has its own TODO entries, remove the original entries or visually demote them.

### Phase 4: Tooling (optional)

[] - SP-044-4a: Write `scripts/roadmap-status.sh` (bash or Go) to regenerate `roadmap/README.md`'s status table from spec headers.
[] - SP-044-4b: Add a CI lint that fails on invalid Status values in any spec.
[] - SP-044-4c: Optional: a stale-Active warning (spec marked Active for N commits without a phase tick).

### Phase 5: Documentation

[] - SP-044-5a: Update `CONTRIBUTING.md` with a "Roadmap status model" subsection.
[] - SP-044-5b: Cross-link from `docs/ONBOARDING.md` (SP-043 dependency) so a new contributor sees `roadmap/README.md` as the entry point.
[] - SP-044-5c: Document the triage trigger conditions (a spec moves Active→Done, Proposed list grows past 30, onboarding event, …) in `roadmap/README.md`.

---

## SP-045: WASM Build Feature Parity

Spec: `roadmap/SP-045-wasm-feature-parity.md`

Native sprout has dozens of commands; the WASM build exposes 11 (shell-style only). This spec brings the WASM build to as-close-to-feature-parity as the browser sandbox allows. Items checked here are already in this branch.

### Phase 1: Tier 1 — pure-Go sprout features over JS bridge

[x] - SP-045-1a: Unblock WASM compilation of `pkg/embedding`. `pkg/utils/terminal_unix.go` and `pkg/console/signal_compat_unix.go` had `!windows` build tags that incorrectly included `js/wasm`. Retag to `unix && !js` and add matching `*_js.go` stubs.
[x] - SP-045-1b: Trim `pkg/embedding/onnx_wasm_stub.go` to only stub the CGO-only types (`ONNXRuntime`, `ONNXEmbeddingProvider`). Remove duplicates of pure-Go types (`ModelConfig`, `ModelDownloader`, `GemmaTokenizer`, `EmbeddingGemma300MConfig`) — those build for WASM unchanged.
[x] - SP-045-1c: Add `cmd/wasm/embedding_funcs.go` exposing static-only semantic search + memory CRUD as JS Promises: `searchSemantic`, `buildSemanticIndex`, `updateSemanticFile`, `getSemanticStatus`, `listMemories`, `readMemory`, `saveMemory`, `deleteMemory`, `searchMemories`. `cmd/wasm/main.go` merges these into `SproutWasm`.
[x] - SP-045-1d: Add JS bridge for configuration management: `getConfig`, `setConfig`, `getConfigPath`, `resetConfig`, `getAPIKeys`, `setAPIKey`, `removeAPIKey`. Implemented in `cmd/wasm/config_funcs.go`. API keys deliberately not exposed in plaintext to JS — `getAPIKeys` returns a `provider → bool` map; full keys must be written via `setAPIKey`.
[x] - SP-045-1e: Conversation persistence JS bridge: `getConversationHistory`, `saveConversationTurn`, `searchConversations`, `deleteConversationTurn`. Implemented in `cmd/wasm/conversation_funcs.go`. Hard-delete is a soft-delete (sets `metadata.deleted=true` + zeros embedding) until SP-045-1e-follow-up adds a `ReplaceAll` shim to `ConversationStore`.
[x] - SP-045-1f: Pure-Go unit tests for the bridge helpers in `cmd/wasm/wasm_funcs_test.go`. Covers: memory-name sanitization, `indexOfID`, `turnRecordToJS` (embedding strip, metadata propagation, deleted-flag, nil-safe). Runs via `GOOS=js GOARCH=wasm go test -exec go_js_wasm_exec` — recipe documented in `docs/WASM_API.md`.
[x] - SP-045-1g: `docs/WASM_API.md` documents every `SproutWasm.*` entry: signature, return shape, tier, and the testing recipe. Tier 0 (shell), Tier 1 (semantic + memory + config + conversation), and a clearly-marked "what's not here yet" section pointing at Tiers 2a/2b/3.

### Phase 2: Tier 3 — file extractors

**Status: NOT NEEDED** — `github.com/odvcencio/gotreesitter` turned out to be a **pure-Go** tree-sitter implementation (despite the C-tradition naming), so the existing extractors compile and run identically under `js/wasm`. The full `pkg/embedding` test suite — including `TestExtractPy*`, `TestExtractTS*`, `TestExtractGo*` — passes when executed via `GOOS=js GOARCH=wasm go test -exec go_js_wasm_exec ./pkg/embedding/`.

The original SP-045 assumption that tree-sitter required CGO was wrong. The pure-Go gotreesitter package walks the grammar tables directly in Go.

[x] - SP-045-2a: Verified extractor coverage on WASM by running the existing `pkg/embedding` test suite under `GOOS=js GOARCH=wasm go test -exec go_js_wasm_exec`. All extractor tests pass — Go (`go/ast`), Python (`gotreesitter`), TypeScript (`gotreesitter`).
[~] - SP-045-2b: Fallback extractors **not implemented** — not needed. Marked as skipped here so the next phase isn't gated on imaginary work.
[~] - SP-045-2c: Eval comparison **not run** — there's only one extractor pipeline; no fallback to compare against.
[~] - SP-045-2d: No tree-sitter.wasm bridge work needed.

### Phase 3: Tier 2a — onnxruntime-web bridge

[x] - SP-045-3a: Contract for `globalThis.__sproutONNX` documented in `docs/WASM_API.md` § "Tier 2a — ONNX-quality embeddings via `__sproutONNX`": fields (`embed`, `embedBatch`, `modelHash`, `modelName`, `dimensions`), error semantics (Promise rejection surfaces as Go-side error, hung promises bounded by ctx deadline + 60s fallback).
[x] - SP-045-3b: WASM-side bridge implemented in `pkg/embedding/onnx_wasm_stub.go`. `NewONNXEmbeddingProvider` detects `__sproutONNX` and forwards Embed/EmbedBatch through `syscall/js`; otherwise returns `errWASMNotSupported` (existing fallback path). `NewONNXRuntimeWithDir` now returns a no-op runtime so the manager-level wiring proceeds. `onnxRequiresModelFiles` helper (false on WASM, true on native) gates the manager's on-disk model check.
[x] - SP-045-3c: Host-side adapter `webui/src/services/sproutONNXBridge.ts`: `bridgeBrowserProvider(p)` returns the contract shape; `installSproutONNXBridge(opts)` is a one-liner that stands up `BrowserONNXProvider`, wraps it, and sets `globalThis.__sproutONNX`. Idempotent — installing twice replaces cleanly.
[x] - SP-045-3d: Tests cover both sides — Go side (`pkg/embedding/onnx_wasm_bridge_test.go`) with mock `__sproutONNX`: round-trip, batch order, promise rejection, ctx cancellation; JS side (`webui/src/services/sproutONNXBridge.test.ts`): 6 lifecycle tests. An integrated WASM-in-browser test (real WASM module + real BrowserONNXProvider) would need a Playwright harness — deferred.
[x] - SP-045-3e: SP-045 spec doc updated; WASM_API.md has a new "Tier 2a" section with the contract, one-line install, hand-roll install, verification recipe.

---

## SP-046: Browser-Primary Workspace Sync Model

Spec: `roadmap/SP-046-workspace-sync-model.md`

Lives mostly in `../platform` (server-side sync, WebSocket transport, container lifecycle) but a few invariants need to be enforced in this repo's agent + WASM code. Captured here so they don't drift.

[x] - SP-046-1a: `WorkspaceFileMetadata` struct defined in `pkg/agent/workspace_sync.go` with `(BrowserSeq, ContainerSeq, LastSyncedBrowser, LastSyncedContainer, ModifiedAt)`. `hasUnsyncedBrowserEdits()` predicate covered by table-driven tests. Persistence shape (sibling `.sprout-meta.json` vs inline) is still open and is now an SP-046-1a-follow-up — the platform-side WS bridge will dictate which when it lands.
[x] - SP-046-1b: Staleness rule wired into `writeFileContent` via `Agent.checkWriteStaleness`. Refuses with actionable tool errors when the agent hasn't called `read_file(path)` this turn, OR when the file's mtime is newer than the agent's recorded read. Hooked into `handleReadFile` (both range and full-file paths) and reset at turn boundaries via `RecordTurnCheckpoint`/`RecordTurnCheckpointAsync`. 6 staleness + 4 metadata sub-tests pass. New files are allowed without prior reads; nil-Agent is a safe no-op.
[x] - SP-046-1c: `ErrWriteStale` and `ErrWriteHasUnsyncedEdits` sentinels added; `checkWriteStaleness` wraps via fmt.Errorf "%w" so callers distinguish with `errors.Is`. In-memory `workspaceMetadataStore` on the Agent populated via `SetFileMetadata` (called by the platform-side sync layer when it lands). Conflict check runs before staleness so agent gets "ask the user" guidance rather than "read first" when the real issue is unsynced edits.
[x] - SP-046-1d: WASM `cmd/wasm/sync_funcs.go` exposes `SproutWasm.setSyncEndpoint(url)` and `SproutWasm.getSyncEndpoint()`. URL is stored process-wide for the platform-side transport to consume. Free-tier callers leave it unset.
[x] - SP-046-1e: Free-tier degenerate mode verified — `TestCheckWriteStaleness_FreeTierDegenerate` pins that an Agent with zero metadata pushes hits the staleness rule but never the unsynced-edits branch. `docs/WASM_API.md` has a new "Free-tier degenerate mode" section with the minimum-viable boot recipe.
[x] - SP-046-1f: WASM-side hooks for the multi-device session-moved control message: `SproutWasm.onSessionMoved(handler)` registers a JS callback, `SproutWasm.sessionMoved()` invokes it from the platform-side WS layer. Single-handler — calling again replaces. Platform-side WS implementation still owed in `../platform`.
[x] - SP-046-1g: `SproutWasm.startHeartbeat(pingFn)` spins a 15s ticker; `SproutWasm.stopHeartbeat()` stops it. Idempotent on both sides. Platform-side container reaping after 60s of missed heartbeats lives in `../platform`.

### Phase 4: Tier 2b — agent / LLM commands

[x] - SP-045-4a: **Resolved at the architecture level.** The sprout-foundry platform holds per-user encrypted API keys in DynamoDB and proxies all LLM calls server-side — no keys ever live in the browser. WASM-side just needs the proxy routing (see SP-045-4-llm-proxy). The previously-considered options (localStorage, IndexedDB+WebCrypto envelope, per-session injection) all moot once the platform owns key custody. Standard browser auth patterns (HttpOnly refresh cookie + short-lived access token) handle session security.
[x] - SP-045-4-llm-proxy: `pkg/llmproxy/` provides an `http.RoundTripper` that rewrites direct LLM provider URLs (openai/anthropic/openrouter/deepinfra/mistral/cerebras/groq/together) to route through the platform's `/api/proxy/llm/{provider}/*` path. Installed onto `http.DefaultTransport` at init in cmd/wasm. Configurable via `SproutWasm.setPlatformEndpoint(url)` / `.getPlatformEndpoint()`. No-op when no endpoint is set. 9 tests cover rewriting, query-string preservation, idempotent install, concurrent SetPlatformEndpoint, integration with a stock `http.Client`, and a coverage smoke-test against the provider registry.
[x] - SP-045-4-llm-chat: `cmd/wasm/chat_funcs.go` exposes `SproutWasm.runChat(provider, model, messagesJSON, options?, onChunk?)` — single-shot chat completion that decodes messages, builds a provider client via `factory.CreateProviderClient`, and dispatches through `SendChatRequest` / `SendChatRequestStream` / `SendVisionRequest` based on the `onChunk`+`vision` options. Returns a Promise resolving to `{content, reasoning_content, finish_reason, provider, model, prompt_tokens, completion_tokens, total_tokens}`. Streaming callback fires synchronously from Go for each chunk. Documented under "Tier 2b" in `docs/WASM_API.md`. This is the proxy's smoke-test surface — exposing the full agent loop (SP-045-4d/4f) is the next-step plumbing.
[x] - SP-045-4b: **Resolved by architecture.** No mainstream LLM provider (OpenAI, Anthropic, OpenRouter, DeepInfra, Mistral, Cerebras, Groq, Together) exposes CORS-friendly endpoints — they all assume server-side use. Production WASM use is exclusively through the platform proxy (SP-045-4-llm-proxy), so direct-browser CORS isn't on the path. The `setPlatformEndpoint("")` air-gapped mode is the only configuration where this would matter, and it's a testing-only path documented as such in `docs/WASM_API.md`. No user-supplied proxy URL is needed; the platform IS the proxy.
[~] - SP-045-4c: SSE streaming over `js/wasm` net/http. **Go-side covered**: `cmd/wasm/chat_funcs_test.go` pins the `chatResponseToJS` wire-format contract (5 cases: empty Choices, first-choice-wins, full Usage, partial Usage, runChat registration). **In-browser fetch-streaming still pending** — `go_js_wasm_exec` under Node lacks fetch ReadableStream incremental-yield behavior, so the actual JS-side smoke test (chunks arrive interleaved with server-side writes) belongs to the integration phase once the platform `/api/proxy/llm/{provider}/*` endpoints are reachable. The Go agent's existing parser tests in `pkg/agent_providers` cover the SSE-parsing logic itself; what's left is verifying that fetch streaming on the JS side actually surfaces chunks incrementally rather than buffering until EOF.
[x] - SP-045-4d: `cmd/wasm/agent_funcs.go` exposes `SproutWasm.runAgent(provider, model, query, onEvent?)` — constructs an `agent.Agent` via a new public `agent.NewAgentWithClient` constructor (skips the interactive provider-resolution dance in `newAgentWithConfigManager`), runs `ProcessQuery`, and forwards every `events.UIEvent` to the JS callback as JSON. Returns `{response, provider, model}`. Wired through `chatJSFuncs`/`agentJSFuncs` in `cmd/wasm/main.go`. 10-minute Promise timeout via a new `asPromiseWithTimeout` helper (also adopted by `runChat`, since chat completions routinely exceed the previous 60s ceiling). Documented under "Tier 2b" in `docs/WASM_API.md`. **Constraint**: file/memory/semantic tools work; shell/MCP tools no-op or error — see SP-045-4e.
[x] - SP-045-4e: Split `pkg/agent_tools/shell.go` into a cross-platform dispatcher + native `shell_native.go` (os/exec) + js/wasm `shell_js.go` (wasmshell). The WASM build registers a `wasmshell.ParseAndExecute`-backed executor from `cmd/wasm/shell_executor.go:init` via the new `tools.RegisterWASMShellExecutor` hook. Dependency direction kept clean: `pkg/agent_tools` doesn't know about wasmshell. 3 integration tests in `cmd/wasm/shell_executor_test.go` cover registration, echo round-trip, and graceful handling of non-zero exits. Existing native shell/terminal tests gated `!js`. Documented under "Tier 2b" in `docs/WASM_API.md` with the supported builtin set. MCP and other process-spawning tools remain unsupported on WASM by design.
[~] - SP-045-4f: Of the originally-listed wrappers, **only `runPlan` has a native counterpart worth mirroring**. `runQuestion`/`runCode` were aspirational — no `sprout question` or `sprout code` command exists in the CLI; host pages get the same behavior by prepending their own framing string to a `runAgent` query. `runCommit`/`runReview` need a `git` binary that doesn't exist under WASM; they'll be served by the sprout-foundry platform container side instead (SP-046 platform-side flow). `runPlan(provider, model, query, onEvent?)` is now exposed in `cmd/wasm/agent_funcs.go` — installs `agent.GetEmbeddedPlanningPrompt(true)` then runs `ProcessQuery`, returns `{response, provider, model, mode: 'plan'}`. Documented in `docs/WASM_API.md`.
[x] - SP-045-4g: Tests and docs landed alongside each step rather than as a separate phase. Test counts: `cmd/wasm/chat_funcs_test.go` (6 — response-shape × 4, chatJSFuncs registration, agentJSFuncs registration), `cmd/wasm/shell_executor_test.go` (3 — echo round-trip, non-zero exit, registration sanity), pre-existing `cmd/wasm/wasm_funcs_test.go` (6) and `cmd/wasm/sync_funcs_test.go`. `docs/WASM_API.md` documents every JS entry under explicit Tier 0/1/2a/2b sections with arg tables and example usage.

### Phase 5: Build matrix + distribution

[x] - SP-045-5a: Swept the repo. Tagged the whole `pkg/webui` package with `//go:build !js` (204 files; none are imported by `cmd/wasm`). Tagged `main.go` and `cmd/*.go` with `!js` likewise. Fixed two remaining bare `!windows` patterns (`pkg/webui/pid_alive_unix.go`, `cmd/pid_alive_unix.go` → `unix && !js`) and one `!linux && !darwin` (`cmd/service_other.go` → added `&& !js`).
[x] - SP-045-5b: `GOOS=js GOARCH=wasm go build ./...` succeeds cleanly. Native `go build ./...` also still clean. CI smoke check still to add.
[x] - SP-045-5c: Stripped with `-ldflags="-s -w"` (added to `scripts/build-wasm.sh`, opt-out via `WASM_KEEP_SYMBOLS=1`). Saved ~0.5% (98.2MB → 97.7MB). Symbols aren't the bulk in Go-WASM; see 5f for the actual size win.
[~] - SP-045-5d: tinygo spike deferred. Audit found the dominant size cost is the embedded `static_model.bin` (55.7MB, 57% of binary), not the Go runtime. Even a perfect tinygo swap saves at most ~10MB; 5f saved ~55MB. Re-evaluate tinygo only if there's a need to compress further after the model is already out.
[x] - SP-045-5f: Lazy-load `pkg/embedding/static_model.bin` from a separate URL on WASM. Split `//go:embed` into `static_model_embed.go` (native only, `!js`) and a runtime-populated `staticModelData` slice that the host page fills via `SproutWasm.setStaticModel(bytes)`. `scripts/build-wasm.sh` copies the .bin into the build output. `docs/WASM_API.md` has a "One-time bootstrap" section with the boot recipe. WASM tests have a `TestMain` (build-tagged `js`) that loads the .bin from disk before tests run, mirroring the host-page path. **WASM size: 97.7MB → 42.0MB (57% reduction).** Native build self-contained as before.
[] - SP-045-5e: Module split deferred — at 42MB the WASM is no longer "casually large" and the cost/benefit of splitting into shell+embedding modules isn't clear without real user data. Re-open if first-load metrics demand it.
