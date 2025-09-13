# V4 Streamlined System Prompt

The current production system prompt used by the agent.

## Source
This prompt is loaded from `pkg/agent/prompts/v4_streamlined.md` in the main codebase.

## Characteristics
- **Length**: ~3464 characters
- **Focus**: General-purpose AI assistant for code editing and software engineering
- **Includes**: Tool usage instructions, code style guidelines, debugging methodology
- **Target**: Works across all models but not optimized for any specific one

## Usage
This serves as the baseline for comparison against model-specific optimizations.

## Known Performance
- **GPT-5 Mini**: 8.6s response time, 1070 char responses
- **Qwen3 Coder Turbo**: 2.3s response time, 1273 char responses  
- **DeepSeek 3.1**: 13.2s response time, 1757 char responses

## Optimization Opportunities
- Could be made more concise for speed-focused models (Qwen3)
- Could include more detailed reasoning guidance for thorough models (DeepSeek)
- Could emphasize specific capabilities per model's strengths