# CLI Reference

Command-line interface reference for `sprout`. This document covers all CLI commands, flags, and interactive features.

## Quick Start

```bash
# Start interactive agent mode (Web UI enabled by default)
sprout

# Run a specific task
sprout agent "Add JWT auth to API"
sprout agent --skip-prompt "Implement user authentication"

# Generate a commit message
sprout commit
sprout commit --skip-prompt  # Auto-review and commit

# Generate a shell script
sprout shell "backup all .go files to a timestamped archive"

# View change history
sprout log
sprout log --raw-log  # Verbose logs

# Manage MCP servers
sprout mcp list
sprout mcp add
```

## Commands

### `sprout agent`

Core AI agent for analysis, generation, explanation, and orchestration.

**Basic Usage:**
```bash
sprout agent [intent] [flags]
```

**Examples:**
```bash
sprout agent  # Interactive mode
sprout agent "Add JWT auth to API" --skip-prompt --model "deepinfra:qwen3-coder"
sprout agent --dry-run "Refactor main.go for modularity"
sprout agent "Analyze this codebase" --persona researcher
```

### `sprout commit`

AI-generated conventional commit for staged Git changes.

**Basic Usage:**
```bash
sprout commit [flags]
```

**Examples:**
```bash
sprout commit --dry-run
sprout commit --skip-prompt  # Auto-review and commit
```

### `sprout review`

LLM code review for staged Git changes.

**Basic Usage:**
```bash
sprout review [flags]
```

**Examples:**
```bash
sprout review --model "openai:gpt-5"
```

### `sprout shell`

Generate shell scripts from natural language descriptions (no execution).

**Basic Usage:**
```bash
sprout shell [description] [flags]
```

**Examples:**
```bash
sprout shell "Setup React dev environment and install dependencies"
```

### `sprout log`

View and revert change history.

**Basic Usage:**
```bash
sprout log [flags]
```

**Examples:**
```bash
sprout log  # Summary
sprout log --raw-log  # Verbose .sprout/workspace.log
```

### `sprout mcp`

Manage MCP (Model Context Protocol) servers.

**Basic Usage:**
```bash
sprout mcp <command> [flags]
```

**Commands:**
```bash
sprout mcp add      # Interactive setup (GitHub or custom)
sprout mcp list     # Show server status
sprout mcp test [name]  # Verify connection
sprout mcp remove [name]  # Remove a server
```

### `sprout custom`

Manage custom OpenAI-compatible providers.

**Basic Usage:**
```bash
sprout custom [command] [flags]
```

### `sprout diag`

Show diagnostic information about configuration and environment.

**Basic Usage:**
```bash
sprout diag [flags]
```

### `sprout version`

Print version, build, and platform information.

**Basic Usage:**
```bash
sprout version
```

### `sprout plan`

Planning and execution mode with todo creation.

**Basic Usage:**
```bash
sprout plan [idea] [flags]
```

### `sprout skill`

Manage agent skills and conventions.

**Basic Usage:**
```bash
sprout skill [command] [flags]
```

### `sprout export-training`

Export session data to training formats (ShareGPT, OpenAI, Alpaca).

**Basic Usage:**
```bash
sprout export-training [flags]
```

---

## Advanced Agent Flags

### Session Management

| Flag | Description | Example |
|------|-------------|---------|
| `--session-id <id>` | Specify a session identifier | `sprout agent --session-id my-session "continue work"` |
| `--last-session` | Resume previous session | `sprout agent --last-session "resume"` |

### Persona Selection

| Flag | Description | Example |
|------|-------------|---------|
| `--persona <name>` | Select agent persona | `sprout agent --persona coder "implement feature"` |

**Available Personas:**
- `orchestrator` — Process-oriented planning and delegation
- `general` — General-purpose tasks (default)
- `coder` — Feature implementation and production code
- `refactor` — Behavior-preserving refactoring
- `debugger` — Bug investigation and root cause analysis
- `tester` — Unit test writing and test coverage
- `reviewer` — Code review and security review (alias: `code_reviewer`)
- `researcher` — Combined local codebase analysis and external research
- `web_scraper` — Web extraction and structured content collection
- `coordinator` — Cross-project orchestration and task queue (alias: `executive_assistant`, `ea`)
- `computer_user` — Desktop automation with screenshots, mouse, and keyboard

### Performance & Safety

| Flag | Description | Example |
|------|-------------|---------|
| `--no-connection-check` | Skip provider connection check | `sprout agent --no-connection-check "task"` |
| `--max-iterations <n>` | Limit iterations (default: 1000) | `sprout agent --max-iterations 50 "task"` |
| `--no-stream` | Disable streaming for scripts | `SPROUT_NO_STREAM=1 sprout agent "task"` |
| `--no-subagents` | Disable subagent tools | `sprout agent --no-subagents "task"` |
| `--risk-profile <name>` | Shell-command gating profile: `readonly`, `cautious`, `default`, `permissive`, `unrestricted`. Overrides `config.risk_profile` for this session. See [SECURITY.md](SECURITY.md#risk-profiles). | `sprout agent --risk-profile readonly "audit the auth flow"` |
| `--unsafe` | Bypass security checks (use with caution) | `sprout agent --unsafe "task"` |

### Custom Prompts

| Flag | Description | Example |
|------|-------------|---------|
| `--system-prompt <file>` | Use custom system prompt from file | `sprout agent --system-prompt prompts/custom.md "task"` |
| `--system-prompt-str <text>` | Inline custom system prompt | `sprout agent --system-prompt-str "You are a security expert..." "task"` |

### Resource Management

| Flag | Description | Example |
|------|-------------|---------|
| `--resource-directory <dir>` | Store web/vision resources | `sprout agent --resource-directory captures "task"` |
| `--workflow-config <file>` | Run workflow configuration | `sprout agent --workflow-config examples/agent_workflow.json "task"` |
| `--trace-dataset-dir <dir>` | Enable dataset tracing | `sprout agent --trace-dataset-dir traces "task"` |
| `--prompt-stdin` | Read prompt from stdin | `echo "task" | sprout agent --prompt-stdin` |

### Model Selection

| Flag | Description | Example |
|------|-------------|---------|
| `--model <provider:model>` | Specify model | `sprout agent --model "deepinfra:qwen3-coder" "task"` |
| `--provider <name>` | Specify provider | `sprout agent --provider ollama "task"` |

### Web UI Control

| Flag | Description | Example |
|------|-------------|---------|
| `--no-web-ui` | Disable Web UI | `sprout agent --no-web-ui "task"` |
| `--web-port <port>` | Use custom port | `sprout agent --web-port 8080` |
| `-d`, `--daemon` | Daemon mode (keep Web UI running) | `sprout agent -d` |

---

## Slash Commands in Interactive Mode

In interactive `sprout` or `sprout agent`, use `/` for commands (tab-complete).

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
| `/risk-profile [name\|clear]` | Show or change the shell-command risk profile mid-session. With no args, lists profiles and marks the active one. See [SECURITY.md](SECURITY.md#risk-profiles). |

### Help

| Command | Description |
|---------|-------------|
| `/help` | Show usage and all slash commands |

---

## Agent Personas

`sprout` supports 11 specialized personas, each optimized for different types of tasks. See [`docs/PERSONAS.md`](PERSONAS.md) for detailed descriptions.

| Persona | Description |
|---------|-------------|
| `orchestrator` | Process-oriented planning and delegation |
| `general` | General-purpose tasks (default) |
| `coder` | Feature implementation and production code |
| `refactor` | Behavior-preserving refactoring specialist |
| `debugger` | Bug investigation and root cause analysis |
| `tester` | Unit test writing and test coverage |
| `reviewer` | Code review and security review (alias: `code_reviewer`) |
| `researcher` | Combined local codebase analysis and external research |
| `web_scraper` | Web extraction and structured content collection |
| `coordinator` | Cross-project orchestration and task queue (alias: `executive_assistant`, `ea`) |
| `computer_user` | Desktop automation with screenshots, mouse, and keyboard |

For strategic project planning, use the `project-planning` skill instead: `activate_skill project-planning`.

**Using Personas:**
```bash
sprout agent --persona coder "implement feature"
sprout agent --persona debugger "fix this bug"
sprout agent --persona reviewer "review my code"
sprout agent --persona researcher "analyze codebase and external sources"
sprout agent --persona web_scraper "extract structured content from web pages"
sprout agent --persona refactor "refactor code while preserving behavior"
sprout agent --persona computer_user "open the browser and navigate to example.com"
```

---

## Memory System

The memory system persists learned information across all conversations. Memories are markdown files stored in `~/.config/sprout/memories/` and automatically loaded into the system prompt.

### Memory Commands

| Command | Description |
|---------|-------------|
| `add_memory` | Save new memories with descriptive names |
| `read_memory` | Read a specific memory |
| `list_memories` | List all saved memories |
| `delete_memory` | Delete a memory |

**Usage in Interactive Mode:**
```
sprout> add_memory "git-safety" "Never force-push to shared branches"
sprout> list_memories
sprout> read_memory "git-safety"
sprout> delete_memory "git-safety"
```

**Use Cases:**
- Store project conventions
- Save user preferences
- Remember discovered patterns
- Document workflow guidelines

---

## Ignoring Files

Add patterns to `.sprout/sproutignore` to exclude files from processing. The file respects `.gitignore` patterns.

**Setup:**
```bash
# Via agent or manually
echo "dist/" >> .sprout/sproutignore
echo "*.log" >> .sprout/sproutignore
```

**Example `.sprout/sproutignore`:**
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

`sprout` includes a comprehensive built-in tool suite for all development tasks.

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

### Todo Management

| Tool | Description |
|------|-------------|
| `todo_write` / `todo_read` | Todo management for breaking down tasks |

---

## MCP Server Integration

MCP (Model Context Protocol) extends `sprout` with external tools and services.

### Quick Start

```bash
# Interactive setup
sprout mcp add

# List servers
sprout mcp list

# Test connection
sprout mcp test [name]

# Remove server
sprout mcp remove [name]
```

### Configuration

- Config file: `~/.config/sprout/mcp_config.json`
- Use in agent: `Create GitHub PR for feature #WS`

### Features

- Connect to external tools and services
- GitHub integration (repos, issues, PRs)
- Custom server support
- Auto-discovery and auto-start options

See [docs/MCP_INTEGRATION.md](MCP_INTEGRATION.md) for detailed MCP setup.

---

## Workspace Initialization

The `.sprout/` directory is automatically created when you first run `sprout` commands. It contains:

- `config.json` — Local configuration overrides
- `sproutignore` — Ignore patterns (augments `.gitignore`)
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
- [Architecture](ARCHITECTURE.md) — Project structure and design
- [MCP Integration](MCP_INTEGRATION.md) — MCP server setup
- [Agent Workflow](AGENT_WORKFLOW.md) — Workflow configuration
- [Provider Catalog](PROVIDER_CATALOG.md) — Available providers and models
- [Testing](TESTING.md) — Test strategy and commands
