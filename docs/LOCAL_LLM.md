# Local LLM (MLX) — run sprout with a local model

Sprout can run a small language model **entirely on your Mac** via MLX (Apple's
unified-memory GPU framework) — no API key, no network calls, no cost. The
local model is served by a small OpenAI-compatible server and used through the
built-in `sprout-local` provider, so everything else (agent loop, tools,
subagents, commit messages) works exactly as with a cloud provider.

## Requirements

- Apple Silicon Mac (M1/M2/M3/M4), macOS 12+
- 8 GB RAM minimum; 16 GB recommended for the balanced 4B model
- `hf` CLI for model downloads (`pip install -U huggingface_hub`)
- MLX libraries (`brew install mlx` — see the gomlx module docs)

## Quick start

```bash
# 1. Download the recommended model for this machine's RAM
make local-model

# 2. Start the local LLM server (auto-selects the best installed model)
make local-llm

# 3. In another terminal, use sprout with the local provider
sprout agent --provider sprout-local "Summarize this repo"
```

`make local-model` inspects your RAM and downloads the right model:

| Machine RAM | Model | Notes |
|---|---|---|
| ≥ 30 GB | `Qwen3.5-9B-4bit` | Best quality; ~5.9 GB weights |
| ≥ 14 GB | `Qwen3.5-4B-4bit` | **Balanced choice for 16 GB machines**; ~2.8 GB weights |
| any | `Qwen3.5-0.8B-4bit` | Universal fallback; ~0.6 GB weights |

Models land in `~/dev/llm-models/` and the server auto-selects the largest one
that fits your RAM (skipping any that aren't installed).

## Manual control

```bash
# Build and run the server with explicit model + port
go build -tags mlx -o llm_server ./cmd/llm_server
./llm_server -model ~/dev/llm-models/qwen3.5-4b-4bit -port 18081
```

The server speaks the OpenAI chat-completions API on `http://127.0.0.1:18081`:

- `POST /v1/chat/completions` — JSON and SSE streaming
- `GET /v1/models` — model discovery (the provider uses this)
- `GET /health` — status check (`make local-llm-status`)

## Using sprout-local

The `sprout-local` provider is embedded in the sprout binary (no setup). It
points at `http://127.0.0.1:18081` and auto-discovers the model the server
loaded, so the same config works whether you're on an 8 GB or 32 GB machine.

```bash
sprout agent --provider sprout-local "your task here"
# or make it the default:
sprout config set provider sprout-local   # if supported
```

The connection check sends a tiny request on startup; the server caps
`max_tokens` (default 512) so a connection check can't trigger a long
generation on a memory-constrained machine.

## Model catalog

The catalog lives in `pkg/gomlx/llm/catalog.go`. To add a model:

1. Download it (mlx-community quantized layout) into `~/dev/llm-models/`
2. Add one entry: `{Name, Dir, HFRepo, MinRAM}`
3. Selection, memory gating, and provider discovery follow automatically

## Memory behavior

The LLM path applies the SP-134 memory protections:

- **RAM gate** — refuses to load a model whose weights are ≥ 50% of physical RAM
- **MLX memory limit** — allocation failures surface as errors instead of
  blocking the process in a Metal cond wait
- **Cache limit + trim** — pooled buffers return to the OS between requests
- **max_tokens cap** — no runaway generations on small machines

If a request would exhaust memory, the server returns an HTTP error instead of
hanging — reduce the model size (e.g. 4B → 0.8B) or raise `-max-tokens`.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `no model from catalog fits` | Run `make local-model` to download one |
| `hf not found` | `pip install -U huggingface_hub` |
| Connection refused on 18081 | `make local-llm-status`; start with `make local-llm` |
| Generation slow / swap-heavy | Use a smaller model (`-model ~/dev/llm-models/qwen3.5-0.8b-4bit`) |
| Server returns memory-limit error | Close other apps; use a smaller model |
