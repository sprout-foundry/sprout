# SP-134 — GPU Acceleration for Native macOS Embeddings (CoreML EP)

## Problem

Local embedding generation (EmbeddingGemma-300M via ONNX Runtime) is CPU-only
in the native Go binary. A cold full-repo index (~12k units) takes ~30 min on
an M1 Pro at ~6.5 units/s (q4). The WebUI already gets GPU via a browser
WebGPU backend (`webui/src/services/onnxEmbeddingProvider.ts:46`, `~10x faster`
claim; default flipped to `webgpu` in
`webui/src/services/embeddingBackendController.ts:91`), but the native binary —
the path the CLI and daemon use — has no acceleration.

A prior attempt (documented at `pkg/embedding/embedding_models.go:345-349`,
2026-08-05) called `AppendExecutionProviderCoreMLV2` on the dynamic-seq export:
CoreML EP shattered the graph into 291 partitions of 1767 nodes and compile
exhausted memory before producing one embedding. This spec scopes whether
fixed-shape (seq-length-bucketed) exports + CoreML EP can make native macOS
GPU/ANE acceleration work.
## Current State

**Runtime plumbing (already exists).** `github.com/yalue/onnxruntime_go
v1.30.1` → ORT 1.30.1 exposes `SessionOptions.AppendExecutionProviderCoreMLV2(
options map[string]string)` (`onnxruntime_go.go:1957`), implemented via the
generic `SessionOptionsAppendExecutionProvider(o, "CoreML", ...)` gated by a
dlsym'd pointer for `OrtSessionOptionsAppendExecutionProvider_CoreML`
(`setup_env.go:46-58`, darwin/ios only). Unsupported platforms return
`ORT_NOT_IMPLEMENTED` ("...library does not support CoreML") — a clean fallback
signal, not a crash. The binding has no build tags, so sprout must gate with
`//go:build darwin` itself; the installed `onnxruntime_arm64.dylib` exports the
old symbol, so V2 is callable today.
`pkg/embedding/onnx_runtime.go:364-379` `SessionOption` carries only
threads/arena/mem-pattern; `newSessionOptions` (~line 390) is where a CoreML
option applies. `onnx_embedding_provider.go` creates one
`DynamicAdvancedSession` per provider (line ~87), maxSeqLen 2048, batch chunks
≤ 32 rows (`defaultBatchChunkSize`), attention budget `8<<20` cells,
`ModelHash() = fileSHA256(modelPath)` (lines 522-546). `manifest.go:122`
invalidates every index when `ModelHash` changes; `model_downloader.go:341`
pins the q4 files by sha256.
**The exports defeat CoreML EP even after bucketing (measured locally).**
Parsed `model_q4.onnx` / `model_fp16.onnx` (onnx 1.22; IR v10, opset 21 +
com.microsoft opset 1; inputs `input_ids[batch_size, sequence_length]`,
`attention_mask[batch_size, total_sequence_length]` — two *different* symbolic
params):

| op (domain) | q4 | fp16 | CoreML EP builder? |
|---|---|---|---|
| MatMulNBits (com.microsoft) | 170 | 0 | ❌ |
| MultiHeadAttention (com.microsoft) | 24 | 24 | ❌ |
| RotaryEmbedding (com.microsoft) | 48 | 48 | ❌ |
| GatherBlockQuantized (com.microsoft) | 1 | 0 | ❌ |
| SimplifiedLayerNormalization (ai.onnx) | 145 | 145 | ❌ (only `LayerNormalization` registered) |
| Equal / Expand (ai.onnx) | 48/49 | 48/49 | ❌ |

Ground truth is `op_builder_factory.cc` in
`onnxruntime/core/providers/coreml/builders/` (main): registers
LayerNormalization but **not** SimplifiedLayerNormalization, and none of the
com.microsoft ops above. The fp16 graph's unsupported nodes (~314 of 1613
after `onnxsim`, check_n=0) sit **inside every transformer block** (attention +
layernorm), so even a fixed-shape fp16 export partitions at every layer and
dispatches only FFN matmuls to ANE/GPU — the same shattering failure mode.
Additional blockers:
- **Vocab 262,144** (tokenizer.json) → embed table `[262144, 768]`. A
  Gemma-class export was rejected with "CoreML does not support input dim >
  16384" for `model.embed_tokens.weight {151936, 2048}`
  (microsoft/onnxruntime#21271, Nov 2025). Must be spike-validated.
- **Tooling friction**: `python -m onnxruntime.tools.make_dynamic_shape_fixed`
  (onnxruntime.ai/docs/tutorials/mobile/helpers/make-dynamic-shape-fixed.html)
  fails on these exports — onnx 1.22 has no schema for
  SimplifiedLayerNormalization ("No Op registered ... domain_version 21").
  Shape fixing must be manual (plain onnx, set input `dim_value`s).
- **fp16 silent conversion**: CoreML's default `NeuralNetwork` format casts
  fp32→fp16 (ym2132.github.io/ONNX_MLProgram_NN_exploration);
  `ModelFormat: "MLProgram"` (macOS 12+) preserves precision. Moot for fp16
  weights, fatal for q4's fp32 activations.
- **No official Metal/MPS EP** in ORT (issue #21271 open Jul 2026;
  onnxruntime-mlx experimental). CoreML EP is the only native-ORT GPU path.
- **Bench reality** (xybrid.ai, M2): fixed-shape CNN 6.8×, dynamic transformer
  ~1×, tiny models 0.2×; cold-start compile 1-5 s per model.

## Plan

**Verdict: do NOT ship "bucket the existing export" as the fix.** Bucketing
fixes dynamic-shape rejection but not unsupported-op partitioning. CoreML EP
becomes viable only with a **re-export from source** using CoreML-friendly
ops. Phase 0 below is a spike that decides.

### Phase 0 — Spike: fixed fp16 bucket on CoreML EP (no code changes)

Scratch venv: fix `model_fp16.onnx` shapes manually to `[32, 256]` (both
inputs), `onnxsim.simplify(m, check_n=0)`, then ORT session with
`("CoreMLExecutionProvider", {"ModelFormat":"MLProgram","MLComputeUnits":"ALL",
"RequireStaticInputShapes":"1","ModelCacheDirectory":"/tmp/coremlcache"})`.
`ProfileComputePlan: "1"`; log partition count + which ops stay on CPU.
Gate: **≥80% of FFN matmuls on ANE/GPU AND ≥1.5× faster than CPU fp16
(~3.9 units/s)** on M1/M2/M3. Expect NO-GO per the registry analysis; record
in `embeddings-bench/results/`. Also probe: does CoreML reject the
`[262144, 768]` embed Gather (16384 limit)? Does batch stay dynamic if only
seq is fixed?

### Phase 1 — Re-export with CoreML-friendly ops (if Phase 0 fails)

New `scripts/export_embeddinggemma_buckets.py` (repo has one script today).
Export from source (google/embeddinggemma-300m or a PyTorch port) with an op
allowlist: **unfused attention** (MatMul/Add/Softmax/Erf/Mul/Transpose — all
registered), **LayerNormalization** (not Simplified), rotary as plain
MatMul/Add/Mul with cos/sin constants, **no** com.microsoft ops, fp16 weights.
Output 5 bucket graphs `model_fp16_b{128,256,512,1024,2048}.onnx` (~655 KB
each) sharing ONE `model_fp16.onnx_data` (~617 MB) via external-data relative
path — no 5× disk. Verify `sentence_embedding` pooling unchanged per bucket;
parity vs API ≥ 0.997 (reuse `pkg/embedding/parity_probe_test.go`). Pin
sha256s in a new `EmbeddingGemma300MBucketConfig()` beside
`model_downloader.go:341`.

### Phase 2 — Go integration (darwin-only, fallback-first)

`pkg/embedding/onnx_runtime.go`: add `CoreML map[string]string` to
`SessionOption`; in `newSessionOptions`, behind `//go:build darwin`, call
`so.AppendExecutionProviderCoreMLV2(opts)`; on any error (incl.
ORT_NOT_IMPLEMENTED) log and continue CPU-only. `onnx_embedding_provider.go`:
replace the single dynamic session with a per-bucket session map keyed by
bucket seq; pick smallest bucket ≥ batch max-seq; pad + mask. Pad cost is
O(seq²): a 101-token unit in bucket 256 pays (256/101)² ≈ 6.4× attention —
buckets 256/512 are the sweet spot for short code units. Keep ≤2 live sessions
(LRU) to bound RSS (~617 MB each). `ModelHash`: hash the bucket set (all .onnx
+ shared data) — any change invalidates all stores (one-time ~30 min rebuild;
say so in release notes). `SPROUT_EMBEDDING_BACKEND=coreml|cpu` escape hatch.
Threading: with CoreML active, intra-op threads serve only CPU-fallback nodes;
keep the min(NumCPU,4) cap, revisit after Phase 0.

### Phase 3 — Rollout

Default ON for darwin/arm64 + macOS ≥ 12 when buckets present and CoreML EP
initializes; CPU otherwise (non-darwin, Intel, disabled). Exit: `go test
./pkg/embedding/`, index of this repo on M-series with `ProfileComputePlan`
showing ANE/GPU capture, `make build-all`, `sprout diag` reporting the active
backend.

## Risks

1. **Partition shattering persists** (highest): unsupported ops are inside
   every block; re-exported graphs must be validated for contiguous CoreML
   subgraphs. Phase 0 gate exists for this.
2. **Compile memory exhaustion**: prior run OOM'd compiling 291 partitions.
   `ModelCacheDirectory` compiles once per bucket and reuses .mlmodelc, but
   the first compile per bucket may still be heavy on 8 GB machines.
3. **macOS gates**: MLProgram needs macOS 12+; NeuralNetwork (10.15+)
   silently fp16-casts. Intel Macs have no ANE — CoreML there ≈ CPU.
4. **Vector compatibility**: CoreML compute may drift past the 0.997 parity
   bar; run the parity probe on CoreML output before trusting stores.
5. **Embedding-table dim limit** (16384 report): if the Gather is rejected the
   head runs CPU — acceptable only if the body still offloads.
6. **RAM/disk**: fp16 weights 617 MB; concurrent bucket sessions each load
   weights — LRU cap is the mitigation.
7. **ORT version drift**: binding pins 1.30.1; CoreML EP builders change
   between releases. Pin and re-probe on upgrade.

## Out of scope

- Browser WebGPU (already shipping — keep as the low-effort GPU win).
- MLX runtime (`onnxruntime-mlx`/mlx-lm): promising native Apple alternative,
  not ORT and still experimental — separate spec if CoreML EP fails.
- Linux/Windows GPU; non-embedding models (Gemma 3 2B generation) reuse the
  SessionOption plumbing but are not covered here.