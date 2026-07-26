/sprout-fix

## Pricing Audit Task

Verify that provider pricing in `pkg/agent_providers/configs/*.json` matches official docs. If you find discrepancies, update the config files and the manifest, then commit and push.

### Steps

1. Run the audit to see current drift:
   ```
   go run ./cmd/audit_pricing --configs-dir=pkg/agent_providers/configs
   ```

2. For each provider, fetch the official pricing page and compare:
   - **DeepSeek**: https://api-docs.deepseek.com/quick_start/pricing
   - **ZAI**: https://docs.z.ai/devpack/overview
   - **OpenAI**: https://platform.openai.com/docs/pricing
   - **MiniMax**: https://platform.minimax.io/docs/api-reference/api-overview
   - **Mistral**: https://docs.mistral.ai/

3. If pricing changed, update BOTH:
   - `cmd/audit_pricing/manifest.json` — update prices + set `last_verified` to today
   - `pkg/agent_providers/configs/*.json` — update `model_info[].input_cost`, `output_cost`, `cached_input_cost`
   - Top-level `cost.input_token_cost` / `cost.output_token_cost` should match the default model

4. Verify:
   ```
   go build ./cmd/audit_pricing/...
   go test ./cmd/audit_pricing/...
   go run ./cmd/audit_pricing --configs-dir=pkg/agent_providers/configs
   ```
   The audit should report 0 drift.

### Rules
- Only modify files in `pkg/agent_providers/configs/` and `cmd/audit_pricing/manifest.json`
- Only update pricing values — don't change model IDs, descriptions, or context lengths
- Prices are USD per 1M tokens
- If a provider page is unreachable, skip it and note it in the PR
