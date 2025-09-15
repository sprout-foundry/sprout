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
    - [Ignoring Files](#ignoring-files)
  - [Advanced Concepts: Prompting with Context](#advanced-concepts-prompting-with-context)
    - [`#<filepath>` - Include a File](#filepath---include-a-file)
    - [`#WORKSPACE` / `#WS` - Smart Context](#workspace--ws---smart-context)
    - [`#SG "query"` - Search Grounding](#sg-query---search-grounding)
- [Supported LLM Providers](#supported-llm-providers)
- [MCP Server Integration](#mcp-server-integration)
- [Documentation](#documentation)
  - [Contributing](#contributing)
  - [File Structure](#file-structure)
    - [Key files maintained by ledit](#key-files-maintained-by-ledit)
  - [Author's notes](#authors-notes)
  - [License](#license)
  - [Support and Community](#support-and-community)

## Disclaimer

Please be aware that using `ledit` involves interactions with Large Language Models (LLMs) and external services, which may incur costs depending on your chosen providers and usage. We are not responsible for any costs incurred, data usage, or any other potential issues, damages, or liabilities that may arise from the use or misuse of this tool. Users are solely responsible for monitoring their own API usage and costs.

Safety: Currently there are very few, and limited safety checks in place. Use at your own risk and ideally use in a container to reduce risk from unsafe command execution.

## Overview

`ledit` is more than just a code generator. It's a development partner that can:

- **Implement complex features**: Take a high-level prompt and break it down into a step-by-step plan of file changes.
- **Intelligently use context**: Automatically determines which files in your workspace are relevant to a task, including either their full content or just a summary to optimize the context provided to the LLM.
- **Self-correct**: When orchestrating changes, it can validate its own work, and if an error occurs, it retries with an understanding of the failure.
- **Stay up-to-date**: Use real-time web search to ground its knowledge and answer questions about new technologies or libraries.
- **Work with your tools**: Integrates with Git for automatic commits and respects your `.gitignore` files.

## Features

- **AI Agent Capabilities**: The `ledit agent` command provides intelligent code analysis, explanation, generation, and orchestration. It can understand natural language intents to explain concepts, analyze code, implement features, and handle complex workflows.
- **Intelligent Code Generation**: Generate new code or modify existing code based on natural language prompts using `ledit code` for direct editing tasks.
- **Self-Correction Loop**: During complex operations, the system automatically analyzes errors and retries with improved context.
- **Smart Workspace Context**: Automatically builds and maintains an index of your workspace. An LLM selects the most relevant files to include as context for any given task.
- **Leaked Credentials Check**: Automatically scans files for common security concerns like API keys, passwords, database/service URLs, SSH private keys, AWS credentials. This helps prevent accidental exposure of sensitive information.
- **Search Grounding**: Augments prompts with fresh information from the web using the `#SG "query"` directive.
- **Interactive and Automated Modes**: Confirm each change manually, or run in a fully automated mode with `--skip-prompt`.
- **Multi-Provider LLM Support**: Works with DeepInfra, OpenAI, Groq, Gemini, Ollama, Cerebras, DeepSeek, and more.
- **MCP Server Integration**: Connect to Model Context Protocol (MCP) servers to extend functionality with external tools and services like GitHub.
- **Change Tracking**: Keeps a local history of all changes made.
- **Git Integration**: Can automatically commit changes to Git with AI-generated conventional commit messages.
- **Automated Code Review**: When running in automated mode (`--skip-prompt`), performs LLM-based code reviews of changes before committing.
- **GitHub Action Integration**: Automatically solve GitHub issues by commenting `/ledit` - the action analyzes issues, creates branches, generates implementations, and opens pull requests.

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
# Initialize ledit in your project (creates .ledit directory)
ledit init

# Use the AI agent for code generation, analysis, or complex tasks
ledit agent "Create a python script that prints 'Hello, World!'"
ledit agent "What does the main function in main.go do?"
ledit agent "Fix the build errors in this Go project"

# Generate a conventional commit message for staged changes
ledit commit

# View the history of changes made by ledit and revert if needed
ledit log

# Ignore a directory from workspace analysis
ledit ignore "dist/"

# For more detailed examples, see the documentation
```

## Configuration

`ledit` is configured via a `config.json` file. It looks for this file first in `./.ledit/config.json` and then in `~/.ledit/config.json`. A default configuration is created on first run.

**API Keys** for services like OpenAI, Groq, Jina AI, etc., are stored securely in `~/.ledit/api_keys.json`. If a key is not found, `ledit` will prompt you to enter it.

### `config.json` settings

The configuration has evolved to include domain-specific sections. Here's an example of the current structure with defaults:

```json
{
  "llm": {
    "EditingModel": "deepinfra:deepseek-ai/DeepSeek-V3-0324",
    "SummaryModel": "deepinfra:mistralai/Mistral-Small-3.2-24B-Instruct-2506",
    "OrchestrationModel": "deepinfra:Qwen/Qwen3-Coder-480B-A35B-Instruct",
    "WorkspaceModel": "deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo",
    "EmbeddingModel": "deepinfra:Qwen/Qwen3-Embedding-4B",
    "OllamaServerURL": "http://localhost:11434",
    "Temperature": 0.1,
    "MaxTokens": 30000,
    "TopP": 0.9,
    "PresencePenalty": 0.1,
    "FrequencyPenalty": 0.1
  },
  "ui": {
    "TrackWithGit": false,
    "JsonLogs": false,
    "TelemetryEnabled": false
  },
  "agent": {
    "OrchestrationMaxAttempts": 12,
    "AutoGenerateTests": false,
    "DryRun": false,
    "CodeStyle": {
      "FunctionSize": "Aim for smaller, single-purpose functions (under 50 lines).",
      "FileSize": "Prefer smaller files, breaking down large components into multiple files (under 500 lines).",
      "NamingConventions": "Use clear, descriptive names for variables, functions, and types. Follow Go conventions (camelCase for local, PascalCase for exported).",
      "ErrorHandling": "Handle errors explicitly, returning errors as the last return value. Avoid panics for recoverable errors.",
      "TestingApproach": "Write unit tests when practical.",
      "Modularity": "Design components to be loosely coupled and highly cohesive.",
      "Readability": "Prioritize code readability and maintainability. Use comments where necessary to explain complex logic."
    }
  },
  "security": {
    "EnableSecurityChecks": true,
    "ShellAllowlist": [
      "rm -rf node_modules",
      "rm -fr node_modules",
      "rm -rf ./node_modules",
      "rm -fr ./node_modules",
      "rm -rf node_modules/",
      "rm -fr node_modules/",
      "rm -rf ./node_modules/",
      "rm -fr ./node_modules/",
      "rm -f package-lock.json",
      "rm -f ./package-lock.json"
    ]
  },
  "performance": {
    "FileBatchSize": 30,
    "EmbeddingBatchSize": 30,
    "MaxConcurrentRequests": 3,
    "RequestDelayMs": 100,
    "ShellTimeoutSecs": 20
  }
}
```

Key sections:

- **`llm`**: LLM-related settings, including models and generation parameters.
- **`ui`**: UI and logging settings, like Git tracking and telemetry.
- **`agent`**: Agent behavior, including orchestration attempts and code style preferences.
- **`security`**: Security checks and shell allowlist.
- **`performance`**: Batch sizes, concurrency, and timeouts.

Legacy fields are still supported for backward compatibility but are migrated to these sections.

## Usage and Commands

### Workspace Initialization

The first time you run `ledit` in a project, it will create a `.ledit` directory. This directory contains:

- `workspace.json`: An index of your project's files, including summaries and exports, used for context selection.
- `leditignore`: A file for patterns to ignore, in addition to `.gitignore`.
- `config.json`: Project-specific configuration (optional, created via `ledit init`).
- Various logs and cache files.

The workspace index is automatically updated whenever you run a command, ensuring the context is always fresh.

### Basic Editing and Interaction

`ledit` provides commands for code manipulation, analysis, and integration.

- **`ledit agent [intent]`**: AI agent for intelligent code analysis, generation, explanation, and complex task orchestration.
  ```bash
  # Interactive mode (chat-like)
  ledit agent

  # Direct mode for specific tasks
  ledit agent "Add a function to reverse a string in main.go"
  ledit agent "Explain what the main function does"
  ledit agent "Fix all build errors in this project"
  ledit agent --skip-prompt "Implement user authentication with JWT"
  ```

- **`ledit code "prompt" [-f file]`**: Generate or modify code directly.
  ```bash
  # Edit an existing file
  ledit code "Add a function to reverse a string" -f main.go

  # Create a new file
  ledit code "Create a python script that prints 'Hello, World!'"
  ```

- **`ledit commit`**: Generate a conventional commit message for staged changes, with optional auto-commit and code review.
  ```bash
  ledit commit
  ledit commit --skip-prompt  # Auto-commit with review
  ```

- **`ledit log`**: View the history of changes made by `ledit` and revert if needed.
  ```bash
  ledit log
  ```

- **`ledit ignore "pattern"`**: Add patterns to `.ledit/leditignore` to exclude files/directories from analysis (respects `.gitignore`).
  ```bash
  ledit ignore "dist/"
  ledit ignore "*.log"
  ```

- **`ledit mcp`**: Manage Model Context Protocol (MCP) servers for external tool integration.
  ```bash
  ledit mcp add     # Interactive setup
  ledit mcp list    # List servers
  ledit mcp test    # Test connections
  ledit mcp remove [name]
  ```

- **`ledit init`**: Initialize project-specific configuration in `.ledit/config.json`.

- **`ledit ui`**: Launch the interactive terminal UI.

### AI Agent Orchestration

The `ledit agent` command handles complex tasks through a structured orchestration process (alpha stage, use with caution):

1. **Analysis**: Analyzes your intent and workspace.
2. **Planning**: Generates a step-by-step plan using LLM.
3. **Execution**: Executes steps (code gen, edits, validation).
4. **Validation & Self-Correction**: Analyzes errors, searches for solutions, retries up to 12 times.
5. **Review**: Performs LLM-based code review in automated mode.

Example:
```bash
ledit agent "Implement a REST API for users using Gin" --skip-prompt
```

## Advanced Concepts: Prompting with Context

Enhance prompts with `#` directives for better context.

### `#<filepath>` - Include a File

Include full file content:
```bash
ledit agent "Refactor using helpers from #./helpers.go" -f main.go
```

### `#WORKSPACE` / `#WS` - Smart Context

Curates relevant files/summaries:
```bash
ledit agent "Add JWT auth. #WORKSPACE"
```

### `#SG "query"` - Search Grounding

Fetches web info:
```bash
ledit agent "Add latest react-query. #SG \"latest react-query version\"" -f package.json
```

## Supported LLM Providers

`ledit` supports OpenAI-compatible APIs. Specify as `<provider>:<model>`.

Supported providers:

- **`deepinfra`**: Default for cost-effective models (e.g., `deepinfra:deepseek-ai/DeepSeek-V3-0324`)
- **`openai`**: OpenAI models (e.g., `openai:gpt-4o`)
- **`groq`**: Fast inference (e.g., `groq:llama3-70b-8192`)
- **`gemini`**: Google Gemini (e.g., `gemini:gemini-1.5-pro`)
- **`ollama`**: Local models (e.g., `ollama:llama3`)
- **`cerebras`**: Cerebras models
- **`deepseek`**: DeepSeek models (e.g., `deepseek:deepseek-coder-v2`)

Additional providers can be added via PR.

## MCP Server Integration

**TL;DR**: `ledit mcp add` for easy GitHub/other integrations.

Supports [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) for external tools.

### Quick Start

```bash
ledit mcp add    # Guided setup
ledit mcp list   # Status
ledit mcp test   # Verify
ledit mcp remove [name]
```

Built-in: GitHub MCP (repos, issues, PRs). Custom servers supported.

Config in `~/.ledit/mcp_config.json`. Env vars: `LEDIT_MCP_ENABLED=true`.

Use in agent: `ledit agent "Create GitHub issue for auth bug"`.

Full details in the [MCP section](#mcp-server-integration) (abridged here for brevity).

## GitHub Action: Automated Issue Solving

`ledit` includes a GitHub Action that automatically implements features and fixes based on issue descriptions. Simply comment `/ledit` on any issue to trigger AI-powered code generation.

### Quick Setup

1. Copy the workflow to your repository:

```bash
# Create the workflow directory
mkdir -p .github/workflows

# Copy the example workflow
cp .github/workflows/ledit-solver-example.yml .github/workflows/ledit-solver.yml
```

2. Add your API key as a repository secret (Settings → Secrets → New repository secret)

3. Comment `/ledit` on any issue to start

### Features

- **Automatic Implementation**: Analyzes issues and generates complete implementations
- **Iterative Development**: Comment `/ledit <additional instructions>` for changes
- **Image Support**: Processes mockups and screenshots attached to issues
- **Multi-Provider Support**: Works with OpenAI, Groq, Gemini, and more
- **Safe PR Workflow**: All changes go through pull requests for review

### Example

```markdown
**Issue**: Add dark mode toggle to settings

**Comment**: /ledit implement using Tailwind CSS
```

The action will:
1. Create branch `issue/123`
2. Implement the feature
3. Open a PR with the changes
4. Report progress to the issue

See [.github/actions/ledit-issue-solver/README.md](.github/actions/ledit-issue-solver/README.md) for full documentation.

## Documentation

- [Getting Started](docs/GETTING_STARTED.md)
- [Cheatsheet](docs/CHEATSHEET.md)
- [Examples](docs/EXAMPLES.md)
- [Tips and Tricks](docs/TIPS_AND_TRICKS.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## File Structure

### Key files maintained by ledit

In `.ledit/`:

- `workspace.json` - File index with summaries/exports.
- `config.json` - Project config.
- `leditignore` - Ignore patterns.
- `requirements.json` - Orchestration plans (if used).
- `changes/` - Revision history.
- `revisions/` - Detailed change logs.
- Logs: `runlogs/`, `workspace.log`, etc.
- Caches: `embeddings/`, `url_cache/`, `evidence_cache.json`.

Generated: `setup.sh`, `validate.sh` (project root).

## Author's notes

Most of this README is generated, but key thoughts:

- DeepInfra defaults work well for workspace indexing.
- Prefer Gemini 1.5 Flash/Pro for editing (speedy, capable).
- For cost: Qwen3 Coder on DeepInfra or Ollama local.
- Agent orchestration is alpha; use `#WS` for context.
- Goal: Streamline personal dev flow, not compete broadly.

## License

[MIT License](LICENSE).

## Support and Community

Open issues at [GitHub](https://github.com/alantheprice/ledit/issues).