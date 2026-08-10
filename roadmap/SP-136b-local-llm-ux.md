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

| Model | Dir | HF Repo | Min RAM | Status |
|-------|-----|---------|---------|--------|
| Qwen3.5-0.8B | qwen3.5-0.8b-4bit | mlx-community/Qwen3.5-0.8B-4bit | 0 GB | Downloaded |
| Qwen3.5-2B | qwen3.5-2b-4bit | mlx-community/Qwen3.5-2B-MLX-4bit | 8 GB | Downloaded |
| Qwen3.5-4B | qwen3.5-4b-4bit | mlx-community/Qwen3.5-4B-4bit | 14 GB | Downloaded |
| Qwen3.5-9B | qwen3.5-9b-4bit | mlx-community/Qwen3.5-9B-4bit | 30 GB | Not downloaded |

Default on 16GB M1 Pro: **Qwen3.5-4B** (4-bit, ~2.5GB GPU memory).

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
