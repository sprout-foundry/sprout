# SP-134 — GPU Acceleration for Native macOS Embeddings (MLX)

## Problem

Local embedding generation (Jina Code v2 via ONNX Runtime) is CPU-only
in the native Go binary. A cold full-repo index (~12k units) takes ~22 min on
an M1 Pro at ~9 units/s (batch=32, seq=128). The WebUI already gets GPU via a
browser WebGPU backend, but the native binary — the path the CLI and daemon
use — has no acceleration.

## Current State

**Model: Jina Code v2** (int8 quantized ONNX, 154 MB). The model was
EmbeddingGemma-300M when this spec was originally written; it was switched
to Jina Code v2 (SP-135) for code-specific retrieval quality. This changes
the GPU acceleration story entirely — see the superseded CoreML findings
below.

**Runtime plumbing (already exists).** The provider interface
(`pkg/embedding/provider.go:12`), shared provider cache
(`pkg/embedding/shared_runtime.go`), batch chunking with attention budget
(`pkg/embedding/onnx_embedding_provider.go:246-345`), and model downloader
(`pkg/embedding/model_downloader.go:341`) are all model-agnostic. A new
`EmbeddingProvider` implementation slots in cleanly.

**The ONNX int8 model is not CoreML-EP compatible** (measured 2026-08-07).
CoreML EP shatters the graph into 49 partitions because the int8-quantized
ops (Cast → ReduceMean → Sub → Pow → Sqrt → Div chains for LayerNorm) fall
back to CPU while attention/FFN go to GPU. The partition boundary overhead
makes CoreML **2.0× slower** than CPU (4.0 vs 7.8 units/s). See "Superseded:
CoreML EP Findings" at the bottom of this spec.

## Spike Results: MLX (2026-08-07)

A Python prototype implementing the Jina Code v2 forward pass in MLX
(Apple's Metal-backed array framework) was validated against ONNX CPU output
and benchmarked on an M1 Pro (16 GB, 8-core).

### Correctness

| Metric | Value |
|---|---|
| Average cosine similarity (MLX vs ONNX int8) | **0.994** |
| Drift source | fp16 safetensors vs int8 quantization (expected) |
| Gate (≥0.99) | **PASS** |

The 0.994 cosine is the expected gap between fp16 weights (safetensors) and
int8 dynamic quantization (ONNX). The ONNX int8 model itself drifts ~0.005
from fp16, so MLX fp16 is actually *closer* to the reference model than the
shipped ONNX int8 export. This drift does NOT invalidate the embedding index
— the Jina thresholds in `constants.go` (dup 0.65, search 0.30) have wide
separation margins (near-dup 0.756, unrelated 0.066).

### Throughput

| Workload | ONNX CPU | MLX Metal | Speedup |
|---|---|---|---|
| **batch=32, seq=128 (index build)** | 9.2 u/s | **88.8 u/s** | **9.7×** |
| batch=1, seq=30 (interactive query) | 22.5 u/s | 32.7 u/s | 1.5× |

**The 9.7× speedup on batch=32 is the critical number.** That's the actual
indexing workload: the Go provider chunks into batches of 32
(`defaultBatchChunkSize`), and real code units average ~128 tokens. A full
~12k-unit repo index drops from **~22 min to ~2.3 min**.

The batch=1 improvement is modest (1.5×) because GPU kernel launch overhead
dominates for tiny inputs. Interactive single-query latency is already
adequate (~30ms); the win is in batch throughput.

### Memory

MLX uses Apple's **unified memory** model — GPU and CPU share the same
physical RAM, no PCIe transfer. On a 16 GB machine the model weights (~307 MB
fp16 safetensors) plus working tensors for a batch=32 × seq=128 inference
create transient memory spikes. The existing Go attention budget
(`defaultBatchAttentionBudget = 8M cells`) already bounds this for ONNX;
the MLX provider must apply the same budget discipline (see Plan Phase 2).

## Plan

### Phase 1 — Python validation gate (DONE)

The prototype at `/tmp/sprout/mlx-proto-v3.py` implements the full forward
pass and verifies correctness. This phase is complete.

### Phase 2 — Go MLX provider (darwin/arm64 only)

**Goal:** implement `pkg/embedding/mlx_provider.go` behind
`//go:build darwin && arm64 && cgo`, providing a third
`EmbeddingProvider` alongside `ONNXEmbeddingProvider` and
`JinaONNXEmbeddingProvider`.

**Architecture:** MLX has no ONNX loader. The Go bindings
(`github.com/luxfi/mlx`) expose low-level array ops (`MatMul`, `Add`,
`Softmax`, etc.). The provider must implement the Jina Code v2 forward pass
in Go, translating the validated Python prototype. The operations needed:

- Token + token-type embedding lookup (Gather)
- Embedding LayerNorm
- 12× transformer layers, each:
  - Q/K/V linear projection + QK-LayerNorm
  - Multi-head attention with ALiBi positional bias
  - Post-attention LayerNorm (residual + dense)
  - GEGLU FFN (split, GELU gate, multiply, down-project)
  - Merge LayerNorm + FFN LayerNorm
- Mean pooling + L2 normalization (already in Go, reusable)

**Weight format:** switch from ONNX int8 to safetensors fp16. The downloader
gets a new `JinaCodeV2SafetensorsConfig()` (sibling to
`JinaCodeV2Config()`). `ModelHash()` changes → manifest invalidation →
one-time re-index. Note this in release notes.

**Memory safety:** the existing attention budget
(`defaultBatchAttentionBudget`) must carry over. MLX's unified memory means
GPU allocations count against system RAM — a batch=32 × seq=2048 attention
score tensor is ~4 GB. The budget caps this at ~400 MB by reducing batch
rows for long sequences. Additionally, the provider should:

1. Check available RAM before loading weights (`syscall.Sysctl("hw.memsize")`
   + reading vm_stat). On machines with <8 GB RAM, skip MLX and fall back
   to ONNX CPU.
2. Use `mx.eval()` to force eager evaluation per batch (MLX is lazy by
   default — unbounded lazy graph buildup is the memory spike source).
3. Cap concurrent batch sessions at 1 (no LRU needed — one model, one
   stream).

**Fallback chain:**
```
darwin/arm64 + ≥8GB RAM + MLX init OK → MLX Metal provider
everything else → ONNX CPU provider (existing)
```

Any MLX init error (shared library missing, Metal unavailable, etc.)
logs a warning and falls back to ONNX CPU. The `SPROUT_EMBEDDING_BACKEND=mlx|cpu`
env var forces a specific backend for debugging.

### Phase 3 — Concurrency and rollout

**Concurrency model (DONE):** The MLX provider uses the same `inferenceGate`
semaphore as the ONNX provider — a 2-permit pool that allows an interactive
query to proceed while a background index build is mid-flight. The `RWMutex`
guards only the `closed` flag (not inference), and `LockOSThread` +
per-call `DefaultGPUStream()` handle MLX's thread-local stream requirement.

This aligns with SP-136's daemon-first architecture: when the daemon lands,
the MLX provider runs inside the daemon process. CLI clients proxy through
the Unix socket (SP-136 Phase 3). The daemon's single inference gate
coordinates all clients, and the 307 MB model stays resident in one process.

**Rollout gate:** Default ON for darwin/arm64 + ≥8 GB RAM when safetensors
weights are present and MLX initializes. CPU otherwise (non-darwin, Intel
Mac, low-RAM, MLX lib missing). Exit criteria: `go test ./pkg/embedding/`,
index of this repo on M-series showing 2×+ speedup vs ONNX CPU,
`make build-all`, `sprout diag` reporting the active backend.

## Risks

1. **MLX Go binding maturity** (highest): `github.com/luxfi/mlx` is a
   community fork, not an Apple product. The Python `mlx` package is
   Apple-official and well-supported; the Go bindings lag. Risk mitigation:
   the Go port mirrors the validated Python prototype exactly; if the Go
   bindings are unstable, the Python subprocess bridge (Phase 2-alt) is the
   fallback.
2. **Memory spikes on low-RAM machines**: MLX lazy evaluation can build up
   large computation graphs before evaluating. `mx.eval()` per batch is the
   mitigation, plus the RAM check gate. The prototype showed transient
   spikes during batch=32 runs on a 16 GB machine — manageable but must be
   bounded.
3. **Vector compatibility**: MLX fp16 output drifts 0.006 from ONNX int8.
   The embedding index stores ONNX int8 vectors; switching to MLX invalidates
   all stores (new ModelHash). One-time ~2.3 min rebuild on M-series (the
   fast path), longer on Intel.
4. **fp16 vs int8 model size**: safetensors fp16 is 307 MB vs ONNX int8's
   154 MB. Both are loaded into unified memory. On 8 GB machines this is
   tight but feasible (the OS + sprout daemon use ~3 GB, leaving ~5 GB).
5. **ALiBi implementation correctness**: the ALiBi bias tensor depends on
   head slopes derived from a geometric sequence. An off-by-one in slope
   computation silently degrades retrieval quality. The Python prototype
   validates against ONNX output; the Go port must pass the same parity test.
6. **MLX version drift**: Apple updates `mlx` frequently. Pin the pip/Go
   module version and re-validate cosine parity on upgrade.

## Phase 2-alt: Python subprocess bridge

If the Go MLX bindings prove unstable, the fallback is a long-lived Python
subprocess that loads the model once and serves embeddings over
stdin/stdout (line-delimited JSON: text in → 768-dim float array out). The
Go provider spawns it once, pipes texts, reads embeddings.

Pros: uses Apple's official Python MLX (stable, well-tested), minimal Go
code. Cons: requires Python + mlx installed on the user's Mac, subprocess
management complexity, slightly higher latency for interactive queries.

The subprocess approach is the pragmatic fallback if Phase 2's Go bindings
hit showstopper bugs. It keeps the ONNX CPU path as the ultimate fallback.

## Superseded: CoreML EP Findings (2026-08-07)

CoreML Execution Provider was tested with the Jina Code v2 int8 ONNX model.
Session creation succeeded (1131/1254 nodes supported, 49 partitions), but
throughput was **2.0× slower than CPU** (4.0 vs 7.8 units/s at batch=1,
seq=256). The int8 quantization's LayerNorm chains (Cast → ReduceMean → Sub
→ Pow → Sqrt → Div) fall back to CPU, creating partition boundary overhead
that dominates the GPU compute time. CoreML EP is not viable for this model.
Results saved at `/tmp/sprout/coreml-spike-results.json`.

The original CoreML spec (targeting EmbeddingGemma-300M) is fully
superseded. Gemma's com.microsoft custom ops (MatMulNBits,
MultiHeadAttention, RotaryEmbedding, SimplifiedLayerNormalization) caused
graph shattering into 291 partitions. Jina's standard ops fixed the node
support problem but the int8 quantization reintroduced fragmentation through
unfused LayerNorm chains.

## Out of Scope

- Browser WebGPU (already shipping — keep as the low-effort GPU win).
- Linux/Windows GPU acceleration (no Metal; CUDA MLX is experimental).
- Non-embedding models (Gemma 3 2B generation) — separate concern.
- Re-exporting Jina with fused LayerNorm ops to improve CoreML compatibility
  — not worth the effort given MLX's 9.7× win.
