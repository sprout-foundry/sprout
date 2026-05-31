# SP-061: Remove Static Embedding Provider, Consolidate on ONNX

**Status:** 📋 Proposed  
**Depends on:** None (self-contained refactor)  
**Priority:** Medium — tech-debt reduction, binary shrink  
**Author:** Sprout Core Team  
**Created:** 2025-05-22

---

## Overview

The embedding system currently maintains two parallel providers:

1. **Static Provider** (`static_provider.go`, `static_tokenizer.go`, `static_loader.go`) — a pure-Go sentence-piece tokenizer + matrix multiply producing 128-dim vectors from a 55 MB model blob baked into the binary via `//go:embed`. Low quality, but works everywhere (including WASM without CGO).
2. **ONNX Provider** (`onnx_embedding_provider.go`, `onnx_runtime.go`, `onnx_tokenizer.go`) — wraps `onnxruntime_go` (CGO) using a downloaded Gemma model, producing 768-dim vectors. High quality, but requires CGO + native ONNX runtime on desktop and the `onnxruntime-web` JS bridge in WASM.

This spec proposes **removing the static provider entirely** and making ONNX the sole embedding provider.

## Motivation

- **Dual-store complexity**: `manager.go` maintains separate HNSW stores, provider fields, background build goroutines, and an RRF merge step to reconcile the two providers' results. This adds ~400 lines of branching logic and has been a source of subtle bugs (e.g., memories migrating to static store but not ONNX, requiring `BackfillMemoryONNX`).
- **Binary bloat**: The static model is 55 MB when embedded. Even when not embedded (the default build), the Go tokenizer/loader code adds ~1100 lines of rarely-touched code.
- **Quality**: The ONNX Gemma 768-dim embeddings produce measurably better retrieval quality than the 128-dim static model. All users who can run ONNX get better results.
- **WASM path is already solved**: The ONNX WASM bridge (`onnx_wasm.go`) provides a fully tested `syscall/js` → `onnxruntime-web` path. Tests in `onnx_wasm_bridge_test.go` cover embed, batch, promise rejection, and context cancellation. The static provider is no longer the only WASM option.
- **Maintenance burden**: Two providers means two code paths to test, debug, and document. Removing one halves the cognitive load for anyone touching the embedding stack.

## Risks & Mitigations

| Risk | Mitigation |
| ---- | ---------- |
| Users without ONNX runtime lose embedding support | Clear error messages via `errONNXNotAvailable`-style errors; UI shows "semantic search unavailable" with a link to docs |
| WASM builds lose zero-CGO path | ONNX WASM bridge (`onnx_wasm.go`) already exists and has test coverage; the host page loads `onnxruntime-web` as a JS dependency |
| ONNX init is slower than static (hundreds of ms) | Only happens once per process; index-building is already async; the old ONNX background goroutine can be simplified to a direct init |
| Existing static-indexed data becomes orphaned | Index directory is provider-tagged by `ModelHash()`; switching providers means the store detects a new model hash and triggers a reindex (existing behavior) |
| `compare_embed.go` users lose utility | The file is already build-tagged with `//go:build ignore` — not part of any build |

---

## Detailed Changes

### Phase 1: Delete Static Provider Files

Remove the following files entirely. These files have no remaining callers after this change:

| File | Lines | Purpose |
| ---- | ----- | ------- |
| `pkg/embedding/static_provider.go` | 233 | `StaticProvider` struct, `Embed`/`EmbedBatch`/`Close`, `staticModelData` var, `SetStaticModelData` |
| `pkg/embedding/static_tokenizer.go` | 612 | SentencePiece tokenizer implementation (vocab loading, BPE merges, tokenization) |
| `pkg/embedding/static_loader.go` | 440 | `StaticModel` struct, `LoadStaticModel`, format parsing (v1/v2/v3), `Validate` |
| `pkg/embedding/static_model_embed.go` | 17 | `//go:embed static_model.bin` (build tag: `!js && staticmodel`) |
| `pkg/embedding/static_model_nostub.go` | 12 | Empty stub when `staticmodel` tag absent (`!js && !staticmodel`) |
| `pkg/embedding/static_model_js_testmain_test.go` | 29 | WASM test bootstrap that loads `static_model.bin` from disk (`//go:build js`) |
| `pkg/embedding/static_test.go` | 160 | Static provider tests (`TestLoadStaticModel`, `TestNewStaticProvider`, etc.) |
| `pkg/embedding/compare_embed.go` | 29 | Debug utility comparing static vs ONNX tokenization (`//go:build ignore`) |
| `pkg/embedding/static_model.bin` | 55 MB | Model blob (if present; may be in `.gitignore`) |

**Total lines removed: ~1,532 + 55 MB binary.**

### Phase 2: Simplify `manager.go`

The embedding manager (`pkg/embedding/manager.go`, 943 lines) currently has dual-provider logic throughout. This phase consolidates to a single ONNX provider + single store.

#### 2.1 — Remove dual-provider struct fields

**Current:**
```go
type EmbeddingManager struct {
    // ...
    provider *StaticProvider
    onnxProvider *ONNXEmbeddingProvider

    store *ConversationStore       // static store
    onnxStore *ConversationStore   // ONNX store

    onnxBuilding   bool
    onnxBuildCancel context.CancelFunc
    onnxBuildWG    sync.WaitGroup
    onnxReady      chan struct{}
    onnxError      error
    onnxInitWG     sync.WaitGroup
    // ...
}
```

**After:**
```go
type EmbeddingManager struct {
    // ...
    provider EmbeddingProvider  // ONNXEmbeddingProvider (or nil if unavailable)

    store *ConversationStore  // single store, keyed by provider's ModelHash

    // Build state (single provider, no background goroutine needed)
    buildingIndex bool
    buildCancel   context.CancelFunc
    // ...
}
```

**Rationale:** With only one provider, there's no need for separate `onnx*` fields. The background ONNX init goroutine can be simplified — since ONNX is now the only path, init happens directly in `Init()` and fails fast if unavailable.

#### 2.2 — Simplify `initLocked()`

**Current flow (simplified):**
```
initLocked():
  1. Create StaticProvider (may fail if model not embedded)
  2. Load static ConversationStore
  3. Start background goroutine:
     a. Build ONNX provider
     b. Create ONNX ConversationStore (separate directory)
     c. Signal onnxReady
     d. Backfill ONNX store from static store
```

**New flow:**
```
initLocked():
  1. Attempt to create ONNX provider:
     - If ONNX runtime available → create ONNXEmbeddingProvider
     - If ONNX runtime unavailable → set provider = nil, return early with clear error logged
  2. Create single ConversationStore keyed by provider's ModelHash
  3. Return
```

**Key change:** Remove the background goroutine entirely. ONNX init is now synchronous in `Init()`. If it fails, `provider` is `nil` and the manager still functions (just returns "embedding provider unavailable" for queries).

#### 2.3 — Remove dual-store methods

**Current:**
```go
func (m *EmbeddingManager) GetConversationStore(ctx context.Context) (*ConversationStore, error)
func (m *EmbeddingManager) GetONNXConversationStore(ctx context.Context) (*ConversationStore, error)
```

**After:**
```go
func (m *EmbeddingManager) GetConversationStore(ctx context.Context) (*ConversationStore, error)
// Returns the single store. Error if provider not initialized.
```

Remove `GetONNXConversationStore` entirely. Callers that used it for dual-write (e.g., `EmbedMemory` calling both stores) now call `GetConversationStore` once.

#### 2.4 — Simplify `SearchSemantic`

**Current:** Queries both stores, RRF-merges results.

**After:** Queries the single store, returns results directly.

Remove `RRFMergeResults` function (29 lines) and its test suite (~70 lines in `manager_test.go`).

#### 2.5 — Simplify `Close()`

**Current:** Closes both static provider + ONNX provider, both stores.

**After:** Closes single provider + single store.

#### 2.6 — Update `ProviderInfo`

**Current:** Returns info for both providers with `isPrimary`/`isSecondary` flags.

**After:** Returns info for the single provider. Remove the "primary/secondary" distinction.

#### 2.7 — Index directory naming

The HNSW store directory is currently named after the provider's `ModelHash()`. Since we're switching from static to ONNX, the model hash changes, which means:
- Old static-indexed data will be left in its directory (orphaned on disk)
- New ONNX index will be created fresh in its directory
- This is existing behavior — no migration is needed, just a fresh index on first run

Add a debug log noting when a stale index directory is detected:
```go
debugLogf("[manager] provider model hash changed, existing index will be recreated")
```

### Phase 3: Update `cmd/wasm/embedding_funcs.go`

#### 3.1 — Remove `setStaticModel` JS export

**Current:**
```go
SproutWasm.setStaticModel = setStaticModelFunc  // line 78
```

Remove this export and the `setStaticModelFunc` function (lines 82-114).

#### 3.2 — Keep ONNX bridge exports

The following exports remain unchanged (they use the ONNX WASM bridge):
- `buildSemanticIndex`
- `searchSemantic`
- `clearSemanticIndex`
- `getEmbeddingStats`

The host page will need to load `onnxruntime-web` before calling these (existing behavior for ONNX-enabled builds).

### Phase 4: Update `pkg/agent/memory_embedding.go`

#### 4.1 — Remove dual-write in `EmbedMemory`

**Current:** Writes to static store, then best-effort writes to ONNX store.

**After:** Writes to single `GetConversationStore`.

Remove the `onnxStore.StoreMemory` block.

#### 4.2 — Remove dual-delete in `DeleteMemoryEmbedding`

**Current:** Deletes from static store, then best-effort deletes from ONNX store.

**After:** Deletes from single `GetConversationStore`.

#### 4.3 — Remove `BackfillMemoryONNX` entirely

This function exists solely to reconcile the two stores after migration. With a single store, it has no purpose. Delete the function and its references in `MigrateMemories`.

#### 4.4 — Simplify `MigrateMemories`

**Current:** Migrates to static store, then snapshots ONNX store for dual-write, then calls `BackfillMemoryONNX`.

**After:** Migrates to single `GetConversationStore`. Remove ONNX-specific logic.

### Phase 5: Update `pkg/agent/memory_search_handler.go`

#### 5.1 — Simplify `queryMemoriesAcrossStores`

**Current:** Queries static store, queries ONNX store, RRF-merges.

**After:** Queries single `GetConversationStore`, returns results directly.

#### 5.2 — Simplify `handleSearchMemoriesJSON`

No structural change needed — it already uses `GetConversationStore`. Just confirm it works with the simplified manager.

### Phase 6: Update Tests

#### 6.1 — `manager_test.go`

- **Remove** `TestEmbeddingManager_Init_SucceedsWithStaticProvider` (tests static-only init path)
- **Remove** `RRFMergeResults` test suite (~70 lines: `TestRRFMergeResults_EqualScores`, `TestRRFMergeResults_NilSlice`, `TestRRFMergeResults_EmptySlices`, etc.)
- **Update** `TestEmbeddingManager_IndexSize_Concurrent` to not reference static provider
- **Update** any tests that assert on dual-provider behavior

#### 6.2 — `memory_embedding_test.go`

- **Update** tests that check for dual-store writes to check single-store behavior
- **Remove** `BackfillMemoryONNX` test (if one exists)

#### 6.3 — `onnx_wasm_bridge_test.go`

No changes needed — these tests verify the ONNX WASM bridge, which is now the sole WASM path.

#### 6.4 — ONNX tests

Ensure `onnx_e2e_test.go`, `onnx_runtime_test.go`, `onnx_tokenizer_test.go` still pass with the simplified manager.

### Phase 7: Update Build System & Documentation

#### 7.1 — Build tags

Remove the `staticmodel` build tag from:
- `Makefile` / build scripts that reference `-tags staticmodel`
- `.gitignore` entries for `pkg/embedding/staticmodel/`

#### 7.2 — `docs/WASM_API.md`

- Remove the `setStaticModel` section (lines ~58-80)
- Update WASM setup documentation to reference `onnxruntime-web` as the required dependency
- Update error handling docs: "No `setStaticModel` → `searchSemantic` rejects" becomes "No ONNX runtime → `searchSemantic` returns error with message"

#### 7.3 — Error messages

Update any user-facing error messages that reference "static" or "onnx" to use provider-agnostic language:
- `"embedding provider not available — semantic search disabled"` (instead of `"static model data is empty"`)
- `"ONNX runtime not installed — see docs for setup"` (new, for native builds)

---

## Acceptance Criteria

### Functional

- [ ] `go build ./...` succeeds with no references to deleted files
- [ ] `go test ./pkg/embedding/...` passes (all non-static tests)
- [ ] `go test ./pkg/agent/...` passes (memory embedding + search tests)
- [ ] `go build -tags wasm ./cmd/wasm/` succeeds (WASM build without static model)
- [ ] Semantic search via ONNX provider still returns correct results for indexed files
- [ ] `GetConversationStore` returns the ONNX-backed store after init
- `GetONNXConversationStore` is removed; callers use `GetConversationStore`
- [ ] Memory embedding (save/delete/migrate) writes to the single store
- [ ] WASM builds work with `onnxruntime-web` bridge (manual verification via `browse_url` on localhost)

### Code Quality

- [ ] Zero references to `StaticProvider`, `StaticModel`, `staticModelData`, `SetStaticModelData` in remaining codebase
- [ ] Zero references to `onnxConvoStore`, `onnxStore`, `onnxProvider` in `manager.go`
- [ ] `RRFMergeResults` removed (or kept in a separate non-embedding package if needed elsewhere — it's not)
- [ ] `BackfillMemoryONNX` removed from `pkg/agent/memory_embedding.go`
- [ ] `staticmodel` build tag removed from all build configs
- [ ] Line count of `pkg/embedding/` reduced by ~1,500 lines

### Documentation

- [ ] `docs/WASM_API.md` updated: `setStaticModel` section removed, ONNX bridge documented as the path
- [ ] Error messages updated to be provider-agnostic
- [ ] `docs/WASM_API.md` error section updated to reflect ONNX-only path

### Migration

- [ ] Existing index directories are handled gracefully (new model hash = fresh index, old data left on disk until user clears)
- [ ] No data loss: conversations/memories already indexed will be re-indexed on next run

---



---

## Estimated Effort

| Task | Effort |
| ---- | ------ |
| Phase 1: Delete static files | 0.5h |
| Phase 2: Simplify `manager.go` | 2h |
| Phase 3: Update WASM exports | 0.5h |
| Phase 4: Update `memory_embedding.go` | 1h |
| Phase 5: Update `memory_search_handler.go` | 0.5h |
| Phase 6: Update tests | 1h |
| Phase 7: Build system + docs | 1h |
| QA: Manual testing (desktop + WASM) | 1.5h |
| **Total** | **~8h** |
