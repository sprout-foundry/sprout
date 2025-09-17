package unified

import (
	"fmt"
	"os"

	agent_config "github.com/alantheprice/ledit/pkg/agent_config"
	"github.com/alantheprice/ledit/pkg/config"
)

// UnifiedConfig provides a unified interface for both config systems
// This interface allows seamless migration from agent_config to main config
type UnifiedConfig interface {
	// API Key Management
	GetAPIKey(provider agent_config.ClientType) (string, error)
	SetAPIKey(provider agent_config.ClientType, key string) error
	EnsureAPIKeyAvailable(provider agent_config.ClientType) error

	// Provider Management
	GetModelForProvider(provider agent_config.ClientType) string
	SetModelForProvider(provider agent_config.ClientType, model string)
	GetLastUsedProvider() agent_config.ClientType
	SetLastUsedProvider(provider agent_config.ClientType)

	// Persistence
	Save() error
	Load() error
	Validate() error

	// Migration Support
	MigrateFromAgentConfig() error
	IsLegacyConfigPresent() bool

	// Access to underlying configs
	GetMainConfig() *config.Config
	GetAgentConfig() *agent_config.Config
}

// UnifiedConfigImpl implements the UnifiedConfig interface
type UnifiedConfigImpl struct {
	mainConfig  *config.Config       // pkg/config.Config
	agentConfig *agent_config.Config // legacy agent config
	configDir   string
}

// NewUnifiedConfig creates a new unified config instance
// It loads both configs and merges them intelligently
func NewUnifiedConfig() (UnifiedConfig, error) {
	unified := &UnifiedConfigImpl{}

	// Load main config (preferred)
	mainCfg, err := config.LoadOrInitConfig(true)
	if err != nil {
		return nil, err
	}
	unified.mainConfig = mainCfg

	// Load agent config (legacy fallback)
	agentCfg, err := agent_config.Load()
	if err != nil {
		// Agent config might not exist, create default
		agentCfg = agent_config.NewConfig()
	}
	unified.agentConfig = agentCfg

	// Get config directory
	configDir, err := agent_config.GetConfigDir()
	if err != nil {
		return nil, err
	}
	unified.configDir = configDir

	return unified, nil
}

// GetAPIKey retrieves API key from main config first, then agent config
func (u *UnifiedConfigImpl) GetAPIKey(provider agent_config.ClientType) (string, error) {
	// For now, use agent config system since main config doesn't have API key management yet
	// This will be updated in later phases when main config is enhanced
	apiKeys, err := agent_config.LoadAPIKeys()
	if err != nil {
		return "", err
	}

	return u.getAPIKeyFromStruct(apiKeys, provider), nil
}

// SetAPIKey sets API key in main config preferentially
func (u *UnifiedConfigImpl) SetAPIKey(provider agent_config.ClientType, key string) error {
	// For now, use agent config system since main config doesn't have API key management yet
	apiKeys, err := agent_config.LoadAPIKeys()
	if err != nil {
		// Create new if doesn't exist
		apiKeys = &agent_config.APIKeys{}
	}

	u.setAPIKeyInStruct(apiKeys, provider, key)
	return agent_config.SaveAPIKeys(apiKeys)
}

// EnsureAPIKeyAvailable ensures an API key is available for the provider
func (u *UnifiedConfigImpl) EnsureAPIKeyAvailable(provider agent_config.ClientType) error {
	// Check if key already exists
	if key, err := u.GetAPIKey(provider); err == nil && key != "" {
		return nil
	}

	// Use agent config's existing logic
	return agent_config.EnsureAPIKeyAvailable(provider)
}

// GetModelForProvider gets the model for a provider
func (u *UnifiedConfigImpl) GetModelForProvider(provider agent_config.ClientType) string {
	// Use agent config for now since main config has different model structure
	return u.agentConfig.GetModelForProvider(provider)
}

// SetModelForProvider sets the model for a provider
func (u *UnifiedConfigImpl) SetModelForProvider(provider agent_config.ClientType, model string) {
	// Use agent config for now since main config has different model structure
	u.agentConfig.SetModelForProvider(provider, model)
}

// GetLastUsedProvider gets the last used provider
func (u *UnifiedConfigImpl) GetLastUsedProvider() agent_config.ClientType {
	// Use agent config for now
	return u.agentConfig.GetLastUsedProvider()
}

// SetLastUsedProvider sets the last used provider
func (u *UnifiedConfigImpl) SetLastUsedProvider(provider agent_config.ClientType) {
	// Use agent config for now
	u.agentConfig.SetLastUsedProvider(provider)
}

// Save persists both configs
func (u *UnifiedConfigImpl) Save() error {
	// Save agent config (main config saving is more complex and will be handled in later phases)
	return u.agentConfig.Save()
}

// Load reloads both configs
func (u *UnifiedConfigImpl) Load() error {
	// Reload main config
	mainCfg, err := config.LoadOrInitConfig(true)
	if err != nil {
		return err
	}
	u.mainConfig = mainCfg

	// Reload agent config
	agentCfg, err := agent_config.Load()
	if err != nil {
		// Create default if not exists
		agentCfg = agent_config.NewConfig()
	}
	u.agentConfig = agentCfg

	return nil
}

// Validate validates both configs
func (u *UnifiedConfigImpl) Validate() error {
	// Main config doesn't have a built-in Validate method
	// But we can do basic validation here
	if u.mainConfig == nil {
		return fmt.Errorf("main config is nil")
	}

	// Note: agent_config doesn't have a Validate method currently
	// Basic validation for now
	if u.agentConfig == nil {
		return fmt.Errorf("agent config is nil")
	}

	return nil
}

// MigrateFromAgentConfig migrates data from agent config to main config
func (u *UnifiedConfigImpl) MigrateFromAgentConfig() error {
	// For Phase 2, this is a placeholder - migration will be implemented in later phases
	// when main config is enhanced to support API keys and provider models properly
	return nil
}

// IsLegacyConfigPresent checks if agent config exists
func (u *UnifiedConfigImpl) IsLegacyConfigPresent() bool {
	configPath, err := agent_config.GetConfigPath()
	if err != nil {
		return false
	}

	_, err = os.Stat(configPath)
	return err == nil
}

// GetMainConfig returns the main config
func (u *UnifiedConfigImpl) GetMainConfig() *config.Config {
	return u.mainConfig
}

// GetAgentConfig returns the agent config
func (u *UnifiedConfigImpl) GetAgentConfig() *agent_config.Config {
	return u.agentConfig
}

// Helper functions to map between client types and strings

func (u *UnifiedConfigImpl) mapClientTypeToString(clientType agent_config.ClientType) string {
	switch clientType {
	case agent_config.OpenAIClientType:
		return "openai"

	case agent_config.OllamaClientType:
		return "ollama"
	case agent_config.DeepInfraClientType:
		return "deepinfra"

	case agent_config.OpenRouterClientType:
		return "openrouter"
	case agent_config.DeepSeekClientType:
		return "deepseek"
	default:
		return "unknown"
	}
}

func (u *UnifiedConfigImpl) mapStringToClientType(provider string) agent_config.ClientType {
	switch provider {
	case "openai":
		return agent_config.OpenAIClientType

	case "ollama":
		return agent_config.OllamaClientType
	case "deepinfra":
		return agent_config.DeepInfraClientType

	case "openrouter":
		return agent_config.OpenRouterClientType
	case "deepseek":
		return agent_config.DeepSeekClientType
	default:
		return agent_config.OpenAIClientType // default fallback
	}
}

// Helper functions for API key management

func (u *UnifiedConfigImpl) getAPIKeyFromStruct(keys *agent_config.APIKeys, provider agent_config.ClientType) string {
	switch provider {
	case agent_config.OpenAIClientType:
		return keys.OpenAI
	case agent_config.DeepInfraClientType:
		return keys.DeepInfra
	case agent_config.OpenRouterClientType:
		return keys.OpenRouter

	case agent_config.DeepSeekClientType:
		return keys.DeepSeek
	default:
		return ""
	}
}

func (u *UnifiedConfigImpl) setAPIKeyInStruct(keys *agent_config.APIKeys, provider agent_config.ClientType, apiKey string) {
	switch provider {
	case agent_config.OpenAIClientType:
		keys.OpenAI = apiKey
	case agent_config.DeepInfraClientType:
		keys.DeepInfra = apiKey
	case agent_config.OpenRouterClientType:
		keys.OpenRouter = apiKey

	case agent_config.DeepSeekClientType:
		keys.DeepSeek = apiKey
	}
}
