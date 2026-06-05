# Workflow Configuration Reference

This document describes every property available in a Sprout workflow configuration JSON file. Use it to understand, customize, and create workflows.

---

## Top-Level Properties

These properties configure the overall workflow behavior.

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `description` | string | *(none)* | Human-readable description shown by `sprout automate list` and `list_automate_workflows`. Strongly recommended — helps users identify workflows. |
| `initial` | object | *(required)* | The first agent run. Defines persona, model, provider, and prompt. See [Initial Run](#initial-run). |
| `steps` | array | `[]` | Additional runs that execute after the initial run, in sequence. See [Steps](#steps). |
| `continue_on_error` | boolean | `false` | If `true`, keep processing steps even if a previous step fails. If `false`, stop on first error. **Recommendation**: `true` for autonomous workflows, `false` for quality-gated pipelines. |
| `persist_runtime_overrides` | boolean | `false` | If `true`, save runtime provider/model changes back to config. Usually `false` to keep the workflow self-contained. |
| `no_web_ui` | boolean | `false` | If `true`, run without the web interface. **Recommendation**: `true` for autonomous workflows. |
| `web_port` | integer | `0` | Override the web UI port. `0` means use default. |
| `daemon` | boolean | `false` | If `true`, run as a persistent daemon. |
| `orchestration` | object | *(none)* | External orchestration config for multi-process coordination. See [Orchestration](#orchestration). |
| `requires_approval` | boolean | `true` | When `false`, the `run_automate` agent tool launches this workflow without surfacing an intent-confirmation prompt. See [Auto-Approval](#auto-approval). Set only for agent-runnable validation workflows. CLI path (`sprout automate run`) still prompts. |
| `budget` | object | *(none)* | USD spend cap for the workflow. See [Budget](#budget). **Strongly recommended for autonomous runs.** |
| `progress` | object | *(none)* | Runtime visibility config. See [Progress](#progress). |

---

## Initial Run

The `initial` object defines the first agent interaction. This is the entry point of the workflow.

```json
"initial": {
  "prompt": "...",
  "prompt_file": "...",
  "persona": "...",
  "provider": "...",
  "model": "...",
  "skip_prompt": true,
  "max_iterations": 500,
  "risk_profile": "permissive",
  "subagent_overrides": { ... }
}
```

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `prompt` | string | *(none)* | Inline instructions for the agent. Use for short prompts. Mutually exclusive with `prompt_file`. |
| `prompt_file` | string | *(none)* | Path to a `.md` file with instructions. Preferred for complex workflows — easier to read and edit. Mutually exclusive with `prompt`. |
| `persona` | string | `"orchestrator"` | The agent persona. Common values: `orchestrator`, `coordinator`, `coder`, `reviewer`, `tester`. |
| `provider` | string | config default | LLM provider (e.g., `openai`, `anthropic`, `openrouter`, `ollama`, `deepseek`, `zai`). |
| `model` | string | config default | Model ID (e.g., `gpt-4o`, `claude-sonnet-4-20250514`, `deepseek-chat`). |
| `skip_prompt` | boolean | `false` | If `true`, don't prompt the user for input — use the provided prompt directly. **Set to `true` for autonomous workflows.** |
| `max_iterations` | integer | `100` | Maximum tool-use iterations before stopping. Higher values allow longer autonomous runs. |
| `no_stream` | boolean | `false` | If `true`, disable streaming output. Useful for scripting. |
| `system_prompt` | string | *(none)* | Custom system prompt. Overrides the persona's default. |
| `system_prompt_file` | string | *(none)* | Path to a system prompt file. Overrides the persona's default. |
| `unsafe` | boolean | `false` | If `true`, allow all tools without restrictions. **Set to `true` for full autonomous workflows.** |
| `no_subagents` | boolean | `false` | If `true`, disable subagent delegation. The agent does all work itself. |
| `dry_run` | boolean | `false` | If `true`, preview what would happen without making changes. Good for testing. |
| `resource_directory` | string | *(none)* | Directory for captured web/vision resources. |
| `reasoning_effort` | string | `"medium"` | How hard the model thinks: `low`, `medium`, `high`. Higher = better reasoning but slower and more expensive. |
| `risk_profile` | string | config default | Shell command safety level: `readonly`, `cautious`, `default`, `permissive`, `unrestricted`. **Use `permissive` for autonomous workflows.** |
| `subagent_overrides` | object | *(none)* | Per-persona provider/model overrides for subagent delegation. See [Subagent Overrides](#subagent-overrides). |

---

## Steps

Each step in the `steps` array is an additional agent run that executes after the previous one completes.

```json
"steps": [
  {
    "name": "deep_review",
    "persona": "reviewer",
    "prompt": "Review the staged changes...",
    "reasoning_effort": "high",
    "when": "on_success",
    "model": "claude-sonnet-4-20250514",
    "provider": "anthropic"
  }
]
```

All properties from the Initial Run are available, plus these step-specific properties:

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `name` | string | auto-generated | A descriptive name for this step. Used in logging and orchestration state. |
| `when` | string | `"always"` | When to run this step: `on_success` (only if previous succeeded), `on_error` (only if previous failed), `always` (regardless). |
| `file_exists` | string[] | *(none)* | Only run this step if ALL listed files exist. Useful for conditional steps. |
| `file_not_exists` | string[] | *(none)* | Only run this step if NONE of the listed files exist. Useful for "create if missing" patterns. |
| `command` | string | *(none)* | Shell command to run as a non-inference step (mutually exclusive with `prompt` / `prompt_file`). Runs via `$SHELL -c`. A non-zero exit code marks the step as failed. |
| `command_file` | string | *(none)* | Path to a shell script run as a non-inference step (mutually exclusive with `prompt` / `prompt_file` / `command`). |

### Step Kinds: Agent vs. Shell

Every step is one of two kinds:

| Kind | How to declare | When to use |
|------|---------------|-------------|
| **Agent step** | `prompt` or `prompt_file` | Anything that requires reasoning, tool use, code-writing, or review. Token-consuming. |
| **Shell step** | `command` or `command_file` | Deterministic work that does NOT need the LLM — build verification, formatters, custom scripts, status checks. No tokens consumed. |

The two declarations are mutually exclusive on the same step. Provider/model/persona settings and `subagent_overrides` are ignored on shell steps. The shared step properties (`name`, `when`, `file_exists`, `file_not_exists`) work for both kinds.

```json
{
  "name": "verify_build",
  "command": "make build-all",
  "when": "on_success"
}
```

```json
{
  "name": "deploy",
  "command_file": "automate/scripts/deploy.sh",
  "when": "on_success",
  "file_exists": ["dist/index.html"]
}
```

### When Conditions Explained

| Value | Meaning | Use Case |
|-------|---------|----------|
| `on_success` | Run only if previous step succeeded | Review after implementation, summarize after review |
| `on_error` | Run only if previous step failed | Error recovery, root cause analysis, escalation |
| `always` | Run regardless of previous outcome | Final summaries, cleanup, reporting |

### Conditional Execution Examples

```json
// Only run code review if go.mod exists (Go project)
{
  "name": "go_review",
  "file_exists": ["go.mod"],
  "prompt": "Review Go code...",
  "when": "on_success"
}

// Only generate docs if docs/ doesn't already exist
{
  "name": "generate_docs",
  "file_not_exists": ["docs/IMPLEMENTATION_NOTES.md"],
  "prompt": "Create implementation notes...",
  "when": "on_success"
}
```

---

## Subagent Overrides

The `subagent_overrides` section is the **primary cost control mechanism**. It maps persona IDs to provider/model pairs, so subagents use cheaper models while the primary agent uses an expensive one.

```json
"subagent_overrides": {
  "coder": {
    "provider": "openrouter",
    "model": "deepseek/deepseek-chat"
  },
  "tester": {
    "provider": "openrouter",
    "model": "deepseek/deepseek-chat"
  },
  "reviewer": {
    "provider": "openrouter",
    "model": "anthropic/claude-sonnet-4-20250514"
  },
  "debugger": {
    "provider": "openrouter",
    "model": "deepseek/deepseek-chat"
  },
  "repo_orchestrator": {
    "provider": "openrouter",
    "model": "anthropic/claude-sonnet-4-20250514"
  }
}
```

### How It Works

1. The primary agent (defined in `initial`) receives the task
2. When it delegates work via `run_subagent`, each subagent uses the provider/model from this mapping
3. If a persona is NOT listed here, the subagent uses the system's default subagent provider/model
4. This lets you use a **high-quality model for orchestration** and **cheaper models for focused work**

### Available Personas for Override

| Persona | What it does | Model requirements |
|---------|-------------|-------------------|
| `coder` | Writes production code, fixes bugs | Good coding ability, follows specifications |
| `tester` | Writes and runs tests | Good at edge cases, test patterns |
| `reviewer` | Reviews code for quality/security | Strong analysis, attention to detail |
| `debugger` | Investigates and fixes bugs | Good root cause analysis |
| `repo_orchestrator` | Coordinates multi-step work within a task | Strong delegation and planning |
| `researcher` | Investigates codebase and researches solutions | Good comprehension, web search |
| `refactor` | Behavior-preserving code restructuring | Good understanding of patterns |

### Cost Optimization Strategy

The key insight: **subagents do focused, well-scoped work** that doesn't require the most expensive models.

**Recommended tiering:**

| Role | Model tier | Why |
|------|-----------|-----|
| Primary agent (initial) | Best available | Makes complex decisions, orchestrates, reviews output quality |
| `repo_orchestrator` | Mid-tier or better | Needs to delegate correctly and verify subagent output |
| `reviewer` | Mid-tier or better | Needs strong analysis for security and quality |
| `coder` | Budget to mid-tier | Follows specific instructions, writes focused code |
| `tester` | Budget to mid-tier | Follows test patterns, writes focused tests |
| `debugger` | Budget to mid-tier | Follows error messages, makes targeted fixes |

**Warning**: If subagent models are too weak:
- Code quality drops → primary agent spends more iterations fixing → costs more overall
- Tests may be incomplete → bugs slip through → more rework later
- Reviews may miss issues → quality debt accumulates

**Sweet spot**: Models that are good at *following specific instructions* but don't need the *reasoning power* of the primary agent.

---

## Auto-Approval

By default, every `run_automate` tool call surfaces an intent-confirmation prompt to the user. Workflows designed to be invoked by an agent as part of its expected workflow (e.g. a validation workflow referenced from `AGENTS.md`) can opt out:

```json
{
  "requires_approval": false,
  "description": "Validates the build before considering work done.",
  ...
}
```

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `requires_approval` | boolean | `true` | When `false`, the agent tool path skips the confirmation prompt. CLI path still prompts unless `--yes` is passed. |

**Trade-off:** Anyone with write access to the workflow file can flip this. The CLI overview displays a prominent `⚠ requires_approval: false` line so the security implication is visible to a reader of the JSON. Don't use this for workflows that commit, push, deploy, or have high cost potential — and combine with `budget.usd` if you must.

## Budget

USD spend cap that crosses the primary agent and every subagent it spawns. The most important safety net for autonomous workflows — without it, an agent in a loop can burn unlimited dollars.

```json
"budget": {
  "usd": 10.00,
  "warn_at": [0.5, 0.8],
  "on_exceed": "truncate"
}
```

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `usd` | number | *(required)* | Hard cap on cumulative USD spend across the workflow. `0` or omitted means no cap. |
| `warn_at` | number[] | `[0.5, 0.8]` | Fractional thresholds in `(0, 1]`. Each crossing emits a one-time `[budget] WARNING` line to stdout. Sorted ascending automatically. |
| `on_exceed` | string | `"truncate"` | What happens when `usd` is reached. `truncate` finishes the current LLM response then stops gracefully. `stop` is reserved for future hard-kill behavior (currently behaves like truncate). |

**Runtime behavior:**
- Every LLM response (primary and subagent) debits its cost to a shared counter.
- Threshold warnings fire at most once each.
- When the cap is reached, the truncation flag is set; the agent finishes its current response, then the workflow terminates with status `fleet_budget_exceeded`.
- The cost-tracking that drives this is the same cost data used to render `GetTotalCost()` in the agent state — pricing accuracy depends on the provider returning structured usage.

**CLI overrides** (override the workflow JSON for a single run):

```bash
sprout automate run NAME --budget-usd 5
sprout automate run NAME --budget-warn 0.5,0.9
```

## Progress

```json
"progress": {
  "heartbeat_seconds": 600
}
```

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `heartbeat_seconds` | integer | `600` when a budget is set, off otherwise | Interval at which a single-line `[budget] $X of $Y · iter N · elapsed Tm` is printed to stdout. `0` disables. |

CLI override: `sprout automate run NAME --heartbeat 120` for tighter visibility.

## Orchestration

For advanced use cases where an external process coordinates multiple workflow runs:

```json
"orchestration": {
  "enabled": true,
  "resume": true,
  "yield_on_provider_handoff": true,
  "state_file": ".sprout/workflow_state.json",
  "events_file": ".sprout/workflow_events.jsonl",
  "conversation_session_id": "workflow"
}
```

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable external orchestration mode. |
| `resume` | boolean | `false` | Resume from last completed step on restart. |
| `yield_on_provider_handoff` | boolean | `false` | Pause when switching providers (for multi-provider concurrency control). |
| `state_file` | string | `.sprout/workflow_state.json` | Path to store execution state. |
| `events_file` | string | `.sprout/workflow_events.jsonl` | Path to append event logs. |
| `conversation_session_id` | string | `"workflow"` | Session ID for conversation persistence. |

---

## Complete Example: Full Autonomous TODO Processor

```json
{
  "description": "Processes TODO.md items one at a time with full build/test/review cycle, then commits each.",
  "continue_on_error": true,
  "initial": {
    "max_iterations": 500,
    "model": "claude-sonnet-4-20250514",
    "persona": "coordinator",
    "prompt_file": "automate/workflow_prompt.md",
    "provider": "anthropic",
    "risk_profile": "permissive",
    "skip_prompt": true,
    "subagent_overrides": {
      "coder": { "provider": "openrouter", "model": "deepseek/deepseek-chat" },
      "tester": { "provider": "openrouter", "model": "deepseek/deepseek-chat" },
      "reviewer": { "provider": "anthropic", "model": "claude-sonnet-4-20250514" },
      "debugger": { "provider": "openrouter", "model": "deepseek/deepseek-chat" },
      "repo_orchestrator": { "provider": "anthropic", "model": "claude-sonnet-4-20250514" }
    }
  },
  "no_web_ui": true,
  "persist_runtime_overrides": false
}
```

## Complete Example: Multi-Step Review Pipeline

```json
{
  "continue_on_error": false,
  "initial": {
    "max_iterations": 500,
    "persona": "orchestrator",
    "prompt": "Implement the following task: '{TODO_TEXT}'. Build, test, then review the solution.",
    "skip_prompt": true,
    "model": "claude-sonnet-4-20250514",
    "provider": "anthropic",
    "unsafe": true
  },
  "no_web_ui": true,
  "steps": [
    {
      "name": "deep_review",
      "persona": "reviewer",
      "prompt": "Perform a deep evidence-based code review of all staged changes. Read the staged diff, then read each changed file for full context. Analyze for correctness, edge cases, error handling, security, and code quality. If the code looks good with no issues, say REVIEW_STATUS: APPROVED. Do NOT make any code changes or stage anything. Do NOT commit.",
      "reasoning_effort": "high",
      "skip_prompt": true,
      "unsafe": true,
      "model": "claude-sonnet-4-20250514",
      "provider": "anthropic",
      "when": "on_success"
    },
    {
      "name": "fix_review_issues",
      "persona": "orchestrator",
      "prompt": "Review the findings from the previous step. If APPROVED, do nothing. Otherwise, fix valid issues using subagents. After fixing, re-review. Iterate until resolved. Stage only files you modified. Do NOT commit.",
      "reasoning_effort": "high",
      "skip_prompt": true,
      "unsafe": true,
      "model": "claude-sonnet-4-20250514",
      "provider": "anthropic",
      "when": "on_success"
    }
  ]
}
```

---

## Running a Workflow

```bash
# Run a workflow
sprout agent --workflow-config automate/workflow.json

# Run with skip-prompt (overrides config)
sprout agent --workflow-config automate/workflow.json --skip-prompt

# Run without web UI (overrides config)
sprout agent --workflow-config automate/workflow.json --no-web-ui
```

### Resuming After Interruption

If orchestration is enabled with `resume: true`:
```bash
# Simply re-run the same command — it picks up where it left off
sprout agent --workflow-config automate/workflow.json
```

## Monitoring a Running Workflow

### CLI Commands

Sprout provides three CLI commands for monitoring and controlling running workflows:

#### Status

```bash
# Show all running automate sessions
sprout automate status

# Include exited sessions
sprout automate status --all

# JSON output for scripting
sprout automate status --json
```

The status command reads `.sprout/automate/*.json` PID files and checks process liveness. Sessions are shown in a table with session ID, workflow name, status (running/exited), PID, start time, and elapsed duration.

#### Stop

```bash
# Stop a specific session
sprout automate stop <session_id>

# Stop all running sessions
sprout automate stop --all
```

Stop uses a graduated signal sequence: SIGINT → 10s grace → SIGTERM → 5s → SIGKILL. The PID file is removed after stopping.

#### Logs

```bash
# View captured output
sprout automate logs <session_id>

# Follow output in real time (500ms polling)
sprout automate logs <session_id> -f

# Show only last N lines
sprout automate logs <session_id> -n 50
```

Logs reads from the output file path stored in the PID file. Follow mode polls at 500ms and stops when the process exits. CLI-launched sessions pipe output directly to the terminal and may not have a captured output file.

### Cross-Terminal Discovery

Workflows write a PID file to `.sprout/automate/<session_id>.json` on launch. This enables cross-terminal monitoring — you can start a workflow from one terminal and check its status from another:

```bash
# Terminal A: start a workflow
sprout automate run my_workflow

# Terminal B: check status from a different terminal
sprout automate status
sprout automate logs <session_id> -f
```

Stale PID files (from processes that have exited) can be cleaned up manually using `sprout automate stop <session_id>` to remove specific sessions, or by stopping all sessions.

### Integration with Agent Sessions

Within an agent session, the `run_automate` tool launches workflows as background processes tracked by the BackgroundProcessManager. Use `shell_command(check_background=<session_id>)` to poll output and `shell_command(stop_background=<session_id>)` to stop them.

## WebUI Usage

The Automations panel in the WebUI provides a visual interface for discovering, running, and monitoring workflows.

### Opening the Panel

Click the **Automations** tab in the sidebar navigation (the Layers icon). The panel has three sections:

- **Available**: Lists all workflow configurations found in your project's `automate/` directory
- **Running**: Shows active sessions with real-time status, elapsed time, and stop controls
- **Recent**: Displays completed sessions with output preview

### Running a Workflow

1. Click the **Run** button on any workflow card
2. Optionally set a per-run budget or heartbeat interval
3. Confirm to start — the workflow runs in the background
4. Monitor progress in the Running tab

### Session Details

Click any session to view:
- Session status and elapsed time
- Captured output stream (for agent-launched sessions)
- Budget utilization bar

### Chat Integration

When a workflow is started via the `run_automate` tool in a chat session, the response includes a direct link to the Automations panel for that session.
