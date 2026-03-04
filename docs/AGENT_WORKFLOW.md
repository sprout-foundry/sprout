# Agent Workflow

Agent workflow lets `ledit agent` run a config-driven sequence of prompts with conditional routing and per-step runtime overrides.

Use it for CI/CD orchestration, post-implementation audits, error recovery paths, and multi-model review pipelines.

## Command

```bash
ledit agent --workflow-config path/to/workflow.json
```

You can still pass a CLI prompt:

```bash
ledit agent --workflow-config path/to/workflow.json "Implement feature X"
```

If both are provided, the CLI prompt is used as the initial prompt.

## How It Runs

1. Load workflow config from `--workflow-config`.
2. Apply top-level startup overrides (`no_web_ui`, `web_port`, `daemon`).
3. Resolve initial prompt:
   - CLI argument (highest priority)
   - `initial.prompt`
   - `initial.prompt_file`
4. Apply `initial` runtime overrides.
5. Run initial prompt.
6. Run `steps` sequentially, honoring:
   - `when` (`always`, `on_success`, `on_error`)
   - file triggers (`file_exists`, `file_not_exists`)
   - per-step runtime overrides
7. If `persist_runtime_overrides` is `false`, runtime changes are restored at workflow end.
8. If `orchestration.enabled` is `true`, workflow progress is checkpointed and can yield at provider handoffs for external schedulers.

## Config Schema

```json
{
  "no_web_ui": false,
  "web_port": 0,
  "daemon": false,

  "persist_runtime_overrides": false,
  "continue_on_error": true,
  "orchestration": {
    "enabled": true,
    "resume": true,
    "yield_on_provider_handoff": true,
    "state_file": ".ledit/workflow_state.json",
    "events_file": ".ledit/workflow_events.jsonl"
  },

  "initial": {
    "prompt": "...",
    "prompt_file": "prompts/initial.md",
    "provider": "openrouter",
    "model": "openai/gpt-5",
    "persona": "coder",
    "reasoning_effort": "low",
    "system_prompt": "...",
    "system_prompt_file": "prompts/system/coder.md",
    "max_iterations": 300,
    "skip_prompt": true,
    "dry_run": false,
    "no_stream": false,
    "unsafe": false,
    "no_subagents": false,
    "resource_directory": "captures"
  },

  "steps": [
    {
      "name": "audit",
      "when": "on_success",
      "file_exists": ["go.mod"],
      "file_not_exists": ["AUDIT_COMPLETE.md"],

      "prompt": "Audit and fix issues.",
      "prompt_file": "prompts/audit.md",

      "provider": "openrouter",
      "model": "openai/gpt-5",
      "persona": "code_reviewer",
      "reasoning_effort": "high",
      "system_prompt": "...",
      "system_prompt_file": "prompts/system/reviewer.md",
      "max_iterations": 500,
      "skip_prompt": true,
      "dry_run": false,
      "no_stream": false,
      "unsafe": false,
      "no_subagents": true,
      "resource_directory": "captures"
    }
  ]
}
```

## Top-Level Fields

- `no_web_ui` (`bool`): Workflow override for `--no-web-ui`.
- `web_port` (`int`): Workflow override for `--web-port`. Must be `>= 0`. `0` means auto-select.
- `daemon` (`bool`): Workflow override for `--daemon`.
- `persist_runtime_overrides` (`bool`, default `true`):
  - `true`: runtime overrides remain after workflow.
  - `false`: provider/model/persona/system prompt/reasoning/env-related runtime settings are restored after workflow.
- `continue_on_error` (`bool`, default `false`): continue to later steps after a step error.
- `initial` (object): optional initial prompt/runtime block.
- `steps` (array): ordered workflow steps.
- `orchestration` (object, optional): enable external orchestration hooks.
  - `enabled` (`bool`): turn orchestration mode on.
  - `resume` (`bool`, default `true`): resume from `state_file` if present.
  - `yield_on_provider_handoff` (`bool`, default `true`): stop before a step whose provider differs from the previous executed provider.
  - `state_file` (`string`, default `.ledit/workflow_state.json`): JSON checkpoint path.
  - `events_file` (`string`, default `.ledit/workflow_events.jsonl`): JSONL event stream path.

Validation rule: workflow must include at least one `steps` item, or an `initial.prompt`/`initial.prompt_file`.

## Step Fields

- `name` (`string`): display name for logs.
- `when` (`string`): `always` | `on_success` | `on_error`. Default: `always`.
- `file_exists` (`[]string`): all listed paths must exist for step to run.
- `file_not_exists` (`[]string`): all listed paths must not exist for step to run.
- `prompt` or `prompt_file` (exactly one required per step).
- Runtime overrides (same fields as `initial` runtime block):
  - `skip_prompt`, `provider`, `model`, `persona`, `dry_run`, `max_iterations`, `no_stream`,
    `system_prompt`, `system_prompt_file`, `unsafe`, `no_subagents`, `resource_directory`, `reasoning_effort`.

## Prompt and System Prompt Files

- `prompt_file` and `system_prompt_file` are read with `os.ReadFile`.
- Paths are interpreted relative to the current working directory (or absolute path).
- `prompt` and `prompt_file` are mutually exclusive.
- `system_prompt` and `system_prompt_file` are mutually exclusive.
- File contents are trimmed before use.

## Runtime Override Behavior

Runtime overrides are applied in this order for each initial/step block:

1. `skip_prompt`
2. `max_iterations`
3. `unsafe`
4. `no_stream`
5. `dry_run` env toggle
6. `no_subagents` env toggle
7. `resource_directory` env
8. `provider`
9. `model`
10. `system_prompt` / `system_prompt_file`
11. `persona`
12. `reasoning_effort`

## Triggering Semantics

A step runs only when all are true:

1. `when` matches the current success/error state.
2. Every path in `file_exists` exists.
3. Every path in `file_not_exists` does not exist.

If trigger conditions are not met, the step is skipped (not treated as an error).

## Persistence and Restoration

If `persist_runtime_overrides` is `false`, ledit restores runtime state after workflow completes:

- provider/model
- persona
- system prompt/base system prompt
- `skip_prompt`
- reasoning effort
- `unsafe`
- `max_iterations`
- `no_stream`
- env-backed toggles (`LEDIT_DRY_RUN`, `LEDIT_NO_SUBAGENTS`, `LEDIT_RESOURCE_DIRECTORY`)

Note: hard termination (for example `SIGKILL`) can prevent restoration.

## Example Patterns

### 1. Initial task from file + deep audit

```json
{
  "persist_runtime_overrides": false,
  "initial": {
    "prompt_file": "prompts/initial_task.md",
    "provider": "my-custom-llm",
    "model": "custom-model-v1",
    "reasoning_effort": "low"
  },
  "steps": [
    {
      "name": "deep_audit",
      "when": "on_success",
      "reasoning_effort": "high",
      "prompt": "Audit all changed files and fix issues."
    }
  ]
}
```

### 2. File-gated docs generation

```json
{
  "steps": [
    {
      "name": "docs_if_missing",
      "when": "on_success",
      "file_not_exists": ["docs/IMPLEMENTATION_NOTES.md"],
      "prompt": "Create docs/IMPLEMENTATION_NOTES.md"
    }
  ]
}
```

### 3. Error branch recovery

```json
{
  "continue_on_error": true,
  "steps": [
    {
      "name": "repair_plan",
      "when": "on_error",
      "reasoning_effort": "high",
      "prompt": "Diagnose failure and propose a repair plan."
    }
  ]
}
```

## Current Example File

See: `examples/agent_workflow.json`

## External Orchestration

When orchestration mode is enabled, `ledit` emits:

- State checkpoint (`state_file`) with:
  - `initial_completed`
  - `next_step_index`
  - `has_error`
  - `first_error`
  - `last_provider`
  - `complete`
- Event stream (`events_file`) JSONL records such as:
  - `workflow_run_started`
  - `workflow_initial_completed`
  - `workflow_step_started`
  - `workflow_step_skipped`
  - `workflow_step_completed`
  - `workflow_step_failed`
  - `workflow_yielded`
  - `workflow_completed`

This allows an external orchestrator to:

1. Read `next_step_index` and determine required provider.
2. Acquire provider-specific concurrency slot.
3. Invoke `ledit agent --workflow-config ...`.
4. Repeat until `complete=true`.

See: `examples/workflow_orchestrator.py` for a full asyncio-based orchestrator with per-provider concurrency limits.
Use it with `examples/workflow_orchestrator_manifest.json`:

```bash
python3 examples/workflow_orchestrator.py --manifest examples/workflow_orchestrator_manifest.json
```

## Troubleshooting

- `failed to read prompt_file ...`: path is wrong or not readable.
- `... mutually exclusive`: remove one of text/file variants.
- `... max_iterations must be > 0`: set positive integer.
- `invalid provider ...`: provider name must match configured/built-in provider IDs.
- Step skipped unexpectedly: verify `when`, `file_exists`, and `file_not_exists` conditions.
