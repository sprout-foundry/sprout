package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"
)

// ProviderConfig defines the configuration for a generic provider
type ProviderConfig struct {
	Name       string            `json:"name"`
	Endpoint   string            `json:"endpoint"`
	Auth       AuthConfig        `json:"auth"`
	Headers    map[string]string `json:"headers"`
	Defaults   RequestDefaults   `json:"defaults"`
	Conversion MessageConversion `json:"message_conversion"`
	Streaming  StreamingConfig   `json:"streaming"`
	Models     ModelConfig       `json:"models"`
	Retry      RetryConfig       `json:"retry"`
	Cost       CostConfig        `json:"cost"`
}

// AuthConfig defines authentication configuration
type AuthConfig struct {
	Type   string `json:"type"`    // "bearer", "api_key", "basic", "oauth"
	EnvVar string `json:"env_var"` // Environment variable containing the auth token
	Key    string `json:"key"`     // Fixed API key (not recommended for production)
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
	IncludeToolCallId        bool   `json:"include_tool_call_id"`
	ConvertToolRoleToUser    bool   `json:"convert_tool_role_to_user"`
	ReasoningContentField    string `json:"reasoning_content_field"`
	ArgumentsAsJSON          bool   `json:"arguments_as_json"`
	SkipToolExecutionSummary bool   `json:"skip_tool_execution_summary"` // For providers with strict role alternation
	ForceToolCallType        string `json:"force_tool_call_type"`        // Force tool call type to specific value (e.g., "function" for Mistral)
}

// StreamingConfig defines streaming behavior
type StreamingConfig struct {
	Format        string `json:"format"` // "sse", "json_lines", "raw"
	ChunkTimeoutMs int    `json:"chunk_timeout_ms"`
	DoneMarker    string `json:"done_marker"`
}

// PatternOverride defines context limit overrides for model patterns
type PatternOverride struct {
	Pattern      string `json:"pattern"`
	ContextLimit int    `json:"context_limit"`
}

// ModelConfig defines model-related configuration
type ModelConfig struct {
	DefaultContextLimit int               `json:"default_context_limit"`
	ModelOverrides      map[string]int    `json:"model_overrides"`
	PatternOverrides    []PatternOverride `json:"pattern_overrides"`
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
		return nil, fmt.Errorf("failed to read provider config file %s: %w", configPath, err)
	}

	var config ProviderConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse provider config file %s: %w", configPath, err)
	}

	return &config, nil
}

// LoadProviderRegistry loads the provider registry from a JSON file
func LoadProviderRegistry(registryPath string) (*ProviderRegistry, error) {
	data, err := os.ReadFile(registryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read provider registry file %s: %w", registryPath, err)
	}

	var registry ProviderRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse provider registry file %s: %w", registryPath, err)
	}

	return &registry, nil
}

// GetAuthToken retrieves the authentication token based on the auth configuration
func (c *ProviderConfig) GetAuthToken() (string, error) {
	switch c.Auth.Type {
	case "none":
		// No authentication required
		return "", nil
	case "bearer", "api_key":
		if c.Auth.EnvVar != "" {
			token := os.Getenv(c.Auth.EnvVar)
			if token == "" {
				return "", fmt.Errorf("environment variable %s is not set", c.Auth.EnvVar)
			}
			return token, nil
		}
		if c.Auth.Key != "" {
			return c.Auth.Key, nil
		}
		return "", fmt.Errorf("no authentication token configured")
	case "basic":
		// For basic auth, we'd need username/password - not implemented yet
		return "", fmt.Errorf("basic authentication not yet implemented")
	case "oauth":
		// OAuth would need flow implementation - not implemented yet
		return "", fmt.Errorf("OAuth authentication not yet implemented")
	default:
		return "", fmt.Errorf("unsupported authentication type: %s", c.Auth.Type)
	}
}

// Validate validates the provider configuration
func (c *ProviderConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	if c.Endpoint == "" {
		return fmt.Errorf("provider endpoint is required")
	}
	if c.Auth.Type == "" {
		return fmt.Errorf("authentication type is required")
	}
	if c.Defaults.Model == "" {
		return fmt.Errorf("default model is required")
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
		return fmt.Errorf("either default_context_limit or context_limit must be set")
	}

	// Validate model overrides are positive
	for modelName, contextLimit := range c.Models.ModelOverrides {
		if contextLimit <= 0 {
			return fmt.Errorf("model override for '%s' must have positive context limit", modelName)
		}
		if contextLimit > 2097152 { // 2M tokens is a practical upper bound
			return fmt.Errorf("model override for '%s' has context limit %d which exceeds reasonable maximum (2M tokens)", modelName, contextLimit)
		}
	}

	// Validate pattern overrides
	for i, patternOverride := range c.Models.PatternOverrides {
		if patternOverride.Pattern == "" {
			return fmt.Errorf("pattern override at index %d has empty pattern", i)
		}
		if patternOverride.ContextLimit <= 0 {
			return fmt.Errorf("pattern override '%s' must have positive context limit", patternOverride.Pattern)
		}
		if patternOverride.ContextLimit > 2097152 { // 2M tokens is a practical upper bound
			return fmt.Errorf("pattern override '%s' has context limit %d which exceeds reasonable maximum (2M tokens)", patternOverride.Pattern, patternOverride.ContextLimit)
		}

		// Test if pattern is valid regex
		if _, err := regexp.Compile(patternOverride.Pattern); err != nil {
			return fmt.Errorf("pattern override '%s' has invalid regex pattern: %w", patternOverride.Pattern, err)
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
// 3. Provider default_context_limit
// 4. Legacy context_limit field (for backward compatibility)
// 5. Conservative fallback (32000)
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

	// 3. Use provider default context limit (if configured)
	if c.Models.DefaultContextLimit > 0 {
		return c.Models.DefaultContextLimit
	}

	// 4. Fall back to legacy context_limit field (for backward compatibility)
	if c.Models.ContextLimit > 0 {
		return c.Models.ContextLimit
	}

	// 5. Conservative fallback
	return 32000
}
