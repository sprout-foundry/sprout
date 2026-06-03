# Agent Workflow

Agent workflow lets `sprout agent` run a config-driven sequence of prompts with conditional routing and per-step runtime overrides.

Use it for CI/CD orchestration, post-implementation audits, error recovery paths, and multi-model review pipelines.

## Command

```bash
sprout agent --workflow-config path/to/workflow.json
```

You can still pass a CLI prompt:

```bash
sprout agent --workflow-config path/to/workflow.json "Implement feature X"
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
    "state_file": ".sprout/workflow_state.json",
    "events_file": ".sprout/workflow_events.jsonl"
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
  - `state_file` (`string`, default `.sprout/workflow_state.json`): JSON checkpoint path.
  - `events_file` (`string`, default `.sprout/workflow_events.jsonl`): JSONL event stream path.

Validation rule: workflow must include at least one `steps` item, or an `initial.prompt`/`initial.prompt_file`.

## Step Fields

- `name` (`string`): display name for logs.
- `when` (`string`): `always` | `on_success` | `on_error`. Default: `always`.
- `file_exists` (`[]string`): all listed paths must exist for step to run.
- `file_not_exists` (`[]string`): all listed paths must not exist for step to run.
- `prompt` or `prompt_file` (exactly one required per step).
- Runtime overrides (same fields as `initial` runtime block):
  - `skip_prompt`, `provider`, `model`, `persona`, `dry_run`, `max_iterations`, `no_stream`,
    `system_prompt`, `system_prompt_file`, `unsafe`, `no_subagents`, `resource_directory`, `reasoning_effort`,
    `subagent_overrides`, `risk_profile`.
- `risk_profile` (`string`, optional): Shell-command gating profile for this step. Accepts a built-in
  (`readonly`, `cautious`, `default`, `permissive`, `unrestricted`) or any name defined in
  `config.risk_profiles`. Unknown values fall back to the built-in `default` with a stderr warning.
  This is the same field as the `--risk-profile` CLI flag and overrides `config.risk_profile` for the
  step's lifetime. Subagents spawned during the step inherit the override (see [SECURITY.md](SECURITY.md#risk-profiles)).

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
13. `subagent_overrides`
14. `risk_profile`

## Subagent Overrides

`subagent_overrides` allows per-step control over which provider/model subagents use for each persona. This is the primary mechanism for cost control within a workflow run.

The field is a map of persona IDs to provider/model overrides, available on both `initial` and each `step`:

```json
{
  "initial": {
    "provider": "anthropic",
    "model": "claude-sonnet-4",
    "subagent_overrides": {
      "tester": { "provider": "anthropic", "model": "claude-haiku-4-20250514" },
      "code_reviewer": { "provider": "openrouter", "model": "google/gemini-2.5-pro" }
    },
    "prompt": "Implement feature X"
  }
}
```

- Persona IDs are normalized (lowercase, hyphens → underscores). Aliases are matched.
- Unknown or disabled personas are silently skipped.
- Each entry requires at least one of `provider` or `model`.
- Overrides patch the per-persona `SubagentTypes` entries in config. They do not affect the global `subagent_provider`/`subagent_model` defaults.
- When `persist_runtime_overrides` is `false`, original persona provider/model values are restored after the workflow completes.
- Later steps can override earlier steps — each step's overrides are applied sequentially.

### Resolution order for subagent provider/model

When a subagent is spawned during a workflow step:

1. **Workflow `subagent_overrides`** for the matching persona (highest priority, per-step)
2. **Persona config** `SubagentTypes[persona].Provider/Model` from persisted config
3. **Global subagent config** `subagent_provider`/`subagent_model`
4. **Parent agent** provider/model inheritance

## Subagent Resource Model

The `SubagentRunner` enforces concurrency limits and token budgets to control resource usage across parallel subagent executions. These settings are configured via `SubagentOptions` on the spawning agent.

### Concurrency limit

- `MaxConcurrentSubagents` (`int` on `SubagentOptions`): when `> 0`, limits how many subagents can execute simultaneously.
- Uses a buffered channel (`chan struct{}`) as a semaphore to gate execution slots.
- Tasks that exceed the limit are queued and wait for an available slot.
- If the parent context is cancelled while tasks are queued, those tasks are dropped immediately with `Cancelled=true`.

### Fleet token budget

- `FleetTokenBudget` (`int` on `SubagentOptions`): when `> 0`, sets a cumulative token budget shared across all subagents in the fleet.
- Tracks total token usage via `atomic.Int64`, debiting after each LLM call.
- When the budget is reached, not-yet-started tasks are skipped with `BudgetExceeded=true`.
- Currently running tasks can be **truncated gracefully** at LLM-call boundaries when the budget is exceeded mid-run (tracked via the `Truncated` field on `SubagentResult`).
- `BudgetExceeded=true` means the task was skipped before it started (budget was already exhausted). `Truncated=true` means the task was running but cut short at an LLM-call boundary.
- Budget check happens **after** acquiring the semaphore slot. A budget-skipped task may have consumed a semaphore slot while waiting in queue; it releases the slot immediately upon being skipped.
- `MaxTokens` (`int` on `SubagentOptions`): per-subagent token budget. When `> 0`, a polling goroutine (500ms ticker via `monitorBudget`) watches token usage and calls `interruptCancel()` to stop the subagent when its individual token count exceeds the limit.
- The Executive Assistant persona sets a default of 200000 tokens via `fleet_token_budget` in `pkg/personas/configs/executive_assistant.json`. When unset (zero), the fleet budget is unlimited.

### Telemetry

Atomic counters track the subagent fleet lifecycle:

- `Active`: currently executing subagents
- `Queued`: waiting for a semaphore slot
- `Completed`: successfully finished
- `Failed`: finished with an error
- `Cancelled`: cancelled via parent context or budget exhaustion
- `TotalQueuedWaitMS`: cumulative milliseconds spent waiting in queue

The `Metrics()` accessor returns a snapshot of these counters at any time.

Lifecycle events are published via `EventBus` (`EventTypeSubagentActivity`) with the following statuses:

- `queued` — task entered the queue
- `started` — task acquired a semaphore slot and began execution
- `completed` — task finished successfully
- `cancelled` — task was cancelled (reason: `context_cancelled` or `budget_exceeded`)

Events are also written to the runlog as JSONL entries for persistent structured logging.

### WebUI Subagents tab

- Located at `webui/src/components/contextPanel/SubagentsTab.tsx`.
- Displays live resource-usage counts as metric chips: Active, Queued, Completed, Failed, Cancelled.
- Updated in real-time from `subagent_activity` events received via WebSocket.

See also: `pkg/agent/subagent_runner.go` for the full implementation.

## Triggering Semantics

A step runs only when all are true:

1. `when` matches the current success/error state.
2. Every path in `file_exists` exists.
3. Every path in `file_not_exists` does not exist.

If trigger conditions are not met, the step is skipped (not treated as an error).

## Persistence and Restoration

If `persist_runtime_overrides` is `false`, sprout restores runtime state after workflow completes:

- provider/model
- persona
- system prompt/base system prompt
- `skip_prompt`
- reasoning effort
- `unsafe`
- `max_iterations`
- `no_stream`
- env-backed toggles (`SPROUT_DRY_RUN`, `SPROUT_NO_SUBAGENTS`, `SPROUT_RESOURCE_DIRECTORY`)

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

### 4. Cost-controlled subagent routing

```json
{
  "persist_runtime_overrides": false,
  "initial": {
    "prompt": "Implement the feature",
    "provider": "anthropic",
    "model": "claude-sonnet-4",
    "subagent_overrides": {
      "tester": { "provider": "deepinfra", "model": "deepseek-v3" }
    }
  },
  "steps": [
    {
      "name": "tests",
      "when": "on_success",
      "subagent_overrides": {
        "tester": { "provider": "deepinfra", "model": "deepseek-v3" }
      },
      "prompt": "Write comprehensive tests."
    },
    {
      "name": "review",
      "when": "on_success",
      "subagent_overrides": {
        "code_reviewer": { "provider": "openrouter", "model": "google/gemini-2.5-pro" }
      },
      "prompt": "Review all changes."
    }
  ]
}
```

### 5. Read-only audit then guarded implementation

Two-phase pattern that uses `risk_profile` to lock the audit step out of any
write operation, then loosens the gate for the implementation step. Subagents
spawned during each step inherit the step's profile.

```json
{
  "steps": [
    {
      "name": "audit",
      "risk_profile": "readonly",
      "prompt": "Investigate the auth flow. Read and report only — no edits."
    },
    {
      "name": "implement",
      "when": "on_success",
      "risk_profile": "default",
      "prompt": "Apply the fixes identified in the audit step."
    }
  ]
}
```

## Subagent Resource Model

When the agent dispatches work via `RunParallel`, two shared limits govern how many subagents run and how much they cost.

### Concurrency Limit (`MaxConcurrentSubagents`)

`SubagentOptions.MaxConcurrentSubagents` caps how many subagents execute simultaneously.

- **Default**: `0` (unlimited — all tasks launch immediately)
- **When > 0**: a buffered channel acts as a counting semaphore. Each goroutine blocks on the channel until a slot is free, respecting context cancellation via `select`. After acquiring a slot, the task runs; when it finishes, the slot is released back to the channel.
- Tasks that cannot acquire a slot remain in the **queued** state and transition to **active** once they get one.

### Fleet Token Budget (`FleetTokenBudget`)

`SubagentOptions.FleetTokenBudget` defines a shared token ceiling for the entire parallel batch.

- Tokens are debited to a shared `cumulativeTokens` counter **after each LLM API call** inside a subagent (not just after the subagent finishes). This is wired via `SetFleetBudget(cumulativeTokens, fleetBudgetLimit)` on each spawned subagent (SP-037-2c).
- A budget check occurs **after acquiring the semaphore but before starting work**. If the budget is already exhausted, the task is cancelled immediately with `BudgetExceeded=true` — it never launches.
- If a subagent is **mid-run** when the budget is exceeded during one of its LLM calls, the subagent truncates gracefully with `Truncated=true`.
- The **Executive Assistant** persona sets a default `fleet_token_budget` of **200,000** tokens (`pkg/personas/configs/executive_assistant.json`).

Result fields:
- `SubagentResult.BudgetExceeded` — task was skipped because the budget was already exceeded before it started
- `SubagentResult.Truncated` — subagent was cut short mid-run due to budget exhaustion

### Telemetry

Every lifecycle transition emits a structured event via both the `EventBus` (for real-time WebUI) and the persistent runlog (JSONL).

| Lifecycle State | EventBus Event (`status`) | Runlog Event |
|-----------------|--------------------------|--------------|
| Task queued     | `subagent_activity` / `status: "queued"` | `subagent.queued` |
| Task started    | `subagent_activity` / `status: "started"` | `subagent.started` |
| Task completed  | `subagent_activity` / `status: "completed"` | `subagent.completed` |
| Task cancelled  | `subagent_activity` / `status: "cancelled"` | `subagent.cancelled` |

Each event contains:

**Fields present on ALL events:**
- `task_id` — unique identifier for the subagent task
- `persona` — the persona used (e.g. `"coder"`, `"tester"`)
- `status` — one of `"queued"`, `"started"`, `"completed"`, `"cancelled"`

**Fields present only on `completed`/`cancelled` events:**
- `tokens_used` — token count for this subagent
- `elapsed_ms` — wall-clock duration in milliseconds

**Fields present only on `cancelled` events:**
- `reason` — cancellation reason: `"context_cancelled"` or `"budget_exceeded"`

### WebUI Subagents Tab

The **Subagents** tab in the context panel displays a resource-usage row showing live counts: **active**, **queued**, **completed**, and **cancelled**.

The data flows through these layers:
1. **Backend** — `publishLifecycleEvent()` emits `EventTypeSubagentActivity` to the `EventBus`, which broadcasts over the WebSocket connection
2. **Hook** — `useSubagentRuns` (`webui/src/components/contextPanel/useSubagentRuns.ts`) subscribes to `subagent_activity` events via WebSocket, maintaining a local state of current and historical runs
3. **Component** — `SubagentsTab` (`webui/src/components/contextPanel/SubagentsTab.tsx`) reads from the hook and renders each run as a status row with persona, prompt excerpt, token usage, and elapsed time

## Current Example File

See: `examples/agent_workflow.json`

## External Orchestration

When orchestration mode is enabled, `sprout` emits:

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
3. Invoke `sprout agent --workflow-config ...`.
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
