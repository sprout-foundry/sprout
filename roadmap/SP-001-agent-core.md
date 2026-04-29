# SP-001: Agent Core Architecture

**Status:** ✅ Active (recently refactored)  
**Location:** `pkg/agent/`  
**Size:** ~49K lines Go (largest package)  
**Test Files:** 70+

## Current State

The Agent is the central LLM orchestration component. It manages conversations, tool execution, persona switching, and subagent spawning. Recently decomposed from a monolithic ~100-field struct into a coordinated set of sub-managers.

## Architecture

### Agent Struct (recently refactored)

```go
type Agent struct {
    // Core LLM coordination
    client           api.ClientInterface
    clientType       api.ClientType
    systemPrompt     string
    baseSystemPrompt string
    maxIterations    int
    currentIteration int

    // Configuration & workspace
    configManager *configuration.Manager
    workspaceRoot string
    debug         bool

    // Input handling
    inputInjectionChan  chan string
    inputInjectionMutex sync.Mutex
    interruptCtx        context.Context
    interruptCancel     context.CancelFunc

    // Sub-managers (interfaces)
    state    StateManager    // Conversation history, checkpoints, tokens, cost, persona
    output   OutputManager   // Streaming, async output, event metadata, routing
    security SecurityManager // Approvals, redaction, elevation, bypass
    mcpSub   MCPSubManager   // MCP server lifecycle and tool caching

    // Event system + validation
    eventBus  *events.EventBus
    validator *validation.Validator

    // Tool execution support
    shellCommandHistory map[string]*ShellCommandResult
    changeTracker       *ChangeTracker
    preparedTools       sync.RWMutex
    lastToolNames       []string

    // UI integration
    ui UI

    // Stats callback
    statsUpdateCallback func(int, float64)
}
```

### Sub-Manager Interfaces

| Manager | File | Responsibility |
|---------|------|----------------|
| `StateManager` | `submanager_state.go` | Messages, checkpoints, tokens, cost, sessionID, config overrides, task actions, trace session, persona |
| `OutputManager` | `submanager_output.go` | Streaming buffer, reasoning buffer, async output channel, output router, flush callback |
| `SecurityManager` | `submanager_security.go` | Security approval manager, output redactor, elevation gate, filesystem bypass, webui client tracking |
| `MCPSubManager` | `submanager_mcp.go` | MCP manager interface, tools cache, init mutex, init error |

### Conversation Flow

```
User Input → ProcessQuery()
  → prepareMessages()     (privacy filter, compaction, MCP tools)
  → callLLM()             (streaming API call)
  → streaming callback    (chunk accumulation)
  → ExecuteTools()        (if model returned tool_calls)
  → tool_executor_*       (sequential or parallel execution)
  → self-review gate      (optional)
  → formatResponse()      (reasoning + text assembly)
  → loop until stop reason
```

### Tool System

- **ToolRegistry** (`tool_definitions.go`): ~20 built-in tools registered with parameter schemas
- **ToolHandler**: `func(ctx, *Agent, args) → (string, error)` or image-returning variant
- **Tool execution**: Sequential (`tool_executor_sequential.go`) or parallel (`tool_executor_parallel.go`)
- **Circuit breaker** (`tool_executor_circuit_breaker.go`): Tracks repetitive tool calls
- **Tool filtering**: Personas define `AllowedTools` to restrict available tools

### Subagent System

- **`run_subagent`** (`tool_handlers_subagent.go`): Spawns OS process via `exec.Command`
- **`run_parallel_subagents`**: Spawns multiple processes concurrently
- Config: `SubagentProvider`, `SubagentModel`, `SubagentTypes[role]`
- Persona resolution via `GetSubagentType(id)` → provider/model/system-prompt/tools
- Output streamed via pipe, batched, published as `subagent_activity` events

### Persona System

- Defined in `config.json` → `subagent_types` map
- Each persona: id, name, provider, model, system prompt (file or inline), tool allowlist
- 13 built-in personas: orchestrator, coder, tester, debugger, refactor, code_reviewer, researcher, web_scraper, general, project_planner, computer_user, repo_orchestrator
- Applied via `ApplyPersona(id)` or `--persona` CLI flag

### Skills System

- Defined in `config.json` → `skills` map
- Each skill: id, name, description, path, enabled, metadata, allowed_tools
- Loaded into context via `activate_skill` tool
- Skills inject instruction bundles into the system prompt

### State Persistence

- **ExportState/ImportState**: JSON serialization of messages, checkpoints, metrics
- **Turn checkpoints**: Per-turn summaries for context compaction
- **Conversation summary**: Previous action summaries for continuity across sessions
- **Session persistence**: WebUI saves/restores full agent state per chat session

### Compaction/Optimization

- **ConversationOptimizer** (`conversation_optimizer.go`): Context window management
- **ConversationPruner** (`conversation_pruner.go`): Automatic message pruning
- **Checkpoint compaction**: Replace old turns with actionable summaries
- **LLM-based compaction**: Optional LLM-generated summaries (when client supports it)
- **File invalidation**: Detects stale file reads after edits

## Open Work (from TODO.md)

- [ ] CONCURRENCY: Remaining sync improvements (channels for cross-component comms, `-race` CI)
- [ ] OBSERVABILITY: Structured error taxonomy and diagnostic logging

## Large Files Needing Attention

| File | Lines | Concern |
|------|-------|---------|
| `tool_handlers_subagent.go` | 1337 | Above 500-line target |
| `conversation_optimizer.go` | 1204 | Above target |
| `scripted_client.go` | 1093 | Above target |

## Key Files

| File | Purpose |
|------|---------|
| `agent.go` | Agent struct, NewAgent*, initSubManagers |
| `submanager_state.go` | StateManager interface + impl |
| `submanager_output.go` | OutputManager interface + impl |
| `submanager_security.go` | SecurityManager interface + impl |
| `submanager_mcp.go` | MCPSubManager interface + impl |
| `conversation_handler.go` | ProcessQuery orchestration |
| `conversation_messaging.go` | prepareMessages, privacy, image processing |
| `conversation_optimizer.go` | Context window management |
| `conversation_pruner.go` | Message pruning |
| `tool_definitions.go` | ToolRegistry, all tool registrations |
| `tool_handlers_subagent.go` | run_subagent, run_parallel_subagents |
| `tool_executor_sequential.go` | Sequential tool execution |
| `tool_executor_parallel.go` | Parallel tool execution |
| `tool_executor_circuit_breaker.go` | Repetitive tool call detection |
| `persona.go` | ApplyPersona, GetAvailablePersonaIDs |
| `skills.go` | Skill loading, activation |
| `state.go` | ExportState, ImportState, summaries |
| `turn_checkpoints.go` | Per-turn checkpoint management |
| `persistence.go` | Session save/load, summary management |
| `streaming.go` | Streaming callbacks, buffer management |
| `output_router.go` | Terminal vs WebUI output routing |
| `summary.go` | Conversation summary generation |
| `mcp.go` | MCP initialization and tool caching bridge |
| `api_client.go` | LLM API client with provider routing |
| `models.go` | Model listing, provider capability queries |
