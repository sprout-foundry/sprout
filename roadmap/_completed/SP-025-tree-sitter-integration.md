# SP-025: Tree-Sitter Integration — Real AST for Multi-Language Symbol Extraction

**Status:** ✅ Shipped (all 5 phases complete 2026-06)

Sprout's symbol extraction and repo map generation used regex patterns,
which were brittle (false positives on string contents, missed edge cases
like nested generics, slow on large files). SP-025 replaced regex with
real AST parsing via tree-sitter (`gotreesitter` for pure-Go, WASM build
for browser). Phases: (1) core AST parser (`pkg/ast/`), (2) replace
regex in repo map (`pkg/agent_tools/repo_map.go`), (3) replace regex
in symbol index (`pkg/index/symbols.go`), (4) WASM integration for
browser-side parsing, (5) embedding extractor uses tree-sitter for
language-aware chunking. The orphaned regex variables were cleaned up in
the same patch series.

## Key decisions

- **`gotreesitter` for server-side**, WASM build for browser. Pure-Go
  avoids CGO so it builds on every platform sprout supports.
- **Cache parsed trees** (`pkg/ast/cache.go`) keyed by file mtime+size.
  Re-parsing on every embedding cycle was the original performance bug.
- **Language detection via file extension first**, content sniffing as
  fallback. Shebang line + extension is correct >99% of the time.
- **Incremental tree updates** on file change — re-parse only the
  changed file, propagate deltas to dependents.
- **Browser cache persistence** so each page reload doesn't re-parse
  from scratch (added in Phase 4 docs).

## Artifacts

- code: `pkg/ast/parser.go` — language detection + dispatch
- code: `pkg/ast/symbols.go` — symbol extraction from trees
- code: `pkg/ast/cache.go` — mtime-keyed AST cache
- code: `pkg/agent_tools/repo_map.go` — uses `pkg/ast` (regex removed)
- code: `pkg/index/symbols.go` — uses tree-sitter
- WASM: tree-sitter WASM build, `pkg/ast/wasm_*.go`

Full specification archived — see git history for original content.