# SP-107 — Code Intelligence Graph

**Status:** ✅ Implemented (auto-build on first query + embedding_index integration; qualified-name edge bug fixed in `repo_map.go:ToCodegraphSymbols`)

## Problem

Today the agent explores codebases by reading files one at a time. Every structural question — "who calls this function?", "is this dead code?", "what's the call chain from A to B?" — triggers a cascade of `grep → read_file → grep → read_file` that burns tens of thousands of tokens. Across five structural questions, file-by-file exploration consumed ~412,000 tokens vs ~3,400 from a knowledge graph.

The pieces to fix this already exist in sprout but aren't connected:

- `pkg/agent_tools/repo_map.go` — flat symbol listing (Go via `go/ast`, TS/JS/Python via tree-sitter). Regenerated from scratch every call. No edges between symbols.
- `pkg/ast/` — tree-sitter integration for multi-language AST parsing.
- `pkg/lsp/` — real language server support (gopls, pyright, etc.) for type resolution and diagnostics.
- `pkg/embedding/` — ONNX embedding store, currently pointed at conversation turns.

## Goal

Upgrade `repo_map` into a persistent, incrementally-indexed knowledge graph of symbols and call edges, with agent-queryable tools that make code navigation a sub-millisecond lookup instead of a multi-file grep cycle.

## Non-Goals

- **Cypher query engine** — our agent queries via structured Go tools, not a query language.
- **Clone/similarity detection** (MinHash/LSH) — niche; falls out of semantic search anyway.
- **Cross-service linking** (gRPC/GraphQL/route detection) — narrow audience.
- **3D graph visualization** — could add a WebUI graph view later if there's demand.
- **158 language grammars** — we support Go, TS, JS, Python well. Add languages incrementally.

---

## Phase 1 — Persistent Call Graph (core, ~3-4 days)

The backbone: nodes (symbols) + call edges + persistence + incremental indexing + agent tools.

### 1a. Graph store (`pkg/codegraph/`)

New package. SQLite-backed persistent store at `.sprout/codegraph.db`.

**Schema (initial):**

```
nodes:    id, qualified_name, display_name, file_path, line, kind (func/type/var/const/iface), language, file_mtime
edges:    id, source_node_id, target_node_id, edge_type (calls/defined_in/imports), line
files:    path, mtime, symbol_count, last_indexed
```

**API:**

```go
type Store interface {
    IndexFile(ctx context.Context, path string, symbols []Symbol, edges []Edge) error
    QueryCallers(ctx context.Context, qualifiedName string) ([]Symbol, error)
    QueryCallees(ctx context.Context, qualifiedName string) ([]Symbol, error)
    FindDeadCode(ctx context.Context) ([]Symbol, error)
    GetStaleFiles(ctx context.Context) ([]string, error)
    Stats() GraphStats
}
```

### 1b. Call-edge extraction

Extend the existing tree-sitter extraction in `pkg/ast/` and Go `go/ast` extraction in `repo_map.go` to also extract call relationships:

- **Go**: walk `ast.CallExpr` nodes → resolve `fn.Name.Name` to a target symbol. Cross-file via package-level symbol table.
- **TS/JS**: walk tree-sitter `call_expression` nodes → resolve callee name. Cross-file via import graph.
- **Python**: walk tree-sitter `call` nodes → resolve dotted names.

Each extracted call becomes a `calls` edge: `source (caller function) → target (callee function)`.

### 1c. Incremental indexing

Replace `repo_map`'s full-walk-every-time with:

1. Compare each file's mtime vs. `files.last_indexed`.
2. Re-parse only changed files.
3. Delete old nodes/edges for changed files, insert new ones.
4. Update `files.last_indexed`.

The first index is a full walk (same speed as today). Subsequent calls are near-instant (only changed files re-parsed).

### 1d. Agent tools

Three new tools registered in the tool registry:

| Tool | Input | Output |
|------|-------|--------|
| `get_callers` | `qualified_name` (string) | List of functions that call this symbol, with file:line |
| `get_callees` | `qualified_name` (string) | List of functions this symbol calls, with file:line |
| `find_dead_code` | `directory` (optional) | Functions with zero inbound call edges (excluding entry points: `main()`, route handlers, exported API, `init()`) |

### 1e. Upgrade `repo_map`

`repo_map` becomes a thin wrapper over the graph store — query the persisted graph instead of walking the filesystem. Same output format (backward compatible), but instant on subsequent calls.

**Acceptance criteria:**
- `.sprout/codegraph.db` persists across sessions
- `get_callers`/`get_callees` return correct results for Go code in this repo
- `find_dead_code` runs in < 100ms
- `repo_map` output is unchanged but returns in < 50ms on warm cache
- Incremental index: touching one file re-indexes only that file

---

## Phase 2 — Type-Aware Resolution via LSP (~2-3 days)

Resolve textual call edges to their actual definitions using our existing LSP infrastructure (`pkg/lsp/`). This is better than reimplementing a type resolver — we use real language servers.

### 2a. LSP-backed edge resolution

When extracting call edges, query the running LSP for `textDocument/definition` at each call site:

- `user.profile.display_name()` → resolves to `Profile.display_name` declared three modules away
- Cross-package calls resolve to the actual package, not just the textual name
- Method calls on interface values resolve to the interface method declaration

### 2b. `RESOLVED_CALLS` edges

Store both raw textual edges (`calls`) and LSP-resolved edges (`resolved_calls`). Query tools prefer resolved edges when available.

### 2c. Interface satisfaction

For Go: track which types satisfy which interfaces. `get_callers` on an interface method includes all concrete implementations.

**Acceptance criteria:**
- LSP resolution runs for Go (gopls) on this repo
- `get_callers` on an interface method returns all implementations
- Cross-package calls resolve correctly

---

## Phase 3 — Semantic Code Search (~1-2 days)

Point the existing embedding infrastructure at code symbols.

### 3a. Embed code symbols

Extend `pkg/embedding/` to index code symbols (function signatures + doc comments) alongside conversation turns. Each symbol gets a vector embedding stored in the existing embedding store.

### 3b. `search_code` tool

New tool:

| Tool | Input | Output |
|------|-------|--------|
| `search_code` | `semantic_query` (string) | Symbols whose meaning matches the query, ranked by similarity — finds `publish` when you search `send` |

**Acceptance criteria:**
- `search_code("retry backoff")` finds the exponential backoff implementation
- Embeddings run on-device via existing ONNX runtime (no API key)
- Results include file:line and function signature

---

## What we deliberately skip

| Capability | Why skip |
|---|---|
| Cypher queries | Agent uses structured Go tools, not query languages |
| Clone detection (MinHash/LSH) | Niche; semantic search covers most cases |
| Cross-service linking | Narrow audience; complex to implement |
| 158 languages | We support 4 well (Go, TS, JS, Python); add incrementally |
| 3D graph visualization | Could add WebUI view later |

## Dependencies

- `pkg/ast/` (tree-sitter) — already exists (SP-025)
- `pkg/lsp/` — already exists (SP-054)
- `pkg/embedding/` — already exists (SP-016b)
- `pkg/agent_tools/repo_map.go` — already exists

## Integration points

- `repo_map` tool — upgraded to read from graph store
- Tool registry — three new tools (`get_callers`, `get_callees`, `find_dead_code`)
- `.sprout/codegraph.db` — new persistent artifact (gitignored)
- WebUI — no changes required for Phase 1; graph stats could appear in context panel later
