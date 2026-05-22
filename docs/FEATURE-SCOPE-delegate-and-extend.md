# Feature Scope: Agent Delegate Tool & Extend Configuration

## Overview

Two complementary features that expand sprout's agent architecture from a
single-worker model to a multi-tier organizational model where orchestrators
can delegate to fully-capable in-process workers, and users can collaboratively
create extended configurations that define new roles.

---

## Feature 1: Delegate Tool (In-Process Full-Agent Delegation)

### Problem

Current `run_subagent` spawns a **separate OS process** (`sprout agent`) via
`exec.Command`. This has several limitations:

1. **No shared state** — the child process has its own config, MCP connections,
   credential store, and workspace view. It cannot see the parent's conversation
   context, tool results, or incremental work.
2. **No incremental visibility** — output is streamed as raw text. The parent
   agent sees stdout/stderr but not structured tool calls, tool results,
   file changes, or LLM reasoning.
3. **Latency** — each subagent pays the cost of a full process spawn, config
   load, MCP initialization, and first API call.
4. **No bidirectional communication** — the parent sends a prompt and waits.
   It cannot interrupt, inject input, or adjust the task mid-flight.
5. **No state handoff** — the child's work (file changes, conversation context,
   accumulated summaries) is lost when the child exits.

### Proposed Design

A new `delegate` tool that creates a **full, in-process `*Agent` instance**
within the parent process. The delegate agent runs with its own conversation,
its own tool set, its own MCP connections, and its own configuration — but
shares the workspace, credential store, and event bus with the parent.

#### Core Architecture

```
Parent Agent (orchestrator)
  │
  ├─ calls delegate tool with prompt + role/config
  │
  └─ spawns in-process DelegateAgent
       ├─ config loaded from role config (or base clone)
       ├─ own conversation history, messages, checkpoints
       ├─ own tool set (filtered by role config)
       ├─ own provider/model (per role config)
       ├─ own MCP servers (per role config)
       ├─ shares: workspace, credentials, event bus parent
       ├─ streams structured events back to parent via event bus
       └─ returns summary + structured results to parent
```

#### Key Components

| Component | Location | Responsibility |
|-----------|----------|----------------|
| `DelegateTool` | `pkg/agent/tool_handlers_delegate.go` | Tool definition, args validation, agent lifecycle |
| `DelegateAgentFactory` | `pkg/agent/delegate_factory.go` | Creates configured in-process agents from role configs |
| `DelegateStreamBridge` | `pkg/agent/delegate_stream.go` | Bridges delegate events → parent event bus |
| `DelegateResult` | `pkg/agent/delegate_types.go` | Structured result type (summary, files changed, tool calls, etc.) |

#### Tool Specification

```go
// "delegate" tool definition
{
  name: "delegate",
  description: "Delegate a task to a full-capability agent running in-process. " +
    "Unlike run_subagent (which spawns an OS process), delegate creates an " +
    "in-process agent with shared workspace access, incremental event streaming, " +
    "and structured result return. Use this for complex multi-step tasks that " +
    "benefit from full agent capabilities.",
  parameters: [
    { name: "prompt",     type: "string", required: true,
      description: "The task description for the delegate agent" },
    { name: "role",       type: "string", required: false,
      description: "Extended role config to use (e.g., 'api-specialist', 'frontend-dev'). Falls back to base config." },
    { name: "provider",   type: "string", required: false,
      description: "Provider override for this delegate" },
    { name: "model",      type: "string", required: false,
      description: "Model override for this delegate" },
    { name: "tools",      type: "array",  required: false,
      description: "Tool allowlist for this delegate (e.g., ['shell_command', 'read_file', 'write_file'])" },
    { name: "context",    type: "string", required: false,
      description: "Additional context/instructions for the delegate" },
    { name: "max_iterations", type: "int", required: false,
      description: "Maximum iterations for this delegate (default: 100)" },
    { name: "files",      type: "string", required: false,
      description: "Comma-separated list of files to include in the delegate's context" },
  ]
}
```

#### Return Value

```go
type DelegateResult struct {
  Summary       string            `json:"summary"`        // Agent's final summary
  FilesChanged  []string          `json:"files_changed"`  // Files the delegate modified
  ToolsCalled   []ToolCallRecord  `json:"tools_called"`   // Structured log of tool usage
  TokensUsed    int               `json:"tokens_used"`    // Total tokens consumed
  Cost          float64           `json:"cost"`           // Total cost
  Iterations    int               `json:"iterations"`     // Iterations used
  ExitStatus    string            `json:"exit_status"`    // "completed", "max_iterations", "error"
  ErrorMessage  string            `json:"error_message"`  // If exit_status == "error"
}

type ToolCallRecord struct {
  Tool    string `json:"tool"`
  Success bool   `json:"success"`
  Summary string `json:"summary,omitempty"`
}
```

#### Implementation Strategy

**Phase A — Core delegate tool (new file, minimal changes)**

1. **`pkg/agent/delegate_types.go`** — Define `DelegateResult`, `DelegateConfig`,
   `ToolCallRecord`.

2. **`pkg/agent/delegate_factory.go`** — `CreateDelegateAgent(parent *Agent, cfg
   DelegateConfig) (*Agent, error)`:
   - Clones the parent's config manager with optional role config overlay
   - Creates a new `*Agent` with the same `NewAgentWithLayers` path
   - Applies tool filtering from `DelegateConfig.Tools` or role config
   - Applies provider/model override if specified
   - Shares the parent's event bus (with delegate-scoped client ID)
   - Shares workspace root
   - Does NOT share conversation history, checkpoints, or metrics

3. **`pkg/agent/delegate_stream.go`** — Event bridge:
   - Wraps the delegate's streaming callback
   - Publishes events on the parent's event bus with a `delegate_id` and `role`
   - Tags so the parent (and webui) can distinguish delegate output from
     parent output
   - Skips verbose output (tool call details) and only surfaces milestones:
     spawn, tool_call_summary, complete

4. **`pkg/agent/tool_handlers_delegate.go`** — Tool handler:
   - Parses arguments
   - Calls `CreateDelegateAgent`
   - Runs `delegate.ProcessQuery(prompt)` (the existing non-interactive path)
   - Wraps the delegate's `PrintLine`/streaming to bridge events
   - Collects file changes from the delegate's `ChangeTracker`
   - Returns structured JSON `DelegateResult`

**Phase B — Structured event streaming (webui integration)**

5. **Event types** — Add `EventTypeDelegateActivity` events:
   ```
   delegate_spawn    → {delegate_id, role, provider, model}
   delegate_tool     → {delegate_id, tool_name, file (optional), success}
   delegate_progress → {delegate_id, iteration, tokens_so_far}
   delegate_complete → {delegate_id, result: DelegateResult}
   ```

6. **Webui rendering** — The webui already handles `subagent_activity`
   events. Add similar rendering for `delegate_activity` with the delegate's
   tool calls rendered as an expandable tree (not raw text output).

**Phase C — Bidirectional communication (future)**

7. **Input injection** — Allow the parent agent to inject follow-up messages
   into a running delegate (via the delegate's `inputInjectionChan`).
   This is not needed for Phase A (blocking delegate) but is the key
   differentiator from `run_subagent` for future interactive delegation.

### Key Differences from `run_subagent`

| Aspect | `run_subagent` | `delegate` |
|--------|---------------|------------|
| Execution | OS process (`exec.Command`) | In-process goroutine |
| Config | Child loads config from disk | Cloned from parent + role overlay |
| MCP | Child initializes its own MCP | Inherits parent MCP + role add-ons |
| State | Lost when child exits | Observable via ChangeTracker, bridgeable |
| Output | Raw stdout/stderr text | Structured events with tool call details |
| Workspace | Separate cwd | Shared workspace root |
| Credentials | Separate credential lookup | Shared credential store |
| Cost | Separate (not tracked by parent) | Tracked and returned to parent |
| Interrupt | Kill process | Cancel context (graceful) |

### Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `pkg/agent/delegate_types.go` | Create | Result types, config types |
| `pkg/agent/delegate_factory.go` | Create | In-process agent factory |
| `pkg/agent/delegate_stream.go` | Create | Event bridge |
| `pkg/agent/tool_handlers_delegate.go` | Create | Tool handler + registration |
| `pkg/agent/tool_definitions.go` | Modify | Register `delegate` tool |
| `pkg/agent/conversation_tools.go` | Modify | Allow `delegate` in tool set |
| `pkg/events/events.go` | Modify | Add delegate event types |
| `pkg/agent/delegate_test.go` | Create | Unit tests |

---

## Feature 2: Extend Configuration (Collaborative Role Configs)

### Problem

Users cannot customize sprout's capabilities at runtime. If a user needs a
specialized tool set, custom MCP servers, or a different provider/model combo
for a specific type of work, they must manually edit `config.json` — which is
fragile, not workspace-scoped, and not role-aware.

The existing `SubagentType` config partially addresses this by defining roles
with tool allowlists, but it is limited:
- No MCP server additions per role
- No custom provider/model presets per role
- No skill pre-loading per role
- No system prompt customization per role beyond static files
- Roles are global only (not workspace-scoped)
- No interactive creation flow

### Proposed Design

An `/extend` slash command (user-activated) that opens a collaborative
configuration session. The agent and user work together to define a new
"extended configuration" — a role-specific config derived from the base
configuration with additions and modifications. This extended config is saved
as a separate file and can be referenced by role name when spawning
subagents or delegates.

#### Config File Layout

```
~/.sprout/
  config.json                          # Base configuration (existing)
  roles/                               # Extended role configurations
    api-specialist.json                # Role: API specialist config
    frontend-dev.json                  # Role: Frontend developer config
    typescript-expert.json             # Role: TS expert config
    deployment-engineer.json           # Role: Deployment config

{workspace}/.sprout/                   # Workspace overrides (existing)
  config.json                          # Workspace config (existing)
  roles/                               # Workspace-scoped role configs
    ci-fix.json                        # Workspace-specific role
```

#### Extended Config Schema

```json
{
  "_meta": {
    "name": "api-specialist",
    "description": "Specialized for REST API development with OpenAPI validation",
    "extends": "base",
    "created_at": "2026-04-28T20:00:00Z",
    "version": 2
  },
  "provider": "openrouter",
  "model": "claude-sonnet-4-20250514",
  "system_prompt_override": "You are an API specialist. Always validate against OpenAPI specs...",
  "system_prompt_append": "\n\n## API Specialist Rules\n- Always generate OpenAPI specs\n- Use Zod for runtime validation",
  "tools": {
    "allowlist": [
      "shell_command", "read_file", "write_file", "edit_file",
      "search_files", "run_subagent", "delegate", "mcp_tools"
    ],
    "denylist": []
  },
  "mcp": {
    "servers": {
      "openapi-validator": {
        "command": "npx",
        "args": ["openapi-lint-server"],
        "env": {}
      },
      "postgres-explorer": {
        "command": "npx",
        "args": ["@anthropic/mcp-server-postgres", "postgresql://..."],
        "env": {}
      }
    }
  },
  "skills": {
    "active": ["project-planning"]
  },
  "constraints": {
    "max_iterations": 200,
    "max_tokens_per_call": 8000
  }
}
```

#### The `/extend` Slash Command Flow

```
User types: /extend api-specialist
         ↓
Agent sees a special system prompt:
  "The user wants to create an extended configuration called 'api-specialist'.
   Help them define the role by asking about:
   1. What kind of work does this role focus on?
   2. Which provider/model should it use? (default: inherit from current)
   3. Which tools should it have access to? (default: all current tools)
   4. Should it add any MCP servers? (provide examples of useful ones)
   5. Should it pre-activate any skills?
   6. Any custom system prompt additions?
   7. Any constraints (max iterations, token budget)?"
         ↓
Agent and user collaborate to define the config
         ↓
Agent generates the JSON config and presents it for review
         ↓
User approves (or requests edits)
         ↓
Config saved to ~/.sprout/roles/api-specialist.json
         ↓
Role is now available:
  - run_subagent(role="api-specialist", prompt="...")
  - delegate(role="api-specialist", prompt="...")
  - ApplyPersona("api-specialist") loads it as the active persona
```

#### Configuration Integration

Extended role configs integrate with the existing config system via a **role
resolution chain**:

```
1. Check workspace roles: {workspace}/.sprout/roles/{role}.json
2. Check global roles: ~/.sprout/roles/{role}.json
3. Check subagent_types in config.json (existing, backward compat)
4. Check built-in defaults (existing)
```

When a role is resolved, its config is **merged on top of** the base config
(user's global + workspace config). This means:
- Provider/model in role overrides base
- MCP servers in role are **added to** base MCP servers (merge, not replace)
- Tool allowlist in role filters the available tools
- Skills in role are **added to** base skills
- System prompt: `base + role_append` (or role override replaces entirely)

#### Key Components

| Component | Location | Responsibility |
|-----------|----------|----------------|
| `RoleConfig` | `pkg/configuration/role.go` | Role schema, load, merge |
| `RoleManager` | `pkg/configuration/role_manager.go` | Resolution chain, caching |
| `ExtendHandler` | `pkg/agent/extend_handler.go` | `/extend` command logic |
| Role loading in `SubagentType` | Modify `config.go` | Extend `GetSubagentType` to check roles/ |
| Role loading in `NewAgentWithLayers` | Modify `agent.go` | Accept optional role config layer |

#### Integration with Delegate Tool

The `delegate` tool's `role` parameter resolves through the same chain:

```go
func (a *Agent) resolveRoleConfig(role string) (*configuration.RoleConfig, error) {
    // 1. Check workspace roles
    // 2. Check global roles
    // 3. Check subagent_types (backward compat)
    // 4. Error if not found
}

func (a *Agent) createAgentWithRole(baseConfig *configuration.Config, role string) (*configuration.Config, error) {
    roleConfig, err := a.resolveRoleConfig(role)
    // Merge: base + role overlay
    return configuration.MergeRoleConfig(baseConfig, roleConfig)
}
```

#### Required Config Changes

`pkg/configuration/config.go` needs a `RolesPath` concept:

```go
const RolesDirName = "roles"

func GetGlobalRolesDir() (string, error) {
    configDir, err := GetConfigDir()
    if err != nil {
        return "", err
    }
    return filepath.Join(configDir, RolesDirName), nil
}

func GetWorkspaceRolesDir(workspaceRoot string) string {
    return filepath.Join(workspaceRoot, ConfigDirName, RolesDirName)
}
```

`pkg/configuration/role.go` — new file:

```go
type RoleConfig struct {
    Meta                 RoleMeta              `json:"_meta"`
    Provider             string                `json:"provider,omitempty"`
    Model                string                `json:"model,omitempty"`
    SystemPromptOverride string                `json:"system_prompt_override,omitempty"`
    SystemPromptAppend   string                `json:"system_prompt_append,omitempty"`
    Tools                RoleToolsConfig       `json:"tools,omitempty"`
    MCP                  mcp.MCPConfig         `json:"mcp,omitempty"`
    Skills               RoleSkillsConfig      `json:"skills,omitempty"`
    Constraints          RoleConstraints       `json:"constraints,omitempty"`
}

type RoleToolsConfig struct {
    Allowlist []string `json:"allowlist,omitempty"`
    Denylist  []string `json:"denylist,omitempty"`
}

type RoleConstraints struct {
    MaxIterations   int `json:"max_iterations,omitempty"`
    MaxTokensPerCall int `json:"max_tokens_per_call,omitempty"`
}
```

`pkg/configuration/role_manager.go` — new file:

```go
type RoleManager struct {
    globalRolesDir  string
    workspaceRolesDir string
    cache           map[string]*RoleConfig
    cacheMu         sync.RWMutex
}

// Resolve returns the RoleConfig for a given role name,
// checking workspace → global → subagent_types → defaults.
func (rm *RoleManager) Resolve(role string) (*RoleConfig, error)

// List returns all available role names (workspace + global + subagent_types).
func (rm *RoleManager) List() []string

// Save writes a role config to the appropriate directory.
func (rm *RoleManager) Save(role string, config *RoleConfig, workspaceScoped bool) error
```

### Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `pkg/configuration/role.go` | Create | `RoleConfig` schema + merge logic |
| `pkg/configuration/role_manager.go` | Create | Role resolution chain + save |
| `pkg/configuration/role_test.go` | Create | Tests for role resolution + merge |
| `pkg/agent/extend_handler.go` | Create | `/extend` slash command handler |
| `pkg/agent/extend_handler_test.go` | Create | Tests for extend flow |
| `pkg/agent/conversation_handler.go` | Modify | Wire `/extend` into command routing |
| `pkg/configuration/config.go` | Modify | Add `RolesDirName`, utility functions |
| `pkg/agent/persona.go` | Modify | `GetSubagentType` → check RoleManager first |
| `pkg/webui/settings_*.go` | Modify | API endpoints for role CRUD |
| `webui/src/...` | Modify | UI for browsing/managing roles (future) |

---

## Dependency Order

```
Feature 2 (Extend Config) should be implemented FIRST because:

  1. The delegate tool (Feature 1) uses role configs from Feature 2
  2. The role resolution chain is needed before delegate can accept a
     "role" parameter
  3. The /extend command creates the configs that delegate consumes
  4. Without roles, delegate is just delegate-without-configs (useful
     but limited)

Recommended implementation order:

  Phase 1: Role config infrastructure (Feature 2 foundation)
    - role.go, role_manager.go, config.go changes
    - Merging, saving, loading, resolution chain
    - Backward compat with existing subagent_types

  Phase 2: /extend slash command (Feature 2 UX)
    - extend_handler.go
    - Conversation handler wiring
    - Agent prompt for collaborative creation

  Phase 3: Delegate tool (Feature 1)
    - delegate_types.go, delegate_factory.go, delegate_stream.go
    - tool_handlers_delegate.go
    - Integration with RoleManager for role resolution

  Phase 4: WebUI role management + delegate rendering
    - Settings API endpoints for role CRUD
    - Delegate activity event rendering in webui
```

---

## Open Questions

1. **Should delegates share the parent's MCP connections or create their own?**
   Sharing is faster but creates coupling. Creating new ones per delegate is
   cleaner but adds latency. Recommend: create new MCP instances scoped to the
   role config (merged MCP = base servers + role servers), but share the
   credential store.

2. **Should delegates be able to delegate further (nested)?**
   Yes — this is the "orchestrator of orchestrators" pattern. Set a max depth
   (e.g., 3) via `SPROUT_MAX_DELEGATE_DEPTH` env var with default 3.

3. **How are role configs versioned?**
   Simple: `_meta.version` field. When loading, if schema version mismatches,
   apply migration or reject with error. Start at version 2 (matching config
   version).

4. **Should roles export their file changes back to the parent?**
   Yes — the delegate's `ChangeTracker` should be inspectable by the parent
   after completion. The parent can then review and decide whether to commit.

5. **Should `/extend` work from the webui?**
   Initially, `/extend` is a text-based interactive flow (slash command + agent
   conversation). Webui support requires rendering the config review step
   as a form/modal — defer to Phase 4.
