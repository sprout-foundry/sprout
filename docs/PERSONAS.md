# Sprout Persona System

A **persona** is a named specialization that configures how the agent behaves: system prompt, tool allowlist, optional provider/model, capability grants (e.g. git-write), and which other personas it may spawn as subagents.

Personas are **catalog-fixed**. The set of personas, their tool allowlists, system prompts, providers, and models ship in `pkg/personas/configs/*.json` and are not user-customizable at runtime. The only mutable knobs are: hide a persona, change the default spawn target, and toggle orchestrator git-write. To extend agent behavior beyond what the catalog ships, use **skills** (`~/.config/sprout/skills/<name>/SKILL.md`).

---

## 1. The Catalog

| ID | Description | Delegatable | Notes |
|----|-------------|-------------|-------|
| `orchestrator` | Planning and delegation; classify → activate skill → delegate → verify | No | Carries `git_write` capability. Aliases: `orchestration`, `repo_orchestrator`, `repo_operator`, `git_orchestrator` |
| `coordinator` | Cross-project orchestration; manages a persistent task queue, delegates to orchestrator subagents | No | Carries `git_write` unconditionally. Lists `orchestrator` in `can_spawn_non_delegatable`. Aliases: `executive_assistant`, `ea`, `assistant` |
| `general` | General-purpose persona for tasks that don't need deep specialization | Yes | Alias: `default` |
| `coder` | Feature implementation and production code writing | Yes | |
| `refactor` | Behavior-preserving refactoring, low-risk and incremental | Yes | |
| `debugger` | Bug investigation, root cause analysis, targeted fixes | Yes | |
| `tester` | Unit-test authoring and coverage | Yes | |
| `reviewer` | Code review, security review, best-practices | Yes | Alias: `code_reviewer` |
| `researcher` | Codebase analysis combined with external research | Yes | |
| `web_scraper` | Web extraction and structured content collection | Yes | Aliases: `web-scraper`, `scraper` |
| `computer_user` | Desktop automation with screenshots, mouse, and keyboard | No | Carries `computer_use` capability |

Source: `pkg/personas/configs/default_personas.json`, `pkg/personas/configs/coordinator.json`, `pkg/personas/configs/computer_user.json`.

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

### `disable_coordinator_auto_activate` — opt out of coordinator activation in $HOME

Coordinator auto-activates when sprout starts with the workspace root resolving to the user's home directory. Set this to `true` to skip auto-activation; the user then picks a persona explicitly.

Implementation: `pkg/agent/agent_creation.go:504-553` (`autoActivateCoordinatorPersona`).

### `subagent_max_depth` — cap nesting depth

Max nesting for subagent chains. Default depends on the root persona: coordinator → 2 (allows coordinator → orchestrator → specialist); other roots → 1. An explicit value overrides both.

Implementation: `pkg/agent/agent_getters.go:503-516` (`MaxSubagentDepth`).

---

## 3. Architecture

Two layers, not three. There is no user-override merge.

### Layer 1: Catalog (embedded JSON)

`pkg/personas/configs/*.json` is embedded into the binary via `//go:embed`. At startup `personas.DefaultDefinitions()` reads every `.json` in that directory, merges the persona arrays, and returns a `map[string]Definition` keyed by normalized ID.

**Conflict detection** runs at load time:
- Duplicate persona IDs across files → load error.
- An alias that shadows another file's ID → load error.
- An alias declared by two personas → load error.

See `loadDefinitionsFromFS` in `pkg/personas/catalog.go:87`. On load failure, a minimal fallback (orchestrator + general) keeps the binary functional.

### Layer 2: Config (in-memory `SubagentTypes`)

`defaultSubagentTypes()` (`pkg/configuration/config.go:2237`) converts every `Definition` into a `SubagentType` and stores them in `Config.SubagentTypes`. This map is **always** repopulated from the catalog at config load; `SubagentTypes` is tagged `json:"-"` so it never round-trips to disk.

The `SubagentType` struct mirrors `Definition`:

```go
type SubagentType struct {
    ID, Name, Description           string
    Provider, Model                 string  // optional overrides
    SystemPrompt, SystemPromptText  string
    SystemPromptAppend              string
    AllowedTools, Aliases           []string
    Enabled, LocalOnly, Delegatable bool
    AutoApproveRules                *AutoApproveRules
    Capabilities                    []string // e.g. "git_write"
    CanSpawnNonDelegatable          []string // persona IDs the spawner may target despite Delegatable=false
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

The only capability constant today is `personas.CapabilityGitWrite = "git_write"` (`pkg/personas/ids.go`).

| Persona | Has `git_write`? | Effective? |
|---------|------------------|------------|
| `orchestrator` | Yes | Always (has capability) |
| `coordinator` | Yes | Always (has capability) |
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
/persona                          # list personas, show active
/persona list                     # same
/persona <name>                   # activate the persona
/persona <name> apply             # same as bare activate
/persona <name> show              # show details (provider, model, tools, prompt path)
/persona <name> enable|disable    # toggle DisabledPersonas
/persona clear                    # clear active persona, restore base prompt
```

Implementation: `pkg/agent_commands/persona.go`.

### WebUI: `GET /api/settings/subagent-types`

Read-only. Returns the catalog map, `disabled_personas`, available providers, and the user's current subagent provider/model. `PUT` and `DELETE` return `405 Method Not Allowed` — persona customization is intentionally not supported (`pkg/webui/settings_api_subagents.go`).

`disabled_personas` and `default_subagent_persona` are exposed via the broader settings endpoint (`pkg/webui/settings_api_put.go`).

---

## 9. Extending behavior

Personas are catalog-fixed. To add behavior, use **skills**:

| Scope | Path |
|-------|------|
| User | `~/.config/sprout/skills/<name>/SKILL.md` |
| Project | `.sprout/skills/<name>/SKILL.md` |

Each skill is a directory with a `SKILL.md` frontmatter manifest plus optional supporting files. Skills are loaded on demand via the `activate_skill` tool; the in-tree library lives at `pkg/skills/library/`. The `self-help` skill in that library documents the format end-to-end.

If a workflow needs a different combination of tools or a tailored system prompt, write a skill — don't fork the catalog.

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
| Coordinator auto-activate | `pkg/agent/agent_creation.go::autoActivateCoordinatorPersona` |
| Subagent spawn gate | `pkg/agent/tool_handlers_subagent.go::handleRunSubagent` |
| Depth limits | `pkg/agent/agent_getters.go::MaxSubagentDepth` |
| Git-write gating | `pkg/agent/tool_handlers_shell.go` |
| `/persona` command | `pkg/agent_commands/persona.go` |
| WebUI read API | `pkg/webui/settings_api_subagents.go` |
| System prompt files | `pkg/agent/prompts/subagent_prompts/*.md` |

### ID normalization

Persona IDs and aliases are normalized by:
1. Trim whitespace
2. Lowercase
3. Replace `-` with `_`

So `"Web-Scraper"`, `"web_scraper"`, and `"web-scraper"` all resolve to `web_scraper`. The same normalization runs in `personas.normalizeID`, `agent.normalizeAgentPersonaID`, and `configuration.normalizePersonaID`.
