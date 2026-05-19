package configuration

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// MergeConfig merges two configs, with override taking precedence over base.
// The override config typically contains only changed fields (deltas).
// Returns a new config without modifying either input.
func MergeConfig(base, override *Config) *Config {
	if base == nil {
		return cloneConfig(override)
	}
	if override == nil {
		return cloneConfig(base)
	}

	result := cloneConfig(base)

	// Override simple string fields if non-empty
	if override.LastUsedProvider != "" {
		result.LastUsedProvider = override.LastUsedProvider
	}

	// Merge ProviderModels - override takes precedence
	if len(override.ProviderModels) > 0 {
		if result.ProviderModels == nil {
			result.ProviderModels = make(map[string]string)
		}
		for k, v := range override.ProviderModels {
			result.ProviderModels[k] = v
		}
	}

	// Override slices if non-empty
	if len(override.ProviderPriority) > 0 {
		result.ProviderPriority = override.ProviderPriority
	}

	// Merge MCP config
	if override.MCP.Enabled {
		result.MCP.Enabled = override.MCP.Enabled
	}
	if override.MCP.Timeout > 0 {
		result.MCP.Timeout = override.MCP.Timeout
	}
	if override.MCP.Servers != nil {
		if result.MCP.Servers == nil {
			result.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		for k, v := range override.MCP.Servers {
			result.MCP.Servers[k] = v
		}
	}

	// Merge Preferences
	if len(override.Preferences) > 0 {
		if result.Preferences == nil {
			result.Preferences = make(map[string]interface{})
		}
		for k, v := range override.Preferences {
			result.Preferences[k] = v
		}
	}

	// Override simple bool/int/string fields
	if override.AllowOrchestratorGitWrite {
		result.AllowOrchestratorGitWrite = override.AllowOrchestratorGitWrite
	}
	if override.ResourceDirectory != "" {
		result.ResourceDirectory = override.ResourceDirectory
	}
	if override.ReasoningEffort != "" {
		result.ReasoningEffort = override.ReasoningEffort
	}
	if override.DisableThinking {
		result.DisableThinking = override.DisableThinking
	}
	if override.SystemPromptText != "" {
		result.SystemPromptText = override.SystemPromptText
	}
	if override.SkipPrompt {
		result.SkipPrompt = override.SkipPrompt
	}

	// Merge DismissedPrompts
	if len(override.DismissedPrompts) > 0 {
		if result.DismissedPrompts == nil {
			result.DismissedPrompts = make(map[string]bool)
		}
		for k, v := range override.DismissedPrompts {
			result.DismissedPrompts[k] = v
		}
	}

	// Merge APITimeouts
	if override.APITimeouts != nil {
		if result.APITimeouts == nil {
			result.APITimeouts = &APITimeoutConfig{}
		}
		if override.APITimeouts.ConnectionTimeoutSec > 0 {
			result.APITimeouts.ConnectionTimeoutSec = override.APITimeouts.ConnectionTimeoutSec
		}
		if override.APITimeouts.FirstChunkTimeoutSec > 0 {
			result.APITimeouts.FirstChunkTimeoutSec = override.APITimeouts.FirstChunkTimeoutSec
		}
		if override.APITimeouts.ChunkTimeoutSec > 0 {
			result.APITimeouts.ChunkTimeoutSec = override.APITimeouts.ChunkTimeoutSec
		}
		if override.APITimeouts.OverallTimeoutSec > 0 {
			result.APITimeouts.OverallTimeoutSec = override.APITimeouts.OverallTimeoutSec
		}
		if override.APITimeouts.CommitMessageTimeoutSec > 0 {
			result.APITimeouts.CommitMessageTimeoutSec = override.APITimeouts.CommitMessageTimeoutSec
		}
	}

	// Merge EmbeddingIndex
	if override.EmbeddingIndex != nil {
		if result.EmbeddingIndex == nil {
			result.EmbeddingIndex = &EmbeddingIndexConfig{}
		}
		if override.EmbeddingIndex.Enabled {
			result.EmbeddingIndex.Enabled = override.EmbeddingIndex.Enabled
		}
		if override.EmbeddingIndex.Provider != "" {
			result.EmbeddingIndex.Provider = override.EmbeddingIndex.Provider
		}
		if override.EmbeddingIndex.IndexDir != "" {
			result.EmbeddingIndex.IndexDir = override.EmbeddingIndex.IndexDir
		}
		if override.EmbeddingIndex.SimilarityThreshold > 0 {
			result.EmbeddingIndex.SimilarityThreshold = override.EmbeddingIndex.SimilarityThreshold
		}
		if override.EmbeddingIndex.MaxResults > 0 {
			result.EmbeddingIndex.MaxResults = override.EmbeddingIndex.MaxResults
		}
		if override.EmbeddingIndex.AutoIndex {
			result.EmbeddingIndex.AutoIndex = override.EmbeddingIndex.AutoIndex
		}
		if len(override.EmbeddingIndex.ExcludePaths) > 0 {
			result.EmbeddingIndex.ExcludePaths = append([]string{}, override.EmbeddingIndex.ExcludePaths...)
		}
	}

	// Merge PersistentContext
	if override.PersistentContext != nil {
		if result.PersistentContext == nil {
			result.PersistentContext = &PersistentContextConfig{}
		}
		if override.PersistentContext.ProactiveContextEnabled != nil {
			result.PersistentContext.ProactiveContextEnabled = override.PersistentContext.ProactiveContextEnabled
		}
		if override.PersistentContext.MaxContextualResults > 0 {
			result.PersistentContext.MaxContextualResults = override.PersistentContext.MaxContextualResults
		}
		if override.PersistentContext.MinRelevanceScore > 0 {
			result.PersistentContext.MinRelevanceScore = override.PersistentContext.MinRelevanceScore
		}
		if override.PersistentContext.MaxContextChars > 0 {
			result.PersistentContext.MaxContextChars = override.PersistentContext.MaxContextChars
		}
		if override.PersistentContext.WorkspaceScopedRetrieval {
			result.PersistentContext.WorkspaceScopedRetrieval = override.PersistentContext.WorkspaceScopedRetrieval
		}
		if override.PersistentContext.DriftDetectionEnabled != nil {
			result.PersistentContext.DriftDetectionEnabled = override.PersistentContext.DriftDetectionEnabled
		}
		if override.PersistentContext.DriftThreshold > 0 {
			result.PersistentContext.DriftThreshold = override.PersistentContext.DriftThreshold
		}
		if override.PersistentContext.DriftCheckInterval > 0 {
			result.PersistentContext.DriftCheckInterval = override.PersistentContext.DriftCheckInterval
		}
	}

	// Merge CustomProviders
	if len(override.CustomProviders) > 0 {
		if result.CustomProviders == nil {
			result.CustomProviders = make(map[string]CustomProviderConfig)
		}
		for k, v := range override.CustomProviders {
			result.CustomProviders[k] = v
		}
	}

	// Override CommandHistoryByPath and HistoryIndexByPath
	if len(override.CommandHistoryByPath) > 0 {
		result.CommandHistoryByPath = override.CommandHistoryByPath
	}
	if len(override.HistoryIndexByPath) > 0 {
		result.HistoryIndexByPath = override.HistoryIndexByPath
	}

	// Override HistoryScope
	if override.HistoryScope != "" {
		result.HistoryScope = override.HistoryScope
	}

	// Override SelfReviewGateMode
	if override.SelfReviewGateMode != "" {
		result.SelfReviewGateMode = override.SelfReviewGateMode
	}

	// Override subagent settings
	if override.SubagentProvider != "" {
		result.SubagentProvider = override.SubagentProvider
	}
	if override.SubagentModel != "" {
		result.SubagentModel = override.SubagentModel
	}
	if override.SubagentMaxParallel > 0 {
		result.SubagentMaxParallel = override.SubagentMaxParallel
	}
	if override.SubagentParallelEnabled != nil {
		result.SubagentParallelEnabled = override.SubagentParallelEnabled
	}
	if override.SubagentMaxDepth > 0 {
		result.SubagentMaxDepth = override.SubagentMaxDepth
	}

	// Merge SubagentTypes
	if len(override.SubagentTypes) > 0 {
		if result.SubagentTypes == nil {
			result.SubagentTypes = make(map[string]SubagentType)
		}
		for k, v := range override.SubagentTypes {
			result.SubagentTypes[k] = v
		}
	}

	// Override commit provider/model
	if override.CommitProvider != "" {
		result.CommitProvider = override.CommitProvider
	}
	if override.CommitModel != "" {
		result.CommitModel = override.CommitModel
	}

	// Override review provider/model
	if override.ReviewProvider != "" {
		result.ReviewProvider = override.ReviewProvider
	}
	if override.ReviewModel != "" {
		result.ReviewModel = override.ReviewModel
	}

	// Override PDF OCR settings
	if override.PDFOCREnabled {
		result.PDFOCREnabled = override.PDFOCREnabled
	}
	if override.PDFOCRProvider != "" {
		result.PDFOCRProvider = override.PDFOCRProvider
	}
	if override.PDFOCRModel != "" {
		result.PDFOCRModel = override.PDFOCRModel
	}

	// Merge Skills
	if len(override.Skills) > 0 {
		if result.Skills == nil {
			result.Skills = make(map[string]Skill)
		}
		for k, v := range override.Skills {
			result.Skills[k] = v
		}
	}

	// Override zsh settings
	if override.EnableZshCommandDetection {
		result.EnableZshCommandDetection = override.EnableZshCommandDetection
	}
	if override.AutoExecuteDetectedCommands {
		result.AutoExecuteDetectedCommands = override.AutoExecuteDetectedCommands
	}

	return result
}

// cloneConfig creates a deep copy of a Config
func cloneConfig(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return &out
}

// Load loads the configuration from file
func Load() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, fmt.Errorf("get config path for default: %w", err)
	}

	// If config doesn't exist, return new default config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return NewConfig(), nil
	}

	// Migrate any api_key values from config.json custom_providers to the credential store
	// before the Config struct unmarshal (which would silently discard api_key fields).
	if err := MigrateConfigFileAPIKeys(configPath); err != nil {
		log.Printf("[config] warning: config.json api_key migration failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Run version-based migrations on the raw JSON before struct unmarshaling.
	var rawConfig map[string]interface{}
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config file for migration: %w", err)
	}
	rawConfig, err = MigrateConfig(rawConfig, ConfigVersion)
	if err != nil {
		log.Printf("[config] warning: config migration failed, using as-is: %v", err)
		// Continue with original data — don't block startup
	} else {
		data, err = json.Marshal(rawConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to re-marshal migrated config: %w", err)
		}
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Defensive nil-checks for map fields
	if config.ProviderModels == nil {
		config.ProviderModels = make(map[string]string)
	}
	if config.Preferences == nil {
		config.Preferences = make(map[string]interface{})
	}
	if config.MCP.Servers == nil {
		config.MCP.Servers = make(map[string]mcp.MCPServerConfig)
	}
	if config.DismissedPrompts == nil {
		config.DismissedPrompts = make(map[string]bool)
	}
	if config.CustomProviders == nil {
		config.CustomProviders = make(map[string]CustomProviderConfig)
	}
	if config.SubagentTypes == nil {
		config.SubagentTypes = make(map[string]SubagentType)
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	// Post-unmarshal operations that truly need struct-level access
	fileCustomProviders, err := MigrateLegacyCustomProviders(&config)
	if err != nil {
		return nil, fmt.Errorf("get config path: %w", err)
	}
	config.CustomProviders = fileCustomProviders

	if err := MigrateEmbeddedAPIKeys(config.CustomProviders); err != nil {
		log.Printf("[config] warning: credential migration failed: %v", err)
	}

	warnUnknownPersonaTools(config.SubagentTypes)

	// Discover project-specific skills from .sprout/skills/
	discoverProjectSkills(&config)

	return &config, nil
}

// Save saves the configuration to file
func (c *Config) Save() error {
	for name := range c.MCP.Servers {
		s := c.MCP.Servers[name]
		count, err := mcp.MigrateEnvSecretsFromServer(name, &s)
		if err != nil {
			log.Printf("[config] Warning: failed to migrate MCP secrets for server %s: %v", name, err)
		} else if count > 0 {
			c.MCP.Servers[name] = s
		}
	}

	configPath, err := GetConfigPath()
	if err != nil {
		return fmt.Errorf("get config path for save: %w", err)
	}

	c.Version = ConfigVersion
	persisted := *c
	persisted.Version = ConfigVersion
	persisted.CustomProviders = nil
	data, err := json.MarshalIndent(&persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// SaveToDir saves the configuration to a specific directory, bypassing
// GetConfigPath() (which reads the SPROUT_CONFIG/LEDIT_CONFIG env vars).
func (c *Config) SaveToDir(dir string) error {
	for name := range c.MCP.Servers {
		s := c.MCP.Servers[name]
		count, err := mcp.MigrateEnvSecretsFromServer(name, &s)
		if err != nil {
			log.Printf("[config] Warning: failed to migrate MCP secrets for server %s: %v", name, err)
		} else if count > 0 {
			c.MCP.Servers[name] = s
		}
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config directory %q: %w", dir, err)
	}

	configPath := filepath.Join(dir, ConfigFileName)
	c.Version = ConfigVersion
	persisted := *c
	persisted.Version = ConfigVersion
	persisted.CustomProviders = nil
	data, err := json.MarshalIndent(&persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
