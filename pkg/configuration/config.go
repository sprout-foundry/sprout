package configuration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alantheprice/ledit/pkg/agent_providers"
	"github.com/alantheprice/ledit/pkg/mcp"
)

const (
	ConfigVersion   = "2.0"
	ConfigDirName   = ".ledit"
	ConfigFileName  = "config.json"
	APIKeysFileName = "api_keys.json"
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

	// Code Style Configuration
	CodeStyle *CodeStyleConfig `json:"code_style,omitempty"`

	// Preferences
	Preferences map[string]interface{} `json:"preferences,omitempty"`

	// SkipPrompt - for non-interactive mode
	SkipPrompt bool `json:"skip_prompt,omitempty"`

	// Performance Configuration
	FileBatchSize         int `json:"file_batch_size,omitempty"`
	MaxConcurrentRequests int `json:"max_concurrent_requests,omitempty"`
	RequestDelayMs        int `json:"request_delay_ms,omitempty"`

	// API Timeout Configuration (in seconds)
	APITimeouts *APITimeoutConfig `json:"api_timeouts,omitempty"`

	// Security Configuration
	EnableSecurityChecks bool `json:"enable_security_checks,omitempty"`

	// Custom Providers Configuration
	CustomProviders map[string]CustomProviderConfig `json:"custom_providers,omitempty"`

	// Command History Configuration
	CommandHistory []string `json:"command_history,omitempty"`
	HistoryIndex   int      `json:"history_index,omitempty"`

	// Other flags
	FromAgent bool `json:"-"` // Internal flag, not persisted
}

// CodeStyleConfig represents code style preferences
type CodeStyleConfig struct {
	IndentationType          string `json:"indentation_type"`
	IndentationSize          int    `json:"indentation_size"`
	QuoteStyle               string `json:"quote_style"`
	LineEndings              string `json:"line_endings"`
	TrailingSemicolons       bool   `json:"trailing_semicolons"`
	TrailingCommas           bool   `json:"trailing_commas"`
	BracketSpacing           bool   `json:"bracket_spacing"`
	JavascriptStyle          string `json:"javascript_style"`
	OptionalChaining         bool   `json:"optional_chaining"`
	NullishCoalescing        bool   `json:"nullish_coalescing"`
	AsynchronousPatterns     string `json:"asynchronous_patterns"`
	TypeScriptStyle          string `json:"typescript_style"`
	ReactStyle               string `json:"react_style"`
	ComponentNaming          string `json:"component_naming"`
	StateManagement          string `json:"state_management"`
	PropTypeEnforcement      bool   `json:"prop_type_enforcement"`
	ImportStyle              string `json:"import_style"`
	ImportExtensions         bool   `json:"import_extensions"`
	AbsoluteImports          bool   `json:"absolute_imports"`
	ImportOrdering           string `json:"import_ordering"`
	CommentStyle             string `json:"comment_style"`
	DocstringFormat          string `json:"docstring_format"`
	InlineCommentSpacing     int    `json:"inline_comment_spacing"`
	FunctionStyle            string `json:"function_style"`
	ArrowFunctionParentheses string `json:"arrow_function_parentheses"`
	ReturnStatementStyle     string `json:"return_statement_style"`
	FunctionSize             string `json:"function_size"`
	FileSize                 string `json:"file_size"`
	NamingConventions        string `json:"naming_conventions"`
	ErrorHandling            string `json:"error_handling"`
	TestingApproach          string `json:"testing_approach"`
	Modularity               string `json:"modularity"`
}

// APITimeoutConfig represents timeout settings for API calls
type APITimeoutConfig struct {
	ConnectionTimeoutSec int `json:"connection_timeout_sec,omitempty"`  // Time to establish connection (default: 30)
	FirstChunkTimeoutSec int `json:"first_chunk_timeout_sec,omitempty"` // Time to receive first response (default: 60)
	ChunkTimeoutSec      int `json:"chunk_timeout_sec,omitempty"`       // Max time between streaming chunks (default: 90)
	OverallTimeoutSec    int `json:"overall_timeout_sec,omitempty"`     // Total request timeout (default: 600)
}

// MCPConfig moved to pkg/mcp package for consolidation
// Import from there: github.com/alantheprice/ledit/pkg/mcp

// MCPServerConfig moved to pkg/mcp package for consolidation
// Import from there: github.com/alantheprice/ledit/pkg/mcp

type APIKeys map[string]string

// CustomProviderConfig represents a custom model provider configuration
type CustomProviderConfig struct {
	Name           string                      `json:"name"`
	Endpoint       string                      `json:"endpoint"`
	ModelName      string                      `json:"model_name"`
	ContextSize    int                         `json:"context_size"`
	RequiresAPIKey bool                        `json:"requires_api_key"`
	APIKey         string                      `json:"api_key,omitempty"`            // Stored in config (not recommended for production)
	EnvVar         string                      `json:"env_var,omitempty"`            // Environment variable name for API key
	Conversion     providers.MessageConversion `json:"message_conversion,omitempty"` // Message conversion configuration
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
			"openai",
			"zai",
			"openrouter",
			"deepinfra",
			"ollama-turbo",

			"ollama-local",
		},
		MCP:                   mcp.DefaultMCPConfig(),
		Preferences:           make(map[string]interface{}),
		FileBatchSize:         10,
		MaxConcurrentRequests: 5,
		RequestDelayMs:        100,
		EnableSecurityChecks:  true,
		CodeStyle: &CodeStyleConfig{
			IndentationType: "spaces",
			IndentationSize: 4,
			QuoteStyle:      "double",
			LineEndings:     "unix",
			ImportStyle:     "grouped",
		},
		APITimeouts: &APITimeoutConfig{
			ConnectionTimeoutSec: 30,
			FirstChunkTimeoutSec: 60,
			ChunkTimeoutSec:      320, // Increased from 90 to 320 seconds (5 minutes) for complex tasks
			OverallTimeoutSec:    600, // 10 minutes
		},
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

// Load loads the configuration from file
func Load() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	// If config doesn't exist, return new default config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return NewConfig(), nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Ensure maps are initialized
	if config.ProviderModels == nil {
		config.ProviderModels = make(map[string]string)
	}
	if config.Preferences == nil {
		config.Preferences = make(map[string]interface{})
	}
	if config.MCP.Servers == nil {
		config.MCP.Servers = make(map[string]mcp.MCPServerConfig)
	}
	if config.CustomProviders == nil {
		config.CustomProviders = make(map[string]CustomProviderConfig)
	}

	// Set version if not present
	if config.Version == "" {
		config.Version = ConfigVersion
	}

	// Apply defaults for API timeouts if missing or zeroed
	if config.APITimeouts == nil {
		def := NewConfig().APITimeouts
		// Copy defaults to avoid sharing pointers
		config.APITimeouts = &APITimeoutConfig{
			ConnectionTimeoutSec: def.ConnectionTimeoutSec,
			FirstChunkTimeoutSec: def.FirstChunkTimeoutSec,
			ChunkTimeoutSec:      def.ChunkTimeoutSec,
			OverallTimeoutSec:    def.OverallTimeoutSec,
		}
	} else {
		def := NewConfig().APITimeouts
		if config.APITimeouts.ConnectionTimeoutSec == 0 {
			config.APITimeouts.ConnectionTimeoutSec = def.ConnectionTimeoutSec
		}
		if config.APITimeouts.FirstChunkTimeoutSec == 0 {
			config.APITimeouts.FirstChunkTimeoutSec = def.FirstChunkTimeoutSec
		}
		if config.APITimeouts.ChunkTimeoutSec == 0 {
			config.APITimeouts.ChunkTimeoutSec = def.ChunkTimeoutSec
		}
		if config.APITimeouts.OverallTimeoutSec == 0 {
			config.APITimeouts.OverallTimeoutSec = def.OverallTimeoutSec
		}
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

// SetModelForProvider sets the model for a specific provider
func (c *Config) SetModelForProvider(provider, model string) {
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

// GetMCPServerTimeout moved to pkg/mcp package with MCPServerConfig
