package providers

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

//go:embed configs/*.json
var embeddedConfigs embed.FS

// ProviderFactory creates provider instances from JSON configurations
type ProviderFactory struct {
	mu       sync.RWMutex
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

// loadConfigFromBytesUnlocked loads a provider configuration from byte data
// and inserts it into the factory's maps. It does NOT acquire any lock —
// the caller must hold f.mu.Lock().
// If config.Name is empty after unmarshaling, nameFallback is used as the key.
func (f *ProviderFactory) loadConfigFromBytesUnlocked(data []byte, nameFallback string) error {
	var config ProviderConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if config.Name == "" && nameFallback != "" {
		config.Name = nameFallback
	}

	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	f.configs[config.Name] = &config
	f.registry.ProviderConfigs[config.Name] = config

	return nil
}

// LoadConfigFromFile loads a single provider configuration from file
func (f *ProviderFactory) LoadConfigFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	nameFallback := ""
	base := filepath.Base(filename)
	if strings.HasSuffix(base, ".json") {
		nameFallback = base[:len(base)-5] // Remove .json extension
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.loadConfigFromBytesUnlocked(data, nameFallback)
}

// LoadConfigFromBytes loads a provider configuration from byte data
func (f *ProviderFactory) LoadConfigFromBytes(data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.loadConfigFromBytesUnlocked(data, "")
}

// CreateProvider creates a provider instance by name
func (f *ProviderFactory) CreateProvider(name string) (api.ClientInterface, error) {
	f.mu.RLock()
	config, exists := f.configs[name]
	if !exists {
		f.mu.RUnlock()
		return nil, fmt.Errorf("provider config not found: %s", name)
	}

	// Make a copy so we don't mutate the stored config.
	// NOTE: this is a shallow copy — nested references (Headers map[string]string,
	// Models.AvailableModels []string, etc.) still share the same underlying data.
	// This is intentional: NewGenericProvider does NOT mutate its input config
	// (it only reads fields like Defaults.Model and calls Validate()/GetTimeout()),
	// so the shallow copy is sufficient for our use.
	configCopy := *config
	f.mu.RUnlock()

	// Inject resolved credentials via the unified path (env → keyring → file store).
	// TrimSpace guards against whitespace in stored values.
	if resolved, resolveErr := credentials.ResolveProvider(name); resolveErr == nil && strings.TrimSpace(resolved.Value) != "" {
		configCopy.Auth.Key = strings.TrimSpace(resolved.Value)
	}

	return NewGenericProvider(&configCopy)
}

// CreateProviderWithModel creates a provider instance with a specific model
func (f *ProviderFactory) CreateProviderWithModel(name, model string) (api.ClientInterface, error) {
	provider, err := f.CreateProvider(name)
	if err != nil {
		return nil, fmt.Errorf("load embedded provider config: %w", err)
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
	f.mu.RLock()
	defer f.mu.RUnlock()

	var names []string
	for name := range f.configs {
		names = append(names, name)
	}
	return names
}

// GetProviderConfig returns a copy of the configuration for a provider.
// A copy is returned (rather than a pointer to internal state) so that
// callers cannot mutate the factory's stored config after the RLock is released.
func (f *ProviderFactory) GetProviderConfig(name string) (*ProviderConfig, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	config, exists := f.configs[name]
	if !exists {
		return nil, fmt.Errorf("provider config not found: %s", name)
	}
	cfgCopy := *config
	return &cfgCopy, nil
}

// GetRegistry returns a deep copy of the provider registry.
// A copy is returned (rather than a pointer to internal state) so that
// callers cannot mutate the factory's stored registry after the RLock is released.
func (f *ProviderFactory) GetRegistry() *ProviderRegistry {
	f.mu.RLock()
	defer f.mu.RUnlock()

	regCopy := *f.registry
	regCopy.ProviderConfigs = make(map[string]ProviderConfig, len(f.registry.ProviderConfigs))
	for k, v := range f.registry.ProviderConfigs {
		regCopy.ProviderConfigs[k] = v
	}
	if f.registry.EnabledProviders != nil {
		regCopy.EnabledProviders = make([]string, len(f.registry.EnabledProviders))
		copy(regCopy.EnabledProviders, f.registry.EnabledProviders)
	}
	return &regCopy
}

// ListProvidersWithModels returns all providers with their available models
func (f *ProviderFactory) ListProvidersWithModels() map[string][]string {
	f.mu.RLock()
	defer f.mu.RUnlock()

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
	f.mu.RLock()
	defer f.mu.RUnlock()

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
	f.mu.RLock()
	defer f.mu.RUnlock()

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

// UpsertConfig inserts or updates a provider configuration in the factory.
// The provided config is deep-copied so external mutations have no effect.
func (f *ProviderFactory) UpsertConfig(name string, cfg *ProviderConfig) {
	if cfg == nil {
		return
	}

	// Make a copy so external mutations don't affect our stored state.
	configCopy := *cfg

	f.mu.Lock()
	defer f.mu.Unlock()

	f.configs[name] = &configCopy
	f.registry.ProviderConfigs[name] = configCopy
}

// ReloadConfig reloads a provider configuration from file
func (f *ProviderFactory) ReloadConfig(filename string) error {
	// Remove existing config with same name (if any)
	base := filepath.Base(filename)
	name := base[:len(base)-5] // Remove .json extension

	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.configs[name]; exists {
		delete(f.configs, name)
		delete(f.registry.ProviderConfigs, name)
	}

	return f.loadConfigFromBytesUnlocked(data, name)
}
