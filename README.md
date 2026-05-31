# Sprout

AI-powered code editing and assistance tool. Leverages LLMs to understand your workspace, generate code, and orchestrate complex development tasks.

> **Disclaimer:** Using `sprout` involves interactions with LLMs and external services which may incur costs. Currently there are limited safety checks — use at your own risk, ideally in a container.

## Features

- **Coding Agent** with smart workspace context, self-correction, and multi-step orchestration
- **10 specialized personas** (coder, debugger, reviewer, researcher, etc.)
- **Web UI** with chat, code editor, terminal, file browser, Git UI, and more
- **Multi-provider LLM support** — OpenAI, DeepInfra, OpenRouter, Z.AI, Ollama, DeepSeek, Mistral, Minimax, LMStudio, Cerebras, Chutes, and custom providers
- **MCP Server Integration** for external tools (GitHub repos, issues, PRs)
- **Persistent Memory** across conversations
- **Built-in tool suite** — file operations, web search, vision analysis, shell execution, PDF analysis, headless browser
- **Background subagents** for parallel task execution
- **Addressing** — SSH remote workspace with local Web UI access via tunneling
- **Dataset tracing** for training data generation
- **Provider catalog** with model lists, costs, and recommendations

## Component Library

The Sprout UI component library (`@sprout/ui`) is available as a standalone npm package, providing reusable IDE-style React components — code editor, terminal, chat panel, file tree, and more — for building IDE-like applications.

```bash
npm install @sprout/ui
```

See the [Consumption Guide](docs/CONSUMPTION_GUIDE.md) for full documentation.

## Installation

### Recommended Installation

**Linux / macOS:**
```bash
curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.ps1 | iex
```

### Install Options

```bash
# Specific version
SPROUT_VERSION=v0.14.0 curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh

# Custom directory
SPROUT_INSTALL_DIR=~/.local/bin curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh

# Without piping (Linux / macOS)
curl -fsSL -o install.sh https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh
sh install.sh
```

### Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh -s -- --uninstall
```

### From Source

Requires Go 1.25.0+ and Node.js 22+:

```bash
git clone https://github.com/sprout-foundry/sprout.git
cd sprout
make deploy-ui   # Build and embed the React web UI (requires Node.js)
go install .
```

> **Note:** `go install github.com/sprout-foundry/sprout@latest` does not currently work for installing via the Go module proxy, because the React web UI assets are built during CI release and committed to the release tag. Clone the repository and use `make deploy-ui` to build the UI assets locally.

## Getting Started

```bash
# Start interactive agent mode (Web UI opens at http://localhost:56000)
sprout

# Run a specific task
sprout agent "Create a python script that prints 'Hello, World!'"
sprout agent --skip-prompt "Implement user authentication"
sprout agent --persona coder "Add JWT auth to API"

# Generate a commit message
sprout commit

# Generate shell scripts
sprout shell "backup all .go files to a timestamped archive"

# View change history
sprout log
```

## Permissions & Risk Profiles

Before sprout runs a shell command it consults a **risk cascade** that decides whether to run, prompt, or block. The cascade is driven by a named profile — five ship out of the box, you can override any of them in config:

| Profile | Effect | Use when |
|---|---|---|
| `readonly` | Only reads (`git status/log/diff`, `read_file`). Everything else is **blocked outright** (no prompt). | Audits, code review, untrusted agents. |
| `cautious` | Reads auto-approve. Everything else prompts you. | Sensitive workspaces. |
| `default` | Reads + common edits auto-approve. Destructive ops (force flags, `rm -rf`, lossy git) prompt. | Daily driver. ← *the default* |
| `permissive` | Almost everything auto-approves; only force-flagged or recursive-destructive patterns prompt. | High-trust agents in recoverable workspaces. |
| `unrestricted` | Nothing prompts. Only catastrophic patterns (rm-rf-root, fork bombs) block. | Sandboxed runs. |

```bash
# Pick a profile for one session
sprout agent --risk-profile=cautious "review this PR"
sprout agent --risk-profile=permissive "rebuild the integration tests"

# Or set a persistent default in ~/.config/sprout/config.json
{ "risk_profile": "default" }

# Or override profile rules entirely — see docs/SECURITY.md#risk-profiles
{
  "risk_profile": "default",
  "risk_profiles": {
    "default": { "low_risk": [...], "high_risk_never": [...], "default_risk": "medium" }
  }
}
```

When the active persona spawns subagents (e.g. EA delegating to `coder`), the subagent's high-risk prompts auto-approve under the root's authority — you set the policy once and orchestration runs without prompts piling up. Catastrophic patterns (`rm -rf /`, fork bombs) stay blocked at every depth regardless of profile. **Full reference: [docs/SECURITY.md#risk-profiles](docs/SECURITY.md#risk-profiles).**

## Documentation

| Document | Description |
|-----------|-------------|
| [Component Library](docs/CONSUMPTION_GUIDE.md) | @sprout/ui npm package usage and consumption guide |
| [CLI Reference](docs/CLI_REFERENCE.md) | All commands, flags, slash commands, personas, tools |
| [Configuration](docs/CONFIGURATION.md) | Config files, environment variables, Zsh detection, CI/CD |
| [Web UI](docs/WEB_UI.md) | Web UI features, SSH tunneling, remote access |
| [Architecture](docs/ARCHITECTURE.md) | Package layout, data flow, workspace files |
| [MCP Integration](docs/MCP_INTEGRATION.md) | MCP server setup, configuration, troubleshooting |
| [Agent Workflow](docs/AGENT_WORKFLOW.md) | Config-driven workflow sequences |
| [Provider Catalog](docs/PROVIDER_CATALOG.md) | Provider catalog system and model metadata |
| [Personas](docs/PERSONAS.md) | Persona system, risk model, and custom persona guide |
| [Testing](docs/TESTING.md) | Test strategy, categories, and commands |
| [Electron Packaging](docs/ELECTRON.md) | Desktop shell packaging and distribution |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines. Run `go test ./...` before PRs.

## License

[MIT License](LICENSE).

## Support

Report issues at [github.com/sprout-foundry/sprout/issues](https://github.com/sprout-foundry/sprout/issues).
