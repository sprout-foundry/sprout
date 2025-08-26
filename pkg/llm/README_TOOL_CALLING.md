# Flexible Tool Calling System

This system provides a flexible approach to handle tool calling across different LLM providers, addressing the discrepancy between providers that support native tool calling and those that require text-based fallbacks.

## How It Works

### Provider Capabilities

The system classifies providers into three categories:

1. **ToolCallingNone** - No native tool support, uses text-based fallbacks
2. **ToolCallingOpenAI** - Basic OpenAI-compatible function calling 
3. **ToolCallingAdvanced** - Advanced tool calling with custom schemas

### Default Provider Support

```go
ProviderToolSupport = map[string]ToolCallingCapability{
    "openai":     ToolCallingOpenAI,    // Native OpenAI function calling
    "groq":       ToolCallingOpenAI,    // OpenAI-compatible
    "gemini":     ToolCallingAdvanced,  // Advanced Gemini function calling
    "deepseek":   ToolCallingOpenAI,    // DeepSeek API supports OpenAI-compatible function calling
    "deepinfra":  ToolCallingOpenAI,    // DeepInfra supports OpenAI-compatible function calling
    "ollama":     ToolCallingNone,      // Local models - text fallback  
    "cerebras":   ToolCallingNone,      // No native tool support
    "lambda":     ToolCallingNone,      // No native tool support
}
```

### Model-Specific Overrides

Some models within a provider may differ from the provider default:

```go
ModelSpecificToolSupport = map[string]ToolCallingCapability{
    // Example: Override for models that don't support provider's default capability
    // "ollama:llama3.1:latest": ToolCallingOpenAI, // Specific Ollama model with tool support
    // "someProvider:legacy-model": ToolCallingNone, // Legacy model without tools
}
```

## Usage Examples

### Automatic Strategy Selection

The system automatically chooses the best approach:

```go
strategy := GetToolCallingStrategy("deepinfra:deepseek-ai/DeepSeek-V3-0324")
// Returns: UseNative=true, will use native OpenAI-compatible function calling

strategy := GetToolCallingStrategy("openai:gpt-4") 
// Returns: UseNative=true, will use native OpenAI function calling

strategy := GetToolCallingStrategy("ollama:llama3.2:latest")
// Returns: UseNative=false, will use text-based tool calling
```

### Runtime Configuration

You can override the default behavior:

```go
// Force a provider to use native tool calling
OverrideProviderToolSupport("deepinfra", ToolCallingOpenAI)

// Configure a specific model
OverrideModelToolSupport("deepinfra:new-model", ToolCallingOpenAI)
```

## Implementation Details

### Native Tool Calling Flow

1. Tools are formatted according to provider schema
2. Tools array is added to the LLM request 
3. Provider returns structured tool calls
4. System parses native tool call format

### Text-Based Fallback Flow

1. Tool descriptions are added to the system prompt
2. LLM generates JSON-formatted tool calls in text
3. System parses tool calls from the response text
4. Falls back to multiple parsing strategies if needed

### Request Format

**Native providers** (OpenAI, some DeepInfra models):
```json
{
  "model": "gpt-4",
  "messages": [...],
  "tools": [...],
  "tool_choice": "auto"
}
```

**Text fallback providers** (DeepSeek, most local models):
```json
{
  "model": "deepseek-chat", 
  "messages": [
    {
      "role": "system",
      "content": "Available tools: ..."
    },
    ...
  ]
}
```

## Benefits

1. **Reliability**: Uses native tool calling when available for better accuracy
2. **Compatibility**: Falls back to text parsing for providers without native support
3. **Flexibility**: Easy to add new providers or override behavior
4. **Performance**: Native tool calling is typically faster and more reliable
5. **Future-proof**: Easy to upgrade providers as they add tool calling support

## Adding New Providers

To add support for a new provider:

1. Add the provider to `ProviderToolSupport` with appropriate capability
2. If needed, implement custom tool formatting in `PrepareToolsForProvider`
3. If needed, implement custom parsing in `ParseToolCallsForProvider`
4. Test with both simple and complex tool calling scenarios

The system is designed to gracefully degrade to text-based tool calling for any unknown or misconfigured providers.