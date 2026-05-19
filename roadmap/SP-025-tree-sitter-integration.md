# SP-025: Tree-Sitter Integration — Real AST for Multi-Language Symbol Extraction

**Status:** ✅ Phases 1–3 complete; Phase 4 measurement complete (gate pending); Phase 5 pending
**Date:** 2026-05-15 (updated 2026-05-18)
**Depends on:** SP-024
**Priority:** Medium
**Effort Estimate:** ~2 weeks (Phase 5 ≈ 3–5 days)

## Current Implementation Status (audited 2026-05-18)

| Phase | Status | Evidence |
|-------|--------|----------|
| 1. Core AST Parser | ✅ Complete | `pkg/ast/` exists — `parser.go` (551 LOC), `symbols.go` (1021 LOC), `cache.go` (293 LOC); `github.com/odvcencio/gotreesitter v0.16.0` pinned in `go.mod`; supports Go, TS, JS, Python |
| 2. Repo Map | ✅ Complete | `pkg/agent_tools/repo_map.go` calls `ast.ParseFile()` for TS/JS/Python (lines 192–208); native `go/ast` retained for Go; simple-regex fallback on parse error |
| 3. Symbol Index | ✅ Complete | `pkg/index/symbols.go:65` calls `ast.ExtractSymbols()` for all supported languages; no regex fallback in current code (spec's "regex fallback" line is now outdated) |
| 4. WASM Integration | ✅ Measurement complete | `pkg/ast/browser_cache.go` (290 LOC) exists; `pkg/wasmshell/commands_ast.go` compiles. **Binary-size delta: +29.7M (4.3M → 34M).** See WASM Binary Size Impact section. Currently enabled in WASM builds (34M); recommendation to gate via build tag pending. |
| 5. Embedding Extractor Migration | ❌ Not started | `pkg/embedding/extractor_ts.go` (531 LOC, 9 regex patterns) and `extractor_py.go` (345 LOC) still use standalone regex; create symbol-coverage drift vs repo_map and index |

**Consequence of the partial migration:** there are now three parallel symbol-extraction paths (repo_map, index/symbols, embedding/extractor) that disagree in edge cases (TS arrow functions, decorated Python methods, multi-line signatures). Symbols indexed for semantic search may not appear in repo_map output and vice versa.



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

#### Phase 1: Core AST Parser — ✅ Complete

1. Add `odvcencio/gotreesitter` dependency to go.mod
2. Create `pkg/ast/parser.go` — unified parser with `ParseFile(path, content) (*ASTResult, error)`
3. Pre-compile grammar blobs for Go, TypeScript, JavaScript, Python
4. Implement `pkg/ast/symbols.go` — walk AST and extract top-level symbols with line numbers
5. Implement `pkg/ast/cache.go` — grammar blob caching (critical for WASM: cache in localStorage/IndexedDB)
6. Write tests: parse Go, TS, JS, Python files; verify symbol names, line numbers, scopes
7. Verify WASM build still works: `GOOS=js GOARCH=wasm go build ./cmd/wasm/`

#### Phase 2: Replace Regex in Repo Map — ✅ Complete

1. Update `pkg/agent_tools/repo_map.go` to use `pkg/ast` for Go, TS, JS, Python
2. Remove regex patterns from repo_map.go (Go AST stays as fallback for parse errors)
3. Update tests with expected line numbers from real AST
4. Verify build and tests pass

#### Phase 3: Replace Regex in Symbol Index — ✅ Complete

1. Update `pkg/index/symbols.go` to use `pkg/ast` for all supported languages
2. Remove regex patterns from symbols.go
3. Update symbol kinds to match AST node types
4. Verify `.sprout/symbols.json` output is correct
5. Verify build and tests pass

#### Phase 4: WASM Integration — ✅ Measurement complete (gate not yet applied)

1. Ensure `pkg/ast` compiles for GOOS=js GOARCH=wasm
2. Implement grammar blob caching for WASM (load once, cache in browser storage)
3. Add `pkg/ast` to WASM shell for code intelligence features
4. ✅ Verify WASM binary size impact and set acceptable threshold
5. Test WASM build: `make build-wasm`
6. ⚠️ Apply build-tag gate (`wasm_ast`) to exclude from default browser builds — pending

**Measurement results:** Baseline (no ast): 4.3M → With ast: 34M → Delta: +29.7M (+690%). See "WASM Binary Size Impact" section for threshold and recommendations.

#### Phase 5: Embedding Extractor (SP-016) — ❌ Not started (primary remaining work)

1. Wire `pkg/ast` into `pkg/embedding/extractor.go` for accurate code unit extraction
2. Extract function bodies (not just signatures) using AST scope information
3. Verify embedding index quality improves with accurate boundaries
4. This phase is deferred until SP-016 Phase 1 is implemented

## WASM Binary Size Impact (measured 2026-05-19)

| Configuration | Binary Size | Notes |
|--------------|-------------|-------|
| Without `pkg/ast` | **4.3M** | Baseline: no gotreesitter, no grammar blobs |
| With `pkg/ast` | **34M** | Current: gotreesitter runtime + grammar blobs for Go, TS, JS, Python |
| **Delta** | **+29.7M (+690%)** | Dominated by embedded grammar binaries |

### Threshold Recommendation
A 29.7M WASM binary is **not acceptable** for browser deployment. Loading ~30MB of WASM over a typical connection introduces significant latency and memory pressure, especially on mobile devices. The spec's estimate of ~500KB-1MB was incorrect because it didn't account for embedded grammar binaries.

**Acceptable threshold:** < 10M total WASM binary size (< ~5M delta from baseline).

**Path forward:**
1. **Grammar blob optimization** — Currently embeds full grammar binaries. Explore `gotreesitter` options to load grammars externally (fetch at runtime from CDN, cache in IndexedDB) rather than embedding in WASM binary.
2. **Lazy loading** — Only load grammar blobs for languages actually needed. Current implementation pre-loads all four at init.
3. **Build tags** — Gate `pkg/ast` behind a build tag (`wasm_ast`) so cloud/non-cloud WASM builds can differ.
4. **WASM-only build variant** — Consider a minimal `pkg/ast/wasm` that excludes unused language grammars.

**Verdict:** Measurement complete. The WASM integration currently produces a 34M binary (+29.7M delta). This exceeds the acceptable threshold and should **not ship to browser users** until the binary size is reduced. The recommendation is to gate `commands_ast.go` behind a build tag (`wasm_ast`) or apply `//go:build ignore` until a solution for the size delta is identified.

## Risks

| Risk | Mitigation |
|------|-----------|
| Binary size increase | Grammar blobs are pre-compiled (~100-500KB total for Go+TS+JS+Python). Acceptable for the accuracy gain in native builds. |
| WASM binary size | See "WASM Binary Size Impact" above. +29.7M delta is not acceptable for browser. |
| Parse errors on malformed files | Fall back to regex for files that fail to parse. Tree-sitter is error-tolerant but edge cases exist. |
| Build time impact | Grammar blobs are pre-compiled. No runtime compilation needed. |
| Breaking existing symbol index format | Maintain backward-compatible JSON schema. New fields (scope, parent) are optional. |
