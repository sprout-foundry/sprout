package layered

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
)

// FileConfigSource loads configuration from a JSON file
type FileConfigSource struct {
	FilePath string
	Name     string
	Priority int
	Required bool // If true, error if file doesn't exist
}

// NewFileConfigSource creates a new file-based configuration source
func NewFileConfigSource(filePath, name string, priority int, required bool) *FileConfigSource {
	return &FileConfigSource{
		FilePath: filePath,
		Name:     name,
		Priority: priority,
		Required: required,
	}
}

// Load implements ConfigSource.Load
func (f *FileConfigSource) Load(ctx context.Context) (*config.Config, error) {
	if _, err := os.Stat(f.FilePath); os.IsNotExist(err) {
		if f.Required {
			return nil, fmt.Errorf("required config file not found: %s", f.FilePath)
		}
		return config.DefaultConfig(), nil
	}

	data, err := os.ReadFile(f.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", f.FilePath, err)
	}

	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", f.FilePath, err)
	}

	// Ensure sub-configs are initialized
	if cfg.LLM == nil {
		cfg.LLM = config.DefaultLLMConfig()
	}
	if cfg.UI == nil {
		cfg.UI = config.DefaultUIConfig()
	}
	if cfg.Agent == nil {
		cfg.Agent = config.DefaultAgentConfig()
	}
	if cfg.Security == nil {
		cfg.Security = config.DefaultSecurityConfig()
	}
	if cfg.Performance == nil {
		cfg.Performance = config.DefaultPerformanceConfig()
	}

	return &cfg, nil
}

// Watch implements ConfigSource.Watch
func (f *FileConfigSource) Watch(ctx context.Context, callback func()) error {
	// For now, return not implemented - would integrate with file system watcher
	return fmt.Errorf("file watching not implemented for config source: %s", f.Name)
}

// GetName implements ConfigSource.GetName
func (f *FileConfigSource) GetName() string {
	return f.Name
}

// GetPriority implements ConfigSource.GetPriority
func (f *FileConfigSource) GetPriority() int {
	return f.Priority
}

// EnvironmentConfigSource loads configuration from environment variables
type EnvironmentConfigSource struct {
	Prefix   string // Environment variable prefix (e.g., "LEDIT_")
	Name     string
	Priority int
}

// NewEnvironmentConfigSource creates a new environment-based configuration source
func NewEnvironmentConfigSource(prefix, name string, priority int) *EnvironmentConfigSource {
	return &EnvironmentConfigSource{
		Prefix:   prefix,
		Name:     name,
		Priority: priority,
	}
}

// Load implements ConfigSource.Load
func (e *EnvironmentConfigSource) Load(ctx context.Context) (*config.Config, error) {
	cfg := config.DefaultConfig()

	// Map environment variables to configuration
	envVars := e.getEnvironmentVariables()

	for key, value := range envVars {
		switch strings.ToLower(key) {
		case "editing_model":
			if cfg.LLM == nil {
				cfg.LLM = config.DefaultLLMConfig()
			}
			cfg.LLM.EditingModel = value
		case "summary_model":
			if cfg.LLM == nil {
				cfg.LLM = config.DefaultLLMConfig()
			}
			cfg.LLM.SummaryModel = value
		case "orchestration_model":
			if cfg.LLM == nil {
				cfg.LLM = config.DefaultLLMConfig()
			}
			cfg.LLM.OrchestrationModel = value
		case "temperature":
			if temp, err := strconv.ParseFloat(value, 64); err == nil {
				if cfg.LLM == nil {
					cfg.LLM = config.DefaultLLMConfig()
				}
				cfg.LLM.Temperature = temp
			}
		case "max_tokens":
			if tokens, err := strconv.Atoi(value); err == nil {
				if cfg.LLM == nil {
					cfg.LLM = config.DefaultLLMConfig()
				}
				cfg.LLM.MaxTokens = tokens
			}
		case "skip_prompts":
			if skip, err := strconv.ParseBool(value); err == nil {
				cfg.SkipPrompt = skip
			}
		case "verbose_logging":
			if verbose, err := strconv.ParseBool(value); err == nil {
				if cfg.UI == nil {
					cfg.UI = config.DefaultUIConfig()
				}
				cfg.UI.JsonLogs = verbose
			}
		case "color_output":
			// Color output is handled at display level, not stored in config
			// Can be ignored for now
		case "orchestration_max_attempts":
			if attempts, err := strconv.Atoi(value); err == nil {
				if cfg.Agent == nil {
					cfg.Agent = config.DefaultAgentConfig()
				}
				cfg.Agent.OrchestrationMaxAttempts = attempts
			}
		case "dry_run":
			if dryRun, err := strconv.ParseBool(value); err == nil {
				if cfg.Agent == nil {
					cfg.Agent = config.DefaultAgentConfig()
				}
				cfg.Agent.DryRun = dryRun
			}
		case "enable_credential_scanning":
			if enable, err := strconv.ParseBool(value); err == nil {
				if cfg.Security == nil {
					cfg.Security = config.DefaultSecurityConfig()
				}
				cfg.Security.EnableSecurityChecks = enable
			}
		case "max_concurrent_requests":
			if maxReq, err := strconv.Atoi(value); err == nil {
				if cfg.Performance == nil {
					cfg.Performance = config.DefaultPerformanceConfig()
				}
				cfg.Performance.MaxConcurrentRequests = maxReq
			}
		}
	}

	return cfg, nil
}

// getEnvironmentVariables gets environment variables with the configured prefix
func (e *EnvironmentConfigSource) getEnvironmentVariables() map[string]string {
	envVars := make(map[string]string)

	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key, value := parts[0], parts[1]
		if strings.HasPrefix(key, e.Prefix) {
			configKey := strings.TrimPrefix(key, e.Prefix)
			envVars[strings.ToLower(configKey)] = value
		}
	}

	return envVars
}

// Watch implements ConfigSource.Watch
func (e *EnvironmentConfigSource) Watch(ctx context.Context, callback func()) error {
	// Environment variables don't change during process lifetime typically
	return nil
}

// GetName implements ConfigSource.GetName
func (e *EnvironmentConfigSource) GetName() string {
	return e.Name
}

// GetPriority implements ConfigSource.GetPriority
func (e *EnvironmentConfigSource) GetPriority() int {
	return e.Priority
}

// DefaultsConfigSource provides default configuration values
type DefaultsConfigSource struct {
	Name     string
	Priority int
}

// NewDefaultsConfigSource creates a new defaults configuration source
func NewDefaultsConfigSource(name string, priority int) *DefaultsConfigSource {
	return &DefaultsConfigSource{
		Name:     name,
		Priority: priority,
	}
}

// Load implements ConfigSource.Load
func (d *DefaultsConfigSource) Load(ctx context.Context) (*config.Config, error) {
	return config.DefaultConfig(), nil
}

// Watch implements ConfigSource.Watch
func (d *DefaultsConfigSource) Watch(ctx context.Context, callback func()) error {
	// Defaults don't change
	return nil
}

// GetName implements ConfigSource.GetName
func (d *DefaultsConfigSource) GetName() string {
	return d.Name
}

// GetPriority implements ConfigSource.GetPriority
func (d *DefaultsConfigSource) GetPriority() int {
	return d.Priority
}

// ConfigurationFactory creates standard configuration setups
type ConfigurationFactory struct{}

// NewConfigurationFactory creates a new configuration factory
func NewConfigurationFactory() *ConfigurationFactory {
	return &ConfigurationFactory{}
}

// CreateStandardSetup creates a standard layered configuration setup
func (f *ConfigurationFactory) CreateStandardSetup() (*LayeredConfigProvider, error) {
	provider := NewLayeredConfigProvider()

	// Add configuration sources in priority order (lowest to highest)

	// 1. Defaults (lowest priority - 0)
	defaultsSource := NewDefaultsConfigSource("defaults", 0)
	if err := provider.AddSource(defaultsSource); err != nil {
		return nil, fmt.Errorf("failed to add defaults source: %w", err)
	}

	// 2. Global configuration (priority 10)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		globalConfigPath := filepath.Join(homeDir, ".ledit", "config.json")
		globalSource := NewFileConfigSource(globalConfigPath, "global", 10, false)
		if err := provider.AddSource(globalSource); err != nil {
			return nil, fmt.Errorf("failed to add global source: %w", err)
		}
	}

	// 3. Project configuration (priority 20)
	if cwd, err := os.Getwd(); err == nil {
		projectConfigPath := filepath.Join(cwd, ".ledit", "config.json")
		projectSource := NewFileConfigSource(projectConfigPath, "project", 20, false)
		if err := provider.AddSource(projectSource); err != nil {
			return nil, fmt.Errorf("failed to add project source: %w", err)
		}
	}

	// 4. Environment variables (highest priority - 30)
	envSource := NewEnvironmentConfigSource("LEDIT_", "environment", 30)
	if err := provider.AddSource(envSource); err != nil {
		return nil, fmt.Errorf("failed to add environment source: %w", err)
	}

	return provider, nil
}

// CreateDevelopmentSetup creates a development-specific configuration setup
func (f *ConfigurationFactory) CreateDevelopmentSetup() (*LayeredConfigProvider, error) {
	provider := NewLayeredConfigProvider()

	// Add development-specific sources
	defaultsSource := NewDefaultsConfigSource("defaults", 0)
	provider.AddSource(defaultsSource)

	// Development config with higher verbosity and debugging enabled
	devConfig := config.DefaultConfig()
	if devConfig.UI == nil {
		devConfig.UI = config.DefaultUIConfig()
	}
	devConfig.UI.JsonLogs = true // Enable verbose logging via JsonLogs
	devConfig.UI.HealthChecks = true
	devConfig.SkipPrompt = false // Enable prompts in development

	// Create a memory-based source for dev config
	devSource := &MemoryConfigSource{
		Name:     "development",
		Priority: 50,
		Config:   devConfig,
	}
	provider.AddSource(devSource)

	return provider, nil
}

// MemoryConfigSource holds configuration in memory
type MemoryConfigSource struct {
	Name     string
	Priority int
	Config   *config.Config
}

// Load implements ConfigSource.Load
func (m *MemoryConfigSource) Load(ctx context.Context) (*config.Config, error) {
	if m.Config == nil {
		return config.DefaultConfig(), nil
	}
	return m.Config, nil
}

// Watch implements ConfigSource.Watch
func (m *MemoryConfigSource) Watch(ctx context.Context, callback func()) error {
	return nil // Memory config doesn't change externally
}

// GetName implements ConfigSource.GetName
func (m *MemoryConfigSource) GetName() string {
	return m.Name
}

// GetPriority implements ConfigSource.GetPriority
func (m *MemoryConfigSource) GetPriority() int {
	return m.Priority
}
