# SP-047: Embedding Store — Migrate JSONL to sqlite-vec

**Status:** 📋 Proposed
**Date:** 2026-05-21
**Depends on:** SP-016 (embedding index), SP-016b (expanded index)
**Priority:** Medium
**Effort Estimate:** ~2 weeks

## Problem

The embedding index stores vector records in a JSONL file (`index.jsonl`). Every record is serialized as a JSON object including its `[]float32` embedding vector. This has several problems:

1. **Slow startup** — The full JSONL file is parsed into memory on load. Every `VectorRecord` with its embedding array is deserialized from JSON. A 100K-record store at 256 dimensions is ~400MB of JSON text that must be parsed on every launch.

2. **Full-file rewrites** — `Store()` writes the entire record set back to disk after any change. Updating a single record means serializing and writing all records.

3. **High memory footprint** — All records live in a Go slice (`[]VectorRecord`) held in memory. No lazy loading or partial reads.

4. **No indexed queries** — Finding all records for a specific file requires scanning the entire slice. Top-K similarity search is brute-force over all loaded records.

5. **JSON serialization overhead** — A 256-dimension `[]float32` becomes ~2KB of JSON text (per record). Binary storage would be 1KB (4 bytes × 256).

## Research Summary

Evaluated 7 options for SQLite-based vector storage in Go:

| # | Project | CGO? | Stars | Verdict |
|---|---------|------|-------|---------|
| 1 | **sqlite-vec** (asg017) | Optional (WASM path = no CGO) | 14k | ⭐ **Best choice** |
| 2 | viant/sqlite-vec | No (pure Go) | ~15 | Fallback option |
| 3 | chromem-go | No | ~1k | Not SQLite-based |
| 4 | vectorlite | Yes (C++) | ~450 | No Go bindings |
| 5 | sqlite-vss | Yes | ~1k | Deprecated |
| 6 | sqlite-lembed | Yes | ~60 | Not a vector store |
| 7 | kelindar/search | No | ~750 | Wrong scope |

Full comparison: `/tmp/sprout_examples/sqlite-vector-comparison.md`

### Recommended: sqlite-vec via ncruces WASM path

**Go module:** `github.com/asg017/sqlite-vec-go-bindings/ncruces` + `github.com/ncruces/go-sqlite3`

**Why sqlite-vec:**
- 17ms queries on 1M × 128d vectors; our 100K × 256d use case = sub-millisecond
- ncruces WASM path = **zero CGO**, cross-compiles to linux/arm64, linux/amd64, darwin/arm64
- ~14k GitHub stars, used by LangChain, Mozilla Builders
- Dual MIT/Apache-2.0 license
- Vectors stored as compact binary BLOBs (no JSON overhead)
- SQL-native: vectors alongside metadata in a single queryable table
- Supports cosine similarity, L2, dot product
- HNSW and brute-force index options

**Fallback:** `viant/sqlite-vec` (pure Go via `modernc.org/sqlite`, brute-force only, Apache-2.0) — if WASM path has issues.

## Proposed Solution

### Architecture

Replace `JSONLFileStore` with a new `SQLiteVecStore` that implements the existing `VectorStore` interface. The interface remains unchanged — the store is an internal implementation detail.

```
┌─────────────────────────────┐
│       VectorStore interface  │  (unchanged: Store, LoadAll, Query, DeleteByFile, Size, Close)
├─────────────┬───────────────┤
│ JSONLFile   │ SQLiteVec     │  ← swap implementation
│ Store       │ Store         │
│ (current)   │ (new)         │
├─────────────┼───────────────┤
│ index.jsonl │ index.db      │
│ JSON text   │ SQLite + BLOB │
│ ~400MB      │ ~100MB        │
└─────────────┴───────────────┘
```

### SQLite Schema

```sql
CREATE VIRTUAL TABLE vec_records USING vec0(
  id TEXT PRIMARY KEY,
  file TEXT,
  name TEXT,
  signature TEXT,
  start_line INTEGER,
  end_line INTEGER,
  language TEXT,
  embedding float[256] distance_metric=cosine,
  hash TEXT,
  indexed_at TEXT,
  type TEXT,
  metadata TEXT  -- JSON-encoded for non-code-unit records
);

-- Fast lookups by file path
CREATE INDEX idx_records_file ON records(file);
```

### Migration Strategy

On first launch with sqlite-vec store:
1. Check for existing `index.jsonl` file
2. If found, load all records and insert into the new SQLite database
3. Rename `index.jsonl` to `index.jsonl.migrated` (keep for rollback)
4. Subsequent launches use SQLite directly

The migration is automatic, one-way, and non-destructive (old file preserved).

### Query Performance

**Current (JSONL brute-force in Go):**
- Load: Parse 400MB JSON → ~3-10 seconds
- Query: Iterate all records, cosine similarity each → O(N) in Go
- Store: Rewrite entire 400MB file → 1-5 seconds

**After (sqlite-vec):**
- Load: Open SQLite database → ~1ms (lazy, no full load)
- Query: `vec_records WHERE ... ORDER BY distance` → <1ms (HNSW) or ~5ms (brute-force in C)
- Store: Individual row upserts → ~1ms per record

## Implementation Plan

### Phase 1: SQLite Store Implementation

**New file:** `pkg/embedding/store_sqlite.go`

1. Implement `SQLiteVecStore` satisfying `VectorStore` interface
2. Use `ncruces/go-sqlite3` + `sqlite-vec-go-bindings/ncruces`
3. Store vectors as BLOBs in `vec0` virtual table
4. Implement `Query()` using SQL vector distance search
5. Implement `Store()` as SQLite upsert (INSERT OR REPLACE)
6. Implement `DeleteByFile()` as SQL DELETE with file=?
7. Implement `LoadAll()` as SQL SELECT (needed for incremental rebuild)

### Phase 2: Automatic Migration

**Modify:** `pkg/embedding/manager.go`

1. On init, check for legacy `index.jsonl` alongside expected `index.db`
2. If JSONL exists and DB doesn't: run migration
3. Load JSONL records → batch insert into SQLite
4. Rename JSONL to `.migrated` (preserve for safety)
5. Log migration stats (records migrated, time taken)

### Phase 3: Manager Integration

**Modify:** `pkg/embedding/manager.go`

1. Replace `NewJSONLFileStore` with `NewSQLiteVecStore` in init paths
2. Update manifest paths (`.index.db.manifest.json` instead of `.index.jsonl.manifest.json`)
3. Keep `JSONLFileStore` code in tree for migration reading and test use
4. Conversation stores (conversation_store.go) remain JSONL for now (separate concern)

### Phase 4: Testing

1. Unit tests for `SQLiteVecStore` against `VectorStore` interface
2. Migration test: create JSONL → migrate to SQLite → verify record count and query results
3. Compatibility test: ensure `IndexManager.BuildIndex` works identically with both stores
4. Performance benchmark: compare query time before/after
5. Existing `store_test.go` tests should pass with `SQLiteVecStore` as the implementation

### Phase 5: WASM Build Compatibility

1. Verify `ncruces/go-sqlite3` works in WASM builds (`cmd/wasm/`)
2. If WASM path isn't compatible, use build tags: `store_sqlite.go` for non-WASM, keep `store.go` (JSONL) for WASM
3. Build tag approach: `//go:build !wasm`

## Risks

| Risk | Mitigation |
|------|-----------|
| ncruces WASM path doesn't work on ARM | Fallback to `viant/sqlite-vec` (pure Go) or build-tag JSONL for ARM |
| Binary size increase (~5MB for WASM module) | Acceptable for a CLI tool; ~1% of current binary |
| Migration corrupts data | Old JSONL preserved as `.migrated`; migration is additive |
| sqlite-vec ABI changes break go bindings | Pin dependency version; test in CI |
| Query results differ from brute-force Go | HNSW is approximate; brute-force mode available as fallback |

## Open Questions

1. **Conversation stores** — Should `ConversationStore` also migrate to SQLite? It has different access patterns (append-heavy, less querying). Decision: defer to follow-up.
2. **ONNX store** — Both static and ONNX stores use separate JSONL files. Each gets its own SQLite database? Or one DB with two tables? Recommendation: separate DBs (simpler, matches current isolation).
3. **Connection pooling** — `ncruces/go-sqlite3` in WAL mode supports concurrent reads. Need to verify thread-safety for our goroutine access patterns.

## Success Criteria

- [ ] `SQLiteVecStore` implements `VectorStore` interface fully
- [ ] All existing embedding tests pass with the new store
- [ ] Migration from JSONL → SQLite is automatic and non-destructive
- [ ] `go build ./...` succeeds on linux/arm64, linux/amd64, darwin/arm64
- [ ] Query latency <5ms for 100K records (down from ~50ms brute-force in Go)
- [ ] Startup load time <100ms (down from 3-10 seconds JSON parsing)
- [ ] Binary size increase <10MB
