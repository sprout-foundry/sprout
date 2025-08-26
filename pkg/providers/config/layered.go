package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// LayeredProvider implements a layered configuration system
type LayeredProvider struct {
	mu             sync.RWMutex
	globalConfig   map[string]interface{}
	userConfig     map[string]interface{}
	projectConfig  map[string]interface{}
	runtimeConfig  map[string]interface{}
	watchCallbacks []func()

	globalPath  string
	userPath    string
	projectPath string
}

// NewLayeredProvider creates a new layered configuration provider
func NewLayeredProvider() *LayeredProvider {
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()

	return &LayeredProvider{
		globalConfig:  make(map[string]interface{}),
		userConfig:    make(map[string]interface{}),
		projectConfig: make(map[string]interface{}),
		runtimeConfig: make(map[string]interface{}),
		globalPath:    "/etc/ledit/config.json",
		userPath:      filepath.Join(home, ".ledit", "config.json"),
		projectPath:   filepath.Join(wd, ".ledit", "config.json"),
	}
}

// GetProviderConfig returns configuration for a specific provider
func (p *LayeredProvider) GetProviderConfig(providerName string) (*types.ProviderConfig, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Look for provider config in layers (runtime > project > user > global)
	providerKey := "providers." + providerName

	var config map[string]interface{}

	// Check runtime config first
	if val, exists := p.runtimeConfig[providerKey]; exists {
		if configMap, ok := val.(map[string]interface{}); ok {
			config = configMap
		}
	}

	// Check project config
	if config == nil {
		if val, exists := p.projectConfig[providerKey]; exists {
			if configMap, ok := val.(map[string]interface{}); ok {
				config = configMap
			}
		}
	}

	// Check user config
	if config == nil {
		if val, exists := p.userConfig[providerKey]; exists {
			if configMap, ok := val.(map[string]interface{}); ok {
				config = configMap
			}
		}
	}

	// Check global config
	if config == nil {
		if val, exists := p.globalConfig[providerKey]; exists {
			if configMap, ok := val.(map[string]interface{}); ok {
				config = configMap
			}
		}
	}

	if config == nil {
		return nil, fmt.Errorf("provider configuration for '%s' not found", providerName)
	}

	// Convert map to ProviderConfig struct
	return p.mapToProviderConfig(providerName, config)
}

// GetAgentConfig returns agent configuration
func (p *LayeredProvider) GetAgentConfig() *types.AgentConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()

	config := &types.AgentConfig{
		MaxRetries:         3,
		RetryDelay:         5,
		MaxContextRequests: 5,
		EnableValidation:   true,
		EnableCodeReview:   true,
		ValidationTimeout:  300,
		DefaultStrategy:    "auto",
		CostThreshold:      10.0,
	}

	// Override with layered config values
	p.applyLayeredConfig("agent", config)

	return config
}

// GetEditorConfig returns editor configuration
func (p *LayeredProvider) GetEditorConfig() *types.EditorConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()

	config := &types.EditorConfig{
		BackupEnabled:     true,
		DiffStyle:         "unified",
		AutoFormat:        false,
		PreferredLanguage: "",
		IgnorePatterns:    []string{"*.log", "*.tmp", "node_modules/*"},
		MaxFileSize:       10 * 1024 * 1024, // 10MB
	}

	p.applyLayeredConfig("editor", config)

	return config
}

// GetSecurityConfig returns security configuration
func (p *LayeredProvider) GetSecurityConfig() *types.SecurityConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()

	config := &types.SecurityConfig{
		EnableCredentialScanning: true,
		BlockedPatterns: []string{
			"password\\s*=\\s*['\"][^'\"]+['\"]",
			"api[_-]?key\\s*=\\s*['\"][^'\"]+['\"]",
			"secret\\s*=\\s*['\"][^'\"]+['\"]",
		},
		AllowedCommands:     []string{"git", "npm", "go", "python", "node"},
		RequireConfirmation: true,
	}

	p.applyLayeredConfig("security", config)

	return config
}

// GetUIConfig returns UI configuration
func (p *LayeredProvider) GetUIConfig() *types.UIConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()

	config := &types.UIConfig{
		SkipPrompts:    false,
		ColorOutput:    true,
		VerboseLogging: false,
		ProgressBars:   true,
		OutputFormat:   "text",
	}

	p.applyLayeredConfig("ui", config)

	return config
}

// SetConfig updates configuration values
func (p *LayeredProvider) SetConfig(key string, value interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Set in runtime config
	p.runtimeConfig[key] = value

	// Notify watchers
	p.notifyWatchers()

	return nil
}

// SaveConfig saves the current configuration to disk
func (p *LayeredProvider) SaveConfig() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Save user config (most appropriate place for user changes)
	if err := p.ensureConfigDir(filepath.Dir(p.userPath)); err != nil {
		return fmt.Errorf("failed to ensure config directory: %w", err)
	}

	// Merge runtime changes into user config for persistence
	merged := make(map[string]interface{})
	for k, v := range p.userConfig {
		merged[k] = v
	}
	for k, v := range p.runtimeConfig {
		merged[k] = v
	}

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(p.userPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ReloadConfig reloads configuration from disk
func (p *LayeredProvider) ReloadConfig() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clear existing config
	p.globalConfig = make(map[string]interface{})
	p.userConfig = make(map[string]interface{})
	p.projectConfig = make(map[string]interface{})

	// Load layers in order
	_ = p.loadConfigFile(p.globalPath, p.globalConfig)   // Ignore errors for global config
	_ = p.loadConfigFile(p.userPath, p.userConfig)       // Ignore errors for user config
	_ = p.loadConfigFile(p.projectPath, p.projectConfig) // Ignore errors for project config

	// Notify watchers
	p.notifyWatchers()

	return nil
}

// WatchConfig watches for configuration changes
func (p *LayeredProvider) WatchConfig(callback func()) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.watchCallbacks = append(p.watchCallbacks, callback)

	// TODO: Implement file system watcher for config files
	return nil
}

// Helper methods

func (p *LayeredProvider) loadConfigFile(path string, target map[string]interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Ignore missing files
		}
		return err
	}

	return json.Unmarshal(data, &target)
}

func (p *LayeredProvider) ensureConfigDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

func (p *LayeredProvider) mapToProviderConfig(name string, config map[string]interface{}) (*types.ProviderConfig, error) {
	// Convert map to JSON then to struct for easy mapping
	data, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal provider config: %w", err)
	}

	var providerConfig types.ProviderConfig
	if err := json.Unmarshal(data, &providerConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal provider config: %w", err)
	}

	// Ensure name is set
	if providerConfig.Name == "" {
		providerConfig.Name = name
	}

	return &providerConfig, nil
}

func (p *LayeredProvider) applyLayeredConfig(section string, target interface{}) {
	// Get layered values for the section
	layers := []map[string]interface{}{
		p.globalConfig,
		p.userConfig,
		p.projectConfig,
		p.runtimeConfig,
	}

	// Build merged config
	merged := make(map[string]interface{})
	for _, layer := range layers {
		if sectionConfig, exists := layer[section]; exists {
			if sectionMap, ok := sectionConfig.(map[string]interface{}); ok {
				for k, v := range sectionMap {
					merged[k] = v
				}
			}
		}
	}

	// Apply to target struct
	if len(merged) > 0 {
		data, _ := json.Marshal(merged)
		json.Unmarshal(data, target) // Ignore errors, keep defaults
	}
}

func (p *LayeredProvider) notifyWatchers() {
	callbacks := make([]func(), len(p.watchCallbacks))
	copy(callbacks, p.watchCallbacks)

	for _, callback := range callbacks {
		go callback() // Call asynchronously
	}
}

// GetLayeredValue gets a value from the layered configuration
func (p *LayeredProvider) GetLayeredValue(key string) (interface{}, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Check layers in priority order (runtime > project > user > global)
	layers := []map[string]interface{}{
		p.runtimeConfig,
		p.projectConfig,
		p.userConfig,
		p.globalConfig,
	}

	for _, layer := range layers {
		if value, exists := layer[key]; exists {
			return value, true
		}
	}

	return nil, false
}

// SetProjectConfig sets project-specific configuration
func (p *LayeredProvider) SetProjectConfig(key string, value interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.projectConfig[key] = value

	// Optionally save to project config file
	if err := p.ensureConfigDir(filepath.Dir(p.projectPath)); err != nil {
		return fmt.Errorf("failed to ensure project config directory: %w", err)
	}

	data, err := json.MarshalIndent(p.projectConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal project config: %w", err)
	}

	if err := os.WriteFile(p.projectPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write project config file: %w", err)
	}

	p.notifyWatchers()

	return nil
}

// Verify LayeredProvider implements ConfigProvider interface
var _ interfaces.ConfigProvider = (*LayeredProvider)(nil)
