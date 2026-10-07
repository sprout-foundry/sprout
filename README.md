# Sprout

An open-source AI coding agent for your terminal and browser. Point it at a project, describe what you want, and review what it changed — every file edit is tracked and can be rolled back, and risky commands ask before they run.

## Install

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh
```

**Windows (PowerShell 5.1+):**

```powershell
irm https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.ps1 | iex
```

The installer verifies the release checksum. Set `SPROUT_INSTALL_DIR` to choose where it goes — see [scripts/install.sh](scripts/install.sh). Run it again to upgrade.

## Get it working (about 5 minutes)

Sprout runs on an AI model from a provider you choose. You pay the provider for what you use — usually cents per task.

**1. Get an API key** from one of these:

- **[DeepInfra](https://deepinfra.com/dash/api_keys)** — sign up, add a little credit, create an API key.
- **[OpenRouter](https://openrouter.ai/keys)** — the alternative: sign up, add credit, create an API key. One key gives you hundreds of models.

**2. Open your project and start Sprout:**

```bash
cd path/to/your/project
sprout
```

The first run asks which provider you use, takes your key and checks it. You can also set the key ahead of time with `sprout keys set deepinfra` (or `openrouter`), or by exporting `DEEPINFRA_API_KEY` / `OPENROUTER_API_KEY`.

**3. Pick a model.** We suggest **DeepSeek V4.1 Flash** or **GLM-5.3-Flash** — both fast, strong at coding, and inexpensive. In Sprout, type one of:

```
/model deepseek-ai/DeepSeek-V4.1-Flash   # on DeepInfra
/model zai-org/GLM-5.3-Flash             # on DeepInfra
/model deepseek/deepseek-v4.1-flash      # on OpenRouter
/model z-ai/glm-5.3-flash                # on OpenRouter
```

`/model` on its own lists every model your provider offers; `/provider select` switches providers.

**4. Ask for something.** For example:

- `Explain how this project is organized`
- `Add a test for <some function>`
- `Find and fix the failing test`

Type `/help` to see every command.

## Using Sprout

**In the terminal.** `sprout` starts an interactive session in the current directory. It creates a `.sprout/` folder in your project for its project settings.

**In the browser.** While `sprout` is running it also serves a web UI — chat, editor, file tree, terminal and git — at the address it prints (http://localhost:56000 by default). To run only the web UI, use `sprout agent -d`. To keep it running in the background on macOS or Linux, install it as a service with `sprout service install`.

**Other commands:**

```bash
sprout plan                  # propose a plan first, build after you approve
sprout review                # AI review of your staged changes
sprout commit                # write a commit message for staged changes
sprout pr                    # open a pull request for the current branch
sprout search "rate limit"   # search your past sessions
```

## Undo and safety

- `/changes` lists the files Sprout changed in this session.
- `/rollback` lists revisions; `/rollback <revision-id>` reverts one. From your shell, `sprout log` shows the same history.
- Risky commands and edits ask for your approval before they run. How much asks is set by a risk profile:

| Profile | Behavior |
|---|---|
| `readonly` | Read-only operations only; changes are blocked |
| `cautious` | Most operations ask first |
| `default` | Reads and common edits run; destructive operations ask |
| `permissive` | Almost everything runs without asking |
| `unrestricted` | Nothing asks; only critical patterns (`rm -rf /`, fork bombs) are blocked |

```bash
sprout agent --risk-profile=cautious    # for one session; set "risk_profile" in your config to keep it
```

Sprout calls your provider's API, which costs money per token; you can see a session's cost in the web UI. Full security model: [docs/SECURITY.md](docs/SECURITY.md).

## What's included

- **Agent and personas** — `coder`, `tester`, `reviewer`, an `orchestrator` that runs subagents in parallel, and more; steer or cancel mid-task
- **Web UI** — chat, editor, file tree, terminal, git, settings, cost meter; built into the binary
- **Providers** — DeepInfra, OpenRouter, OpenAI, Z.AI, GLM Coding Plan, DeepSeek, Mistral, MiniMax, Cerebras, Chutes, Ollama Cloud, LM Studio, self-hosted Ollama, and any OpenAI-compatible endpoint (`sprout custom add`)
- **History** — every file change tracked and reversible; full-text search across past sessions
- **Skills and MCP** — instruction packs (`sprout skill`) and external tool servers (`sprout mcp`)
- **LSP** — diagnostics, definitions and rename (`sprout lsp`)
- **Automations** — JSON-defined workflows (`sprout automate`)

## Configuration

Settings are layered — global defaults, optional per-project overrides, then the current session — and every setting is also editable in the web UI's settings panel. See [docs/CONFIGURATION.md](docs/CONFIGURATION.md) and [docs/FALLBACKS.md](docs/FALLBACKS.md).

## Contributing

Requirements: **Go 1.26+** and **Node.js 22+**.

```bash
git clone https://github.com/sprout-foundry/sprout.git
cd sprout
make prepare-grammars        # editor language grammars (Make targets run it automatically)
make build-all               # WASM shell + embedded web UI + Go binary + @sprout-foundry/workspace
./sprout                     # run from the repo root
```

On Windows without `make`/bash, `powershell -File scripts\prepare-grammars.ps1` then `go build .` builds the binary; the agent's shell tool uses Git for Windows' bash when installed (cmd.exe otherwise).

| Command | What it does |
|---|---|
| `make build-all` | Build everything (WASM, web UI, binary, workspace package) |
| `make test-smoke` | Fast smoke suite |
| `go test ./...` | Go unit tests |
| `make test-webui-vitest` | Web UI unit tests (vitest) |
| `npx playwright test --project=webui test/webui/<spec>.spec.ts` | Web UI e2e (backend + Vite stack auto-starts) |
| `make vet && make fmt-check && make lint` | Pre-PR gates |

**Web UI development with hot reload:** run the backend
(`./sprout agent --daemon`) next to the Vite dev server
(`cd webui && npm run dev`) and edit `webui/src/**` with instant HMR at
http://localhost:3000 — no UI rebuild needed. `./sprout serve` swaps in a
canned-response mock LLM so no provider credentials are required. Full
instructions and gotchas: [webui/DEVELOPMENT.md](webui/DEVELOPMENT.md).

Agent guidance for working in this repo: [AGENTS.md](AGENTS.md). Contribution rules: [CONTRIBUTING.md](CONTRIBUTING.md). Authoritative specs: [roadmap/](roadmap/) (`SP-###.md`).

### Architecture in 30 seconds

- **CLI** (`sprout`, Go) — cobra command tree (`agent`, `plan`, `commit`, `review`, `pr`, `search`, `service`, …)
- **Web UI** (`webui/`, React 18 + Vite + TypeScript) — built by `make deploy-ui`, embedded in the binary
- **`@sprout/ui`** (`packages/ui`) — shared component library, `npm install @sprout/ui` [consumes it standalone](docs/CONSUMPTION_GUIDE.md); `@sprout/events` ships the event schemas
- **`@sprout-foundry/workspace`** (`packages/workspace`) — the composition API a host mounts (SP-160); built by `make build-workspace-package` into `packages/workspace/dist`
- **Provider catalog** (`pkg/providercatalog/providers.json`) — embedded in the binary, refreshed from GitHub at startup
- **Sister project** `sprout-foundry` pins a `SPROUT_VERSION` and ships the binary in Docker images — integration contract: [docs/FOUNDRY_CHAT_CONTRACT.md](docs/FOUNDRY_CHAT_CONTRACT.md)

Full layout and data flow: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Platform matrix (Linux/macOS/Windows/WASM/no-CGO): [docs/PLATFORM_SUPPORT.md](docs/PLATFORM_SUPPORT.md).

### Documentation

| Document | Description |
|---|---|
| [CLI Reference](docs/CLI_REFERENCE.md) | All commands, flags, slash commands, personas, tools |
| [Onboarding](docs/onboarding.md) | First-run setup in more detail |
| [Configuration](docs/CONFIGURATION.md) | Config files, environment variables, CI/CD |
| [Fallbacks](docs/FALLBACKS.md) | Every default, inheritance, and fallback resolution chain |
| [Architecture](docs/ARCHITECTURE.md) | Package layout, data flow, workspace files |
| [Security](docs/SECURITY.md) | Risk profiles, tool call classification, security model |
| [Personas](docs/PERSONAS.md) | Persona system, risk model, custom personas |
| [MCP Integration](docs/MCP_INTEGRATION.md) | MCP server setup, configuration, troubleshooting |
| [Agent Workflow](docs/AGENT_WORKFLOW.md) | Config-driven workflow sequences |
| [Service / Daemon](docs/SERVICE.md) | Run sprout as a long-lived HTTP/WS service |
| [Local LLM (MLX)](docs/LOCAL_LLM.md) | Run a model on your Mac (Apple Silicon), no API key |
| [Provider Catalog](docs/PROVIDER_CATALOG.md) | Provider catalog system and model metadata |
| [Provider Registry](docs/PROVIDER_REGISTRY.md) | Remote provider registry, community provider PRs |
| [LSP Architecture](docs/LSP_ARCHITECTURE.md) | Language server integration |
| [Component Library](docs/CONSUMPTION_GUIDE.md) | @sprout/ui usage and architecture |
| [Testing](docs/TESTING.md) | Test strategy, categories, commands |
| [Changelog](CHANGELOG.md) | Per-release commit log |
| [Roadmap](roadmap/) | Authoritative spec docs (`SP-###.md`) for planned and shipped work |

## License and support

Apache 2.0 — see [LICENSE](LICENSE). Report issues at [github.com/sprout-foundry/sprout/issues](https://github.com/sprout-foundry/sprout/issues).
