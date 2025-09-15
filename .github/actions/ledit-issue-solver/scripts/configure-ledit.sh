#!/bin/bash
set -e

echo "Configuring ledit..."

# Create ledit config directory
mkdir -p ~/.ledit

# Map provider names to environment variable names
case "$AI_PROVIDER" in
    openai)
        API_KEY_NAME="OPENAI_API_KEY"
        ;;
    openrouter)
        API_KEY_NAME="OPENROUTER_API_KEY"
        ;;
    groq)
        API_KEY_NAME="GROQ_API_KEY"
        ;;
    deepinfra)
        API_KEY_NAME="DEEPINFRA_API_KEY"
        ;;
    ollama)
        API_KEY_NAME="" # Ollama doesn't need API key
        ;;
    cerebras)
        API_KEY_NAME="CEREBRAS_API_KEY"
        ;;
    deepseek)
        API_KEY_NAME="DEEPSEEK_API_KEY"
        ;;
    *)
        echo "ERROR: Unknown AI provider: $AI_PROVIDER"
        exit 1
        ;;
esac

# Create API keys file
cat > ~/.ledit/api_keys.json << EOF
{
  "openai_api_key": "${AI_PROVIDER == 'openai' && echo $AI_API_KEY || echo ''}",
  "openrouter_api_key": "${AI_PROVIDER == 'openrouter' && echo $AI_API_KEY || echo ''}",
  "groq_api_key": "${AI_PROVIDER == 'groq' && echo $AI_API_KEY || echo ''}",
  "deepinfra_api_key": "${AI_PROVIDER == 'deepinfra' && echo $AI_API_KEY || echo ''}",
  "cerebras_api_key": "${AI_PROVIDER == 'cerebras' && echo $AI_API_KEY || echo ''}",
  "deepseek_api_key": "${AI_PROVIDER == 'deepseek' && echo $AI_API_KEY || echo ''}"
}
EOF

# Create configuration file
cat > ~/.ledit/config.json << EOF
{
  "editing_model": "$AI_MODEL",
  "agent_model": "$AI_MODEL",
  "workspace_model": "$AI_MODEL",
  "orchestration_model": "$AI_MODEL",
  "summarization_model": "$AI_MODEL",
  "code_style": "Follow the existing code style and patterns in the repository. Maintain consistency with surrounding code.",
  "max_iterations": $MAX_ITERATIONS,
  "provider": "$AI_PROVIDER"
}
EOF

echo "Ledit configured with:"
echo "  Provider: $AI_PROVIDER"
echo "  Model: $AI_MODEL"
echo "  Max iterations: $MAX_ITERATIONS"