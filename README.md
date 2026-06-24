# Sprout

AI-powered code editing and assistance tool. Leverages LLMs to understand your workspace, generate code, and orchestrate complex development tasks.

> **Disclaimer:** Using `sprout` involves interactions with LLMs and external services which may incur costs. Currently there are limited safety checks — use at your own risk, ideally in a container.

## Features

- **Coding Agent** with smart workspace context, self-correction, and multi-step orchestration
- **12 specialized personas** (coder, debugger, reviewer, researcher, executive assistant, project planner, etc.)
- **Web UI** with chat, code editor, terminal, file browser, Git UI, and more
- **Multi-provider LLM support** — OpenAI, DeepInfra, OpenRouter, Z.AI, Ollama, DeepSeek, Mistral, Minimax, LMStudio, Cerebras, Chutes, plus community providers via the [remote registry](docs/PROVIDER_REGISTRY.md) and local custom providers (`sprout custom add`)
- **MCP Server Integration** for external tools (GitHub repos, issues, PRs)
- **Persistent Memory** across conversations
- **Built-in tool suite** — file operations, web search, vision analysis, shell execution, PDF analysis, headless browser
- **Background subagents** for parallel task execution
- **Workflow Automations** — define and run autonomous agent workflows from `automate/` configs
- **Addressing** — SSH remote workspace with local Web UI access via tunneling

The UI component library (`@sprout/ui`) is also available as a [standalone npm package](docs/CONSUMPTION_GUIDE.md).

## Installation

**Linux / macOS:**

```bash
curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh
```

**Windows (PowerShell 5.1+):**

```powershell
irm https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.ps1 | iex
```

**From source** (requires Go 1.25.0+ and Node.js 22+):

```bash
git clone https://github.com/sprout-foundry/sprout.git
cd sprout
make deploy-ui && make prepare-grammars
go install .
```

For upgrade, uninstall, version pinning, checksum/provenance verification, Homebrew, and Termux, see the [CLI Reference](docs/CLI_REFERENCE.md).

## Getting Started

```bash
sprout                                                          # interactive mode (Web UI at http://localhost:56000)
sprout agent "Create a python script that prints 'Hello, World!'"
sprout agent --persona coder "Add JWT auth to API"
sprout commit                                                   # generate a commit message
sprout shell "backup all .go files to a timestamped archive"
sprout log                                                      # view change history
```

## Permissions & Risk Profiles

Before running a shell command, sprout consults a **risk cascade** that decides whether to run, prompt, or block. Five profiles ship out of the box — `readonly`, `cautious`, `default`, `permissive`, and `unrestricted`. Pick one for a session or set a persistent default:

```bash
sprout agent --risk-profile=cautious "review this PR"
```

Full reference, profile table, and custom overrides: [docs/SECURITY.md#risk-profiles](docs/SECURITY.md#risk-profiles).

## Documentation

| Document                                       | Description                                               |
| ---------------------------------------------- | -------------------------------------------------------- |
| [CLI Reference](docs/CLI_REFERENCE.md)         | All commands, flags, slash commands, personas, tools     |
| [Configuration](docs/CONFIGURATION.md)         | Config files, environment variables, Zsh detection, CI/CD |
| [Architecture](docs/ARCHITECTURE.md)           | Package layout, data flow, workspace files               |
| [Security](docs/SECURITY.md)                   | Risk profiles, tool call classification, security model  |
| [Personas](docs/PERSONAS.md)                   | Persona system, risk model, and custom persona guide     |
| [MCP Integration](docs/MCP_INTEGRATION.md)     | MCP server setup, configuration, troubleshooting         |
| [Agent Workflow](docs/AGENT_WORKFLOW.md)       | Config-driven workflow sequences                         |
| [Provider Catalog](docs/PROVIDER_CATALOG.md)   | Provider catalog system and model metadata               |
| [Provider Registry](docs/PROVIDER_REGISTRY.md) | Remote provider registry, community provider PRs, schema |
| [Component Library](docs/CONSUMPTION_GUIDE.md) | @sprout/ui npm package usage and architecture            |
| [Testing](docs/TESTING.md)                     | Test strategy, categories, and commands                  |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines. Run `go test ./...` before PRs.

## License

[MIT License](LICENSE).

## Support

Report issues at [github.com/sprout-foundry/sprout/issues](https://github.com/sprout-foundry/sprout/issues).
