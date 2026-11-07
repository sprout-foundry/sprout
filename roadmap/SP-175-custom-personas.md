# SP-175 — Custom Personas

Status: Proposed (2026-11-07).

## Problem

Anyone building on Sprout should be able to mint a persona and decide
everything it can do: its prompt, its tools (built-in, host, MCP), its
skills, where it may write, what auto-approves, which model it runs on.
That was the original intent of personas. It was lost in `2964a99b3e`
(remove role manager and extend command), which made personas
catalog-fixed:

- `Config.SubagentTypes` is hydrated from the embedded catalog and tagged
  `json:"-"`; nothing on disk can add to it (`pkg/configuration/config.go`:
  "personas are catalog-fixed and user customization is intentionally not
  supported").
- The headless agent API takes a persona **id** only
  (`CreateAgentOptions.persona: string`), resolved against that fixed
  catalog (`cmd/wasm/agent_options.go`).
- A host tool is offered only when the active persona's `allowed_tools`
  names it, and only a catalog persona can name it.

The roles system that was removed let users *override* the shipped
personas, which mixed two different things: changing a core persona, and
having your own. This spec keeps the first impossible and makes the second
first-class.

The cost today is concrete. A host that embeds Sprout (the Sprout Foundry
platform runs the in-browser agent as its planner) cannot give its agent a
new tool, or change which tools it has, without a change to Sprout's
catalog and a Sprout release. The prompt is already the host's (the
`context` option appends to it); the tool set is not.

## Decisions

1. **Core personas are immutable.** The embedded catalog
   (`pkg/personas/configs/*.json`) is not overridable from any source: no
   file, config key or API call changes a core persona's prompt, tools,
   confinement, capabilities or rules. `disabled_personas` still hides one.
2. **Custom personas are owned entirely by their creator.** Everything in
   the persona schema is theirs to set. Nothing is inherited at run time
   from a core persona, and no Sprout release changes what a custom persona
   can do.
3. **A custom persona never shadows a core one.** Its id and aliases must
   not collide with any core id or alias. A collision is a load error for
   that persona (named, with the file it came from), not an override.
4. **"Complete control" is over the persona, not over the runtime's safety
   gates.** A custom persona chooses what it can do within what the person
   or host running it allows. The global classifier (gate 1), the SP-169
   prompt-injection defence, the SP-170 MCP authorization, the sandbox and
   the computer-use activation gates apply to every persona. A persona
   cannot grant itself past them.
5. **Who mints it is the trust boundary.** The person (user scope) and the
   embedding host (host scope) are trusted as the creator. A persona that
   arrives inside a repository (project scope) is content until the person
   accepts it.

## The persona definition

The schema is the existing catalog `Definition` (`pkg/personas/catalog.go`)
and `SubagentType`, unchanged in meaning, with these differences for custom
personas:

| Field | For a custom persona |
|---|---|
| `id`, `name`, `description`, `aliases` | Required id; no collision with core ids or aliases. |
| `system_prompt_text` | The prompt, inline. |
| `system_prompt` | A prompt file: relative to the persona file (user and project scope), or the path of a **core** prompt (`subagent_prompts/planner.md`), so a creator can start from a shipped prompt. This is the one thing that can point at core content, and it is only text; it carries none of the core persona's tools or rules. |
| `system_prompt_append` | As today. |
| `allowed_tools` | The whole tool set: built-in tool names, host tool names, and MCP selectors (below). Empty means the default agent's tools, as today. |
| `host_tools` | Tool names the host provides, or `["*"]`: any tool the host registers is offered. A name in `host_tools` is offered without also being listed in `allowed_tools`. |
| `skills` | **New.** Skill names the persona may see and activate, or `["*"]`. Absent: none. Listing any skill offers `activate_skill`/`list_skills` without naming them in `allowed_tools`. |
| `mcp` | **New.** MCP servers or tools the persona may use: `"<server>"` or `"<server>__<tool>"` (the SP-170 keys), or `["*"]`. Absent: none. The person's SP-170 allowlist still decides whether each call runs without asking. |
| `write_paths`, `capabilities`, `auto_approve_rules`, `read_only`, `delegatable`, `can_spawn_non_delegatable`, budgets, `provider`, `model`, `local_only` | As today. `auto_approve_rules` can only reduce asking up to what gate 1 allows (decision 4). |
| `enabled` | Defaults to true for custom personas. |

Unknown tool names are a warning when the persona loads (it may name a host
tool the current host does not register) and are reported by
`sprout persona validate`. Malformed JSON, a missing required field or a
collision is an error that skips that one persona; it never stops the
others from loading or Sprout from starting.

## Where custom personas come from

| Scope | Source | Trust |
|---|---|---|
| **Host** | The headless agent API: `createAgent({ persona: <definition> })`, and `definePersona(def)` on the runtime for a host that runs several agents. In memory; never written to disk. | Trusted: the host is the creator. |
| **User** | `<config dir>/personas/*.json` (`~/.config/sprout/personas/`, under `SPROUT_CONFIG` when set). One persona or a `{"personas": [...]}` catalog per file, the same shape as the embedded catalog. | Trusted: the person is the creator. |
| **Project** | `<workspace>/.sprout/personas/*.json`. | Listed, but not usable until the person accepts it. Accepting is recorded against the file's content hash; a changed file needs accepting again. In headless and non-interactive runs, an unaccepted project persona is unavailable. |

Precedence among custom personas with the same id: host, then user, then
project. A shadowed custom persona is reported in `/persona list`, not
silently dropped.

## Surfaces

- **Headless agent API** (`@sprout-foundry/workspace/agent`):
  `CreateAgentOptions.persona` becomes `string | PersonaDefinition`. The
  `PersonaDefinition` type mirrors the JSON schema and is exported. The WASM
  bridge carries the definition in the run options it already passes
  (`cmd/wasm/agent_options.go`), validates it with the same loader as the
  file sources, and applies it through the same `ApplyPersona` path. An
  invalid definition makes `createAgent` reject with the validation errors.
  A string still resolves a core persona, or one the host defined with
  `definePersona`.
- **CLI:**
  - `/persona new <id> [--from <core-id>]` writes a user-scope file. With
    `--from`, it copies the core persona's definition into the file once
    (and points `system_prompt` at the core prompt). It is the creator's
    file from then on.
  - `/persona list` shows each persona's source (core, host, user,
    project), and whether it is waiting to be accepted.
  - `/persona show <id>` shows the effective prompt source and tool set.
  - `/persona accept <id>` accepts a project persona.
  - `sprout persona validate [path]` checks the definitions.
- **Web UI:** the persona picker lists custom personas with their source.
  Creating and editing them in the web UI is out of scope for this spec.
- **Subagents:** a custom persona with `delegatable: true` can be spawned
  like any worker, and appears in the coordinator's roster. Spawning is
  governed by the existing `delegatable` / `can_spawn_non_delegatable`
  rules.
- **Events and telemetry:** the `active_persona` metadata gains
  `persona_source`. A custom persona's prompt and definition are never sent
  anywhere Sprout does not already send the active prompt.

## Implementation notes

- **Loading:** `personas.DefaultDefinitions()` stays the core catalog. A new
  loader reads user and project files with the existing
  `loadDefinitionsFromFS` conflict checks, plus the collision check against
  core. Configuration hydration merges custom definitions into
  `SubagentTypes`, marked with their source, after core. Core entries are
  never replaced. `SubagentTypes` stays `json:"-"`: custom personas live in
  their own files, not in `config.json`.
- **Tool roster** (`getActivePersonaToolAllowlist`, `seed_tool_roster.go`,
  `conversation.go`): the effective set for a custom persona is
  `allowed_tools` ∪ host tools matched by `host_tools` ∪ the skill tools
  when `skills` is set ∪ the MCP tools matched by `mcp`. For core personas
  the roster is computed exactly as today.
- **Host tools:** `host_tools: ["*"]` means the persona sees every tool
  registered through `registerHostTool`, at the moment the turn's roster is
  built.
- **Confinement:** `write_paths` keeps its meaning (it also enforces the
  tool set at execution). An unconfined custom persona has its roster
  enforced at listing time, as unconfined core personas do.
- **Accepting project personas:** stored alongside the SP-169 trust
  settings, keyed by workspace root, persona id and content hash.
- **The legacy `subagent_types` key**, if left in an old `config.json`,
  stays ignored, with a one-time notice pointing at
  `<config dir>/personas/`.

## Tests

- A core persona cannot be changed from user, project or host scope: each
  attempt to reuse a core id or alias is reported and the core definition
  is unchanged.
- A user-scope persona loads, applies, and its roster is exactly what it
  declares (built-in, host `*`, the named skills, the named MCP servers).
- `host_tools: ["*"]` offers a tool registered after the agent was created.
- A host-scope definition through `createAgent` works in the WASM build
  (`packages/workspace/src/agent/integration.wasm.test.ts`), and an invalid
  one rejects with its errors.
- A project persona is unavailable until accepted, is available after,
  and is unavailable again when its file changes.
- `auto_approve_rules` on a custom persona cannot auto-approve what gate 1
  denies. SP-170 still asks for an MCP tool outside the person's allowlist.
- Computer-use capabilities on a custom persona still pass the activation
  gates.
- One malformed persona file is skipped and named; the rest load.
- The existing exact-tool-set tests for core personas
  (`pkg/personas/planner_test.go`, `dev_planner_test.go`, …) still pass
  unchanged.

## Docs

- `docs/PERSONAS.md`: a "Custom personas" section (the schema table, the
  scopes, trust, and decision 4's limits).
- `docs/integration/headless-agent.md`: passing a definition to
  `createAgent`, `definePersona`, and `host_tools: ["*"]`.
- `CHANGELOG.md`.

## Out of scope

- Editing core personas in any form, including per-field patches.
- Creating or editing personas in the web UI.
- Sharing or publishing personas (a registry).
- Live inheritance from a core persona. `--from` copies once; a
  `system_prompt` path can reuse a core prompt's text, and that is all.

## First consumer

The Sprout Foundry platform defines its planners as host-scope personas: its
own prompt (built on `subagent_prompts/planner.md` with its additions), its
own tool set, and `host_tools: ["*"]` for the platform's tools, such as the
account actions the planner uses when someone asks about their account
instead of a new idea. After this ships, the platform changes what its
planner can do without a Sprout release.
