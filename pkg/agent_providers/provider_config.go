package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

const (
	BillingPayPerToken  = "pay_per_token" // default — real USD per token
	BillingSubscription = "subscription"  // flat-rate, quota/rate-limited
	BillingFree         = "free"          // self-hosted, zero marginal cost
)

// ProviderConfig defines the configuration for a generic provider
type ProviderConfig struct {
	Name        string `json:"name"`
	BillingType string `json:"billing_type,omitempty"`
	// DisplayName is the user-facing label (e.g. "GLM Coding Plan").
	// Carried in the JSON config so onboarding menus, the env-var
	// credential sweep, and the model picker can label remote-only
	// providers (published to GitHub Pages but not embedded) without
	// a binary rebuild. Optional — callers should fall back to the
	// static knownProviderDisplayNames map and then the raw Name.
	DisplayName string            `json:"display_name,omitempty"`
	Endpoint    string            `json:"endpoint"`
	Auth        AuthConfig        `json:"auth"`
	Headers     map[string]string `json:"headers"`
	Defaults    RequestDefaults   `json:"defaults"`
	Conversion  MessageConversion `json:"message_conversion"`
	Streaming   StreamingConfig   `json:"streaming"`
	Models      ModelConfig       `json:"models"`
	Retry       RetryConfig       `json:"retry"`
	Cost        CostConfig        `json:"cost"`
}

// AuthConfig defines authentication configuration
type AuthConfig struct {
	Type   string `json:"type"`    // "bearer", "api_key", "basic", "oauth"
	EnvVar string `json:"env_var"` // Environment variable containing the auth token
	Key    string `json:"-"`       // Runtime-only API key; injected at startup, never persisted
}

// RequestDefaults defines default request parameters
type RequestDefaults struct {
	Model       string                 `json:"model"`
	Temperature *float64               `json:"temperature"`
	MaxTokens   *int                   `json:"max_tokens"`
	TopP        *float64               `json:"top_p"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"` // Provider-specific parameters
}

// MessageConversion defines how messages should be converted
type MessageConversion struct {
	IncludeToolCallID        bool   `json:"include_tool_call_id"`
	ConvertToolRoleToUser    bool   `json:"convert_tool_role_to_user"`
	ReasoningContentField    string `json:"reasoning_content_field"`
	ArgumentsAsJSON          bool   `json:"arguments_as_json"`
	SkipToolExecutionSummary bool   `json:"skip_tool_execution_summary"` // For providers with strict role alternation
	ForceToolCallType        string `json:"force_tool_call_type"`        // Force tool call type to specific value (e.g., "function" for Mistral)
	// CacheControl enables provider prompt-prefix caching (Anthropic-style
	// cache_control: {type: "ephemeral"} markers). When true, cache breakpoints
	// are injected at three locations:
	//   1. The system message (static prefix)
	//   2. The last tool definition (static tool schema)
	//   3. The last conversation message (growing conversation prefix — highest impact)
	// Anthropic allows up to 4 breakpoints; we use 3, leaving headroom for future use.
	CacheControl bool `json:"cache_control,omitempty"`
}

// StreamingConfig defines streaming behavior
type StreamingConfig struct {
	Format         string `json:"format"` // "sse", "json_lines", "raw"
	ChunkTimeoutMs int    `json:"chunk_timeout_ms"`
	DoneMarker     string `json:"done_marker"`
}

// PatternOverride defines context limit overrides for model patterns
type PatternOverride struct {
	Pattern      string `json:"pattern"`
	ContextLimit int    `json:"context_limit"`
}

// ModelInfo represents information about a model (simplified version for config)
type ModelInfo struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	ContextLength int      `json:"context_length"`
	Tags          []string `json:"tags,omitempty"`
	// Pricing (USD per million tokens) — optional, used by enrich_registry
	// to estimate probe cost for models sourced from embedded configs.
	InputCost  float64 `json:"input_cost,omitempty"`
	OutputCost float64 `json:"output_cost,omitempty"`
	CachedCost float64 `json:"cached_input_cost,omitempty"`
}

// ModelConfig defines model-related configuration
type ModelConfig struct {
	DefaultContextLimit        int `json:"default_context_limit"`
	DefaultMaxCompletionTokens int `json:"default_max_completion_tokens,omitempty"`
	// DefaultModelPatterns defines preference patterns for auto-selecting a default model.
	// Patterns are tried in order; the first model whose ID contains all substrings in a pattern wins.
	// Example: []string{"deepseek.*instruct", "deepseek", "llama"}
	DefaultModelPatterns       []string          `json:"default_model_patterns,omitempty"`
	ModelOverrides             map[string]int    `json:"model_overrides"`
	MaxCompletionOverrides     map[string]int    `json:"max_completion_overrides,omitempty"`
	PatternOverrides           []PatternOverride `json:"pattern_overrides"`
	CompletionPatternOverrides []PatternOverride `json:"completion_pattern_overrides,omitempty"`
	// Config-based model definitions (fallback when endpoint fetch fails or lacks details)
	ModelInfo []ModelInfo `json:"model_info,omitempty"`
	// Legacy fields for backward compatibility
	ContextLimit    int      `json:"context_limit,omitempty"`
	SupportsVision  bool     `json:"supports_vision"`
	VisionModel     string   `json:"vision_model"`
	DefaultModel    string   `json:"default_model"`
	AvailableModels []string `json:"available_models"`
}

// RetryConfig defines retry behavior
type RetryConfig struct {
	MaxAttempts       int      `json:"max_attempts"`
	BaseDelayMs       int      `json:"base_delay_ms"`
	BackoffMultiplier float64  `json:"backoff_multiplier"`
	MaxDelayMs        int      `json:"max_delay_ms"`
	RetryableErrors   []string `json:"retryable_errors"`
}

// CostConfig defines cost tracking configuration
type CostConfig struct {
	InputTokenCost  float64 `json:"input_token_cost"`
	OutputTokenCost float64 `json:"output_token_cost"`
	Currency        string  `json:"currency"`
}

// ProviderRegistry holds all provider configurations
type ProviderRegistry struct {
	DefaultProvider  string                    `json:"default_provider"`
	EnabledProviders []string                  `json:"enabled_providers"`
	ProviderConfigs  map[string]ProviderConfig `json:"provider_configs"`
}

// LoadProviderConfig loads a provider configuration from a JSON file
func LoadProviderConfig(configPath string) (*ProviderConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, agenterrors.NewConfig(fmt.Sprintf("failed to read provider config file %s", configPath), err)
	}

	var config ProviderConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, agenterrors.NewConfig(fmt.Sprintf("failed to parse provider config file %s", configPath), err)
	}

	return &config, nil
}

// LoadProviderRegistry loads the provider registry from a JSON file
func LoadProviderRegistry(registryPath string) (*ProviderRegistry, error) {
	data, err := os.ReadFile(registryPath)
	if err != nil {
		return nil, agenterrors.NewConfig(fmt.Sprintf("failed to read provider registry file %s", registryPath), err)
	}

	var registry ProviderRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, agenterrors.NewConfig(fmt.Sprintf("failed to parse provider registry file %s", registryPath), err)
	}

	return &registry, nil
}

// GetAuthToken retrieves the authentication token based on the auth configuration.
//
// For "bearer" and "api_key" auth types, token resolution follows this precedence:
//  1. Auth.Key — runtime-resolved key injected by the provider factory via the
//     unified credential path (credentials.ResolveProvider). This is the primary
//     source because it checks env vars, keyring, and the encrypted file store.
//  2. Auth.EnvVar — direct os.Getenv lookup as a fallback (only used when the
//     factory did not pre-resolve credentials, e.g., in unit tests).
//
// Auth.Key is runtime-only and must never be persisted to disk.
func (c *ProviderConfig) GetAuthToken() (string, error) {
	switch c.Auth.Type {
	case "none":
		// No authentication required
		return "", nil
	case "bearer", "api_key":
		// Priority 1: Runtime-resolved key (injected by factory via unified credential path)
		if c.Auth.Key != "" {
			return c.Auth.Key, nil
		}
		// Priority 2: Direct env var lookup (fallback for non-factory paths)
		if c.Auth.EnvVar != "" {
			token := os.Getenv(c.Auth.EnvVar)
			if token == "" {
				return "", agenterrors.NewValidation(fmt.Sprintf("environment variable %s is not set", c.Auth.EnvVar), nil)
			}
			return token, nil
		}
		return "", errors.New("no authentication token configured")
	case "basic":
		// For basic auth, we'd need username/password - not implemented yet
		return "", errors.New("basic authentication not yet implemented")
	case "oauth":
		// OAuth would need flow implementation - not implemented yet
		return "", errors.New("OAuth authentication not yet implemented")
	default:
		return "", agenterrors.NewValidation(fmt.Sprintf("unsupported authentication type: %s", c.Auth.Type), nil)
	}
}

// BillingTypeResolved returns the effective billing model for this provider.
// Explicit config value takes priority; otherwise heuristics apply:
//   - localhost / 127.0.0.1 endpoints → free
//   - zai-coding → subscription
//   - everything else → pay_per_token (default)
func (c *ProviderConfig) BillingTypeResolved() string {
	if c.BillingType != "" {
		return c.BillingType
	}
	endpoint := strings.ToLower(c.Endpoint)
	if strings.Contains(endpoint, "127.0.0.1") || strings.Contains(endpoint, "localhost") {
		return BillingFree
	}
	if c.Name == "zai-coding" {
		return BillingSubscription
	}
	return BillingPayPerToken
}

// Validate validates the provider configuration
func (c *ProviderConfig) Validate() error {
	if c.Name == "" {
		return errors.New("provider name is required")
	}
	if c.Endpoint == "" {
		return errors.New("provider endpoint is required")
	}
	if c.Auth.Type == "" {
		return errors.New("authentication type is required")
	}
	// Validate model configuration
	if err := c.validateModelConfig(); err != nil {
		return fmt.Errorf("invalid model configuration: %w", err)
	}

	return nil
}

// validateModelConfig validates the model configuration
func (c *ProviderConfig) validateModelConfig() error {
	// At least one of default_context_limit or context_limit should be set
	if c.Models.DefaultContextLimit == 0 && c.Models.ContextLimit == 0 {
		return errors.New("either default_context_limit or context_limit must be set")
	}

	// Validate model overrides are positive
	for modelName, contextLimit := range c.Models.ModelOverrides {
		if contextLimit <= 0 {
			return agenterrors.NewValidation(fmt.Sprintf("model override for '%s' must have positive context limit", modelName), nil)
		}
		if contextLimit > 2097152 { // 2M tokens is a practical upper bound
			return agenterrors.NewValidation(fmt.Sprintf("model override for '%s' has context limit %d which exceeds reasonable maximum (2M tokens)", modelName, contextLimit), nil)
		}
	}

	// Validate pattern overrides
	for i, patternOverride := range c.Models.PatternOverrides {
		if patternOverride.Pattern == "" {
			return agenterrors.NewValidation(fmt.Sprintf("pattern override at index %d has empty pattern", i), nil)
		}
		if patternOverride.ContextLimit <= 0 {
			return agenterrors.NewValidation(fmt.Sprintf("pattern override '%s' must have positive context limit", patternOverride.Pattern), nil)
		}
		if patternOverride.ContextLimit > 2097152 { // 2M tokens is a practical upper bound
			return agenterrors.NewValidation(fmt.Sprintf("pattern override '%s' has context limit %d which exceeds reasonable maximum (2M tokens)", patternOverride.Pattern, patternOverride.ContextLimit), nil)
		}

		// Test if pattern is valid regex
		if _, err := regexp.Compile(patternOverride.Pattern); err != nil {
			return agenterrors.NewValidation(fmt.Sprintf("pattern override '%s' has invalid regex pattern: %v", patternOverride.Pattern, err), nil)
		}
	}

	// Validate max completion overrides are positive
	for modelName, maxCompletion := range c.Models.MaxCompletionOverrides {
		if maxCompletion <= 0 {
			return agenterrors.NewValidation(fmt.Sprintf("max completion override for '%s' must have positive value", modelName), nil)
		}
		if maxCompletion > 2097152 {
			return agenterrors.NewValidation(fmt.Sprintf("max completion override for '%s' has value %d which exceeds reasonable maximum (2M tokens)", modelName, maxCompletion), nil)
		}
	}

	// Validate completion pattern overrides
	for i, patternOverride := range c.Models.CompletionPatternOverrides {
		if patternOverride.Pattern == "" {
			return agenterrors.NewValidation(fmt.Sprintf("completion pattern override at index %d has empty pattern", i), nil)
		}
		if patternOverride.ContextLimit <= 0 {
			return agenterrors.NewValidation(fmt.Sprintf("completion pattern override '%s' must have positive limit", patternOverride.Pattern), nil)
		}
		if patternOverride.ContextLimit > 2097152 {
			return agenterrors.NewValidation(fmt.Sprintf("completion pattern override '%s' has limit %d which exceeds reasonable maximum (2M tokens)", patternOverride.Pattern, patternOverride.ContextLimit), nil)
		}
		if _, err := regexp.Compile(patternOverride.Pattern); err != nil {
			return agenterrors.NewValidation(fmt.Sprintf("completion pattern override '%s' has invalid regex pattern: %v", patternOverride.Pattern, err), nil)
		}
	}

	return nil
}

// GetTimeout returns the configured timeout duration
func (c *ProviderConfig) GetTimeout() time.Duration {
	if c.Streaming.ChunkTimeoutMs > 0 {
		return time.Duration(c.Streaming.ChunkTimeoutMs) * time.Millisecond
	}
	return 300 * time.Second // Default timeout (5 minutes)
}

// GetStreamingTimeout returns the configured streaming timeout duration
func (c *ProviderConfig) GetStreamingTimeout() time.Duration {
	if c.Streaming.ChunkTimeoutMs > 0 {
		return time.Duration(c.Streaming.ChunkTimeoutMs) * time.Millisecond
	}
	return 900 * time.Second // Default streaming timeout (15 minutes)
}

// GetContextLimit returns the context limit for a given model based on configuration
// Uses the following priority:
// 1. Exact model match in model_overrides
// 2. Pattern match in pattern_overrides
// 3. Lookup in model_info (catalog — source of truth for known models)
// 4. Provider default_context_limit (conservative fallback when catalog is absent)
// 5. Legacy context_limit field (for backward compatibility)
// 6. Conservative fallback (32000)
func (c *ProviderConfig) GetContextLimit(model string) int {
	// 1. Check for exact model match in overrides
	if contextLimit, exists := c.Models.ModelOverrides[model]; exists {
		return contextLimit
	}

	// 2. Check for pattern matches in overrides
	for _, patternOverride := range c.Models.PatternOverrides {
		if matched, _ := regexp.MatchString(patternOverride.Pattern, model); matched {
			return patternOverride.ContextLimit
		}
	}

	// 3. Check model_info catalog for a matching ID — handles full provider/model
	// names like "MiniMaxAI/MiniMax-M2.7" matching ID "MiniMax-M2.7". This is the
	// primary lookup for models published in the remote registry catalog.
	if len(c.Models.ModelInfo) > 0 {
		for _, mi := range c.Models.ModelInfo {
			if mi.ContextLength > 0 && (model == mi.ID || strings.HasSuffix(model, "/"+mi.ID)) {
				return mi.ContextLength
			}
		}
	}

	// 4. Use provider default context limit (fallback when catalog lacks this model)
	if c.Models.DefaultContextLimit > 0 {
		return c.Models.DefaultContextLimit
	}

	// 5. Fall back to legacy context_limit field (for backward compatibility)
	if c.Models.ContextLimit > 0 {
		return c.Models.ContextLimit
	}

	// 6. Conservative fallback
	return 32000
}

// GetMaxCompletionLimit returns the completion-token limit for a given model.
// Uses the following priority:
// 1. Exact model match in max_completion_overrides
// 2. Pattern match in completion_pattern_overrides
// 3. Provider default_max_completion_tokens
// 4. 0 (unknown/unset)
func (c *ProviderConfig) GetMaxCompletionLimit(model string) int {
	if c.Models.MaxCompletionOverrides != nil {
		if maxCompletion, exists := c.Models.MaxCompletionOverrides[model]; exists && maxCompletion > 0 {
			return maxCompletion
		}
	}

	for _, patternOverride := range c.Models.CompletionPatternOverrides {
		if matched, _ := regexp.MatchString(patternOverride.Pattern, model); matched && patternOverride.ContextLimit > 0 {
			return patternOverride.ContextLimit
		}
	}

	if c.Models.DefaultMaxCompletionTokens > 0 {
		return c.Models.DefaultMaxCompletionTokens
	}

	return 0
}

// GetModelInfo returns model information from config if available
func (c *ProviderConfig) GetModelInfo(modelID string) *ModelInfo {
	for _, mi := range c.Models.ModelInfo {
		if mi.ID == modelID {
			return &mi
		}
	}
	return nil
}
