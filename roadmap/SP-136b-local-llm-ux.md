# SP-136b — Local LLM User Experience

Parent: SP-134 (GPU macOS embeddings), SP-136 (cross-platform local LLM)

## Problem

The local LLM engine works — `cmd/llm_server` serves models, the `sprout-local`
provider connects to it, generation is correct. But using it requires manual
steps: starting the server, knowing which model to download, running `hf download`
by hand. The user experience should be: **select "Local" provider → sprout handles
everything else**.

## Current State (2026-08-10)

### What works
- `cmd/llm_server` — Go MLX server, OpenAI-compatible API on port 18081
- `pkg/gomlx/llm/catalog.go` — model catalog with RAM-based selection (`SelectModelForRAM`)
- `pkg/gomlx/llm/` — Qwen3.5 architecture (forward pass, tokenizer, KV cache, MTP)
- `pkg/agent_providers/configs/sprout-local.json` — provider config at :18081
- `cmd/llm_download` — CLI tool that downloads the recommended model via `hf`
- Generation verified: 0.8B (60 tok/s), 4B (28 tok/s) on M1 Pro 16GB
- Quality verified: both pass real sprout agent bug-fix and add-feature tasks

### What's missing
1. **Server lifecycle** — nothing starts `llm_server` when sprout-local is selected
2. **Model picker** — onboarding doesn't show local models or hardware recommendation
3. **Download flow** — no progress bar, no "ensure model present before use"
4. **Auto-select** — first-run doesn't default to the RAM-recommended model

## Implementation Plan

### Phase 1: Server lifecycle (`pkg/localmodel/server.go`)
**Status: DONE (committed 0e5d780bf)**

- `EnsureServer(ctx, port, modelDir)` — health check → spawn if needed → wait for healthy
- `HealthCheck(port)` — GET /health on the server port
- `StopServer(port)` — POST /shutdown
- Server process is detached (survives CLI exit), managed via health checks
- Uses the daemon autostart pattern from `pkg/daemon/autostart.go`

**Files:**
- `pkg/localmodel/server.go` — lifecycle management
- `pkg/localmodel/server_platform.go` — `detachedSysProcAttr()` per-OS

### Phase 2: Model management (`pkg/localmodel/model.go`)
**Status: DONE (committed 0e5d780bf)**
- `ListModels()` — scan models dir + catalog, return installed + available
- `EnsureModel(ctx, catalogEntry, modelsDir, progressFn)` — download if missing
- Download via `hf download` subprocess with progress parsing
- `RecommendedModel()` — wraps `catalog.RecommendModelForRAM()`

**Files:**
- `pkg/localmodel/model.go` — model listing, download, recommendation

### Phase 3: Onboarding integration (`cmd/onboarding_local.go`)
**Status: DONE (committed 0e5d780bf)**
- `sprout-local` appears in the provider picker as "Local (Offline)"
- When selected:
  1. Show hardware info and recommended model
  2. List available models (downloaded vs not)
  3. If model needs download: show progress bar, block until complete
  4. Start server via `EnsureServer`
  5. Persist `sprout-local` as provider

**Files:**
- `cmd/onboarding_local.go` — local provider onboarding flow
- `cmd/onboarding.go` — add sprout-local to provider list

### Phase 4: Agent integration (`cmd/agent_execution.go`)
**Status: DONE (committed 0e5d780bf)**
- When `sprout-local` is the active provider and no server responds: auto-start it
- Cache the running model path so server restarts use the same model
- Handle server death gracefully: detect on next request, restart

**Files:**
- `cmd/agent_execution.go` — pre-flight server check for sprout-local
- `pkg/agent_providers/generic_provider.go` — lazy server start on connection error

### Phase 5: Daemon mode integration
**Status: DONE (committed b218d4d64)**
- When sprout daemon starts, optionally pre-load the local model
- Server lifecycle tied to daemon lifecycle (idle timeout reaps both)
- WebUI shows local model status (model name, tok/s, memory)

## Catalog (current)

**Interim**: single hardcoded model until per-hardware mapping is finalized.

| Model | Dir | HF Repo | Notes |
|-------|-----|---------|-------|
| Gemma4 e2b (4-bit) | gemma-4-e2b-it-4bit | mlx-community/gemma-4-e2b-it-4bit | Default for all machines |

NOTE: 5-bit quantization (`gemma-4-e2b-5bit`) produces garbage output
through the Go server — the 5-bit affine quantized matmul path needs
debugging. Using 4-bit (verified working) as the interim default.

## Future Work: Per-hardware model mapping

The catalog currently hardcodes a single model (Gemma4 e2b 5-bit) for all
machines. Before PR to main, we need to finalize the model lineup and map
the right model + quantization to each hardware tier:

1. **Benchmark candidate models** (Gemma4 e2b/e4b, Qwen3.5 0.8B/2B/4B/9B,
   sprout-tuned variants) across hardware tiers
2. **Decide per-tier defaults** — likely:
   - 8 GB: smallest viable model (e2b q5 or q4)
   - 16 GB: balanced model (e4b q4 or tuned 4b q5)
   - 32 GB: largest model (9B q4 or tuned 4b q8)
3. **Wire sprout-tuned models** into the catalog with proper HF repos once
   they're published
4. **Implement Gemma4 architecture** in Go (`pkg/gomlx/llm/gemma4/`) so
   the native server can serve Gemma4 models without mlx-lm subprocess
5. **Update `ModelCatalog`** with the finalized per-tier entries

This work is blocked on: (a) final model selection from the user,
(b) Gemma4 Go architecture implementation, (c) sprout-tuned model publication.

## Performance Reality

- Go MLX server: 28 tok/s on 4B (vs 45 tok/s mlx-lm Python)
- Gap is CGO overhead (~1000 FFI calls/token ≈ 10ms)
- Fused DeltaNet kernel: 18× prefill speedup (already shipped)
- No path to close remaining gap without C extension or MLX C API improvements

## What NOT to change
- Don't bundle the `llm_server` binary into sprout — keep it separate, find via PATH/exe-dir
- Don't implement Gemma4 architecture yet — Qwen3.5 is the shipping model
- Don't add MTP to the default path — it's counterproductive at 4B scale
- Don't change the sprout-local provider endpoint (:18081)

## Build/verify after each phase
```bash
make build-all                                    # full build
go test ./pkg/localmodel/                          # package tests
SPROUT_PROVIDER=sprout-local sprout agent "hello"  # e2e test
```
