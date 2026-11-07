# Sprout Persona System

A **persona** is a named specialization that configures how the agent behaves: system prompt, tool allowlist, optional provider/model, capability grants (e.g. git-write), and which other personas it may spawn as subagents.

Personas come in two kinds. **Core personas** are immutable: the set, their tool allowlists, system prompts, providers, and models ship in `pkg/personas/configs/*.json` and cannot be changed by a file, a config key, or an API call. **Custom personas** are owned entirely by their creator — the person, or the embedding host — and decide everything: prompt, tools (built-in, host, MCP), skills, write paths, what auto-approves, provider and model. See §10.

The runtime knobs for core personas are: hide one, change the default spawn target, and toggle orchestrator git-write. To extend agent behavior beyond what the catalog ships without minting a persona, use **skills** (`~/.config/sprout/skills/<name>/SKILL.md`).

---

## 1. The Catalog

| ID | Description | Delegatable | Notes |
|----|-------------|-------------|-------|
| `orchestrator` | Planning and delegation; classify → activate skill → delegate → verify | No | Carries `git_write` capability. Aliases: `orchestration`, `repo_orchestrator`, `repo_operator`, `git_orchestrator` |
| `coordinator` | Cross-project coordination; delegates to orchestrator subagents | No | Carries `git_write` unconditionally. Lists `orchestrator` in `can_spawn_non_delegatable`. Aliases: `executive_assistant`, `ea`, `assistant` |
| `general` | General-purpose persona for tasks that don't need deep specialization | Yes | Alias: `default` |
| `coder` | Feature implementation, production code, debugging, and refactoring | Yes | Aliases: `refactor`, `debugger` (retired personas, consolidated 2026-09) |
| `tester` | Unit-test authoring and coverage | Yes | |
| `reviewer` | Diff-focused code review: correctness, security, quality | Yes | Alias: `code_reviewer` |
| `researcher` | Codebase analysis combined with external research and web extraction | Yes | Aliases: `web_scraper`, `web-scraper`, `scraper` (retired persona, consolidated 2026-09) |
| `designer` | Open-format UX design: DTCG tokens, SVG wireframes, mermaid flows, screens, brand | Yes | Carries `git_write` capability. Aliases: `ux`, `design` |
| `computer_user` | Desktop automation with screenshots, mouse, and keyboard | No | Carries `computer_use` capability |
| `planner` | Turns an idea into an agreed plan (`.sprout/plan.json`) and a clickable mockup (`design/mockup.html`) in plain builder words, before anything is built | No | **Confined** (`write_paths: ["design/"]`, see §6a). No capabilities. Run by a host (e.g. a builder screen) as the conversation's persona |

Source: `pkg/personas/configs/default_personas.json`, `pkg/personas/configs/coordinator.json`, `pkg/personas/configs/computer_user.json`, `pkg/personas/configs/designer.json`, `pkg/personas/configs/planner.json`.

> Strategic project planning lives in the `project-planning` **skill** (`pkg/skills/library/project-planning/SKILL.md`), not a persona. Activate it via `activate_skill project-planning` when starting or aligning a project.

---

## 2. Runtime knobs

These are the only persona-related settings users can change. All live in `~/.config/sprout/config.json` (or `.sprout/config.json` for project scope).

### `disabled_personas` — hide a persona

A list of canonical persona IDs. Hidden personas disappear from `/persona list`, can't be activated, and can't be spawned as subagents. The catalog itself is never mutated.

```json
{
  "disabled_personas": ["web_scraper", "researcher"]
}
```

CLI: `/persona <id> disable` and `/persona <id> enable` toggle the list (`pkg/agent_commands/persona.go`).

Implementation: `Config.IsPersonaDisabled` is checked inside `GetSubagentType` (returns nil for disabled personas) and inside `GetAvailablePersonaIDs` (filters them out). See `pkg/configuration/config.go:2139` and `pkg/agent/persona.go:185`.

### `default_subagent_persona` — change the default spawn target

When `run_subagent` is called without a `persona` argument, Sprout uses this persona. If unset, the hardcoded fallback is `general`.

```json
{
  "default_subagent_persona": "coder"
}
```

Implementation: `pkg/agent/tool_handlers_subagent.go:456-463`.

Git-write authorization is governed solely by the persona's `CapabilityGitWrite` capability — personas that declare it (orchestrator, coordinator) are allowed; all others are not. No config toggle is needed.

Implementation: `pkg/agent/persona.go` (`isGitWriteAllowed`), `pkg/agent/tool_handlers_shell.go`.

### `coordinator_auto_activate` — opt in to coordinator activation in $HOME

Coordinator no longer auto-activates. To restore the old $HOME behavior, set `coordinator_auto_activate: true`; the coordinator persona then activates on startup when the workspace resolves to the home directory. Otherwise activate it explicitly with `/persona coordinator`.

Implementation: `pkg/agent/agent_creation.go` (`maybeAutoActivateCoordinatorPersona`). The legacy `disable_coordinator_auto_activate` flag is still accepted as a no-op for config compatibility.

### `subagent_max_depth` — cap nesting depth

Max nesting for subagent chains. Default depends on the root persona: coordinator → 2 (allows coordinator → orchestrator → specialist); other roots → 1. An explicit value overrides both.

Implementation: `pkg/agent/agent_getters.go:503-516` (`MaxSubagentDepth`).

---

## 3. Architecture

Three layers: the embedded catalog, the custom-persona files, and the in-memory `SubagentTypes` they both hydrate. There is still no user-override merge — a custom persona can never replace a core one.

### Layer 1: Catalog (embedded JSON)

`pkg/personas/configs/*.json` is embedded into the binary via `//go:embed`. At startup `personas.DefaultDefinitions()` reads every `.json` in that directory, merges the persona arrays, and returns a `map[string]Definition` keyed by normalized ID.

**Conflict detection** runs at load time:
- Duplicate persona IDs across files → load error.
- An alias that shadows another file's ID → load error.
- An alias declared by two personas → load error.

See `loadDefinitionsFromFS` in `pkg/personas/catalog.go:87`. On load failure, a minimal fallback (orchestrator + general) keeps the binary functional.

### Layer 2: Custom personas (files, §10)

`personas.LoadCustomPersonas` reads `<config dir>/personas/*.json` and `<workspace>/.sprout/personas/*.json` and returns the definitions that may be used, plus every problem found. A custom persona that reuses a core id or alias is skipped and reported — core is immutable.

### Layer 3: Config (in-memory `SubagentTypes`)

`defaultSubagentTypes()` converts every `Definition` into a `SubagentType` and stores them in `Config.SubagentTypes`. `HydrateCustomPersonas` then merges the custom definitions in, after core, tagging each with its `Source`. This map is **always** repopulated at config load; `SubagentTypes` is tagged `json:"-"` so it never round-trips to disk.

The `SubagentType` struct mirrors `Definition`:

```go
type SubagentType struct {
    ID, Name, Description           string
    Provider, Model                 string  // optional overrides
    SystemPrompt, SystemPromptText  string
    SystemPromptAppend              string
    AllowedTools, Aliases           []string
    HostTools                       []string // tools the host registers at run time
    Skills                          []string // skills this persona may see and activate
    MCP                             []string // "<server>" | "<server>__<tool>" | "*"
    Enabled, LocalOnly, Delegatable bool
    AutoApproveRules                *AutoApproveRules
    Capabilities                    []string // e.g. "git_write"
    CanSpawnNonDelegatable          []string // persona IDs the spawner may target despite Delegatable=false
    Source                          string   // "core" | "host" | "user" | "project"
}
```

`SubagentType.HasCapability(name)` is the only legitimate way to check what a persona is allowed to do. The old practice of inferring capabilities by sniffing `AutoApproveRules` is gone — capabilities are explicit.

### Resolution

`Config.GetSubagentType(id)` (`pkg/configuration/config.go:2180`):

1. Normalize the input (lowercase, `-` → `_`).
2. Look up by ID, then by alias.
3. If the canonical ID is in `DisabledPersonas`, return nil.
4. Return a deep copy (callers can't mutate the catalog).

---

## 4. Capabilities

The only capability constants today are `personas.CapabilityGitWrite = "git_write"` and `personas.CapabilityComputerUse = "computer_use"` (`pkg/personas/ids.go`).

| Persona | Has `git_write`? | Effective? |
|---------|------------------|------------|
| `orchestrator` | Yes | Always (has capability) |
| `coordinator` | Yes | Always (has capability) |
| `designer` | Yes | Always (has capability) — deliberate: the design↔code loop commits design and code together (SP-140) |
| All others | No | Never; `commit` tool is rejected, write subcommands are blocked |

`Agent.isGitWriteAllowed()` (`pkg/agent/persona.go`):

```go
func (a *Agent) isGitWriteAllowed() bool {
    persona := cfg.GetSubagentType(a.GetActivePersona())
    if persona == nil {
        return false
    }
    return persona.HasCapability(personas.CapabilityGitWrite)
}
```

When `false`, write-class git subcommands routed through `shell_command` are rejected with a security error, and commits are redirected to the `commit` tool only when capability + flag both pass. See `pkg/agent/tool_handlers_shell.go:142-172`.

---

## 5. Spawning subagents

`handleRunSubagent` (`pkg/agent/tool_handlers_subagent.go`) gates a spawn with four checks, in order:

1. **Persona resolution** — `GetSubagentType(persona)` must return non-nil (the persona exists and isn't disabled).
2. **LocalOnly + cloud-mode** — `subagentType.LocalOnly && !a.IsLocalMode()` → reject. No built-in persona is currently `LocalOnly`; the field exists for future use.
3. **Delegatable + spawn override** — if `subagentType.Delegatable == false`, the spawner must list the target in its own `CanSpawnNonDelegatable`. Coordinator declares `["orchestrator"]`, which expresses the canonical coordinator → orchestrator → specialist chain. The same mechanism naturally prevents EA-spawns-EA (coordinator's list doesn't include itself) and orchestrator-spawns-coordinator (orchestrator has no list at all).
4. **Self-spawn prevention** — a persona can't spawn itself, regardless of any other rule.

```go
if subagentType.LocalOnly && !a.IsLocalMode() {
    return "", fmt.Errorf("persona '%s' is local-only and cannot be used as a subagent in cloud mode", persona)
}
if !subagentType.Delegatable && !a.canSpawnNonDelegatable(persona) {
    return "", fmt.Errorf("persona '%s' is not spawnable from %q (delegatable=false and not listed in spawner's can_spawn_non_delegatable)", persona, a.GetActivePersona())
}
if currentPersona := a.GetActivePersona(); currentPersona != "" && currentPersona == persona {
    return "", fmt.Errorf("persona '%s' cannot spawn itself (prevents self-recursion)", persona)
}
```

`canSpawnNonDelegatable` (`pkg/agent/persona.go:298`) just walks the active persona's catalog-declared list.

### Depth model

| Depth | Role | Constraint |
|-------|------|------------|
| 0 | Primary agent (whatever the user activates) | Full access |
| 1 | First-level subagent (e.g. orchestrator) | Can spawn one more level |
| 2 | Second-level subagent (e.g. coder) | Cannot spawn further |

`MaxSubagentDepth` defaults to 2 when the root is coordinator, 1 otherwise. Override with `subagent_max_depth`. See `pkg/agent/agent_getters.go:503`.

---

## 6. The two-gate risk model

The two gates evaluate independently; the more restrictive result wins.

### Gate 1: global classifier

`pkg/agent_tools/security_classifier.go` — string-based heuristic on tool name + args. Persona-agnostic. Can outright block (e.g. `rm -rf /`) or prompt the user.

### Gate 2: persona risk cascade

`SubagentType.EvaluateOperationRisk(command)` (`pkg/configuration/config.go:635`) returns one of `Low` / `Medium` / `High` / `Critical`:

1. **Critical patterns** (always-block, profile-independent) short-circuit first.
2. **`HighRiskNever` patterns** in the persona's `auto_approve_rules` → `High`.
3. Categorize the command (`git_add`, `git_commit`, `git_push`, `rm_command`, `docker`, …); look up in `LowRiskOps` then `MediumRiskOps`.
4. Otherwise fall back to the persona's `DefaultRisk`, or `Medium` if unspecified.

`AutoApproveRules` only controls runtime auto-approval — it does **not** grant capabilities. A persona without `AutoApproveRules` falls back to `DefaultAutoApproveRules()` and behaves as if `Medium` for unknown ops.

Coordinator is the only persona that ships explicit rules today (see `pkg/personas/configs/coordinator.json`). Its rules include high-risk patterns like `force_flag`, `rm_recursive`, `git_reset_hard`, `git_clean`, `git_push_force`, `git_checkout`, `git_switch`, `git_restore`, `git_branch_delete`.

When `EvaluateOperationRisk(command) == RiskLevelHigh` inside `tool_handlers_shell.go`, the operation is rejected:

```go
if risk := a.EvaluateOperationRisk(pseudoCmd); risk == configuration.RiskLevelHigh {
    return "", agenterrors.NewSecurityError(
        fmt.Sprintf("high-risk git operation rejected by persona risk cascade: %s (command: '%s')", risk, pseudoCmd), nil)
}
```

### 6a. Confined personas (`write_paths`)

A persona that declares `write_paths` (workspace-relative directories, e.g. `["design/"]`) is **confined**. For every persona, `allowed_tools` is the agent's whole tool set: the tool registry built for each turn holds only the tools it names, so the model is offered nothing else and a call to anything else is an unknown tool (`pkg/agent/seed_tool_roster.go`; with no active persona the agent keeps every tool). For a confined persona the agent also checks it when a tool runs (`checkPersonaConfinement`, `pkg/agent/persona_confinement.go`, called from the seed pre-execute hook and the legacy `executeTool` path, MCP tools included):

1. A tool outside `allowed_tools` is refused with a permission error, even if the model names it unprompted.
2. `write_file`, `edit_file`, `write_structured_file`, `patch_structured_file` (and a `browse_url` `screenshot_path`) must resolve, symlinks followed, strictly inside one of `write_paths`. `..` escapes, absolute paths elsewhere, and look-alike directories (`design-old/`) are refused.
3. `browse_url` only reads a page: `steps`, `action: interact|inspect`, persistent sessions, and `allow_file_url` are refused.

Tools that write to a fixed location of their own are governed by `allowed_tools` alone: the planner writes its plan only through `write_plan` (which validates against `plancontract` and writes `.sprout/plan.json` + `plan.md`) and its mockup only through `draw_mockup` (which writes `design/mockup.html`, inside its `write_paths`). It holds no file writers, so a raw write anywhere, including `.sprout/plan.json` or the mockup itself, is refused. Unconfined personas are unaffected.

The `planner` is the one confined persona today. Its tools: `ask_user`, `analyze_image_content`, `browse_url`, `write_plan`, `draw_mockup`, `read_file`, `list_directory`, `search`, and the host tool `start_change`. Host-specific facts (what can be built, where it is put online) are expected as a "What is offered" section the host supplies as conversation context; without it, the prompt plans the idea in plain terms and names no starters or services. `design_critique` and `design_render` are deliberately left off: the bar is a first screen within seconds, and the person reviews the mockup themselves and asks for changes. Host-provided tools are declared in `host_tools` and also listed on `allowed_tools`: `start_change` (which hands a change to an existing project's queue) is registered by the host through the headless agent API (`docs/integration/headless-agent.md`); without a host that registers it, it is simply absent, and the prompt says what the change is instead.

### 6b. The mockup tool (`draw_mockup`)

The mockup is one self-contained document, `design/mockup.html`, with a `<section data-screen="id">` per screen and a small script switching screens on `data-go` clicks (the first screen shows without script). It is drawn by its own streamed model call rather than written into a tool argument, so a host can render it as it is written (`pkg/agent_tools/draw_mockup_handler.go`, `mockup_document.go`, prompts in `pkg/agent_tools/mockup_prompts/`).

The model builds it with the design kit (`pkg/mockupkit`): it names a kit (a look with its own design brief) and a contrast-checked palette on `<html>`, writes markup with the kit's classes and references icons by name, and never writes the kit itself. The kit is added to the stream as soon as `<body>` has passed, so the first screen paints designed; a change is made to the document with the kit stripped; and the saved file is the assembled document, which opens on its own. Writing markup rather than CSS also makes a draw about twice as fast.

- No arguments: draws the document from `.sprout/plan.json`.
- `change` (and optionally `part`, the piece of a screen it is pinned to): with an existing mockup, one model call returns find/replace edits, applied exactly first and then line by line ignoring indentation, each matching once; if they do not apply, the whole document is drawn again with the change.
- The call goes through the agent's own client (`ToolFuncSet.StreamModelText`), with thinking off so the first bytes arrive at once, and its usage is booked under the `mockup` role.
- Events (each payload carries `mockup_id` plus the agent's event metadata, e.g. `chat_id`): `mockup_start {mode: "whole"|"edit", path}`, `mockup_chunk {seq, chunk}` (whole mode only), `mockup_done {mode, path, html, screens}`, `mockup_error {message}`. A second `mockup_start` with `mode: "whole"` after an edit-mode start means the edits did not apply: discard and render the chunks that follow. Start, done and error are never dropped by the event bus; chunks may be under load, which is why done carries the whole document.

---

## 7. Applying a persona

`Agent.ApplyPersona(personaID)` (`pkg/agent/persona.go:27`):

1. Resolve persona via `GetSubagentType` (alias-aware, disabled-aware). Canonicalize the ID.
2. If `persona.Provider` is set and differs from current, switch provider (model resets to provider default).
3. If `persona.Model` is set, apply it.
4. System prompt composition (in this order):
   - `system_prompt_text` set → replace current prompt entirely.
   - Else `system_prompt` (file path) set → load file, replace.
   - `system_prompt_append` set → append after the base/file/text prompt, separated by `---`.
5. **Orchestrator-only**: if active persona is `orchestrator`, append the embedded git-policy markdown (`orchestratorGitPolicyAppend`). The policy text documents the commit tool preference, staging rules, and which shell-side git ops are blocked.
6. Record the persona on `Agent.state`; at depth 0 also stamp `rootPersonaID`.
7. Merge depth + active persona into event metadata so every published event is tagged.

`ClearActivePersona()` restores `baseSystemPrompt`.

---

## 8. CLI and WebUI surfaces

### CLI: `/persona`

```
/persona                          # list personas, show active + source
/persona list                     # same
/persona <name>                   # activate the persona
/persona <name> apply             # same as bare activate
/persona <name> show              # show details (source, provider, model, tools, prompt)
/persona <name> enable|disable    # toggle DisabledPersonas
/persona new <id> [--from <core>] # create a user-scope persona file (§10)
/persona accept <id>              # accept a project-scope persona (§10)
/persona clear                    # clear active persona, restore base prompt
```

Implementation: `pkg/agent_commands/persona.go` (core surface) and `pkg/agent_commands/persona_custom.go` (custom personas).

`sprout persona validate [path]` checks persona definitions without starting an agent; `sprout persona dirs` prints the directories in use. Implementation: `cmd/persona.go`.

### WebUI: `GET /api/settings/subagent-types`

Read-only. Returns the hydrated persona map (core plus custom, each with its `source`), `disabled_personas`, available providers, and the user's current subagent provider/model. `PUT` and `DELETE` return `405 Method Not Allowed` — creating or editing personas in the WebUI is out of scope (`pkg/webui/settings_api_subagents.go`).

`disabled_personas` and `default_subagent_persona` are exposed via the broader settings endpoint (`pkg/webui/settings_api_put.go`).

---

## 9. Extending behavior

There are two ways to add behavior beyond what the catalog ships.

**Custom personas** (§10) when you need a different *tool set*, *prompt*, *skills*, *MCP servers*, or *write scope* — the persona owns all of it, and no Sprout release changes it.

**Skills** when you need reusable instructions the existing personas can load:

| Scope | Path |
|-------|------|
| User | `~/.config/sprout/skills/<name>/SKILL.md` |
| Project | `.sprout/skills/<name>/SKILL.md` |

Each skill is a directory with a `SKILL.md` frontmatter manifest plus optional supporting files. Skills are loaded on demand via the `activate_skill` tool; the in-tree library lives at `pkg/skills/library/`. The `self-help` skill in that library documents the format end-to-end.

---

## 10. Custom personas

A custom persona is a definition its creator owns entirely. Nothing is inherited from a core persona at run time, and no Sprout release changes what it can do. Core personas stay immutable: a custom persona may not reuse a core id or alias, and attempting to is a load error naming the file — never an override.

### The definition

The schema is the same `Definition` a core persona uses (`pkg/personas/catalog.go`), with these differences:

| Field | For a custom persona |
|---|---|
| `id`, `name`, `description`, `aliases` | Required id; no collision with core ids or aliases. |
| `system_prompt_text` | The prompt, inline. |
| `system_prompt` | A prompt file: relative to the persona file (user and project scope), or the path of a **core** prompt (`subagent_prompts/planner.md`), so a creator can start from a shipped prompt. This is the one thing that can point at core content, and it is only text; it carries none of the core persona's tools or rules. |
| `system_prompt_append` | As for core personas. |
| `allowed_tools` | The whole tool set: built-in tool names, host tool names, and MCP selectors. Empty means the default agent's tools. |
| `host_tools` | Tool names the host provides, or `["*"]`: any tool the host registers is offered. A name here is offered without also being listed in `allowed_tools`. |
| `skills` | Skill names the persona may see and activate, or `["*"]`. Absent: none. Listing any skill offers `activate_skill`/`list_skills` without naming them in `allowed_tools`. |
| `mcp` | MCP servers or tools the persona may use: `"<server>"` or `"<server>__<tool>"` (the SP-170 keys), or `["*"]`. Absent: none. The person's `approved_mcp_tools` still decides whether each call runs without asking. |
| `write_paths`, `capabilities`, `auto_approve_rules`, `read_only`, `delegatable`, `can_spawn_non_delegatable`, budgets, `provider`, `model`, `local_only` | As for core personas. |
| `enabled` | Defaults to true. |

Unknown tool names are a warning when the persona loads — it may name a host tool the current host does not register — and are reported by `sprout persona validate`. Malformed JSON, a missing required field, or a collision is an error that skips that one persona; it never stops the others from loading or Sprout from starting.

### Scopes and trust

| Scope | Source | Trust |
|---|---|---|
| **Host** | The headless agent API: `createAgent({ persona: <definition> })`, and `definePersona(def)` on the runtime for a host that runs several agents. In memory; never written to disk. | Trusted: the host is the creator. |
| **User** | `<config dir>/personas/*.json` (`~/.config/sprout/personas/`, under `SPROUT_CONFIG` when set). One persona or a `{"personas": [...]}` catalog per file. | Trusted: the person is the creator. |
| **Project** | `<workspace>/.sprout/personas/*.json`. | Listed, but not usable until the person accepts it. Accepting is recorded against the file's content hash; a changed file needs accepting again. In headless and non-interactive runs, an unaccepted project persona is unavailable. |

Precedence among custom personas with the same id: host, then user, then project. A shadowed custom persona is reported in `/persona list`, never silently dropped.

### The limits — what "complete control" does not mean

A custom persona chooses what it can do *within what the person or host running it allows*. These gates apply to every persona, core or custom, and a persona cannot grant itself past them:

- The global classifier (gate 1, §6). `auto_approve_rules` can reduce asking only up to what gate 1 permits; critical operations stay blocked.
- The SP-169 prompt-injection defence for ingested content.
- The SP-170 MCP authorization gate. A persona's `mcp` list decides *which* MCP tools it may call; `approved_mcp_tools` still decides whether a call runs without asking.
- The sandbox and the computer-use activation gates. A persona holding `computer_use` is still held to `computer_use.enabled`, the top-level rule, and interactive consent.

### Surfaces

- **Headless agent API** (`@sprout-foundry/workspace/agent`): `CreateAgentOptions.persona` is `string | PersonaDefinition`. An invalid definition makes `createAgent` reject with the validation errors. `definePersona(def)` registers a persona on the runtime for every agent it builds from then on.
- **CLI:** `/persona new <id> [--from <core-id>]`, `/persona list` (with source), `/persona show <id>` (effective prompt source and tool set), `/persona accept <id>`, and `sprout persona validate [path]`.
- **Web UI:** the persona picker lists custom personas with their source. Creating and editing them in the Web UI is out of scope.
- **Subagents:** a custom persona with `delegatable: true` can be spawned like any worker, governed by the existing `delegatable` / `can_spawn_non_delegatable` rules.
- **Events:** the `active_persona` metadata gains `persona_source`. A custom persona's prompt and definition are never sent anywhere Sprout does not already send the active prompt.

### The retired `subagent_types` key

A `subagent_types` block left in an old `config.json` is ignored, as it has been since personas became catalog-fixed. Sprout prints a one-time notice pointing at `<config dir>/personas/`.

---

## Reference

### Source-file index

| Component | Location |
|-----------|----------|
| Catalog definitions (Go) | `pkg/personas/catalog.go` |
| Catalog data (JSON) | `pkg/personas/configs/*.json` |
| Persona ID + capability constants | `pkg/personas/ids.go` |
| Config integration | `pkg/configuration/config.go` (`SubagentType`, `GetSubagentType`, `IsPersonaDisabled`, `DefaultSubagentPersona`) |
| Persona activation | `pkg/agent/persona.go` (`ApplyPersona`, `isGitWriteAllowed`, `canSpawnNonDelegatable`, `GetAvailablePersonaIDs`) |
| Coordinator auto-activate | `pkg/agent/agent_creation.go::maybeAutoActivateCoordinatorPersona` |
| Subagent spawn gate | `pkg/agent/tool_handlers_subagent.go::handleRunSubagent` |
| Depth limits | `pkg/agent/agent_getters.go::MaxSubagentDepth` |
| Git-write gating | `pkg/agent/tool_handlers_shell.go` |
| `/persona` command | `pkg/agent_commands/persona.go`, `pkg/agent_commands/persona_custom.go` |
| Custom persona loader | `pkg/personas/custom.go` |
| Custom persona hydration | `pkg/configuration/config_personas.go` |
| `sprout persona` command | `cmd/persona.go` |
| WebUI read API | `pkg/webui/settings_api_subagents.go` |
| System prompt files | `pkg/agent/prompts/subagent_prompts/*.md` |
| Confined-persona enforcement | `pkg/agent/persona_confinement.go` |

### ID normalization

Persona IDs and aliases are normalized by:
1. Trim whitespace
2. Lowercase
3. Replace `-` with `_`

So `"Web-Scraper"`, `"web_scraper"`, and `"web-scraper"` all resolve to `web_scraper`. The same normalization runs in `personas.normalizeID`, `agent.normalizeAgentPersonaID`, and `configuration.normalizePersonaID`.
