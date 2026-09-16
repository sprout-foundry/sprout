# Sprout

AI-powered coding agent and development environment: a chat-first CLI, a Web UI, and 11 specialized personas that collaborate to understand your workspace, edit code, run tests, open PRs, and drive multi-step tasks against any LLM provider.

> **Cost & safety:** Sprout talks to LLM providers and external services, which may incur per-token costs. It ships a 5-profile risk cascade (`readonly` … `unrestricted`), per-hunk diff approval, and configurable tool allowlists — start with `cautious` or `readonly`. See [docs/SECURITY.md](docs/SECURITY.md).

## Install

**Linux / macOS:**

```bash
curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh
```

**Windows (PowerShell 5.1+):**

```powershell
irm https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.ps1 | iex
```

Upgrade, uninstall, version pinning, checksum verification, Homebrew, and Termux: [docs/CLI_REFERENCE.md](docs/CLI_REFERENCE.md).

## Getting started

```bash
sprout                                                          # interactive mode (Web UI at http://localhost:56000)
sprout agent "Create a python script that prints 'Hello, World!'"
sprout agent --persona coder "Add JWT auth to the API"
sprout plan                                                     # planning mode (no code changes)
sprout review                                                   # AI review of staged changes
sprout commit                                                   # generate a conventional commit
sprout pr                                                       # open a PR for the current branch
sprout search "embedding index"                                 # search past sessions
```

First run walks you through provider selection and API-key validation: [docs/onboarding.md](docs/onboarding.md).

### What's included

- **Agent + 11 personas** (`orchestrator`, `coder`, `tester`, `reviewer`, …) with parallel subagents, mid-flight steering, and cooperative cancellation; 40+ slash commands for in-session control
- **Web UI** — chat, editor, file tree, terminal, git UI, settings, cost dashboard; built into the binary, nothing extra to install
- **Providers** — OpenAI, DeepInfra, OpenRouter, Z.AI, GLM Coding Plan, DeepSeek, Mistral, MiniMax, LMStudio, Cerebras, Chutes, Ollama Cloud, plus self-hosted Ollama and any OpenAI-compatible endpoint (`sprout custom add`); the embedded catalog refreshes between releases
- **Safety** — risk cascade, per-hunk diff approval, security audit log (`sprout audit`)
- **Skills & MCP** — loadable instruction packs (`sprout skill`) and external tool servers (`sprout mcp`)
- **LSP** — diagnostics, definitions, rename via `sprout lsp`
- **Automations** — JSON-defined workflows under `automate/`, run with `sprout automate`
- **History** — every file mutation tracked and reversible (`sprout log` / `sprout history`); full-text search across past sessions (`sprout search`)
- **Service mode** — background daemon with HTTP API and WebSocket terminal/editor sessions (`sprout service`)

## Configuration

Configuration is layered — global defaults, optional per-workspace overrides, then session settings — with later layers winning. Every field is also editable in the Web UI settings panel. References: [docs/CONFIGURATION.md](docs/CONFIGURATION.md) and [docs/FALLBACKS.md](docs/FALLBACKS.md) (every default and inheritance chain).

Risk profiles control what runs without prompting:

| Profile | Behavior |
|---|---|
| `readonly` | Read-only operations only; mutations blocked |
| `cautious` | Most operations prompt; subagent writes blocked |
| `default` | Auto-approves reads and common edits; destructive ops prompt |
| `permissive` | High trust; almost everything runs without prompting |
| `unrestricted` | No gating; only critical patterns (`rm -rf /`, fork bombs) block |

```bash
sprout agent --risk-profile=cautious "review this PR"           # per session
# persistent: set "risk_profile" in your sprout config (see CONFIGURATION.md)
```

Full model: [docs/SECURITY.md](docs/SECURITY.md).

## Development

Requirements: **Go 1.26+** and **Node.js 22+**.

```bash
git clone https://github.com/sprout-foundry/sprout.git
cd sprout
make prepare-grammars        # editor language grammars (needed for IDE; Make targets run it automatically)
make build-all               # WASM shell + embedded Web UI + Go binary
./sprout                     # run from the repo root
```

| Command | What it does |
|---|---|
| `make build-all` | Build everything (WASM, Web UI, binary) |
| `make test-smoke` | Fast smoke suite |
| `go test ./...` | Go unit tests |
| `make test-webui-vitest` | Web UI unit tests (vitest) |
| `npx playwright test --project=webui test/webui/<spec>.spec.ts` | Web UI e2e (backend + Vite stack auto-starts) |
| `make vet && make fmt-check && make lint` | Pre-PR gates |

Agent guidance for working in this repo: [AGENTS.md](AGENTS.md). Contribution rules: [CONTRIBUTING.md](CONTRIBUTING.md). Authoritative specs: [roadmap/](roadmap/) (`SP-###.md`).

## Architecture in 30 seconds

- **CLI** (`sprout`, Go) — cobra command tree, 30+ commands (`agent`, `plan`, `commit`, `review`, `pr`, `search`, `service`, …)
- **Web UI** (`webui/`, React 18 + Vite + TypeScript) — built by `make deploy-ui`, embedded in the binary
- **`@sprout/ui`** (`packages/ui`) — shared component library, also [consumable standalone](docs/CONSUMPTION_GUIDE.md); `@sprout/events` ships the event schemas
- **Provider catalog** (`pkg/providercatalog/providers.json`) — embedded in the binary, refreshed from GitHub at startup
- **Sister project** `sprout-foundry` pins a `SPROUT_VERSION` and ships the binary in Docker images — integration contract: [docs/FOUNDRY_CHAT_CONTRACT.md](docs/FOUNDRY_CHAT_CONTRACT.md)

Full layout and data flow: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Platform matrix (Linux/macOS/Windows/WASM/no-CGO): [docs/PLATFORM_SUPPORT.md](docs/PLATFORM_SUPPORT.md).

## Documentation

| Document | Description |
|---|---|
| [CLI Reference](docs/CLI_REFERENCE.md) | All commands, flags, slash commands, personas, tools |
| [Onboarding](docs/onboarding.md) | Guided first-run setup (provider → API key → first chat) |
| [Configuration](docs/CONFIGURATION.md) | Config files, environment variables, Zsh detection, CI/CD |
| [Fallbacks](docs/FALLBACKS.md) | Every default, inheritance, and fallback resolution chain |
| [Architecture](docs/ARCHITECTURE.md) | Package layout, data flow, workspace files |
| [Security](docs/SECURITY.md) | Risk profiles, tool call classification, security model |
| [Personas](docs/PERSONAS.md) | Persona system, risk model, custom personas |
| [MCP Integration](docs/MCP_INTEGRATION.md) | MCP server setup, configuration, troubleshooting |
| [Agent Workflow](docs/AGENT_WORKFLOW.md) | Config-driven workflow sequences |
| [Service / Daemon](docs/SERVICE.md) | Run sprout as a long-lived HTTP/WS service |
| [Local LLM (MLX)](docs/LOCAL_LLM.md) | Local models via MLX — no API key or network |
| [Provider Catalog](docs/PROVIDER_CATALOG.md) | Provider catalog system and model metadata |
| [Provider Registry](docs/PROVIDER_REGISTRY.md) | Remote provider registry, community provider PRs |
| [LSP Architecture](docs/LSP_ARCHITECTURE.md) | Language server integration |
| [Component Library](docs/CONSUMPTION_GUIDE.md) | @sprout/ui usage and architecture |
| [Electron Launcher](docs/ELECTRON.md) | Desktop app wrapper |
| [Testing](docs/TESTING.md) | Test strategy, categories, commands |
| [Changelog](CHANGELOG.md) | Per-release commit log |
| [Roadmap](roadmap/) | Authoritative spec docs (`SP-###.md`) for planned and shipped work |

## License & support

Apache 2.0 — see [LICENSE](LICENSE). Report issues at [github.com/sprout-foundry/sprout/issues](https://github.com/sprout-foundry/sprout/issues).
