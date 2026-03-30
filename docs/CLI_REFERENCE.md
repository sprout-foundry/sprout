# CLI Reference

Command-line interface reference for `ledit`. This document covers all CLI commands, flags, and interactive features.

## Quick Start

```bash
# Start interactive agent mode (Web UI enabled by default)
ledit

# Run a specific task
ledit agent "Add JWT auth to API"
ledit agent --skip-prompt "Implement user authentication"

# Generate a commit message
ledit commit
ledit commit --skip-prompt  # Auto-review and commit

# Generate a shell script
ledit shell "backup all .go files to a timestamped archive"

# View change history
ledit log
ledit log --raw-log  # Verbose logs

# Manage MCP servers
ledit mcp list
ledit mcp add
```

## Commands

### `ledit agent`

Core AI agent for analysis, generation, explanation, and orchestration.

**Basic Usage:**
```bash
ledit agent [intent] [flags]
```

**Examples:**
```bash
ledit agent  # Interactive mode
ledit agent "Add JWT auth to API" --skip-prompt --model "deepinfra:qwen3-coder"
ledit agent --dry-run "Refactor main.go for modularity"
ledit agent "Analyze this codebase" --persona researcher
```

### `ledit commit`

AI-generated conventional commit for staged Git changes.

**Basic Usage:**
```bash
ledit commit [flags]
```

**Examples:**
```bash
ledit commit --dry-run
ledit commit --skip-prompt  # Auto-review and commit
```

### `ledit review`

LLM code review for staged Git changes.

**Basic Usage:**
```bash
ledit review [flags]
```

**Examples:**
```bash
ledit review --model "openai:gpt-5"
```

### `ledit shell`

Generate shell scripts from natural language descriptions (no execution).

**Basic Usage:**
```bash
ledit shell [description] [flags]
```

**Examples:**
```bash
ledit shell "Setup React dev environment and install dependencies"
```

### `ledit log`

View and revert change history.

**Basic Usage:**
```bash
ledit log [flags]
```

**Examples:**
```bash
ledit log  # Summary
ledit log --raw-log  # Verbose .ledit/workspace.log
```

### `ledit mcp`

Manage MCP (Model Context Protocol) servers.

**Basic Usage:**
```bash
ledit mcp <command> [flags]
```

**Commands:**
```bash
ledit mcp add      # Interactive setup (GitHub or custom)
ledit mcp list     # Show server status
ledit mcp test [name]  # Verify connection
ledit mcp remove [name]  # Remove a server
```

### `ledit custom`

Manage custom OpenAI-compatible providers.

**Basic Usage:**
```bash
ledit custom [command] [flags]
```

### `ledit diag`

Show diagnostic information about configuration and environment.

**Basic Usage:**
```bash
ledit diag [flags]
```

### `ledit version`

Print version, build, and platform information.

**Basic Usage:**
```bash
ledit version
```

### `ledit plan`

Planning and execution mode with todo creation.

**Basic Usage:**
```bash
ledit plan [idea] [flags]
```

### `ledit skill`

Manage agent skills and conventions.

**Basic Usage:**
```bash
ledit skill [command] [flags]
```

### `ledit export-training`

Export session data to training formats (ShareGPT, OpenAI, Alpaca).

**Basic Usage:**
```bash
ledit export-training [flags]
```

---

## Advanced Agent Flags

### Session Management

| Flag | Description | Example |
|------|-------------|---------|
| `--session-id <id>` | Specify a session identifier | `ledit agent --session-id my-session "continue work"` |
| `--last-session` | Resume previous session | `ledit agent --last-session "resume"` |

### Persona Selection

| Flag | Description | Example |
|------|-------------|---------|
| `--persona <name>` | Select agent persona | `ledit agent --persona coder "implement feature"` |

**Available Personas:**
- `orchestrator` — Process-oriented planning and delegation
- `general` — General-purpose tasks (default)
- `coder` — Feature implementation and production code
- `refactor` — Behavior-preserving refactoring
- `debugger` — Bug investigation and root cause analysis
- `tester` — Unit test writing and test coverage
- `code_reviewer` — Code review and security review
- `researcher` — Combined local codebase analysis and external research
- `web_scraper` — Web extraction and structured content collection
- `computer_user` — System administration and engineering execution

### Performance & Safety

| Flag | Description | Example |
|------|-------------|---------|
| `--no-connection-check` | Skip provider connection check | `ledit agent --no-connection-check "task"` |
| `--max-iterations <n>` | Limit iterations (default: 1000) | `ledit agent --max-iterations 50 "task"` |
| `--no-stream` | Disable streaming for scripts | `LEDIT_NO_STREAM=1 ledit agent "task"` |
| `--no-subagents` | Disable subagent tools | `ledit agent --no-subagents "task"` |
| `--unsafe` | Bypass security checks (use with caution) | `ledit agent --unsafe "task"` |

### Custom Prompts

| Flag | Description | Example |
|------|-------------|---------|
| `--system-prompt <file>` | Use custom system prompt from file | `ledit agent --system-prompt prompts/custom.md "task"` |
| `--system-prompt-str <text>` | Inline custom system prompt | `ledit agent --system-prompt-str "You are a security expert..." "task"` |

### Resource Management

| Flag | Description | Example |
|------|-------------|---------|
| `--resource-directory <dir>` | Store web/vision resources | `ledit agent --resource-directory captures "task"` |
| `--workflow-config <file>` | Run workflow configuration | `ledit agent --workflow-config examples/agent_workflow.json "task"` |
| `--trace-dataset-dir <dir>` | Enable dataset tracing | `ledit agent --trace-dataset-dir traces "task"` |
| `--prompt-stdin` | Read prompt from stdin | `echo "task" | ledit agent --prompt-stdin` |

### Model Selection

| Flag | Description | Example |
|------|-------------|---------|
| `--model <provider:model>` | Specify model | `ledit agent --model "deepinfra:qwen3-coder" "task"` |
| `--provider <name>` | Specify provider | `ledit agent --provider ollama "task"` |

### Web UI Control

| Flag | Description | Example |
|------|-------------|---------|
| `--no-web-ui` | Disable Web UI | `ledit agent --no-web-ui "task"` |
| `--web-port <port>` | Use custom port | `ledit agent --web-port 8080` |
| `-d`, `--daemon` | Daemon mode (keep Web UI running) | `ledit agent -d` |

---

## Slash Commands in Interactive Mode

In interactive `ledit` or `ledit agent`, use `/` for commands (tab-complete).

### Session & History

| Command | Description |
|---------|-------------|
| `/clear` | Clear conversation history |
| `/sessions [session_num]` | Show and load previous conversation sessions |
| `/log` | View changes |

### Models & Providers

| Command | Description |
|---------|-------------|
| `/models [select\|<id>]` | List/select models (e.g., `/models select` for interactive dropdown) |
| `/providers [select\|<name>]` | Switch providers (e.g., `/providers ollama`) |

### Agent Features

| Command | Description |
|---------|-------------|
| `/commit` | Generate commit message |
| `/shell <desc>` | Generate shell script |
| `/init` | Regenerate workspace context |
| `/mcp` | Manage MCP servers |
| `/exit` | Quit session |

### Skills & Configuration

| Command | Description |
|---------|-------------|
| `/skills` | List and load agent skills |
| `/export` | Export training data |
| `/plan [idea]` | Start planning mode |
| `/custom` | Manage custom providers |
| `/diag` | Show diagnostic information |

### Help

| Command | Description |
|---------|-------------|
| `/help` | Show usage and all slash commands |

---

## Agent Personas

`ledit` supports 10 specialized personas, each optimized for different types of tasks. See [`docs/subagent_personas.md`](subagent_personas.md) for detailed descriptions.

| Persona | Description |
|---------|-------------|
| `orchestrator` | Process-oriented planning and delegation |
| `general` | General-purpose tasks (default) |
| `coder` | Feature implementation and production code |
| `refactor` | Behavior-preserving refactoring specialist |
| `debugger` | Bug investigation and root cause analysis |
| `tester` | Unit test writing and test coverage |
| `code_reviewer` | Code review and security review |
| `researcher` | Combined local codebase analysis and external research |
| `web_scraper` | Web extraction and structured content collection |
| `computer_user` | System administration and engineering execution |

**Using Personas:**
```bash
ledit agent --persona coder "implement feature"
ledit agent --persona debugger "fix this bug"
ledit agent --persona code_reviewer "review my code"
ledit agent --persona researcher "analyze codebase and external sources"
ledit agent --persona web_scraper "extract structured content from web pages"
ledit agent --persona refactor "refactor code while preserving behavior"
ledit agent --persona computer_user "execute system administration tasks"
```

---

## Memory System

The memory system persists learned information across all conversations. Memories are markdown files stored in `~/.ledit/memories/` and automatically loaded into the system prompt.

### Memory Commands

| Command | Description |
|---------|-------------|
| `add_memory` | Save new memories with descriptive names |
| `read_memory` | Read a specific memory |
| `list_memories` | List all saved memories |
| `delete_memory` | Delete a memory |

**Usage in Interactive Mode:**
```
ledit> add_memory "git-safety" "Never force-push to shared branches"
ledit> list_memories
ledit> read_memory "git-safety"
ledit> delete_memory "git-safety"
```

**Use Cases:**
- Store project conventions
- Save user preferences
- Remember discovered patterns
- Document workflow guidelines

---

## Ignoring Files

Add patterns to `.ledit/leditignore` to exclude files from processing. The file respects `.gitignore` patterns.

**Setup:**
```bash
# Via agent or manually
echo "dist/" >> .ledit/leditignore
echo "*.log" >> .ledit/leditignore
```

**Example `.ledit/leditignore`:**
```
# Build artifacts
dist/
build/
*.min.js

# Logs
*.log
logs/

# IDE
.idea/
.vscode/
*.swp

# OS files
.DS_Store
Thumbs.db
```

---

## Tool Suite

`ledit` includes a comprehensive built-in tool suite for all development tasks.

### File Operations

| Tool | Description |
|------|-------------|
| `edit_file` | Edit files with intelligent context |
| `read_file` | Read file contents with optional line ranges |
| `write_file` | Create or overwrite files |
| `search_files` | Search text in files using patterns |

### Structured File Operations

| Tool | Description |
|------|-------------|
| `write_structured_file` | Write schema-validated JSON/YAML files |
| `patch_structured_file` | Apply JSON Patch operations to JSON/YAML files |

### Web & Vision

| Tool | Description |
|------|-------------|
| `browse_url` | Open URLs in headless browser for screenshot/DOM/text extraction |
| `web_search` | Real-time web search for grounding knowledge |
| `analyze_ui_screenshot` | Analyze UI screenshots, mockups, or HTML files |
| `analyze_image_content` | Extract text/code from images |

### Shell & Git

| Tool | Description |
|------|-------------|
| `shell_command` | Execute shell commands (allowlisted) |
| `git` | Execute git write operations (requires approval) |

### Memory & Skills

| Tool | Description |
|------|-------------|
| `add_memory` / `read_memory` / `list_memories` / `delete_memory` | Persistent memory system |
| `list_skills` / `activate_skill` | Skill management for loading instruction bundles |

### Change History

| Tool | Description |
|------|-------------|
| `view_history` | View change history |
| `rollback_changes` | Revert changes |

### Validation & Review

| Tool | Description |
|------|-------------|
| `self_review` | Review agent's work against canonical specification |

### Todo Management

| Tool | Description |
|------|-------------|
| `todo_write` / `todo_read` | Todo management for breaking down tasks |

---

## MCP Server Integration

MCP (Model Context Protocol) extends `ledit` with external tools and services.

### Quick Start

```bash
# Interactive setup
ledit mcp add

# List servers
ledit mcp list

# Test connection
ledit mcp test [name]

# Remove server
ledit mcp remove [name]
```

### Configuration

- Config file: `~/.ledit/mcp_config.json`
- Use in agent: `Create GitHub PR for feature #WS`

### Features

- Connect to external tools and services
- GitHub integration (repos, issues, PRs)
- Custom server support
- Auto-discovery and auto-start options

See [docs/MCP_INTEGRATION.md](MCP_INTEGRATION.md) for detailed MCP setup.

---

## Workspace Initialization

The `.ledit/` directory is automatically created when you first run `ledit` commands. It contains:

- `config.json` — Local configuration overrides
- `leditignore` — Ignore patterns (augments `.gitignore`)
- `changes/` — Per-change diff logs
- `memories/` — Persistent memory storage (markdown files)
- `revisions/` — Per-session directories with instructions and LLM responses
- `runlogs/` — JSONL workflow traces
- `workspace.log` — Verbose execution log
- `workspace_index/` — Workspace context index

The index is automatically updated on commands for fresh context.

---

## Related Documentation

- [Configuration](CONFIGURATION.md) — Config files, environment variables, and CI/CD
- [Web UI](WEB_UI.md) — Web UI features and SSH tunneling
- [Architecture](ARCHITECTURE.md) — Project structure and design
- [MCP Integration](MCP_INTEGRATION.md) — MCP server setup
- [Agent Workflow](AGENT_WORKFLOW.md) — Workflow configuration
- [Provider Catalog](PROVIDER_CATALOG.md) — Available providers and models
- [Testing](TESTING.md) — Test strategy and commands
