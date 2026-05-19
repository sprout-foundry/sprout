# SP-034: ONNX Embedding Provider — EmbeddingGemma 300m

**Status:** 🚧 In Progress
**Depends on:** SP-016 (embedding infrastructure, complete)
**Priority:** Medium
**Effort Estimate:** ~1 week

## Problem

The static embedding provider (model2vec distillation, 55MB, 384d) works well for **code-vs-code duplicate detection** (similarity 0.99+) but fails for **natural language semantic search** and **memory/conversation retrieval**:

| Query Type | Static Provider | Needed |
|---|---|---|
| Code vs near-identical code | 0.997 ✅ | 0.90+ |
| NL query → code function | 0.09 ❌ | 0.50+ |
| NL query → stored memory | ~0.1 ❌ | 0.50+ |
| NL query → documentation | ~0.1 ❌ | 0.50+ |

The agent's `semantic_search` tool returns empty results for most natural language queries. Memory embedding stores correctly but retrieval is effectively broken.

## Proposed Solution

Add a second embedding provider alongside the static one, using a real transformer model (EmbeddingGemma 300m) via ONNX Runtime. Route embedding calls to the appropriate provider based on use case:

- **Static provider** → duplicate detection, code-vs-code (fast, always available)
- **ONNX provider** → semantic search, memory, conversation turns (slower, quality)

## Model: Google EmbeddingGemma 300m

| Property | Value |
|---|---|
| Parameters | 308M |
| Output dimensions | 768 (MRL: 512, 256, 128 possible) |
| Context length | 2048 tokens |
| Languages | 100+ (trained on code + multilingual text) |
| MTEB English v2 | 68.36 (768d) — class-leading for size |
| MTEB Code v1 | 68.76 (768d) — excellent code understanding |
| License | Gemma Terms of Use — permissive, commercial use allowed |
| ONNX available | Yes — `onnx-community/embeddinggemma-300m-ONNX` on HuggingFace |

### License Summary

EmbeddingGemma is under the [Gemma Terms of Use](https://ai.google.dev/gemma/terms), NOT Apache 2.0 (that's Gemma 4 only). Key points:

- **Commercial use allowed** — explicitly permitted
- **Redistribution allowed** — must include Gemma Terms of Use notice and Section 3.2 use restrictions in any downstream license
- **Model Derivatives allowed** — ONNX conversion, quantization, fine-tuning all permitted
- **No royalty or payment** required
- **Must comply with Prohibited Use Policy** — standard restrictions (no harm, no illegal use, no surveillance)
- **Disclaimer of warranty** — standard "as is"

For sprout: we distribute the ONNX model file as a first-run download. We include the Gemma Terms of Use notice in the download directory and link in our documentation. No issues.

### ONNX Conversion

**No GPU needed for conversion.** The ONNX-converted model already exists on HuggingFace:

```
onnx-community/embeddinggemma-300m-ONNX
```

Available formats:
| Variant | Size | Quality |
|---|---|---|
| FP32 ONNX | ~1.2 GB | Full precision |
| Q8 (quantized) | ~350 MB | Negligible quality loss |
| Q4 (quantized) | ~180 MB | Minor quality loss |

We ship **Q8** as the default (best quality/size tradeoff). FP32 is available as an opt-in for maximum quality.

**No conversion step needed on our end.** The model is pre-converted and hosted on HuggingFace.

### Expected Inference Speed

| Hardware | Latency per embedding | Throughput |
|---|---|---|
| **Modern CPU (M-series, Ryzen 9)** | 5–15 ms | 60–200 embed/s |
| **Mid-range CPU (i7, 2+ years old)** | 15–30 ms | 30–65 embed/s |
| **Older CPU (i5, 4+ years old)** | 30–80 ms | 12–33 embed/s |
| **With GPU (via ONNX Runtime)** | 1–5 ms | 200–1000 embed/s |
| **EdgeTPU** | <22 ms (Google's benchmark) | 45+ embed/s |

For comparison, the static provider runs at **0.05 ms** — but it can't do semantic search.

**Practical impact on sprout:**
- Semantic search query (1 embedding + topK scan): **15–30ms** on modern hardware
- Memory embedding on save (1 embedding): **15–30ms**
- Full index rebuild of 10,000 code units: **150–300 seconds** (use static provider instead)
- Background indexing of conversation turns: trivial, happens async

The ONNX provider is **only used for interactive queries** (search, memory retrieval) where 15-30ms latency is perfectly acceptable. The static provider continues to handle all bulk operations.

## Architecture

### Generic ONNX Runtime (Shared Infrastructure)

The ONNX runtime is a **shared base** that any ONNX-based model can use. This enables both embedding models (EmbeddingGemma 300m) and generation models (Gemma 3 2B for local LLM tasks) to share the same download, tokenizer, and runtime infrastructure.

```
┌─────────────────────────────────────────────────────┐
│               ONNXRuntime (shared)                   │
│                                                      │
│  • onnxruntime_go environment                        │
│  • Platform detection (CPU/GPU layers)               │
│  • Model download + checksum validation              │
│  • Tokenizer loading (tokenizer.json)                │
│  • Session creation from .onnx files                 │
└──────────────────┬──────────────────────────────────┘
                   │
     ┌─────────────┴─────────────┐
     │                           │
┌────▼─────┐            ┌────────▼──────┐
│ Embedding │ (now)     │ LocalLLM      │ (future)
│ Provider  │           │ Provider      │
│           │           │               │
│ Embedding │           │ Gemma 3 2B    │
│ Gemma 300m│           │ for:          │
│           │           │ • Actionable  │
│ (onnx-    │           │   summaries   │
│ comm/     │           │ • Intent      │
│ embedding │           │   classif.    │
│ gemma-    │           │ • Commit      │
│ 300m-     │           │   messages    │
│ ONNX)     │           │               │
└───────────┘           └───────────────┘
```

**Key design decision:** `ONNXRuntime` owns the environment, model download, and session creation. Embedding and generation providers each instantiate their own `ONNXRuntime` — they are independent consumers. The runtime extracts the ONNX shared library into a shared location so it's extracted only once.

### `pkg/embedding/onnx_runtime.go` — Shared Base

```go
type ONNXRuntime struct {
    env       *onnxruntime.Env       // shared ONNX environment
    modelDir  string                  // ~/.config/sprout/models/
    runtimeDir string                 // ~/.config/sprout/models/onnxruntime/
}

func NewONNXRuntime() *ONNXRuntime
func (r *ONNXRuntime) ExtractSharedLib() error
func (r *ONNXRuntime) NewSession(ctx context.Context, modelPath string, opts ...SessionOption) (*onnxruntime.InferenceSession, error)
func (r *ONNXRuntime) Close() error
```

### `pkg/embedding/model_downloader.go` — Generic Downloader

```go
type ModelConfig struct {
    Name          string   // e.g. "embeddinggemma-300m-q8"
    ModelURL      string   // HuggingFace download URL
    TokenizerURL  string   // tokenizer.json URL
    ModelHash     string   // SHA256 of model file
    TokenizerHash string   // SHA256 of tokenizer
    ModelSize     int64    // expected download size
}

type ModelDownloader struct {
    modelDir string // ~/.config/sprout/models/
}

func (d *ModelDownloader) Download(ctx context.Context, cfg ModelConfig, progress func(float64)) error
func (d *ModelDownloader) GetModelPath(name string) string
func (d *ModelDownloader) GetTokenizerPath(name string) string
func (d *ModelDownloader) IsDownloaded(name string) bool
```

### `pkg/embedding/onnx_embedding_provider.go` — EmbeddingGemma Provider

```go
type ONNXEmbeddingProvider struct {
    runtime   *ONNXRuntime
    session   *onnxruntime.InferenceSession
    tokenizer *GemmaTokenizer
    dims      int
    modelHash string
    closed    bool
}

func NewONNXEmbeddingProvider(ctx context.Context, modelPath, tokenizerPath string, dims int) (*ONNXEmbeddingProvider, error)
func (p *ONNXEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error)
func (p *ONNXEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
func (p *ONNXEmbeddingProvider) Dimensions() int
func (p *ONNXEmbeddingProvider) Name() string
func (p *ONNXEmbeddingProvider) ModelHash() string
func (p *ONNXEmbeddingProvider) Close() error
```

### `pkg/embedding/onnx_tokenizer.go` — Gemma Tokenizer

EmbeddingGemma uses a BPE tokenizer (not SentencePiece). The `tokenizer.json` from the ONNX export uses the standard HuggingFace format with Byte-level BPE. We need a Go implementation that parses this format and tokenizes text.

```go
type GemmaTokenizer struct {
    vocab       map[string]int32
    merges      []string
    unkToken    string
    vocabSize   int
    modelHash   string
}

func NewGemmaTokenizer(path string) (*GemmaTokenizer, error)
func (t *GemmaTokenizer) Encode(text string) []int32
func (t *GemmaTokenizer) Decode(tokens []int32) string
```

### Dual-Provider Design

```
┌───────────────────────────────────────────────────┐
│                EmbeddingManager                    │
│                                                   │
│  ┌─────────────┐          ┌──────────────────┐   │
│  │  StaticProvider       │  ONNXEmbeddingProvider│
│  │  (pure Go)            │  (onnxruntime_go)  │   │
│  │  384d, 55MB           │  768d, ~350MB      │   │
│  │  0.05ms/embed         │  15-30ms/embed     │   │
│  └──────┬───────────────┘  └────────┬────────┘   │
│         │                          │              │
│  Used for:                 Used for:             │
│  • Duplicate detection     • Semantic search     │
│  • Bulk index build        • Memory embedding    │
│  • File-level indexing     • Conversation turns  │
│                            • Drift detection      │
└───────────────────────────────────────────────────┘
```

### Provider Selection

Each call site explicitly chooses which provider to use:

```go
// Duplicate detection → static (fast, good enough for code-vs-code)
mgr.CheckDuplicates(ctx, path, content)  // uses StaticProvider

// Semantic search → ONNX (quality)
mgr.QuerySimilar(ctx, "user authentication", 5, 0.3)  // uses ONNX provider when available

// Memory embedding → ONNX (quality)
mgr.GetConversationStore(ctx)  // uses ONNX provider when available
```

The `EmbeddingManager` holds both providers. The static provider is always available (embedded in binary). The ONNX provider is lazy-initialized on first semantic query or explicit activation.

### Vector Store Separation

Static and ONNX providers produce **incompatible vectors** (different dimensions, different embedding spaces). They **must not share the same JSONL store**.

Solution: separate store files in the same directory:

```
~/.config/sprout/embeddings/
  index.jsonl          ← static provider records (existing)
  index_onnx.jsonl     ← ONNX provider records (new)
  conversation_turns.jsonl  ← ONNX provider records (existing, migrated)
```

The `ConversationStore` and memory embedding are migrated to the ONNX provider when available. The static provider's store remains for duplicate detection.

### Configuration

```json
{
  "embedding_index": {
    "enabled": true,
    "auto_index": true,
    "provider": "auto",
    "onnx": {
      "model_url": "https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main/onnx/model_q8.onnx",
      "tokenizer_url": "https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main/tokenizer.json",
      "dimensions": 256,
      "gpu_layers": 0
    }
  }
}
```

**`provider` values:**
- `"auto"` — use ONNX if available, fall back to static
- `"static"` — always use static provider (current behavior)
- `"onnx"` — require ONNX, error if not downloaded

**`dimensions`**: Uses Matryoshka Representation Learning to truncate 768d → 256d. This cuts storage by 3x with minimal quality loss (MTEB: 68.36 → 66.89).

## Implementation

### Phase 1: ONNX Runtime Base (~1 day)

**`pkg/embedding/onnx_runtime.go`** (~80 lines)

Shared ONNX runtime infrastructure:

- Creates and manages `onnxruntime.Env` (singleton, shared across providers)
- Extracts platform-specific shared library to `~/.config/sprout/models/onnxruntime/`
- Creates `InferenceSession` from model path with configurable intra-op threads and GPU layers
- Provides `Close()` to release environment resources

**`pkg/embedding/model_downloader.go`** (~100 lines)

Generic model downloader:

- Downloads from configurable HuggingFace URLs
- Stores to `~/.config/sprout/models/{model_name}/`
- SHA256 checksum validation on download
- Progress callback for UI integration
- Skip download if files already present and valid
- Writes `NOTICE` file with license info

### Phase 2: Embedding Provider (~2 days)

**`pkg/embedding/onnx_tokenizer.go`** (~200 lines)

Pure Go BPE tokenizer for Gemma models:

- Parses `tokenizer.json` (HuggingFace format)
- Byte-level BPE with Gemma-specific byte encoding
- `Encode(text) []int32` and `Decode(tokens) string`
- No external dependencies

**`pkg/embedding/onnx_embedding_provider.go`** (~120 lines)

EmbeddingGemma provider:

- Implements `EmbeddingProvider` interface
- Uses ONNX runtime for inference
- Tokenizes text, runs model, extracts mean-pooled, L2-normalized embedding
- Applies EmbeddingGemma prompt prefixes (e.g., `"task: search result | query: "`)
- MRL dimension truncation (768 → 256)
- Graceful error handling — falls back to static on ONNX failure

### Phase 3: Manager Integration (~1 day)

**`pkg/embedding/manager.go`** (modify existing)

Add ONNX provider as second provider:

```go
type EmbeddingManager struct {
    // existing fields...
    onnxProvider *ONNXEmbeddingProvider  // nil until activated
    onnxStore    *JSONLFileStore          // separate store for ONNX vectors
    onnxReady    bool
}
```

Key changes:
- `QuerySimilar()` routes to ONNX provider when available (and provider != "static")
- `GetConversationStore()` uses ONNX provider when available
- `CheckDuplicates()` continues using static provider
- `BuildIndex()` continues using static provider
- New `ActivateONNX(ctx)` method to download + initialize ONNX provider
- Lazy init: ONNX provider loads on first call that needs it
- `Close()` cleans up ONNX provider if present

### Phase 4: Prompt Prefixes (~0.5 days)

EmbeddingGemma requires task-specific prompt prefixes for best results:

```go
const (
    QueryPrefix    = "task: search result | query: "
    DocumentPrefix = "title: none | text: "
    CodeQueryPrefix = "task: code retrieval | query: "
)
```

These are prepended to text before embedding. The static provider doesn't need them — only the ONNX provider uses them.

### Phase 5: Config (~1 day)

- Add `ONNX` section to `EmbeddingIndexConfig` in `config_types.go`
- Wire into `NewConfig()` defaults in `config.go`

## Files to Create/Modify

| File | Action | Lines | Description |
|---|---|---|---|
| `pkg/embedding/onnx_runtime.go` | Create | ~80 | Shared ONNX runtime base |
| `pkg/embedding/model_downloader.go` | Create | ~100 | Generic model downloader |
| `pkg/embedding/onnx_tokenizer.go` | Create | ~200 | Gemma BPE tokenizer |
| `pkg/embedding/onnx_embedding_provider.go` | Create | ~120 | EmbeddingGemma provider |
| `pkg/embedding/manager.go` | Modify | ~100 | Dual-provider routing, ONNX lazy init |
| `pkg/embedding/onnx_runtime_test.go` | Create | ~50 | ONNX runtime tests |
| `pkg/embedding/onnx_tokenizer_test.go` | Create | ~80 | Tokenizer tests with known inputs |
| `pkg/configuration/config_types.go` | Modify | ~15 | Add ONNX config section |
| `pkg/configuration/config.go` | Modify | ~10 | Default ONNX config values |
| `go.mod` | Modify | ~3 | Add `github.com/yalue/onnxruntime_go` |

**Total new code: ~750 lines**

## Dependencies

### New Go Dependency

**`github.com/yalue/onnxruntime_go`** — Go bindings for ONNX Runtime
- No CGO build-time dependency — extracts platform-specific shared library at runtime
- Bundled shared libs for Linux, macOS, Windows (~15MB each)
- MIT licensed
- Well-maintained, used in production

### ONNX Runtime Shared Library

The `onnxruntime_go` library extracts the appropriate shared library at runtime:

```
~/.config/sprout/models/onnxruntime/
  libonnxruntime.so       (Linux x86_64, ~15MB)
  libonnxruntime.dylib    (macOS arm64, ~15MB)
  onnxruntime.dll         (Windows, ~15MB)
```

Extracted on first use from the Go library's embedded assets. No separate download needed.

### Model Files

Downloaded from HuggingFace on first use:

```
~/.config/sprout/models/embeddinggemma-300m-q8/
  model.onnx          (~350MB, Q8 quantized)
  model.onnx_data     (weights, if external data format)
  tokenizer.json      (~13MB, HuggingFace format)
  NOTICE               (Gemma Terms of Use notice)
```

## Success Criteria

| Criterion | Target |
|---|---|
| ONNX runtime extracts shared lib without error | Linux/macOS/Windows |
| Model downloader completes and validates checksum | SHA256 matches |
| Tokenizer encodes known text to expected tokens | Compare against HuggingFace output |
| Embedding provider produces non-zero vectors | All dimensions non-trivial |
| Semantic search: "user authentication" → returns `AuthenticateUser` | Similarity ≥ 0.50 |
| Semantic search: "error handling" → returns error-related functions | Similarity ≥ 0.40 |
| Memory retrieval: "database connection" → returns stored DB memory | Similarity ≥ 0.50 |
| First-run download completes | < 2 minutes on broadband |
| ONNX provider init time (after download) | < 3 seconds |
| Single embedding latency (modern CPU) | < 30ms |
| No regression in duplicate detection | Static provider unchanged |
| All existing tests pass | `go test ./pkg/embedding/...` green |
| Build succeeds | `make build-all` passes |

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| ONNX Runtime shared lib not available for platform | `yalue/onnxruntime_go` bundles for all major platforms; fallback to static provider |
| 350MB download too large | Opt-in via config; show size before download; Q4 variant (~180MB) available |
| Tokenizer incompatibility | Validate against known outputs during download; fall back to static on mismatch |
| ONNX provider crash/onnxruntime error | Graceful error handling; static provider always available |
| Gemma license change | Pin model version; license is permissive for commercial use |

## Future: Local LLM Provider

The ONNX runtime and model downloader are designed to support a future local LLM provider. When needed for low-latency tasks (actionable summaries, intent classification, commit messages), a `LocalLLMProvider` can wrap Gemma 3 2B ONNX using the same infrastructure:

- **Same runtime**: `ONNXRuntime` creates sessions for any ONNX model
- **Same tokenizer**: Gemma 3 2B uses the same BPE tokenizer format
- **Same downloader**: `ModelDownloader` handles the model download
- **New file**: `pkg/inference/local_llm.go` — wraps `ONNXEmbeddingProvider` pattern for generation

Target models:
| Model | Q8 Size | CPU Latency (first token) | Use Case |
|---|---|---|---|
| **Gemma 3 2B** | ~1.1 GB | 50-150ms | Summarization, classification, intent |
| **Phi-3.5 Mini (3.8B)** | ~2 GB | 100-250ms | Higher quality alternative |
| **Qwen2.5 1.5B** | ~900 MB | 40-100ms | Lightweight, fast |

This is **not implemented now** — just the architectural foundation is in place.
