# Ledit - AI-Powered Code Editing and Assistance Tool

`ledit` is an AI-powered code editing and assistance tool designed to streamline software development by leveraging Large Language Models (LLMs) to understand your entire workspace, generate code, and orchestrate complex features.

## Table of Contents

- [Ledit - LLM-Powered Code Editing and Assistance Tool](#ledit---llm-powered-code-editing-and-assistance-tool)
  - [Table of Contents](#table-of-contents)
  - [Disclaimer](#disclaimer)
  - [Overview](#overview)
  - [Features](#features)
    - [Web UI](#web-ui)
    - [SSH Tunneling (Remote Web UI Access)](#ssh-tunneling-remote-web-ui-access)
  - [Installation](#installation)
    - [Prerequisites](#prerequisites)
    - [From Source (Preferred Method)](#from-source-preferred-method)
  - [Getting Started](#getting-started)
  - [Configuration](#configuration)
    - [`config.json` settings](#configjson-settings)
  - [Usage and Commands](#usage-and-commands)
    - [Workspace Initialization](#workspace-initialization)
    - [Basic Editing and Interaction](#basic-editing-and-interaction)
    - [Slash Commands in Interactive Mode](#slash-commands-in-interactive-mode)
    - [Ignoring Files](#ignoring-files)
  - [MCP Server Integration](#mcp-server-integration)
  - [Contributing](#contributing)
  - [File Structure](#file-structure)
    - [Key files maintained by ledit](#key-files-maintained-by-ledit)
  - [License](#license)
  - [Version Management and Release Process](#version-management-and-release-process)
    - [Release Workflow](#release-workflow)
    - [Creating a Release](#creating-a-release)
    - [Version Information](#version-information)
    - [Release Validation](#release-validation)
  - [CI/CD and Non-Interactive Usage](#cicd-and-non-interactive-usage)
  - [Support and Community](#support-and-community)

## Disclaimer

Please be aware that using `ledit` involves interactions with Large Language Models (LLMs) and external services, which may incur costs depending on your chosen providers and usage. We are not responsible for any costs incurred, data usage, or any other potential issues, damages, or liabilities that may arise from the use or misuse of this tool. Users are solely responsible for monitoring their own API usage and costs.

Safety: Currently there are very few, and limited safety checks in place. Use at your own risk and ideally use in a container to reduce risk from unsafe command execution.

## Overview

`ledit` is more than just a code generator. It's a development partner that can:

- **Implement complex features**: Take a high-level prompt and break it down into a step-by-step plan of file changes.
- **Intelligently use context**: Automatically determines which files in your workspace are relevant to a task, including either their full content or just a summary to optimize the context provided to the LLM.
- **Self-correct**: When orchestrating changes, it can validate its own work, and if an error occurs, it retries with an understanding of the failure (up to 12 attempts).
- **Stay up-to-date**: Use real-time web search to ground its knowledge and answer questions about new technologies or libraries.
- **Work with your tools**: Integrates with Git for automatic commits and respects your `.gitignore` files.

## Features

- **AI Agent Capabilities**: The `ledit agent` command provides intelligent code analysis, explanation, generation, and orchestration. It can understand natural language intents to explain concepts, analyze code, implement features, and handle complex workflows.
- **Self-Correction Loop**: During complex operations, the system automatically analyzes errors and retries with improved context.
- **Smart Workspace Context**: Automatically builds and maintains an index of your workspace with syntactic analysis of files. An LLM selects the most relevant files to include as context for any given task.
- **Leaked Credentials Check**: Automatically scans files for common security concerns like API keys, passwords, database/service URLs, SSH private keys, AWS credentials. This helps prevent accidental exposure of sensitive information.
- **Search Grounding**: Augments prompts with fresh information from the web using the `WebSearch` tool.
- **Interactive and Automated Modes**: Confirm each change manually, or run in a fully automated mode with `--skip-prompt`.
- **Multi-Provider LLM Support**: Works with OpenAI, DeepInfra, OpenRouter, Z.AI, Ollama (local/Turbo), DeepSeek, Chutes, LMStudio, Mistral, Minimax, and custom providers.
  - Gemini models available through OpenRouter integration.
  - Z.AI Coding Plan support via `--provider zai` with models like `GLM-4.6`.
- **MCP Server Integration**: Connect to Model Context Protocol (MCP) servers to extend functionality with external tools and services like GitHub.
- **Change Tracking**: Keeps a local history of all changes made in `.ledit/changes/`.
- **Git Integration**: Can automatically commit changes to Git with AI-generated conventional commit messages.
- **Automated Code Review**: When running in automated mode (`--skip-prompt`), performs LLM-based code reviews of changes before committing.
- **Shell Script Generation**: Generate executable shell scripts from natural language descriptions (`ledit shell`).
- **Todo Tracking**: Built-in todo management for breaking down tasks during workflows.
- **TPS Monitoring**: Tracks tokens-per-second for performance analysis across providers.
- **Interactive UI**: Rich terminal UI with streaming output, progress bars, and slash command support (via `--ui` or LEDIT_UI=1).
- **Tool Suite**: Built-in tools for editing, reading/writing files, web search, vision analysis, shell execution (allowlisted), user interaction, URL browsing, structured file operations, git operations, memory management, skill loading, change history, and self-review.
- **Persistent Memory System**: Save and recall information across conversations. Memories are stored as markdown files in `~/.ledit/memories/` and loaded into the system prompt automatically.
- **Specification Extraction and Scope Validation**: Automatically extracts canonical specifications from conversations and validates changes against requirements to prevent scope creep.
- **Context Compaction**: Intelligent conversation pruning and summarization to maintain context within token limits during long sessions.
- **PDF Analysis**: Extract and analyze content from PDF files using vision-capable models or Python-based OCR.
- **Headless Browser Integration**: Browse, screenshot, and extract content from web pages including SPAs, with rate limiting and caching.
- **Dataset Tracing**: Record runs, turns, tool calls, and artifacts to JSONL files for training dataset generation (`--trace-dataset-dir`).
- **SSH Remote Workspace**: Launch `ledit` on a remote server and access the full Web UI from your local browser via SSH tunneling, including remote directory browsing.
- **Isolated Config Mode**: `--isolated-config` flag creates per-project configuration clones at `./.ledit/` for independent project settings.
- **Multi-Window Client Isolation**: Web UI supports multiple browser windows/tabs with isolated client contexts.
- **Subagent Configuration**: Configure separate provider/model for subagents via `--subagent-provider` and `--subagent-model` flags or config settings.
- **Provider Catalog System**: Built-in provider catalog with model lists, costs, and recommended models. Refreshes from remote source.
- **Comprehensive Tool Suite**: Extensive built-in tools for all development tasks:
  - **`edit_file`** - Edit files with intelligent context
  - **`read_file`** - Read file contents with optional line ranges
  - **`write_file`** - Create or overwrite files
  - **`search_files`** - Search text in files using patterns
  - **`shell_command`** - Execute shell commands (allowlisted)
  - **`browse_url`** - Open URLs in headless browser for screenshot/DOM/text extraction with SPA support
  - **`write_structured_file`** - Write schema-validated JSON/YAML files
  - **`patch_structured_file`** - Apply JSON Patch operations to JSON/YAML files
  - **`git`** - Execute git write operations (requires approval)
  - **`add_memory`** / **`read_memory`** / **`list_memories`** / **`delete_memory`** - Persistent memory system across conversations
  - **`list_skills`** / **`activate_skill`** - Skill management for loading instruction bundles
  - **`view_history`** / **`rollback_changes`** - Change history viewing and rollback
  - **`self_review`** - Review agent's work against canonical specification
  - **`analyze_ui_screenshot`** - Analyze UI screenshots, mockups, or HTML files for implementation guidance
  - **`analyze_image_content`** - Extract text/code from images
  - **`todo_write`** / **`todo_read`** - Todo management for breaking down tasks
  - **`web_search`** - Real-time web search for grounding knowledge
  - **`vision_analysis`** - Vision-capable model analysis for images and PDFs

### Web UI

`ledit` includes a built-in **React-based Web UI** that launches automatically alongside the terminal interface when you run `ledit` or `ledit agent` in interactive mode. It provides a full browser-based environment for AI-assisted coding:

- **AI Chat Interface**: Real-time streaming conversation with the AI agent, with interactive prompts and tool output rendered inline.
- **Code Editor**: CodeMirror-based editor with syntax highlighting for JavaScript, Python, Go, JSON, HTML, CSS, and more. Supports multiple tabs, split views, and unsaved change detection.
- **Integrated Terminal**: Full terminal session running in the browser via WebSocket, with command history and PTY support.
- **File Browser**: Browse and navigate your workspace files directly in the browser. Click files to open them in the editor.
- **Git Integration**: Full Git UI for staging/unstaging files, viewing diffs, committing with AI-generated messages, and AI-powered deep code review with auto-fix.
- **Search & Replace**: Workspace-wide search with case-sensitive, whole-word, and regex options. Search results link directly to the editor.
- **Change History**: Browse changelogs, view file revisions with diffs, and rollback changes — all from the browser.
- **Settings Panel**: Configure providers, models, MCP servers, skills, and other settings without touching config files.
- **Memory Management**: View, create, edit, and delete persistent memories from the browser.
- **Provider Catalog**: Browse available providers and models directly in settings.
- **Command Palette**: Quick-access command palette (`Ctrl+Shift+P`) for fast navigation and actions (Go to File, toggle views, etc.).
- **Multi-Instance Support**: Multiple `ledit` sessions can share a single Web UI server. Switch between instances from the UI.
- **Session Management**: Save and restore chat sessions from the browser.
- **Image Upload**: Upload images for the AI to analyze (vision support).
- **Themes**: Multiple dark and light editor themes (Atom One Dark, Dracula, Solarized, etc.).
- **PWA Support**: Installable as a Progressive Web App on desktop and mobile — works as a standalone app with app shortcuts for Chat and Editor.
- **Responsive & Mobile-Friendly**: Adapts to different screen sizes with collapsible sidebar and touch-friendly controls.
- **Customizable Hotkeys**: Keyboard shortcuts customizable through the Settings panel.

**Accessing the Web UI:**

When you start `ledit` in interactive mode, the Web UI is available at `http://localhost:54421` (or the next available port if 54421 is in use). The terminal will display the URL on startup.

The Web UI binds to `127.0.0.1` (localhost) only — it is **not** accessible from other machines on the network by default. To access it from a remote machine, see [SSH Tunneling](#ssh-tunneling-remote-web-ui-access) below.

```bash
# Start with Web UI (default)
ledit

# Disable the Web UI if not needed
ledit --no-web-ui
ledit agent --no-web-ui "Analyze this code"

# Use a custom port
ledit agent --web-port 8080

# Daemon mode — keep the Web UI running without an interactive prompt
ledit agent -d
ledit agent --daemon
```

### SSH Tunneling (Remote Web UI Access)

The Web UI binds to `127.0.0.1` (localhost only) for security, so it is not directly accessible from other machines. To access the Web UI from your local browser when `ledit` is running on a remote server, use SSH port forwarding to create a secure tunnel.

**Quick Start:**

```bash
# Forward local port 54421 to the same port on the remote server
ssh -L 54421:127.0.0.1:54421 user@remote-server

# Then open in your local browser:
# http://localhost:54421
```

**Options:**

| Flag | Description |
|------|-------------|
| `-L` | Local port forwarding (binds a local port to a remote address) |
| `-N` | Don't execute a remote command (useful when you only need the tunnel) |
| `-f` | Run SSH in the background after authenticating |
| `-T` | Disable pseudo-terminal allocation |

**Common Scenarios:**

```bash
# 1. Tunnel in the background — run once, keep it open
ssh -fN -L 54421:127.0.0.1:54421 user@remote-server
# Open http://localhost:54421 in your browser
# Kill the tunnel when done: kill $(lsof -t -i:54421)

# 2. If the remote ledit is on a custom port
ssh -L 54421:127.0.0.1:8080 user@remote-server

# 3. Different local port (avoid conflicts)
ssh -L 9090:127.0.0.1:54421 user@remote-server
# Open http://localhost:9090 in your browser

# 4. Jump host / bastion
ssh -J bastion.example.com -L 54421:127.0.0.1:54421 user@internal-server

# 5. Attach to an existing tmux/screen session and start ledit
ssh -t -L 54421:127.0.0.1:54421 user@remote-server "tmux attach -t ledit"
```

**Tips:**

- The tunnel only works while the SSH connection is alive. If you close the terminal, the tunnel closes (unless you used `-f`).
- Make sure `ledit` is already running on the remote server before opening the URL (or use `--daemon` mode).
- If the port is already in use locally, choose a different local port (scenario 3).
- You can add SSH config entries to `~/.ssh/config` to simplify frequent tunnels:

```ssh-config
Host ledit-remote
    HostName remote-server.example.com
    User youruser
    LocalForward 54421 127.0.0.1:54421
```

Then simply run `ssh -fN ledit-remote` to establish the tunnel.

## Installation

To get started with `ledit`, the preferred method is to install it via `go install`.

### Prerequisites

- Go 1.25.0+
- Git (for version control integration)

### From Source (Preferred Method)

Make sure you have Go installed and configured.

**For public access (recommended):**
```bash
go install github.com/alantheprice/ledit@latest
```

**For private repository access:**
```bash
# Clone the repository first
git clone https://github.com/alantheprice/ledit.git
cd ledit
go install
```

This will install the `ledit` executable in your `GOPATH/bin` directory (e.g., `~/go/bin` on Linux/macOS).

**Note on PATH:** If `ledit` is not found after installation, you may need to add your `GOPATH/bin` directory to your system's PATH environment variable. For example, you can add the following line to your shell's configuration file (e.g., `.bashrc`, `.zshrc`, or `.profile`):

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```
After adding this, restart your terminal or run `source ~/.bashrc` (or your respective config file) for the changes to take effect.

## Getting Started

Once installed, you can use `ledit` in your project directory and start using its powerful features.

```bash
# Start interactive agent mode (default; Web UI enabled automatically, use --no-web-ui to disable)
ledit

# The Web UI opens at http://localhost:54421
# Run a specific task with the AI agent (Web UI still launches)
ledit agent "Create a python script that prints 'Hello, World!'"
ledit agent "What does the main function in main.go do?"
ledit agent "Fix the build errors in this Go project"
ledit agent --skip-prompt "Implement user authentication"

# Generate a conventional commit message for staged changes
ledit commit
ledit commit --skip-prompt  # Auto-commit with review

# Perform AI code review on staged changes
ledit review

# Generate shell scripts from natural language
ledit shell "backup all .go files to a timestamped archive"

# View the history of changes made by ledit and revert if needed
ledit log
ledit log --raw-log  # Show verbose logs

# Manage MCP servers
ledit mcp list
ledit mcp add  # Interactive setup

# For more detailed examples, see the documentation
```

## Configuration

`ledit` is configured via a `config.json` file. It looks for this file first in `./.ledit/config.json` and then in `~/.ledit/config.json`. A default configuration is created on first run.

**API Keys** for services like DeepInfra, OpenAI, Ollama, etc., are stored securely in `~/.ledit/api_keys.json`. If a key is not found, `ledit` will prompt you to enter it. Set env vars like `DEEPINFRA_API_KEY`, `OPENAI_API_KEY`, `OLLAMA_API_KEY` for convenience.

For Z.AI Coding Plan support, set `ZAI_API_KEY` and select the provider/model:

```bash
export ZAI_API_KEY=your_api_key
ledit agent --provider zai --model GLM-4.6 "implement feature X"
```

### Environment Variables

`ledit` respects the following environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `LEDIT_NO_STREAM=1` | Disable streaming mode | `LEDIT_NO_STREAM=1 ledit agent "task"` |
| `LEDIT_NO_SUBAGENTS=1` | Disable subagent tools | `LEDIT_NO_SUBAGENTS=1 ledit agent "task"` |
| `LEDIT_NO_CONNECTION_CHECK=1` | Skip provider connection check | `LEDIT_NO_CONNECTION_CHECK=1 ledit agent "task"` |
| `LEDIT_RESOURCE_DIRECTORY=<dir>` | Store web/vision resources | `LEDIT_RESOURCE_DIRECTORY=captures` |
| `LEDIT_TRACE_DATASET_DIR=<dir>` | Enable dataset tracing | `LEDIT_TRACE_DATASET_DIR=traces` |
| `LEDIT_CONFIG=<dir>` | Custom config directory | `LEDIT_CONFIG=/my/config` |
| `CI=1` or `GITHUB_ACTIONS=1` | CI environment mode | `CI=1 ledit agent "task"` |
| `GITHUB_PERSONAL_ACCESS_TOKEN` | GitHub token for MCP | Auto-discovers GitHub MCP server |
| `OPENAI_API_KEY`, `DEEPINFRA_API_KEY`, etc. | API keys for providers | Set directly or in `api_keys.json` |

### `config.json` settings

The configuration uses a flat structure focused on provider and model management. Here's the current structure with defaults:

```json
{
  "version": "2.0",
  "last_used_provider": "openai",
  "provider_models": {
    "openai": "gpt-5-mini",
    "zai": "GLM-4.6",
    "deepinfra": "meta-llama/Llama-3.3-70B-Instruct",
    "openrouter": "openai/gpt-5",
    "ollama-local": "qwen3-coder:30b",
    "ollama-turbo": "deepseek-v3.1:671b"
  },
  "provider_priority": [
    "openai",
    "zai",
    "openrouter",
    "deepinfra",
    "ollama-turbo",
    "ollama-local"
  ],
  "mcp": {
    "enabled": false,
    "servers": {},
    "auto_start": false,
    "auto_discover": false,
    "timeout": 30000000000
  },
  "file_batch_size": 10,
  "max_concurrent_requests": 5,
  "request_delay_ms": 100,
  "enable_security_checks": true,
  "enable_pre_write_validation": false,
  "code_style": {
    "indentation_type": "spaces",
    "indentation_size": 4,
    "quote_style": "double",
    "line_endings": "unix",
    "import_style": "grouped"
  },
  "api_timeouts": {
    "connection_timeout_sec": 30,
    "first_chunk_timeout_sec": 60,
    "chunk_timeout_sec": 320,
    "overall_timeout_sec": 600
  },
  "preferences": {},
  "custom_providers": {},
  "resource_directory": "",
  "reasoning_effort": "",
  "self_review_gate_mode": "off",
  "subagent_provider": "",
  "subagent_model": "",
  "pdf_ocr_enabled": true,
  "pdf_ocr_provider": "ollama",
  "pdf_ocr_model": "glm-ocr",
  "history_scope": "workspace"
}
```

Key sections:

- **`provider_models`**: Maps each provider to their default model
- **`provider_priority`**: Defines the order in which providers are tried
- **`mcp`**: Model Context Protocol configuration
- **`last_used_provider`**: Tracks the most recently used provider
- **`code_style`**: Code formatting preferences
- **`preferences`**: General application preferences

Additional settings are managed internally and configured through the agent interface rather than the config file.

### Zsh Command Detection

When using zsh as your shell, `ledit` automatically detects commands available in your environment (external commands, builtins, aliases, and functions) and executes them directly instead of sending them to the AI. This feature is **enabled by default** when using zsh.

**Configuration Options:**

To modify behavior, add to your `~/.ledit/config.json`:

```json
{
  "enable_zsh_command_detection": true,
  "auto_execute_detected_commands": true
}
```

- `enable_zsh_command_detection`: Enable/disable command detection (default: `true`)
- `auto_execute_detected_commands`: Auto-execute detected commands without prompting (default: `true`)

**To disable auto-execution** (prompt for confirmation):
```json
{
  "auto_execute_detected_commands": false
}
```

**To disable the feature entirely**:
```json
{
  "enable_zsh_command_detection": false
}
```

**How it works:**

1. When you type a command that matches an available zsh command (e.g., `git status`, `ls -la`), `ledit` detects it
2. By default, **auto-executes** the command immediately (configurable):
   ```
   [Detected external command: git] [/usr/bin/git]
   [Auto-executing]
   ▶ Executing: git status
   ```
3. If you've disabled auto-execution, it will ask for confirmation first
4. If it's not a clear command, falls through to normal AI processing

**Manual execution with `!`:**

Prefix your command with `!` to force auto-execution (overrides config):
```bash
ledit> !git status  # Always executes immediately
```

**Why use this?**

- **Faster execution**: Commands run instantly without AI involvement
- **Predictable behavior**: Exact command execution vs AI interpretation
- **Better for routine tasks**: Use shell for simple commands, AI for complex ones
- **Configurable safety**: Choose between auto-execute or confirmation prompts

**Fallback behavior:**

If the input is not clearly a command, it will be passed to the AI as normal. This feature only triggers when zsh can confirm the first word is a valid command, builtin, alias, or function.

## Usage and Commands

**Quick Start**: Just type `ledit` to start the interactive AI agent mode with terminal UI!

### Workspace Initialization

The `.ledit/` directory is automatically created when you first run `ledit` commands. It contains the workspace index, configuration, and other metadata. The index is automatically updated on commands for fresh context.

### Basic Editing and Interaction

- **`ledit agent [intent]`**: Core AI agent for analysis, generation, explanation, orchestration.
  ```bash
  ledit agent  # Interactive mode
  ledit agent "Add JWT auth to API" --skip-prompt --model "deepinfra:qwen3-coder"
  ledit agent --dry-run "Refactor main.go for modularity"
  ```

- **`ledit commit`**: AI-generated conventional commit for staged changes.
  ```bash
  ledit commit --dry-run
  ledit commit --skip-prompt  # Auto-review and commit
  ```

- **`ledit review`**: LLM code review for staged Git changes.
  ```bash
  ledit review --model "openai:gpt-5"
  ```

- **`ledit shell [description]`**: Generate shell scripts from natural language (no execution).
  ```bash
  ledit shell "Setup React dev environment and install dependencies"
  ```

- **`ledit log`**: View/revert change history.
  ```bash
  ledit log  # Summary
  ledit log --raw-log  # Verbose .ledit/workspace.log
  ```

- **`ledit mcp`**: Manage MCP servers (see MCP section).
- **`ledit custom`**: Manage custom OpenAI-compatible providers.
- **`ledit diag`**: Show diagnostic information about configuration and environment.
- **`ledit version`**: Print version, build, and platform information.
- **`ledit plan [idea]`**: Planning and execution mode with todo creation.
- **`ledit skill`**: Manage agent skills and conventions.
- **`ledit export-training`**: Export session data to training formats (sharegpt, openai, alpaca).

### Advanced Agent Flags

The `ledit agent` command supports numerous flags for fine-grained control:

**Session Management:**
```bash
ledit agent --session-id my-session "continue work"
ledit agent --last-session "resume previous session"
```

**Persona Selection:**
```bash
ledit agent --persona coder "implement feature"
ledit agent --persona debugger "fix this bug"
ledit agent --persona code_reviewer "review my code"
ledit agent --persona researcher "analyze codebase and external sources"
ledit agent --persona web_scraper "extract structured content from web pages"
ledit agent --persona refactor "refactor code while preserving behavior"
ledit agent --persona computer_user "execute system administration tasks"
```

**Performance & Safety:**
```bash
ledit agent --no-connection-check "skip provider check (saves 1-3s)"
ledit agent --max-iterations 50 "limit iterations (default: 1000)"
ledit agent --no-stream "disable streaming for scripts"
ledit agent --no-subagents "disable subagent tools"
ledit agent --unsafe "bypass security checks (use with caution)"
```

**Custom Prompts:**
```bash
ledit agent --system-prompt prompts/custom.md "use custom system prompt"
ledit agent --system-prompt-str "You are a security expert..." "inline prompt"
```

**Resource Management:**
```bash
ledit agent --resource-directory captures "store web/vision resources"
ledit agent --workflow-config examples/agent_workflow.json "run workflow"
ledit agent --trace-dataset-dir traces "enable dataset tracing"
ledit agent --prompt-stdin "read prompt from stdin (avoids ARG_MAX)"
```

**Workflow Automation:**
```bash
ledit agent --workflow-config examples/agent_workflow.json "Initial task"
```
See [docs/AGENT_WORKFLOW.md](docs/AGENT_WORKFLOW.md) for workflow configuration.

### Slash Commands in Interactive Mode

In interactive `ledit` or `ledit agent`, use `/` for commands (tab-complete):

**Session & History:**
- `/clear`: Clear conversation history.
- `/sessions [session_num]`: Show and load previous conversation sessions.
- `/log`: View changes.

**Models & Providers:**
- `/models [select|<id>]`: List/select models (e.g., `/models select` for interactive dropdown).
- `/providers [select|<name>]`: Switch providers (e.g., `/providers ollama`).

**Agent Features:**
- `/commit`: Generate commit message.
- `/shell <desc>`: Generate shell script.
- `/init`: Regenerate workspace context.
- `/mcp`: Manage MCP servers.
- `/exit`: Quit session.

**Skills & Configuration:**
- `/skills`: List and load agent skills.
- `/export`: Export training data.
- `/plan [idea]`: Start planning mode.
- `/custom`: Manage custom providers.
- `/diag`: Show diagnostic information.

### Agent Personas

`ledit` supports 10 specialized personas, each optimized for different types of tasks:

| Persona | Aliases | Description |
|---------|---------|-------------|
| `orchestrator` | `orchestration` | Process-oriented planning and delegation |
| `general` | `default` | General-purpose tasks |
| `coder` | - | Feature implementation and production code |
| `refactor` | - | Behavior-preserving refactoring specialist |
| `debugger` | - | Bug investigation and root cause analysis |
| `tester` | - | Unit test writing and test coverage |
| `code_reviewer` | `reviewer` | Code review and security review |
| `researcher` | - | Combined local codebase analysis and external research |
| `web_scraper` | `web-scraper`, `scraper` | Web extraction and structured content collection |
| `computer_user` | `sysadmin`, `ops` | System administration and engineering execution |

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

### Memory System

The memory system persists learned information across all conversations. Memories are markdown files stored in `~/.ledit/memories/` and automatically loaded into the system prompt.

- **`add_memory`** — Save new memories with descriptive names
- **`read_memory`** — Read a specific memory
- **`list_memories`** — List all saved memories  
- **`delete_memory`** — Delete a memory

Memories are useful for storing project conventions, user preferences, and patterns discovered during work.

**Help:**
- `/help`: Show usage and all slash commands.

### Ignoring Files

Add patterns to `.ledit/leditignore` (respects `.gitignore`):
```bash
# Via agent or manually
echo "dist/" >> .ledit/leditignore
echo "*.log" >> .ledit/leditignore
```

## MCP Server Integration

MCP extends `ledit` with external tools (e.g., GitHub repos/issues/PRs).

```bash
ledit mcp add  # Interactive (GitHub or custom)
ledit mcp list  # Status
ledit mcp test [name]  # Verify
ledit mcp remove [name]
```

Config: `~/.ledit/mcp_config.json`. Use in agent: "Create GitHub PR for feature #WS".

## Contributing

See CONTRIBUTING.md for guidelines. Run `go test ./...` before PRs.

## File Structure

### Key files maintained by ledit

- **Root**: `main.go` (entry), `cmd/` (CLI subcommands: `agent`, `commit`, `custom`, `diag`, `export-training`, `log`, `mcp`, `plan`, `review`, `shell`, `skill`, `version`).
- **pkg/**: 
  - `agent/`: Core agent orchestration and conversation handling
  - `agent_api/`: LLM provider integrations and API clients
  - `agent_commands/`: CLI command implementations (commit, shell, etc.)
  - `agent_providers/`: Generic provider factory and configuration
  - `agent_tools/`: Built-in tools (file operations, web search, shell execution)
  - `commands/`: Command utilities and helpers (sessions)
  - `codereview/`: Code review functionality
  - `configuration/`: Configuration management and API keys
  - `console/`: Terminal UI, streaming, ANI handling, mouse support
  - `credentials/`: Credential store for API key management
  - `events/`: Event bus system
  - `factory/`: Provider client factory
  - `filediscovery/`: File discovery and indexing
  - `filesystem/`: Workspace filesystem context and security
  - `git/`: Git integration
  - `history/`: Change tracking and rollback functionality
  - `index/`: Workspace indexing
  - `interfaces/`: Common interfaces and abstractions
  - `logging/`: Structured logging with process step tracking
  - `mcp/`: Model Context Protocol client implementation
  - `model_settings/`: Model-specific parameter resolution
  - `personas/`: Agent persona definitions
  - `prompts/`: Prompt templates
  - `providercatalog/`: Provider catalog with model lists and costs
  - `pythonruntime/`: Python runtime detection and validation
  - `security/`: Credential scanning and safety checks
  - `spec/`: Specification extraction and scope validation
  - `text/`: Text processing utilities
  - `tools/`: Tool registry and execution framework
  - `trace/`: Dataset tracing to JSONL files
  - `training/`: Training data export (ShareGPT, OpenAI, Alpaca)
  - `types/`: Common type definitions
  - `ui/`: Terminal UI framework with themes and dropdowns
  - `utils/`: Utility functions and helpers
  - `validation/`: Code validation (gofmt/goimports for Go)
  - `webcontent/`: Web content fetching
  - `webui/`: React-based Web UI server (embedded assets, WebSocket API, Git/Settings/Search/History APIs, terminal management)
  - `zsh/`: Zsh command detection and auto-execution
- **.ledit/** (project-local):
  - `config.json`: Local overrides.
  - `leditignore`: Ignore patterns (augments `.gitignore`).
  - `changes/`: Per-change diff logs with original and updated files.
  - `memories/`: Persistent memory storage (markdown files).
  - `revisions/`: Per-session directories with instructions and LLM responses.
  - `runlogs/`: JSONL workflow traces.
  - `workspace.log`: Verbose execution log.
- **Global (~/.ledit/)**: `config.json` (global config), `api_keys.json`, `mcp_config.json`, `memories/`.
- **Tests**: Unit tests in each `pkg/` subdirectory, `integration_tests/`, `e2e_tests/`, `smoke_tests/`.

## License

MIT License (LICENSE).

## Version Management and Release Process

`ledit` uses a comprehensive CI-gated release process to ensure stable releases.

### Release Workflow

Releases are created through GitHub Actions and enforce strict quality gates:

1. **CI-Gated Releases**: Releases can only be created through the GitHub Actions workflow
2. **Main Branch Only**: Releases must be created from the `main` branch with no uncommitted changes
3. **Automated Changelog**: Changelog is automatically generated using `ledit` itself
4. **Comprehensive Testing**: All tests must pass before release
5. **Multi-Platform Builds**: Automatic builds for Linux, Windows, and macOS

### Creating a Release

**Via GitHub Actions (Recommended)**:
1. Go to GitHub Actions → "Create Release" workflow
2. Click "Run workflow" and specify version (e.g., `v1.2.0`)
3. The workflow will validate prerequisites and create the release

**Local Development (for testing)**:
```bash
# Build with version information
./scripts/version-manager.sh build

# Manual release creation
make build-version
```

### Version Information

Each release includes comprehensive version information:
```bash
ledit version
```

This displays:
- Semantic version
- Git commit hash
- Build timestamp
- Release channel

### Release Validation

The release process validates:
- ✅ On `main` branch
- ✅ No uncommitted changes
- ✅ Valid semantic version format
- ✅ Tag doesn't already exist
- ✅ All tests pass
- ✅ Changelog is updated

## CI/CD and Non-Interactive Usage

`ledit` is designed to work seamlessly in CI/CD pipelines and automated environments:

- **Automatic CI detection** via `CI`/`GITHUB_ACTIONS` environment variables
- **Clean, structured output** without terminal control sequences
- **Progress updates** every 5 seconds with token/cost tracking
- **Structured summaries** at completion with iteration counts and metrics
- **Piped input support** for scripted automation
- **Exit code handling** for integration with CI systems

## Support and Community

File issues at GitHub. Community discussions in issues/PRs.
