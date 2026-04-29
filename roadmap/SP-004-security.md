# SP-004: Security, Validation & MCP

**Status:** ✅ Active  
**Location:** `pkg/security/`, `pkg/validation/`, `pkg/mcp/`, `pkg/agent_tools/security.go`  
**Test Coverage:** Good

## Current State

Three systems provide security checks, code validation, and MCP (Model Context Protocol) server integration.

## File Security (`pkg/security/`)

### System

- **Security concern detection** (`security_checks.go`): Scans file content for secrets, credentials, API keys, and sensitive patterns before writes
- **Output redaction** (`output_redactor.go`): `OutputRedactor` scans tool output for detected secrets and redacts them
- **Elevation gate** (`elevation.go`): `ElevationGate` manages user elevation decisions for privileged operations (persists across session)
- **Security concern types**: Hardcoded credentials, API keys, private keys, tokens, passwords, connection strings

### Workflow

```
File write tool called
  → security.CheckFileContentSecurity(filePath, content)
  → DetectSecurityConcernsWithContext(content, filePath)
  → For each concern:
    → Check if already ignored for this file
    → Build prompt with code snippet
    → Prompt user (CLI or WebUI via event bus)
    → If dismissed: track as ignored to prevent re-prompting
```

### Agent Integration

- `SecurityManager` (sub-manager) holds: `SecurityApprovalMgr`, `OutputRedactor`, `ElevationGate`, filesystem bypass state, ignored concerns map, webui client callback

## Tool Security Classification (`pkg/agent_tools/security.go`)

### System

Classifies tool calls by risk level before execution:

| Tool | Risk | Description |
|------|------|-------------|
| `shell_command` | `CRITICAL` | Must prompt for all destructive commands |
| `write_file`, `edit_file`, `patch_structured_file`, `write_structured_file` | `HIGH` | File writes |
| `browse_url`, `fetch_url` | `CAUTION` | External network access |
| Unregistered tools | `CAUTION` | Default for unknown tools |
| `read_file`, `search_files`, `git` | `SAFE` | Read-only operations |

### Classification Logic

```go
func ClassifyToolCall(toolName string, args map[string]interface{}) SecurityResult {
    switch toolName {
    case "shell_command": return classifyShellCommand(args)   // CRITICAL/DANGEROUS/SAFE
    case "write_file", "edit_file": ...   // HIGH (or CAUTION with confirmation)
    case "browse_url", "fetch_url": ...   // CAUTION
    default: return SecurityResult{        // CAUTION (recently tightened)
        Risk: SecurityCaution,
        Reasoning: "Unknown tool - requires approval",
        ShouldPrompt: true,
    }
}
```

## Validation (`pkg/validation/`)

### System

- **Syntax validation** for Go and TypeScript files
- **Async diagnostics** — runs language servers in background
- **Pre-write validation** — can be enabled to validate before file writes
- **Validator** passed to Agent struct, used by tool handlers

### Architecture

```
Tool handler calls write_file
  → a.validator.ValidateSyntax(filePath, content, language)
  → If errors: returns validation errors to agent
  → Agent can fix and retry
```

## MCP — Model Context Protocol (`pkg/mcp/`)

### System

MCP servers extend agent capabilities by providing additional tools at runtime (e.g., GitHub, databases, external APIs).

### Configuration

MCP servers defined in `config.json` → `mcp.servers`:

```json
{
  "mcp": {
    "enabled": true,
    "timeout": 30000000000,
    "auto_start": false,
    "auto_discover": true,
    "servers": {
      "github": {
        "command": "npx",
        "args": ["-y", "@anthropic/mcp-server-github"],
        "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "..." }
      }
    }
  }
}
```

### Lifecycle

1. **Init** — `MCPManager.Initialize()` starts configured servers
2. **Health check** — `CheckHealth()` pings each server
3. **Tool discovery** — Lists tools from each MCP server
4. **Tool execution** — Agent calls MCP tools via `mcp_tools` built-in tool
5. **Caching** — Tool list cached to avoid re-fetching on every API call

### Concurrency

- `MCPSubManager` (sub-manager) manages initialization mutex
- Servers started with timeout protection
- Tool calls go through the standard tool executor pipeline

### Integration Points

- **Agent:** `mcp.go` bridges Agent ↔ MCPSubManager ↔ MCPManager
- **Config:** `pkg/configuration/config.go` holds MCP config, merged across layers
- **Security:** MCP server secrets migrated to credential store
- **WebUI:** Settings panel for MCP server CRUD (`settings_api_mcp.go`)

###credential management

- MCP server credentials stored in `MCPServerConfig.Credentials` map
- HTTP client generic auth headers via `buildAuthHeaders()`
- API endpoints: `GET/PUT/DELETE /api/settings/mcp/servers/{name}/credentials`

## Open Work

No open security items in TODO.md — system is mature and well-tested.

## Key Files

| File | Purpose |
|------|---------|
| `pkg/security/security_checks.go` | Security concern detection patterns |
| `pkg/security/output_redactor.go` | Output scanning and redaction |
| `pkg/security/elevation.go` | Privilege escalation management |
| `pkg/agent_tools/security.go` | Tool call classification |
| `pkg/validation/` | Syntax validation + diagnostics |
| `pkg/mcp/manager.go` | MCP server lifecycle |
| `pkg/mcp/types.go` | MCP types (config, tools) |
| `pkg/agent/mcp.go` | Agent ↔ MCP bridge |
| `pkg/agent/submanager_mcp.go` | MCPSubManager interface |
| `pkg/webui/settings_api_mcp.go` | MCP server CRUD API |
