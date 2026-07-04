# SP-107 Code Intelligence Graph — Audit

**Date:** 2026-07-04
**Status:** ⚠️ Implementation sound, but feature is unreachable

## Summary

The code intelligence graph is a **well-built engine with no ignition key**. The design is
clean, the tests are thorough, and all building blocks are in place. But the graph database is
never populated — none of the three agent tools (`get_callers`, `get_callees`, `find_dead_code`)
can produce results, and the `repo_map` graph-store fast path is dead code.

## What's Built (all good)

### Core data model (`pkg/codegraph/`)
- `SQLiteStore` with three tables (nodes, edges, files), WAL mode, foreign keys, proper indexes
- Clean `Store` interface with full CRUD + query surface
- Cross-file edge resolution via intra-transaction DB lookup fallback
- Incremental indexing via mtime comparison (`GetStaleFiles` → `IndexChangedFiles`)
- Deleted file detection and cleanup (`deleteFileFromIndex`, `removeDeletedFiles`)

### Query layer
- `QueryCallers` / `QueryCallees` — correct JOIN-based call relationship queries
- `FindDeadCode` — thoughtful exclusions: `init`, `main`, exported, `Test*`, non-func kinds,
  Go methods (parenthesized receiver detection)
- `QueryAllNodes` — ordered by file_path/line, used by `repo_map` fast path

### Code extraction (`pkg/agent_tools/repo_map.go`)
- `ExtractCallsAndSymbols` — unified entry point, dispatches to Go AST or tree-sitter
- `extractGoSymbolsASTWithEdges` — `go/ast` based: `FuncDecl` → symbols, `CallExpr` → edges
- `extractSymbolsAndEdgesViaTreeSitter` — TS/JS/Python via `pkg/ast`
- Edge extraction covers Go, TypeScript, JavaScript, and Python

### Agent tool registration
- `get_callers`, `get_callees`, `find_dead_code` registered in `AllTools()` at
  `pkg/agent_tools/all.go`
- All three implement `ToolHandler` with `Definition()`, `Validate()`, `Execute()`,
  and metadata methods (`Aliases`, `Timeout`, `MaxResultSize`, `SafeForParallel`, `Interactive`)
- Graceful degradation: "not been indexed yet" message when DB doesn't exist

### repo_map integration
- `GenerateRepoMap` tries graph store first (near-instant on warm cache), falls back to
  filesystem walk

### Tests
- **28 tests** in `pkg/codegraph/codegraph_test.go` — CRUD, replacement, cross-file edges,
  staleness, dead code filters, IndexAll directory traversal (symlinks, hidden dirs,
  unsupported extensions, subdirectories, cleanup)
- **19+ tests** in `pkg/agent_tools/repo_map_edges_test.go` — Go/TS/JS/Python extraction,
  edge cases
- **14 handler tests** in `pkg/agent/tool_handlers_codegraph_test.go` — format, registration,
  "not indexed" path, output format
- All tests pass: `go test ./pkg/codegraph/... ./pkg/agent_tools/...`

## Critical Gap: Graph is Never Populated

`IndexAll` and `IndexChangedFiles` accept a `FileParser func(path, content) ([]Symbol, []Edge, error)`
callback, but **no production code ever constructs a `FileParser` and calls these methods**.

```go
// Exists but never called outside tests:
store.IndexAll(ctx, myParser)
store.IndexChangedFiles(ctx, myParser)
```

Consequences:
- `get_callers` / `get_callees` / `find_dead_code` always return "not been indexed yet"
- `repo_map` graph-store fast path is dead code (always falls through to filesystem walk)
- The `.sprout/codegraph.db` file is never created (unless a test runs)

**What's needed:** A single wiring point that:
1. Creates a `FileParser` from `ExtractCallsAndSymbols` (adapting `SymbolWithEdges` return
   type to match `([]Symbol, []Edge, error)`)
2. Calls `store.IndexAll(ctx, parser)` for initial build
3. Hooks `IndexChangedFiles` into a trigger (CLI command, Makefile target, `embedding_index`
   integration, or background worker)

## Hidden Bug: Edge Qualified-Name Mismatch (ALL Languages)

`ToCodegraphSymbols` (repo_map.go:351) transforms symbol names to qualified form
(e.g., `"pkg/app.run"`) but returns edges as-is from the extractor. The extractors
use bare/unqualified names:

- **Go**: `goFuncName()` produces `"func run"`, `"func (*Server).Start"` — includes
  `"func "` prefix and has no package path.
- **TS/JS/Python**: `extractCalls()` uses `sym.Name` which is just the function name
  (e.g., `"greet"`, `"main"`).

When `SQLiteStore.IndexFile` resolves edge `SourceQualifiedName` / `TargetQualifiedName`
to node IDs, it queries `nodes WHERE qualified_name = ?`. The qualified names in the
nodes table are prefixed (e.g., `"pkg/app.run"`) — edges with bare names (e.g.,
`"func run"`) do NOT match → **edges are silently dropped**.

This bug is latent (the graph is never populated) but must be fixed before indexing
goes live. The fix belongs in `ToCodegraphSymbols`: transform edge names to match
the qualified name format.

### Smaller Gaps

### 1. `find_dead_code` `directory` parameter is a no-op
**CONFIRMED.** The handler (`codegraph_handler.go` and `tool_handlers_codegraph.go`)
accepts `directory` as an optional parameter but discards it:
```go
_ = args["directory"]
```
The store-level query doesn't support directory filtering. Either implement it or drop
the parameter.

### 2. Duplicate handler code — `pkg/agent/` copy IS dead code
**CONFIRMED.** Both `pkg/agent_tools/codegraph_handler.go` and `pkg/agent/tool_handlers_codegraph.go`
contain nearly identical logic. The `agent_tools` versions implement `ToolHandler` and
are registered in `AllTools()`. The `agent/` versions are ONLY called from their own test
file (`tool_handlers_codegraph_test.go`). No production code references them. They are
dead code — remove them.

### 3. No `store_wasm.go` parity
The WASM stub returns errors for every method. If the webui ever ships code indexing,
it won't work. Low priority (webui doesn't run code extraction).

### 4. Python extraction depth
Tree-sitter captures direct `call_expression` nodes but misses decorator-invoked
functions, `__init__`-time registrations, and dynamic dispatch patterns. Low priority
(pragmatic first pass).

## Verdict

The feature is ~95% done — architecturally sound, well-tested, properly registered —
but ships dead because nothing triggers graph construction. One wiring point would
make it fully operational. The gap is small and well-bounded; the fix should take
under an hour.
