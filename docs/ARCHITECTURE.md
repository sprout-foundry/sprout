# Ledit Architecture

Project architecture documentation for the ledit AI-powered code editing and assistance tool.

---

## Package Layout

The `pkg/` directory contains the core modular components of ledit:

### Agent System

| Package | Description |
|---------|-------------|
| `pkg/agent/` | Core agent orchestration and conversation handling |
| `pkg/agent_api/` | LLM provider integrations and API clients |
| `pkg/agent_commands/` | CLI command implementations (commit, shell, etc.) |
| `pkg/agent_providers/` | Generic provider factory and configuration |
| `pkg/agent_tools/` | Built-in tools (file operations, web search, shell execution) |
| `pkg/personas/` | Agent persona definitions |

### Commands & Tools

| Package | Description |
|---------|-------------|
| `pkg/commands/` | Command utilities and helpers (sessions) |
| `pkg/tools/` | Tool registry and execution framework |

### Code Analysis & Review

| Package | Description |
|---------|-------------|
| `pkg/codereview/` | Code review functionality |
| `pkg/validation/` | Code validation (gofmt/goimports for Go) |
| `pkg/security/` | Credential scanning and safety checks |
| `pkg/spec/` | Specification extraction and scope validation |

### Configuration & State

| Package | Description |
|---------|-------------|
| `pkg/configuration/` | Configuration management and API keys |
| `pkg/credentials/` | Credential store for API key management |
| `pkg/filesystem/` | Workspace filesystem context and security |
| `pkg/history/` | Change tracking and rollback functionality |

### Terminal UI

| Package | Description |
|---------|-------------|
| `pkg/console/` | Terminal UI, streaming, ANI handling, mouse support |
| `pkg/ui/` | Terminal UI framework with themes and dropdowns |

### Indexing & Discovery

| Package | Description |
|---------|-------------|
| `pkg/index/` | Workspace indexing |
| `pkg/filediscovery/` | File discovery and indexing |

### Git Integration

| Package | Description |
|---------|-------------|
| `pkg/git/` | Git integration |

### MCP & External Services

| Package | Description |
|---------|-------------|
| `pkg/mcp/` | Model Context Protocol client implementation |
| `pkg/webcontent/` | Web content fetching |

### LLM & Model Management

| Package | Description |
|---------|-------------|
| `pkg/model_settings/` | Model-specific parameter resolution |
| `pkg/providercatalog/` | Provider catalog with model lists and costs |
| `pkg/factory/` | Provider client factory |

### Prompts & Text

| Package | Description |
|---------|-------------|
| `pkg/prompts/` | Prompt templates |
| `pkg/text/` | Text processing utilities |

### Utilities & Infrastructure

| Package | Description |
|---------|-------------|
| `pkg/events/` | Event bus system |
| `pkg/interfaces/` | Common interfaces and abstractions |
| `pkg/logging/` | Structured logging with process step tracking |
| `pkg/pythonruntime/` | Python runtime detection and validation |
| `pkg/types/` | Common type definitions |
| `pkg/utils/` | Utility functions and helpers |
| `pkg/trace/` | Dataset tracing to JSONL files |
| `pkg/training/` | Training data export (ShareGPT, OpenAI, Alpaca) |

### Web UI

| Package | Description |
|---------|-------------|
| `pkg/webui/` | React-based Web UI server (embedded assets, WebSocket API, Git/Settings/Search/History APIs, terminal management) |

### Shell Integration

| Package | Description |
|---------|-------------|
| `pkg/zsh/` | Zsh command detection and auto-execution |

---

## Key Data Flow

1. **User Input** → CLI commands in `cmd/` parse and route to appropriate handlers
2. **Agent Processing** → `pkg/agent/` orchestrates request processing using LLM providers from `pkg/agent_api/`
3. **Context Building** → `pkg/filediscovery/` and `pkg/index/` select relevant files for LLM context
4. **Code Generation** → Agent generates code changes with workspace awareness via `pkg/agent_tools/`
5. **Change Management** → `pkg/history/` records modifications with rollback support
6. **Git Integration** → `pkg/git/` handles commits and version control operations

---

## Change Tracking System

The change tracking system provides comprehensive modification management:

- **Revision Tracking**: Every edit generates a revision ID
- **Change Recording**: All file modifications tracked in `.ledit/changes/`
- **Rollback Support**: Complete rollback capability for any changes
- **Diff Logs**: Per-change diff logs with original and updated files in `.ledit/changes/`
- **View History**: `ledit log` command to view changes
- **Rollback**: `ledit log --raw-log` for verbose logs and rollback support

---

## Workspace Files

### Project-Local `.ledit/` Directory

Located in the project root, contains workspace-specific metadata:

| File/Directory | Description |
|----------------|-------------|
| `config.json` | Local configuration overrides |
| `leditignore` | Ignore patterns (augments `.gitignore`) |
| `changes/` | Per-change diff logs with original and updated files |
| `memories/` | Persistent memory storage (markdown files) |
| `revisions/` | Per-session directories with instructions and LLM responses |
| `runlogs/` | JSONL workflow traces |
| `workspace.log` | Verbose execution log |

### Global `~/.ledit/` Directory

User-wide configuration and state:

| File | Description |
|------|-------------|
| `config.json` | Global configuration |
| `api_keys.json` | Secure API key storage |
| `mcp_config.json` | MCP server configuration |
| `memories/` | Persistent memory storage (markdown files) |

---

## Command Architecture

Main CLI commands in `cmd/`:

- **`agent`**: Interactive AI-powered code editing and assistance
- **`commit`**: AI-generated conventional commit for staged changes
- **`custom`**: Manage custom OpenAI-compatible providers
- **`diag`**: Show diagnostic information about configuration and environment
- **`export-training`**: Export session data to training formats
- **`log`**: View/revert change history
- **`mcp`**: Manage MCP servers
- **`plan`**: Planning and execution mode with todo creation
- **`review`**: LLM code review for staged Git changes
- **`shell`**: Generate shell scripts from natural language
- **`skill`**: Manage agent skills and conventions
- **`version`**: Print version, build, and platform information

---

## Configuration Layers

Configuration uses a layered approach:

1. **Global**: `~/.ledit/config.json` - User-wide defaults
2. **Project**: `./.ledit/config.json` - Project-specific overrides
3. **API Keys**: `~/.ledit/api_keys.json` - Secure credential storage

---

## Architecture Summary

- **Modular Architecture**: Clean separation between agent logic, UI components, and API providers
- **Provider Support**: Multi-provider LLM support (OpenAI, Ollama, DeepInfra, Cerebras, etc.)
- **Console UI**: Component-based terminal interface with proper input handling and display
- **Testing**: Python-based E2E test runner and Go unit tests for components
- **Streaming**: Real-time response streaming for improved user experience
- **Change Tracking**: Comprehensive revision tracking and rollback support
- **Persistent Memory**: Markdown-based memory system loaded into system prompt
- **Specification Validation**: Scope creep prevention through spec extraction

---

## File Structure Summary

```
ledit/
├── main.go                      # Entry point
├── cmd/                         # CLI subcommands
│   ├── agent.go                 # Interactive AI agent
│   ├── commit.go                # Git commit generation
│   ├── shell.go                 # Shell script generation
│   └── ...                      # Other commands
├── pkg/                         # Core packages
│   ├── agent/                   # Agent orchestration
│   ├── agent_api/               # LLM provider integrations
│   ├── agent_tools/             # Built-in tools
│   ├── console/                 # Terminal UI
│   ├── git/                     # Git integration
│   ├── history/                 # Change tracking
│   ├── index/                   # Workspace indexing
│   ├── mcp/                     # MCP client
│   ├── personas/                # Agent personas
│   ├── tools/                   # Tool registry
│   ├── webui/                   # React Web UI server
│   └── ...                      # Other packages
├── .ledit/                      # Project-local workspace
│   ├── config.json              # Local config
│   ├── changes/                 # Change diffs
│   ├── memories/                # Persistent memories
│   └── ...
├── ~/.ledit/                    # Global configuration
│   ├── config.json              # Global config
│   ├── api_keys.json            # API keys
│   └── ...
└── docs/                        # Documentation
    └── ARCHITECTURE.md          # This file
```
