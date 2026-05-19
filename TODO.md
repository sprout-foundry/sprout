# TODO

---

## SP-026: Executive Assistant Persona

Spec: `roadmap/SP-026-executive-assistant.md`

[x] - SP-026 Phase A: Replace `isSubagent bool` with `subagentDepth int` on Agent struct — enables 3-level nesting: EA (depth=0) → orchestrator (depth=1) → coder/tester (depth=2). Update `getOptimizedToolDefinitions()` to filter delegation tools at depth >= 2. Add `MaxSubagentDepth` config (default: 2). Update all references. `pkg/agent/agent.go`, `pkg/agent/agent_getters.go`, `pkg/agent/conversation.go`, `pkg/agent/subagent_runner.go`, `pkg/configuration/config.go`
[x] - SP-026 Phase B: Add `working_dir` parameter to `run_subagent` tool — allows spawning subagents at any directory under `$HOME`. Add `WorkingDir` to `SubagentOptions` and `SubagentTask`. Validate target exists and is within `$HOME`. `pkg/agent/subagent_runner.go`, `pkg/agent/tool_handlers_subagent.go`
[x] - SP-026 Phase C: File-based task queue tools — `task_queue_read`, `task_queue_publish`, `task_queue_add` with atomic writes, file locking, and persistent storage at `~/.config/sprout/task_queue.json`. `pkg/agent_tools/task_queue.go`, `pkg/agent/tool_definitions.go`
[x] - SP-026 Phase D: Persona infrastructure — `LocalOnly bool` on `SubagentType`, `IsLocalMode()` detection, sliding risk cascade for EA approvals (auto-approve low-risk, reason about medium-risk, escalate high-risk), `-f`/`--force` auto-reject. `pkg/configuration/config.go`, `pkg/agent/persona.go`, `pkg/agent/tool_handlers_shell.go`
[x] - SP-026 Phase E: Executive Assistant persona definition — full replacement system prompt, project discovery (AGENTS.md → git scan → memory → organic), auto-activate when started from `~`, commit tool with strict rules (reject force, require meaningful message), EA-spawned subagents get depth=1, two startup modes (queue mode for autonomous processing, interactive mode for standard chat). `subagent_prompts/executive_assistant.md`, `pkg/agent/project_discovery.go`, `pkg/agent/agent_creation.go`, `cmd/sprout/main.go`

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
[] - SP-029-4c: Read `pkg/agent/scripted_client.go` (1068 LOC) end-to-end, separate DSL parsing from playback engine from response builders, then execute. Likely 3 files.

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
[] - SP-030-3b: Audit `README.md` and update non-historical `ledit` references; leave `CHANGELOG.md` historical sections intact

### Phase 4: Decide-then-act on service names — **DE-SCOPED (moved to SP-032)**

[x] - SP-030-4a: ~~Audit `cmd/service_*` code paths~~ — Done as part of SP-032 audit; live install uses `sprout-daemon` and `cmd/service_linux.go` refuses non-sprout binaries. Pre-existing `ledit` daemon detection becomes **SP-032-2b**; fixture cleanup becomes **SP-032-4a**.
[x] - SP-030-4b: ~~Service name comment/migration~~ — No action required under SP-030. Covered by SP-032.

### Phase 5: Test fixtures

[] - SP-030-5a: Per-file audit of `pkg/agent/conversation_image_test.go`, `pkg/agent/tool_handlers_search_new_test.go`, `pkg/git/commit_helpers_test.go`, `pkg/history/history_tools_test.go` — replace `ledit` where it's incidental; leave where the literal string is being asserted

---

## SP-031: MCP Tool Input Validation Hardening

Spec: `roadmap/SP-031-mcp-input-validation.md`

### Phase 1: Implement validation

[] - SP-031-1a: Add `github.com/santhosh-tekuri/jsonschema/v6` to `go.mod`
[] - SP-031-1b: Add `compiledSchema *jsonschema.Schema` field to `MCPToolWrapper` and a `compileSchema()` method with lazy initialization (cache once at first use). `pkg/mcp/tool_wrapper.go`
[] - SP-031-1c: Replace the `ValidateArgs` stub at `pkg/mcp/tool_wrapper.go:233-238` with real validation — skip on nil schema, fail-open on compile error (warn once), return `*InvalidArgsError` on validation failure
[] - SP-031-1d: Create `pkg/mcp/errors.go` with `InvalidArgsError` typed error (Tool, Server, Wrapped fields; implements `Error()` and `Unwrap()`)

### Phase 2: Wire into execution

[] - SP-031-2a: Call `w.ValidateArgs(args)` at the top of `MCPToolWrapper.Execute` before the network round-trip; return early on validation error
[] - SP-031-2b: Update `CanExecute` (`pkg/mcp/tool_wrapper.go:171`) to call `ValidateArgs` and return `false` on failure; remove the TODO comment
[] - SP-031-2c: Format validation errors as a concise LLM-visible message — enumerate failing field paths and reasons, not raw `jsonschema` output. Use this as the tool result so the model can self-correct on the next iteration.

### Phase 3: Tests

[] - SP-031-3a: Replace the trivial assertions in `TestMCPToolWrapper_ValidateArgs` (`pkg/mcp/tool_wrapper_test.go:124-127`) with real cases — required fields, type mismatches, enum violations, nested objects
[] - SP-031-3b: Add test: `ValidateArgs` with `nil` schema → returns nil (skip path)
[] - SP-031-3c: Add test: `ValidateArgs` with malformed schema → warns once, returns nil (fail-open on our bug)
[] - SP-031-3d: Add integration test in `pkg/agent/` — stub MCP wrapper that returns `InvalidArgsError`; verify agent surfaces a useful tool-result message to the LLM mock

### Phase 4: Observability

[] - SP-031-4a: Add structured log entry on validation failure with `{tool, server, errors[]}` fields (cooperates with SP-008 structured logging)
[] - SP-031-4b: Add a counter/metric for `mcp_validation_failures` so we can see if a particular server is producing bad arguments at rate

---

## SP-025: Tree-Sitter Integration — Remaining Work

Spec: `roadmap/SP-025-tree-sitter-integration.md`

Phases 1–3 are complete: `pkg/ast/` is in place (tree-sitter via `odvcencio/gotreesitter v0.16.0`) and consumed by `pkg/agent_tools/repo_map.go` and `pkg/index/symbols.go`. The remaining work closes the gap so `pkg/embedding/extractor_*.go` stops maintaining its own parallel regex zoo, and finishes the WASM wiring.

### Phase 4: WASM Integration (finish)

[] - SP-025-4a: Add a `pkg/ast` import to `pkg/wasmshell/` and surface a basic code-intelligence entry point (e.g. a function-symbol lookup that the WASM shell can call). Today `pkg/ast/browser_cache.go` exists but no caller in `wasmshell` exercises it.
[] - SP-025-4b: Run `make build-wasm` and record the binary-size delta from enabling `pkg/ast` in the WASM target. Document the threshold the team is willing to accept.
[] - SP-025-4c: Verify `pkg/ast/browser_cache.go` (290 LOC) actually persists compiled grammars to browser storage (IndexedDB / localStorage) across page loads — write a manual reproduction note or a headless test.

### Phase 5: Embedding Extractor Migration (the consistency fix)

[] - SP-025-5a: Replace the body of `pkg/embedding/extractor_ts.go` (~531 LOC, 9 standalone regex patterns starting at `tsFuncRegex` line 13) with a thin adapter that calls `pkg/ast.ExtractSymbols()` and emits the existing embedding record shape. Keep the public function signature stable so callers in `pkg/embedding/index.go:106` don't change.
[] - SP-025-5b: Replace the body of `pkg/embedding/extractor_py.go` (~345 LOC, regex + indent-level tracking starting at `pyFuncRegex` line 14) with the same adapter pattern over `pkg/ast.ExtractSymbols()`. Confirm class/method nesting comes out of the AST scope info correctly — that's the subtle case the old indent tracker handled.
[] - SP-025-5c: Decide on `pkg/embedding/extractor_go.go` (currently uses native `go/ast` directly) — keep as-is for performance (no tree-sitter overhead) or migrate to `pkg/ast` for codebase consistency. Document the decision in a one-line comment at the top of the file.
[] - SP-025-5d: Add a symbol-coverage parity test in `pkg/embedding/extractor_parity_test.go` — given a fixture file in each of TS, JS, Python, assert that the set of symbol names returned by `repo_map`, `pkg/index/symbols`, and `pkg/embedding/extractor` is identical. This is the regression test that would have caught today's three-way disagreement.
[] - SP-025-5e: Delete the now-orphaned regex variables at the top of `extractor_ts.go` and `extractor_py.go` after the body migration in 5a/5b. Net code reduction target: ~700 LOC (with corresponding test simplification).
[] - SP-025-5f: Run `make build-all && go test ./pkg/embedding/...` and exercise an embedding refresh against the repo itself — verify previously-missed symbols (TS arrow functions, decorated Python methods, multi-line signatures) now appear in `~/.config/sprout/embeddings/*.jsonl`.

---

## SP-032: Daemon Mode Hardening

Spec: `roadmap/SP-032-daemon-mode-hardening.md`

The daemon's install/uninstall surface is solid, but `systemctl stop sprout` leaks the agent, MCP children, and active PTYs — and the HTTP API can be exposed unauthenticated if `SPROUT_BIND_ADDR` is misconfigured. SP-032 closes these gaps.

### Phase 1: Graceful shutdown (CRITICAL)

[] - SP-032-1a: Add `chatAgent.Shutdown()` call to the graceful-shutdown block at `cmd/agent_modes.go:447-460` — with a bounded context (5s) so it can't block daemon exit. `chatAgent.Shutdown()` is defined at `pkg/agent/agent_lifecycle.go:10` and is currently never invoked from the daemon path.
[] - SP-032-1b: Wire `ws.terminalManager.CloseAllSessions()` into `pkg/webui/server_lifecycle.go:126` `Shutdown()` before `ws.server.Shutdown(ctx)`. **Blocked by SP-028 Phase 3** (cancellable PTY read loop is a prerequisite — without it, `CloseAllSessions()` will block on `pty.Read`).
[] - SP-032-1c: Update the systemd unit template in `cmd/service_linux.go` — add `TimeoutStopSec=15`, `KillMode=mixed`, `KillSignal=SIGTERM` to the `[Service]` block.
[] - SP-032-1d: Manual verification — install + start the daemon, open a web terminal, kick off an agent query, run `systemctl --user stop sprout`. `pgrep -f sprout` (and `pgrep -f gopls` / `pgrep -f bash` from the terminal) returns empty within 15s.

### Phase 2: Security & migration (HIGH)

[] - SP-032-2a: At `pkg/webui/server.go:161`, read both `SPROUT_AUTH_TOKEN` and `SPROUT_BIND_ADDR`. If bind is non-`127.0.0.1`/`localhost` and token is empty, refuse to start with: `"Refusing to start: SPROUT_BIND_ADDR=%s requires SPROUT_AUTH_TOKEN to be set."` Cover this with a startup test.
[] - SP-032-2b: Add `detectLegacyService()` helper in `cmd/service.go` (cross-platform). Darwin checks `~/Library/LaunchAgents/com.ledit.*.plist`; Linux checks `~/.config/systemd/user/ledit*.service`. On `sprout service install`: print notice, prompt for confirmation (`-y` bypasses), then `launchctl bootout` / `systemctl --user disable && rm` the old unit before installing.
[] - SP-032-2c: Launchd crash backoff — `cmd/service_darwin.go:77`, switch `KeepAlive=true` to the dictionary form with `SuccessfulExit=false` (and `ExponentialBackoff=true` if targeting macOS 12+; document the minimum). Prevents the panic hot-loop.

### Phase 3: Operability (MEDIUM/LOW)

[] - SP-032-3a: Wrap Darwin daemon stdout/stderr log files (`~/.sprout/logs/daemon.{stdout,stderr}.log` from `cmd/service_darwin.go:35-36`) in `lumberjack.Logger` — 10MB max, 5 backups. `lumberjack` is already a dep.
[] - SP-032-3b: Pre-uninstall active-session check — before `Uninstall()` in `cmd/service_darwin.go:220` and `cmd/service_linux.go:125`, query the running daemon (if any) for active session count via its HTTP API. Print warning + count; require `-y`/`--yes` flag to skip.
[] - SP-032-3c: Add `syscall.SIGHUP` to the signal handler at `cmd/agent_modes.go:240`. On SIGHUP, call `configuration.Reload()`. Scope is on-disk config re-read only; running agents/tools unaffected.
[] - SP-032-3d: Write `docs/SERVICE.md` — install, start, stop, uninstall, troubleshoot, log file locations, env-file structure, and the security model section (user-uid execution, 127.0.0.1 default, auth-token requirement for non-local binds).

### Phase 4: Test fixture cleanup

[] - SP-032-4a: Update `cmd/service_darwin_test.go` (lines 11, 28, 69, 96) and `cmd/service_linux_test.go` (lines 18, 20, 102, 130) — replace `/usr/local/bin/ledit`, `/opt/ledit/bin/ledit`, `/usr/bin/ledit` test fixtures with the `sprout` equivalents so tests actually exercise the binary-name guard in `cmd/service_linux.go` `Install()`.

---

## SP-033: Agent Trust Boundary Hardening

Spec: `roadmap/SP-033-agent-trust-boundary-hardening.md`

Three trust boundaries to defend: the project (skills auto-load silently), the disk (tool outputs persist unredacted at `0644` and accrete forever), and subprocesses (MCP restarts unbounded, Chromium leaks on panic, Python has no timeout). Already-good baselines (api_keys.json at 0600, git-arg validator, `$()` recursive classification) stay untouched.

### Phase 1: Skill discovery UX

[] - SP-033-1a: Print a discovery notice listing every project-local skill (name + path) when `discoverProjectSkills` at `pkg/configuration/config.go:1690-1755` finds any. Stderr in CLI mode, startup banner in WebUI.
[] - SP-033-1b: Implement `.sprout/allowed_skills` allowlist file (one ID per line) with read/write helpers.
[] - SP-033-1c: In `discoverProjectSkills`, load skills not in the allowlist with `Enabled: false` so they appear but don't activate.
[] - SP-033-1d: New CLI commands `sprout skills allow <id>...` / `sprout skills revoke <id>...` / `sprout skills list` in `cmd/skills.go`.
[] - SP-033-1e: `--no-project-skills` flag on the agent command; default to "skip" when stdin is non-TTY (CI / non-interactive).
[] - SP-033-1f: Set `Metadata["source"]` to `builtin` / `project:<repo-root>` / `user` on every loaded skill; surface in the agent system prompt so the model knows where instructions came from.

### Phase 2: Redaction + file modes

[] - SP-033-2a: Create `pkg/redact/redact.go` with `Apply([]byte) []byte` covering AWS keys (`AKIA...`), GitHub tokens (`gh[pousr]_...`), Slack tokens, OpenAI/Anthropic-style `sk-...`, `BEGIN ... PRIVATE KEY` blocks, `Authorization:` / `X-API-Key:` headers, `*_TOKEN|*_KEY|*_SECRET|*_PASSWORD` env-style assignments. Replacement: `[REDACTED:<kind>]`.
[] - SP-033-2b: Pipe HTTP bodies through `redact.Apply` in `pkg/logging/request_logger.go` (runlog write path).
[] - SP-033-2c: Apply `redact.Apply` to `UserPrompt` and `ActionableSummary` in `pkg/agent/turn_checkpoints.go` before SP-027's `EmbedAndStoreTurn()`.
[] - SP-033-2d: Apply `redact.Apply` in `pkg/agent/memory_handlers.go` before writing memory files.
[] - SP-033-2e: Conditional redaction in `pkg/history/changetracker.go:461,481` — only redact when the revision target is *outside* the workspace root (in-workspace revisions are the real file content; out-of-workspace revisions like `~/.aws/credentials` are leakable).
[] - SP-033-2f: Change file modes `0644` → `0600` at `pkg/history/changetracker.go:461,481`. Audit all `os.WriteFile(…0644)` sites under `pkg/logging/`, `pkg/embedding/` (for `conversation_turns.jsonl`), `pkg/agent/memory*.go` — tighten where data is user-private.

### Phase 3: Lifecycle commands

[] - SP-033-3a: `sprout history clear [--older-than DURATION] [--workspace PATH]` in `cmd/history.go` — removes runlogs and change-tracker entries.
[] - SP-033-3b: `sprout embeddings clear [--type conversation_turn|memory|code]` in `cmd/embeddings.go`.
[] - SP-033-3c: Add `RetentionDays int` to `PersistentContextConfig` (default `0` = forever); background sweep on agent startup removes expired entries.
[] - SP-033-3d: All clear operations confirmation-prompt by default with `-y`/`--yes` bypass; support `--dry-run` to preview deletions.

### Phase 4: Subprocess hardening

[] - SP-033-4a: At `pkg/mcp/client.go:147`, replace bare `restartCount++` with a sliding-window check — after 3 failures in 60s, exponential backoff (start 1s, double, max 5min); after 10 failures in 24h, disable the server and surface a notice.
[] - SP-033-4b: Register `webcontent.RodRenderer.Close()` in the interactive-mode signal handler; add a `runtime.SetFinalizer` backstop on the renderer struct in `pkg/webcontent/browser_rod.go:1311`. Coordinate with SP-032 A1 so the daemon path is also covered.
[] - SP-033-4c: At `pkg/pythonruntime/runtime.go:65`, replace `exec.Command(...)` with `exec.CommandContext(ctx, ...)` carrying a 30s default deadline (configurable for longer operations).

### Phase 5: Audit log + documentation

[] - SP-033-5a: Extend runlog entries in `pkg/agent/tool_executor*.go` to capture all four of: raw tool-call JSON, executed (post-substitution) command, classifier decision (`SecuritySafe`/`SecurityCaution`/`SecurityDangerous`), and approval source (auto-rule X / manual / denied).
[] - SP-033-5b: Write `docs/SECURITY.md` — trust boundaries, classifier limitations (lift from `pkg/agent_tools/security_classifier.go:12-25` header), file layout per directory, how to clear persisted data, skill allowlist model, auth-token requirement for non-local binds (refs SP-032 B1).
[] - SP-033-5c: Create `SECURITY.md` at repo root with vuln-reporting contact and a link to `docs/SECURITY.md`.

---

## SP-034: WebUI ↔ Backend Workflow Hardening

Spec: `roadmap/SP-034-webui-workflow-hardening.md`

Four user-visible defects: Stop button doesn't cancel the in-flight LLM HTTP call, reloading the page during an agent run loses the live stream, two tabs on the same chat corrupt each other's state, and UI config writes silently overwrite concurrent CLI writes. Plus protocol hygiene: hand-maintained TS types drift from Go structs, outbound WebSocket messages aren't validated, error envelope is inconsistent.

### Phase 1: Cancellation that actually cancels (CRITICAL)

[] - SP-034-1a: Add `ctx context.Context` as first arg to `api.ClientInterface.SendChatRequest` and `SendChatRequestStream` in `pkg/agent_api/interface.go`. Mechanical signature change.
[] - SP-034-1b: Update every implementation in `pkg/agent_providers/` (Generic, Ollama, etc.) and the test scripted client to accept and forward the context.
[] - SP-034-1c: Update every caller in `pkg/agent/` (`api_client.go`, `conversation.go`, `seed_integration.go`) to pass through a real context.
[] - SP-034-1d: At `pkg/agent_providers/generic_provider.go:1160`, change bare `http.NewRequest("POST", ...)` to `http.NewRequestWithContext(ctx, "POST", ...)`. Grep the package for any other contextless `http.NewRequest` and convert.
[] - SP-034-1e: In `pkg/webui/api_query.go`, create `ctx, cancel := context.WithCancel(parent)` when a query starts; stash the `cancel` on the chat session struct.
[] - SP-034-1f: `handleAPIQueryStop` (around `pkg/webui/api_query.go:399`) calls **both** the existing `TriggerInterrupt()` and the stashed `cancel()` so the provider HTTP request gets aborted.
[] - SP-034-1g: Add a `RequestTimeout` (default 10min) to the chat http.Client in `pkg/agent_providers/generic_provider.go:172-176`. Defensive bound against hung providers.
[] - SP-034-1h: Integration test — start a query against a stub provider that sleeps 30s; click Stop after 1s; assert HTTP request was cancelled within 1s and tokens stopped accruing.

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
[] - SP-034-5e: Define a `WebUIError` struct `{Code, Message, Details, Retryable}` in `pkg/webui/errors.go`. Replace stringy 503 returns at `pkg/webui/api_query.go:391-396` and audit other handlers for the same anti-pattern.
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
