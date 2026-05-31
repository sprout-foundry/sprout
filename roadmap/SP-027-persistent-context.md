# SP-027: Persistent Context & Conversational Memory

**Status:** 📋 Proposed

## 1. Executive Summary

This spec extends the existing compaction, embedding, and memory systems to provide **persistent conversational context** across sessions. The system remembers what you worked on before, detects when conversations drift into new territory, and maintains a searchable history of conversation turns — all without a vector database.

The key insight: we already have a fast static embedding model for code (SP-016), per-turn checkpoints (SP-024), and file-based memories (SP-018). This spec adds **semantic retrieval over conversation history** and **drift detection** on top of that foundation.

## 2. What Already Exists

| Component | File(s) | Status |
|-----------|---------|--------|
| **Static Embedding** | `pkg/embedding/static_provider.go` | ✅ Embedded model2vec model, pure Go, ~0.05ms/embedding, 256 dimensions |
| **Vector Store** | `pkg/embedding/store.go` | ✅ JSONL file store with in-memory linear scan (TopK over all records) |
| **Code Indexing** | `pkg/embedding/extractor*.go` | ✅ Function-level extraction for Go/TS/Python + file-level |
| **Turn Checkpoints** | `pkg/agent/turn_checkpoints.go` | ✅ Per-turn summaries with `ActionableSummary` |
| **Compaction** | `pkg/agent/conversation_optimizer.go` | ✅ 3-tier: dedup, structural compaction, checkpoint compaction |
| **Pruning** | `pkg/agent/conversation_pruner.go` | ✅ 5 strategies, adaptive default, context-aware thresholds |
| **Memory System** | `pkg/agent/memory.go` | ✅ File-based `.md` in `~/.config/sprout/memories/`, no semantic search |
| **Session Persistence** | `pkg/agent/persistence.go` | ✅ `ConversationState` saved to workspace-scoped JSON |
| **Chat Sessions** | `pkg/webui/chat_sessions.go` | ✅ In-memory sessions with lazy agent creation |

**Gaps this spec fills:**
1. No semantic search over conversation history (only over code)
2. No connection between memories and conversational context
3. No proactive context priming when starting a session
4. No drift detection across turns
5. No cross-session memory beyond `PreviousSummary`/`CompactSummary`

## 3. Architecture

### 3.1. Single Embedding Model

The system uses **one embedding model** for all purposes: the existing static model2vec provider.

| Use Case | What Gets Embedded | Where Used |
|----------|-------------------|------------|
| **Proactive Context** | User prompts + actionable summaries (as `VectorRecord`) | Retrieval on session start |
| **Drift Detection** | First user prompt (as `SessionIntentEmbedding`) | Comparison against subsequent prompts |
| **Memory Search** | Memory file content (Phase 4) | `search_memories` tool |

**Why one model, not two:** The user spec proposed separate static and transformer models. The static model2vec is already embedded in the binary, pure Go (~0.05ms/embedding), and sufficiently accurate for short-text semantic similarity. Adding a transformer model would require ~100MB+ binary, CGO or WASM runtime, and 10-100× slower inference. If higher fidelity is needed later, we can add an opt-in enhanced model (same pattern as SP-016's enhanced model).

### 3.2. The Conversation Turn Record

Each completed turn produces a `ConversationTurn` in memory, serialized into the conversation store as a `VectorRecord`. The mapping is:

```go
// In-memory representation (pkg/agent/conversation_turn.go)
type ConversationTurn struct {
    ID              string    // UUID
    SessionID       string    // parent session
    TurnNumber      int       // sequential within session
    Timestamp       time.Time // when the turn completed
    UserPrompt      string    // original user input
    ActionableSummary string  // from TurnCheckpoint.ActionableSummary
    PromptEmbedding  []float32 // embedding of user prompt (256 dims)
    FilesTouched    []string  // files read/modified during turn
    WorkingDir      string    // workspace at time of turn
    Duration        float64   // seconds from prompt to turn completion
    TokenUsage      int       // total tokens in this turn
}
```

**Serialization into `VectorRecord`** (stored in JSONL):

| VectorRecord Field | Mapping |
|-------------------|---------|
| `ID` | turn UUID |
| `File` | `session_{sessionID}.json` |
| `Name` | `turn_{N}` |
| `Signature` | user prompt (truncated to 2000 chars) |
| `Embedding` | mean of prompt embedding + summary embedding (256 dims) |
| `Type` | `"conversation_turn"` |
| `IndexedAt` | turn timestamp |

The `ActionableSummary`, `FilesTouched`, `WorkingDir`, `Duration`, and `TokenUsage` are stored in the `Metadata` map (extending `VectorRecord` with a `map[string]interface{}` field).

### 3.3. Storage

Code embeddings and conversation embeddings are stored in **separate files**:

```
~/.config/sprout/embeddings/
├── workspace_{hash}.jsonl     # code unit embeddings (existing)
└── conversation_turns.jsonl   # new: conversation turn embeddings
```

**Implementation:** A second `JSONLFileStore` instance is created for conversation turns. The new `ConversationStore` type wraps this store:

```go
// pkg/embedding/conversation_store.go
type ConversationStore struct {
    store   *JSONLFileStore     // second store instance
    provider EmbeddingProvider   // shared static provider
    path    string               // ~/.config/sprout/embeddings/conversation_turns.jsonl
}
```

The `ConversationStore` is created lazily by the `EmbeddingManager` (extended in Phase 1). It has its own in-memory record cache loaded on open. When the store is closed, it writes atomically (temp file + rename, same as existing `JSONLFileStore`).

**Why separate files:** Code embeddings are workspace-scoped and rebuilt when the codebase changes. Conversation embeddings are user-scoped and persist across all workspaces. Separating them avoids unnecessary re-indexing of conversation history when switching projects.

### 3.4. Time-Decayed Similarity

The retrieval scoring function combines cosine similarity with temporal decay:

```go
func ScoreWithDecay(similarity float64, timestamp time.Time, now time.Time) float64 {
    daysAgo := now.Sub(timestamp).Hours() / 24.0
    decay := math.Pow(0.5, daysAgo/30.0) // 30-day half-life
    return similarity * decay
}
```

This ensures that:
- A highly relevant turn from yesterday scores higher than the same relevance from 2 years ago
- Very recent turns (same day) have ~1.0 decay weight
- Turns older than 180 days are heavily deprioritized but not eliminated

### 3.5. Retrieval Query Flow

When doing proactive context retrieval, the flow is:

1. **Embed the current prompt** → `queryVector` (256 dims)
2. **Load all conversation turn records** from `ConversationStore` (linear scan over in-memory cache)
3. **Compute raw cosine similarity** for each record against `queryVector`
4. **Apply time decay** to each similarity score
5. **Filter** by `MinRelevanceScore` (0.50 after decay)
6. **Sort** by decayed score descending
7. **Cap** at `MaxContextualResults` (5)
8. **Optionally filter** by `WorkingDir` if workspace-scoped retrieval is enabled

**Performance note:** The existing `TopK()` in `similarity.go` does a linear scan (not HNSW). With 12,000 turns/year, this is ~12ms (12,000 × 0.05ms per cosine computation). Acceptable for interactive use. HNSW can be added later if stores grow significantly.

## 4. Key Workflows

### 4.1. Session Initialization — Proactive Context

When a new session starts (or a query is sent to a cold session), the system performs a retrieval to prime the LLM with relevant past work:

**Trigger:** At the start of `ProcessQuery()` if this is the first turn (no prior messages beyond the system prompt) or if the session was just restored from persistence with no active query.

**Flow:**
1. Embed the user's prompt using the static provider
2. Query `ConversationStore` using the retrieval flow (§3.5)
3. Format results and inject into the system prompt as:

```
## Previous Work (Contextual Memory)

The following past work may be relevant. Evaluate critically and discard anything irrelevant.

### Working on authentication (2 days ago)
User: "Add OAuth2 login with Google"
Summary: Implemented Google OAuth2 flow in pkg/auth/google.go...

### Fixed embedding index performance (1 week ago)
User: "The semantic search is slow on large codebases"
Summary: Reduced batch size from 64 to 32, added...
```

**Configuration:**
- `MaxContextualResults`: 5 (configurable, default)
- `MinRelevanceScore`: 0.50 (time-decayed score; below this = no injection)
- `MaxContextChars`: 4000 (hard cap on total injected character count)
- `WorkspaceScopedRetrieval`: false (default; when true, only retrieve turns from current workspace)

**Graceful degradation:** If the store is empty, the provider is unavailable, or embedding fails, the retrieval is silently skipped — no error is surfaced to the user or agent.

**Implementation:** `pkg/agent/proactive_context.go` — called from `ProcessQuery()` before the first LLM call.

### 4.2. Post-Turn Processing — Embedding & Storage

After each turn completes (after `TurnCheckpoint` is recorded in the existing flow):

**Flow:**
1. `embedTurn()` — compute embeddings for the user prompt and actionable summary using static provider
2. `meanEmbedding()` — compute element-wise mean of the two embeddings (for general retrieval)
3. `storeTurn()` — create a `VectorRecord` (§3.2 mapping) and append to `ConversationStore`
4. The store flushes to disk if the in-memory cache exceeds a batch threshold (default: 10 turns) or on explicit close

**Timing:** Embedding is ~0.05ms per text. Total overhead per turn: ~0.1ms. This runs in the same goroutine as checkpoint recording; no additional async overhead.

**Error handling:** If embedding or storage fails, the turn checkpoint is still recorded normally. The embedding is simply skipped for that turn. No error is surfaced.

**Implementation:** Extend `pkg/agent/turn_checkpoints.go` with `EmbedAndStoreTurn()` call after existing checkpoint recording.

### 4.3. Conversational Drift Detection

The system tracks the original session intent and checks whether the conversation has drifted:

**Setup (first turn):**
- Embed the first user prompt → `SessionIntentEmbedding` (256 dims)
- Persist `SessionIntentEmbedding` in `ConversationState` (new field) so it survives session restore

**Check (every Nth turn):**
- Embed the current user prompt
- Compute cosine similarity between current prompt and `SessionIntentEmbedding`
- If similarity < `DriftThreshold` (default: 0.60), flag as potential drift

**Non-blocking behavior:**
- **WebUI**: A subtle notification appears in the chat area (non-modal). Options: "Continue here" / "Start new chat". The agent does NOT wait for user response — it continues processing. The notification is purely advisory. If the user clicks "Start new chat", a new session is created with context handoff. If the user clicks "Continue here" or dismisses, the notification is suppressed for that session.
- **CLI**: After the turn completes, before accepting the next prompt, display: "Conversation drift detected. Press Enter to continue or 's' to start a new chat." Default (Enter) continues immediately — no blocking timeout.
- **Suppression**: After 3+ drift rejections in the same session, drift detection is disabled for the remainder of that session.

**Implementation:** `pkg/agent/drift_detection.go` — check runs after turn completion, before the next prompt is accepted. The `SessionIntentEmbedding` is stored on `ConversationState` and restored on session load.

### 4.4. Context Handoff (New Chat from Drift)

When the user accepts the drift suggestion and starts a new chat:

1. The `ActionableSummary` of the last completed turn is extracted (already exists from checkpoint)
2. A new session is created with a system prompt supplement:

```
## Context from Previous Chat

You were working on: [actionable summary of last turn]
The conversation has shifted to a new topic. Use the above context as background only.
```

3. The new chat gets a fresh `SessionIntentEmbedding` on its first user prompt

**Implementation:** `pkg/webui/chat_sessions.go` — add `CreateSessionWithHandoff(sourceSessionID, lastSummary)` method.

## 5. Implementation Plan

### Phase 1: Conversation Turn Embedding (Foundation)

**Goal**: Embed and store conversation turns in a dedicated store.

| File | Change |
|------|--------|
| `pkg/embedding/static_provider.go` | No change — reuse existing provider |
| `pkg/embedding/conversation_store.go` | **New** — `ConversationStore` wrapping a second `JSONLFileStore` instance |
| `pkg/agent/conversation_turn.go` | **New** — `ConversationTurn` struct, `EmbedAndStoreTurn()` function |
| `pkg/agent/turn_checkpoints.go` | Modify — call `EmbedAndStoreTurn()` after checkpoint recording |
| `pkg/embedding/manager.go` | Modify — add `GetConversationStore()` lazy initialization |
| `pkg/agent/persistence.go` | Modify — add `SessionIntentEmbedding []float32` to `ConversationState` |

**Deliverables:**
- Each turn produces a `VectorRecord` stored in `~/.config/sprout/embeddings/conversation_turns.jsonl`
- `SessionIntentEmbedding` saved on first turn, restored on session load
- Graceful degradation: turn checkpoint still recorded if embedding/storage fails
- Tests: unit test for embed → store round-trip, test for graceful failure

### Phase 2: Proactive Context Retrieval

**Goal**: Prime sessions with relevant past work on first prompt.

| File | Change |
|------|--------|
| `pkg/agent/proactive_context.go` | **New** — query store with time decay, format for system prompt injection |
| `pkg/agent/conversation.go` | Modify — hook `proactiveContext.Inject()` in pre-loop of `ProcessQuery()` |
| `pkg/configuration/config.go` | Modify — add `PersistentContextConfig` struct with fields |

**Deliverables:**
- Top-5 relevant past turns injected into system prompt on first turn
- Configurable: `ProactiveContextEnabled`, `MaxContextualResults`, `MinRelevanceScore`, `MaxContextChars`, `WorkspaceScopedRetrieval`
- Graceful degradation: no error if store is empty or embedding fails
- Tests: unit test for retrieval with time decay, test for empty store

### Phase 3: Drift Detection

**Goal**: Detect when conversations shift topics and offer new chat.

| File | Change |
|------|--------|
| `pkg/agent/drift_detection.go` | **New** — intent tracking, similarity check, non-blocking notification |
| `pkg/agent/conversation.go` | Modify — hook drift check after turn completion |
| `pkg/webui/chat_sessions.go` | Modify — add `CreateSessionWithHandoff()` method |
| `webui/src/components/` | **New** — drift notification component (non-modal toast) |
| `pkg/configuration/config.go` | Modify — add `DriftDetectionEnabled`, `DriftThreshold`, `DriftCheckInterval` |

**Deliverables:**
- Non-blocking drift notification in WebUI (toast-style)
- CLI prompt after turn completion (Enter to continue, 's' for new chat)
- `SessionIntentEmbedding` persisted in `ConversationState`
- Suppression after 3 rejections per session
- Tests: unit test for drift detection, test for suppression

### Phase 4: Memory Integration

**Goal**: Connect the existing file-based memory system with semantic search.

| File | Change |
|------|--------|
| `pkg/embedding/conversation_store.go` | Modify — add `StoreMemory()` method |
| `pkg/agent/memory_embedding.go` | **New** — embed and index memories into conversation store |
| `pkg/agent/memory.go` | Modify — call `EmbedMemory()` on `SaveMemory()`, `DeleteMemory()` |
| `pkg/agent/tool_definitions.go` | Modify — register `search_memories` tool |
| `pkg/agent/memory_handlers.go` | Modify — add `handleSearchMemories()` |

**Migration:** On first launch of Phase 4, all existing memories in `~/.config/sprout/memories/*.md` are embedded and indexed into the conversation store. This is a one-time background operation triggered on first `search_memories` call or on app startup if the memory store is empty.

**Tool definition:**
```
search_memories(query: string, max_results?: int) → []{name, title, relevance}
```

**Deliverables:**
- Memories get embeddings stored in `conversation_turns.jsonl` (Type: `"memory"`)
- `search_memories` tool allows LLM to find relevant memories by semantic query
- Migration of existing memories on first use
- Embedding updated on memory save/delete
- Tests: unit test for memory embedding, test for search tool

## 6. Risks & Mitigations

### 6.1. Signal-to-Noise Ratio

The static model may return false positives for short text. The `MinRelevanceScore` (0.50 after decay) is conservative. The system prompt instructs the LLM to evaluate retrieved context critically and discard irrelevant snippets.

### 6.2. Conversation Store Growth

Each turn stores one `VectorRecord` with a 256-dim embedding (~1KB per turn). At 1000 turns/month, that's ~1MB/month or ~12MB/year. With `ConversationTurnRetention` (default: 365 days), records older than the retention period are pruned on store open.

**Pruning implementation:** When `ConversationStore.Open()` loads records, any with `IndexedAt` older than `RetentionDays` are silently dropped (not written back). A counter of pruned records is logged.

### 6.3. Drift Threshold Tuning

The right threshold varies by user. Start conservative (0.60), make it configurable. The rejection-suppression feedback loop prevents repeated annoyance without manual tuning.

### 6.4. Privacy

All embedding data stays local. The `conversation_turns.jsonl` file contains user prompts (in `Signature`). Document this in the config option descriptions.

### 6.5. Retrieval Performance

Linear scan over in-memory records. At 12,000 turns/year, this is ~12ms. If stores grow to 100,000+ records, add an HNSW index (or switch to binary search with sorted records).

## 7. Alignment with Existing Specs

| Spec | Relationship |
|------|-------------|
| **SP-016** (Embedding Index) | Reuses existing `StaticProvider` and `JSONLFileStore` infrastructure |
| **SP-016b** (Expanded Index) | Conversation turns use `Type: "conversation_turn"` (third record type after `"code_unit"` and `"file"`) |
| **SP-018** (Memory System) | Phase 4 connects memories to the embedding store for semantic retrieval |
| **SP-024** (Context Management) | Builds on `TurnCheckpoint.ActionableSummary` and compaction; proactive context supplements observation masking |
| **SP-026** (Executive Assistant) | Drift detection and context handoff are useful for EA autonomous operation |

## 8. Configuration Options

New `PersistentContextConfig` struct added to `pkg/configuration/config.go` (nested under the main `Config` struct, parallel to `EmbeddingIndexConfig`):

```go
type PersistentContextConfig struct {
    // Proactive Context
    ProactiveContextEnabled bool    `json:"proactiveContextEnabled"`  // default: true
    MaxContextualResults    int     `json:"maxContextualResults"`     // default: 5
    MinRelevanceScore       float64 `json:"minRelevanceScore"`       // default: 0.50
    MaxContextChars         int     `json:"maxContextChars"`          // default: 4000
    WorkspaceScoped         bool    `json:"workspaceScopedRetrieval"` // default: false

    // Retention
    TurnRetentionDays int `json:"turnRetentionDays"` // default: 365

    // Drift Detection
    DriftDetectionEnabled bool    `json:"driftDetectionEnabled"` // default: true
    DriftThreshold        float64 `json:"driftThreshold"`        // default: 0.60
    DriftCheckInterval    int     `json:"driftCheckInterval"`    // default: 5 (turns)
}
```