# SP-016b: Expanded Embedding Index — Full Workspace Semantic Search

**Status:** 📋 Proposed
**Depends on:** SP-016 (core embedding infrastructure, now complete)
**Priority:** Medium
**Effort Estimate:** ~2 weeks

## Background

SP-016 built the core embedding infrastructure: a static model provider (gemma-distilled, 128d, int8, ~0.05ms/embedding), code extractors for Go/Python/TypeScript, a JSONL vector store, and the index manager. The index currently only contains **extracted code units** (functions, methods, classes, types).

The static model refactor replaced the ONNX MiniLM (384d, ~280ms/embedding, requires CGO) with a pure-Go implementation that is ~5600x faster and has zero external dependencies. This speed makes it practical to index the entire workspace, not just extracted code symbols.

## Problem

The current index has three gaps:

1. **Non-code files are invisible.** READMEs, config files (YAML, TOML, JSON), CI/CD pipelines, makefiles, Dockerfiles, and other non-code artifacts are not indexed. Users searching for "how to deploy" or "database connection config" get no results.

2. **File-level search lacks granularity.** The index stores function-level embeddings but has no file-level embeddings. When searching for "where is authentication configured?", the user wants the config file, not a specific Go function.

3. **Duplicate detection is code-only.** Near-duplicate YAML configs, similar CI pipelines, or redundant Makefile targets go undetected.

## Proposed Solution

Expand the embedding index to include **all checked-in files** by adding a file-level indexing pass alongside the existing code-unit extractor. This enables four new capabilities:

### 1. Full-Workspace Semantic Search

Users can query across the entire codebase including documentation, configs, scripts, and CI/CD:

```
Query: "how is the build system configured?"
Results:
  0.85  Makefile
  0.82  .github/workflows/test.yml
  0.78  scripts/build-webui-embed.mjs
  0.71  docs/BUILD.md
```

### 2. Documentation Discovery

Find relevant documentation without knowing the exact file names:

```
Query: "how does the MCP protocol work?"
Results:
  0.89  docs/MCP.md
  0.82  README.md
  0.76  AGENTS.md (mentions MCP sections)
```

### 3. Configuration Search

Locate configuration across formats (YAML, TOML, JSON, shell env):

```
Query: "database connection settings"
Results:
  0.84  config/database.yaml
  0.79  docker-compose.yml
  0.72  .env.example
  0.68  pkg/db/config.go (code-level: env var names)
```

### 4. Non-Code Duplicate Detection

Detect near-duplicate configurations, CI pipelines, and documentation:

```
⚠ Potential duplicate detected:
  • .github/workflows/test.yml ↔ .github/workflows/lint.yml (similarity: 0.91)
    Both define nearly identical step sequences for checkout, setup, and run
```

## Architecture

The existing index pipeline gains a second indexing pass for full-file content:

```
                    ┌─────────────────────────────────┐
                    │       WalkCodeFiles              │
                    │   (expanded file type list)       │
                    └──────────────┬──────────────────┘
                                   │
                    ┌──────────────▼──────────────────┐
                    │         Code Extractor           │
                    │  (Go/Python/TS/JS → functions)   │
                    └──────────────┬──────────────────┘
                                   │
                    ┌──────────────▼──────────────────┐
                    │      Batch Embed + Store         │
                    │   (existing pipeline, unchanged)  │
                    └─────────────────────────────────┘

                    ┌─────────────────────────────────┐
                    │    File-Level Pass (NEW)         │
                    │                                  │
                    │  For each non-extracted file:     │
                    │  1. Truncate to max_file_len      │
                    │  2. Strip binary/header noise      │
                    │  3. Embed full file content        │
                    │  4. Store as file-level record     │
                    └─────────────────────────────────┘
```

### Index Record Types

Two types of records coexist in the JSONL store:

```json
// Code unit record (existing)
{
  "type": "code_unit",
  "id": "pkg/agent/tools.go:executeTool",
  "file": "pkg/agent/tools.go",
  "name": "executeTool",
  "start_line": 14, "end_line": 87,
  "language": "go",
  "embedding": [...]
}

// File-level record (NEW)
{
  "type": "file",
  "id": "config/database.yaml",
  "file": "config/database.yaml",
  "name": "database.yaml",
  "language": "yaml",
  "start_line": 0, "end_line": 120,
  "embedding": [...]
}
```

### File Types to Index

Files are grouped by indexing strategy:

| Strategy | File Types | Notes |
|----------|-----------|-------|
| **Code extraction** | `.go`, `.py`, `.ts`, `.tsx`, `.js`, `.jsx`, `.java`, `.rs`, `.c`, `.cpp` | Existing extractors parse into functions/classes/types |
| **Full-file embedding** | `.md`, `.rst`, `.txt`, `.yaml`, `.yml`, `.toml`, `.json`, `.xml`, `.html`, `.css`, `.sql`, `.sh`, `.bash`, `.zsh`, `.fish`, `.make`, `Makefile`, `Dockerfile`, `.dockerignore`, `.gitignore`, `.env`, `.env.example`, `.cfg`, `.ini`, `.conf`, `.properties`, `.gradle`, `.cmake` | Embed first N bytes of file content |
| **Skip** | Binary, media, archives, lock files, generated outputs | Existing ignore patterns + file extension blocklist |

### Performance Implications

The static model runs at ~0.05ms per embedding. For a typical workspace:

| Metric | Code Units Only (current) | Full Workspace (proposed) |
|--------|------------------------|------------------------|
| Files to index | ~900 Go files | ~2,500 files (code + configs + docs) |
| Records indexed | ~10,000 code units | ~10,000 code units + ~2,500 file-level |
| Index time | ~1.2s (10K × 0.05ms batching) | ~1.4s (+ ~0.1s for file-level) |
| Index on disk | ~37MB | ~39MB (+ ~2MB for file records) |
| Query latency | ~0.5ms (10K cosine comparisons) | ~0.6ms (12.5K comparisons) |

**The cost of full-workspace indexing is negligible** — the model is so fast that adding 2,500 file-level records increases index time by ~200ms and index size by ~2MB.

### Implementation

#### 1. File-Level Extractor

Create `pkg/embedding/extractor_file.go`:

```go
// FileExtractor produces file-level embeddings for non-code files.
type FileExtractor struct {
    maxFileBytes int  // Truncate files larger than this (default: 8000)
}

func (e *FileExtractor) Extract(path string, content []byte) ([]CodeUnit, error)
```

Strategy per file type:
- **Markdown (`.md`)**: Strip HTML tags, keep headings and body text. Good signal in heading structure.
- **YAML/TOML/JSON (`.yaml`, `.toml`, `.json`)**: Embed as-is (the keys and values carry semantic meaning).
- **Shell scripts (`.sh`, `.bash`, `Dockerfile`)**: Embed as-is (commands are self-documenting).
- **Config files (`.cfg`, `.ini`, `.env`)**: Embed as-is, skip comment-only lines.
- **Large files (>8KB)**: Truncate to first 8KB. The most meaningful content (headers, key definitions) is typically at the top.

#### 2. Index Manager Update

Modify `IndexManager.BuildIndex()` to run two passes:
1. Existing code-unit extraction pass (Go/Python/TypeScript extractors)
2. New file-level pass for all non-extracted files

File-level records use `type: "file"` to distinguish from code-unit records.

#### 3. Query API Update

Modify `GET /api/search/semantic` to return both code-unit and file-level results, merged and ranked by similarity:

```json
// Response includes both record types
{
  "results": [
    {
      "type": "code_unit",
      "file": "pkg/agent/tools.go",
      "name": "executeTool",
      "similarity": 0.91
    },
    {
      "type": "file",
      "file": "docs/MCP.md",
      "name": "MCP.md",
      "similarity": 0.85
    }
  ]
}
```

The webui search results distinguish between the two types visually:
- Code-unit results show function name → file:path:line (existing UI)
- File-level results show filename → file:path (new UI, simpler card)

#### 4. Duplicate Discovery via Semantic Search

Instead of a dedicated duplicate scan UI, surface potential duplicates contextually when semantic search results cluster around similar code across files. When multiple results from different files have similarity >0.90, show a "Possible duplicate" hint.

**Rationale**: A validation experiment (May 2026) showed the signal-to-noise ratio was too low for a dedicated scan (~6 useful findings from 61K pairs). Trivial getters, interface stubs, and anonymous closures dominated. The same fix (minimum line count filter) also improves edit-time duplicate warnings.

**Implementation**:
- Add trivial-function filtering (<5 lines) to `CheckFileForDuplicates` to reduce noise in edit-time warnings
- In the search API response, add a `duplicate_clusters` field that groups results by cross-file similarity
- In SearchView, show a hint when results cluster: "These results may share common code patterns"

## Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `pkg/embedding/extractor_file.go` | ✅ Create | File-level extractor for non-code files |
| `pkg/embedding/extractor_file_test.go` | ✅ Create | Tests for file extraction logic |
| `pkg/embedding/index.go` | ✅ Modify | Add file-level indexing pass to `BuildIndex()` |
| `pkg/embedding/index_test.go` | ✅ Modify | Add tests for file-level indexing |
| `pkg/embedding/check.go` | ✅ Modify | Add trivial-function filtering to reduce false positives |
| `pkg/webui/search_semantic_api.go` | ✅ Modify | Include file-level results in query response |
| `pkg/embedding/store.go` | ✅ Modify | Add `Type` field to `VectorRecord` for record discrimination |
| `pkg/embedding/ignore.go` | ✅ Modify | Expand accepted file types and skip directories |
| `webui/src/components/SearchView.tsx` | ✅ Modify | Render file-level results with appropriate UI |
| `webui/src/components/SearchView.css` | ✅ Modify | Styles for file-level result cards |
| `pkg/webui/search_semantic_api.go` | Modify | Add `duplicate_clusters` field to search response |
| `webui/src/components/SearchView.tsx` | Modify | Show duplicate hints when results cluster |

## Success Criteria

| Metric | Target |
|--------|--------|
| ✅ Query "how is the project configured?" | Returns config files in top-5 |
| ✅ Query "MCP protocol documentation" | Returns relevant .md files in top-3 |
| ✅ Query "Dockerfile build steps" | Returns Dockerfiles in top-3 |
| ✅ Index time increase | <500ms over current code-only indexing |
| ✅ Index size increase | <5MB over current code-only index |
| Trivial function filtering | Reduces false-positive warnings by >70% |
| Semantic search duplicate hints | Show duplicate hints when results cluster across files |
| ✅ No regressions | Existing code-level semantic search results unchanged |
