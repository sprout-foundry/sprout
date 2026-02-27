# Model Settings Registry

This package applies model tuning settings independent of serving provider.

## Goal

Use a bounded model list (OpenRouter catalog), then resolve parameter defaults by precedence:

1. explicit request override
2. creator recommendation (exact model)
3. creator recommendation (model family)
4. third-party fallback (OpenRouter model defaults)
5. provider config fallback

If a parameter is undefined by creator docs, it remains unset (`null` / omitted).

## Files

- `openrouter_model_settings.json`: bounded model catalog snapshot from OpenRouter.
- `creator_recommendations.json`: curated creator-sourced recommendations and family rules.
- `settings.go`: resolver, normalization, and precedence merge.

## Priority open-model families

Current creator-priority profiles focus on:

- DeepSeek (`deepseek-*`, plus `deepseek-r1*` overrides)
- MiniMax (`minimax-m2*`)
- Qwen (`qwen3*`, `qwen2.5*`, `qwen2.5-coder*`)
- ZAI GLM (exact creator-backed rules currently for `glm-4.6` and `glm-4-9b-chat`)

Gemma currently has no accessible creator-published sampling defaults in this environment; values remain unset unless explicit overrides are provided.

## Normalization

Model names are normalized to base model IDs:

- strips provider prefix (`openai/gpt-oss-20b` -> `gpt-oss-20b`)
- strips OpenRouter variants (`:free`, `:nitro`, etc.)
- strips common quant suffixes (`_Q4`, `-int8`, `-gguf`, etc.)

This makes variants like `gpt-oss-20b_Q4` resolve to the same recommendation profile as `openai/gpt-oss-20b`.

## Refresh OpenRouter catalog

```bash
curl -s https://openrouter.ai/api/v1/models > /tmp/openrouter_models.json
jq -c '{
  generated_at: (now|todate),
  source: "https://openrouter.ai/api/v1/models",
  model_count: (.data|length),
  models: [.data[] | {
    id: .id,
    slug: (.id|split("/")[1]),
    supported_parameters: .supported_parameters,
    default_parameters: {
      temperature: .default_parameters.temperature,
      top_p: .default_parameters.top_p,
      frequency_penalty: .default_parameters.frequency_penalty
    }
  }]
}' /tmp/openrouter_models.json > pkg/model_settings/openrouter_model_settings.json
```
