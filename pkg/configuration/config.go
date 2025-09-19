package configuration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
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
	MCP MCPConfig `json:"mcp"`

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

	// Security Configuration
	EnableSecurityChecks bool `json:"enable_security_checks,omitempty"`

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

// MCPConfig represents the MCP configuration
type MCPConfig struct {
	Enabled      bool                       `json:"enabled"`
	Servers      map[string]MCPServerConfig `json:"servers"`
	AutoStart    bool                       `json:"auto_start"`
	AutoDiscover bool                       `json:"auto_discover"`
	Timeout      string                     `json:"timeout"` // Duration as string
}

// MCPServerConfig represents an individual MCP server configuration
type MCPServerConfig struct {
	Name        string            `json:"name"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	AutoStart   bool              `json:"auto_start"`
	MaxRestarts int               `json:"max_restarts"`
	Timeout     string            `json:"timeout"` // Duration as string
	Env         map[string]string `json:"env,omitempty"`
}

// APIKeys represents the API keys configuration
type APIKeys struct {
	OpenAI     string `json:"openai,omitempty"`
	DeepInfra  string `json:"deepinfra,omitempty"`
	OpenRouter string `json:"openrouter,omitempty"`
	DeepSeek   string `json:"deepseek,omitempty"`
	Gemini     string `json:"gemini,omitempty"`
	Anthropic  string `json:"anthropic,omitempty"`
	Groq       string `json:"groq,omitempty"`
	Cerebras   string `json:"cerebras,omitempty"`
}

// NewConfig creates a new configuration with sensible defaults
func NewConfig() *Config {
	return &Config{
		Version:          ConfigVersion,
		LastUsedProvider: "",
		ProviderModels: map[string]string{
			"openai":       "gpt-4o",
			"deepinfra":    "meta-llama/Llama-3.3-70B-Instruct",
			"openrouter":   "openai/gpt-4o",
			"ollama":       "qwen2.5-coder:latest",
			"ollama-local": "qwen2.5-coder:3b",
			"ollama-turbo": "qwen2.5-coder:latest",
		},
		ProviderPriority: []string{
			"openai",
			"openrouter",
			"deepinfra",
			"ollama-turbo",
			"ollama",
			"ollama-local",
		},
		MCP: MCPConfig{
			Enabled:      false,
			Servers:      make(map[string]MCPServerConfig),
			AutoStart:    true,
			AutoDiscover: true,
			Timeout:      "30s",
		},
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
		config.MCP.Servers = make(map[string]MCPServerConfig)
	}

	// Set version if not present
	if config.Version == "" {
		config.Version = ConfigVersion
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
	if c.MCP.Timeout == "" {
		return 30 * time.Second
	}

	duration, err := time.ParseDuration(c.MCP.Timeout)
	if err != nil {
		return 30 * time.Second
	}

	return duration
}

// GetMCPServerTimeout returns a server's timeout as a time.Duration
func (s *MCPServerConfig) GetTimeout() time.Duration {
	if s.Timeout == "" {
		return 30 * time.Second
	}

	duration, err := time.ParseDuration(s.Timeout)
	if err != nil {
		return 30 * time.Second
	}

	return duration
}
