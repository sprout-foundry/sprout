# CLI Reference

Command-line interface reference for `sprout`. This document covers all CLI commands, flags, and interactive features.

## Quick Start

```bash
# Start interactive agent mode (Web UI enabled by default)
sprout

# Run a specific task
sprout agent "Add JWT auth to API"
sprout agent --skip-prompt "Implement user authentication"

# Generate a commit message
sprout commit
sprout commit --skip-prompt  # Auto-review and commit

# Generate a shell script
sprout shell "backup all .go files to a timestamped archive"

# View change history
sprout log
sprout log --raw-log  # Verbose logs

# Manage MCP servers
sprout mcp list
sprout mcp add
```

## Commands

### `sprout agent`

Core AI agent for analysis, generation, explanation, and orchestration.

**Basic Usage:**
```bash
sprout agent [intent] [flags]
```

**Examples:**
```bash
sprout agent  # Interactive mode
sprout agent "Add JWT auth to API" --skip-prompt --model "deepinfra:qwen3-coder"
sprout agent --dry-run "Refactor main.go for modularity"
sprout agent "Analyze this codebase" --persona researcher
```

### `sprout commit`

AI-generated conventional commit for staged Git changes.

**Basic Usage:**
```bash
sprout commit [flags]
```

**Examples:**
```bash
sprout commit --dry-run
sprout commit --skip-prompt  # Auto-review and commit
sprout commit --trailer "Assisted-by: Sprout (anthropic/claude-opus-5-5)"  # append a git trailer
```

`--trailer "Key: Value"` is repeatable and joins the default trailers from the
`commit_trailers` config (the `.sprout` config) and the repo's `AGENTS.md`
`## Commit Trailers` section. Trailers are written with `git interpret-trailers`
semantics and appear in `--dry-run` output.

### `sprout review`

LLM code review for staged Git changes, or with `--base <ref>` for the
commits on the current branch since that ref.

**Basic Usage:**
```bash
sprout review [flags]
```

**Examples:**
```bash
sprout review --model "openai:gpt-5"
sprout review --base main    # review the branch the way a PR against main would show it
```

### `sprout shell`

Generate shell scripts from natural language descriptions (no execution).

**Basic Usage:**
```bash
sprout shell [description] [flags]
```

**Examples:**
```bash
sprout shell "Setup React dev environment and install dependencies"
```

### `sprout log`

View and revert change history.

**Basic Usage:**
```bash
sprout log [flags]
```

**Examples:**
```bash
sprout log  # Summary
sprout log --raw-log  # Verbose .sprout/workspace.log
```

### `sprout mcp`

Manage MCP (Model Context Protocol) servers.

**Basic Usage:**
```bash
sprout mcp <command> [flags]
```

**Commands:**
```bash
sprout mcp add      # Interactive setup (GitHub or custom)
sprout mcp list     # Show server status
sprout mcp test [name]  # Verify connection
sprout mcp remove [name]  # Remove a server
```

### `sprout custom`

Manage custom OpenAI-compatible providers.

**Basic Usage:**
```bash
sprout custom [command] [flags]
```

### `sprout config`

Read and change settings. `set` writes the layer the agent reads: the
workspace config inside a git repository, the global config elsewhere (or
always, with `--global`).

```bash
sprout config show                            # merged config, credentials redacted
sprout config get output_verbosity --show-origin
sprout config set output_verbosity compact
sprout config set --help-keys                 # list settable keys
```

### `sprout diag`

Show diagnostic information about configuration and environment.

**Basic Usage:**
```bash
sprout diag [flags]
```

### `sprout version`

Print version, build, and platform information.

**Basic Usage:**
```bash
sprout version
```

### `sprout bug`

Open a new issue on the public repository
([github.com/sprout-foundry/sprout](https://github.com/sprout-foundry/sprout)),
prefilled with a short template and your environment (version, OS). The issue
URL is also printed, so it can be copied into a browser by hand. The same
prefilled URL is what the Web UI's "Report a bug" affordances open.

**Basic Usage:**
```bash
sprout bug
```

### `sprout plan`

Planning and execution mode with todo creation.

**Basic Usage:**
```bash
sprout plan [idea] [flags]
```

### `sprout skill`

Manage agent skills and conventions.

**Basic Usage:**
```bash
sprout skill [command] [flags]
```

### `sprout export-training`

Export session data to training formats (ShareGPT, OpenAI, Alpaca).

**Basic Usage:**
```bash
sprout export-training [flags]
```

### `sprout automate`

Discover and run automated agent workflows from your project's `automate/` directory. Workflows are JSON configuration files that define automated agent behavior — building, testing, reviewing, and committing code without manual intervention. Running `sprout automate` with no subcommand opens an interactive workflow picker.

**Basic Usage:**
```bash
sprout automate [flags]
sprout automate <command> [flags]
```

**Commands:**
```bash
sprout automate list               # List available workflows
sprout automate run <workflow>     # Run a workflow by name or filename
sprout automate status             # Show running automate sessions
sprout automate stop <session_id>  # Stop a running session (SIGINT → SIGTERM → SIGKILL)
sprout automate logs <session_id>  # View output from a session
```

**Examples:**
```bash
sprout automate run full_autonomous          # Foreground: output streams to this terminal
sprout automate run review --detach          # Background: returns immediately, logs to a file
sprout automate run review --yes --budget-usd 5
sprout automate status --all                 # Include exited sessions
sprout automate status --json                # Machine-readable JSON array
sprout automate logs abc123 -f -n 100        # Follow last 100 lines until the process exits
sprout automate stop --all
```

**Persistent Flags:**

| Flag | Description | Example |
|------|-------------|---------|
| `--dir <path>` | Workflow directory (default: `./automate`) | `sprout automate run --dir my-workflows review` |
| `-y`, `--yes` | Skip the confirmation prompt before starting the workflow | `sprout automate run --yes review` |
| `--detach` | Run the workflow in the background: child stdio is redirected to `.sprout/automate/logs/<sessionID>.log` under the discovered sprout root (recorded in the session file), and the command returns immediately instead of streaming to the terminal | `sprout automate run review --detach` |
| `--budget-usd <amount>` | Hard cap on workflow USD spend (overrides workflow JSON `budget.usd`; 0 = no cap) | `sprout automate run review --budget-usd 5` |
| `--budget-warn <fractions>` | Comma-separated warning thresholds as fractions of the budget, e.g. `'0.5,0.8'` | `sprout automate run review --budget-warn 0.5,0.8` |
| `--heartbeat <seconds>` | Print `[budget]` progress every N seconds during the run (overrides workflow JSON `progress.heartbeat_seconds`) | `sprout automate run review --heartbeat 30` |

**Detached Runs:**

With `--detach`, child stdio is redirected to `.sprout/automate/logs/<sessionID>.log` under the discovered sprout root (the nearest `.sprout/automate/` found walking up from the current directory), the log path is recorded in the session file (`output_file_path`), and the command returns immediately. Monitor a detached run with `sprout automate status` and `sprout automate logs <session_id>`. Attached (foreground) sessions stream to the terminal and have no captured output file.

**Session Discovery (`status` / `stop` / `logs`):**

Sessions are found by walking up from the current directory to the nearest `.sprout/automate/` (then the central registry), so these commands work from any project subdirectory — not just the repo root. Each accepts a local `--dir` flag that overrides the session root; note this shadows the parent `automate --dir` flag, which selects the *workflow* directory instead.

| Flag | Description | Example |
|------|-------------|---------|
| `--dir <path>` | Path to the `.sprout` session root (default: nearest `.sprout/automate/` found walking up from the current directory) | `sprout automate status --dir ../other-project/.sprout` |
| `--all` (status) | Include exited sessions (default: running sessions plus those that ended within the last 24h) | `sprout automate status --all` |
| `--json` (status) | Output sessions as a JSON array (machine-readable) | `sprout automate status --json` |
| `--all` (stop) | Stop all running sessions (session ID not required) | `sprout automate stop --all` |
| `-f`, `--follow` (logs) | Follow output in real time — keeps tailing the log file until the process exits | `sprout automate logs abc123 -f` |
| `-n`, `--lines <N>` (logs) | Show only the last N lines (0 = all) | `sprout automate logs abc123 -n 100` |

To create workflows, activate the `workflow-automation` skill in an agent session or run `sprout skill list`.

### `sprout deploy`

Build the current project and ship it through a deploy target. The project's
`.sprout/deploy.json` names where it ships (`target` and `project`) and,
optionally, a build output directory; the build command comes from the starter
manifest `.sprout/starter.json`. The build runs in the workspace and the target
only ever receives the built output — the adapter never rebuilds on the target.

**Basic Usage:**

```bash
sprout deploy [flags]
sprout deploy status
sprout deploy history
sprout deploy rollback <id>
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--dir <path>` | Project root to deploy (default: nearest `.sprout/` walking up from the working directory) |
| `--target <id>` | Deploy target id, overriding `.sprout/deploy.json` |
| `--production` | Deploy to production (the live site); always requires `--yes` |
| `-y`, `--yes` | Confirm a production deploy without an interactive prompt |

**Deploy kind:** a preview deploy runs by default. A production deploy is
always gated on explicit confirmation — pass `--yes`, otherwise the command
refuses before anything is built or uploaded.

**Cloudflare Workers bindings:** when the deploy config's target is
`cloudflare` and the starter manifest declares `deploy_target: "workers"`, the
adapter reads the project's `wrangler.toml` and brings the declared bindings
with the deploy:

- For each `[[d1_databases]]` block (and `[[kv_namespaces]]` /
  `[[r2_buckets]]` blocks, only when present) the adapter finds or creates the
  resource in the account and attaches it to the uploaded script. A
  placeholder D1 `database_id` (all zeros, or empty) means "create it": the
  real id is written back into `wrangler.toml` so local and deployed config
  agree.
- Created resources are recorded in `.sprout/deploy-resources.json` and named
  from the worker name plus a stable suffix, so a re-run reuses them and two
  projects never share a resource by accident.
- Pending D1 migrations under the binding's `migrations_dir`
  (`drizzle/migrations` for the reference starter) are applied before the
  script goes live, tracked in the database's `d1_migrations` table so a
  re-run is idempotent.
- The deploy is all-or-nothing: if any declared binding cannot be created or
  attached, the deploy fails with a message naming it and the script is never
  uploaded — it never goes live with bindings missing.

**Credentials:** the Cloudflare adapter uses the existing deploy credential,
resolved from the credential store (preferred) or the environment variable
`CLOUDFLARE_API_TOKEN`. The account id comes from `CLOUDFLARE_ACCOUNT_ID`.
The token is never passed as a command argument or printed; it reaches the API
only in the request's `Authorization` header.

**Examples:**

```bash
sprout deploy                       # preview
sprout deploy --production --yes    # confirmed production
sprout deploy status                # latest deployment
sprout deploy history               # every recorded deployment
sprout deploy rollback my-app-2     # roll back to the previous deployment
```

---

### `sprout login`

Sign this machine's sprout in to your platform account. You approve it once
in the browser (the approval page names the account it is for); after that,
`sprout runner link` and `sprout gpu link` link this machine straight to that
account without another approval. You type the code sprout shows on the
platform's approval page (links never carry it), and the page warns when the
request comes from a different network than your browser. The sign-in can
read who you are, link machines to your account, and use the platform's MCP
server: list your projects, read task results and diffs, and add, run and
manage tasks. It has no direct git access, but tasks it adds change your
repositories as any task does and spend your credits; sprout asks before each
platform tool call unless you allow it. It is listed, and can be revoked,
under **Account → Developer** on the platform; signing in again revokes the
previous sign-in, and `sprout logout` revokes the current one.

On Pro and Team plans, `sprout login` also adds the platform's MCP server to
sprout's global MCP config as `sprout-foundry` (`<platform>/mcp`, the token in
the credential store, only a placeholder in `mcp_config.json`), so sprout's
agent gets the platform's tools from its next session. Signing in again
refreshes it; `sprout logout` removes it.

```bash
sprout login [--platform <url>] [--no-browser]
sprout whoami    # the account and plan this machine is signed in to, and the MCP server
sprout logout    # revoke the sign-in and remove the MCP server on this machine
```

The same three are slash commands — `/login [platform-url] [--no-browser]`,
`/whoami`, `/logout` — so you can sign in without leaving a session. They sign
in this machine, so the terminal and the browser share the result. `/whoami`
only reads and works from the browser too; `/login` waits inside the call for
the approval you make elsewhere, and `/logout` revokes a credential, so both
run in the terminal.

Without a sign-in, `runner link` and `gpu link` ask for a browser approval
each time, and print the account the machine was linked to; `sprout runner
status` and `sprout gpu status` show it too.

### `sprout gpu`

Link inference engines this machine runs (Ollama, llama.cpp, vLLM, MLX,
LM Studio: anything that answers OpenAI's `/v1/chat/completions`) to your
platform account, so your tasks and Sprout can run on their models — several
engines per machine, one link (and one background service) each. The agent
connects out to the platform over one WebSocket per link: no port is opened
on your network, and the platform reaches only the engine's model-serving
paths (`/v1/chat/completions`, `/v1/completions`, `/v1/embeddings`,
`/v1/models`, `/health`). Linking needs a paid plan on the platform.

**Basic Usage:**

```bash
sprout gpu link --engine 127.0.0.1:11434   # approve in the browser; then it serves in the background
sprout gpu link --engine 127.0.0.1:8021    # a second engine: named hostname-8021 automatically
sprout gpu link --sinter --model qwen3.5-9b  # or serve a model in-process with sinter (Apple Silicon)
sprout gpu install                         # (re)install every background service (one systemd user-unit instance / launchd agent per link)
sprout gpu start --name <name>             # serve one in the foreground instead (what the service runs)
sprout gpu status                          # every link, its service, and whether its engine answers
sprout gpu unlink --name <name>
sprout gpu rotate-key --name <name>        # trade a link's key for a new one
```

**`link` flags:**

| Flag | Description |
|------|-------------|
| `--engine <host:port>` | The engine as this machine reaches it (default `127.0.0.1:11434`, Ollama; llama.cpp 8080, vLLM 8000, LM Studio 1234). When it is not given and nothing answers at the default, Apple Silicon builds link with sinter instead |
| `--engine-key <key>` | The engine's own API key, if it requires one (kept in the credential store) |
| `--name <name>` | Name shown on the platform (default: the hostname) |
| `--platform <url>` | Platform URL (default `$SPROUT_PLATFORM_URL`, else the previous link) |
| `--no-browser` | Print the approval URL instead of opening a browser |
| `--sinter` | Serve a model in-process with sinter instead of a server at `--engine` (Apple Silicon builds) |
| `--fleet` | Link to the platform's own GPU fleet (a machine the platform owns or rents) instead of your account. Approved in the browser by a platform admin (or with `--token`); the GPU is then added as a Linked GPU (fleet) provider |
| `--no-service` | Link only; do not install and start the background service. By default `link` installs it (a systemd user-unit instance or launchd agent running `sprout gpu start --name <name>`, restarted if it exits) and restarts it after a new link; on Linux it also enables lingering where allowed, so the service keeps serving with nobody logged in |
| `--token <token>` | An enrollment token a platform admin minted: links to the fleet without the browser approval, for a rented box that links itself on boot. Single use and short-lived; implies `--fleet` |
| `--model <name\|dir>` | The model to serve with sinter (implies `--sinter`): a catalog name (e.g. `qwen3.5-9b`), an installed model, or a model directory; default: the model recommended for this machine's memory. A missing catalog model and the MLX runtime are downloaded once |

While linked, the agent also answers the platform's metrics poll itself
(it is never passed to the engine): the engine's load, token counts, latency
and cache figures read from its `/metrics` and `/slots` (ninfer, vLLM,
llama.cpp and SGLang are recognised), and the GPUs' utilisation, memory,
temperature, power, clock and fan from the engine's own GPU metrics where it
reports them (ninfer's `ninfer_gpu_*`). The agent runs nothing on the machine
to read the GPUs: an engine that reports no GPU metrics shows none.

`sprout gpu start` warns when a linked engine does not answer, and keeps the
tunnel open: the platform picks up the engine's models within a minute of
it starting. With `--sinter`, `sprout gpu start` loads the model and serves it through the
tunnel itself: no separate engine and no local port. Requests are served one
at a time, as the GPU runs one generation at a time.

A fleet GPU's agent keeps two tunnels open, each to a different platform
replica when it can, so a platform deploy never leaves it unreachable, and
rotates its key every 30 days. `sprout gpu rotate-key` rotates any link's key
by hand; a running agent keeps the old key until it restarts, and the old
key stops working once the new one connects, so a rotation the agent did not
save cannot lock it out.

After linking, choose one of the engine's models under **Account → Models**
on the platform. Each link's key and engine key live in sprout's credential
store under the link's slot — its name reduced to a file-safe form; its
state is one file in the config directory's `gpu/links/<slot>.json`.

**Several GPUs on one machine:** each link has its own name, state file, keys
and background service (`sprout-gpu@<name>.service` on Linux,
`com.sprout.gpu.<name>` on macOS). A link's default name is the machine's
hostname; when that is already linked, the next link takes `hostname-port`
(say `box-8021`) for free, so a second engine needs no flags. Pass `--name`
to `link` to choose it yourself, and to `start`, `unlink`, `rotate-key` and
`uninstall` to say which link (not needed while only one is linked).
`sprout gpu status` lists every link. A sprout that linked only one GPU
before this keeps working: its `gpu.json`, keys and single service migrate
to the per-link layout on the next run, and `gpu install` replaces the old
single service with per-link instances.

## Advanced Agent Flags

### Session Management

| Flag | Description | Example |
|------|-------------|---------|
| `--session-id <id>` | Specify a session identifier | `sprout agent --session-id my-session "continue work"` |
| `--last-session` | Resume previous session | `sprout agent --last-session "resume"` |

### Persona Selection

| Flag | Description | Example |
|------|-------------|---------|
| `--persona <name>` | Select agent persona | `sprout agent --persona coder "implement feature"` |

**Available Personas:**
- `orchestrator` — Process-oriented planning and delegation
- `general` — General-purpose tasks (default)
- `coder` — Feature implementation and production code
- `refactor` — Behavior-preserving refactoring
- `debugger` — Bug investigation and root cause analysis
- `tester` — Unit test writing and test coverage
- `reviewer` — Code review and security review (alias: `code_reviewer`)
- `researcher` — Combined local codebase analysis and external research
- `web_scraper` — Web extraction and structured content collection
- `coordinator` — Cross-project orchestration and task queue (alias: `executive_assistant`, `ea`)
- `computer_user` — Desktop automation with screenshots, mouse, and keyboard

### Performance & Safety

| Flag | Description | Example |
|------|-------------|---------|
| `--no-connection-check` | Skip provider connection check | `sprout agent --no-connection-check "task"` |
| `--max-iterations <n>` | Limit iterations (default: 1000) | `sprout agent --max-iterations 50 "task"` |
| `--no-stream` | Disable streaming for scripts | `SPROUT_NO_STREAM=1 sprout agent "task"` |
| `--no-subagents` | Disable subagent tools | `sprout agent --no-subagents "task"` |
| `--risk-profile <name>` | Shell-command gating profile: `readonly`, `cautious`, `default`, `permissive`, `unrestricted`. Overrides `config.risk_profile` for this session. See [SECURITY.md](SECURITY.md#risk-profiles). | `sprout agent --risk-profile readonly "audit the auth flow"` |
| `--unsafe` | Bypass security checks (use with caution) | `sprout agent --unsafe "task"` |

### Custom Prompts

| Flag | Description | Example |
|------|-------------|---------|
| `--system-prompt <file>` | Use custom system prompt from file | `sprout agent --system-prompt prompts/custom.md "task"` |
| `--system-prompt-str <text>` | Inline custom system prompt | `sprout agent --system-prompt-str "You are a security expert..." "task"` |

### Resource Management

| Flag | Description | Example |
|------|-------------|---------|
| `--resource-directory <dir>` | Store web/vision resources | `sprout agent --resource-directory captures "task"` |
| `--workflow-config <file>` | Run workflow configuration | `sprout agent --workflow-config examples/agent_workflow.json "task"` |
| `--trace-dataset-dir <dir>` | Enable dataset tracing | `sprout agent --trace-dataset-dir traces "task"` |
| `--prompt-stdin` | Read prompt from stdin | `echo "task" | sprout agent --prompt-stdin` |
| `--steer-inbox <dir>` | Steer a run with no terminal: messages dropped into the directory are delivered while it works | `sprout agent --steer-inbox /tmp/steers --json "task"` |

### Steering a headless run

`--steer-inbox <dir>` lets something outside the process add to a run while it
works, as if the person had typed a message mid-task:

- Write each message to its own file, `<id>.msg`, atomically: write it under
  another name in the same directory, then rename it. Ids are read in name
  order, so use ids that sort in the order you send them.
- At its next step the run claims the oldest message by renaming it to
  `<id>.taken`, then delivers it. One message is delivered per step, and only
  when no typed steer is waiting.
- To take a message back, delete `<id>.msg`. Once the run has claimed it, the
  delete fails and the message has been read.
- A message must be UTF-8 text of at most 8 KB; one that is not is set aside
  and reported dropped.
- With `--progress-events`, the run reports `progress: steer landed <id>` once
  the message is in the conversation, and `progress: steer dropped <id>
  <reason>` when it never got there. With `--json`, the result lists every
  message's outcome under `steers`: `landed`, `dropped`, or `unread` (still
  waiting, or claimed but not delivered before the run ended).

The inbox is read by an in-process run; a turn handed to a running daemon does
not read it.

### Model Selection

| Flag | Description | Example |
|------|-------------|---------|
| `--model <provider:model>` | Specify model | `sprout agent --model "deepinfra:qwen3-coder" "task"` |
| `--provider <name>` | Specify provider | `sprout agent --provider ollama "task"` |

### Web UI Control

| Flag | Description | Example |
|------|-------------|---------|
| `--no-web-ui` | Disable Web UI | `sprout agent --no-web-ui "task"` |
| `--web-port <port>` | Use custom port | `sprout agent --web-port 8080` |
| `-d`, `--daemon` | Daemon mode (keep Web UI running) | `sprout agent -d` |

---

## Slash Commands in Interactive Mode

In interactive `sprout` or `sprout agent`, use `/` for commands (tab-complete).

### Session & History

| Command | Description |
|---------|-------------|
| `/clear` | Clear conversation history |
| `/sessions [session_num]` | Show and load previous conversation sessions |
| `/log` | View changes |
| `/original` | Show the original text of the last language-corrected reply |

### Models & Providers

| Command | Description |
|---------|-------------|
| `/model [select\|<id>]` | List/select models (e.g., `/model select` for interactive dropdown) |
| `/provider [select\|<name>]` | Switch providers (e.g., `/provider ollama`) |

### Agent Features

| Command | Description |
|---------|-------------|
| `/commit` | Generate commit message |
| `/shell <desc>` | Generate shell script |
| `/init` | Regenerate workspace context |
| `/mcp` | Manage MCP servers |
| `/exit` | Quit session |

### Skills & Configuration

| Command | Description |
|---------|-------------|
| `/skill` | Install, update, remove, list, enable, or disable skills |
| `/transcript [preview\|markdown\|diff]` | Snapshot the current conversation to JSON for inspection (includes the session's provider/model) |
| `/custom` | Manage custom providers |
| `/login [platform-url]` | Sign this machine in to your platform account (terminal only — it waits for your browser approval) |
| `/whoami` | Show the platform account this machine is signed in to |
| `/logout` | Revoke this machine's platform sign-in (terminal only) |
| `/risk-profile [name\|clear]` | Show or change the shell-command risk profile mid-session. With no args, lists profiles and marks the active one. See [SECURITY.md](SECURITY.md#risk-profiles). |

### Help

| Command | Description |
|---------|-------------|
| `/help` | Show usage and all slash commands |

---

## Agent Personas

`sprout` supports nine specialized personas, each optimized for different types of tasks. See [`docs/PERSONAS.md`](PERSONAS.md) for detailed descriptions.

| Persona | Description |
|---------|-------------|
| `orchestrator` | Process-oriented planning and delegation |
| `general` | General-purpose tasks (default) |
| `coder` | Feature implementation and production code |
| `refactor` | Behavior-preserving refactoring specialist |
| `debugger` | Bug investigation and root cause analysis |
| `tester` | Unit test writing and test coverage |
| `reviewer` | Code review and security review (alias: `code_reviewer`) |
| `researcher` | Combined local codebase analysis and external research |
| `web_scraper` | Web extraction and structured content collection |
| `coordinator` | Cross-project orchestration and task queue (alias: `executive_assistant`, `ea`) |
| `computer_user` | Desktop automation with screenshots, mouse, and keyboard |

For strategic project planning, use the `project-planning` skill instead: `activate_skill project-planning`.

**Using Personas:**
```bash
sprout agent --persona coder "implement feature"
sprout agent --persona debugger "fix this bug"
sprout agent --persona reviewer "review my code"
sprout agent --persona researcher "analyze codebase and external sources"
sprout agent --persona web_scraper "extract structured content from web pages"
sprout agent --persona refactor "refactor code while preserving behavior"
sprout agent --persona computer_user "open the browser and navigate to example.com"
```

---

## Memory System

The memory system persists learned information across all conversations. Memories are markdown files stored in `~/.config/sprout/memories/` and automatically loaded into the system prompt.

### Memory Tool

The agent manages memories through the `manage_memory` tool (operations: `add`, `read`, `list`, `delete`, `search`). Ask the agent in natural language and it calls the tool for you:

```
sprout> remember that we never force-push to shared branches
sprout> what memories do you have about git workflow?
sprout> delete the git-safety memory
```

**Use Cases:**
- Store project conventions
- Save user preferences
- Remember discovered patterns
- Document workflow guidelines

---

## Ignoring Files

Add patterns to `.sprout/sproutignore` to exclude files from processing. The file respects `.gitignore` patterns.

**Setup:**
```bash
# Via agent or manually
echo "dist/" >> .sprout/sproutignore
echo "*.log" >> .sprout/sproutignore
```

**Example `.sprout/sproutignore`:**
```
# Build artifacts
dist/
build/
*.min.js

# Logs
*.log
logs/

# IDE
.idea/
.vscode/
*.swp

# OS files
.DS_Store
Thumbs.db
```

---

## Tool Suite

`sprout` includes a comprehensive built-in tool suite for all development tasks.

### File Operations

| Tool | Description |
|------|-------------|
| `edit_file` | Edit files with intelligent context |
| `read_file` | Read file contents with optional line ranges |
| `write_file` | Create or overwrite files |
| `search_files` | Search text in files using patterns |

### Structured File Operations

| Tool | Description |
|------|-------------|
| `write_structured_file` | Write schema-validated JSON/YAML files |
| `patch_structured_file` | Apply JSON Patch operations to JSON/YAML files |

### Web & Vision

| Tool | Description |
|------|-------------|
| `browse_url` | Open URLs in headless browser for screenshot/DOM/text extraction |
| `web_search` | Real-time web search for grounding knowledge |
| `analyze_ui_screenshot` | Analyze UI screenshots, mockups, or HTML files |

### Shell & Git

| Tool | Description |
|------|-------------|
| `shell_command` | Execute shell commands (allowlisted) |
| `git` | Execute git write operations (requires approval) |

### Memory & Skills

| Tool | Description |
|------|-------------|
| `manage_memory` | Persistent memory system (add/read/list/delete/search operations) |
| `list_skills` / `activate_skill` | Skill management for loading instruction bundles |

### Change History

| Tool | Description |
|------|-------------|
| `view_history` | View change history |
| `rollback_changes` | Revert changes |

### Todo Management

| Tool | Description |
|------|-------------|
| `todo_write` / `todo_read` | Todo management for breaking down tasks |

---

## MCP Server Integration

MCP (Model Context Protocol) extends `sprout` with external tools and services.

### Quick Start

```bash
# Interactive setup
sprout mcp add

# List servers
sprout mcp list

# Test connection
sprout mcp test [name]

# Remove server
sprout mcp remove [name]
```

### Configuration

- Config file: `~/.config/sprout/mcp_config.json`
- Use in agent: `Create GitHub PR for feature #WS`

### Features

- Connect to external tools and services
- GitHub integration (repos, issues, PRs)
- Custom server support
- Auto-discovery and auto-start options

See [docs/MCP_INTEGRATION.md](MCP_INTEGRATION.md) for detailed MCP setup.

---

## Workspace Initialization

The `.sprout/` directory is automatically created when you first run `sprout` commands. It contains:

- `config.json` — Local configuration overrides
- `sproutignore` — Ignore patterns (augments `.gitignore`)
- `changes/` — Per-change diff logs
- `memories/` — Persistent memory storage (markdown files)
- `revisions/` — Per-session directories with instructions and LLM responses
- `runlogs/` — JSONL workflow traces
- `workspace.log` — Verbose execution log
- `workspace_index/` — Workspace context index

The index is automatically updated on commands for fresh context.

---

## Related Documentation

- [Configuration](CONFIGURATION.md) — Config files, environment variables, and CI/CD
- [Architecture](ARCHITECTURE.md) — Project structure and design
- [MCP Integration](MCP_INTEGRATION.md) — MCP server setup
- [Agent Workflow](AGENT_WORKFLOW.md) — Workflow configuration
- [Provider Catalog](PROVIDER_CATALOG.md) — Available providers and models
- [Testing](TESTING.md) — Test strategy and commands
