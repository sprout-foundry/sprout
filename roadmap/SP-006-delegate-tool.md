# SP-006: Delegate Tool — In-Process Agent Delegation

**Status:** 📋 Proposed  
**Depends on:** SP-007 (Extend Configuration — role config infrastructure)  
**Priority:** High  
**Effort Estimate:** ~3-4 weeks (3 phases)

## Problem

The current `run_subagent` tool spawns a **separate OS process** (`sprout agent`) via `exec.Command`. This creates fundamental limitations:

1. **No shared state** — child process has its own config, MCP connections, credential store
2. **No incremental visibility** — output is raw stdout/stderr, not structured tool events
3. **Latency** — full process spawn + config load + MCP init per subagent
4. **No bidirectional communication** — parent sends prompt, blocks until completion
5. **State lost on exit** — file changes, conversation context, summaries all vanish

## Proposed Solution

A new `delegate` tool that creates a **full, in-process `*Agent` instance** — sharing workspace, credentials, and event bus with the parent, but maintaining independent conversation, tools, MCP, and config.

## Architecture

```
Parent Agent (orchestrator)
  │
  ├─ calls delegate tool with {prompt, role, provider, model, tools}
  │
  └─ spawns in-process DelegateAgent
       ├─ config: base cloned + role config overlay (SP-007)
       ├─ own conversation history, messages, checkpoints
       ├─ own tool set (filtered by role or tools param)
       ├─ own provider/model (from role or params)
       ├─ own MCP servers (base + role additive merge)
       ├─ shares: workspace root, credential store, parent event bus
       ├─ streams structured events via DelegateStreamBridge
       └─ returns DelegateResult {summary, files_changed, tools_called, tokens, cost}
```

## Tool Specification

```go
{
  name: "delegate",
  description: "Delegate a task to a full-capability in-process agent. " +
    "Unlike run_subagent (OS process), delegate shares workspace and " +
    "credentials, returns structured results, and streams tool activity.",
  parameters: [
    {name: "prompt",        type: "string", required: true,
     description: "The task description for the delegate agent"},
    {name: "role",          type: "string", required: false,
     description: "Role config name from SP-007 (e.g., 'api-specialist')"},
    {name: "provider",      type: "string", required: false,
     description: "Provider override for this delegate"},
    {name: "model",         type: "string", required: false,
     description: "Model override for this delegate"},
    {name: "tools",         type: "array",  required: false,
     description: "Tool allowlist (e.g., ['shell_command', 'read_file'])"},
    {name: "context",       type: "string", required: false,
     description: "Additional context/instructions"},
    {name: "max_iterations",type: "int",   required: false,
     description: "Max iterations (default: 100)"},
    {name: "files",         type: "string", required: false,
     description: "Comma-separated files for delegate context"},
  ]
}
```

## Return Value

```go
type DelegateResult struct {
  Summary      string           `json:"summary"`
  FilesChanged []string         `json:"files_changed"`
  ToolsCalled  []ToolCallRecord `json:"tools_called"`
  TokensUsed   int              `json:"tokens_used"`
  Cost         float64          `json:"cost"`
  Iterations   int              `json:"iterations"`
  ExitStatus   string           `json:"exit_status"` // completed | max_iterations | error
  ErrorMessage string           `json:"error_message,omitempty"`
}
```

## Key Differences from run_subagent

| Aspect | run_subagent | delegate |
|--------|-------------|----------|
| Execution | OS process (`exec.Command`) | In-process goroutine |
| Config | Child loads from disk | Cloned from parent + role overlay |
| MCP | Child inits its own | Base + role (additive merge) |
| Output | Raw stdout/stderr | Structured events with tool detail |
| State | Lost on exit | Observable via ChangeTracker |
| Workspace | Separate cwd | Shared workspace root |
| Credentials | Separate lookup | Shared credential store |
| Cost | Not tracked by parent | Tracked and returned |
| Interrupt | Kill process | Cancel context (graceful) |

## Implementation Phases

### Phase A: Core Delegate Tool

**New files:**
- `pkg/agent/delegate_types.go` — `DelegateResult`, `DelegateConfig`, `ToolCallRecord`
- `pkg/agent/delegate_factory.go` — `CreateDelegateAgent(parent, cfg) (*Agent, error)`
- `pkg/agent/delegate_stream.go` — `DelegateStreamBridge` (event bus bridge)
- `pkg/agent/tool_handlers_delegate.go` — Tool handler + registration

**Modified files:**
- `pkg/agent/tool_definitions.go` — Register `delegate` tool
- `pkg/events/events.go` — Add delegate event types

**Key design decisions:**
- Use `ProcessQuery()` (existing non-interactive path) for delegate execution
- Delegate gets its own `*Agent` via `NewAgentWithLayers` + role overlay
- Event bridge publishes: `delegate_spawn`, `delegate_tool`, `delegate_progress`, `delegate_complete`
- Max nesting depth: `SPROUT_MAX_DELEGATE_DEPTH=3` (env var)

### Phase B: WebUI Integration

- Render `delegate_activity` events (similar to existing `subagent_activity`)
- Expandable tool call tree instead of raw text output
- Show delegate cost/token accumulation in real-time

### Phase C: Bidirectional Communication (Future)

- Allow parent to inject follow-up messages into running delegate
- Support interactive delegation (not just blocking)
- Delegate can request clarification from parent via event bus

## Open Questions

1. Should delegates be allowed to further delegate (nesting)? → Yes, max depth 3
2. Should delegate file changes be auto-committable by parent? → Yes, via ChangeTracker inspection
3. Should delegate share parent's conversation context? → No, independent conversation (prevents context bloat)

## Files Reference

| File | Action |
|------|--------|
| `pkg/agent/tool_handlers_subagent.go` | Existing: subagent spawning pattern to reference |
| `pkg/agent_tools/subagent.go` | Existing: `RunSubagent` process-spawn implementation |
| `pkg/agent/agent.go` | Existing: Agent struct, `initSubManagers` |
| `pkg/configuration/role.go` | SP-007: Role config schema |
| `pkg/configuration/role_manager.go` | SP-007: Role resolution chain |
