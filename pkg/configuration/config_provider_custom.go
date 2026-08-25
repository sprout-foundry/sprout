package configuration

import (
	"github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// LanguageServerOverride allows users to customize or add language server
// configurations beyond the built-in defaults. When a matching ID exists
// in the default set, this override replaces it entirely. New IDs are
// appended to the merged list.
type LanguageServerOverride struct {
	ID          string   `json:"id" yaml:"id"`                                         // Unique server ID (e.g. "go", "typescript")
	Binary      string   `json:"binary" yaml:"binary"`                                 // Path to the binary (e.g. "gopls", "typescript-language-server")
	Args        []string `json:"args,omitempty" yaml:"args,omitempty"`                 // Command-line arguments (e.g. ["--stdio"])
	LanguageIDs []string `json:"language_ids,omitempty" yaml:"language_ids,omitempty"` // Language IDs this server handles (e.g. ["go"])
	InstallHint string   `json:"install_hint,omitempty" yaml:"install_hint,omitempty"` // Installation instructions
}

// CustomProviderConfig represents a custom model provider configuration
type CustomProviderConfig struct {
	Name                   string                      `json:"name"`
	Endpoint               string                      `json:"endpoint"`
	ModelName              string                      `json:"model_name"`
	ContextSize            int                         `json:"context_size"`                  // Default context size for provider
	ModelContextSizes      map[string]int              `json:"model_context_sizes,omitempty"` // Per-model context sizes (e.g., "my-model": 131072)
	ReasoningEffort        string                      `json:"reasoning_effort,omitempty"`    // Optional provider-specific reasoning effort override
	Temperature            *float64                    `json:"temperature,omitempty"`         // Optional default temperature
	TopP                   *float64                    `json:"top_p,omitempty"`               // Optional default top_p
	Parameters             map[string]interface{}      `json:"parameters,omitempty"`          // Optional provider-specific default parameters
	RequiresAPIKey         bool                        `json:"requires_api_key"`
	ToolCalls              []string                    `json:"tool_calls,omitempty"`               // Optional explicit tool allowlist; when set, only these tools are exposed
	EnvVar                 string                      `json:"env_var,omitempty"`                  // Environment variable name for API key
	ChunkTimeoutMs         int                         `json:"chunk_timeout_ms,omitempty"`         // Streaming chunk timeout in milliseconds
	Conversion             providers.MessageConversion `json:"message_conversion,omitempty"`       // Message conversion configuration
	SupportsVision         bool                        `json:"supports_vision,omitempty"`          // Whether this provider supports vision requests
	VisionModel            string                      `json:"vision_model,omitempty"`             // Vision-capable model for this provider
	VisionFallbackProvider string                      `json:"vision_fallback_provider,omitempty"` // Optional fallback provider for vision
	VisionFallbackModel    string                      `json:"vision_fallback_model,omitempty"`    // Optional fallback model for vision provider
	BillingType            string                      `json:"billing_type,omitempty"`             // Billing model: pay_per_token (default), subscription, free
}
