# SP-136 — Cross-Platform Local LLM Backend (GGML/CUDA/ROCm/Vulkan)

## Current State (2026-08-08)

### What's built and verified

**Phase 1 — tensor.Backend interface + Metal implementation (DONE)**
- `pkg/tensor/types.go` — `Backend` (102 methods), `Array`, `Stream`, `Dtype` interfaces
- `pkg/tensor/detect.go` — `DetectBackend()` probes registered backends; `RegisterBackend()` for self-registration via `init()`
- `pkg/gomlx/mlx/backend.go` — `MetalBackend` implements `tensor.Backend` by delegating to existing CGO calls. Zero overhead.
- `pkg/gomlx/mlx/backend_stub.go` — stub for non-Apple platforms
- `pkg/gomlx/mlx/dtype.go` — `Dtype` aliased to `tensor.Dtype` so `*mlx.Array` satisfies `tensor.Array` natively
- Module wiring: `pkg/tensor/go.mod`, `pkg/gomlx/go.mod` requires tensor, root `go.mod` has replace directives
- **Verified**: `make build-all` ✓, `go test -tags mlx ./llm/` ✓, `go vet` ✓

**Phase 2 — GGML tensor.Backend implementation (CORE DONE, stubs remain)**
- `pkg/tensor/ggml/backend.go` — full `tensor.Backend` implementation on GGML C API
- CGO bindings to `ggml.h`, `ggml-backend.h`, `ggml-alloc.h`
- Eager-to-graph bridge: each op builds a single-op graph; `ggml_gallocr` allocates all graph tensors; input data set post-allocation; `ggml_backend_graph_compute` runs on GPU
- **Critical fix**: gallocr stored on Array, freed in `Array.Free()` — keeps backend buffers alive for the tensor's lifetime
- **Verified on M1 Pro Metal** (4 tests pass):
  - MatMul: `[38, 44, 50, 56, 83, 98, 113, 128]` ✓
  - Add: `[11, 22, 33, 44]` ✓
  - Softmax: sums to 1.0 ✓
  - RMSNorm: correct normalization ✓
- `pkg/tensor/ggml/go.mod` depends on `pkg/tensor`

**GGML op coverage:**
| Op | Status | Notes |
|---|---|---|
| MatMul | ✓ | `ggml_mul_mat`, verified correct |
| Add/Subtract/Multiply/Divide | ✓ | elementwise binary |
| Abs/Exp/Log/Log1p/Sqrt/Square/Negative | ✓ | elementwise unary |
| Sigmoid/Softplus/Sin/Cos/Tanh/Power | ✓ | composed or direct |
| Maximum | ✓ | composed: `a + relu(b-a)` |
| Sum/Mean/Max | ✓ | reductions |
| Softmax/SoftmaxAxis | ✓ | `ggml_soft_max` |
| FastRMSNorm | ✓ | `ggml_rms_norm` + `ggml_mul` for weight |
| FastScaledDotProductAttention | ✓ | `ggml_flash_attn_ext` |
| FastRoPE | ✓ | `ggml_rope` with position tensor |
| Reshape/Transpose/TransposeAxes | ✓ | `ggml_reshape`/`ggml_transpose`/`ggml_permute` |
| SqueezeAxis | ✓ | no-op (GGML uses 4D internally) |
| Slice | ✓ (2D only) | `ggml_view_2d` |
| ConcatenateAxis/Stack/RepeatAxis | ✓ | `ggml_concat` |
| Pad | ✓ | `ggml_pad` |
| ArgMax/ArgMaxAxis | ✓ | `ggml_argmax` |
| AsType | ✓ | `ggml_cpy` with target type |
| **Conv1D** | **STUB** | needs `ggml_im2col` + `ggml_mul_mat` |
| **GatherAxis** | **STUB** | needs `ggml_get_rows` |
| **Quantize/QuantizedMatMul/Dequantize** | **STUB** | needs GGML quantized types (GGML_TYPE_Q4_0 etc.) |
| **SliceUpdate** | **STUB** | needs `ggml_acc` or custom |
| **SplitAxis** | **STUB** | needs `ggml_view` + manual split |
| **Where** | **STUB** | needs `ggml_cmp` + `ggml_where` or composition |
| **Tril** | **STUB** | needs mask tensor creation |

### What's NOT done

1. **Model layer migration** — `pkg/gomlx/llm/` still imports `mlx` directly, not `tensor.Backend`. ~200 call sites across 22 files.
2. **Remaining GGML op stubs** — 8 ops need implementation (see table above)
3. **GGML quantized matmul** — GGML has native quantized types (Q4_0, Q4_1, Q5_0, etc.) but our model uses affine quantization (weights/scales/biases triplets). Need to either: (a) map our triplet format to GGML's quantized types, or (b) dequantize on load and use GGML's built-in quantization
4. **Cross-platform testing** — everything verified on macOS Metal only; CUDA/ROCm/Vulkan untested
5. **Bundling** — GGML `.so`/`.dylib`/`.dll` files need to be distributed or built on target machines

---

## Implementation Plan

### Phase 2 Completion: Remaining GGML ops (on Mac, no new hardware needed)

**Effort: 1 session**

1. Implement the 8 stub ops:
   - `Conv1D`: `ggml_im2col` → reshape → `ggml_mul_mat`
   - `GatherAxis`: `ggml_get_rows` (maps directly — gathers rows by index)
   - `QuantizedMatMul`: dequantize on load into GGML F32 tensor, then standard `ggml_mul_mat`. Defer GGML native quant types until perf testing shows it's needed.
   - `Dequantize`: trivial — return the tensor as-is (already F32 after load-time dequant)
   - `SliceUpdate`: `ggml_acc` (accumulate into a view)
   - `SplitAxis`: create N `ggml_view` tensors at the right offsets
   - `Where`: `ggml_cmp` (comparison) → `ggml_mul` (mask) → add
   - `Tril`: create a lower-triangular mask tensor, multiply
2. Write parity tests: each op should produce the same output as the MLX equivalent for the same input

### Phase 3: Model layer migration (on Mac, no new hardware needed)

**Effort: 2-3 sessions**

This is the mechanical but critical step: change every `mlx.*` call to `backend.*`.

1. **Add `backend tensor.Backend` field to every struct that currently holds a `*mlx.Stream`**:
   - `Qwen35` struct in `qwen35/forward.go`
   - `Qwen3` struct in `qwen3/forward.go`
   - `Model` struct in `model.go`
   - `Linear` struct in `linear.go`
   - `Embedding` struct in `embedding.go`
   - `KVCache` struct in `kv_cache.go`

2. **Change type signatures from `*mlx.Array`/`*mlx.Stream` to `tensor.Array`/`tensor.Stream`**:
   - Every function parameter and return value
   - Local variables that hold op results
   - The `defer result.Free()` pattern stays the same

3. **Replace `mlx.OpName(a, b, s)` with `backend.OpName(a, b, s)`**:
   - ~200 call sites across 22 files
   - Mechanical but must be done carefully — some ops have slightly different argument names
   - The `MetalBackend` methods already exist and delegate correctly

4. **Update `NewModel()` to accept a `tensor.Backend`**:
   - Currently calls `mlx.NewGPUStream()` directly
   - Change to `backend := tensor.DetectBackend()` → `stream, _ := backend.NewGPUStream()`
   - Pass `backend` to all architecture constructors

5. **Update the stub** (`stub.go`):
   - Non-MLX builds need a non-functional `tensor.Backend` that returns errors
   - `DetectBackend()` returns nil → `NewModel()` returns error → sprout falls back to cloud

6. **Verify**: `make build-all` ✓, `go test -tags mlx ./llm/` ✓ (all existing tests pass with Metal backend), `go test` without tags ✓ (stub compiles and returns errors gracefully)

### Phase 4: Cross-platform testing (REQUIRES other hardware)

**Effort: 1-2 sessions on target hardware**

This is where we need other machines. The code should be correct after Phase 3 — we just need to verify GGML loads the right backend and produces correct output.

#### Test environment setup

**Linux/NVIDIA (CUDA):**
```bash
# Install GGML with CUDA support
sudo apt install cmake nvidia-cuda-toolkit
git clone https://github.com/ggml-org/llama.cpp
cd llama.cpp && cmake -B build -DGGML_CUDA=ON && cmake --build build
sudo cp build/src/libggml*.so /usr/local/lib/
sudo cp ggml/include/*.h /usr/local/include/

# Build sprout with GGML
CGO_ENABLED=1 go build -tags ggml ./cmd/llm_server/

# Run parity test
go test -tags ggml -v ./pkg/tensor/ggml/ -run TestGGML
```

**Linux/AMD (ROCm):**
```bash
# Same as above but -DGGML_ROCM=ON instead of -DGGML_CUDA=ON
```

**Linux/Intel/Any (Vulkan):**
```bash
# Same but -DGGML_VULKAN=ON
```

**Windows/WSL2:**
```bash
# In WSL2 Ubuntu, same as Linux. NVIDIA CUDA works via WSL CUDA drivers.
# For Snapdragon ARM: WSL2 linux/arm64, GGML CPU backend (Oryon cores are fast)
```

#### What to test on each platform

1. **Backend detection**: `DetectBackend()` returns the right backend name
2. **Core ops**: MatMul, Add, Softmax, RMSNorm produce identical values to Mac Metal
3. **Full forward pass**: load Qwen3.5-4B safetensors, generate text, verify output matches Mac MLX output
4. **Performance**: measure tok/s — should be competitive with llama.cpp on the same hardware
5. **Memory**: verify `ApplyMemoryLimits` and RAM gating still work

#### Expected results per platform

| Platform | Backend | GPU | Expected tok/s (4B Q4) | Notes |
|---|---|---|---|---|
| Mac M1 Pro | MLX (Metal) | Apple GPU | ~14 | Current baseline |
| Mac M1 Pro | GGML (Metal) | Apple GPU | ~14 | Should match MLX |
| Linux + RTX 4090 | GGML (CUDA) | NVIDIA | ~60-100 | Much faster |
| Linux + RX 7900 | GGML (ROCm) | AMD | ~40-60 | |
| WSL2 + RTX 3070 | GGML (CUDA) | NVIDIA | ~40-60 | |
| WSL2 ARM (Snapdragon) | GGML (CPU) | Oryon cores | ~10-15 | No GPU passthrough |

### Phase 5: GGML quantized matmul (perf optimization)

**Effort: 1 session, after Phase 4 confirms correctness**

Currently the GGML backend dequantizes weights to F32 on load. For production performance:

1. Map our affine quantization (weights/scales/biases) to GGML's native quantized types:
   - GGML Q4_0 = 4-bit with per-group scale (closest to our format)
   - `ggml_new_tensor` with type `GGML_TYPE_Q4_0` → `ggml_mul_mat` auto-uses quantized kernel
2. Or: load F32 weights and call `ggml_quantize_chunk()` to quantize on load
3. Verify: output quality parity (should be identical — same quant math, different kernel)

### Phase 6: Bundling and distribution

**Effort: 1-2 sessions**

1. **macOS**: no change — MLX is the primary path, GGML is secondary
2. **Linux**: bundle `libggml*.so` in the sprout distribution, or document `apt install`
3. **Windows/WSL2**: bundle `.so` files for WSL2, or document install steps
4. **Snapdragon**: document CPU backend (fast Oryon cores), note GPU passthrough is future work
5. **CI**: add build targets for `linux/amd64` with `ggml` tag (CUDA + Vulkan builds)

### CI Matrix (target)

| OS | Arch | Backend | Build Tag | Status |
|---|---|---|---|---|
| macOS | arm64 | MLX (Metal) | `mlx` | ✓ working |
| macOS | arm64 | GGML (Metal) | `ggml` | ✓ core ops verified |
| Ubuntu | amd64 | GGML (CUDA) | `ggml` | Needs testing |
| Ubuntu | amd64 | GGML (Vulkan) | `ggml` | Needs testing |
| Ubuntu | arm64 | GGML (CPU) | `ggml` | Needs testing |
| Windows | arm64 | (via WSL2) | — | Document WSL2 path |

---

## Architecture Summary

```
                    tensor.DetectBackend()
                           │
              ┌────────────┼────────────┐
              │            │            │
         MetalBackend   GGMLBackend   (future)
         (mlx CGO)      (ggml CGO)    (custom)
              │            │
         Apple GPU    ┌───┼───┬────────┐
         (Metal)     │   │   │        │
                    CUDA ROCm Vulkan  CPU
                    (NVIDIA) (AMD) (any) (fallback)
```

**On macOS**: MLX wins (first in detection order). GGML is secondary.
**On Linux/WSL2**: GGML wins. CUDA > ROCm > Vulkan > CPU.
**Detection**: `ggml_backend_load_all()` discovers `.so`/`.dylib` files at runtime.

## What stays Apple-specific

`pkg/gomlx/mlx/` — the MLX CGO bindings. Still `darwin/arm64`-only with `mlx` build tag. But now it's one implementation of `tensor.Backend`, not the only path. The MLX model logic (Qwen3, Qwen3.5 forward passes) is shared via the `tensor.Backend` interface — the same Go code runs on all platforms.

## Key Files

| File | Purpose |
|---|---|
| `pkg/tensor/types.go` | Backend, Array, Stream, Dtype interfaces |
| `pkg/tensor/detect.go` | Backend detection and registration |
| `pkg/gomlx/mlx/backend.go` | MetalBackend (wraps mlx CGO) |
| `pkg/gomlx/mlx/backend_stub.go` | Non-Apple stub |
| `pkg/gomlx/mlx/dtype.go` | Dtype alias to tensor.Dtype |
| `pkg/tensor/ggml/backend.go` | GGMLBackend (wraps ggml CGO) |
| `pkg/tensor/ggml/backend_test.go` | Verified tests (MatMul, Add, Softmax, RMSNorm) |
| `pkg/gomlx/llm/*.go` | Model layer (22 files, needs Phase 3 migration) |
| `cmd/llm_server/main.go` | OpenAI-compatible local server |
| `pkg/webui/local_llm_api.go` | Local LLM status/lifecycle API |

## Build Commands

```bash
# Mac with MLX (primary)
make build-all                    # full sprout build
go test -tags mlx ./pkg/gomlx/llm/  # model layer tests

# Mac with GGML (secondary, for development)
CGO_ENABLED=1 go test -tags ggml ./pkg/tensor/ggml/

# Linux with GGML (target)
CGO_ENABLED=1 go build -tags ggml ./cmd/llm_server/
CGO_ENABLED=1 go test -tags ggml ./pkg/tensor/ggml/
```
