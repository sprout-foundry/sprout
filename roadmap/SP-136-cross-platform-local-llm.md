# SP-136 — Cross-Platform Local LLM Backend (CUDA/ROCm/Vulkan)

## Problem

Local LLM inference (`sprout-local` provider) works on Apple Silicon via a
custom MLX Go binding (`pkg/gomlx/`). Users on Linux (NVIDIA/AMD/Intel GPUs)
and Windows cannot use local inference — there is no backend for them.

The current MLX path is Apple-only at the CGO layer (`mlx_cgo.go` binds the
Metal-only `mlx-c` C API). The model logic (`pkg/gomlx/llm/`) is conceptually
portable but tightly coupled to `*mlx.Array` / `*mlx.Stream` — concrete CGO
types, not interfaces.

## Design Decision: In-Process Tensor Backend Abstraction

**Build a tensor backend interface and implement it per GPU vendor.**

The original instinct was "just run llama.cpp as a subprocess." That was
rejected because **process management and consistency is the reason we built
the Go MLX path in the first place.** A subprocess means:

- **Different tokenizers** — llama.cpp's BPE, GGUF tokenizer, or SentencePiece
  produce different token IDs than our Go BPE implementation. The same prompt
  yields different token sequences, different context limits, different
  token-count billing. Our token counting, repetition penalty, and context
  management all assume the Go tokenizer.
- **Different quantization behavior** — GGUF Q4_K_M ≠ our affine 4-bit.
  Output quality, token distributions, and error modes differ. A user
  debugging "the local model said something different than cloud" would face
  a second layer of variance from the quant format.
- **Different streaming formats** — llama.cpp's SSE format, error messages,
  and stop conditions differ from ours. MTP, thinking-mode filtering, and EOS
  handling are all in our Go code.
- **Different memory management** — our code applies `ApplyMemoryLimits`,
  `TrimCachedMemory`, and RAM-gated model selection. A subprocess manages its
  own memory; we can't trim its cache mid-generation or gate it reliably.
- **Process lifecycle** — crashes, port conflicts, startup races, zombie
  processes, signal handling, log capture. Every subprocess adds operational
  failure modes that in-process code doesn't have.

Instead: **define a `tensor.Backend` interface, implement it for each GPU
vendor, and keep the entire model layer (forward pass, tokenizer, KV cache,
MTP, sampling, server) in Go.** Same tokenizer, same quantization, same
streaming, same memory management — only the compute kernel changes.

## Architecture

### Layer 1: Tensor Backend Interface (`pkg/tensor/`)

New top-level package defining the abstraction:

```go
// pkg/tensor/backend.go

// Backend is a compute backend for tensor operations. Implementations:
// metal (Apple), cuda (NVIDIA), rocm (AMD), vulkan (portable).
type Backend interface {
    // Available reports whether this backend can run on the current machine.
    Available() bool

    // NewStream creates a compute stream (command queue) for ordering ops.
    NewStream() (Stream, error)

    // Tensor creation
    NewArrayFromFloat32(data []float32, shape []int) (Array, error)
    NewArrayFromInt64(data []int64, shape []int) (Array, error)

    // Ops — the minimal set needed by the model layer.
    // Each returns a new Array; the caller frees inputs.
    MatMul(a, b Array, s Stream) (Array, error)
    QuantizedMatMul(...) (Array, error)
    Softmax(a Array, s Stream) (Array, error)
    RMSNorm(a, weight Array, eps float32, s Stream) (Array, error)
    // ... etc (full op set derived from current mlx_* functions)
}

type Array interface {
    Shape() []int
    Dtype() Dtype
    Float32Data() ([]float32, error)
    Eval() error
    Free()
    Retain() Array
}

type Stream interface {
    Synchronize() error
    Free()
}
```

**Migration path from `pkg/gomlx/mlx/`:**
1. `mlx.Array` → `tensor.Array` (interface, same methods)
2. `mlx.Stream` → `tensor.Stream`
3. `mlx.MatMul(a, b, s)` → `backend.MatMul(a, b, s)` (method on backend)
4. The model layer (`pkg/gomlx/llm/`) imports `pkg/tensor`, not `pkg/gomlx/mlx/`

### Layer 2: Metal Backend (port of current MLX binding)

`pkg/tensor/metal/` — wraps the existing CGO calls to `mlx-c`. This is a
mechanical port: every function in `mlx_cgo.go` / `mlx_ops.go` / 
`mlx_fast_ops.go` becomes a method on `metalBackend`.

On `darwin/arm64` with build tag `metal`, this backend is active. On all
other platforms, it's a stub returning `Available() = false`.

### Layer 3: CUDA Backend (NVIDIA)

`pkg/tensor/cuda/` — CGO bindings to a CUDA C library. Two implementation
options:

**Option A: ONNX Runtime C API (reuse existing dependency)**
- sprout already depends on ONNX Runtime (`pkg/embedding/`)
- Export the Qwen3.5 forward pass as an ONNX graph, run via ORT's CUDA EP
- Pros: no new C++ code to write; ORT handles kernel fusion, memory
- Cons: ONNX export of Qwen3.5 DeltaNet hybrid is non-trivial; loses the
  hand-tuned Metal kernels; MTP requires a separate graph

**Option B: Custom CUDA kernels via CGO**
- Write CUDA kernels for MatMul, Attention, RMSNorm, etc. (or bind cuBLAS)
- Pros: full control, can match MLX's fused kernel performance
- Cons: significant engineering effort; CUDA-only (no AMD/Intel)

**Option C: Use GGML (llama.cpp's tensor library) as a C dependency**
- GGML already supports CUDA, ROCm, Vulkan, Metal, and CPU backends
- Bind `ggml.h` via CGO; implement `tensor.Backend` on top of GGML tensors
- Pros: one C library covers all GPU vendors; mature, optimized kernels
- Cons: GGML's API is lower-level than MLX's; some ops need composition

**Recommended: Option C (GGML)**. It's the least code to maintain, covers all
GPU vendors with one integration, and GGML is the most battle-tested tensor
library in the local-LLM ecosystem. We get consistent behavior because the
Go model layer still controls the forward pass, tokenizer, and sampling.

### Layer 4: ROCm (AMD) and Vulkan (portable)

With Option C (GGML), these come for free — GGML already has ROCm and Vulkan
backends. The `tensor.Backend` implementation is the same; GGML dispatches to
the right compute backend internally based on what's available.

If we chose Option A or B, AMD/Intel would each need separate work.

### Layer 5: Model Layer (unchanged logic, new import path)

`pkg/gomlx/llm/` → `pkg/llm/` (or stays, with import changed to `pkg/tensor`)

The forward pass code changes from:
```go
import "github.com/sprout-foundry/sprout/pkg/gomlx/mlx"

out, err := mlx.MatMul(a, b, s)
```
to:
```go
import "github.com/sprout-foundry/sprout/pkg/tensor"

out, err := backend.MatMul(a, b, s)
```

This is a mechanical find-and-replace across ~30 files. The model logic
(Qwen3 attention, Qwen3.5 DeltaNet, MTP, KV cache, sampling) does not change.
The tokenizer, safetensors loader, and server are pure Go and need no changes.

### Layer 6: Backend Detection and Selection

```go
// pkg/tensor/detect.go

func DetectBackend() Backend {
    // Try in priority order:
    for _, b := range []Backend{
        &metal.Backend{},  // Apple Silicon
        &cuda.Backend{},   // NVIDIA
        &rocm.Backend{},   // AMD
        &vulkan.Backend{}, // Portable fallback
        &cpu.Backend{},    // Last resort
    } {
        if b.Available() {
            return b
        }
    }
    return nil
}
```

Build tags control which backends are compiled in:
- `metal` — darwin/arm64 only
- `cuda` — linux/amd64 with NVIDIA toolkit
- `rocm` — linux/amd64 with ROCm
- `vulkan` — any platform with Vulkan SDK
- Default (no tags) — CPU backend (slow but works everywhere)

### Layer 7: Quantization and Model Format

**Keep safetensors, not GGUF.** Our Go safetensors loader already handles:
- Full-precision (BF16/F32)
- Pre-quantized triplets (weights/scales/biases)
- Load-time quantization via `GO_QUANTIZE`
- Sharded files with index

The tensor backend's `QuantizedMatMul` op handles the dequantize-then-multiply
at the compute level. Different backends may use different fused kernel
strategies, but the weight format on disk is the same.

GGML/GGUF is only needed if we go with Option C and want to load GGUF files
directly. But loading safetensors into GGML tensors is straightforward —
GGML has `ggml_new_tensor` and we can copy the bytes in.

## Implementation Plan

### Phase 1: Tensor interface + Metal backend (refactor, no new deps)

1. Create `pkg/tensor/` with `Backend`, `Array`, `Stream` interfaces
2. Port `pkg/gomlx/mlx/*` → `pkg/tensor/metal/*` (mechanical CGO rename)
3. Update `pkg/gomlx/llm/*` imports from `mlx` to `tensor`
4. `cmd/llm_server` selects backend via `tensor.DetectBackend()`
5. **Verify**: all existing tests pass, no behavioral change

Effort: ~2 sessions. Mechanical work, no new functionality.

### Phase 2: GGML C binding (one integration, all GPUs)

1. Add GGML as a C dependency (vendored or pkg-config)
2. Implement `pkg/tensor/ggml/` — `Backend` interface on top of `ggml.h`
3. Map safetensors weights → GGML tensors at load time
4. Implement each op: MatMul (ggml_mul_mat), RMSNorm (ggml_rms_norm),
   Softmax, RoPE (ggml_rope), SDPA (ggml_flash_attn), etc.
5. Build tags: `cuda` compiles GGML with CUDA, `vulkan` with Vulkan, etc.
6. **Verify**: parity test — same model, same prompt, same output on Mac
   (Metal via GGML) vs Mac (Metal via MLX)

Effort: ~3-4 sessions. This is the core investment.

### Phase 3: Model layer migration

1. Move `pkg/gomlx/llm/` → `pkg/llm/` (drop the `gomlx` prefix)
2. Update all cmd/ imports
3. Update webui local_llm_api.go to use `tensor.DetectBackend()` instead of
   platform checks
4. CI: add Linux/Vulkan and Linux/CUDA build targets
5. **Verify**: end-to-end on Linux (GitHub Actions GPU runner or a test box)

Effort: ~1-2 sessions.

### Phase 4: Polish and ship

1. Model catalog updated with cross-platform download links
2. Settings tab shows detected backend ("Apple Metal" / "NVIDIA CUDA" / etc.)
3. CPU fallback backend for machines with no GPU
4. CI matrix: macOS (Metal), Ubuntu (CUDA), Ubuntu (Vulkan), Ubuntu (CPU)
5. Documentation: platform-specific build instructions

## Trade-offs

**Why GGML over ONNX Runtime for the C layer:**
- GGML's op set is closer to MLX's (both are eager-mode tensor libs)
- ORT would require exporting the model as a static graph, losing the
  ability to control the forward pass from Go (needed for MTP, KV cache
  management, speculative decoding)
- GGML already supports all four GPU backends (Metal, CUDA, ROCm, Vulkan)
- GGML's quantized matmul is the most battle-tested in the local LLM world

**Why not keep MLX-only + subprocess for other platforms:**
- **Process management and consistency** — the reason we built the Go path.
  Different tokenizers, different quantization, different streaming, different
  memory management. In-process means one tokenizer, one quant format, one
  error surface, one memory model.
- Subprocess adds port conflicts, startup races, crash recovery, zombie
  processes, and log capture as operational failure modes
- Users debugging "different output on Linux vs Mac" would face a second
  layer of variance — not just the GPU backend, but the entire inference
  engine

**Why not write CUDA kernels directly (Option B):**
- NVIDIA-only; leaves AMD and Intel out
- Significant engineering for each op (cuBLAS for MatMul is easy; fused
  SDPA, RoPE, quantized matmul are all custom)
- GGML already has these kernels, tested across millions of users

## What stays Apple-specific

- `pkg/gomlx/mlx/` — the MLX CGO bindings become `pkg/tensor/metal/`. Still
  darwin/arm64-only, still compiled with the `metal` build tag. But it's now
  one implementation of a common interface, not the only path.
- The Metal-specific fused kernels (FastScaledDotProductAttention,
  FastRoPE, fused gated delta update) stay as Metal implementations. The
  GGML backend provides equivalent fused kernels for CUDA/ROCm/Vulkan.

## Relationship to embedding model

The embedding model (`pkg/embedding/`) has its own GPU story via ONNX Runtime:
- macOS: CoreML EP (or CPU)
- Linux/Windows: CUDA/DirectML EP (already supported by onnxruntime-go)

Embeddings are orthogonal — they use ONNX Runtime, not the LLM tensor backend.
No changes needed there.
