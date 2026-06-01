# SP-007: Extend Configuration — Role-Based Configs

**Status:** 📋 Proposed  
**Depends on:** None (foundation for SP-006)  
**Priority:** High  
**Effort Estimate:** ~2-3 weeks (2 phases)

## Problem

Users cannot customize sprout's capabilities at runtime. The existing `SubagentType` config allows defining personas with tool allowlists and system prompts, but lacks:

- Per-role MCP server additions
- Per-role provider/model presets
- Per-role skill pre-loading
- Workspace-scoped roles (global only today)
- No interactive creation flow

Users must manually edit `config.json` for any customization — fragile, not role-aware, and not workspace-scoped.

## Proposed Solution

1. A **`/extend` slash command** (user-activated) that opens a collaborative configuration session where the agent and user define a new role together.
2. A **role config file system** stored in `~/.sprout/roles/` (global) and `{workspace}/.sprout/roles/` (workspace-scoped).
3. A **RoleManager** that resolves role names through a chain: workspace → global → subagent_types → built-in defaults.

## Config File Layout

```
~/.sprout/
  config.json                          # Base configuration (existing)
  roles/                               # Global extended role configs
    api-specialist.json
    frontend-dev.json
    typescript-expert.json

{workspace}/.sprout/
  config.json                          # Workspace config (existing)
  roles/                               # Workspace-scoped role configs
    ci-fix.json
```

## Role Config Schema

```json
{
  "_meta": {
    "name": "api-specialist",
    "description": "REST API development with OpenAPI validation",
    "extends": "base",
    "created_at": "2026-04-28T20:00:00Z",
    "version": 2
  },
  "provider": "openrouter",
  "model": "claude-sonnet-4-20250514",
  "system_prompt_override": "You are an API specialist...",
  "system_prompt_append": "\n\n## API Rules\n- Always validate against OpenAPI specs",
  "tools": {
    "allowlist": ["shell_command", "read_file", "write_file", "edit_file", "run_subagent"],
    "denylist": ["commit", " browse_url"]
  },
  "mcp": {
    "servers": {
      "openapi-validator": {
        "command": "npx",
        "args": ["openapi-lint-server"],
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

## Role Resolution Chain

```
1. {workspace}/.sprout/roles/{role}.json    (workspace-scoped)
2. ~/.sprout/roles/{role}.json              (global)
3. config.json → subagent_types[role]      (backward compat)
4. built-in defaults                       (Coder, Tester, etc.)
```

When resolved, role config **merges on top of** the base config:
- `provider` / `model` → override base
- `mcp.servers` → **additive merge** (base servers + role servers)
- `tools.allowlist` → filters available tools
- `skills.active` → **additive merge** (base skills + role skills)
- `system_prompt_append` → appended to base prompt
- `system_prompt_override` → replaces prompt entirely (if set)

## /extend Slash Command Flow

```
User types: /extend api-specialist
      ↓
Agent receives system prompt with guided questions:
  "Help define the 'api-specialist' role:
   1. What kind of work? 2. Provider/model? 3. Tools?
   4. MCP servers to add? 5. Skills to pre-activate?
   6. System prompt additions? 7. Constraints?"
      ↓
Collaborative conversation (agent asks, user answers, refine)
      ↓
Agent generates role JSON, presents for review
      ↓
User approves → saved to ~/.sprout/roles/api-specialist.json
      ↓
Role available for:
  - run_subagent(persona="api-specialist", ...)
  - ApplyPersona("api-specialist")
```

## Go Types

```go
// pkg/configuration/role.go

type RoleMeta struct {
    Name        string    `json:"name"`
    Description string    `json:"description"`
    Extends     string    `json:"extends"`       // "base" or another role name
    CreatedAt   time.Time `json:"created_at"`
    Version     int       `json:"version"`
}

type RoleConfig struct {
    Meta                 RoleMeta         `json:"_meta"`
    Provider             string           `json:"provider,omitempty"`
    Model                string           `json:"model,omitempty"`
    SystemPromptOverride string           `json:"system_prompt_override,omitempty"`
    SystemPromptAppend   string           `json:"system_prompt_append,omitempty"`
    Tools                RoleToolsConfig  `json:"tools,omitempty"`
    MCP                  mcp.MCPConfig    `json:"mcp,omitempty"`
    Skills               RoleSkillsConfig `json:"skills,omitempty"`
    Constraints          RoleConstraints  `json:"constraints,omitempty"`
}

type RoleToolsConfig struct {
    Allowlist []string `json:"allowlist,omitempty"`
    Denylist  []string `json:"denylist,omitempty"`
}

type RoleConstraints struct {
    MaxIterations    int `json:"max_iterations,omitempty"`
    MaxTokensPerCall int `json:"max_tokens_per_call,omitempty"`
}

// pkg/configuration/role_manager.go

type RoleManager struct {
    globalRolesDir    string
    workspaceRolesDir string
    cache             map[string]*RoleConfig
    cacheMu           sync.RWMutex
}

func (rm *RoleManager) Resolve(role string) (*RoleConfig, error)
func (rm *RoleManager) List() []string
func (rm *RoleManager) Save(role string, config *RoleConfig, workspaceScoped bool) error
func (rm *RoleManager) Delete(role string, workspaceScoped bool) error
```

## Config Merge Function

```go
// pkg/configuration/role.go

func MergeRoleConfig(base *Config, role *RoleConfig) *Config {
    result := base.DeepCopy()

    if role.Provider != "" { result.LastUsedProvider = role.Provider }
    if role.Model != ""    { /* set model for provider */ }

    // MCP: additive merge
    if role.MCP.Servers != nil {
        for name, server := range role.MCP.Servers {
            result.MCP.Servers[name] = server
        }
    }

    // System prompt
    if role.SystemPromptOverride != "" {
        result.SystemPromptOverride = role.SystemPromptOverride
    }

    // Store role reference for persona system
    // (SubagentType synthesis from RoleConfig)

    return result
}
```

## Integration Points

### Persona System (`pkg/agent/persona.go`)

Extend `GetSubagentType()` to check RoleManager before falling back to existing `subagent_types`:

```go
func (c *Config) GetSubagentType(id string) *SubagentType {
    // 1. Check RoleManager (new)
    // 2. Check SubagentTypes map (existing, backward compat)
    // 3. Check defaults (existing)
}
```

A RoleConfig is converted to a SubagentType for backward compatibility:

```go
func roleToSubagentType(role *RoleConfig) *SubagentType {
    return &SubagentType{
        ID:               role.Meta.Name,
        Name:             role.Meta.Description,
        Provider:         role.Provider,
        Model:            role.Model,
        SystemPromptText: role.SystemPromptOverride,
        SystemPromptAppend: role.SystemPromptAppend,
        AllowedTools:     role.Tools.Allowlist,
        Enabled:          true,
    }
}
```

### WebUI Settings API

New endpoints:
- `GET /api/settings/roles` — list all roles
- `GET /api/settings/roles/{name}` — get role config
- `PUT /api/settings/roles/{name}` — create/update role
- `DELETE /api/settings/roles/{name}` — delete role

## Implementation Phases

### Phase 1: Role Config Infrastructure

**New files:**
- `pkg/configuration/role.go` — `RoleConfig`, `RoleMeta`, merge logic
- `pkg/configuration/role_manager.go` — resolution chain, save, list
- `pkg/configuration/role_test.go` — unit tests

**Modified files:**
- `pkg/configuration/config.go` — add `RolesDirName` constant, utility functions
- `pkg/agent/persona.go` — extend `GetSubagentType` to check RoleManager

### Phase 2: /extend Slash Command

**New files:**
- `pkg/agent/extend_handler.go` — `/extend` command logic
- `pkg/agent/extend_handler_test.go` — tests

**Modified files:**
- `pkg/agent/conversation_handler.go` — wire `/extend` into command routing

### Phase 3 (Future): WebUI Role Management

- Settings panel for role CRUD
- Visual role editor (form-based, not raw JSON)
- Role selector in agent persona picker

## Files Reference

| File | Action |
|------|--------|
| `pkg/configuration/config.go` | Modify: add RolesDirName, helper funcs |
| `pkg/configuration/role.go` | Create: RoleConfig schema + merge |
| `pkg/configuration/role_manager.go` | Create: resolution chain + save/list/delete |
| `pkg/agent/persona.go` | Modify: GetSubagentType checks RoleManager |
| `pkg/agent/extend_handler.go` | Create: /extend slash command |
| `pkg/webui/settings_api_general.go` | Modify: role CRUD endpoints |
