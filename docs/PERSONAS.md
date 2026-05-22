# Sprout Persona System

Personas are instruction bundles that specialize the AI agent's behavior, tool access, and risk policies for different engineering roles. This document covers the architecture, configuration, and usage of the persona system.

---

## 1. Overview

A **persona** is a named specialization that configures how the AI agent behaves during a session. Personas control:

- **System prompt** — The behavioral instructions loaded into context (e.g., "You are Coder, a specialized software engineering agent…")
- **Tool allowlist** — Which tools the persona can use (e.g., `coder` gets `read_file`, `write_file`, `edit_file`, `shell_command`; `web_scraper` gets only `fetch_url` and `browse_url`)
- **Provider/model overrides** — Optional provider or model selection specific to a persona
- **Risk cascade rules** — For the Executive Assistant persona, auto-approve/reason/reject rules that determine which operations require user confirmation
- **Local-only flag** — Whether the persona is restricted to local (CLI/WebUI) execution or available in cloud deployments
- **Delegation capability** — Whether the persona can be spawned as a subagent by other personas

### Built-in Personas

| Persona | ID | Description | Delegatable |
|---------|-----|-------------|-------------|
| General | `general` | General-purpose implementation assistant | Yes |
| Coder | `coder` | Feature implementation and development | Yes |
| Tester | `tester` | Unit test writing and coverage | Yes |
| Debugger | `debugger` | Bug investigation and root cause analysis | Yes |
| Refactor | `refactor` | Behavior-preserving code refactoring | Yes |
| Code Reviewer | `code_reviewer` | Code quality and security review | Yes |
| Researcher | `researcher` | Combined local codebase analysis and external research | Yes |
| Web Scraper | `web_scraper` | Web content extraction | Yes |
| Computer User | `computer_user` | Hands-on system administration and engineering execution | Yes |
| Executive Assistant | `executive_assistant` | Cross-project orchestration and task queue | No |
| Project Planner | `project_planner` | Strategic Planning & Alignment Architect | No |
| Orchestrator | `orchestrator` | Planning and delegation; git-write (commit/stage/push) is gated by `AllowOrchestratorGitWrite`. Aliases: `repo_orchestrator`, `repo_operator`, `git_orchestrator`, `orchestration` | No (EA exception) |

**Source**: `pkg/personas/configs/default_personas.json`, `pkg/personas/configs/executive_assistant.json`, `pkg/personas/configs/project_planner.json`

---

## 2. Three-Layer Architecture

Personas are defined, merged, and activated through three layers. Each layer adds specificity over the previous one.

### Layer 1: Catalog (Embedded Defaults)

The catalog layer consists of **embedded JSON files** compiled into the Sprout binary. These are the authoritative definitions of every built-in persona.

**Location**: `pkg/personas/configs/*.json`

Each JSON file defines a `Catalog` containing an array of `Definition` objects:

```json
{
  "personas": [
    {
      "id": "coder",
      "name": "Coder",
      "description": "Implementation and feature development",
      "system_prompt": "pkg/agent/prompts/subagent_prompts/coder.md",
      "allowed_tools": [
        "read_file", "write_file", "edit_file",
        "write_structured_file", "patch_structured_file",
        "search_files", "shell_command"
      ],
      "enabled": true,
      "delegatable": true
    }
  ]
}
```

The `Definition` struct (`pkg/personas/catalog.go`) has these fields:

| Field | Type | Purpose |
|-------|------|---------|
| `id` | `string` | Unique identifier (normalized to lowercase with underscores) |
| `name` | `string` | Human-readable name |
| `description` | `string` | What this persona specializes in |
| `provider` | `string` | Optional provider override (e.g., `"ollama"`) |
| `model` | `string` | Optional model override (e.g., `"qwen3-coder"`) |
| `system_prompt` | `string` | Path to system prompt markdown file |
| `system_prompt_text` | `string` | Inline system prompt (replaces file-based prompt) |
| `system_prompt_append` | `string` | Text appended to the base prompt |
| `allowed_tools` | `[]string` | Tool allowlist (empty = all tools) |
| `enabled` | `bool` | Whether the persona is available |
| `aliases` | `[]string` | Alternative names (e.g., `"web-scraper"`) |
| `local_only` | `bool` | Available only in local mode |
| `delegatable` | `bool` | Can be spawned as a subagent |
| `auto_approve_rules` | `*AutoApproveRules` | Risk cascade rules (EA persona only) — **note**: this field exists on the `SubagentType` struct in `config.go`, not on the catalog `Definition` struct. It is declared inline in the JSON config (e.g., `executive_assistant.json`) and mapped to `SubagentType.AutoApproveRules` during the catalog→config conversion. |

**Loading**: The catalog is loaded at startup via `personas.DefaultDefinitions()` which uses `sync.Once` for thread-safe initialization. If loading fails, a minimal fallback with `orchestrator` and `general` is used.

**Source**: `pkg/personas/catalog.go`

### Layer 2: Config (User/Project Overrides)

When Sprout starts, catalog definitions are converted to `SubagentType` objects and stored in `config.SubagentTypes` (a `map[string]SubagentType`).

**Location**: `pkg/configuration/config.go` — `SubagentType` struct

The `SubagentType` struct mirrors `Definition` but uses Go naming conventions:

```go
type SubagentType struct {
    ID               string            `json:"id"`
    Name             string            `json:"name"`
    Description      string            `json:"description"`
    Provider         string            `json:"provider"`
    Model            string            `json:"model"`
    SystemPrompt     string            `json:"system_prompt"`
    SystemPromptText string            `json:"system_prompt_text,omitempty"`
    SystemPromptAppend string          `json:"system_prompt_append,omitempty"`
    AllowedTools     []string          `json:"allowed_tools,omitempty"`
    Aliases          []string          `json:"aliases,omitempty"`
    Enabled          bool              `json:"enabled"`
    LocalOnly        bool              `json:"local_only,omitempty"`
    Delegatable      bool              `json:"delegatable,omitempty"`
    AutoApproveRules *AutoApproveRules `json:"auto_approve_rules,omitempty"`
}
```

**User customization**: Users can override personas in their `.sprout/config.json` under the `subagent_types` key. The merge logic (see Section 3) handles how user overrides interact with catalog defaults.

**Auto-migration**: The config loader (`mergePersonaToolLists` in `config.go`) automatically adds `write_structured_file` and `patch_structured_file` to any persona that has `write_file` or `edit_file`, ensuring structured file tools are always available alongside general file tools.

**Source**: `pkg/configuration/config.go` — `defaultSubagentTypes()`, `mergePersonaToolLists()`

### Layer 3: Session (Runtime Activation)

At runtime, a persona can be **activated** for the current session via `ApplyPersona(personaID)`. This is the session layer — it applies persona settings to the agent's current state.

**What happens when a persona is applied** (`pkg/agent/persona.go` — `ApplyPersona()`):

1. **Provider switch** (if persona specifies one): The agent switches its LLM provider and resets model to the provider's default.
2. **Model switch** (if persona specifies one): The agent sets the specified model on the effective provider.
3. **System prompt**: The persona's system prompt is loaded — either from file (`system_prompt`), inline text (`system_prompt_text`), or the base prompt is kept and `system_prompt_append` is added.
4. **Active persona tracking**: The persona ID is recorded in the agent's state. For depth-0 agents, it becomes the `rootPersonaID`.
5. **Tool allowlist**: The persona's `allowed_tools` list is used to filter available tools for the session.

**System prompt composition rules** (order of precedence):
- If `system_prompt_text` is set → **replaces** the current prompt entirely
- Else if `system_prompt` is set → **loads** the markdown file and replaces the current prompt
- If `system_prompt_append` is set → **appends** to the result (separated by `---`)

**Real-world example of conditional append**: The `orchestrator` persona conditionally appends a Git Operations Policy section to its system prompt when `AllowOrchestratorGitWrite=true`. The policy lives as a `go:embed`'d markdown file (`pkg/agent/prompts/persona_appends/orchestrator_git_policy.md`) and is concatenated by `ApplyPersona` only when the flag is on — so the same persona ID covers both the read-only and git-write variants without two catalog entries.

**Clearing a persona**: `ClearActivePersona()` removes the active persona and restores the `baseSystemPrompt`.

**Source**: `pkg/agent/persona.go` — `ApplyPersona()`, `ClearActivePersona()`, `GetActivePersona()`, `getActivePersonaToolAllowlist()`

---

## 3. Merge Resolution Rules

When catalog defaults, user config, and runtime activation coexist, specific rules determine which values take effect.

### Config Merge (Catalog → User Config)

When the config system initializes (`config.go` — `NewConfig()`), it:

1. Loads catalog defaults via `defaultSubagentTypes()`
2. Merges any user-defined `subagent_types` from `.sprout/config.json`
3. For any **user-defined persona that shares an ID with a catalog default**, the **user config completely replaces** the catalog definition for that ID

**Example**: If the user defines a `coder` persona in their config with a custom `provider`, all other fields (`allowed_tools`, `system_prompt`, etc.) must also be specified — they are not merged field-by-field from the catalog. The user definition is a full replacement.

### Tool List Augmentation

After merging, the config loader augments tool lists automatically:

- Any persona with `write_file` in its `allowed_tools` gets `write_structured_file` and `patch_structured_file` added if not already present.
- Any persona with `edit_file` in its `allowed_tools` gets `write_structured_file` and `patch_structured_file` added if not already present.

This ensures structured file tools are always available alongside general file editing tools.

**Source**: `pkg/configuration/config.go` — `mergePersonaToolLists()`, lines ~1450-1550

### System Prompt Cascade

System prompts follow this precedence at session activation time:

| Priority | Field | Effect |
|----------|-------|--------|
| 1 (highest) | `system_prompt_text` | Replaces current prompt entirely |
| 2 | `system_prompt` | Loads from file path, replaces current prompt |
| 3 (lowest) | `system_prompt_append` | Appends to whatever prompt is set |
| — | (empty) | Keeps existing prompt unchanged |

### Provider/Model Resolution

Provider and model resolution follows a cascade:

1. **Persona's own `provider`/`model`** — If the persona specifies these, they override everything.
2. **Subagent provider/model** — Config-level `subagent_provider` and `subagent_model` settings.
3. **Parent agent's runtime provider/model** — Inherited from the spawning agent.
4. **System defaults** — Whatever the config's last-used provider/model is.

For subagent spawning specifically (`tool_handlers_subagent.go`):

```go
// Resolve provider/model: use opts overrides, then parent agent, then config defaults
provider := opts.Provider
model := opts.Model

if provider == "" && r.parentAgent != nil {
    parentProvider := r.parentAgent.GetProvider()
    if parentProvider != "" && parentProvider != "unknown" {
        provider = parentProvider
    }
}
```

**Source**: `pkg/agent/persona.go` (provider/model resolution), `pkg/agent/subagent_runner.go` (createSubagent)

---

## 4. The Two-Gate Risk Model

Sprout uses a **two-gate security model** for shell commands and git operations. Both gates evaluate independently — the more restrictive result always wins.

### Gate 1: Global Static Classifier

**Location**: `pkg/agent_tools/security_classifier.go`

A static, string-based heuristic that inspects the tool name and arguments for dangerous patterns. This gate runs for **all personas**, regardless of configuration. It can:
- **Block** dangerous operations outright (e.g., `rm -rf /`)
- **Prompt** the user for confirmation on risky operations

This gate is persona-agnostic — it operates at the tool-calling layer before any persona-specific logic.

### Gate 2: Persona Risk Cascade

**Location**: `pkg/agent/agent_getters.go` — `EvaluateOperationRisk()`, `pkg/configuration/config.go` — `SubagentType.EvaluateOperationRisk()`

The persona risk cascade is **only active for personas with `auto_approve_rules` defined** (primarily the Executive Assistant). For all other personas, this gate returns `RiskLevelLow` (no interception).

**Risk levels**:

| Level | Meaning | Behavior |
|-------|---------|----------|
| `low` | Auto-approved | Operation executes without user confirmation |
| `medium` | Reason and decide | Persona evaluates the operation itself and decides |
| `high` | Always reject | Operation is blocked, even by autonomous personas |

**The cascade logic** (`config.go` — `EvaluateOperationRisk()`):

```go
func (st *SubagentType) EvaluateOperationRisk(command string) RiskLevel {
    rules := st.GetAutoApproveRules()
    cmdLower := strings.ToLower(command)

    // 1. Force flags always escalate to HIGH
    if containsForceFlag(cmdLower) {
        return RiskLevelHigh
    }

    // 2. Check high-risk patterns
    for _, pattern := range rules.HighRiskNever {
        if matchesRiskPattern(cmdLower, pattern) {
            return RiskLevelHigh
        }
    }

    // 3. Categorize the command, then check low/medium lists
    opCategory := categorizeCommand(cmdLower)
    for _, pattern := range rules.LowRiskOps {
        if opCategory == pattern {
            return RiskLevelLow
        }
    }
    for _, pattern := range rules.MediumRiskOps {
        if opCategory == pattern {
            return RiskLevelMedium
        }
    }

    // 4. Default to medium for unrecognized operations
    return RiskLevelMedium
}
```

**Command categorization**: Commands are mapped to categories like `git_add`, `git_commit`, `git_push`, `rm_command`, `docker`, `read_file`, `shell_command`:

| Example Command | Category | Default Risk |
|----------------|----------|-------------|
| `git status` | `git_status` | low |
| `git add foo.go` | `git_add` | low |
| `git commit` | `git_commit` | medium |
| `git push` | `git_push` | medium |
| `rm -f file` | `rm_command` | high (force flag) |
| `git push --force` | `git_push` | high (force flag) |
| `git reset --hard` | `git_reset_hard` | high |
| `git clean -f` | `git_clean` | high |
| `cat file.go` | `read_file` | low |
| `docker build .` | `docker` | medium |
| *(subagent spawn)* | `subagent_spawn` | medium |
| *(cross-directory access)* | `cross_directory` | medium |

**Force flag detection**: The `containsForceFlag()` function is carefully tuned to avoid false positives:
- `--force` as an exact token → always high risk
- `-f` standalone → only high risk for `git`, `rm`, `mv`, `cp`, `docker` (not for `grep -f` or `tail -f`)
- Combined flags like `-rf` → high risk for `rm`, `mv`, `cp`, `docker`
- `--force-with-lease` → explicitly excluded (it's a safer alternative)

**High-risk patterns** (default for EA):
```json
"high_risk_never": [
    "force_flag", "rm_recursive", "git_reset_hard",
    "git_clean", "docker_prune", "git_push_force",
    "git_checkout", "git_switch", "git_restore", "git_branch_delete"
]
```

### Risk Evaluation in Tool Handlers

When a shell command is executed (`tool_handlers_shell.go`):

```go
// Risk cascade for personas with auto-approve rules
if risk := a.EvaluateOperationRisk(command); risk == configuration.RiskLevelHigh {
    return "", agenterrors.NewSecurityError(
        fmt.Sprintf("high-risk operation rejected by persona risk cascade: %s", risk), nil,
    )
}
```

**Medium-risk operations** are not blocked — the persona's system prompt guides its own reasoning about whether to proceed. **Low-risk operations** execute immediately.

**Source**: `pkg/agent/tool_handlers_shell.go` (lines 70-90), `pkg/agent/agent_getters.go` (lines 499-515), `pkg/configuration/config.go` (lines 256-290)

---

## 5. The Depth Model (0/1/2)

The depth model controls how deeply personas can delegate work to subagents. This prevents infinite delegation chains and ensures cost-effective execution.

### Depth Levels

| Depth | Role | Examples | Tool Constraints |
|-------|------|----------|-----------------|
| **0** | Primary agent | Executive Assistant, Project Planner (when activated as root) | Full tool access, can spawn subagents |
| **1** | Orchestrator subagent | Orchestrator | Can spawn subagents, inherits parent provider/model |
| **2** | Specialist subagent | Coder, Tester, Debugger, Code Reviewer | **Cannot** spawn subagents, focused tool set |

**Note on non-delegatable personas**: The `orchestrator` is marked `delegatable: false` in its catalog definition, so it cannot be spawned as a subagent by regular personas. However, the `executive_assistant` bypasses this restriction via `hasEASpawnAuthority()` — when the EA spawns a subagent, it can delegate to any persona regardless of the `delegatable` flag. This is what enables the canonical three-level nesting chain (EA → orchestrator → specialist).

### How Depth Works

**Tracking** (`pkg/agent/agent.go`):
- `subagentDepth int` — Tracks the nesting depth (0 = primary, 1 = first-level subagent, 2 = second-level)
- `rootPersonaID string` — The persona of the depth-0 agent, propagated to all subagents

**Propagation** (`pkg/agent/subagent_runner.go` — `createSubagent()`):
```go
// Set subagentDepth based on parent's depth + 1
agent.subagentDepth = r.parentAgent.subagentDepth + 1

// Propagate rootPersonaID from parent
if r.parentAgent.rootPersonaID != "" {
    agent.rootPersonaID = r.parentAgent.rootPersonaID
}
```

### Configurable Max Depth

The maximum nesting depth is configurable in `.sprout/config.json`:

```json
{
  "subagent_max_depth": 2
}
```

- **Default**: 2 (allows EA → orchestrator → specialist)
- **Can be set** to 1 (EA → specialist only) for cost-sensitive environments
- **Can be set** to higher values for complex multi-stage workflows

### Spawn Restriction at Max Depth

When a subagent at the maximum depth tries to spawn another subagent, the tool handler returns an error:

```
error: cannot spawn subagent at depth 2 (max depth reached)
```

The `hasEASpawnAuthority()` function provides a special rule: the Executive Assistant persona (depth 0) has elevated spawn authority, enabling the full 3-level chain even when max depth is 2.

**Source**: `pkg/agent/agent.go` (subagentDepth, rootPersonaID), `pkg/agent/subagent_runner.go` (createSubagent), `pkg/agent/persona.go` (hasEASpawnAuthority)

### Three-Level Nesting Chain

The canonical delegation chain:

```
Depth 0: Executive Assistant (EA)
    └─ Depth 1: Orchestrator (spawns by EA, has git write access)
        └─ Depth 2: Coder / Tester / Debugger (specialist, focused tools)
```

The EA (`hasEASpawnAuthority`) can delegate to orchestrator subagents, which in turn can delegate to specialist subagents. Specialist subagents at depth 2 cannot spawn further subagents.

---

## 6. LocalOnly + IsLocalMode Semantics

### What `local_only` Means

The `local_only` flag on a persona definition indicates that the persona is **only available when running locally** (CLI or local WebUI). It is **not available in cloud deployments**.

**Current built-in personas**: No built-in persona currently sets `local_only: true`. This flag exists as a mechanism for custom personas or future use — for example, a persona that requires tight integration with local system services, hardware, or user-specific configuration directories could use this flag to prevent it from being available in cloud deployments.

**Custom personas**: When defining your own personas, set `local_only: true` if the persona depends on local-only resources:

```json
{
  "id": "my_local_only_persona",
  "name": "My Local Persona",
  "description": "A persona that requires local filesystem access",
  "local_only": true
}
```

### How `IsLocalMode()` Detection Works

**Source**: `pkg/agent/agent_getters.go` — `IsLocalMode()`

```go
func (a *Agent) IsLocalMode() bool {
    return configuration.GetEnvSimple("CLOUD") != "1"
}
```

Detection is based on the `SPROUT_CLOUD` environment variable:
- `SPROUT_CLOUD` **not set** or **empty** → Local mode (returns `true`)
- `SPROUT_CLOUD=0` → Local mode (returns `true`)
- `SPROUT_CLOUD=1` → Cloud mode (returns `false`)
- `SPROUT_CLOUD=anything_else` → Local mode (only `"1"` triggers cloud)

### Filtering in `GetAvailablePersonaIDs()`

When the agent lists available personas (`GetAvailablePersonaIDs()`), it filters out local-only personas in cloud mode:

```go
func (a *Agent) GetAvailablePersonaIDs() []string {
    // ...
    isLocal := a.IsLocalMode()

    for id, persona := range config.SubagentTypes {
        if !persona.Enabled {
            continue
        }
        // Filter out LocalOnly personas in cloud mode
        if persona.LocalOnly && !isLocal {
            continue
        }
        personaIDs = append(personaIDs, id)
    }
    // ...
}
```

This also applies to subagent spawning — if the `run_subagent` tool targets a local-only persona and the agent is in cloud mode, the spawn is rejected.

### Subagent Spawning and LocalOnly

In `handleRunSubagent()` (`tool_handlers_subagent.go`):

```go
if subagentType.LocalOnly && !a.IsLocalMode() {
    return "", fmt.Errorf("persona %q is only available in local mode", persona)
}
```

This ensures that even if a parent agent somehow references a local-only persona, the spawn is blocked in cloud environments.

**Source**: `pkg/agent/agent_getters.go` (IsLocalMode, lines 485-493), `pkg/agent/persona.go` (GetAvailablePersonaIDs, lines 140-155), `pkg/agent/tool_handlers_subagent.go` (lines 630-635)

---

## 7. How to Define a Custom Persona

Creating a custom persona involves three steps: defining the catalog entry, writing the system prompt, and configuring it.

### Step 1: Write the System Prompt

Create a markdown file with the persona's behavioral instructions:

```
# MyCustomTool Subagent

You are **MyCustomTool**, a specialized agent focused on [description].

## Your Core Expertise
- Skill 1
- Skill 2

## Your Approach
1. Understand the task
2. [Step 2]
3. [Step 3]

## What You Focus On
[Details about what this persona does and doesn't do]

## Tool Usage
- `read_file` — Examine existing code
- `write_file` — Create new files

## Git Operations Policy
- **Do NOT commit or push** — The primary agent handles git operations
```

**Location**: Place the prompt file anywhere accessible — commonly in `.sprout/prompts/` for project-local personas or `pkg/agent/prompts/subagent_prompts/` for built-in personas.

### Step 2: Define the Catalog Entry (Built-in)

For a built-in persona, add a JSON entry to `pkg/personas/configs/`:

```json
{
  "personas": [
    {
      "id": "my_custom_persona",
      "name": "My Custom Persona",
      "description": "A custom persona for [purpose]",
      "system_prompt": "pkg/agent/prompts/subagent_prompts/my_custom_persona.md",
      "allowed_tools": [
        "read_file",
        "write_file",
        "edit_file",
        "search_files"
      ],
      "enabled": true,
      "delegatable": true,
      "local_only": false
    }
  ]
}
```

For **autonomous or EA-like personas** that need risk cascade behavior, include `auto_approve_rules` in the JSON config. This field is only supported on `SubagentType` in `config.go` (not on the catalog `Definition` struct), so it should be declared in your `.sprout/config.json` under `subagent_types`:

```json
{
  "subagent_types": {
    "my_custom_persona": {
      "id": "my_custom_persona",
      // ... other fields ...
      "auto_approve_rules": {
        "low_risk": ["git_status", "read_file"],
        "medium_risk": ["git_commit", "write_file", "shell_command"],
        "high_risk_never": ["force_flag", "rm_recursive", "git_reset_hard"]
      }
    }
  }
}
```

### Step 3: Configure the Persona (User/Project Level)

For a user or project-level persona, add it to `.sprout/config.json`:

```json
{
  "subagent_types": {
    "my_custom_persona": {
      "id": "my_custom_persona",
      "name": "My Custom Persona",
      "description": "A custom persona for [purpose]",
      "system_prompt": ".sprout/prompts/my_custom_persona.md",
      "allowed_tools": [
        "read_file",
        "write_file",
        "edit_file",
        "search_files"
      ],
      "enabled": true
    }
  }
}
```

### Step 4: Configure Tool Allowlist

Choose tools carefully based on the persona's role:

| Persona Type | Recommended Tools |
|-------------|-------------------|
| **Reader-only** | `read_file`, `search_files` |
| **Implementation** | `read_file`, `write_file`, `edit_file`, `search_files`, `shell_command` |
| **Review** | `read_file`, `search_files` |
| **Full automation** | `read_file`, `write_file`, `edit_file`, `search_files`, `shell_command`, `git` |

**Important**: The `write_structured_file` and `patch_structured_file` tools are **automatically added** to any persona that has `write_file` or `edit_file` in its allowlist. You don't need to list them explicitly.

### Step 5: Configure Risk Levels (For Autonomous Personas)

If your custom persona should operate autonomously (like the EA), add `auto_approve_rules`:

```json
{
  "auto_approve_rules": {
    "low_risk": [
      "git_add", "git_status", "git_log", "git_diff", "read_file"
    ],
    "medium_risk": [
      "git_commit", "git_push", "write_file", "edit_file", "shell_command"
    ],
    "high_risk_never": [
      "force_flag", "rm_recursive", "git_reset_hard", "git_clean"
    ]
  }
}
```

**Operation categories** you can reference:
- `git_add`, `git_status`, `git_log`, `git_diff`, `git_commit`, `git_push`, `git_pull`, `git_fetch`
- `git_reset_hard`, `git_clean`, `git_checkout`, `git_switch`, `git_restore`, `git_branch_delete`
- `read_file`, `write_file`, `edit_file`, `shell_command`, `rm_command`, `docker`
- `subagent_spawn`, `cross_directory`
- `force_flag`, `rm_recursive`, `docker_prune`, `git_push_force`

### Step 6: Test the Persona

1. **Activate the persona**: Use `ApplyPersona("my_custom_persona")` or spawn via `run_subagent`
2. **Verify tool access**: Check that the expected tools are available and unexpected ones are filtered
3. **Test risk cascade**: If you defined `auto_approve_rules`, verify that low-risk operations auto-execute and high-risk operations are blocked
4. **Check provider/model**: If you set `provider` or `model`, verify the persona uses them

---

## 8. Provider/Model Cost Considerations

### How Personas Interact with Provider/Model Selection

Personas can specify their own provider and model, which takes precedence over the user's general configuration:

```json
{
  "id": "coder",
  "provider": "ollama",
  "model": "qwen3-coder"
}
```

**Resolution cascade** (from highest to lowest priority):
1. **Persona's `provider`/`model`** — If set, these override everything
2. **Config `subagent_provider`/`subagent_model`** — User-level subagent defaults
3. **Parent agent's runtime provider/model** — Inherited from the spawning agent
4. **System defaults** — Last-used provider and its default model

### Cost Implications

| Strategy | Cost Impact | When to Use |
|----------|-------------|-------------|
| **Expensive model for EA** | High (EA does reasoning and delegation) | EA benefits from strong reasoning; use your best model |
| **Mid-range model for orchestrator** | Moderate (orchestrator plans and delegates) | Good balance of capability and cost |
| **Budget model for specialists** | Low (coders/testers do focused tasks) | Specialists have narrow tasks; cheaper models work well |
| **Same model for all** | Predictable but potentially wasteful | Simplest to manage; use when budget is not a concern |

### Recommended Configurations

**Cost-optimized (multi-model):**
```json
{
  "subagent_types": {
    "executive_assistant": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-20250514"
    },
    "orchestrator": {
      "provider": "openai",
      "model": "gpt-4.1"
    },
    "coder": {
      "provider": "ollama",
      "model": "qwen3-coder-480b"
    },
    "tester": {
      "provider": "ollama",
      "model": "qwen3-coder-480b"
    },
    "code_reviewer": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-20250514"
    }
  }
}
```

**Simple (single model):**
```json
{
  "subagent_provider": "ollama",
  "subagent_model": "qwen3-coder-480b"
}
```
All subagents use the same provider/model, regardless of persona.

### Token Budget Controls

Each subagent has a **default token budget of 2,000,000 tokens** (`DefaultSubagentTokenBudget` in `tool_handlers_subagent.go`). This limits how much a single subagent can spend before being terminated.

For parallel subagent execution, a **fleet token budget** can be set via `SubagentOptions.FleetTokenBudget` to cap total spending across all concurrent subagents.

### Practical Guidance

- **EA (depth 0)**: Use your most capable model. The EA does high-level reasoning, task decomposition, and decision-making. Cost here pays off in better delegation quality.
- **Orchestrator (depth 1)**: Use a capable but cost-effective model. The orchestrator coordinates code-level work and delegates to specialists.
- **Specialists (depth 2)**: Use budget models. These agents have narrow, well-defined tasks (write tests, implement a function, debug an issue) where cheaper models perform adequately.
- **Code Reviewer**: May benefit from a more capable model since security review requires strong reasoning.

**Source**: `pkg/agent/persona.go` (provider/model resolution), `pkg/agent/subagent_runner.go` (createSubagent, token budget), `pkg/configuration/config.go` (GetSubagentTypeProvider, GetSubagentTypeModel)

---

## Reference

### Source File Index

| Component | Source File(s) |
|-----------|---------------|
| Catalog definitions | `pkg/personas/catalog.go`, `pkg/personas/configs/*.json` |
| Config structures | `pkg/configuration/config.go` (`SubagentType`, `AutoApproveRules`) |
| Persona activation | `pkg/agent/persona.go` (`ApplyPersona`, `ClearActivePersona`) |
| Subagent runner | `pkg/agent/subagent_runner.go` (`SubagentRunner`, `createSubagent`) |
| Tool handlers | `pkg/agent/tool_handlers_subagent.go` (`handleRunSubagent`, `handleRunParallelSubagents`) |
| Shell command risk | `pkg/agent/tool_handlers_shell.go` (`handleShellCommand`) |
| Risk evaluation | `pkg/agent/agent_getters.go` (`EvaluateOperationRisk`, `IsLocalMode`) |
| Agent struct | `pkg/agent/agent.go` (`subagentDepth`, `rootPersonaID`) |
| System prompts | `pkg/agent/prompts/subagent_prompts/*.md` |

### Key Structs

```go
// pkg/personas/catalog.go — Definition
type Definition struct {
    ID               string   `json:"id"`
    Name             string   `json:"name"`
    Description      string   `json:"description"`
    Provider         string   `json:"provider,omitempty"`
    Model            string   `json:"model,omitempty"`
    SystemPrompt     string   `json:"system_prompt,omitempty"`
    SystemPromptText string   `json:"system_prompt_text,omitempty"`
    SystemPromptAppend string `json:"system_prompt_append,omitempty"`
    AllowedTools     []string `json:"allowed_tools,omitempty"`
    Enabled          bool     `json:"enabled"`
    Aliases          []string `json:"aliases,omitempty"`
    LocalOnly        bool     `json:"local_only,omitempty"`
    Delegatable      bool     `json:"delegatable,omitempty"`
}

// pkg/configuration/config.go — SubagentType
type SubagentType struct {
    ID               string            `json:"id"`
    Name             string            `json:"name"`
    Description      string            `json:"description"`
    Provider         string            `json:"provider"`
    Model            string            `json:"model"`
    SystemPrompt     string            `json:"system_prompt"`
    SystemPromptText string            `json:"system_prompt_text,omitempty"`
    SystemPromptAppend string          `json:"system_prompt_append,omitempty"`
    AllowedTools     []string          `json:"allowed_tools,omitempty"`
    Aliases          []string          `json:"aliases,omitempty"`
    Enabled          bool              `json:"enabled"`
    LocalOnly        bool              `json:"local_only,omitempty"`
    Delegatable      bool              `json:"delegatable,omitempty"`
    AutoApproveRules *AutoApproveRules `json:"auto_approve_rules,omitempty"`
}

// pkg/configuration/config.go — AutoApproveRules
type AutoApproveRules struct {
    LowRiskOps     []string `json:"low_risk,omitempty"`
    MediumRiskOps  []string `json:"medium_risk,omitempty"`
    HighRiskNever  []string `json:"high_risk_never,omitempty"`
}
```

### ID Normalization

Persona IDs are normalized by:
1. Converting to lowercase
2. Replacing hyphens with underscores
3. Trimming whitespace

So `"Web-Scraper"`, `"web_scraper"`, and `"web-scraper"` all resolve to `"web_scraper"`.

**Source**: `pkg/personas/catalog.go` — `normalizeID()`, `pkg/agent/persona.go` — `normalizeAgentPersonaID()`, `pkg/configuration/config.go` — `normalizePersonaID()`
