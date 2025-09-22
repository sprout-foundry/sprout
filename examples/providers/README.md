# Provider Configuration Examples

This directory contains example provider configurations that demonstrate how to set up different types of LLM providers with the new provider registry system.

## Quick Start

1. Copy any example configuration to your `~/.ledit/providers/` directory:
   ```bash
   mkdir -p ~/.ledit/providers
   cp examples/providers/custom-llm.yaml ~/.ledit/providers/my-provider.yaml
   ```

2. Edit the configuration to match your provider's settings

3. Set the required API key environment variable:
   ```bash
   export CUSTOM_LLM_API_KEY="your-api-key"
   ```

4. Use the provider:
   ```bash
   ledit agent --provider custom-llm
   ```

## Configuration Format

All provider configurations use YAML format with the following structure:

```yaml
# Provider type (unique identifier)
type: provider-name

# Human-readable name
display_name: "Provider Display Name"

# OpenAI-compatible API endpoint
base_url: "https://api.provider.com/v1/chat/completions"

# Environment variable containing the API key
api_key_env: "PROVIDER_API_KEY"

# Whether an API key is required
api_key_required: true

# Feature support flags
features:
  vision: false     # Image/vision capabilities
  tools: true       # Function calling support
  streaming: true   # Streaming response support
  reasoning: false  # Reasoning mode support
  audio: false      # Audio capabilities

# Default model to use
default_model: "model-name"

# Request timeout
default_timeout: "2m"

# Optional: Cost configuration for usage tracking
cost_config:
  type: "tiered"  # or "flat"
  tiers:
    - model_pattern: "*"  # or specific model name
      input_per_1m: 0.0   # Cost per 1M input tokens
      output_per_1m: 0.0  # Cost per 1M output tokens

# Optional: Additional HTTP headers
extra_headers:
  "X-Custom-Header": "value"
```

## Available Examples

- `openai.yaml` - OpenAI provider configuration
- `custom-llm.yaml` - Generic OpenAI-compatible provider template

## Notes

- All providers using the `generic-openai` implementation automatically support OpenAI-compatible APIs
- The system will automatically fall back to the generic provider for unknown provider types
- Provider configurations in `~/.ledit/providers/` override built-in defaults
- Multiple configuration directories are supported (project-level and user-level)