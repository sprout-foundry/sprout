# TODO: Future Improvements

## Provider Architecture

### Refactor Provider Interface (Medium Priority) ⚠️
**Status**: Analysis Complete - No Action Needed

**Current State**:
The Provider interface in `pkg/agent_api/provider_interface.go` has 15 methods, which is borderline but still manageable. The interface is already well-organized with:
- Clear method grouping (Core, Model management, Provider info, Features, Config)
- A BaseProvider that implements common functionality
- Good separation between required and optional features

**Assessment**:
While the interface could be split into smaller interfaces as originally proposed, doing so would:
1. Break backward compatibility with all existing provider implementations
2. Require significant refactoring across the codebase
3. Add complexity without significant immediate benefit

**Recommendation**: Keep as-is
The current interface is functional and not causing immediate problems. Consider interface segregation only if:
- New provider types emerge that only need a subset of functionality
- Testing becomes difficult due to interface size
- The interface grows beyond 20+ methods

**Alternative Approach**:
If refactoring becomes necessary in the future, use adapter pattern:
- Keep existing Provider interface for compatibility
- Create smaller interfaces for new implementations
- Use adapters to bridge between old and new interfaces

### Replace String-based Model Detection (Low Priority) ✅
**Status**: Partially Complete

**What Was Done**:
1. ✅ Model registry already exists (`model_registry.go`)
2. ✅ Updated `provider_openai.go` to use registry for:
   - `GetModelContextLimit()` - Now uses `registry.GetModelContextLength()`
   - `EstimateCost()` - Now uses `registry.GetModelPricing()`
3. ✅ Updated `DeepInfraClientWrapper` in `interface.go` to use registry

**Model Registry Features**:
- Pattern-based matching with priority levels
- Comprehensive OpenAI model database
- Extensible design for adding new providers
- Unified pricing (per 1M tokens) and context limits

**Remaining String-Based Detection**:
Some string matching remains in:
- Provider-specific context limit fallbacks (when API calls fail)
- Feature detection (vision, reasoning capabilities)
- Model family identification for behavior adjustments

**Decision**: Current Implementation is Sufficient
The model registry provides the core functionality needed:
- Central source of truth for model metadata
- Pattern-based fallback for new models
- Easy to extend with new models/providers

The remaining string-based checks are mostly in provider-specific code where they serve as appropriate fallbacks or feature detection that doesn't warrant registry complexity.

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
- GitHub action scripts may set individual model fields but not `provider_models`
- This used to cause unexpected fallback to featured models (now fixed)

**Solution Implemented**:
The --provider flag has been added to the agent command, allowing explicit provider selection:
```bash
ledit agent --provider "$AI_PROVIDER" --model "$AI_MODEL" "$PROMPT"
```

**Recommended GitHub Action Configuration**:
For GitHub actions, use command-line flags instead of config files:
```yaml
- name: Run Ledit Agent
  run: |
    ledit agent --provider "${{ inputs.ai-provider }}" \
                --model "${{ inputs.ai-model }}" \
                "${{ inputs.prompt }}"
```

This approach:
- Avoids config file generation issues
- Makes provider/model selection explicit
- Works reliably in CI/CD environments
- Overrides any existing config settings

**If Config File Generation is Still Needed**:
Ensure the config properly sets provider_models:
```json
{
  "provider_models": {
    "openrouter": "deepseek/deepseek-chat-v3.1:free",
    "openai": "gpt-4o-mini"
  },
  "last_used_provider": "openrouter"
}
```

### Consolidate Provider Detection ✅
**Issue**: Multiple places determine which provider to use with different logic.

**Solution Implemented**:
Created unified `DetermineProvider()` function with clear precedence:
1. Command-line flag (explicit provider string)
2. Environment variable (LEDIT_PROVIDER)
3. Config file (last_used_provider)
4. First available provider from priority list
5. Fallback to Ollama

**Changes Made**:
- Added `DetermineProvider()` function in interface.go
- Added `parseProviderName()` for consistent provider name handling
- Added `IsProviderAvailable()` for unified availability checking
- Updated `GetBestProvider()` to use the new unified function
- Added `GetProviderWithExplicit()` for command-line override support
- Deprecated `GetClientTypeFromEnv()` in favor of unified approach

**Benefits**:
- Single source of truth for provider selection logic
- Clear, documented precedence order
- Consistent provider name parsing
- Reusable availability checking
- Works seamlessly with --provider flag

### Remove Model Aliasing and Normalization ✅
**Issue**: Complex model name handling adds confusion.

**Status**: Partially Complete
- ✅ Featured models implicit aliases removed
- ✅ Clear canonical model names established (exact provider names)
- ⚠️  Pricing service still normalizes for lookup (case-insensitive)

**Remaining Normalization**:
The pricing service normalizes model names for case-insensitive lookup:
- `normalizeModelKey()` lowercases and removes provider prefixes
- `normalizePricingKeys()` lowercases all pricing table keys

**Decision**: Keep pricing normalization
This normalization is actually beneficial for pricing lookup because:
- Model names may vary in casing across providers
- Pricing should work regardless of exact casing
- It's isolated to pricing logic and doesn't affect model selection

**What Was Fixed**:
- Removed featured models that created implicit aliases
- Model selection now uses exact names from providers
- No more string manipulation for model detection in selection logic

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