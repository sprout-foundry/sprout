# Sprout

AI-powered coding agent and development environment. Sprout gives you a chat-first CLI, a Web UI for visual work, and 11 specialized personas that collaborate to understand your workspace, edit code, run tests, open PRs, and orchestrate multi-step development tasks against any LLM provider.

> **Cost & safety:** Using sprout involves LLM interactions and external services which may incur per-token costs. Sprout ships a tiered risk cascade (5 profiles — `readonly`, `cautious`, `default`, `permissive`, `unrestricted`), per-hunk diff approval for edits, OS-notification-based agent completion signals, and configurable tool allowlists. Use `cautious` or `readonly` until you understand the cost profile of your chosen model. See [docs/SECURITY.md](docs/SECURITY.md) for the full model.

## Features

### Agent & personas
- **Coding agent** with workspace-aware context, self-correction, parallel subagents, and steerable execution
- **11 specialized personas** that collaborate through structured delegation: `orchestrator` (top-level routing), `coordinator` (long-running workflow driver), `coder`, `refactor`, `tester`, `reviewer`, `debugger`, `researcher`, `web_scraper`, `general`, and `computer_user` (desktop automation)
- **Mid-flight steering** — type while a subagent runs and the message is delivered to the active subagent, not buffered
- **Cooperative cancellation** — Stop button (UI) or Ctrl-C (CLI) cancels the primary plus all running subagents
- **34 slash commands** for in-session control — `/search`, `/compact`, `/clear`, `/rewind`, `/commit`, `/review`, `/risk-profile`, `/persona`, `/models`, `/max-context`, and more

### Editor, UI, and tools
- **Web UI** with chat, file tree, code editor, integrated terminal, git UI, settings, command palette, sessions picker, cost dashboard, and notifications
- **Plan mode** (`sprout plan`) — interactive planning before any code changes
- **Cross-session search** (`/search`, `sprout search`) — full-text search across past sessions with debounced incremental indexing
- **MCP server integration** — connect external tool servers (GitHub, Postgres, custom) via `sprout mcp`
- **LSP support** (`sprout lsp`) — language servers for symbol resolution, diagnostics, and code intelligence
- **Skills system** (`sprout skill`, `sprout skills`) — loadable, versionable instruction packs for domain expertise
- **Computer-use persona** (`computer_user`) — desktop automation with screenshots, mouse, and keyboard under safety gates

### Workflow automation & integrations
- **Workflow automations** — define autonomous agent workflows in `automate/*.json` and run them with `sprout automate`
- **Background tasks**, **parallel subagents** (`run_parallel_subagents`), and **cooperative cancellation** for long-running work
- **PR creation** (`sprout pr`) — push branch and open GitHub PR from the CLI with title/body/base/head, with credential-store lookup and `gh` CLI fallback
- **Review staged changes** (`sprout review`) — AI-powered code review on staged git diffs before commit
- **Service / daemon mode** (`sprout service`) — run sprout as a background daemon with HTTP API and WebSocket terminal/editor sessions
- **Onboarding flow** (`docs/onboarding.md`) — guided first-run setup with provider selection, API key validation, and a working first chat

### Providers & models
- **12 first-party providers** — OpenAI, DeepInfra, OpenRouter, Z.AI, GLM Coding Plan, DeepSeek, Mistral, MiniMax, LMStudio, Cerebras, Chutes, Ollama Cloud (plus a built-in self-hosted Ollama Local connector)
- **Community provider registry** — pull new providers from the curated registry via `docs/PROVIDER_REGISTRY.md`
- **Custom OpenAI-compatible providers** (`sprout custom add`) — local llama.cpp, vLLM, ollama, or any OpenAI-shaped endpoint
- **Live provider catalog** — `pkg/providercatalog/providers.json` is embedded in the binary and refreshed from `raw.githubusercontent.com` at startup so models stay current between releases

### Safety, persistence, and context
- **Risk cascade** with 5 profiles (`readonly`, `cautious`, `default`, `permissive`, `unrestricted`) plus per-call overrides
- **Per-hunk diff approval** (`SP-072`) — review and accept/reject each change before it lands
- **Persistent memory** across conversations, with cross-session recall and structured summarization
- **Built-in tool suite** — file ops, web search, vision analysis, shell execution, PDF analysis, headless browser, image processing, atomic structured-file writes that preserve key order
- **Change tracking** — every file mutation is reversible via `sprout log` / `sprout history` (rewind to any prior state)
- **Cross-session search** — find past conversations by content with sub-100ms response once indexed

The UI component library (`@sprout/ui`) is also available as a [standalone npm package](docs/CONSUMPTION_GUIDE.md) for embedding in your own apps.

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

**Upgrade, uninstall, version pinning, checksum/provenance verification, Homebrew, and Termux:** see the [CLI Reference](docs/CLI_REFERENCE.md).

## Getting started

```bash
sprout                                                          # interactive mode (Web UI at http://localhost:56000)
sprout agent "Create a python script that prints 'Hello, World!'"
sprout agent --persona coder "Add JWT auth to the API"
sprout plan                                                     # planning mode (no code changes)
sprout search "embedding index"                                  # search past sessions
sprout review                                                   # review staged changes
sprout pr                                                       # open a PR for the current branch
sprout commit                                                   # generate a conventional commit
sprout shell "backup all .go files to a timestamped archive"    # generate a shell script (review, then run manually)
```

First time? Follow the [Onboarding guide](docs/onboarding.md) for a guided provider-setup → first-chat flow.

## Permissions & risk profiles

Before running a shell command, sprout consults a **risk cascade** that classifies the command and decides whether to run silently, prompt, or block. Five profiles ship out of the box (canonical descriptions from `pkg/agent_commands/risk_profile.go`):

| Profile | Behavior |
|---|---|
| `readonly` | Read-only operations only; any mutation is blocked |
| `cautious` | Most operations prompt; subagent writes blocked |
| `default` | Built-in defaults — auto-approves reads and common edits/commits; only destructive ops prompt |
| `permissive` | High trust; almost everything passes without prompting |
| `unrestricted` | No risk-cascade gating; only critical patterns (`rm -rf /`, fork bombs) block |

```bash
sprout agent --risk-profile=cautious "review this PR"           # one session
# persistent default: set "risk_profile": "cautious" in ~/.config/sprout/config.json
```

Full reference, profile table, and custom overrides: [docs/SECURITY.md#risk-profiles](docs/SECURITY.md#risk-profiles).

## Documentation

| Document | Description |
|---|---|
| [CLI Reference](docs/CLI_REFERENCE.md) | All commands, flags, slash commands, personas, tools |
| [Onboarding](docs/onboarding.md) | Guided first-run setup (provider → API key → first chat) |
| [Configuration](docs/CONFIGURATION.md) | Config files, environment variables, Zsh detection, CI/CD |
| [Fallbacks](docs/FALLBACKS.md) | Every default, inheritance, and fallback resolution chain |
| [Architecture](docs/ARCHITECTURE.md) | Package layout, data flow, workspace files |
| [Security](docs/SECURITY.md) | Risk profiles, tool call classification, security model |
| [Personas](docs/PERSONAS.md) | Persona system, risk model, and custom persona guide |
| [MCP Integration](docs/MCP_INTEGRATION.md) | MCP server setup, configuration, troubleshooting |
| [Agent Workflow](docs/AGENT_WORKFLOW.md) | Config-driven workflow sequences |
| [Service / Daemon](docs/SERVICE.md) | Run sprout as a long-lived HTTP/WS service |
| [Provider Catalog](docs/PROVIDER_CATALOG.md) | Provider catalog system and model metadata |
| [Provider Registry](docs/PROVIDER_REGISTRY.md) | Remote provider registry, community provider PRs, schema |
| [LSP Architecture](docs/LSP_ARCHITECTURE.md) | Language server integration |
| [Component Library](docs/CONSUMPTION_GUIDE.md) | @sprout/ui npm package usage and architecture |
| [Electron Launcher](docs/ELECTRON.md) | Desktop app wrapper |
| [Testing](docs/TESTING.md) | Test strategy, categories, and commands |
| [Changelog](CHANGELOG.md) | Per-release commit log |
| [Roadmap](roadmap/) | Authoritative spec docs (`SP-###.md`) for planned and shipped work |

## Architecture in 30 seconds

- **CLI binary** (`sprout`, Go) — cobra command tree with `agent`, `plan`, `commit`, `review`, `pr`, `search`, `history`, `log`, `audit`, `lsp`, `mcp`, `service`, `config`, `policy`, `keys`, `embeddings`, `skills`, `automate`, `explain`, `diag`, `export-training`, `custom`, `upgrade`, `version`
- **Web UI** (`webui/`, React 18 + Vite + TypeScript) — embeds into the Go binary via `make deploy-ui` and is served by the daemon or by `sprout` itself in interactive mode
- **Shared UI library** (`@sprout/ui`, packages/ui) — extracted design-system components; both webui and downstream consumers import from it
- **Embedded provider catalog** (`pkg/providercatalog/providers.json`) — embedded in the binary; refreshed from GitHub at startup
- **Distributed via `scripts/install.sh`**; the sister project [`sprout-foundry`](../sprout-foundry) pins to a `SPROUT_VERSION` and installs the binary in Docker images

For the full architecture: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). For how the binary is consumed by sister projects: [docs/FOUNDRY_CHAT_CONTRACT.md](docs/FOUNDRY_CHAT_CONTRACT.md).

## Platform Support

Sprout targets five build platforms. Most features work everywhere; some
are inherently platform-specific (process management, terminal control,
native embeddings). This matrix documents what's available on each.

### Build targets

| Target | Build command | Usage |
|--------|---------------|-------|
| **Linux/macOS (native)** | `go build .` | CLI daemon, WebUI server, full agent |
| **Windows** | `go build .` | CLI daemon, WebUI server, full agent |
| **WASM** | `make build-all` (`GOOS=js GOARCH=wasm`) | Browser shell, WebUI embedded agent |
| **no-CGO** | `go build -tags '!cgo'` | Stripped binary without ONNX embeddings |
| **Browser (rod)** | `go build .` | Requires Chromium for headless browser features |

### Feature availability matrix

| Feature | Linux/macOS | Windows | WASM | no-CGO |
|---------|:-----------:|:-------:|:----:|:------:|
| **Shell execution** | ✅ Full | ✅ Full | ⚠️ No streaming | ✅ Full |
| **Background processes** | ✅ Process groups | ⚠️ No group kill | ❌ Not available | ✅ Full |
| **Process signals** | ✅ SIGINT→TERM→KILL | ⚠️ Kill only | ❌ N/A | ✅ Full |
| **Vision / image analysis** | ✅ Full | ✅ Full | ❌ Not available | ✅ Full |
| **PDF processing** | ✅ Full | ✅ Full | ❌ Not available | ✅ Full |
| **Headless browser** | ✅ Full (rod) | ✅ Full (rod) | ❌ Not available | ✅ Full |
| **Code intelligence graph** | ✅ SQLite store | ✅ SQLite store | ❌ Not available | ✅ Full |
| **ONNX embeddings** | ✅ Full (CGO) | ✅ Full (CGO) | ⚠️ JS bridge | ❌ Static fallback |
| **Static embeddings** | ❌ Uses ONNX | ❌ Uses ONNX | ✅ JS-native | ✅ Hash fallback |
| **Terminal raw mode** | ✅ ioctl (OPOST safe) | ⚠️ term.MakeRaw | ❌ N/A | ✅ Full |
| **Terminal health** | ✅ Termios capture | ⚠️ Always "sane" | ❌ N/A | ✅ Full |
| **Signal handling** | ✅ Full set | ⚠️ Interrupt only | ⚠️ Interrupt only | ✅ Full |
| **OOM watchdog** | ✅ /proc scan | ❌ No-op | ❌ No-op | ✅ Full |
| **Automate sessions** | ✅ PID tracking | ✅ PID tracking | ❌ Not available | ✅ Full |
| **Foreground app detection** | ✅ osascript/xdotool | ❌ Not available | ❌ N/A | ✅ Full |
| **Panic key chord** | ✅ CGEvent/xrecord | ❌ Not available | ❌ N/A | ✅ Full |
| **Computer use** | ✅ Full | ⚠️ No process groups | ❌ N/A | ✅ Full |
| **Structured file tools** | ✅ Full | ✅ Full | ✅ Full | ✅ Full |
| **Memory / settings** | ✅ Full | ✅ Full | ✅ Full | ✅ Full |
| **Git operations** | ✅ Full | ✅ Full | ❌ No filesystem | ✅ Full |
| **Subagent spawning** | ✅ Full | ✅ Full | ✅ Full | ✅ Full |

Legend: ✅ Full support · ⚠️ Degraded but functional · ❌ Not available

### Known platform limitations

**WASM (browser):** Cannot spawn processes, access the filesystem directly,
or run native libraries. Shell commands route through a JS executor
registered by the WebUI. Vision, codegraph, and background processes return
informative errors. Terminal is hardcoded to 80×24.

**Windows:** No Unix process groups (`Setpgid`), so background process
cleanup can't cascade to children. Signal escalation uses
`CTRL_BREAK_EVENT` → `TerminateProcess` instead of SIGINT → SIGTERM →
SIGKILL. Terminal raw mode uses `term.MakeRaw` (may cause staircase
rendering without OPOST preservation). PID-alive checks may return false
positives.

**no-CGO:** ONNX Runtime requires CGO. Without it, embeddings fall back to
a deterministic hash-based provider (384-dim FNV-1a). Search and recall
work but with lower quality than ONNX models.

**non-Linux:** OOM watchdog has no `/proc` to scan, so memory alerts never
fire. Process start-time comparison (for PID-reuse detection) fail-opens.

Full audit details: see [SP-112](roadmap/SP-112-platform-parity.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines. Run `make build-all` and `go test ./...` before PRs.

## Recommended MCP Servers

Sprout's [MCP integration](docs/MCP_INTEGRATION.md) lets you connect external tool servers. Here are some that pair well with sprout's built-in capabilities:

| Server | What it adds |
|---|---|
| [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp) | Persistent codebase knowledge graph — call chains, dead-code detection, type-aware resolution, semantic code search across 158 languages. Extends sprout's native `repo_map` for very large or polyglot repos. |
| [GitHub MCP](https://github.com/github/github-mcp-server) | Direct GitHub API access — issues, PRs, reviews, actions, search. Complements sprout's built-in `git` and `create_pull_request` tools. |
| [Postgres MCP](https://github.com/modelcontextprotocol/servers/tree/main/src/postgres) | Read-only database queries and schema inspection. Useful when the agent needs to understand data shapes or debug data issues. |
| [Filesystem MCP](https://github.com/modelcontextprotocol/servers/tree/main/src/filesystem) | Sandboxed filesystem access with configurable allowlists. Useful for restricting agent file access to specific directories. |

Add any of these with:

```bash
sprout mcp add
```

See `docs/MCP_INTEGRATION.md` for full setup, configuration, and troubleshooting.

## License

[MIT License](LICENSE).

## Support

Report issues at [github.com/sprout-foundry/sprout/issues](https://github.com/sprout-foundry/sprout/issues).
