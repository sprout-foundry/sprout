# Sprout

AI-powered code editing and assistance tool. Leverages LLMs to understand your workspace, generate code, and orchestrate complex development tasks.

> **Disclaimer:** Using `sprout` involves interactions with LLMs and external services which may incur costs. Currently there are limited safety checks — use at your own risk, ideally in a container.

## Features

- **Coding Agent** with smart workspace context, self-correction, and multi-step orchestration
- **12 specialized personas** (coder, debugger, reviewer, researcher, executive assistant, project planner, etc.)
- **Web UI** with chat, code editor, terminal, file browser, Git UI, and more
- **Multi-provider LLM support** — OpenAI, DeepInfra, OpenRouter, Z.AI, Ollama, DeepSeek, Mistral, Minimax, LMStudio, Cerebras, Chutes, plus community-contributed providers via the remote registry (see [Provider Registry](docs/PROVIDER_REGISTRY.md)) and local custom providers (`sprout custom add`)
- **MCP Server Integration** for external tools (GitHub repos, issues, PRs)
- **Persistent Memory** across conversations
- **Built-in tool suite** — file operations, web search, vision analysis, shell execution, PDF analysis, headless browser
- **Background subagents** for parallel task execution
- **Workflow Automations**: Define and run autonomous agent workflows from `automate/` directory configs. Monitor with `sprout automate status/stop/logs` or the WebUI Automations panel.
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

**Windows (PowerShell 5.1+ — the default on Windows 10/11, or PowerShell 7):**

```powershell
irm https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.ps1 | iex
```

**Termux (Android, arm64):**

The same Linux installer detects Termux, installs into `$PREFIX/bin`,
skips the systemd/launchd service step, and surfaces a clear error if
the binary's libc requirements don't match Bionic:

```bash
pkg install curl tar
curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh
```

The release pipeline cross-compiles `sprout-linux-arm64` with CGO disabled,
so the resulting static binary runs on Termux's Bionic libc unmodified.
If the post-install verification fails the installer prints a
build-from-source recipe specific to Termux (`pkg install golang nodejs
make git` + `make deploy-ui && go install .`).

**macOS via Homebrew (once the tap is published):**

```bash
brew tap sprout-foundry/sprout
brew install sprout
```

Or install directly from the release URL without adding the tap:

```bash
brew install --formula https://github.com/sprout-foundry/sprout/releases/latest/download/sprout.rb
```

The formula source lives at [`Formula/sprout.rb`](Formula/sprout.rb);
`scripts/update-homebrew-formula.sh` stamps it with each release's
version and SHA256s and `release.yml` uploads the result as an asset.

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

### Upgrading

Once sprout is installed, you can upgrade from the binary itself — no
need to re-pipe curl into a shell:

```bash
sprout upgrade                     # confirm, then download + verify + replace
sprout upgrade --check             # just print whether an upgrade is available
sprout upgrade -y                  # skip the prompt (useful in CI)
sprout upgrade --version v0.14.0   # pin a specific tag
sprout upgrade --pre-release       # include pre-release tags as "latest"
sprout upgrade --rollback          # restore the previous binary saved by the last upgrade
```

The same SHA256 verification used by `install.sh` runs inside the
command; bypass with `SPROUT_SKIP_CHECKSUM=1` only when you have a
specific reason.

On Windows the running `.exe` is renamed to `sprout.exe.old` in place
(Windows can't replace a loaded executable) and the new binary is written
to the original path — restart any running sprout process to pick up the
new build.

### Uninstall

By default this removes the binary, the service files, and the config /
session state under `~/.config/sprout/` and `~/.sprout/`. Pass
`--keep-config` (or `-KeepConfig` on Windows) to preserve them.

```bash
curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh -s -- --uninstall
# Keep your settings + session history:
curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh -s -- --uninstall --keep-config
```

### Verifying the download

Every release ships a `SHA256SUMS` manifest and an SLSA build-provenance
attestation. The install scripts verify the checksum automatically; the
provenance attestation can be checked manually with the GitHub CLI:

```bash
# Integrity (already automated by install.sh — this is for manual re-check):
curl -fsSL https://github.com/sprout-foundry/sprout/releases/latest/download/SHA256SUMS \
  | sha256sum -c --ignore-missing

# Provenance — proves the binary came from the official release workflow:
gh release download <tag> --pattern 'sprout-*' --repo sprout-foundry/sprout
gh attestation verify sprout-linux-amd64.tar.gz --repo sprout-foundry/sprout
```

Set `SPROUT_SKIP_CHECKSUM=1` (or `$env:SPROUT_SKIP_CHECKSUM='1'` on
Windows) only if you have a specific reason to bypass verification — e.g.
a release that pre-dates the manifest.

### From Source

Requires Go 1.25.0+ and Node.js 22+:

```bash
git clone https://github.com/sprout-foundry/sprout.git
cd sprout
make deploy-ui   # Build and embed the React web UI (requires Node.js)
make prepare-grammars
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

| Profile        | Effect                                                                                                | Use when                                     |
| -------------- | ----------------------------------------------------------------------------------------------------- | -------------------------------------------- |
| `readonly`     | Only reads (`git status/log/diff`, `read_file`). Everything else is **blocked outright** (no prompt). | Audits, code review, untrusted agents.       |
| `cautious`     | Reads auto-approve. Everything else prompts you.                                                      | Sensitive workspaces.                        |
| `default`      | Reads + common edits auto-approve. Destructive ops (force flags, `rm -rf`, lossy git) prompt.         | Daily driver. ← _the default_                |
| `permissive`   | Almost everything auto-approves; only force-flagged or recursive-destructive patterns prompt.         | High-trust agents in recoverable workspaces. |
| `unrestricted` | Nothing prompts. Only catastrophic patterns (rm-rf-root, fork bombs) block.                           | Sandboxed runs.                              |

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

| Document                                       | Description                                               |
| ---------------------------------------------- | --------------------------------------------------------- |
| [Component Library](docs/CONSUMPTION_GUIDE.md) | @sprout/ui npm package usage and architecture             |
| [CLI Reference](docs/CLI_REFERENCE.md)         | All commands, flags, slash commands, personas, tools      |
| [Configuration](docs/CONFIGURATION.md)         | Config files, environment variables, Zsh detection, CI/CD |
| [Architecture](docs/ARCHITECTURE.md)           | Package layout, data flow, workspace files                |
| [MCP Integration](docs/MCP_INTEGRATION.md)     | MCP server setup, configuration, troubleshooting          |
| [Agent Workflow](docs/AGENT_WORKFLOW.md)       | Config-driven workflow sequences                          |
| [Provider Catalog](docs/PROVIDER_CATALOG.md)   | Provider catalog system and model metadata                |
| [Provider Registry](docs/PROVIDER_REGISTRY.md) | Remote provider registry, community provider PRs, schema  |
| [Personas](docs/PERSONAS.md)                   | Persona system, risk model, and custom persona guide      |
| [Testing](docs/TESTING.md)                     | Test strategy, categories, and commands                   |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines. Run `go test ./...` before PRs.

## License

[MIT License](LICENSE).

## Support

Report issues at [github.com/sprout-foundry/sprout/issues](https://github.com/sprout-foundry/sprout/issues).
