# SP-107: Single-Source Tool Definitions — Eliminate Dual Maintenance

**Status:** 🔵 Proposed — Design complete. 4 phases.

Every tool the agent can invoke is defined twice: once as a `ToolConfig` in
`pkg/agent/tool_registrations.go` (for LLM-facing schema + seed execution) and
once as a `ToolHandler.Definition()` in `pkg/agent_tools/` (for dual-dispatch
execution). These two sources drift — `shell_command` had mismatched parameter
order, `wait_seconds` descriptions, and type (`number` vs `integer`) before the
2026-07-02 sync. Every future tool gets two definitions; every parameter change
requires two edits. The dual-path also hides bugs: 10 handler tools
(`embedding_index`, `semantic_search`, `list_directory`, etc.) are executable
but invisible to the LLM because `BuildToolDefinitions()` only iterates legacy
`ToolConfig` entries.

## Goal

Make `ToolHandler.Definition()` the single source of truth. `BuildToolDefinitions()`
and `convertToSeedToolConfig()` both read from the handler registry. Legacy
`ToolConfig` entries are deleted for every migrated tool.

## Current Architecture

```
LLM tool list                    Seed execution                    Dual-dispatch
──────────────                   ────────────────                  ──────────────
BuildToolDefinitions()      →   convertToSeedToolConfig()     →   ExecuteTool()
    ↓                               ↓                               ↓
ToolConfig entries              ToolConfig entries              handler.Definition()
(tool_registrations.go)        (tool_registrations.go)         (pkg/agent_tools/)
```

Two separate registries (`ToolRegistry` in `pkg/agent`, `ToolRegistry` in
`pkg/agent_tools`) with no shared interface.

## Target Architecture

```
LLM tool list + Seed execution + Dual-dispatch
─────────────────────────────────────────────
          handler.Definition() + metadata methods
          (pkg/agent_tools/ — single source)
```

## Gap Analysis

### Metadata only in `ToolConfig`, missing from `ToolHandler`

| Field | Purpose | Used by |
|---|---|---|
| `Aliases` | Alternative names (e.g. `git` tool: `op` → `operation`) | Seed param parsing |
| `Timeout` | Per-tool timeout override (default 5 min) | Seed execution |
| `MaxResultSize` | Per-tool result cap (default 50 KB) | Seed execution |
| `SafeForParallel` | Allow concurrent execution | Seed parallel dispatch |
| `Interactive` | Suppress activity spinner during tool execution | CLI UI |
| `HandlerImages` | Optional image-returning handler variant | Seed + legacy dispatch |

### Handler-only tools (missing from legacy → invisible to LLM)

| Tool | Notes |
|---|---|
| `embedding_index` | Embedding index management — LLM can never call this |
| `semantic_search` | Semantic code search — LLM can never call this |
| `list_directory` | Directory listing — LLM can never call this |
| `save_memory`, `search_memories` | Legacy individual memory tools (consolidated into `manage_memory`; may be dead) |
| `task_queue_add/publish/read` | Individual task ops (consolidated into `task_queue`; may be dead) |
| `todo_read`, `todo_write` | Case mismatch: legacy has `TodoRead`/`TodoWrite` (capital T); handler has lowercase |

### Legacy-only tools (no handler → only works via seed path)

| Tool | Notes |
|---|---|
| `run_subagent`, `run_parallel_subagents` | Need `*Agent` access; explicitly excluded from handler registry |
| `create_pull_request` | No handler exists |
| `list_automate_workflows`, `run_automate` | No handlers exist |
| `mcp_refresh` | No handler exists |
| `manage_settings` | No handler exists |
| `manage_memory` | Consolidated CRUD tool; no handler exists |
| `task_queue` | Consolidated CRUD tool; no handler exists |
| `request_clarification`, `respond_clarification` | Subagent communication tools; no handlers exist |
| `list_changes`, `revert_my_changes`, `recover_file` | Change tracker tools; no handlers exist |
| `TodoRead`, `TodoWrite` | Capitalized variants; handler has lowercase `todo_read`/`todo_write` |

## Phases

### Phase 1: Extend `ToolHandler` interface (1 day)

Add optional metadata methods to `ToolHandler` (in `pkg/agent_tools/handler.go`):

```go
type ToolHandler interface {
    Name() string
    Definition() ToolDefinition
    Validate(args map[string]any) error
    Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error)

    // Metadata — all optional with sensible defaults.
    Aliases() []string           // default: nil
    Timeout() time.Duration      // default: 0 (use registry default)
    MaxResultSize() int          // default: 0 (use registry default)
    SafeForParallel() bool       // default: false
    Interactive() bool           // default: false
}
```

Also add `ToolEnv.Agent interface{}` (or a focused interface) so `run_subagent`
and `run_parallel_subagents` can be migrated into handlers — they currently
need `*Agent` access.

**Acceptance:** Interface compiles. All 32 existing handlers satisfy it
(default methods via embedding or explicit no-op stubs). No behavior change.

### Phase 2: Build canonical tool list from handlers (1 day)

1. Add `BuildToolDefinitions()` equivalent that iterates `GetNewToolRegistry().All()`
   and converts `ToolDefinition` → `api.Tool`.
2. Add `convertHandlerToSeedToolConfig()` equivalent in `seed_tool_registry.go`
   that builds `core.ToolConfig` from a `ToolHandler`.
3. Run BOTH old and new paths in parallel, assert identical output in tests.
4. Switch `getOptimizedToolDefinitions()` and `newSeedToolRegistryWithPublisher()`
   to the new path. Delete the parallel-path assertions.

**Acceptance:** All existing tests pass. `make build-all` clean. LLM tool list
is identical to before (modulo fixes for handler-only tools now appearing).

### Phase 3: Migrate legacy-only tools to handlers (2 days)

Create handlers for the 16 tools that only exist in the legacy registry:

- **Batch A (simple CRUD, ~1 hour each):** `manage_memory`, `manage_settings`,
  `mcp_refresh`, `task_queue`, `list_changes`, `revert_my_changes`,
  `recover_file`, `create_pull_request`, `list_automate_workflows`,
  `run_automate`
- **Batch B (subagent, ~2 hours each):** `run_subagent`, `run_parallel_subagents`
  — requires `ToolEnv` extension for agent access
- **Batch C (clarification, ~1 hour):** `request_clarification`,
  `respond_clarification` — subagent-to-parent communication

Also fix the `TodoRead`/`todo_read` case mismatch — standardize on lowercase
in the legacy registry (the handlers already use lowercase).

For dead individual tools (`save_memory`, `search_memories`, `task_queue_add`,
`task_queue_publish`, `task_queue_read`), remove them from `AllTools()` —
they were consolidated into `manage_memory` and `task_queue`.

**Acceptance:** Every tool in the LLM list has a handler. Every handler is in
the LLM list. `make build-all` and `go test ./...` pass.

### Phase 4: Delete legacy ToolConfig registry (0.5 days)

1. Remove all `ToolConfig` registration calls from `tool_registrations.go`
   (keeping only the handler registration in `pkg/agent_tools/all.go`).
2. Delete `ToolRegistry` type and `BuildToolDefinitions()` old path.
3. Rename the handler-based `BuildToolDefinitions()` as the canonical one.
4. Delete `ParameterConfig`, `ToolConfig`, `ToolHandler` func type, and
   `ToolHandlerWithImages` from `tool_definitions.go`.
5. Update comments referencing the old registry.

**Acceptance:** Only one definition per tool. `make build-all` and full test
suite pass. No `TODO(SP-038)` or `TODO(dual-dispatch)` markers remain.

## Side-Effect Fixes

This migration corrects several bugs automatically:

1. **`embedding_index` and `semantic_search`** become visible to the LLM (they're
   handler-executable but `BuildToolDefinitions()` never included them).
2. **`list_directory`** becomes visible to the LLM.
3. **`TodoRead`/`todo_read` case mismatch** is resolved when the legacy entry
   is deleted and the handler name becomes canonical.
4. **Dead individual tools** (`save_memory`, `search_memories`, `task_queue_*`)
   are properly removed from the tool list if they're dead, or surfaced as
   discrete tools if they're still needed.

## Risk

- **Seed path regression:** `convertToSeedToolConfig()` currently wraps legacy
  `ToolHandler` funcs with `logToolExecution`/`handleToolError`/`postProcessResult`.
  Phase 2 must replicate this wrapping exactly.
- **Handler-only tools becoming visible:** `embedding_index` and
  `semantic_search` appearing in the LLM tool list may change agent behavior
  (the model may start using them). This is a feature, not a bug, but worth
  watching in E2E tests.
- **`*Agent` coupling:** `run_subagent`/`run_parallel_subagents` need
  significant refactoring to work through `ToolEnv` instead of `*Agent`.
  If this proves too invasive, these two tools can remain as a small legacy
  shim with explicit documentation.

## Artifacts

- spec: `roadmap/SP-107-single-source-tool-definitions.md` (this file)
- interface: `pkg/agent_tools/handler.go` — extended `ToolHandler` interface
- build: `pkg/agent/tool_definitions_from_registry.go` — rewritten to iterate handlers
- seed: `pkg/agent/seed_tool_registry.go` — `convertHandlerToSeedToolConfig`
- handlers: 16 new handler files in `pkg/agent_tools/`
- deletion: `pkg/agent/tool_registrations.go` — legacy entries removed
- deletion: `pkg/agent/tool_definitions.go` — `ToolConfig`/`ParameterConfig` types removed
