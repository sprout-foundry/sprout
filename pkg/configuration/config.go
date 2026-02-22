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

// SecurityValidationConfig configures LLM-based security validation
type SecurityValidationConfig struct {
	// Enabled turns on LLM-based security validation
	Enabled bool `json:"enabled,omitempty"`

	// Model is the path to the GGUF model file for security validation
	// Example: ~/.ledit/models/qwen2.5-coder-0.5b-q4_k_m.gguf
	// Download from: https://huggingface.co/models?search=gguf+qwen+2.5+coder
	Model string `json:"model,omitempty"`

	// Threshold controls sensitivity (0=allow_all, 1=cautious, 2=strict)
	// 0: Allow all operations (validation disabled but still logs)
	// 1: Allow safe operations, ask user for cautious ones
	// 2: Block dangerous operations, require explicit approval
	Threshold int `json:"threshold,omitempty"`

	// TimeoutSeconds is max time to wait for security validation
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

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

	// Security Validation Configuration
	SecurityValidation *SecurityValidationConfig `json:"security_validation,omitempty"`

	// Custom Providers Configuration
	CustomProviders map[string]CustomProviderConfig `json:"custom_providers,omitempty"`

	// Command History Configuration
	CommandHistory []string `json:"command_history,omitempty"`
	HistoryIndex   int      `json:"history_index,omitempty"`

	// Change History Configuration
	HistoryScope string `json:"history_scope,omitempty"` // "project" or "global"

	// Subagent Configuration
	SubagentProvider string                  `json:"subagent_provider,omitempty"` // Provider for subagents (defaults to LastUsedProvider)
	SubagentModel    string                  `json:"subagent_model,omitempty"`    // Model for subagents (defaults to provider's default model)
	SubagentTypes    map[string]SubagentType `json:"subagent_types,omitempty"`    // Named subagent personas (coder, tester, etc.)

	// Skills Configuration
	Skills map[string]Skill `json:"skills,omitempty"` // Agent Skills that can be loaded into context

	// Zsh Command Execution
	EnableZshCommandDetection   bool `json:"enable_zsh_command_detection,omitempty"`   // Enable zsh-aware command detection (default: false)
	AutoExecuteDetectedCommands bool `json:"auto_execute_detected_commands,omitempty"` // Auto-execute detected commands without prompting (default: true)

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
	ConnectionTimeoutSec int `json:"connection_timeout_sec,omitempty"`  // Time to establish connection (default: 300)
	FirstChunkTimeoutSec int `json:"first_chunk_timeout_sec,omitempty"` // Time to receive first response (default: 300)
	ChunkTimeoutSec      int `json:"chunk_timeout_sec,omitempty"`       // Max time between streaming chunks (default: 300)
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
	ChunkTimeoutMs int                         `json:"chunk_timeout_ms,omitempty"`   // Streaming chunk timeout in milliseconds
	Conversion     providers.MessageConversion `json:"message_conversion,omitempty"` // Message conversion configuration
}

// SubagentType defines a specialized subagent persona with its own configuration
type SubagentType struct {
	ID           string `json:"id"`            // Unique identifier (e.g., "coder", "tester", "debugger")
	Name         string `json:"name"`          // Human-readable name (e.g., "Coder", "Tester")
	Description  string `json:"description"`   // What this subagent specializes in
	Provider     string `json:"provider"`      // Provider for this subagent type (optional, falls back to SubagentProvider)
	Model        string `json:"model"`         // Model for this subagent type (optional, falls back to SubagentModel)
	SystemPrompt string `json:"system_prompt"` // Relative path to system prompt file (e.g., "subagent_prompts/coder.md")
	Enabled      bool   `json:"enabled"`       // Whether this subagent type is available for use
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
		CustomProviders:       make(map[string]CustomProviderConfig),
		MCP:                   mcp.DefaultMCPConfig(),
		Preferences:           make(map[string]interface{}),
		FileBatchSize:         10,
		MaxConcurrentRequests: 5,
		RequestDelayMs:        100,
		EnableSecurityChecks:  true,
		SecurityValidation: &SecurityValidationConfig{
			Enabled:        true, // Enabled by default (uses Ollama fallback if llama.cpp unavailable)
			Model:          "",   // Empty = use default Ollama model (qwen2.5-coder:1.5b)
			Threshold:      1,    // Cautious by default
			TimeoutSeconds: 10,   // 10 second timeout
		},
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
		HistoryScope:                "project", // Default to project-scoped history
		EnableZshCommandDetection:   true,      // Enable zsh command detection by default
		AutoExecuteDetectedCommands: true,      // Auto-execute detected commands without prompting
		SubagentTypes: map[string]SubagentType{
			"general": {
				ID:           "general",
				Name:         "General",
				Description:  "General-purpose subagent for tasks that don't require specialized expertise",
				SystemPrompt: "pkg/agent/prompts/subagent_prompts/general.md",
				Enabled:      true,
			},
			"coder": {
				ID:           "coder",
				Name:         "Coder",
				Description:  "Implementation and feature development specialist",
				SystemPrompt: "pkg/agent/prompts/subagent_prompts/coder.md",
				Enabled:      true,
			},
			"tester": {
				ID:           "tester",
				Name:         "Tester",
				Description:  "Unit test writing and test coverage specialist",
				SystemPrompt: "pkg/agent/prompts/subagent_prompts/tester.md",
				Enabled:      true,
			},
			"qa_engineer": {
				ID:           "qa_engineer",
				Name:         "QA Engineer",
				Description:  "Quality assurance, test planning, and integration testing specialist",
				SystemPrompt: "pkg/agent/prompts/subagent_prompts/qa_engineer.md",
				Enabled:      true,
			},
			"code_reviewer": {
				ID:           "code_reviewer",
				Name:         "Code Reviewer",
				Description:  "Code review, security, and best practices specialist",
				SystemPrompt: "pkg/agent/prompts/subagent_prompts/code_reviewer.md",
				Enabled:      true,
			},
			"debugger": {
				ID:           "debugger",
				Name:         "Debugger",
				Description:  "Bug investigation, root cause analysis, and fixes specialist",
				SystemPrompt: "pkg/agent/prompts/subagent_prompts/debugger.md",
				Enabled:      true,
			},
			"web_researcher": {
				ID:           "web_researcher",
				Name:         "Web Researcher",
				Description:  "Documentation lookup, API research, and solution discovery specialist",
				SystemPrompt: "pkg/agent/prompts/subagent_prompts/web_researcher.md",
				Enabled:      true,
			},
			"researcher": {
				ID:           "researcher",
				Name:         "Researcher",
				Description:  "Local codebase analysis and web research specialist - investigates code, architecture, and finds external information",
				SystemPrompt: "pkg/agent/prompts/subagent_prompts/researcher.md",
				Enabled:      true,
			},
		},
		Skills: map[string]Skill{
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
	if config.SubagentTypes == nil {
		config.SubagentTypes = make(map[string]SubagentType)
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
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

	// Apply default for EnableZshCommandDetection if not explicitly set
	// Note: We can't distinguish between "not set" and "set to false" in JSON unmarshaling
	// for booleans, so we only apply the default if the config version is old
	// However, since this is a new field in an existing config, we need to check
	// if it was explicitly set. A simple heuristic: if version < 2.1, enable it
	// Actually, better approach: Use the zero value as a signal to apply default
	// But since we want it true by default, we need to be careful
	// For now, let's just set it to true if it's false and the config was recently created
	// A better solution would be to use a pointer, but that would break the API
	// For now, we'll rely on the fact that new configs get the default via NewConfig()
	// and existing configs will need to be manually updated or we add migration logic
	// As a pragmatic solution: if the field doesn't exist in the JSON, it will be false,
	// so we need to detect this case and apply the default
	// The cleanest way is to check if the field exists in the raw JSON

	// Check if enable_zsh_command_detection exists in the raw JSON
	var rawConfig map[string]interface{}
	if err := json.Unmarshal(data, &rawConfig); err == nil {
		if _, exists := rawConfig["enable_zsh_command_detection"]; !exists {
			// Field doesn't exist in config file, apply default
			config.EnableZshCommandDetection = true
		}
		if _, exists := rawConfig["auto_execute_detected_commands"]; !exists {
			// Field doesn't exist in config file, apply default
			config.AutoExecuteDetectedCommands = true
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
	return "openai" // Ultimate fallback
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

// GetSubagentType retrieves a subagent type configuration by ID
// Returns nil if the subagent type doesn't exist or is disabled
func (c *Config) GetSubagentType(id string) *SubagentType {
	if c.SubagentTypes == nil {
		return nil
	}
	if subagentType, exists := c.SubagentTypes[id]; exists && subagentType.Enabled {
		return &subagentType
	}
	return nil
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
	// Skill path is relative to ledit source root
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
