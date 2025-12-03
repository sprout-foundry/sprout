package providers

import (
	"encoding/json"
	"fmt"
	"os"
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
	Model       string   `json:"model"`
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int     `json:"max_tokens"`
	TopP        *float64 `json:"top_p"`
}

// MessageConversion defines how messages should be converted
type MessageConversion struct {
	IncludeToolCallId     bool   `json:"include_tool_call_id"`
	ConvertToolRoleToUser bool   `json:"convert_tool_role_to_user"`
	ReasoningContentField string `json:"reasoning_content_field"`
}

// StreamingConfig defines streaming behavior
type StreamingConfig struct {
	Format         string `json:"format"` // "sse", "json_lines", "raw"
	ChunkTimeoutMs int    `json:"chunk_timeout_ms"`
	DoneMarker     string `json:"done_marker"`
}

// ModelConfig defines model-related configuration
type ModelConfig struct {
	ContextLimit    int      `json:"context_limit"`
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
	return nil
}

// GetTimeout returns the configured timeout duration
func (c *ProviderConfig) GetTimeout() time.Duration {
	if c.Streaming.ChunkTimeoutMs > 0 {
		return time.Duration(c.Streaming.ChunkTimeoutMs) * time.Millisecond
	}
	return 320 * time.Second // Default timeout
}

// GetStreamingTimeout returns the configured streaming timeout duration
func (c *ProviderConfig) GetStreamingTimeout() time.Duration {
	if c.Streaming.ChunkTimeoutMs > 0 {
		return time.Duration(c.Streaming.ChunkTimeoutMs) * time.Millisecond
	}
	return 900 * time.Second // Default streaming timeout (15 minutes)
}
