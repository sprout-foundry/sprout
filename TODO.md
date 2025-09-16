# TODO: Future Improvements

## Provider Architecture

### Refactor Provider Interface (Medium Priority)
**Issue**: The Provider interface has too many methods (14+), violating the Interface Segregation Principle.

**Current State**:
```go
type Provider interface {
    SendChatRequest(...)
    CheckConnection(...)
    GetModel()
    SetModel(...)
    GetAvailableModels(...)
    GetModelContextLimit()
    GetName()
    GetType()
    GetEndpoint()
    SupportsVision()
    SupportsTools()
    SupportsStreaming()
    SupportsReasoning()
    SetDebug(...)
    IsDebug()
}
```

**Proposed Solution**:
Split into smaller, focused interfaces:
```go
type ChatProvider interface {
    SendChatRequest(ctx context.Context, req *ProviderChatRequest) (*ChatResponse, error)
}

type ModelProvider interface {
    GetModel() string
    SetModel(model string) error
    GetAvailableModels(ctx context.Context) ([]ModelDetails, error)
    GetModelContextLimit() (int, error)
}

type FeatureProvider interface {
    SupportsVision() bool
    SupportsTools() bool
    SupportsStreaming() bool
    SupportsReasoning() bool
}

type Provider interface {
    ChatProvider
    ModelProvider
    FeatureProvider
    GetName() string
    GetType() ClientType
    // ... other core methods
}
```

**Benefits**:
- Easier to test individual aspects
- Providers can implement only what they need
- Better separation of concerns

### Replace String-based Model Detection (Low Priority)
**Issue**: Using `strings.Contains()` for model detection is fragile and error-prone.

**Current Examples**:
```go
// In provider_openai.go
if strings.Contains(model, "gpt-5") {
    return 272000, nil
}
if strings.Contains(model, "o3-mini") {
    return 200000, nil
}
```

**Proposed Solution**:
Create a model registry with structured metadata:
```go
type ModelMetadata struct {
    ID            string
    Family        string
    Version       string
    ContextLimit  int
    Features      []string
    Pricing       PricingInfo
}

type ModelRegistry struct {
    models map[string]ModelMetadata
}

func (r *ModelRegistry) GetModelInfo(modelID string) (ModelMetadata, bool) {
    // Exact match first
    if info, ok := r.models[modelID]; ok {
        return info, true
    }
    // Fallback to pattern matching if needed
    // ...
}
```

**Implementation Steps**:
1. Create ModelMetadata and ModelRegistry types
2. Build registry from existing model data
3. Replace all string.Contains checks with registry lookups
4. Add fallback logic for unknown models
5. Make registry configurable/extensible

**Benefits**:
- Type-safe model information
- Easier to maintain and update
- Better support for model aliases and versions
- Can be extended with additional metadata

## Anonymous Structs in Provider Responses

### Context
The provider implementations use anonymous structs that match the existing `ChatResponse` structure. Changing these would require updating the core response types across the codebase.

**Current State**:
```go
Usage: struct {
    PromptTokens        int     `json:"prompt_tokens"`
    CompletionTokens    int     `json:"completion_tokens"`
    TotalTokens         int     `json:"total_tokens"`
    EstimatedCost       float64 `json:"estimated_cost"`
    PromptTokensDetails struct {
        CachedTokens     int  `json:"cached_tokens"`
        CacheWriteTokens *int `json:"cache_write_tokens"`
    } `json:"prompt_tokens_details,omitempty"`
}{
    // field assignments
}
```

**Recommendation**: 
Keep as-is for now. These anonymous structs match the existing `ChatResponse` type definition in `client.go`. Changing them would require a larger refactor of the response types across all providers and consumers. The current approach maintains compatibility while being explicit about the structure.

## Model Selection and Configuration Cleanup (HIGH PRIORITY)

### Remove Featured Models Concept
**Issue**: The "featured models" concept adds unnecessary complexity without providing real value.

**Current Problems**:
- Featured models are used as defaults when provider_models config is missing
- Leads to unexpected model substitutions (e.g., `qwen3-coder-30b` → `qwen3-coder:free`)
- Duplicates functionality that should be in configuration
- Creates brittle dependencies between provider implementations and defaults

**Action Items**:
1. Remove `GetFeaturedModels()` from all providers
2. Remove `GetFeaturedModelsForProvider()` from interface  
3. Remove featured model logic from `GetDefaultModelForProvider()`
4. Remove featured models display from CLI commands (`cmd/agent.go`)
5. Replace with simple, explicit defaults per provider
6. Update model selection UI to show all models equally

### Simplify Model Configuration
**Issue**: Model configuration is scattered across multiple places with unclear precedence.

**Current Problems**:
- Config can set individual model fields (editing_model, agent_model, etc.)
- Config can set provider_models map
- Defaults come from featured models (first in list)
- Agent can be created with explicit model override
- No clear documentation of precedence

**Proposed Solution**:
```go
// Single source of truth for model configuration
type ModelConfig struct {
    // Provider-specific models (highest priority)
    ProviderModels map[string]string `json:"provider_models"`
    
    // Role-specific overrides (optional)
    RoleModels map[string]string `json:"role_models,omitempty"`
    
    // Default provider
    DefaultProvider string `json:"default_provider"`
}

// Clear precedence:
// 1. Explicit model passed to agent creation
// 2. provider_models[provider] from config
// 3. Simple hardcoded default for provider
```

### Fix Config File Generation
**Issue**: GitHub action creates incomplete config that causes model fallback.

**Current Problem**:
- `run-ledit.sh` sets individual model fields but not `provider_models`
- This causes unexpected fallback to featured models

**Fix Options**:
1. Update config generation to properly set provider_models:
```json
{
  "provider_models": {
    "$AI_PROVIDER": "$AI_MODEL"
  },
  "last_used_provider": "$AI_PROVIDER"
}
```

2. OR use the --model command-line flag with provider prefix:
```bash
ledit agent --model "$AI_PROVIDER:$AI_MODEL" "$PROMPT"
```

3. BETTER: Add --provider flag to agent command and use both:
```bash
ledit agent --provider "$AI_PROVIDER" --model "$AI_MODEL" "$PROMPT"
```

The command-line flag approach is simpler and avoids config file issues entirely, but
requires adding the missing --provider flag for clarity.

### Consolidate Provider Detection
**Issue**: Multiple places determine which provider to use with different logic.

**Current Problems**:
- `GetBestProvider()` has complex fallback logic
- `GetClientTypeFromEnv()` duplicates provider detection
- No clear hierarchy of configuration sources

**Proposed Solution**:
Single function with clear precedence:
1. Command-line flag (--model with provider prefix, e.g., "openrouter:model-name")
2. Environment variable (LEDIT_PROVIDER)
3. Config file (last_used_provider)
4. First available provider from priority list

**Note**: Currently there's a --model flag but no --provider flag. The model flag can include
provider prefix (e.g., "openrouter:qwen/qwen3-coder-30b") but this is:
- Undocumented
- Conflates provider and model selection
- Falls back to unreliable "best provider" logic without prefix

**Recommendation**: Add explicit --provider flag to agent command:
```bash
ledit agent --provider openrouter --model "qwen/qwen3-coder-30b-a3b-instruct"
```

### Remove Model Aliasing and Normalization
**Issue**: Complex model name handling adds confusion.

**Current Problems**:
- Model names are normalized in pricing service
- Featured models create implicit aliases
- No clear canonical model names
- String manipulation for model detection

**Proposed Solution**:
- Use exact model names as provided by each service
- No aliasing or normalization
- Clear documentation of valid model names per provider
- Validation against known model list

## Additional Improvements

### Add Provider Health Checks
- Implement circuit breaker pattern for provider failures
- Add health check endpoints
- Monitor provider availability and performance

### Implement Streaming Support
- Add streaming methods to Provider interface
- Implement for providers that support it
- Handle partial responses and errors

### Add Metrics Collection
- Track provider response times
- Monitor token usage and costs
- Create dashboards for visibility

### Improve Error Handling
- Create provider-specific error types
- Add retry logic with exponential backoff
- Better error messages for users