# Ledit - AI-Powered Code Editing and Assistance Tool

`ledit` is an AI-powered code editing and assistance tool designed to streamline software development by leveraging Large Language Models (LLMs) to understand your entire workspace, generate code, and orchestrate complex features.

## Table of Contents

- [Ledit - AI-Powered Code Editing and Assistance Tool](#ledit---ai-powered-code-editing-and-assistance-tool)
  - [Table of Contents](#table-of-contents)
  - [Disclaimer](#disclaimer)
  - [Overview](#overview)
  - [Features](#features)
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
- **Multi-Provider LLM Support**: Works with DeepInfra, OpenAI, Ollama (local/Turbo), OpenRouter, Gemini, DeepSeek, and more.
  - Now includes Z.AI Coding Plan (OpenAI-compatible) via `--provider zai` with models like `GLM-4.6`.
- **MCP Server Integration**: Connect to Model Context Protocol (MCP) servers to extend functionality with external tools and services like GitHub.
- **Change Tracking**: Keeps a local history of all changes made in `.ledit/changes/`.
- **Git Integration**: Can automatically commit changes to Git with AI-generated conventional commit messages.
- **Automated Code Review**: When running in automated mode (`--skip-prompt`), performs LLM-based code reviews of changes before committing.
- **Shell Script Generation**: Generate executable shell scripts from natural language descriptions (`ledit shell`).
- **Todo Tracking**: Built-in todo management for breaking down tasks during workflows.
- **TPS Monitoring**: Tracks tokens-per-second for performance analysis across providers.
- **Interactive UI**: Rich terminal UI with streaming output, progress bars, and slash command support (via `--ui` or LEDIT_UI=1).
- **Tool Suite**: Built-in tools for editing, reading/writing files, web search, vision analysis, shell execution (allowlisted), and user interaction.

## Installation

To get started with `ledit`, the preferred method is to install it via `go install`.

### Prerequisites

- Go 1.24+
- Git (for version control integration)

### From Source (Preferred Method)

Make sure you have Go installed and configured.

```bash
go install github.com/alantheprice/ledit@latest
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
# Start interactive agent mode (default; use --ui or LEDIT_UI=1 for enhanced UI)
ledit

# Run a specific task with the AI agent
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

### `config.json` settings

The configuration uses a flat structure focused on provider and model management. Here's the current structure with defaults:

```json
{
  "version": "2.0",
  "last_used_provider": "ollama-turbo",
  "provider_models": {
    "deepinfra": "deepseek-ai/DeepSeek-V3.1-Terminus",
    "ollama-local": "qwen3-coder:30b",
    "ollama-turbo": "qwen3-coder:480b",
    "openai": "gpt-5-mini-2025-08-07",
    "openrouter": "qwen/qwen3-coder-30b-a3b-instruct",
    "test": "test"
  },
  "provider_priority": [
    "openrouter",
    "deepinfra"
  ],
  "mcp": {
    "enabled": false,
    "servers": {},
    "auto_start": false,
    "auto_discover": false,
    "timeout": 30000000000
  },
  "code_style": {
    "indentation_type": "",
    "indentation_size": 0,
    "quote_style": "",
    "line_endings": "",
    "trailing_semicolons": false,
    "trailing_commas": false,
    "bracket_spacing": false,
    "javascript_style": "",
    "optional_chaining": false,
    "arrow_parens": false,
    "space_before_function_paren": false,
    "jsx_single_quote": false,
    "jsx_bracket_same_line": false
  },
  "preferences": {
    "auto_commit": false,
    "auto_review": false,
    "streaming": true,
    "max_retries": 3,
    "retry_delay": 1000,
    "context_window": 128000,
    "temperature": 0.7,
    "max_tokens": 4096
  }
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

### Slash Commands in Interactive Mode

In interactive `ledit` or `ledit agent`, use `/` for commands (tab-complete):
- `/help`: Show usage and slash commands.
- `/models [select|<id>]`: List/select models (e.g., `/models select` for interactive dropdown).
- `/providers [select|<name>]`: Switch providers (e.g., `/providers ollama`).
- `/commit`: Generate commit message.
- `/shell <desc>`: Generate shell script.
- `/init`: Regenerate workspace context.
- `/log`: View changes.
- `/mcp`: Manage MCP.
- `/exit`: Quit session.

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

See CONTRIBUTING.md for guidelines. Run `go test ./...` and e2e_tests/ before PRs.

## File Structure

### Key files maintained by ledit

- **Root**: main.go (entry), cmd/ (CLI subcommands: agent, commit, log, mcp, review, shell).
- **pkg/**: agent/ (orchestration, state, tools), agent_api/ (providers, TPS tracker), configuration/ (config loading), workspace/ (indexing/syntactic analysis), changetracker/ (history/diffs), git/ (commit integration), security/ (credential scans, allowlist), mcp/ (protocol client), console/ (terminal UI, streaming), codereview/ (code review functionality).
- **.ledit/** (project-local):
  - `config.json`: Local overrides.
  - `leditignore`: Ignore patterns (augments .gitignore).
  - `changes/`: Per-change diff logs with original and updated files.
  - `revisions/`: Per-session directories with instructions and LLM responses.
  - `runlogs/`: JSONL workflow traces.
  - `workspace.log`: Verbose execution log.
- **Global (~/.ledit/)**: config.json (global config), api_keys.json, mcp_config.json.
- **Tests**: Unit in pkg/ (e.g., tps_tracker_test.go), integration_tests/ (git/file mods), e2e_tests/ (shell workflows), smoke_tests/ (API).

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
make build-release

# Create a release (local testing only)
make deploy-release VERSION=v1.2.0
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
