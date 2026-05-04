package configuration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

var personaDefaultsWarningOnce sync.Once

const (
	ConfigVersion   = "2.0"
	ConfigDirName   = ".sprout"
	ConfigFileName  = "config.json"
	APIKeysFileName = "api_keys.json"

	SelfReviewGateModeOff    = "off"
	SelfReviewGateModeCode   = "code"
	SelfReviewGateModeAlways = "always"
)

// Config represents the unified application configuration
type Config struct {
	Version string `json:"version"`

	// Provider and Model Configuration
	LastUsedProvider string            `json:"last_used_provider"`
	ProviderModels   map[string]string `json:"provider_models"`
	ProviderPriority []string          `json:"provider_priority"`

	// MCP Configuration
	MCP mcp.MCPConfig `json:"mcp"`

	// Preferences
	Preferences map[string]interface{} `json:"preferences,omitempty"`

	// AllowOrchestratorGitWrite controls whether the orchestrator persona is allowed to execute
	// writable git operations (commit, push, add, etc.) via shell_command.
	// When true (default), the orchestrator can use git write commands through shell_command
	// as an alternative to the dedicated git tool. Other personas are never allowed.
	// When false, ALL personas must use the git tool for write operations.
	AllowOrchestratorGitWrite bool `json:"allow_orchestrator_git_write,omitempty"`

	// ResourceDirectory stores captured web/vision resources relative to the current working directory.
	// This can be overridden at runtime with --resource-directory.
	ResourceDirectory string `json:"resource_directory,omitempty"`

	// ReasoningEffort sets a global default reasoning effort for chat requests.
	// Valid values: "low", "medium", "high". Empty means automatic selection.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// DisableThinking disables thinking/reasoning mode for thinking-capable models.
	// When true, models like Qwen3, Qwen3.5, GLM models, and Minimax models will
	// not use their thinking/reasoning mode. Note: GPT-OSS models do not support
	// disabling thinking (they use reasoning_effort instead).
	DisableThinking bool `json:"disable_thinking,omitempty"`

	// SystemPromptText overrides the main agent system prompt inline.
	// Empty means use the embedded default prompt.
	SystemPromptText string `json:"system_prompt_text,omitempty"`

	// SkipPrompt - for non-interactive mode
	SkipPrompt bool `json:"skip_prompt,omitempty"`

	// DismissedPrompts tracks which one-time prompts the user has dismissed.
	DismissedPrompts map[string]bool `json:"dismissed_prompts,omitempty"`

	// API Timeout Configuration (in seconds)
	APITimeouts *APITimeoutConfig `json:"api_timeouts,omitempty"`

	// Custom Providers Configuration
	CustomProviders map[string]CustomProviderConfig `json:"custom_providers,omitempty"`

	// Command History Configuration
	CommandHistoryByPath map[string][]string `json:"command_history_by_path,omitempty"`
	HistoryIndexByPath   map[string]int      `json:"history_index_by_path,omitempty"`

	// Change History Configuration
	HistoryScope string `json:"history_scope,omitempty"` // "project" or "global"

	// Self-Review Gate Configuration
	SelfReviewGateMode string `json:"self_review_gate_mode,omitempty"` // "off", "code", or "always"

	// Subagent Configuration
	SubagentProvider       string                  `json:"subagent_provider,omitempty"` // Provider for subagents (defaults to LastUsedProvider)
	SubagentModel          string                  `json:"subagent_model,omitempty"`    // Model for subagents (defaults to provider's default model)
	SubagentTypes          map[string]SubagentType `json:"subagent_types,omitempty"`    // Named subagent personas (coder, tester, etc.)
	SubagentMaxParallel    int                     `json:"subagent_max_parallel,omitempty"`     // Maximum number of parallel subagents (default: 2)
	SubagentParallelEnabled *bool                   `json:"subagent_parallel_enabled,omitempty"` // Enable/disable parallel subagent execution (default: true)

	// Commit Configuration
	CommitProvider string `json:"commit_provider,omitempty"` // Provider for commit message generation (defaults to LastUsedProvider)
	CommitModel    string `json:"commit_model,omitempty"`    // Model for commit message generation (defaults to provider's default model)

	// Review Configuration
	ReviewProvider string `json:"review_provider,omitempty"` // Provider for review commands (defaults to LastUsedProvider)
	ReviewModel    string `json:"review_model,omitempty"`    // Model for review commands (defaults to provider's default model)

	// PDF OCR Configuration
	PDFOCREnabled    bool   `json:"pdf_ocr_enabled,omitempty"`    // Enable PDF OCR processing
	PDFOCRProvider   string `json:"pdf_ocr_provider,omitempty"`   // Provider for PDF OCR (e.g., "ollama", "openai", "deepinfra")
	PDFOCRModel      string `json:"pdf_ocr_model,omitempty"`      // Model for PDF OCR (e.g., "glm-ocr", "llama3.2-vision")
	PDFOCRDownloaded bool   `json:"pdf_ocr_downloaded,omitempty"` // Whether the model has been downloaded

	// Embedding Index Configuration
	EmbeddingIndex *EmbeddingIndexConfig `json:"embedding_index,omitempty"`

	// Skills Configuration
	Skills map[string]Skill `json:"skills,omitempty"` // Agent Skills that can be loaded into context

	// Zsh Command Execution
	EnableZshCommandDetection   bool `json:"enable_zsh_command_detection,omitempty"`   // Enable zsh-aware command detection (default: false)
	AutoExecuteDetectedCommands bool `json:"auto_execute_detected_commands,omitempty"` // Auto-execute detected commands without prompting (default: true)

	// Other flags
	FromAgent bool `json:"-"` // Internal flag, not persisted
}

// APITimeoutConfig represents timeout settings for API calls
type APITimeoutConfig struct {
	ConnectionTimeoutSec int `json:"connection_timeout_sec,omitempty"`  // Time to establish connection (default: 300)
	FirstChunkTimeoutSec int `json:"first_chunk_timeout_sec,omitempty"` // Time to receive first response (default: 600)
	ChunkTimeoutSec      int `json:"chunk_timeout_sec,omitempty"`       // Max time between streaming chunks (default: 600)
	OverallTimeoutSec    int `json:"overall_timeout_sec,omitempty"`     // Total request timeout (default: 1800)
	CommitMessageTimeoutSec int `json:"commit_message_timeout_sec,omitempty"` // Timeout for commit message generation (default: 300)
}

// MCPConfig moved to pkg/mcp package for consolidation
// Import from there: github.com/sprout-foundry/sprout/pkg/mcp

// MCPServerConfig moved to pkg/mcp package for consolidation
// Import from there: github.com/sprout-foundry/sprout/pkg/mcp

type APIKeys map[string]string

// CustomProviderConfig represents a custom model provider configuration
type CustomProviderConfig struct {
	Name                   string                      `json:"name"`
	Endpoint               string                      `json:"endpoint"`
	ModelName              string                      `json:"model_name"`
	ContextSize            int                         `json:"context_size"`                  // Default context size for provider
	ModelContextSizes      map[string]int              `json:"model_context_sizes,omitempty"` // Per-model context sizes (e.g., "my-model": 131072)
	ReasoningEffort        string                      `json:"reasoning_effort,omitempty"`    // Optional provider-specific reasoning effort override
	Temperature            *float64                    `json:"temperature,omitempty"`         // Optional default temperature
	TopP                   *float64                    `json:"top_p,omitempty"`               // Optional default top_p
	Parameters             map[string]interface{}      `json:"parameters,omitempty"`          // Optional provider-specific default parameters
	RequiresAPIKey         bool                        `json:"requires_api_key"`
	ToolCalls              []string                    `json:"tool_calls,omitempty"`               // Optional explicit tool allowlist; when set, only these tools are exposed
	EnvVar                 string                      `json:"env_var,omitempty"`                  // Environment variable name for API key
	ChunkTimeoutMs         int                         `json:"chunk_timeout_ms,omitempty"`         // Streaming chunk timeout in milliseconds
	Conversion             providers.MessageConversion `json:"message_conversion,omitempty"`       // Message conversion configuration
	SupportsVision         bool                        `json:"supports_vision,omitempty"`          // Whether this provider supports vision requests
	VisionModel            string                      `json:"vision_model,omitempty"`             // Vision-capable model for this provider
	VisionFallbackProvider string                      `json:"vision_fallback_provider,omitempty"` // Optional fallback provider for vision
	VisionFallbackModel    string                      `json:"vision_fallback_model,omitempty"`    // Optional fallback model for vision provider
}

// SubagentType defines a specialized subagent persona with its own configuration
type SubagentType struct {
	ID               string   `json:"id"`                           // Unique identifier (e.g., "coder", "tester", "debugger")
	Name             string   `json:"name"`                         // Human-readable name (e.g., "Coder", "Tester")
	Description      string   `json:"description"`                  // What this subagent specializes in
	Provider         string   `json:"provider"`                     // Provider for this subagent type (optional, falls back to SubagentProvider)
	Model            string   `json:"model"`                        // Model for this subagent type (optional, falls back to SubagentModel)
	SystemPrompt     string   `json:"system_prompt"`                // Relative path to system prompt file (e.g., "subagent_prompts/coder.md")
	SystemPromptText string   `json:"system_prompt_text,omitempty"` // Optional inline system prompt text (replaces base prompt entirely)
	SystemPromptAppend string `json:"system_prompt_append,omitempty"` // Optional inline text appended to the base or loaded system prompt (for composition)
	AllowedTools     []string `json:"allowed_tools,omitempty"`      // Optional explicit tool allowlist for focused persona behavior
	Aliases          []string `json:"aliases,omitempty"`            // Optional aliases (e.g., "web-scraper")
	Enabled          bool     `json:"enabled"`                      // Whether this subagent type is available for use
}

// Skill defines an Agent Skill that can be loaded into context
type Skill struct {
	ID           string            `json:"id"`            // Unique identifier (e.g., "go-best-practices")
	Name         string            `json:"name"`          // Human-readable name
	Description  string            `json:"description"`   // What this skill provides and when to use it
	Path         string            `json:"path"`          // Relative path to skill directory
	Enabled      bool              `json:"enabled"`       // Whether this skill is available
	Metadata     map[string]string `json:"metadata"`      // Optional metadata (author, version, etc.)
	AllowedTools string            `json:"allowed_tools"` // Optional space-delimited list of pre-approved tools
}

// EmbeddingIndexConfig configures the embedding-based duplicate detection and semantic search.
type EmbeddingIndexConfig struct {
	// Enabled controls whether the embedding index is active.
	Enabled bool `json:"enabled,omitempty"`

	// Provider is the embedding provider to use. Currently only "bundled" is supported.
	// Default: "bundled"
	Provider string `json:"provider,omitempty"`

	// ORTLibraryPath is the path to the ONNX Runtime shared library.
	// If empty, the system will try to find it automatically.
	ORTLibraryPath string `json:"ort_library_path,omitempty"`

	// ModelDir is the directory containing the ONNX model and tokenizer files.
	// If empty, uses the bundled models in the binary.
	ModelDir string `json:"model_dir,omitempty"`

	// IndexDir is the directory where the embedding index JSONL files are stored.
	// If empty, uses ~/.config/sprout/embeddings/
	IndexDir string `json:"index_dir,omitempty"`

	// SimilarityThreshold is the cosine similarity threshold for duplicate detection.
	// Range: 0.0 to 1.0. Default: 0.90
	SimilarityThreshold float32 `json:"similarity_threshold,omitempty"`

	// MaxResults is the maximum number of duplicate candidates to return.
	// Default: 3
	MaxResults int `json:"max_results,omitempty"`

	// AutoIndex controls whether the index is built automatically on first use.
	// Default: true
	AutoIndex bool `json:"auto_index,omitempty"`

	// ExcludePaths is a list of additional paths to exclude from indexing.
	ExcludePaths []string `json:"exclude_paths,omitempty"`
}

// Optional helpers
func (a APIKeys) Get(provider string) string {
	return a[provider]
}

func (a *APIKeys) Set(provider, key string) {
	if *a == nil {
		*a = make(map[string]string)
	}
	(*a)[provider] = key
}

// NewConfig creates a new configuration with sensible defaults
func NewConfig() *Config {
	return &Config{
		Version:          ConfigVersion,
		LastUsedProvider: "",
		ProviderModels: map[string]string{
			"openai":       "gpt-5-mini",
			"zai":          "GLM-4.6",
			"deepinfra":    "deepseek-ai/DeepSeek-V3.1-Terminus",
			"openrouter":   "openai/gpt-5",
			"ollama-local": "qwen3-coder:30b",
			"ollama-turbo": "deepseek-v3.1:671b",
		},
		ProviderPriority: []string{
			"openrouter",
			"zai",
			"deepinfra",
			"ollama-turbo",
			"ollama-local",
			"openai",
		},
		CustomProviders:      make(map[string]CustomProviderConfig),
		CommandHistoryByPath: make(map[string][]string),
		HistoryIndexByPath:   make(map[string]int),
		MCP:                  mcp.DefaultMCPConfig(),
		Preferences:          make(map[string]interface{}),
		APITimeouts: &APITimeoutConfig{
			ConnectionTimeoutSec: 300,
			FirstChunkTimeoutSec: 600,
			ChunkTimeoutSec:      600,
			OverallTimeoutSec:    1800,
			CommitMessageTimeoutSec: 300, // 5 minutes for commit message generation
		},
		HistoryScope:                "project", // Default to project-scoped history
		SelfReviewGateMode:          SelfReviewGateModeOff,
		EnableZshCommandDetection:   true, // Enable zsh command detection by default
		AutoExecuteDetectedCommands: true, // Auto-execute detected commands without prompting
		SubagentTypes:               defaultSubagentTypes(),
		Skills:                      defaultSkills(),
		PDFOCREnabled:               true,
		PDFOCRProvider:              "ollama",
		PDFOCRModel:                 "glm-ocr",
		SubagentMaxParallel:         2,    // Default max parallel subagents
		SubagentParallelEnabled:     func() *bool { t := true; return &t }(), // Default to enabling parallel subagents
	}
}

// GetConfigDir returns the configuration directory path
func GetConfigDir() (string, error) {
	return envutil.GetConfigDir()
}

func getDefaultConfigDir() (string, error) {
	xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "sprout"), nil
	}

	homeEnv := strings.TrimSpace(os.Getenv("HOME"))
	if homeEnv != "" {
		return filepath.Join(homeEnv, ".config", "sprout"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "sprout"), nil
}

// GetConfigPath returns the full path to the config file
func GetConfigPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	return filepath.Join(configDir, ConfigFileName), nil
}

// GetWorkspaceConfigPath returns the path to workspace-level config
func GetWorkspaceConfigPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ConfigDirName, ConfigFileName)
}

// IsWorkspaceConfigPresent checks if a workspace config file exists
func IsWorkspaceConfigPresent(workspaceRoot string) bool {
	_, err := os.Stat(GetWorkspaceConfigPath(workspaceRoot))
	return err == nil
}

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

	// Defensive nil-checks for map fields (migration ensures these exist in raw JSON,
	// but these checks provide Go-level safety for edge cases).
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
	// Migrate any plaintext secrets in MCP server env blocks to the
	// credential store before persisting. This is defense-in-depth: most
	// callers already migrate before reaching here, but this ensures the
	// main config file never contains raw token values regardless.
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

	// Write with explicit 0600 permissions (owner read/write only)
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// SaveToDir saves the configuration to a specific directory, bypassing
// GetConfigPath() (which reads the SPROUT_CONFIG/LEDIT_CONFIG env vars).
// Use this when a Manager has an explicit configDir so that saves go to
// the correct location even after the env var has been restored.
func (c *Config) SaveToDir(dir string) error {
	// Migrate any plaintext secrets in MCP server env blocks to the
	// credential store before persisting.
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

// GetModelForProvider returns the configured model for a provider
func (c *Config) GetModelForProvider(provider string) string {
	if model, exists := c.ProviderModels[provider]; exists && model != "" {
		return model
	}

	// Return default from NewConfig if not set
	defaults := NewConfig()
	if defaultModel, exists := defaults.ProviderModels[provider]; exists {
		return defaultModel
	}

	return ""
}

// NormalizeSelfReviewGateMode validates and normalizes self-review gate mode.
func NormalizeSelfReviewGateMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SelfReviewGateModeOff:
		return SelfReviewGateModeOff, true
	case SelfReviewGateModeCode:
		return SelfReviewGateModeCode, true
	case SelfReviewGateModeAlways:
		return SelfReviewGateModeAlways, true
	default:
		return "", false
	}
}

// GetSelfReviewGateMode returns the effective self-review gate mode.
func (c *Config) GetSelfReviewGateMode() string {
	mode, ok := NormalizeSelfReviewGateMode(c.SelfReviewGateMode)
	if !ok {
		return SelfReviewGateModeOff
	}
	return mode
}

// SetSelfReviewGateMode sets the self-review gate mode.
func (c *Config) SetSelfReviewGateMode(mode string) error {
	normalized, ok := NormalizeSelfReviewGateMode(mode)
	if !ok {
		return fmt.Errorf("invalid self-review gate mode %q (allowed: off, code, always)", mode)
	}
	c.SelfReviewGateMode = normalized
	return nil
}

// SetModelForProvider sets the model for a specific provider.
// The test provider is silently rejected to prevent it from leaking
// into the persisted config via direct Config access.
func (c *Config) SetModelForProvider(provider, model string) {
	// Defense-in-depth: reject test provider at the Config level so that
	// even code that bypasses the Manager guard cannot persist it.
	if provider == "test" {
		return
	}
	if c.ProviderModels == nil {
		c.ProviderModels = make(map[string]string)
	}
	c.ProviderModels[provider] = model
	c.LastUsedProvider = provider
}

// GetMCPTimeout returns the MCP timeout as a time.Duration
func (c *Config) GetMCPTimeout() time.Duration {
	if c.MCP.Timeout == 0 {
		return 30 * time.Second
	}
	return c.MCP.Timeout
}

// GetSubagentProvider returns the configured provider for subagents
// If not explicitly set, falls back to the last used provider
func (c *Config) GetSubagentProvider() string {
	if c.SubagentProvider != "" {
		return c.SubagentProvider
	}
	// Fall back to last used provider
	if c.LastUsedProvider != "" {
		return c.LastUsedProvider
	}
	// Fall back to first priority provider
	if len(c.ProviderPriority) > 0 {
		return c.ProviderPriority[0]
	}
	return "ollama-local" // Ultimate fallback
}

// GetSubagentModel returns the configured model for subagents
// If not explicitly set, falls back to the provider's default model
func (c *Config) GetSubagentModel() string {
	if c.SubagentModel != "" {
		return c.SubagentModel
	}
	// Use the provider for subagents
	provider := c.GetSubagentProvider()
	return c.GetModelForProvider(provider)
}

// SetSubagentProvider sets the provider for subagents
func (c *Config) SetSubagentProvider(provider string) {
	c.SubagentProvider = provider
}

// SetSubagentModel sets the model for subagents
func (c *Config) SetSubagentModel(model string) {
	c.SubagentModel = model
}

// GetCommitProvider returns the configured provider for commit message generation
// If not explicitly set, falls back to the last used provider
func (c *Config) GetCommitProvider() string {
	if c.CommitProvider != "" {
		return c.CommitProvider
	}
	// Fall back to last used provider
	if c.LastUsedProvider != "" {
		return c.LastUsedProvider
	}
	// Fall back to first priority provider
	if len(c.ProviderPriority) > 0 {
		return c.ProviderPriority[0]
	}
	return "ollama-local" // Ultimate fallback
}

// GetCommitModel returns the configured model for commit message generation
// If not explicitly set, falls back to the provider's default model
func (c *Config) GetCommitModel() string {
	if c.CommitModel != "" {
		return c.CommitModel
	}
	// Use the provider for commits
	provider := c.GetCommitProvider()
	return c.GetModelForProvider(provider)
}

// SetCommitProvider sets the provider for commit message generation
func (c *Config) SetCommitProvider(provider string) {
	c.CommitProvider = provider
}

// SetCommitModel sets the model for commit message generation
func (c *Config) SetCommitModel(model string) {
	c.CommitModel = model
}

// GetReviewProvider returns the configured provider for review commands
// If not explicitly set, falls back to the last used provider
func (c *Config) GetReviewProvider() string {
	if c.ReviewProvider != "" {
		return c.ReviewProvider
	}
	// Fall back to last used provider
	if c.LastUsedProvider != "" {
		return c.LastUsedProvider
	}
	// Fall back to first priority provider
	if len(c.ProviderPriority) > 0 {
		return c.ProviderPriority[0]
	}
	return "ollama-local" // Ultimate fallback
}

// GetReviewModel returns the configured model for review commands
// If not explicitly set, falls back to the provider's default model
func (c *Config) GetReviewModel() string {
	if c.ReviewModel != "" {
		return c.ReviewModel
	}
	// Use the provider for reviews
	provider := c.GetReviewProvider()
	return c.GetModelForProvider(provider)
}

// SetReviewProvider sets the provider for review commands
func (c *Config) SetReviewProvider(provider string) {
	c.ReviewProvider = provider
}

// SetReviewModel sets the model for review commands
func (c *Config) SetReviewModel(model string) {
	c.ReviewModel = model
}

// GetSubagentType retrieves a subagent type configuration by ID
// Returns nil if the subagent type doesn't exist or is disabled
func (c *Config) GetSubagentType(id string) *SubagentType {
	if c.SubagentTypes == nil {
		return nil
	}

	normalizedID := normalizePersonaID(id)

	// Find user override if any and determine the primary ID
	var userOverride SubagentType
	userOverrideFound := false
	var primaryID string
	for personaID, subagentType := range c.SubagentTypes {
		normalizedPersonaID := normalizePersonaID(personaID)
		normalizedSubagentTypeID := normalizePersonaID(subagentType.ID)
		if normalizedPersonaID == normalizedID || normalizedSubagentTypeID == normalizedID {
			userOverride = subagentType
			userOverrideFound = true
			primaryID = normalizedSubagentTypeID // Use the actual persona ID
			break
		}
		for _, alias := range subagentType.Aliases {
			if normalizePersonaID(alias) == normalizedID {
				userOverride = subagentType
				userOverrideFound = true
				primaryID = normalizedSubagentTypeID // This is the primary ID
				break
			}
		}
		if userOverrideFound {
			break
		}
	}

	// Warn if multiple config entries could match the same normalized ID.
	// This can happen if two map keys map to the same persona ID, which would
	// produce non-deterministic results since Go map iteration is unordered.
	if primaryID != "" && primaryID != normalizedID {
		// The match was via ID field, not map key — check if the map key also exists
		for k := range c.SubagentTypes {
			if normalizePersonaID(k) == normalizedID && normalizePersonaID(k) != primaryID {
				log.Printf("[config] WARNING: multiple subagent config entries match %q — behavior is non-deterministic due to map iteration order", normalizedID)
				break
			}
		}
	}

	// Get the default persona definition using the primary ID
	defaultPersonas := defaultSubagentTypes()
	var defaultPersona SubagentType
	defaultExists := false
	if primaryID != "" {
		defaultPersona, defaultExists = defaultPersonas[primaryID]
	} else {
		defaultPersona, defaultExists = defaultPersonas[normalizedID]
	}

	// If no default exists and no user override, persona doesn't exist
	if !defaultExists && !userOverrideFound {
		return nil
	}

	// Custom persona: only exists in user config, not in defaults
	if !defaultExists && userOverrideFound {
		if !userOverride.Enabled {
			return nil
		}
		// Deep copy slices to avoid sharing backing arrays with the config map
		result := userOverride
		result.AllowedTools = append([]string{}, userOverride.AllowedTools...)
		result.Aliases = append([]string{}, userOverride.Aliases...)
		return &result
	}

	// Default persona with user override: check if user disabled it
	if defaultExists {
		// If user has disabled the persona, return nil
		if userOverrideFound && !userOverride.Enabled {
			return nil
		}

		// Make a deep copy to avoid modifying the original default
		result := SubagentType{
			ID:                   defaultPersona.ID,
			Name:                 defaultPersona.Name,
			Description:          defaultPersona.Description,
			Provider:             defaultPersona.Provider,
			Model:                defaultPersona.Model,
			SystemPrompt:         defaultPersona.SystemPrompt,
			SystemPromptText:     defaultPersona.SystemPromptText,
			SystemPromptAppend:   defaultPersona.SystemPromptAppend,
			AllowedTools:         make([]string, len(defaultPersona.AllowedTools)),
			Aliases:              make([]string, len(defaultPersona.Aliases)),
			Enabled:              defaultPersona.Enabled,
		}
		// Copy slices
		copy(result.AllowedTools, defaultPersona.AllowedTools)
		copy(result.Aliases, defaultPersona.Aliases)
		
		// If user has override, overlay only the user-overridable fields
		if userOverrideFound {
			// Provider and model can be overridden
			if userOverride.Provider != "" {
				result.Provider = userOverride.Provider
			}
			if userOverride.Model != "" {
				result.Model = userOverride.Model
			}
			if userOverride.SystemPromptAppend != "" {
				result.SystemPromptAppend = userOverride.SystemPromptAppend
			}
		}

		if result.Enabled {
			return &result
		}
		return nil
	}
	
	return nil // This should never be reached, but needed for compilation
}

func mergeMissingDefaultSubagentTypes(config *Config) {
	if config == nil {
		return
	}
	if config.SubagentTypes == nil {
		config.SubagentTypes = make(map[string]SubagentType)
	}

	for id, persona := range defaultSubagentTypes() {
		if _, exists := config.SubagentTypes[id]; !exists {
			config.SubagentTypes[id] = persona
		}
	}
}

func mergeLegacyStructuredToolsIntoPersonaAllowlists(config *Config) {
	if config == nil || config.SubagentTypes == nil {
		return
	}

	defaults := defaultSubagentTypes()
	for id, persona := range config.SubagentTypes {
		normalizedID := normalizePersonaID(id)
		if _, exists := defaults[normalizedID]; !exists {
			continue
		}
		if len(persona.AllowedTools) == 0 {
			continue
		}
		if !hasAnyTool(persona.AllowedTools, "write_file", "edit_file") {
			continue
		}

		changed := false
		if !hasTool(persona.AllowedTools, "write_structured_file") {
			persona.AllowedTools = append(persona.AllowedTools, "write_structured_file")
			changed = true
		}
		if !hasTool(persona.AllowedTools, "patch_structured_file") {
			persona.AllowedTools = append(persona.AllowedTools, "patch_structured_file")
			changed = true
		}

		if changed {
			config.SubagentTypes[id] = persona
		}
	}

	for id, persona := range config.SubagentTypes {
		normalizedID := normalizePersonaID(id)
		if normalizedID != "web_scraper" {
			continue
		}
		if len(persona.AllowedTools) == 0 {
			continue
		}
		if hasTool(persona.AllowedTools, "shell_command") {
			continue
		}
		persona.AllowedTools = append(persona.AllowedTools, "shell_command")
		config.SubagentTypes[id] = persona
	}
}

func hasAnyTool(tools []string, candidates ...string) bool {
	for _, candidate := range candidates {
		if hasTool(tools, candidate) {
			return true
		}
	}
	return false
}

func hasTool(tools []string, candidate string) bool {
	for _, tool := range tools {
		if strings.TrimSpace(tool) == candidate {
			return true
		}
	}
	return false
}

func defaultSubagentTypes() map[string]SubagentType {
	definitions, err := personas.DefaultDefinitions()
	if err != nil {
		personaDefaultsWarningOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "WARNING: failed to load embedded persona definitions, using fallback defaults: %v\n", err)
		})
	}

	types := make(map[string]SubagentType, len(definitions))
	for id, definition := range definitions {
		types[normalizePersonaID(id)] = SubagentType{
			ID:               normalizePersonaID(definition.ID),
			Name:             definition.Name,
			Description:      definition.Description,
			Provider:         definition.Provider,
			Model:            definition.Model,
			SystemPrompt:     definition.SystemPrompt,
			SystemPromptText: definition.SystemPromptText,
			SystemPromptAppend: definition.SystemPromptAppend,
			AllowedTools:     append([]string{}, definition.AllowedTools...),
			Aliases:          append([]string{}, definition.Aliases...),
			Enabled:          definition.Enabled,
		}
	}

	return types
}

func defaultSkills() map[string]Skill {
	return map[string]Skill{
		"go-conventions": {
			ID:          "go-conventions",
			Name:        "Go Conventions",
			Description: "Go coding conventions, best practices, and style guidelines. Use when writing or reviewing Go code.",
			Path:        "pkg/agent/skills/go-conventions",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"test-writing": {
			ID:          "test-writing",
			Name:        "Test Writing",
			Description: "Guidelines for writing effective unit tests, integration tests, and test coverage. Use when creating tests.",
			Path:        "pkg/agent/skills/test-writing",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"commit-msg": {
			ID:          "commit-msg",
			Name:        "Commit Message",
			Description: "Conventional commits format and best practices for writing clear commit messages.",
			Path:        "pkg/agent/skills/commit-msg",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"repo-onboarding": {
			ID:          "repo-onboarding",
			Name:        "Project Planning",
			Description: "Strategic planning and alignment for new (greenfield) or existing (brownfield) projects. Use when starting a new project, onboarding to an unfamiliar repo, or aligning an existing codebase to a standardized workflow.",
			Path:        "pkg/agent/skills/project-planning",
			Enabled:     true,
			Metadata:    map[string]string{"version": "2.0"},
		},
		"bug-triage": {
			ID:          "bug-triage",
			Name:        "Bug Triage",
			Description: "Repro-first debugging workflow with root-cause validation and minimal-risk fix planning.",
			Path:        "pkg/agent/skills/bug-triage",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"safe-refactor": {
			ID:          "safe-refactor",
			Name:        "Safe Refactor",
			Description: "Behavior-preserving refactor workflow focused on small steps, verification gates, and low regression risk.",
			Path:        "pkg/agent/skills/safe-refactor",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"test-author": {
			ID:          "test-author",
			Name:        "Test Author",
			Description: "Process for adding targeted tests, edge cases, and regressions for changed behavior.",
			Path:        "pkg/agent/skills/test-author",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"release-preflight": {
			ID:          "release-preflight",
			Name:        "Release Preflight",
			Description: "Pre-release checklist for build, test, and risk validation with clear go/no-go output.",
			Path:        "pkg/agent/skills/release-preflight",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"docs-sync": {
			ID:          "docs-sync",
			Name:        "Docs Sync",
			Description: "Process to keep docs aligned with shipped behavior and command surface.",
			Path:        "pkg/agent/skills/docs-sync",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"review-workflow": {
			ID:          "review-workflow",
			Name:        "Review Workflow",
			Description: "Evidence-first review process for triaging findings, reducing false positives, and prioritizing must-fix risks.",
			Path:        "pkg/agent/skills/review-workflow",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"python-conventions": {
			ID:          "python-conventions",
			Name:        "Python Conventions",
			Description: "Python 3.11+ coding conventions, best practices, and style guidelines. Use when writing or reviewing Python code.",
			Path:        "pkg/agent/skills/python-conventions",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"typescript-conventions": {
			ID:          "typescript-conventions",
			Name:        "TypeScript Conventions",
			Description: "TypeScript 5.x and JavaScript ES2022+ coding conventions, best practices, and style guidelines. Use when writing or reviewing TypeScript/JavaScript code.",
			Path:        "pkg/agent/skills/typescript-conventions",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"rust-conventions": {
			ID:          "rust-conventions",
			Name:        "Rust Conventions",
			Description: "Rust 2021 edition coding conventions, best practices, and style guidelines. Use when writing or reviewing Rust code.",
			Path:        "pkg/agent/skills/rust-conventions",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"project-planning": {
			ID:          "project-planning",
			Name:        "Project Planning",
			Description: "Structured planning and project initialization workflow. Use when starting a new project, setting up a new codebase, or creating a project plan.",
			Path:        "pkg/agent/skills/project-planning",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
	}
}

func mergeMissingDefaultSkills(config *Config) {
	if config == nil {
		return
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	for id, skill := range defaultSkills() {
		if _, exists := config.Skills[id]; !exists {
			config.Skills[id] = skill
		}
	}
}

// discoverProjectSkills scans the .sprout/skills/ directory for project-specific skills
// and adds them to the config. This allows users to create custom skills without
// modifying the global config.
func discoverProjectSkills(config *Config) {
	if config == nil {
		return
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	// Check for .sprout/skills directory
	skillsDir := filepath.Join(cwd, ".sprout", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return // No project skills directory, that's fine
	}

	// Scan for skill directories
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillID := entry.Name()
		skillFile := filepath.Join(skillsDir, skillID, "SKILL.md")

		// Check if SKILL.md exists
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}

		// Read skill file to extract metadata
		content, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}

		// Parse front matter
		name, description := parseSkillFrontMatter(string(content))
		if name == "" {
			name = skillID
		}
		if description == "" {
			description = fmt.Sprintf("Project-specific skill: %s", skillID)
		}

		// Add to config (don't override if already exists)
		if _, exists := config.Skills[skillID]; !exists {
			config.Skills[skillID] = Skill{
				ID:          skillID,
				Name:        name,
				Description: description,
				Path:        filepath.Join(".sprout", "skills", skillID),
				Enabled:     true,
				Metadata:    map[string]string{"source": "project"},
			}
		}
	}
}

// parseSkillFrontMatter extracts name and description from SKILL.md front matter
func parseSkillFrontMatter(content string) (name, description string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inFrontMatter := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			inFrontMatter = !inFrontMatter
			continue
		}
		if inFrontMatter {
			if strings.HasPrefix(line, "name:") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			} else if strings.HasPrefix(line, "description:") {
				description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			}
		}
	}
	return name, description
}

func normalizePersonaID(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

// GetSubagentTypeProvider returns the provider for a specific subagent type
// Falls back to the general subagent provider if not specified
func (c *Config) GetSubagentTypeProvider(id string) string {
	if st := c.GetSubagentType(id); st != nil && st.Provider != "" {
		return st.Provider
	}
	return c.GetSubagentProvider()
}

// GetSubagentTypeModel returns the model for a specific subagent type
// Falls back to the general subagent model if not specified
func (c *Config) GetSubagentTypeModel(id string) string {
	if st := c.GetSubagentType(id); st != nil && st.Model != "" {
		return st.Model
	}
	return c.GetSubagentModel()
}

// GetSkill retrieves a skill configuration by ID
// Returns nil if the skill doesn't exist or is disabled
func (c *Config) GetSkill(id string) *Skill {
	if c.Skills == nil {
		return nil
	}
	if skill, exists := c.Skills[id]; exists && skill.Enabled {
		return &skill
	}
	return nil
}

// GetSkillPath returns the full path to a skill directory
func (c *Config) GetSkillPath(id string) string {
	skill := c.GetSkill(id)
	if skill == nil || skill.Path == "" {
		return ""
	}
	// Skill path is relative to sprout source root
	return skill.Path
}

// GetAllEnabledSkills returns all enabled skills
func (c *Config) GetAllEnabledSkills() map[string]Skill {
	if c.Skills == nil {
		return nil
	}
	result := make(map[string]Skill)
	for id, skill := range c.Skills {
		if skill.Enabled {
			result[id] = skill
		}
	}
	return result
}

// GetMCPServerTimeout moved to pkg/mcp package with MCPServerConfig

// Validate validates the configuration and returns any errors
func (c *Config) Validate() error {
	if _, ok := NormalizeSelfReviewGateMode(c.SelfReviewGateMode); !ok {
		return fmt.Errorf("invalid self_review_gate_mode: %q (allowed: off, code, always)", c.SelfReviewGateMode)
	}

	// Validate PDF OCR configuration
	if c.PDFOCREnabled {
		if c.PDFOCRProvider == "" {
			return fmt.Errorf("PDF OCR provider cannot be empty when PDF OCR is enabled")
		}
		if c.PDFOCRModel == "" {
			return fmt.Errorf("PDF OCR model cannot be empty when PDF OCR is enabled")
		}
	}

	return nil
}

// GetSubagentMaxParallel returns the maximum number of parallel subagents
// Defaults to 2 if not configured or set to 0
func (c *Config) GetSubagentMaxParallel() int {
	if c.SubagentMaxParallel > 0 {
		return c.SubagentMaxParallel
	}
	return 2 // Default
}

// GetSubagentParallelEnabled returns whether parallel subagent execution is enabled
// Defaults to true if not explicitly set (nil pointer)
func (c *Config) GetSubagentParallelEnabled() bool {
	if c.SubagentParallelEnabled == nil {
		return true // default when not configured
	}
	return *c.SubagentParallelEnabled
}
