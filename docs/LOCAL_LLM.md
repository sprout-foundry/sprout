# Local LLM (MLX) — run sprout with a local model

Sprout can run a small language model **entirely on your Mac** via MLX (Apple's
unified-memory GPU framework) — no API key, no network calls after setup, no
cost. The model runs inside sprout through the built-in `sprout-local`
provider, so everything else (agent loop, tools, subagents, commit messages)
works exactly as with a cloud provider.

## Requirements

- Apple Silicon Mac (M1 or later)
- 8 GB RAM minimum; more RAM unlocks larger models
- Nothing else: the release binary downloads the MLX runtime (about 40 MB)
  the first time you set up a local model. If Homebrew's `mlx-c` is
  installed, sprout uses that instead.

## Quick start

Run `sprout`, choose **Local (Offline)** in the setup wizard, and pick a model (the
wizard recommends one for your RAM). Sprout downloads the runtime and the
model, then restarts itself on the local model.

Later, switch to a local model from any session with `/models`; if the
runtime was missing it is downloaded too, and local models work after you
restart sprout.

Models and the runtime live in `~/.sprout-local/` (`models/` and
`mlx-runtime/`). Delete that directory to remove them.

## How the runtime is found

MLX is loaded once when sprout starts, from the first of: `$MLX_C_LIB`, a
`libmlxc.dylib` next to the sprout binary, Homebrew's `mlx-c`. When none of
those load and `~/.sprout-local/mlx-runtime` exists, sprout restarts itself
once with `MLX_C_LIB` pointing there.

The runtime archive is built by `scripts/package-mlx-runtime.sh` during each
release and verified against the release's `SHA256SUMS` before install.

## Standalone server (contributors)

Building from source, you can also run the model as a separate
OpenAI-compatible server:

```bash
make local-model   # download the recommended model for this machine
make local-llm     # serve it on http://127.0.0.1:18081
```

- `POST /v1/chat/completions` — JSON and SSE streaming
- `GET /v1/models` — model discovery
- `GET /health` — status check (`make local-llm-status`)

The server caps `max_tokens` (default 512) so a connection check can't
trigger a long generation on a memory-constrained machine.

## Model catalog

The catalog lives in sinter's `llm/catalog/catalog.go`
(`github.com/sprout-foundry/sinter/llm/catalog`). To add a model:

1. Download it (mlx-community quantized layout) into your models root
   (`~/.sprout-local/models` by default)
2. Add one entry to `ModelCatalog`:
   `{Name, Dir, HFRepo, MinRAMSelect, MinRAMSuggested}`
3. Selection, memory gating, and provider discovery follow automatically

## Memory behavior

The LLM path applies the SP-134 memory protections:

- **RAM gate** — refuses to load a model whose weights are ≥ 50% of physical RAM
- **MLX memory limit** — allocation failures surface as errors instead of
  blocking the process in a Metal cond wait
- **Cache limit + trim** — pooled buffers return to the OS between requests
- **max_tokens cap** — no runaway generations on small machines

If a request would exhaust memory, it fails with an error instead of
hanging — reduce the model size (e.g. 4B → 0.8B).

## Troubleshooting

| Symptom | Fix |
|---|---|
| `no model from catalog fits` | Run `/models` and pick a model that fits your RAM |
| Local AI missing from setup | Needs an Apple Silicon Mac and the macOS arm64 release binary |
| Runtime download fails | Check network access to github.com, then retry setup |
| Generation slow / swap-heavy | Use a smaller model (e.g. 4B → 0.8B) |
| Memory-limit error | Close other apps; use a smaller model |
