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
[] - SP-027-2b: Create `pkg/agent/proactive_context.go` — query `ConversationStore` with time decay, filter by `MinRelevanceScore` (0.50), cap at `MaxContextualResults` (5), format as "Previous Work" section for system prompt injection
[x] - SP-027-2c: Hook `proactiveContext.Inject()` into `ProcessQuery()` pre-loop — only on first turn (no prior messages beyond system prompt) or cold session restore
[x] - SP-027-2d: Add `PersistentContextConfig` struct to `pkg/configuration/config.go` — `ProactiveContextEnabled` (true), `MaxContextualResults` (5), `MinRelevanceScore` (0.50), `MaxContextChars` (4000), `WorkspaceScopedRetrieval` (false)
[x] - SP-027-2e: Tests — unit test for retrieval with time decay, test for empty store (graceful no-op), test for workspace-scoped filtering

### Phase 3: Drift Detection

[x] - SP-027-3a: Create `pkg/agent/drift_detection.go` — track `SessionIntentEmbedding` (from `ConversationState`), compute cosine similarity with current prompt every Nth turn, flag if below `DriftThreshold` (0.60)
[x] - SP-027-3b: Implement non-blocking drift notification — WebUI: toast-style notification with "Continue here" / "Start new chat" options (non-modal, agent continues). CLI: post-turn prompt with Enter to continue, 's' for new chat
[x] - SP-027-3c: Implement suppression logic — disable drift detection for session after 3 consecutive rejections
[x] - SP-027-3d: Add `CreateSessionWithHandoff()` to `pkg/webui/chat_sessions.go` — extract `ActionableSummary` from last turn, pre-populate new chat system prompt with "Context from Previous Chat" section
[] - SP-027-3d: Add `CreateSessionWithHandoff()` to `pkg/webui/chat_sessions.go` — extract `ActionableSummary` from last turn, pre-populate new chat system prompt with "Context from Previous Chat" section
[] - SP-027-3e: Add drift config fields to `PersistentContextConfig` — `DriftDetectionEnabled` (true), `DriftThreshold` (0.60), `DriftCheckInterval` (5 turns)
[] - SP-027-3f: Create WebUI drift notification component in `webui/src/components/` — non-modal toast with "Continue here" / "Start new chat" buttons
[] - SP-027-3g: Tests — unit test for drift detection with threshold, test for suppression after 3 rejections, test for intent embedding persistence across session restore

### Phase 4: Memory Integration

[] - SP-027-4a: Add `StoreMemory()` to `ConversationStore` — embed memory file content, store as `VectorRecord` with Type: "memory"
[] - SP-027-4b: Create `pkg/agent/memory_embedding.go` — `EmbedMemory()` function called from `SaveMemory()`, `DeleteMemory()` also removes from store
[] - SP-027-4c: Implement one-time memory migration — on first `search_memories` call or app startup, embed all existing `~/.config/sprout/memories/*.md` files into conversation store
[] - SP-027-4d: Add `search_memories` tool to `pkg/agent/tool_definitions.go` — `search_memories(query: string, max_results?: int) → []{name, title, relevance}`
[] - SP-027-4e: Implement `handleSearchMemories()` in `pkg/agent/memory_handlers.go` — embed query, search conversation store for Type:"memory" records, return ranked results
[] - SP-027-4f: Tests — unit test for memory embedding round-trip, test for search tool with semantic query, test for migration of existing memories

---

## SP-028: Test Suite Stabilization — Deadlock Resolution & CI Hardening

Spec: `roadmap/SP-028-test-suite-stabilization.md`

### Phase 1: Unblock CI (loud failures instead of silent hangs)

[] - SP-028-1a: Add `go.uber.org/goleak` to `go.mod` test deps
[] - SP-028-1b: Create `pkg/webui/main_test.go` with `TestMain` calling `goleak.VerifyNone(t)` — ignore known long-lived workers from `pkg/logging`/`pkg/history` via `goleak.IgnoreTopFunction(...)`
[] - SP-028-1c: Create `pkg/agent/main_test.go` with `TestMain` calling `goleak.VerifyNone(t)` — same ignore list as webui
[] - SP-028-1d: Update `Makefile` `test` target — add `-race -count=1 -timeout=90s` to `pkg/agent` and `pkg/webui` test invocations
[] - SP-028-1e: Update `.github/workflows/build.yml` — drop `-short` from the race step for `pkg/agent` and `pkg/webui`

### Phase 2: Fix MCP init deadlock

[] - SP-028-2a: Add double-checked RWMutex fast path to `getMCPTools` in `pkg/agent/mcp.go` (sites at lines 162 and 184) — `RLock` and check init flag; only acquire write lock on cache miss
[] - SP-028-2b: Audit every transitive caller of `LockInit` (`pkg/agent/submanager_mcp.go:73`) for lock-order violations; document the lock-order invariant as a doc comment on `AgentMCPManager`
[] - SP-028-2c: Reduce `TestMCPConcurrency_StressTest` (`pkg/agent/mcp_concurrency_test.go:264`) from 200×10 to 32×50 and add `t.Cleanup(func() { agent.Shutdown() })`
[] - SP-028-2d: Verify with `go test -race -run TestMCPConcurrency -count=20 ./pkg/agent/` — must pass 20× in a row

### Phase 3: Fix WebUI PTY goroutine leak

[] - SP-028-3a: Add `done chan struct{}` to terminal session struct; close it on `session.Close()` (`pkg/webui/terminal_session.go`)
[] - SP-028-3b: Rewrite the PTY read loop at `pkg/webui/terminal_create.go:146-175` — `select` on `done` alongside the read; use `pty.SetDeadline()` (with periodic-polling fallback if unsupported on platform)
[] - SP-028-3c: Audit every test that creates a terminal session; add `t.Cleanup(session.Close)` — sweep `pkg/webui/*_test.go`
[] - SP-028-3d: Verify with `go test -race -count=5 ./pkg/webui/` — `goleak` reports zero leaks

### Phase 4: Sustain

[] - SP-028-4a: Create `pkg/agent/concurrency_test.go` — pin the new MCP-init invariant with a fast regression test (16 goroutines, single phase, cleanup-verified)
[] - SP-028-4b: Add package-level doc comments to `pkg/agent/submanager_mcp.go` and `pkg/webui/terminal_create.go` documenting lock order and PTY lifecycle

---

## SP-029: Monolith Decomposition — File Size Reduction

Spec: `roadmap/SP-029-monolith-decomposition.md`

**Blocked by:** SP-028 Phase 1 (need green baseline before refactoring)

### Phase 1: Smallest blast radius first

[] - SP-029-1a: Split `pkg/agent/tool_handlers_subagent.go` (1318 LOC) — extract `handleRunParallelSubagents` to `tool_handlers_subagent_parallel.go`, batching to `tool_handlers_subagent_batch.go`, utilities to `tool_handlers_subagent_utils.go`. Pure move, no signature changes.
[] - SP-029-1b: Split `pkg/agent_providers/generic_provider.go` (1276 LOC) — extract HTTP-error helpers to `generic_provider_errors.go`, request-building helpers to `generic_provider_request.go`, model-listing/vision to `generic_provider_models.go`, max-tokens retry to `generic_provider_retry.go`

### Phase 2: Configuration and optimizer

[] - SP-029-2a: Split `pkg/configuration/config.go` (1895 LOC) into 9 files per the table in SP-029 — `config_types.go`, `config_risk.go`, `config_subagents.go`, `config_skills.go`, `config_paths.go`, `config_persistence.go`, `config_accessors.go`, `config_validate.go`, plus the slimmed `config.go`. Single PR; coordinate to avoid merge conflicts.
[] - SP-029-2b: Split `pkg/agent/conversation_optimizer.go` (1319 LOC) — extract summary builders to `conversation_optimizer_summary.go`, file-read tracking to `conversation_optimizer_files.go`, shell-command tracking to `conversation_optimizer_shell.go`

### Phase 3: Surface area packages

[] - SP-029-3a: Split `pkg/wasmshell/commands.go` (1633 LOC) — `commands_fs.go` (filesystem builtins), `commands_text.go` (text-processing builtins), `commands_env.go` (env/help/util builtins), `commands_util.go` (private helpers)
[] - SP-029-3b: Split `pkg/webcontent/browser_rod.go` (1335 LOC) — `browser_rod_session.go`, `browser_rod_actions.go`, `browser_rod_capture.go`, `browser_rod_gpu.go`

### Phase 4: Investigate-then-split

[] - SP-029-4a: Read `pkg/agent/seed_tool_registry.go` (1223 LOC) end-to-end, define the split table, then execute. Likely 3 files (definitions / dispatcher / handler bindings).
[] - SP-029-4b: Read `pkg/lsp/semantic/go_adapter.go` (1188 LOC) end-to-end, define the split by LSP capability area (definitions, references, completions, diagnostics), then execute. Likely 4 files.
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

[] - SP-030-3a: Per-file audit and update of `ledit` references in `docs/ELECTRON.md`, `docs/AGENT_WORKFLOW.md`, `docs/PROVIDER_CATALOG.md`, `docs/TESTING.md`, `docs/PRODUCT_BACKLOG.md`, `docs/subagent_personas.md`
[] - SP-030-3b: Audit `README.md` and update non-historical `ledit` references; leave `CHANGELOG.md` historical sections intact

### Phase 4: Decide-then-act on service names

[] - SP-030-4a: Audit `cmd/service_darwin_test.go` and `cmd/service_linux_test.go` plus the production service install code paths — decide whether the on-disk service identifier (`launchd`/`systemd` label) stays `ledit` for backwards compat. **Document the decision in the audit PR.**
[] - SP-030-4b: If keeping the service name: add a `// keep: backwards-compat with installed daemons` comment at each `ledit` literal. If renaming: implement upgrade migration (uninstall old + install new, bump config version)

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
