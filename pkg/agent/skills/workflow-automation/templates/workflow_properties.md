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
| `persona` | string | `"orchestrator"` | The agent persona. Common values: `orchestrator`, `executive_assistant`, `coder`, `code_reviewer`, `tester`. |
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
    "persona": "code_reviewer",
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
  "code_reviewer": {
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
| `code_reviewer` | Reviews code for quality/security | Strong analysis, attention to detail |
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
| `code_reviewer` | Mid-tier or better | Needs strong analysis for security and quality |
| `coder` | Budget to mid-tier | Follows specific instructions, writes focused code |
| `tester` | Budget to mid-tier | Follows test patterns, writes focused tests |
| `debugger` | Budget to mid-tier | Follows error messages, makes targeted fixes |

**Warning**: If subagent models are too weak:
- Code quality drops → primary agent spends more iterations fixing → costs more overall
- Tests may be incomplete → bugs slip through → more rework later
- Reviews may miss issues → quality debt accumulates

**Sweet spot**: Models that are good at *following specific instructions* but don't need the *reasoning power* of the primary agent.

---

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
    "persona": "executive_assistant",
    "prompt_file": "automate/workflow_prompt.md",
    "provider": "anthropic",
    "risk_profile": "permissive",
    "skip_prompt": true,
    "subagent_overrides": {
      "coder": { "provider": "openrouter", "model": "deepseek/deepseek-chat" },
      "tester": { "provider": "openrouter", "model": "deepseek/deepseek-chat" },
      "code_reviewer": { "provider": "anthropic", "model": "claude-sonnet-4-20250514" },
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
      "persona": "code_reviewer",
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

### Monitoring Progress

- **Workflow state**: Check `.sprout/workflow_state.json` for current step and status
- **Event log**: Check `.sprout/workflow_events.jsonl` for detailed execution log
- **Task queue**: Check `~/.config/sprout/task_queue.json` for EA task progress

### Resuming After Interruption

If orchestration is enabled with `resume: true`:
```bash
# Simply re-run the same command — it picks up where it left off
sprout agent --workflow-config automate/workflow.json
```
