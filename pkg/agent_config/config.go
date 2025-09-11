package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alantheprice/ledit/pkg/agent_api"
)

// Config represents the application configuration
type Config struct {
	LastUsedProvider api.ClientType            `json:"last_used_provider"`
	ProviderModels   map[string]string         `json:"provider_models"`
	ProviderPriority []string                  `json:"provider_priority"`
	Preferences      map[string]interface{}    `json:"preferences"`
	Version          string                    `json:"version"`
}

const (
	ConfigVersion = "1.0"
	ConfigDirName = ".coder"
	ConfigFileName = "config.json"
)

// NewConfig creates a new configuration with sensible defaults
func NewConfig() *Config {
	return &Config{
		LastUsedProvider: "",
		ProviderModels: map[string]string{
			"deepinfra":  api.GetDefaultModelForProvider(api.DeepInfraClientType),
			"ollama":     api.GetDefaultModelForProvider(api.OllamaClientType),
			"cerebras":   api.GetDefaultModelForProvider(api.CerebrasClientType),
			"openrouter": api.GetDefaultModelForProvider(api.OpenRouterClientType),
			"groq":       api.GetDefaultModelForProvider(api.GroqClientType),
			"deepseek":   api.GetDefaultModelForProvider(api.DeepSeekClientType),
		},
		ProviderPriority: []string{"openrouter", "deepinfra", "ollama", "cerebras", "groq", "deepseek"},
		Preferences:      make(map[string]interface{}),
		Version:          ConfigVersion,
	}
}

// GetConfigDir returns the configuration directory path
func GetConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	
	configDir := filepath.Join(homeDir, ConfigDirName)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	
	return configDir, nil
}

// GetConfigPath returns the full path to the config file
func GetConfigPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, ConfigFileName), nil
}

// Load loads the configuration from file, creating defaults if not found
func Load() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}
	
	// If config doesn't exist, create default
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config := NewConfig()
		if err := config.Save(); err != nil {
			return nil, fmt.Errorf("failed to create default config: %w", err)
		}
		return config, nil
	}
	
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	
	// Migrate or validate config if needed
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	
	return &config, nil
}

// Save saves the configuration to file
func (c *Config) Save() error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}
	
	c.Version = ConfigVersion
	
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	
	return os.WriteFile(configPath, data, 0600)
}

// Validate validates the configuration and migrates if necessary
func (c *Config) Validate() error {
	// Initialize maps if nil
	if c.ProviderModels == nil {
		c.ProviderModels = make(map[string]string)
	}
	if c.Preferences == nil {
		c.Preferences = make(map[string]interface{})
	}
	
	// Ensure all providers have default models
	providers := []struct {
		name string
		clientType api.ClientType
	}{
		{"deepinfra", api.DeepInfraClientType},
		{"ollama", api.OllamaClientType},
		{"cerebras", api.CerebrasClientType},
		{"openrouter", api.OpenRouterClientType},
		{"groq", api.GroqClientType},
		{"deepseek", api.DeepSeekClientType},
	}
	
	for _, provider := range providers {
		if _, exists := c.ProviderModels[provider.name]; !exists {
			c.ProviderModels[provider.name] = api.GetDefaultModelForProvider(provider.clientType)
		}
	}
	
	// Set default priority if empty
	if len(c.ProviderPriority) == 0 {
		c.ProviderPriority = []string{"deepinfra", "ollama", "cerebras", "openrouter", "groq", "deepseek"}
	}
	
	return nil
}

// GetModelForProvider returns the configured model for a provider
func (c *Config) GetModelForProvider(provider api.ClientType) string {
	providerName := getProviderConfigName(provider)
	if model, exists := c.ProviderModels[providerName]; exists && model != "" {
		return model
	}
	return api.GetDefaultModelForProvider(provider)
}

// SetModelForProvider sets the model for a specific provider
func (c *Config) SetModelForProvider(provider api.ClientType, model string) {
	providerName := getProviderConfigName(provider)
	c.ProviderModels[providerName] = model
	c.LastUsedProvider = provider
}

// GetLastUsedProvider returns the last used provider, with fallback
func (c *Config) GetLastUsedProvider() api.ClientType {
	if c.LastUsedProvider != "" {
		return c.LastUsedProvider
	}
	// Fall back to environment-based detection
	return api.GetClientTypeFromEnv()
}

// SetLastUsedProvider sets the last used provider
func (c *Config) SetLastUsedProvider(provider api.ClientType) {
	c.LastUsedProvider = provider
}

// getProviderConfigName converts ClientType to config key
func getProviderConfigName(clientType api.ClientType) string {
	switch clientType {
	case api.DeepInfraClientType:
		return "deepinfra"
	case api.OllamaClientType:
		return "ollama"
	case api.CerebrasClientType:
		return "cerebras"
	case api.OpenRouterClientType:
		return "openrouter"
	case api.GroqClientType:
		return "groq"
	case api.DeepSeekClientType:
		return "deepseek"
	default:
		return string(clientType)
	}
}

// GetProviderFromConfigName converts config key to ClientType
func GetProviderFromConfigName(name string) (api.ClientType, error) {
	switch name {
	case "deepinfra":
		return api.DeepInfraClientType, nil
	case "ollama":
		return api.OllamaClientType, nil
	case "cerebras":
		return api.CerebrasClientType, nil
	case "openrouter":
		return api.OpenRouterClientType, nil
	case "groq":
		return api.GroqClientType, nil
	case "deepseek":
		return api.DeepSeekClientType, nil
	default:
		return "", fmt.Errorf("unknown provider: %s", name)
	}
}