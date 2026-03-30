# Ledit

AI-powered code editing and assistance tool. Leverages LLMs to understand your workspace, generate code, and orchestrate complex development tasks.

> **Disclaimer:** Using `ledit` involves interactions with LLMs and external services which may incur costs. Currently there are limited safety checks — use at your own risk, ideally in a container.

## Features

- **AI Agent** with smart workspace context, self-correction, and multi-step orchestration
- **10 specialized personas** (coder, debugger, reviewer, researcher, etc.)
- **Web UI** with chat, code editor, terminal, file browser, Git UI, and more
- **Multi-provider LLM support** — OpenAI, DeepInfra, OpenRouter, Z.AI, Ollama, DeepSeek, Mistral, Minimax, LMStudio, and custom providers
- **MCP Server Integration** for external tools (GitHub repos, issues, PRs)
- **Persistent Memory** across conversations
- **Built-in tool suite** — file operations, web search, vision analysis, shell execution, PDF analysis, headless browser
- **Background subagents** for parallel task execution
- **Addressing** — SSH remote workspace with local Web UI access via tunneling
- **Dataset tracing** for training data generation
- **Provider catalog** with model lists, costs, and recommendations

## Installation

### Quick Install

**Linux / macOS:**
```bash
curl -fsSL https://raw.githubusercontent.com/alantheprice/ledit/main/scripts/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/alantheprice/ledit/main/scripts/install.ps1 | iex
```

### Install Options

```bash
# Specific version
LEDIT_VERSION=v0.14.0 curl -fsSL https://raw.githubusercontent.com/alantheprice/ledit/main/scripts/install.sh | sh

# Custom directory
LEDIT_INSTALL_DIR=~/.local/bin curl -fsSL https://raw.githubusercontent.com/alantheprice/ledit/main/scripts/install.sh | sh

# Without piping (Linux / macOS)
curl -fsSL -o install.sh https://raw.githubusercontent.com/alantheprice/ledit/main/scripts/install.sh
sh install.sh
```

### Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/alantheprice/ledit/main/scripts/install.sh | sh -s -- --uninstall
```

### From Source

Requires Go 1.25.0+:

```bash
go install github.com/alantheprice/ledit@latest
```

## Getting Started

```bash
# Start interactive agent mode (Web UI opens at http://localhost:54000)
ledit

# Run a specific task
ledit agent "Create a python script that prints 'Hello, World!'"
ledit agent --skip-prompt "Implement user authentication"
ledit agent --persona coder "Add JWT auth to API"

# Generate a commit message
ledit commit

# Generate shell scripts
ledit shell "backup all .go files to a timestamped archive"

# View change history
ledit log
```

## Documentation

| Document | Description |
|-----------|-------------|
| [CLI Reference](docs/CLI_REFERENCE.md) | All commands, flags, slash commands, personas, tools |
| [Configuration](docs/CONFIGURATION.md) | Config files, environment variables, Zsh detection, CI/CD |
| [Web UI](docs/WEB_UI.md) | Web UI features, SSH tunneling, remote access |
| [Architecture](docs/ARCHITECTURE.md) | Package layout, data flow, workspace files |
| [MCP Integration](docs/MCP_INTEGRATION.md) | MCP server setup, configuration, troubleshooting |
| [Agent Workflow](docs/AGENT_WORKFLOW.md) | Config-driven workflow sequences |
| [Provider Catalog](docs/PROVIDER_CATALOG.md) | Provider catalog system and model metadata |
| [Subagent Personas](docs/subagent_personas.md) | Specialized persona descriptions and configuration |
| [Testing](docs/TESTING.md) | Test strategy, categories, and commands |
| [Electron Packaging](docs/ELECTRON.md) | Desktop shell packaging and distribution |
| [Product Backlog](docs/PRODUCT_BACKLOG.md) | Desktop-first productization roadmap |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines. Run `go test ./...` before PRs.

## License

[MIT License](LICENSE).

## Support

Report issues at [github.com/alantheprice/ledit/issues](https://github.com/alantheprice/ledit/issues).
