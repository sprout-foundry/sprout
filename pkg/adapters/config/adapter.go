package config

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// ConfigAdapter adapts the existing config.Config to interfaces.ConfigProvider
type ConfigAdapter struct {
	config *config.Config
}

// NewConfigAdapter creates a new config adapter
func NewConfigAdapter(cfg *config.Config) interfaces.ConfigProvider {
	return &ConfigAdapter{
		config: cfg,
	}
}

// GetProviderConfig implements interfaces.ConfigProvider
func (a *ConfigAdapter) GetProviderConfig(providerName string) (*types.ProviderConfig, error) {
	if a.config == nil {
		return nil, fmt.Errorf("config is nil")
	}

	llmConfig := a.config.GetLLMConfig()

	config := &types.ProviderConfig{
		Name:        providerName,
		Model:       llmConfig.GetPrimaryModel(),
		Temperature: llmConfig.Temperature,
		MaxTokens:   llmConfig.MaxTokens,
		Timeout:     llmConfig.DefaultTimeoutSecs,
		Enabled:     true,
	}

	// Set provider-specific configurations
	switch providerName {
	case "openai":
		config.BaseURL = "https://api.openai.com/v1"
		config.APIKey = "test-key-placeholder" // In real implementation, would get from API keys file
		if config.Model == "" {
			config.Model = "gpt-4"
		}
	case "gemini":
		config.BaseURL = "https://generativelanguage.googleapis.com"
		config.APIKey = "test-key-placeholder"
		if config.Model == "" {
			config.Model = "gemini-pro"
		}
	case "ollama":
		config.BaseURL = llmConfig.OllamaServerURL
		if config.Model == "" {
			config.Model = llmConfig.LocalModel
		}
	case "groq":
		config.BaseURL = "https://api.groq.com/openai/v1"
		config.APIKey = "test-key-placeholder"
		if config.Model == "" {
			config.Model = "mixtral-8x7b-32768"
		}
	default:
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	return config, nil
}

// GetAgentConfig implements interfaces.ConfigProvider
func (a *ConfigAdapter) GetAgentConfig() *types.AgentConfig {
	return &types.AgentConfig{
		MaxRetries:         3,
		RetryDelay:         5,
		MaxContextRequests: 10,
		EnableValidation:   !a.config.SkipPrompt,
		EnableCodeReview:   !a.config.SkipPrompt,
		ValidationTimeout:  30,
		DefaultStrategy:    "quick",
		CostThreshold:      0.1,
	}
}

// GetEditorConfig implements interfaces.ConfigProvider
func (a *ConfigAdapter) GetEditorConfig() *types.EditorConfig {
	return &types.EditorConfig{
		BackupEnabled:     true,
		DiffStyle:         "unified",
		AutoFormat:        true,
		PreferredLanguage: "go",
		IgnorePatterns:    []string{"*.test", "*.tmp"},
		MaxFileSize:       1024 * 1024, // 1MB default
	}
}

// GetSecurityConfig implements interfaces.ConfigProvider
func (a *ConfigAdapter) GetSecurityConfig() *types.SecurityConfig {
	return &types.SecurityConfig{
		EnableCredentialScanning: true,
		BlockedPatterns:          []string{".*_key.*", ".*password.*", ".*secret.*"},
		AllowedCommands:          []string{"git", "go", "npm", "python"},
		RequireConfirmation:      !a.config.SkipPrompt,
	}
}

// GetUIConfig implements interfaces.ConfigProvider
func (a *ConfigAdapter) GetUIConfig() *types.UIConfig {
	return &types.UIConfig{
		SkipPrompts:    a.config.SkipPrompt,
		ColorOutput:    true,
		VerboseLogging: false, // Would use config.Debug if available
		ProgressBars:   !a.config.SkipPrompt,
		OutputFormat:   "text",
	}
}

// SetConfig implements interfaces.ConfigProvider
func (a *ConfigAdapter) SetConfig(key string, value interface{}) error {
	if a.config == nil {
		return fmt.Errorf("config is nil")
	}

	// Update supported config fields
	switch key {
	case "editing_model":
		if model, ok := value.(string); ok && a.config.LLM != nil {
			a.config.LLM.EditingModel = model
		}
	case "skip_prompt":
		if skipPrompt, ok := value.(bool); ok {
			a.config.SkipPrompt = skipPrompt
		}
	case "temperature":
		if temp, ok := value.(float64); ok && a.config.LLM != nil {
			a.config.LLM.Temperature = temp
		}
	case "max_tokens":
		if maxTokens, ok := value.(int); ok && a.config.LLM != nil {
			a.config.LLM.MaxTokens = maxTokens
		}
	default:
		return fmt.Errorf("unsupported config key: %s", key)
	}

	return nil
}

// SaveConfig implements interfaces.ConfigProvider
func (a *ConfigAdapter) SaveConfig() error {
	// For now, return not implemented - would save to file
	return fmt.Errorf("config saving not implemented in adapter")
}

// ReloadConfig implements interfaces.ConfigProvider
func (a *ConfigAdapter) ReloadConfig() error {
	// For now, return not implemented - would reload from file
	return fmt.Errorf("config reloading not implemented in adapter")
}

// WatchConfig implements interfaces.ConfigProvider
func (a *ConfigAdapter) WatchConfig(callback func()) error {
	// For now, return not implemented - would integrate with config file watching
	return fmt.Errorf("config watching not implemented in adapter")
}

// Basic config adapter complete - layered config would be implemented separately if needed
