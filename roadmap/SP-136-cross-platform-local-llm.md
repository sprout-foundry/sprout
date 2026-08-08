# SP-136 — Cross-Platform Local LLM Backend (Vulkan/CUDA/ROCm)

## Problem

Local LLM inference (`sprout-local` provider) works on Apple Silicon via a
custom MLX Go binding (`pkg/gomlx/`). Users on Linux (NVIDIA/AMD/Intel GPUs)
and Windows cannot use local inference — there is no backend for them.

The current MLX path is Apple-only at the CGO layer (`mlx_cgo.go` binds the
Metal-only `mlx-c` C API). The model logic (`pkg/gomlx/llm/`) is conceptually
portable but tightly coupled to `*mlx.Array` / `*mlx.Stream` — concrete CGO
types, not interfaces.

## Design Decision: Subprocess, Not Abstraction Layer

**Do NOT build a tensor abstraction interface.** Attempting to make
`mlx.Array` an interface with CUDA/ROCm/Vulkan implementations would mean:

- Rewriting ~30 files with `*mlx.Array` references (forward passes, KV cache,
  attention, tokenizer, embedding, safetensors loader, MTP, DeltaNet kernel)
- Reimplementing every op (MatMul, Softmax, RMSNorm, RoPE, Conv1d, SDPA,
  Gather, QuantizedMatMul, etc.) for each backend
- Maintaining parity across 4+ backends for every new op
- The Go ecosystem has no mature CUDA/ROCm tensor library

Instead: **run a backend-appropriate inference engine as a subprocess behind
the existing OpenAI-compatible API.** The `sprout-local` provider already
talks HTTP to `localhost:18081`. On Mac that's our MLX server; on Linux/Windows
it's llama.cpp (or vLLM, or any OpenAI-compatible local server).

```
sprout-local provider config → http://127.0.0.1:18081/v1/chat/completions
                                      │
                    ┌─────────────────┼─────────────────┐
                    │                 │                  │
              macOS arm64       Linux/Win          Any OS
              (MLX native)      (llama.cpp)        (Ollama, LM Studio)
              llm_server        llm-server-cpp     external process
              (in-process)      (subprocess)       (user-managed)
```

## Architecture

### Layer 1: Backend Detection (`pkg/local_llm/`)

New package, separate from `pkg/gomlx/` (which stays Apple-only).

```
pkg/local_llm/
    detect.go     — BackendType detection (mlx, llamacpp, ollama, none)
    manager.go    — lifecycle: start/stop/health-check the backend process
    status.go     — structured status for the UI
    catalog.go    — model catalog with backend-specific download URLs
```

**BackendType** enum:
- `mlx` — Apple Silicon, in-process Go MLX (current path)
- `llamacpp` — Linux/Windows, llama.cpp subprocess
- `ollama` — any OS, Ollama as subprocess (if installed)
- `none` — no local backend available

Detection logic:
1. `runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"` + MLX build tag → `mlx`
2. `which llama-server` or `which ollama` on PATH → use that
3. Otherwise → `none` (fall back to cloud providers)

### Layer 2: Server Lifecycle

The existing `pkg/webui/local_llm_api.go` already has `ensureLocalLLMRunning()`.
This gets generalized:

```go
// pkg/local_llm/manager.go

type Backend interface {
    Detect() bool                                    // is this backend usable here?
    Status() (*Status, error)                        // probe health, models
    EnsureRunning(modelID string) (endpoint string, err error)
    Stop() error
    DownloadModel(modelID string) error
    ListModels() ([]ModelEntry, error)
}
```

Implementations:
- `mlxBackend` — wraps current `cmd/llm_server` (existing, macOS only)
- `llamacppBackend` — spawns `llama-server` (from llama.cpp build)
- `ollamaBackend` — spawns `ollama serve` + `ollama pull <model>`

The webui's `local_llm_api.go` calls through the manager, which dispatches to
the right backend. The API contract (`/api/local-llm/*`) stays identical.

### Layer 3: Model Catalog (backend-specific)

Different backends need different model formats:

| Backend | Format | Qwen3.5-4B source |
|---------|--------|--------------------|
| MLX | MLX-quantized safetensors | `mlx-community/Qwen3.5-4B-4bit` |
| llama.cpp | GGUF | `Qwen/Qwen3.5-4B-GGUF` (Q4_K_M) |
| Ollama | Ollama manifest | `qwen3.5:4b` |

`catalog.go` maps the same model ID (`qwen3.5-4b-4bit`) to the backend-specific
download artifact. The UI shows one model; the backend resolves the format.

### Layer 4: UI (no changes)

The UI already talks to `/api/local-llm/status|start|download|models`. The
backend type is surfaced as a field in the status response. The onboarding,
settings tab, and status bar all work unchanged — they show "Local (Offline)"
regardless of whether it's MLX or llama.cpp underneath.

## Implementation Plan

### Phase 1: Extract manager interface (this repo, no new deps)

1. Move `pkg/webui/local_llm_api.go` logic into `pkg/local_llm/manager.go`
2. Define `Backend` interface
3. Implement `mlxBackend` wrapping the current behavior
4. webui calls `local_llm.GetBackend()` instead of inline logic
5. No user-visible change — refactor only

### Phase 2: llama.cpp backend (Linux/Windows support)

1. Implement `llamacppBackend`:
   - Detect `llama-server` binary on PATH (or bundled)
   - `EnsureRunning`: spawn `llama-server -m <gguf> --port 18081`
   - `DownloadModel`: `huggingface-cli download Qwen/Qwen3.5-4B-GGUF`
   - Health: `GET /health` (llama.cpp already supports this)
2. Model catalog with GGUF download URLs
3. CI: build `llama-server` in a Linux container for bundling
4. Test: verify on Linux/NVIDIA (GitHub Actions runner)

### Phase 3: Ollama fallback (cross-platform, user-managed)

1. Implement `ollamaBackend`:
   - Detect `ollama` binary
   - `EnsureRunning`: spawn `ollama serve` (default port 11434)
   - `DownloadModel`: `ollama pull qwen3.5:4b`
   - Health: `GET /api/tags` on Ollama's port
2. Provider config for Ollama uses port 11434, not 18081
3. Model catalog maps to Ollama model tags

### Phase 4: Bundling

- **macOS**: `llm_server` (MLX) built and shipped alongside sprout (existing)
- **Linux**: bundle `llama-server` binary or document `apt install llama.cpp`
- **Windows**: same as Linux; vLLM is also an option once it stabilizes on Win
- **Universal fallback**: if no local backend binary is found, the UI shows
  "Install [Ollama](https://ollama.com)" as a one-click setup path

## What stays Apple-only

- `pkg/gomlx/` — the MLX Go bindings, model forward passes, MTP, DeltaNet
  kernel. These are the "secret sauce" for Apple Silicon — they give us
  in-process inference without an external dependency. They stay as-is,
  compiled only with `-tags mlx` on `darwin/arm64`.
- The MLX model logic (Qwen3, Qwen3.5 forward pass, tokenizer, KV cache,
  safetensors loader) is reusable in principle, but porting it to another
  tensor library would be a separate large effort with little payoff when
  llama.cpp already covers those platforms.

## What this does NOT do

- Does not add CUDA/ROCm/Vulkan tensor ops to Go
- Does not create a Go tensor abstraction layer
- Does not compile MLX for other platforms (MLX is Metal-only by design)
- Does not change the provider config or API contract

## Relationship to embedding model

The embedding model (`pkg/embedding/`) has its own GPU story:
- macOS: ONNX Runtime with CoreML EP (current, via SP-134)
- Linux/Windows: ONNX Runtime with CUDA/DirectML EP (already supported by
  onnxruntime-go, just needs the right execution provider build)

Embeddings are orthogonal to LLM inference. This spec covers LLM only.
