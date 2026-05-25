# SP-025: Tree-Sitter Integration — Real AST for Multi-Language Symbol Extraction

**Status:** 📋 Spec  
**Date:** 2026-05-15  
**Depends on:** SP-024  
**Priority:** Medium  
**Effort Estimate:** ~2 weeks

## Problem

The codebase has two parallel regex-based symbol extraction systems that produce inaccurate results for complex code:

1. **Repo map** (`pkg/agent_tools/repo_map.go`) — Go AST for Go, regex for TS/JS/Python
2. **Symbol index** (`pkg/index/symbols.go`) — regex for ALL languages (Go, Python, TS/JS, Ruby, PHP, Rust, Java)

Regex-based extraction has fundamental limitations:

- Cannot handle multi-line declarations (function signatures spanning 3+ lines)
- Cannot distinguish top-level from nested symbols (methods inside classes)
- Cannot parse generic types, template parameters, or complex type signatures
- False positives from strings/comments containing code-like patterns
- Cannot extract accurate symbol scopes (start/end lines)
- Cannot extract decorator metadata, access modifiers, or visibility

The Go AST parser in repo_map works well for Go, but TS/JS/Python still use regex. The symbol index uses regex for everything including Go.

Additionally, SP-016 (embedding index) specifies tree-sitter for TS/Python extraction but hasn't implemented it yet. The embedding extractor needs accurate function boundaries for embedding code units.

## Solution

Integrate `odvcencio/gotreesitter` (pure Go tree-sitter, no CGO) as the unified AST parser for all languages. This enables:

- Accurate symbol extraction for Go, TypeScript, JavaScript, Python, Rust, Java, and 100+ languages
- Correct line numbers, scopes, and symbol kinds
- Reusable across repo_map, symbol index, embedding extractor, and WASM shell
- Works in all build targets (native Go, WASM, no CGO)

**Library:** `github.com/odvcencio/gotreesitter` (v0.16.0, 486 stars, pure Go, WASM-compatible)

### Architecture

```
pkg/ast/
  parser.go        — Unified AST parser using gotreesitter
  symbols.go       — Symbol extraction from AST nodes
  cache.go         — Grammar blob caching (WASM-aware)
```

The parser is a shared dependency consumed by:

- `pkg/agent_tools/repo_map.go` (replaces regex for TS/JS/Python)
- `pkg/index/symbols.go` (replaces regex for all languages)
- `pkg/embedding/extractor.go` (SP-016, future)
- `pkg/wasmshell/` (WASM build, for code intelligence in browser)

### Migration Matrix

| Area | Current | Target | Phase |
|------|---------|--------|-------|
| `pkg/agent_tools/repo_map.go` | Go AST + regex (TS/JS/Python) | gotreesitter for all, Go AST fallback | 2 |
| `pkg/index/symbols.go` | Regex for all 8 languages | gotreesitter for all | 3 |
| `pkg/embedding/extractor.go` | Not implemented yet | gotreesitter for all | 5 |
| `pkg/wasmshell/` | No AST parsing | gotreesitter for code intelligence | 4 |
| `pkg/lsp/semantic/go_adapter.go` | Regex for compiler output | No change (parses tool output, not source) | N/A |

### Implementation Plan

#### Phase 1: Core AST Parser

1. Add `odvcencio/gotreesitter` dependency to go.mod
2. Create `pkg/ast/parser.go` — unified parser with `ParseFile(path, content) (*ASTResult, error)`
3. Pre-compile grammar blobs for Go, TypeScript, JavaScript, Python
4. Implement `pkg/ast/symbols.go` — walk AST and extract top-level symbols with line numbers
5. Implement `pkg/ast/cache.go` — grammar blob caching (critical for WASM: cache in localStorage/IndexedDB)
6. Write tests: parse Go, TS, JS, Python files; verify symbol names, line numbers, scopes
7. Verify WASM build still works: `GOOS=js GOARCH=wasm go build ./cmd/wasm/`

#### Phase 2: Replace Regex in Repo Map

1. Update `pkg/agent_tools/repo_map.go` to use `pkg/ast` for Go, TS, JS, Python
2. Remove regex patterns from repo_map.go (Go AST stays as fallback for parse errors)
3. Update tests with expected line numbers from real AST
4. Verify build and tests pass

#### Phase 3: Replace Regex in Symbol Index

1. Update `pkg/index/symbols.go` to use `pkg/ast` for all supported languages
2. Remove regex patterns from symbols.go
3. Update symbol kinds to match AST node types
4. Verify `.sprout/symbols.json` output is correct
5. Verify build and tests pass

#### Phase 4: WASM Integration

1. Ensure `pkg/ast` compiles for GOOS=js GOARCH=wasm
2. Implement grammar blob caching for WASM (load once, cache in browser storage)
3. Add `pkg/ast` to WASM shell for code intelligence features
4. Verify WASM binary size impact and set acceptable threshold
5. Test WASM build: `make build-wasm`

#### Phase 5: Embedding Extractor (SP-016)

1. Wire `pkg/ast` into `pkg/embedding/extractor.go` for accurate code unit extraction
2. Extract function bodies (not just signatures) using AST scope information
3. Verify embedding index quality improves with accurate boundaries
4. This phase is deferred until SP-016 Phase 1 is implemented

## Risks

| Risk | Mitigation |
|------|-----------|
| Binary size increase | Grammar blobs are pre-compiled (~100-500KB total for Go+TS+JS+Python). Acceptable for the accuracy gain. |
| WASM binary size | Gotreesitter runtime + grammar blobs add ~500KB-1MB to WASM binary. Cache grammar blobs in browser storage after first load. Measured: ~61MB stripped (acceptable — well within 100MB threshold). |
| Parse errors on malformed files | Fall back to regex for files that fail to parse. Tree-sitter is error-tolerant but edge cases exist. |
| Build time impact | Grammar blobs are pre-compiled. No runtime compilation needed. |
| Breaking existing symbol index format | Maintain backward-compatible JSON schema. New fields (scope, parent) are optional. |
