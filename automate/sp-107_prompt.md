# SP-107: Single-Source Tool Definitions — Autonomous Implementation

You are an autonomous Coordinator agent implementing SP-107 end-to-end.
Read the full spec at `roadmap/SP-107-single-source-tool-definitions.md`
before starting. Your CWD is the sprout repo root.

**Goal:** Make `ToolHandler.Definition()` the single source of truth for all
tool definitions. Eliminate the dual-maintenance problem where every tool is
defined twice (once as `ToolConfig`, once as `ToolHandler.Definition()`).

**Do NOT stop until all 4 phases are complete or you hit a genuinely
unrecoverable error. Budget warnings are not stop conditions.**

## Pre-Flight Checklist

Before starting any phase, read these files:
- `roadmap/SP-107-single-source-tool-definitions.md` — full spec
- `pkg/agent_tools/handler.go` — ToolHandler interface, ToolDefinition, ToolEnv
- `pkg/agent_tools/registry.go` — GetNewToolRegistry singleton
- `pkg/agent/tool_registrations.go` — legacy ToolConfig entries (will be deleted)
- `pkg/agent/tool_definitions_from_registry.go` — BuildToolDefinitions (will be rewritten)
- `pkg/agent/seed_tool_registry.go` — convertToSeedToolConfig (will be rewritten)
- `pkg/agent/conversation.go` — getOptimizedToolDefinitions (consumer)
- `pkg/agent/tool_security.go` — ExecuteTool (consumer, uses GetNewToolRegistry)

## Phase 1: Extend ToolHandler Interface (~1 day)

**Read first:** `pkg/agent_tools/handler.go`

The `ToolHandler` interface currently has: `Name()`, `Definition()`, `Validate()`, `Execute()`.
It needs optional metadata methods that `ToolConfig` currently carries:

```
Aliases() []string           → default: nil
Timeout() time.Duration      → default: 0 (use registry default of 5min)
MaxResultSize() int          → default: 0 (use registry default of 50KB)
SafeForParallel() bool       → default: false
Interactive() bool           → default: false
```

**Steps:**
1. Add these 5 methods to the `ToolHandler` interface in `pkg/agent_tools/handler.go`
2. Create a `BaseToolHandler` struct (or `ToolHandlerDefaults`) with default no-op implementations
3. Embed `BaseToolHandler` into every existing handler struct (32 handlers in `pkg/agent_tools/`)
4. For handlers that need non-default values, override the specific method:
   - `shellCommandHandler`: `Interactive() bool { return true }`, `Timeout() time.Duration { return 2 * time.Minute }`
   - `run_subagent` / `run_parallel_subagents` (when migrated in Phase 3): `Timeout() time.Duration { return 30 * time.Minute }`
   - `run_automate`: `Timeout() time.Duration { return 0 }` (no timeout)
   - `readFileHandler`: `SafeForParallel() bool { return true }`
   - `viewHistoryHandler`: `SafeForParallel() bool { return true }`
   - `askUserHandler`: `Interactive() bool { return true }`, `Timeout() time.Duration { return 10 * time.Minute }`
5. `make build-all` must pass
6. Commit: `feat: add metadata methods to ToolHandler interface (SP-107-1)`

**Acceptance:** Interface compiles. All handlers satisfy it. No behavior change.

## Phase 2: Build Canonical Tool List from Handlers (~1 day)

**Goal:** Rewrite `BuildToolDefinitions()` and `convertToSeedToolConfig()` to read
from `GetNewToolRegistry().All()` instead of `GetToolRegistry().GetAllToolConfigs()`.

**Steps:**
1. Add a `BuildToolDefinitionsFromHandlers()` function that iterates
   `tools.GetNewToolRegistry().All()`, calling each handler's `Definition()` and
   metadata methods, and converts to `[]api.Tool`. Place this in
   `pkg/agent_tools/tool_definitions.go` (new file) so it lives near the handler
   interface rather than in `pkg/agent/`.
2. Add `convertHandlerToSeedToolConfig()` that takes a `ToolHandler` and returns
   `core.ToolConfig`. Wire `Aliases()`, `Timeout()`, `MaxResultSize()`,
   `SafeForParallel()` from metadata methods.
3. Add a test that runs BOTH old `BuildToolDefinitions()` AND the new handler-based
   version and asserts the outputs are identical. This catches any drift.
4. Switch `getOptimizedToolDefinitions()` in `conversation.go` to use the new function.
5. Switch `newSeedToolRegistryWithPublisher()` in `seed_tool_registry.go` to iterate
   handlers instead of `ToolConfig` entries.
6. Run `make build-all` and `go test ./...` — all must pass.
7. Remove the parallel-output assertion test (it served its purpose).
8. Commit: `feat: build tool definitions from handler registry (SP-107-2)`

**Acceptance:** All tests pass. LLM tool list identical to before. Both execution
paths (seed + dual-dispatch) use the same handler source.

## Phase 3: Migrate Legacy-Only Tools to Handlers (~2 days)

**Goal:** Create handlers for the 16 tools that only exist in the legacy `ToolConfig`
registry. After this phase, every tool the LLM can call has a handler.

**Batch A — Simple CRUD tools (~1 hour each, 10 tools):**
Create a handler file for each in `pkg/agent_tools/`:
- `manage_memory` — `manage_memory_handler.go`
- `manage_settings` — `manage_settings_handler.go`
- `mcp_refresh` — `mcp_refresh_handler.go`
- `task_queue` — `task_queue_handler.go`
- `list_changes` — `list_changes_handler.go`
- `revert_my_changes` — `revert_my_changes_handler.go`
- `recover_file` — `recover_file_handler.go`
- `create_pull_request` — `create_pull_request_handler.go`
- `list_automate_workflows` — `list_automate_workflows_handler.go`
- `run_automate` — `run_automate_handler.go`

Each handler must:
- Implement `ToolHandler` interface (Name, Definition, Validate, Execute)
- Embed `BaseToolHandler` from Phase 1
- Extract the legacy handler func body from `tool_registrations.go` and adapt
  it to the `(ctx, ToolEnv, args) → (ToolResult, error)` signature
- The legacy handler is a `ToolHandler func(ctx, *Agent, args) (string, error)` —
  extract its logic, replace `*Agent` references with `ToolEnv` fields where possible
- Register in `AllTools()` in `pkg/agent_tools/all.go`

**Batch B — Subagent tools (~2 hours each, 2 tools):**
- `run_subagent` — needs `*Agent` for spawning subagents. Add a `SubagentRunner`
  interface to `ToolEnv` so the handler doesn't directly reference `*Agent`.
- `run_parallel_subagents` — same pattern.

**Batch C — Clarification tools (~1 hour, 2 tools):**
- `request_clarification` — subagent-to-parent communication
- `respond_clarification` — parent-to-subagent response

**Also fix:**
- `TodoRead`/`TodoWrite` case mismatch: the legacy registry has capitalized names
  but handlers use lowercase `todo_read`/`todo_write`. Standardize on lowercase
  by updating the legacy entries (they'll be deleted in Phase 4 anyway, but
  the LLM must see consistent names until then).
- Remove dead individual tools from `AllTools()` if confirmed dead:
  `save_memory`, `search_memories` (consolidated into `manage_memory`)
  `task_queue_add`, `task_queue_publish`, `task_queue_read` (consolidated into `task_queue`)

After EACH batch: `make build-all` + `go test ./...` must pass. Commit after each batch.

**Acceptance:** Every tool in the LLM list has a handler. `make build-all` and
`go test ./...` pass.

## Phase 4: Delete Legacy ToolConfig Registry (~0.5 days)

**Goal:** Remove all dual-definition code. Only the handler registry remains.

**Steps:**
1. Remove all `registry.RegisterTool(ToolConfig{...})` calls from
   `tool_registrations.go`. The file should be empty (or deleted if no
   remaining registrations).
2. Delete the `ToolRegistry` type, `ToolConfig` struct, `ParameterConfig` struct,
   `ToolHandler` func type, and `ToolHandlerWithImages` func type from
   `tool_definitions.go`. These are no longer needed.
3. Delete `tool_definitions_from_registry.go` (or rename the new handler-based
   function to take its place).
4. Delete `GetToolRegistry()` singleton and `newDefaultToolRegistry()` — the
   handler registry (`GetNewToolRegistry()`) is now canonical.
5. Update any remaining references to old types. Search for `ToolConfig`,
   `ParameterConfig`, `GetToolRegistry` across the codebase.
6. `make build-all` + `go test ./...` must pass.
7. Commit: `refactor: delete legacy ToolConfig registry, handlers are now single source of truth (SP-107-4)`

**Acceptance:** Only one definition per tool. No `ToolConfig` or `ParameterConfig`
types remain. Full test suite passes.

## Post-Implementation Checklist

After Phase 4 completes:
1. Run `grep -r "ToolConfig\|ParameterConfig\|GetToolRegistry" pkg/agent/` —
   should return nothing or only historical comments
2. Run `grep -r "TODO(SP-038)\|TODO(dual-dispatch)" pkg/` — should return nothing
3. Verify `embedding_index` and `semantic_search` appear in the LLM tool list
   (they were handler-only and invisible before — this is a feature, not a bug)
4. Update `TODO.md` — mark all SP-107 items `[x]`
5. Move `roadmap/SP-107-single-source-tool-definitions.md` to `roadmap/_completed/`
6. Update `roadmap/00-INDEX.md` — move SP-107 from Pending to Shipped

## Isolation Rules (CRITICAL)

- Focus ONLY on SP-107 work. Do not modify, revert, or delete unrelated changes.
- Do NOT run `git checkout`, `git restore`, `git reset`, or any command that
  alters the working tree beyond your own changes.
- If a build/test fails due to unrelated changes in the tree: pause 2 min, retry.
  Repeat up to 3 times. After 3 failures, mark the current phase as blocked
  and escalate.

## Git Safety Rules

- **NEVER force push** or `git push` at all
- **NEVER rebase**
- **NEVER `git reset --hard`** or amend commits
- **NEVER `git add .`** — stage only specific files
- Commit after each phase with conventional commit messages
- Use the commit tool with `notes` parameter, not `message`
