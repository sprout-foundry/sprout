# SP-038: Tool Dispatch Consolidation — Registry Over Switch

**Status:** 📋 Proposed
**Date:** 2026-05-19
**Priority:** MEDIUM (maintainability; not a runtime bug, but a sustained tax on every new tool)
**Depends on:** SP-029 (Monolith Decomposition) — complementary; this targets a different decomposition axis
**Related:** SP-031 (MCP Tool Input Validation), SP-034 (WebUI Protocol Hygiene — error envelope)

## Problem

Adding a tool to sprout requires editing **at least four locations** spread across two packages and ten-plus files:

1. **Definition**: append a `ToolDefinition` struct in `pkg/agent/tool_definitions.go` (currently **1007 lines** — TODO.md / commit messages note the size repeatedly).
2. **Handler**: find or create the right `tool_handlers_*.go` file. The current sharding:
   - `tool_handlers_subagent.go`, `tool_handlers_subagent_parallel.go`, `tool_handlers_subagent_utils.go`, `tool_handlers_subagent_test.go` (4 files for one tool family)
   - `tool_handlers_task_queue.go`, `tool_handlers_todo.go`, `tool_handlers_shell.go`, `tool_handlers_test.go`
   - Plus several more (`tool_handlers_*.go`) for read/write/web/etc.
3. **Dispatch**: a switch statement (or chain of `if` blocks) somewhere in `pkg/agent/tool_executor*.go` that maps tool name → handler function.
4. **Command surface**: in `pkg/agent_commands/` (**62 files**), separate command-style wrappers for the same tools. Commands and tools both exist for many capabilities (e.g., `commit`, `clear`, `changes`) — the relationship is undocumented.

### Concrete pain points

- **No single source of truth for "what tools exist."** `tool_definitions.go` lists them; `pkg/agent/persona.go` filters them per persona; `pkg/agent_commands/command_selector.go` exposes a parallel command set; the personas JSON files name them via `AllowedTools`. Drift between these is the root cause of issues like the `write_file` / `write_structured_file` migration warning in SP-035 Phase 4.
- **No interface contract.** Each handler has its own signature variation. Some take `*Agent`, some take `context.Context`, some take a config snapshot, some validate input themselves, some delegate to a validator. There is no `ToolHandler` interface.
- **Registration is implicit.** A new tool is discoverable only by string match against `tool_definitions.go`. There is no `init()`-time registration, no test that enumerates tools and verifies handler existence, and no startup assertion that every declared tool has a handler.
- **Tests are pessimistic.** Because adding a tool touches so many files, the tests at `pkg/agent/tool_handlers_test.go`, `pkg/agent/tool_handlers_todo_test.go`, `pkg/agent/tool_handlers_subagent_test.go`, etc. test one tool family per file with bespoke setup. A registry would enable a single tool-conformance test parameterized over the registry.

### Why not "just split tool_definitions.go more"?

SP-029 (Monolith Decomposition) is splitting large files by file count. That fixes the line-count metric but not the underlying problem: the tool model is not abstracted. Splitting 1007 lines into ten 100-line files preserves the same "edit four places to add one tool" tax. The fix is to introduce an abstraction (registry + interface), not just move bytes.

## Goals / Non-Goals

**Goals**
- A `ToolHandler` interface that every tool implements (`Name() / Definition() / Validate(input) / Execute(ctx, agent, input) (output, error)`).
- A `ToolRegistry` that tools register into via `init()` (or explicit `RegisterTool` calls from a central tools-init file).
- `tool_definitions.go` becomes a 100-line file that lists the registered tools and exposes `RegistryForPersona(p)` returning the filtered set.
- Adding a new tool requires creating **one** file under `pkg/agent_tools/<tool_name>.go` (or `pkg/agent/tools/<tool_name>.go`) and importing it from a central tools-init file.
- Every registered tool has automated conformance tests covering: input validation, error envelope, persona allowlist filtering.

**Non-Goals**
- Rewriting tool *semantics* — output formats, file effects, and side effects stay identical.
- Merging `pkg/agent_commands/` with the tool registry (they serve different surfaces — CLI commands vs LLM tool calls). That's a follow-up if useful; out of scope here.
- Auto-generating tool definitions from Go reflection — explicit is better than magic for the LLM-facing schemas.
- Changing the on-wire tool-call protocol or JSON schemas exposed to the model.

## Current State

| Surface | Location | LOC | Concern |
|---------|----------|-----|---------|
| Definitions | `pkg/agent/tool_definitions.go` | 1007 | Single large list; no per-tool isolation |
| Handlers | `pkg/agent/tool_handlers_*.go` | 10,215 across many files | Inconsistent signatures; sharded by accident |
| Dispatch | `pkg/agent/tool_executor*.go` | (audit needed) | switch/if chain |
| Commands | `pkg/agent_commands/*.go` | 62 files | Parallel command surface; relationship undocumented |
| Persona filter | `pkg/agent/persona.go` | (audit) | String-match against `AllowedTools` JSON |
| Tests | `pkg/agent/tool_handlers_*_test.go` | many | Per-family setup boilerplate |

## Proposed Solution

### Track A — Define the interface

A1. **`ToolHandler` interface** in a new file `pkg/agent_tools/handler.go`:
```go
type ToolHandler interface {
    Name() string
    Definition() ToolDefinition          // existing struct, moved here
    Validate(rawInput json.RawMessage) error
    Execute(ctx context.Context, env ToolEnv, input json.RawMessage) (ToolResult, error)
}

type ToolEnv struct {                    // explicit dependencies, no *Agent
    Cfg           *configuration.Config
    Workspace     string
    Logger        logging.Logger
    SecurityCheck func(ToolCall) RiskClassification  // injected from tool_definitions.go classifier
    EventEmitter  events.Emitter
    // … add as needed; goal: minimal, explicit, mockable
}

type ToolResult struct {
    Output       string
    StructuredOut json.RawMessage  // optional; for structured tools
    TokenUsage   int               // for SP-037 fleet budget
}
```

A2. **`ToolRegistry`** as a struct keyed by tool name with `Register(h ToolHandler)` and `Lookup(name) (ToolHandler, bool)` methods. Thread-safe (registry is built once at init then read-only).

A3. **`PersonaFilter`** as a `func(persona Persona, all []ToolHandler) []ToolHandler` that applies the existing AllowedTools logic. Moved from `pkg/agent/persona.go` into the registry package so the filter ships with the registry.

### Track B — Migrate tools incrementally

B1. **Pick the simplest tool first** (likely `read_file` or `list_directory`). Move its definition + handler into `pkg/agent_tools/read_file.go`. Implement `ToolHandler`. Register from `pkg/agent_tools/all.go`.

B2. **Dual-dispatch shim.** During migration, the dispatcher in `pkg/agent/tool_executor*.go` checks the registry first; if found, calls the new path. Otherwise falls back to the legacy switch. This lets migration happen one tool at a time with no big-bang.

B3. **Migrate in priority order** based on size of the legacy handler file (start small to validate the pattern, finish with the big ones):
  - Smallest: `read_file`, `list_directory`, `web_fetch`
  - Medium: `write_file`, `edit_file`, `shell_command`
  - Large: subagent family (`run_subagent`, `run_subagent_parallel`), task queue, todo

B4. **Per-tool migration check.** Each tool's migration PR adds a `TestTool_<Name>_Conformance` (see Track D) and removes the legacy switch case. CI must stay green.

### Track C — Shrink `tool_definitions.go`

C1. **End state for `pkg/agent/tool_definitions.go`**: a thin file that:
  - Imports `pkg/agent_tools/all` (which has the side-effect of registering every tool).
  - Exposes `RegistryForPersona(p) []ToolHandler` calling the registry's filter.
  - Retains the security classifier (`ClassifyToolCall`) since that's a cross-cutting concern, *not* per-tool.

C2. **Move the classifier to its own file.** `pkg/agent_tools/security_classifier.go` already exists (referenced in SP-033 / SP-035). Make `ClassifyToolCall` live there and have the registry call into it. This breaks the last reason `tool_definitions.go` exists in the agent package.

C3. **Target line count for `tool_definitions.go`**: ≤ 150 lines once migration completes.

### Track D — Tests by registry iteration

D1. **`TestRegistry_AllToolsHaveValidDefinitions`** — iterate the registry; for each tool, assert `Definition().Name == Name()`, `Description != ""`, `InputSchema` is parseable JSON Schema.

D2. **`TestRegistry_AllToolsRespectPersonaFilter`** — synthesize personas with various `AllowedTools` lists; assert the filter produces exactly the named subset for each.

D3. **`TestRegistry_AllToolsValidate`** — for each tool with a documented invalid-input fixture, call `Validate(badInput)` and assert it returns an error.

D4. **`TestRegistry_NoOrphanHandlers`** — assert every registered tool's `Definition().Name` matches some entry in the persona allowlists across all built-in personas, OR is in a "tool not in any persona" allowlist (caught explicitly so deletions are deliberate).

D5. **Conformance template.** A helper `runToolConformance(t, name, validFixture, invalidFixture)` that runs the standard battery for any tool — used by per-tool tests to reduce boilerplate.

### Track E — Document the model

E1. **`docs/TOOLS.md`** — covers: how to add a tool (one-file recipe), the `ToolHandler` interface, the `ToolEnv` contract, the registry init order, the persona filter, the relationship between tools and `pkg/agent_commands/` commands.

E2. **Package doc comment** on `pkg/agent_tools/handler.go` covering the same.

## Implementation Phases

### Phase 1: Interface + registry
[ ] SP-038-1a: Create `pkg/agent_tools/handler.go` with `ToolHandler`, `ToolEnv`, `ToolResult` types.
[ ] SP-038-1b: Create `pkg/agent_tools/registry.go` with `ToolRegistry` (Register, Lookup, All, ForPersona).
[ ] SP-038-1c: Move `ClassifyToolCall` from `pkg/agent/tool_definitions.go` to `pkg/agent_tools/security_classifier.go`. Update all callers.
[ ] SP-038-1d: Add `pkg/agent_tools/all.go` as the central tools-init file (initially empty — tools migrate into it).

### Phase 2: Dual-dispatch shim
[ ] SP-038-2a: In `pkg/agent/tool_executor*.go`, check the registry first; fall back to legacy switch on miss. Audit caller log statements.
[ ] SP-038-2b: Add a `TestDualDispatch_RegistryWins` test confirming a registered tool takes precedence over a legacy entry of the same name.

### Phase 3: Migrate small tools
[ ] SP-038-3a: Migrate `read_file` to `pkg/agent_tools/read_file.go`. Remove from legacy. Add conformance test.
[ ] SP-038-3b: Migrate `list_directory`.
[ ] SP-038-3c: Migrate `web_fetch`.
[ ] SP-038-3d: Migrate `read_directory` / `glob` / similar small tools (one per commit if possible).

### Phase 4: Migrate medium tools
[ ] SP-038-4a: Migrate `write_file` (+ `write_structured_file`).
[ ] SP-038-4b: Migrate `edit_file`.
[ ] SP-038-4c: Migrate `shell_command` (carefully — interacts with two-gate risk model, see SP-035).
[ ] SP-038-4d: Migrate `search_memories` / `save_memory` (touches SP-027 paths).

### Phase 5: Migrate large/complex tools
[ ] SP-038-5a: Migrate the subagent family — likely keep them in `pkg/agent_tools/subagent/` subdirectory due to size.
[ ] SP-038-5b: Migrate task queue + todo tools.
[ ] SP-038-5c: Migrate remaining tools (image/vision, PDF, browser, etc.).

### Phase 6: Cleanup + tests
[ ] SP-038-6a: Remove the legacy switch from `pkg/agent/tool_executor*.go` once registry covers every tool.
[ ] SP-038-6b: Verify `pkg/agent/tool_definitions.go` is ≤ 150 lines.
[ ] SP-038-6c: Add `TestRegistry_*` suite (the 4 conformance tests in Track D).
[ ] SP-038-6d: Run `go test -race ./pkg/agent/ ./pkg/agent_tools/` 10× clean.

### Phase 7: Documentation
[ ] SP-038-7a: Write `docs/TOOLS.md`.
[ ] SP-038-7b: Add package doc comment on `pkg/agent_tools/handler.go`.

## Success Criteria

| Metric | Target |
|--------|--------|
| `pkg/agent/tool_definitions.go` LOC | ≤ 150 |
| Per-tool file location | One file per tool under `pkg/agent_tools/` |
| Adding a new tool requires editing files | 1 (the new tool's file) + 1 (registry init) |
| `TestRegistry_*` conformance suite | Passes for every registered tool |
| Behavioral parity tests | Existing `tool_handlers_*_test.go` continue to pass unchanged |

## Files Reference

| File | Action |
|------|--------|
| `pkg/agent_tools/handler.go` | Create: `ToolHandler` interface + `ToolEnv` + `ToolResult` |
| `pkg/agent_tools/registry.go` | Create: thread-safe registry + persona filter |
| `pkg/agent_tools/security_classifier.go` | Modify: take ownership of `ClassifyToolCall` from `pkg/agent/tool_definitions.go` |
| `pkg/agent_tools/all.go` | Create: side-effect import of every tool |
| `pkg/agent_tools/<tool>.go` (×N) | Create: one file per migrated tool |
| `pkg/agent/tool_definitions.go` | Modify: shrink to ≤ 150 lines; delegate to registry |
| `pkg/agent/tool_executor*.go` | Modify: dual-dispatch during migration, registry-only after |
| `pkg/agent/persona.go` | Modify: persona filter logic moves to registry; this file becomes thinner |
| `pkg/agent/tool_handlers_*.go` | Delete (incrementally as tools migrate) |
| `pkg/agent_tools/registry_test.go` | Create: 4 conformance tests |
| `docs/TOOLS.md` | Create |

## Risks

- **Long migration window.** With 20+ tools to move, this is a multi-week effort. Mitigation: the dual-dispatch shim means every individual migration is atomic and reversible.
- **Persona allowlist regressions.** Moving the filter could change which tools a persona sees by one off-by-one. Mitigation: Track D's conformance test enumerates persona-tool pairs; a regression fails CI.
- **Hidden coupling in handlers.** Some handlers may reach into `*Agent` for state not in `ToolEnv`. Mitigation: during migration, audit each handler's `*Agent` access and either pass it through `ToolEnv` explicitly or refactor the handler to not need it.
- **`pkg/agent_commands/` relationship stays muddy.** This spec doesn't unify commands and tools. Mitigation: in `docs/TOOLS.md`, explicitly document them as separate surfaces (CLI vs LLM); a follow-up spec can decide if they should converge.
- **Test churn.** The conformance suite may surface latent issues. Mitigation: that's the point.
