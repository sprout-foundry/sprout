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
[] - SP-027-2d: Add `PersistentContextConfig` struct to `pkg/configuration/config.go` — `ProactiveContextEnabled` (true), `MaxContextualResults` (5), `MinRelevanceScore` (0.50), `MaxContextChars` (4000), `WorkspaceScopedRetrieval` (false)
[] - SP-027-2e: Tests — unit test for retrieval with time decay, test for empty store (graceful no-op), test for workspace-scoped filtering

### Phase 3: Drift Detection

[] - SP-027-3a: Create `pkg/agent/drift_detection.go` — track `SessionIntentEmbedding` (from `ConversationState`), compute cosine similarity with current prompt every Nth turn, flag if below `DriftThreshold` (0.60)
[] - SP-027-3b: Implement non-blocking drift notification — WebUI: toast-style notification with "Continue here" / "Start new chat" options (non-modal, agent continues). CLI: post-turn prompt with Enter to continue, 's' for new chat
[] - SP-027-3c: Implement suppression logic — disable drift detection for session after 3 consecutive rejections
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
