package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alantheprice/ledit/pkg/agent_api"
)

// ClientType is an alias for the agent API ClientType
type ClientType = api.ClientType

// Re-export the constants from agent_api for convenience
const (
	DeepInfraClientType  = api.DeepInfraClientType
	OllamaClientType     = api.OllamaClientType
	CerebrasClientType   = api.CerebrasClientType
	OpenRouterClientType = api.OpenRouterClientType
	GroqClientType       = api.GroqClientType
	DeepSeekClientType   = api.DeepSeekClientType
)

// Config represents the application configuration
type Config struct {
	LastUsedProvider ClientType               `json:"last_used_provider"`
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
			"deepinfra":  getDefaultModelForProvider(DeepInfraClientType),
			"ollama":     getDefaultModelForProvider(OllamaClientType),
			"cerebras":   getDefaultModelForProvider(CerebrasClientType),
			"openrouter": "deepseek/deepseek-chat-v3.1:free",
			"groq":       getDefaultModelForProvider(GroqClientType),
			"deepseek":   getDefaultModelForProvider(DeepSeekClientType),
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
		clientType ClientType
	}{
		{"deepinfra", DeepInfraClientType},
		{"ollama", OllamaClientType},
		{"cerebras", CerebrasClientType},
		{"openrouter", OpenRouterClientType},
		{"groq", GroqClientType},
		{"deepseek", DeepSeekClientType},
	}
	
	for _, provider := range providers {
		if _, exists := c.ProviderModels[provider.name]; !exists {
			c.ProviderModels[provider.name] = getDefaultModelForProvider(provider.clientType)
		}
	}
	
	// Set default priority if empty
	if len(c.ProviderPriority) == 0 {
		c.ProviderPriority = []string{"deepinfra", "ollama", "cerebras", "openrouter", "groq", "deepseek"}
	}
	
	return nil
}

// GetModelForProvider returns the configured model for a provider
func (c *Config) GetModelForProvider(provider ClientType) string {
	providerName := getProviderConfigName(provider)
	if model, exists := c.ProviderModels[providerName]; exists && model != "" {
		return model
	}
	return getDefaultModelForProvider(provider)
}

// SetModelForProvider sets the model for a specific provider
func (c *Config) SetModelForProvider(provider ClientType, model string) {
	providerName := getProviderConfigName(provider)
	c.ProviderModels[providerName] = model
	c.LastUsedProvider = provider
}

// GetLastUsedProvider returns the last used provider, with fallback
func (c *Config) GetLastUsedProvider() ClientType {
	if c.LastUsedProvider != "" {
		return c.LastUsedProvider
	}
	// Fall back to environment-based detection
	return getClientTypeFromEnv()
}

// SetLastUsedProvider sets the last used provider
func (c *Config) SetLastUsedProvider(provider ClientType) {
	c.LastUsedProvider = provider
}

// getProviderConfigName converts ClientType to config key
func getProviderConfigName(clientType ClientType) string {
	switch clientType {
	case DeepInfraClientType:
		return "deepinfra"
	case OllamaClientType:
		return "ollama"
	case CerebrasClientType:
		return "cerebras"
	case OpenRouterClientType:
		return "openrouter"
	case GroqClientType:
		return "groq"
	case DeepSeekClientType:
		return "deepseek"
	default:
		return string(clientType)
	}
}

// GetProviderFromConfigName converts config key to ClientType
func GetProviderFromConfigName(name string) (ClientType, error) {
	switch name {
	case "deepinfra":
		return DeepInfraClientType, nil
	case "ollama":
		return OllamaClientType, nil
	case "cerebras":
		return CerebrasClientType, nil
	case "openrouter":
		return OpenRouterClientType, nil
	case "groq":
		return GroqClientType, nil
	case "deepseek":
		return DeepSeekClientType, nil
	default:
		return "", fmt.Errorf("unknown provider: %s", name)
	}
}

// getDefaultModelForProvider returns the best default model for each provider
func getDefaultModelForProvider(clientType ClientType) string {
	// Use the agent_api implementation
	return api.GetDefaultModelForProvider(clientType)
}

// getClientTypeFromEnv determines which client to use based on environment variables
func getClientTypeFromEnv() ClientType {
	// Use the agent_api implementation
	return api.GetClientTypeFromEnv()
}