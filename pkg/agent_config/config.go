package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alantheprice/ledit/pkg/agent_api"
	"golang.org/x/term"
)

// ClientType is an alias for the agent API ClientType
type ClientType = api.ClientType

// Re-export the constants from agent_api for convenience
const (
	OpenAIClientType     = api.OpenAIClientType
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

// APIKeys represents the API keys configuration
type APIKeys struct {
	OpenAI     string `json:"openai,omitempty"`
	DeepInfra  string `json:"deepinfra,omitempty"`
	OpenRouter string `json:"openrouter,omitempty"`
	Cerebras   string `json:"cerebras,omitempty"`
	Groq       string `json:"groq,omitempty"`
	DeepSeek   string `json:"deepseek,omitempty"`
	Gemini     string `json:"gemini,omitempty"`
}

const (
	ConfigVersion = "1.0"
	ConfigDirName = ".ledit"
	ConfigFileName = "config.json"
	APIKeysFileName = "api_keys.json"
)

// NewConfig creates a new configuration with sensible defaults
func NewConfig() *Config {
	return &Config{
		LastUsedProvider: "",
		ProviderModels: map[string]string{
			"openai":     getDefaultModelForProvider(OpenAIClientType),
			"deepinfra":  getDefaultModelForProvider(DeepInfraClientType),
			"ollama":     getDefaultModelForProvider(OllamaClientType),
			"cerebras":   getDefaultModelForProvider(CerebrasClientType),
			"openrouter": getDefaultModelForProvider(OpenRouterClientType),
			"groq":       getDefaultModelForProvider(GroqClientType),
			"deepseek":   getDefaultModelForProvider(DeepSeekClientType),
		},
		ProviderPriority: []string{"openai", "openrouter", "deepinfra", "ollama", "cerebras", "groq", "deepseek"},
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
		{"openai", OpenAIClientType},
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
		c.ProviderPriority = []string{"openai", "openrouter", "deepinfra", "ollama", "cerebras", "groq", "deepseek"}
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
	case "openai":
		return OpenAIClientType, nil
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

// GetAPIKeysPath returns the full path to the API keys file
func GetAPIKeysPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, APIKeysFileName), nil
}

// LoadAPIKeys loads API keys from the file and sets environment variables
func LoadAPIKeys() (*APIKeys, error) {
	apiKeysPath, err := GetAPIKeysPath()
	if err != nil {
		return nil, err
	}

	// If API keys file doesn't exist, return empty keys
	if _, err := os.Stat(apiKeysPath); os.IsNotExist(err) {
		return &APIKeys{}, nil
	}

	data, err := os.ReadFile(apiKeysPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read API keys file: %w", err)
	}

	var keys APIKeys
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("failed to parse API keys file: %w", err)
	}

	// Set environment variables from loaded keys
	setEnvVarsFromAPIKeys(&keys)

	return &keys, nil
}

// SaveAPIKeys saves API keys to file
func SaveAPIKeys(keys *APIKeys) error {
	apiKeysPath, err := GetAPIKeysPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal API keys: %w", err)
	}

	return os.WriteFile(apiKeysPath, data, 0600)
}

// setEnvVarsFromAPIKeys sets environment variables from API keys
func setEnvVarsFromAPIKeys(keys *APIKeys) {
	if keys.OpenAI != "" {
		os.Setenv("OPENAI_API_KEY", keys.OpenAI)
	}
	if keys.DeepInfra != "" {
		os.Setenv("DEEPINFRA_API_KEY", keys.DeepInfra)
	}
	if keys.OpenRouter != "" {
		os.Setenv("OPENROUTER_API_KEY", keys.OpenRouter)
	}
	if keys.Cerebras != "" {
		os.Setenv("CEREBRAS_API_KEY", keys.Cerebras)
	}
	if keys.Groq != "" {
		os.Setenv("GROQ_API_KEY", keys.Groq)
	}
	if keys.DeepSeek != "" {
		os.Setenv("DEEPSEEK_API_KEY", keys.DeepSeek)
	}
	if keys.Gemini != "" {
		os.Setenv("GOOGLE_API_KEY", keys.Gemini)
	}
}

// GetProviderAPIKeyName returns the environment variable name for a provider
func GetProviderAPIKeyName(provider ClientType) string {
	switch provider {
	case OpenAIClientType:
		return "OPENAI_API_KEY"
	case DeepInfraClientType:
		return "DEEPINFRA_API_KEY"
	case OpenRouterClientType:
		return "OPENROUTER_API_KEY"
	case CerebrasClientType:
		return "CEREBRAS_API_KEY"
	case GroqClientType:
		return "GROQ_API_KEY"
	case DeepSeekClientType:
		return "DEEPSEEK_API_KEY"
	default:
		return ""
	}
}

// PromptForAPIKey prompts the user for an API key and saves it
func PromptForAPIKey(provider ClientType) (string, error) {
	providerName := getProviderDisplayName(provider)
	fmt.Printf("🔑 API key required for %s\n", providerName)
	fmt.Printf("Please enter your %s API key: ", providerName)

	// Read API key securely (hidden input)
	byteKey, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		// Fall back to regular input if term doesn't work
		fmt.Println() // New line after the prompt
		reader := bufio.NewReader(os.Stdin)
		key, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read API key: %w", err)
		}
		byteKey = []byte(strings.TrimSpace(key))
	} else {
		fmt.Println() // New line after hidden input
	}

	apiKey := strings.TrimSpace(string(byteKey))
	if apiKey == "" {
		return "", fmt.Errorf("no API key provided")
	}

	// Load existing keys
	keys, err := LoadAPIKeys()
	if err != nil {
		keys = &APIKeys{} // Create new if loading fails
	}

	// Set the new key
	setAPIKeyInStruct(keys, provider, apiKey)

	// Save keys
	if err := SaveAPIKeys(keys); err != nil {
		return "", fmt.Errorf("failed to save API key: %w", err)
	}

	// Set environment variable
	envVar := GetProviderAPIKeyName(provider)
	if envVar != "" {
		os.Setenv(envVar, apiKey)
	}

	fmt.Printf("✅ API key saved for %s\n", providerName)
	return apiKey, nil
}

// setAPIKeyInStruct sets the API key in the appropriate field
func setAPIKeyInStruct(keys *APIKeys, provider ClientType, apiKey string) {
	switch provider {
	case OpenAIClientType:
		keys.OpenAI = apiKey
	case DeepInfraClientType:
		keys.DeepInfra = apiKey
	case OpenRouterClientType:
		keys.OpenRouter = apiKey
	case CerebrasClientType:
		keys.Cerebras = apiKey
	case GroqClientType:
		keys.Groq = apiKey
	case DeepSeekClientType:
		keys.DeepSeek = apiKey
	}
}

// getProviderDisplayName returns a user-friendly name for the provider
func getProviderDisplayName(provider ClientType) string {
	switch provider {
	case OpenAIClientType:
		return "OpenAI"
	case DeepInfraClientType:
		return "DeepInfra"
	case OpenRouterClientType:
		return "OpenRouter"
	case CerebrasClientType:
		return "Cerebras"
	case GroqClientType:
		return "Groq"
	case DeepSeekClientType:
		return "DeepSeek"
	default:
		return string(provider)
	}
}

// EnsureAPIKeyAvailable ensures an API key is available for a provider, prompting if needed
func EnsureAPIKeyAvailable(provider ClientType) error {
	envVar := GetProviderAPIKeyName(provider)
	if envVar == "" {
		return nil // No API key needed (e.g., Ollama)
	}

	// Check if already set in environment
	if os.Getenv(envVar) != "" {
		return nil
	}

	// Try to load from file
	_, err := LoadAPIKeys()
	if err == nil && os.Getenv(envVar) != "" {
		return nil
	}

	// Prompt for API key
	_, err = PromptForAPIKey(provider)
	return err
}