package config

import (
	"strings"
	"time"
)

// LLMConfig contains all Large Language Model related configuration
type LLMConfig struct {
	// Model Selection
	EditingModel       string `json:"editing_model"`       // Primary model for code editing
	SummaryModel       string `json:"summary_model"`       // Model for summarization tasks
	OrchestrationModel string `json:"orchestration_model"` // Model for orchestration tasks
	WorkspaceModel     string `json:"workspace_model"`     // Model for workspace analysis
	EmbeddingModel     string `json:"embedding_model"`     // Model for embeddings
	CodeReviewModel    string `json:"code_review_model"`   // Model for code review tasks
	LocalModel         string `json:"local_model"`         // Local model configuration
	SearchModel        string `json:"search_model"`        // Model for search tasks

	// Generation Parameters
	Temperature      float64 `json:"temperature"`       // LLM temperature (0.0-1.0)
	MaxTokens        int     `json:"max_tokens"`        // Maximum output tokens
	TopP             float64 `json:"top_p"`             // Nucleus sampling parameter
	PresencePenalty  float64 `json:"presence_penalty"`  // Presence penalty
	FrequencyPenalty float64 `json:"frequency_penalty"` // Frequency penalty

	// Timeout Configuration
	DefaultTimeoutSecs int            `json:"default_timeout_secs"`        // Default timeout in seconds
	ModelTimeouts      map[string]int `json:"model_timeouts,omitempty"`    // Model-specific timeouts in seconds
	ProviderTimeouts   map[string]int `json:"provider_timeouts,omitempty"` // Provider-specific timeouts in seconds

	// Infrastructure
	OllamaServerURL   string            `json:"ollama_server_url"`            // Ollama server endpoint
	ProviderEndpoints map[string]string `json:"provider_endpoints,omitempty"` // Map of provider names to their API endpoints
}

// DefaultLLMConfig returns sensible defaults for LLM configuration
func DefaultLLMConfig() *LLMConfig {
	return &LLMConfig{
		EditingModel:       "deepinfra:google/gemini-2.5-flash",
		SummaryModel:       "deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo",
		OrchestrationModel: "deepinfra:moonshotai/Kimi-K2-Instruct",
		WorkspaceModel:     "deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo",
		EmbeddingModel:     "deepinfra:Qwen/Qwen3-Embedding-4B",
		CodeReviewModel:    "deepinfra:google/gemini-2.5-flash",
		LocalModel:         "ollama:hf.co/unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF:UD-Q4_K_XL",
		SearchModel:        "",

		Temperature:      0.7,
		MaxTokens:        8192, // Reasonable default that fits within validation range
		TopP:             1.0,
		PresencePenalty:  0.0,
		FrequencyPenalty: 0.0,

		// Timeout Configuration - increased defaults for better reliability
		DefaultTimeoutSecs: 120, // 2 minutes default
		ProviderTimeouts: map[string]int{
			"deepinfra": 180, // 3 minutes for DeepInfra (can be slower)
			"openai":    90,  // 1.5 minutes for OpenAI
			"groq":      60,  // 1 minute for Groq (fast)
			"deepseek":  120, // 2 minutes for DeepSeek
			"ollama":    300, // 5 minutes for local Ollama models
			"gemini":    90,  // 1.5 minutes for Gemini
		},
		ModelTimeouts: map[string]int{
			// Reasoning models need more time
			"deepseek-r1":                           300, // 5 minutes for reasoning models
			"deepinfra:deepseek-ai/DeepSeek-R1":     300,
			"deepinfra:deepseek-ai/DeepSeek-V3":     240, // 4 minutes for large models
			"deepinfra:meta-llama/Llama-3.3-70B":    180, // 3 minutes for 70B models
			"deepinfra:moonshotai/Kimi-K2-Instruct": 240, // 4 minutes for Kimi (can be slow)
			"ollama:":                               300, // 5 minutes for any Ollama model
		},

		OllamaServerURL: "http://localhost:11434",
		ProviderEndpoints: map[string]string{
			"deepinfra": "https://api.deepinfra.com/v1/openai",
			"openai":    "https://api.openai.com/v1",
			"groq":      "https://api.groq.com/openai/v1",
			"ollama":    "http://localhost:11434/api",
		},
	}
}

// Validate checks if the LLM configuration is valid
func (c *LLMConfig) Validate() error {
	if c.EditingModel == "" {
		return NewValidationError("editing_model", "cannot be empty")
	}

	if c.Temperature < 0.0 || c.Temperature > 2.0 {
		return NewValidationError("temperature", "must be between 0.0 and 2.0")
	}

	if c.MaxTokens < 1 || c.MaxTokens > 32768 {
		return NewValidationError("max_tokens", "must be between 1 and 32768")
	}

	if c.TopP < 0.0 || c.TopP > 1.0 {
		return NewValidationError("top_p", "must be between 0.0 and 1.0")
	}

	return nil
}

// GetPrimaryModel returns the most commonly used model for general operations
func (c *LLMConfig) GetPrimaryModel() string {
	if c.EditingModel != "" {
		return c.EditingModel
	}
	return "deepinfra:google/gemini-2.5-flash" // fallback
}

// IsLocalModel returns true if the primary model is a local model
func (c *LLMConfig) IsLocalModel() bool {
	primary := c.GetPrimaryModel()
	return primary == c.LocalModel || c.OllamaServerURL != ""
}

// GetTimeoutForModel returns the appropriate timeout duration for a specific model
func (c *LLMConfig) GetTimeoutForModel(modelName string) time.Duration {
	// First check for exact model match
	if timeoutSecs, exists := c.ModelTimeouts[modelName]; exists {
		return time.Duration(timeoutSecs) * time.Second
	}

	// Check for partial model matches (for cases like "ollama:" prefix)
	for pattern, timeoutSecs := range c.ModelTimeouts {
		if strings.Contains(modelName, pattern) {
			return time.Duration(timeoutSecs) * time.Second
		}
	}

	// Extract provider from model name (format: "provider:model")
	parts := strings.SplitN(modelName, ":", 2)
	if len(parts) > 0 {
		provider := parts[0]
		if timeoutSecs, exists := c.ProviderTimeouts[provider]; exists {
			return time.Duration(timeoutSecs) * time.Second
		}
	}

	// Use default timeout
	defaultTimeout := c.DefaultTimeoutSecs
	if defaultTimeout <= 0 {
		defaultTimeout = 120 // 2 minutes fallback
	}
	return time.Duration(defaultTimeout) * time.Second
}

// GetSmartTimeout returns an appropriate timeout based on the operation type and model
func (c *LLMConfig) GetSmartTimeout(modelName string, operationType string) time.Duration {
	baseTimeout := c.GetTimeoutForModel(modelName)

	// Adjust timeout based on operation type
	switch operationType {
	case "code_review", "analysis":
		// Code review and analysis operations may need more time
		return baseTimeout + (30 * time.Second)
	case "search", "quick":
		// Quick operations can use shorter timeouts
		return time.Duration(float64(baseTimeout) * 0.5)
	case "commit", "summary":
		// Commit and summary generation is usually quick
		return time.Duration(float64(baseTimeout) * 0.75)
	default:
		return baseTimeout
	}
}
