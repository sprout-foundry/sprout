---
name: Self-Help
description: Internal help and settings reference. Activate when the user asks "how do I...", wants to configure settings, or needs help understanding Sprout's capabilities.
---

# Self-Help & Settings Reference

You are Sprout's internal help system. Use this skill to quickly answer "how do I..." questions about configuring, using, and understanding Sprout's capabilities.

## How to Respond

- Keep answers **scannable** — use tables and bullet points, not paragraphs
- Give the **exact tool call or slash command** to run
- If the user's intent is unclear, use `ask_user` to clarify before acting
- After configuring something, use `add_memory` to persist preferences if they seem intentional

---

## 1. Settings Catalog

Use `manage_settings` to get, set, and test settings. Values are persisted across sessions.

| Key | Type | Valid Values | Description |
|-----|------|-------------|-------------|
| `provider` | string | `openai`, `anthropic`, `deepseek`, `openrouter`, `ollama`, `ollama-local`, `lmstudio`, `deepinfra`, `cerebras`, `chutes`, `minimax`, `mistral`, `zai`, or custom | Current LLM provider |
| `model` | string | Any model ID for active provider | Current model for the active provider |
| `reasoning_effort` | enum | `low`, `medium`, `high` | Reasoning depth for thinking-capable models |
| `disable_thinking` | boolean | `true`, `false` | Disable extended thinking mode (Qwen3, GLM, Minimax, etc.) |
| `resource_directory` | string | Any valid path | Directory for captured web/vision resources |
| `history_scope` | enum | `project`, `global` | Whether change history is scoped to current project or shared globally |
| `subagent_provider` | string | Any provider name | Default provider for subagent tasks |
| `subagent_model` | string | Any model ID | Default model for subagent tasks |

### Provider-specific settings

| Key | Type | Description |
|-----|------|-------------|
| `commit_provider` / `commit_model` | string | Provider/model used for commit message generation |
| `review_provider` / `review_model` | string | Provider/model used for code review |

### `manage_settings` Operations

| Operation | Required Args | Description |
|-----------|---------------|-------------|
| `get` | `key` | Retrieve a setting value |
| `set` | `key`, `value` | Update a setting value |
| `list_providers` | `provider` (optional) | List available providers, optionally filtered |
| `test_credential` | `provider` | Check if a provider has valid API credentials |

### Examples

```
# Check current provider
manage_settings(operation="get", key="provider")

# Switch provider
manage_settings(operation="set", key="provider", value="openai")

# Verify credentials after switching
manage_settings(operation="test_credential", provider="openai")

# Change model
manage_settings(operation="set", key="model", value="gpt-4o")

# List all providers
manage_settings(operation="list_providers")

# List providers matching a filter
manage_settings(operation="list_providers", provider="open")
```

---

## 2. Slash Commands

| Command | Aliases | Description |
|---------|---------|-------------|
| `/help` | `/?`, `/h` | Show help and available commands |
| `/model` | `/m` | List/select models for current provider |
| `/provider` | `/p` | Show status or switch providers |
| `/persona` | — | Apply/configure direct personas (provider, model, tools, prompt) |
| `/risk-profile` | — | Show or change shell-command risk profile |
| `/mcp` | — | Manage MCP server configuration |
| `/commit` | — | Interactive commit workflow |
| `/review` | — | AI code review on staged Git changes |
| `/review-deep` | — | Deep evidence-based code review |
| `/stats` | — | Session statistics and usage info |
| `/sessions` | — | List/manage chat sessions |
| `/clear` | — | Clear conversation history |
| `/compact` | — | Compact conversation to save tokens |
| `/edit` | — | Open $EDITOR to compose a query |
| `/exec` | — | Execute a shell command |
| `/shell` | — | Open interactive shell |
| `/exit` | `/q`, `/x` | Exit Sprout |
| `/init` | — | Initialize Sprout configuration |
| `/changes` | — | List recent file changes |
| `/log` | — | Show change history log |
| `/rollback` | — | Rollback tracked changes |
| `/status` | — | Show current working tree status |
| `/index` | — | Manage semantic search index |
| `/extend` | — | Extend current session context window |
| `/subagent-provider` | — | Configure subagent default provider |
| `/subagent-model` | — | Configure subagent default model |
| `/subagent-personas` | — | List all subagent personas |
| `/subagent-persona` | — | Show or configure a specific persona |

**Tip:** Use `/help <command>` for per-command details (e.g., `/help model`).

---

## 3. Common Workflows

### Switch Providers
```
1. manage_settings(operation="list_providers")          # See what's available
2. manage_settings(operation="set", key="provider", value="openai")
3. manage_settings(operation="test_credential", provider="openai")  # Verify it works
```
Or use the slash command: `/provider select` (interactive picker) or `/provider openai` (direct).

### Change Model
```
1. /model                              # List available models
2. manage_settings(operation="set", key="model", value="gpt-4o")
```
Or use: `/model gpt-4o` (direct) or `/model select` (interactive picker).

### Configure Subagents
```
# Set defaults for all subagents
manage_settings(operation="set", key="subagent_provider", value="openai")
manage_settings(operation="set", key="subagent_model", value="gpt-4o")

# Configure individual persona overrides via slash commands
/subagent-persona coder provider openai
/subagent-persona coder model gpt-4o
/subagent-personas                               # List all personas
```

### Configure Code Review Pipeline
```
# The review_provider and review_model are read from config
# Set them via manage_settings for the review pipeline:
# Note: these are configuration-level settings, not in supportedSettings for manage_settings
# Use /review and /review-deep commands to trigger reviews on staged changes
```

### Adjust Risk Profile
```
/risk-profile                          # Show current profile and options
/risk-profile cautious                 # Switch to cautious (most ops prompt)
/risk-profile permissive               # High trust, minimal prompting
/risk-profile clear                    # Clear override, use config default
```

Profiles from strictest to loosest:
| Profile | Behavior |
|---------|----------|
| `readonly` | Only read operations; writes blocked |
| `cautious` | Most operations prompt; subagent writes blocked |
| `default` | Built-in defaults (historical behavior) |
| `permissive` | High trust; almost everything passes |
| `unrestricted` | No gating except Critical threats (rm -rf /, fork bombs) |

### Create a Custom Skill
```
1. Create directory: .sprout/skills/<name>/
2. Write SKILL.md with YAML frontmatter:

---
name: <name>
description: <description — appears in /help and skill listings>
---

# Your skill instructions here
```
User-level skills go in `~/.config/sprout/skills/<name>/SKILL.md`.
Project-level skills go in `.sprout/skills/<name>/SKILL.md` (available to anyone in the repo).

**Hot-reload:** Skills added through the webui or `/api/skills/install` appear in `list_skills` immediately — no restart. Skills dropped on disk are picked up on the next config reload (triggered by any settings change or `/mcp` command).

### Set Up MCP Servers (External Tools)
```
activate_skill("mcp-setup")           # Load the full MCP setup guide
```
Or configure directly:
- **Webui**: Settings → MCP → Add Server (hot-reloads instantly — no restart)
- **Slash command**: `/mcp add` (interactive) or `/mcp list` (status)
- **Config**: Edit `mcp.servers` in `~/.config/sprout/config.json`

MCP servers added through the webui **start immediately**. The agent's available tools are refreshed automatically after add/update/delete. Common servers: GitHub, filesystem, PostgreSQL, browser automation.

### Onboard to a New Project
```
1. activate_skill project-planning       # Load planning workflow
2. Or for a familiar repo: activate_skill repo-onboarding
```

### Change Reasoning Effort
```
manage_settings(operation="set", key="reasoning_effort", value="high")  # More reasoning
manage_settings(operation="set", key="reasoning_effort", value="low")   # Less reasoning, faster
```

### Disable Thinking Mode
```
manage_settings(operation="set", key="disable_thinking", value="true")   # Turn off thinking
manage_settings(operation="set", key="disable_thinking", value="false")  # Turn on thinking
```
Affects models like Qwen3, GLM, and Minimax that support extended thinking.

### Run an Autonomous Workflow Over a TODO List

For batch work like processing a roadmap TODO list without human intervention, use the autonomous workflow system. The coordinator persona reads `TODO.md`, delegates each item to a specialist, and commits results — see the `workflow-automation` skill for the full walkthrough.

```
1. activate_skill("project-planning")          # Plan tasks into TODO.md
2. activate_skill("workflow-automation")       # Generate an automate/workflow.json
3. sprout automate run automate/workflow.json  # Run it autonomously
```

The `TODO.md → [x]` transition is the only persistent state. Progress shows up live via the TodoWrite UI indicator. No task queue file is involved.

---

## 4. Tools for Settings & Help Actions

| Tool | When to Use |
|------|-------------|
| `manage_settings` | Get/set/list/test/describe/preview settings and credentials |
| `ask_user` | Clarify intent before making changes |
| `add_memory` | Persist preferences learned during setup (e.g., "user prefers OpenRouter + Claude") |
| `list_skills` | Discover available skills |
| `activate_skill` | Load a skill's instructions into context |
| `run_subagent` | Delegate a task to a specialist persona (coder, tester, etc.) |

### Common Patterns

**User asks "how do I change my provider?"**
```
→ Show them the options from the settings catalog above
→ Offer to do it: manage_settings(operation="set", key="provider", value="<provider>")
→ Test credentials: manage_settings(operation="test_credential", provider="<provider>")
```

**User asks "which model should I use for subagents?"**
```
→ Check current: manage_settings(operation="get", key="subagent_model")
→ Suggest a cost/performance appropriate model for their use case
→ Set it: manage_settings(operation="set", key="subagent_model", value="gpt-4o")
```

**User asks "how do I set up code review?"**
```
→ Explain the review pipeline settings
→ Explain /review and /review-deep commands
```

**User asks "what can Sprout do?"**
```
→ Point to /help for commands
→ Point to list_skills for skills
→ Summarize key capabilities: code editing, shell commands, subagents, review, memory
```

**User asks "how do I batch-process my TODO list?"**
```
→ Point them at activate_skill("workflow-automation") for the autonomous TODO processor
→ The coordinator reads TODO.md, delegates each item, commits, marks [x]
→ The same workflow runs locally via the CLI or via automate/run_automate
```

**User asks "set up my project for autonomous work"**
```
1. activate_skill("project-planning") to plan
2. activate_skill("workflow-automation") to generate the autonomous workflow
3. Run: sprout automate run automate/workflow.json
```

---

## 5. Background Shell Sessions

Long-running commands can be promoted to background via `shell_command(background=true, command="...")`. The returned `session_id` lets you:

| Command | Purpose |
|---------|---------|
| `sprout shell-bg list` | List active background sessions |
| `sprout shell-bg status <id>` | Get accumulated output + runtime + status |
| `sprout shell-bg stop <id>` | Stop a session (graceful SIGINT→SIGTERM→SIGKILL cascade) |
| `sprout shell-bg stop-all` | Stop every active session |

Works in both CLI and WebUI modes.

---

## 6. Configuration File Locations

| Location | Purpose |
|----------|---------|
| `~/.config/sprout/config.json` | User-level config (provider, model, preferences) |
| `.sprout/config.json` | Workspace-level config (overrides user config) |
| `~/.config/sprout/api_keys.json` | API keys and credentials |
| `~/.config/sprout/skills/` | User-level custom skills |
| `.sprout/skills/` | Project-level custom skills |
| `~/.config/sprout/memories/` | Persistent cross-session memories |

Config precedence: workspace config (`.sprout/config.json`) overrides user config (`~/.config/sprout/config.json`).

### Hot-Reload (No Restart Needed)

Most configuration changes take effect immediately without restarting sprout:

| Change | Hot-Reloads? | How |
|--------|-------------|-----|
| Provider/model switch | ✅ Yes | `manage_settings` or webui settings |
| MCP server add/update/remove | ✅ Yes | Webui settings or `/mcp` — server starts/stops live |
| Skill install/uninstall | ✅ Yes | Webui skills panel or `/api/skills/install` |
| Skill dropped on disk | ✅ On next reload | Picked up on next settings change or `/mcp` |
| Reasoning effort / thinking | ✅ Yes | `manage_settings` |
| Risk profile | ✅ Yes | `/risk-profile` |
| Persona configuration | ✅ Yes | `/subagent-persona` |
| Memory add/delete | ✅ Yes | `manage_memory` |