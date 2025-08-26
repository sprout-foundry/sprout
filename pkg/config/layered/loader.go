package layered

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// ConfigurationLayer represents a single layer of configuration
type ConfigurationLayer struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"` // Higher priority overrides lower
	Source   string `json:"source"`   // file path, environment, etc.
	Data     *config.Config
}

// LayeredConfigLoader loads and manages multiple configuration layers
type LayeredConfigLoader struct {
	layers        []ConfigurationLayer
	mergedConfig  *config.Config
	watchHandlers []func()
}

// ConfigSource defines where configuration can be loaded from
type ConfigSource interface {
	Load(ctx context.Context) (*config.Config, error)
	Watch(ctx context.Context, callback func()) error
	GetName() string
	GetPriority() int
}

// NewLayeredConfigLoader creates a new layered configuration loader
func NewLayeredConfigLoader() *LayeredConfigLoader {
	return &LayeredConfigLoader{
		layers:        []ConfigurationLayer{},
		watchHandlers: []func(){},
	}
}

// AddConfigSource adds a configuration source to the loader
func (l *LayeredConfigLoader) AddConfigSource(source ConfigSource) error {
	ctx := context.Background()
	cfg, err := source.Load(ctx)
	if err != nil {
		// Non-fatal - configuration source might not exist yet
		cfg = config.DefaultConfig()
	}

	layer := ConfigurationLayer{
		Name:     source.GetName(),
		Priority: source.GetPriority(),
		Source:   source.GetName(),
		Data:     cfg,
	}

	l.layers = append(l.layers, layer)
	l.sortLayersByPriority()
	l.mergeConfigurations()

	return nil
}

// GetMergedConfig returns the final merged configuration
func (l *LayeredConfigLoader) GetMergedConfig() *config.Config {
	if l.mergedConfig == nil {
		l.mergeConfigurations()
	}
	return l.mergedConfig
}

// ReloadAll reloads all configuration sources
func (l *LayeredConfigLoader) ReloadAll(ctx context.Context) error {
	// For now, create a simplified reload - would integrate with actual sources
	l.mergeConfigurations()
	return nil
}

// sortLayersByPriority sorts layers by priority (highest first)
func (l *LayeredConfigLoader) sortLayersByPriority() {
	// Simple bubble sort by priority
	for i := 0; i < len(l.layers)-1; i++ {
		for j := 0; j < len(l.layers)-i-1; j++ {
			if l.layers[j].Priority < l.layers[j+1].Priority {
				l.layers[j], l.layers[j+1] = l.layers[j+1], l.layers[j]
			}
		}
	}
}

// mergeConfigurations merges all layers into a single configuration
func (l *LayeredConfigLoader) mergeConfigurations() {
	if len(l.layers) == 0 {
		l.mergedConfig = config.DefaultConfig()
		return
	}

	// Start with the lowest priority (last in sorted array)
	base := l.layers[len(l.layers)-1].Data
	if base == nil {
		base = config.DefaultConfig()
	}

	// Create a copy to avoid modifying the original
	merged := l.copyConfig(base)

	// Apply higher priority layers on top
	for i := len(l.layers) - 2; i >= 0; i-- {
		layer := l.layers[i]
		if layer.Data != nil {
			merged = l.mergeConfig(merged, layer.Data)
		}
	}

	l.mergedConfig = merged
}

// copyConfig creates a deep copy of a config (simplified)
func (l *LayeredConfigLoader) copyConfig(src *config.Config) *config.Config {
	// For now, use JSON marshal/unmarshal for deep copy
	data, err := json.Marshal(src)
	if err != nil {
		return config.DefaultConfig()
	}

	var dst config.Config
	if err := json.Unmarshal(data, &dst); err != nil {
		return config.DefaultConfig()
	}

	// Copy non-JSON fields manually
	dst.SkipPrompt = src.SkipPrompt

	return &dst
}

// mergeConfig merges overlay config into base config
func (l *LayeredConfigLoader) mergeConfig(base, overlay *config.Config) *config.Config {
	result := l.copyConfig(base)

	// Merge LLM configuration
	if overlay.LLM != nil {
		if result.LLM == nil {
			result.LLM = overlay.LLM
		} else {
			result.LLM = l.mergeLLMConfig(result.LLM, overlay.LLM)
		}
	}

	// Merge UI configuration
	if overlay.UI != nil {
		if result.UI == nil {
			result.UI = overlay.UI
		} else {
			result.UI = l.mergeUIConfig(result.UI, overlay.UI)
		}
	}

	// Merge Agent configuration
	if overlay.Agent != nil {
		if result.Agent == nil {
			result.Agent = overlay.Agent
		} else {
			result.Agent = l.mergeAgentConfig(result.Agent, overlay.Agent)
		}
	}

	// Merge Security configuration
	if overlay.Security != nil {
		if result.Security == nil {
			result.Security = overlay.Security
		} else {
			result.Security = l.mergeSecurityConfig(result.Security, overlay.Security)
		}
	}

	// Merge Performance configuration
	if overlay.Performance != nil {
		if result.Performance == nil {
			result.Performance = overlay.Performance
		} else {
			result.Performance = l.mergePerformanceConfig(result.Performance, overlay.Performance)
		}
	}

	// Override simple fields if they're set in overlay
	if overlay.SkipPrompt {
		result.SkipPrompt = overlay.SkipPrompt
	}

	return result
}

// mergeLLMConfig merges LLM configurations
func (l *LayeredConfigLoader) mergeLLMConfig(base, overlay *config.LLMConfig) *config.LLMConfig {
	result := *base // Shallow copy

	if overlay.EditingModel != "" {
		result.EditingModel = overlay.EditingModel
	}
	if overlay.SummaryModel != "" {
		result.SummaryModel = overlay.SummaryModel
	}
	if overlay.OrchestrationModel != "" {
		result.OrchestrationModel = overlay.OrchestrationModel
	}
	if overlay.WorkspaceModel != "" {
		result.WorkspaceModel = overlay.WorkspaceModel
	}
	if overlay.Temperature != 0 {
		result.Temperature = overlay.Temperature
	}
	if overlay.MaxTokens != 0 {
		result.MaxTokens = overlay.MaxTokens
	}

	return &result
}

// mergeUIConfig merges UI configurations
func (l *LayeredConfigLoader) mergeUIConfig(base, overlay *config.UIConfig) *config.UIConfig {
	result := *base // Shallow copy

	// Merge the actual fields that exist in UIConfig
	if overlay.JsonLogs != base.JsonLogs {
		result.JsonLogs = overlay.JsonLogs
	}
	if overlay.HealthChecks != base.HealthChecks {
		result.HealthChecks = overlay.HealthChecks
	}
	if overlay.PreapplyReview != base.PreapplyReview {
		result.PreapplyReview = overlay.PreapplyReview
	}
	if overlay.TelemetryEnabled != base.TelemetryEnabled {
		result.TelemetryEnabled = overlay.TelemetryEnabled
	}
	if overlay.TelemetryFile != "" && overlay.TelemetryFile != base.TelemetryFile {
		result.TelemetryFile = overlay.TelemetryFile
	}
	if overlay.TrackWithGit != base.TrackWithGit {
		result.TrackWithGit = overlay.TrackWithGit
	}
	if overlay.StagedEdits != base.StagedEdits {
		result.StagedEdits = overlay.StagedEdits
	}

	return &result
}

// mergeAgentConfig merges Agent configurations
func (l *LayeredConfigLoader) mergeAgentConfig(base, overlay *config.AgentConfig) *config.AgentConfig {
	result := *base // Shallow copy

	if overlay.OrchestrationMaxAttempts != 0 {
		result.OrchestrationMaxAttempts = overlay.OrchestrationMaxAttempts
	}
	if overlay.PolicyVariant != "" {
		result.PolicyVariant = overlay.PolicyVariant
	}
	if overlay.AutoGenerateTests != base.AutoGenerateTests {
		result.AutoGenerateTests = overlay.AutoGenerateTests
	}
	if overlay.DryRun != base.DryRun {
		result.DryRun = overlay.DryRun
	}
	if overlay.CodeToolsEnabled != base.CodeToolsEnabled {
		result.CodeToolsEnabled = overlay.CodeToolsEnabled
	}

	return &result
}

// mergeSecurityConfig merges Security configurations
func (l *LayeredConfigLoader) mergeSecurityConfig(base, overlay *config.SecurityConfig) *config.SecurityConfig {
	result := *base // Shallow copy

	if overlay.EnableSecurityChecks != base.EnableSecurityChecks {
		result.EnableSecurityChecks = overlay.EnableSecurityChecks
	}
	if len(overlay.ShellAllowlist) > 0 {
		result.ShellAllowlist = overlay.ShellAllowlist
	}
	if len(overlay.AllowedCommands) > 0 {
		result.AllowedCommands = overlay.AllowedCommands
	}
	if len(overlay.BlockedCommands) > 0 {
		result.BlockedCommands = overlay.BlockedCommands
	}
	if len(overlay.AllowedPaths) > 0 {
		result.AllowedPaths = overlay.AllowedPaths
	}
	if len(overlay.BlockedPaths) > 0 {
		result.BlockedPaths = overlay.BlockedPaths
	}
	if overlay.RequireApproval != base.RequireApproval {
		result.RequireApproval = overlay.RequireApproval
	}

	return &result
}

// mergePerformanceConfig merges Performance configurations
func (l *LayeredConfigLoader) mergePerformanceConfig(base, overlay *config.PerformanceConfig) *config.PerformanceConfig {
	result := *base // Shallow copy

	if overlay.MaxConcurrentRequests != 0 {
		result.MaxConcurrentRequests = overlay.MaxConcurrentRequests
	}
	if overlay.RequestDelayMs != 0 {
		result.RequestDelayMs = overlay.RequestDelayMs
	}
	if overlay.EmbeddingBatchSize != 0 {
		result.EmbeddingBatchSize = overlay.EmbeddingBatchSize
	}

	return &result
}

// LayeredConfigProvider adapts LayeredConfigLoader to interfaces.ConfigProvider
type LayeredConfigProvider struct {
	loader *LayeredConfigLoader
}

// NewLayeredConfigProvider creates a new layered configuration provider
func NewLayeredConfigProvider() *LayeredConfigProvider {
	return &LayeredConfigProvider{
		loader: NewLayeredConfigLoader(),
	}
}

// AddSource adds a configuration source
func (p *LayeredConfigProvider) AddSource(source ConfigSource) error {
	return p.loader.AddConfigSource(source)
}

// GetMergedConfig returns the merged configuration from all sources
func (p *LayeredConfigProvider) GetMergedConfig() *config.Config {
	return p.loader.GetMergedConfig()
}

// GetProviderConfig implements interfaces.ConfigProvider
func (p *LayeredConfigProvider) GetProviderConfig(providerName string) (*types.ProviderConfig, error) {
	merged := p.loader.GetMergedConfig()
	llmConfig := merged.GetLLMConfig()

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
		config.APIKey = "placeholder-key" // Would get from API keys file
	case "gemini":
		config.BaseURL = "https://generativelanguage.googleapis.com"
		config.APIKey = "placeholder-key"
	case "ollama":
		config.BaseURL = llmConfig.OllamaServerURL
	default:
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	return config, nil
}

// GetAgentConfig implements interfaces.ConfigProvider
func (p *LayeredConfigProvider) GetAgentConfig() *types.AgentConfig {
	merged := p.loader.GetMergedConfig()
	agentConfig := merged.GetAgentConfig()

	return &types.AgentConfig{
		MaxRetries:         agentConfig.OrchestrationMaxAttempts,
		RetryDelay:         5,
		MaxContextRequests: 10,
		EnableValidation:   !merged.SkipPrompt,
		EnableCodeReview:   !merged.SkipPrompt,
		ValidationTimeout:  30,
		DefaultStrategy:    "quick",
		CostThreshold:      0.1,
	}
}

// GetEditorConfig implements interfaces.ConfigProvider
func (p *LayeredConfigProvider) GetEditorConfig() *types.EditorConfig {
	return &types.EditorConfig{
		BackupEnabled:     true,
		DiffStyle:         "unified",
		AutoFormat:        true,
		PreferredLanguage: "go",
		IgnorePatterns:    []string{"*.test", "*.tmp"},
		MaxFileSize:       1024 * 1024,
	}
}

// GetSecurityConfig implements interfaces.ConfigProvider
func (p *LayeredConfigProvider) GetSecurityConfig() *types.SecurityConfig {
	merged := p.loader.GetMergedConfig()
	securityConfig := merged.GetSecurityConfig()

	return &types.SecurityConfig{
		EnableCredentialScanning: securityConfig.EnableSecurityChecks,
		BlockedPatterns:          securityConfig.BlockedCommands, // Map blocked commands to blocked patterns
		AllowedCommands:          securityConfig.AllowedCommands,
		RequireConfirmation:      !merged.SkipPrompt,
	}
}

// GetUIConfig implements interfaces.ConfigProvider
func (p *LayeredConfigProvider) GetUIConfig() *types.UIConfig {
	merged := p.loader.GetMergedConfig()
	uiConfig := merged.GetUIConfig()

	return &types.UIConfig{
		SkipPrompts:    merged.SkipPrompt,
		ColorOutput:    true,              // Default to true
		VerboseLogging: uiConfig.JsonLogs, // Map JsonLogs to VerboseLogging
		ProgressBars:   uiConfig.ShouldDisplayProgress(),
		OutputFormat:   "text",
	}
}

// SetConfig implements interfaces.ConfigProvider
func (p *LayeredConfigProvider) SetConfig(key string, value interface{}) error {
	// Updates would be applied to the highest priority writable layer
	return fmt.Errorf("configuration updates not implemented in layered provider")
}

// SaveConfig implements interfaces.ConfigProvider
func (p *LayeredConfigProvider) SaveConfig() error {
	return fmt.Errorf("configuration saving not implemented in layered provider")
}

// ReloadConfig implements interfaces.ConfigProvider
func (p *LayeredConfigProvider) ReloadConfig() error {
	return p.loader.ReloadAll(context.Background())
}

// WatchConfig implements interfaces.ConfigProvider
func (p *LayeredConfigProvider) WatchConfig(callback func()) error {
	p.loader.watchHandlers = append(p.loader.watchHandlers, callback)
	return nil
}
