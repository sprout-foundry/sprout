package providers

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

//go:embed configs/*.json
var embeddedConfigs embed.FS

// ProviderFactory creates provider instances from JSON configurations
type ProviderFactory struct {
	registry *ProviderRegistry
	configs  map[string]*ProviderConfig
}

// NewProviderFactory creates a new provider factory
func NewProviderFactory() *ProviderFactory {
	return &ProviderFactory{
		registry: &ProviderRegistry{
			ProviderConfigs: make(map[string]ProviderConfig),
		},
		configs: make(map[string]*ProviderConfig),
	}
}

// LoadConfigsFromDirectory loads all provider configurations from a directory
func (f *ProviderFactory) LoadConfigsFromDirectory(configDir string) error {
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		return fmt.Errorf("config directory does not exist: %s", configDir)
	}

	files, err := filepath.Glob(filepath.Join(configDir, "*.json"))
	if err != nil {
		return fmt.Errorf("failed to glob config files: %w", err)
	}

	for _, file := range files {
		if err := f.LoadConfigFromFile(file); err != nil {
			return fmt.Errorf("failed to load config from %s: %w", file, err)
		}
	}

	return nil
}

// LoadEmbeddedConfigs loads all provider configurations from the embedded filesystem
func (f *ProviderFactory) LoadEmbeddedConfigs() error {
	entries, err := embeddedConfigs.ReadDir("configs")
	if err != nil {
		return fmt.Errorf("failed to read embedded configs directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filename := filepath.Join("configs", entry.Name())
		data, err := embeddedConfigs.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("failed to read embedded config file %s: %w", filename, err)
		}

		if err := f.LoadConfigFromBytes(data); err != nil {
			return fmt.Errorf("failed to load embedded config from %s: %w", filename, err)
		}
	}

	return nil
}

// LoadConfigFromFile loads a single provider configuration from file
func (f *ProviderFactory) LoadConfigFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config ProviderConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Extract provider name from filename if not set
	if config.Name == "" {
		base := filepath.Base(filename)
		config.Name = base[:len(base)-5] // Remove .json extension
	}

	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config in %s: %w", filename, err)
	}

	f.configs[config.Name] = &config
	f.registry.ProviderConfigs[config.Name] = config

	return nil
}

// LoadConfigFromBytes loads a provider configuration from byte data
func (f *ProviderFactory) LoadConfigFromBytes(data []byte) error {
	var config ProviderConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	f.configs[config.Name] = &config
	f.registry.ProviderConfigs[config.Name] = config

	return nil
}

// CreateProvider creates a provider instance by name
func (f *ProviderFactory) CreateProvider(name string) (api.ClientInterface, error) {
	config, exists := f.configs[name]
	if !exists {
		return nil, fmt.Errorf("provider config not found: %s", name)
	}

	return NewGenericProvider(config)
}

// CreateProviderWithModel creates a provider instance with a specific model
func (f *ProviderFactory) CreateProviderWithModel(name, model string) (api.ClientInterface, error) {
	provider, err := f.CreateProvider(name)
	if err != nil {
		return nil, err
	}

	// Only set model if it's not empty - otherwise use the default model from config
	if model != "" {
		if err := provider.SetModel(model); err != nil {
			return nil, fmt.Errorf("failed to set model %s: %w", model, err)
		}
	}

	return provider, nil
}

// GetAvailableProviders returns a list of available provider names
func (f *ProviderFactory) GetAvailableProviders() []string {
	var names []string
	for name := range f.configs {
		names = append(names, name)
	}
	return names
}

// GetProviderConfig returns the configuration for a provider
func (f *ProviderFactory) GetProviderConfig(name string) (*ProviderConfig, error) {
	config, exists := f.configs[name]
	if !exists {
		return nil, fmt.Errorf("provider config not found: %s", name)
	}
	return config, nil
}

// GetRegistry returns the provider registry
func (f *ProviderFactory) GetRegistry() *ProviderRegistry {
	return f.registry
}

// ListProvidersWithModels returns all providers with their available models
func (f *ProviderFactory) ListProvidersWithModels() map[string][]string {
	result := make(map[string][]string)

	for name, config := range f.configs {
		if len(config.Models.AvailableModels) > 0 {
			result[name] = config.Models.AvailableModels
		} else {
			// If no explicit models, use default model
			result[name] = []string{config.Defaults.Model}
		}
	}

	return result
}

// ValidateProvider checks if a provider and model combination is valid
func (f *ProviderFactory) ValidateProvider(providerName, modelName string) error {
	config, exists := f.configs[providerName]
	if !exists {
		return fmt.Errorf("provider not found: %s", providerName)
	}

	// Check if model is in available models list (if specified)
	if len(config.Models.AvailableModels) > 0 {
		found := false
		for _, availableModel := range config.Models.AvailableModels {
			if availableModel == modelName {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("model %s not available for provider %s", modelName, providerName)
		}
	}

	return nil
}

// GetDefaultProvider returns the default provider name
func (f *ProviderFactory) GetDefaultProvider() string {
	// Try to find a provider marked as default in registry
	if f.registry.DefaultProvider != "" {
		return f.registry.DefaultProvider
	}

	// Fallback to first available provider
	if len(f.configs) > 0 {
		for name := range f.configs {
			return name
		}
	}

	return ""
}

// ReloadConfig reloads a provider configuration from file
func (f *ProviderFactory) ReloadConfig(filename string) error {
	// Remove existing config with same name (if any)
	base := filepath.Base(filename)
	name := base[:len(base)-5] // Remove .json extension

	if _, exists := f.configs[name]; exists {
		delete(f.configs, name)
		delete(f.registry.ProviderConfigs, name)
	}

	return f.LoadConfigFromFile(filename)
}
