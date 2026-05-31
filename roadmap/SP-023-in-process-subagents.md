# SP-023: In-Process Subagent Execution

**Status:** ✅ Active

## Problem

Subagents are currently spawned as separate binary processes via `os/exec`. This creates:

1. **No structured events** - Parent gets raw text lines, cannot observe tool calls, LLM requests, or errors in real-time
2. **High startup cost** - Each subagent spawns a full binary: config loading, provider initialization, credential loading (seconds of overhead)
3. **Fragile token budget monitoring** - Temp file polling every 2 seconds, racy and imprecise
4. **No graceful cancellation** - `context.WithTimeout` kills the entire process, no "finish current tool call then stop"
5. **No shared state** - Each subagent has its own embedding index, todo manager, memory - cannot coordinate
6. **Blind parallel execution** - No visibility into which subagents are making progress, stuck, or completed early
7. **Late error detection** - Errors only surface at process exit
8. **File conflicts** - AGENTS.md blocks parallel subagents because there's no coordination mechanism

## Solution

Replace process-based subagent execution with in-process `Agent` instances running in goroutines.

### Architecture

```
Parent Agent
  ├── SubagentRunner (manages lifecycle)
  │     ├── Subagent 1 (Agent instance, goroutine)
  │     │     └── shares event bus, config, todo manager, embeddings
  │     ├── Subagent 2 (Agent instance, goroutine)
  │     │     └── shares event bus, config, todo manager, embeddings
  │     └── Subagent 3 (Agent instance, goroutine)
  │           └── shares event bus, config, todo manager, embeddings
```

### Key Components

**SubagentRunner** - Manages in-process subagent lifecycle:
- Creates `Agent` instances with shared state
- Tracks running subagents with structured metrics
- Provides cancellation, token budgeting, and timeout control
- Returns structured results (tokens, cost, tool calls, elapsed time)

**SharedState** - Resources shared between parent and subagents:
- Event bus (for real-time event streaming)
- Todo manager (for shared todo lists)
- Embedding manager (for shared semantic search)
- Config manager (for shared configuration)
- Workspace root (for shared file system context)

**SubagentResult** - Structured output replacing JSON-serialized stdout:
- Output text
- Token usage, cost, tool call count
- Elapsed time, cancellation status, budget exceeded flag

### Benefits

1. **Structured event streaming** - Each subagent publishes to shared event bus; parent subscribes for real-time visibility
2. **Near-zero startup cost** - Just struct allocation + interface injection, no binary spawn
3. **Precise token budgeting** - Read token counts directly from `AgentState`, cancel exactly at limit
4. **Graceful cancellation** - Context propagation allows finishing current tool call before stopping
5. **Shared state** - Subagents share embeddings, todos, memory - can see parent and sibling work
6. **Coordinated parallel execution** - File locking, dependency tracking, conflict detection become possible
7. **Immediate error detection** - Errors surface via event bus, not at process exit

### Migration Path

1. Create `SubagentRunner` interface layer
2. Implement in-process version using existing `Agent` creation
3. Wire up shared event bus and state
4. Replace `RunSubagent` calls with `SubagentRunner.Run()`
5. Update tool handlers to use `SubagentRunner`
6. Remove old process-based `subagent.go`

## Files Changed

- `pkg/agent/subagent_runner.go` - New: SubagentRunner implementation
- `pkg/agent/tool_handlers_subagent.go` - Modified: Wire to SubagentRunner
- `pkg/agent_tools/subagent.go` - Removed: Process-based implementation
- `pkg/agent/subagent_runner_test.go` - New: Tests

## Related

- SP-001: Agent Core Architecture (defines the agent structure)
- SP-020: Trace/Dataset Mode (benefits from structured subagent events)
- TODO: Subagent output visibility in WebUI
