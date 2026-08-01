## Pricing & Model Audit Task

Keep provider pricing and model lists current. This means discovering new
models from live APIs, adding them to configs, verifying pricing against
official docs, and flagging stale models for removal.

### Step 1 — Discover and auto-populate new models

Run discovery to find models in the live API that are missing from configs:

```
go run ./cmd/audit_pricing --discover --update \
  --configs-dir=pkg/agent_providers/configs \
  --manifest-path=cmd/audit_pricing/manifest.json
```

This calls each provider's live `/models` endpoint, compares against the
embedded configs, and auto-adds new models with pricing + context + capabilities
from the API (or OpenRouter as a reference). It also adds pricing entries to
the manifest for each new model.

**Review what was auto-populated.** API-returned pricing is usually correct
for OpenRouter, DeepInfra, and Chutes (they expose it directly). For other
providers, pricing may be $0 or estimated — you need to verify in Step 2.

### Step 2 — Verify pricing against official docs

For each provider, fetch the official pricing page and compare:

| Provider | Pricing URL |
|---|---|
| DeepSeek | https://api-docs.deepseek.com/quick_start/pricing |
| ZAI | https://docs.z.ai/devpack/overview |
| OpenAI | https://platform.openai.com/docs/pricing |
| MiniMax | https://platform.minimax.io/docs/guides/pricing-paygo |
| Mistral | https://docs.mistral.ai/ |
| OpenRouter | https://openrouter.ai/models (pricing is per-model on each model card) |
| DeepInfra | https://deepinfra.com/models (pricing is per-model on each model card) |
| Cerebras | https://cerebras.ai/inference |
| Chutes | https://chutes.ai/pricing |
| Ollama Cloud | https://ollama.com/pricing |

If pricing changed, update BOTH:
- `cmd/audit_pricing/manifest.json` — update prices + set `last_verified` to today
- `pkg/agent_providers/configs/*.json` — update `model_info[].input_cost`, `output_cost`, `cached_input_cost`
- Top-level `cost.input_token_cost` / `cost.output_token_cost` should match the default model

### Step 3 — Fix model metadata for newly added models

Auto-discovered models get minimal metadata from the API. For important
models (flagship, commonly used), enrich:
- `description` — a short human-readable description
- `tags` — capabilities like `["tools"]`, `["vision"]`, `["reasoning"]`
- `context_length` — if the API returned 0, look up the correct value
- `name` — display name if the API only returned the ID

Don't spend time on obscure/little-used models — focus on flagship and
recently-released models that users would select.

### Step 4 — Flag stale models

The discovery report shows models in config that are NOT in the live API.
These may be deprecated or renamed. For each:
- If the provider's deprecation page confirms it, remove it from config and manifest
- If uncertain, leave it — false removals break users

### Step 5 — Verify

```
go build ./cmd/audit_pricing/...
go test ./cmd/audit_pricing/...
go run ./cmd/audit_pricing --configs-dir=pkg/agent_providers/configs
```

The audit should report 0 drift. Discovery may still show new/stale models
if providers added models between the discover step and now — that's fine.

### Allowed files

You may modify:
- `pkg/agent_providers/configs/*.json` — all provider config files
- `cmd/audit_pricing/manifest.json` — pricing manifest
- `cmd/audit_pricing/manifest.go` — if structural changes are needed
- `cmd/audit_pricing/*.go` — audit tool source code if you find bugs

### Rules
- Prices are USD per 1M tokens
- If a provider page is unreachable, skip it and note it in the PR
- Don't remove models unless you're confident they're deprecated
- Don't change `endpoint`, `auth`, `retry`, or `streaming` config sections
