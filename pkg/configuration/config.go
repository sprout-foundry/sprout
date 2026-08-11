package configuration

import (
	"fmt"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// personaDefaultsWarningOnce guards the warning output when embedded persona
// definitions fail to load during defaultSubagentTypes initialization.
var personaDefaultsWarningOnce sync.Once

const (
	ConfigVersion  = "2.0"
	ConfigDirName  = ".sprout"
	ConfigFileName = "config.json"

	// WorkspaceConfigFileName is the per-workspace config file. Deliberately
	// different from ConfigFileName to avoid a collision when the workspace
	// root is $HOME (both layers would otherwise share the same directory).
	WorkspaceConfigFileName = "workspace.json"

	// ConfigLocalFileName is the user-scope machine-local override file.
	// Same schema as ConfigFileName, higher precedence, never committed.
	// Lives in the config dir alongside config.json.
	ConfigLocalFileName = "config.local.json"

	// WorkspaceLocalFileName is the workspace-scope personal override file.
	// Same schema as WorkspaceConfigFileName, higher precedence within the
	// workspace layer, gitignored.
	WorkspaceLocalFileName = "workspace.local.json"

	APIKeysFileName = "api_keys.json"

	OutputVerbosityCompact = "compact"
	OutputVerbosityDefault = "default"
	OutputVerbosityVerbose = "verbose"
)

// Config represents the unified application configuration
type Config struct {
	Version string `json:"version"`

	// Provider and Model Configuration
	LastUsedProvider string            `json:"last_used_provider"`
	ProviderModels   map[string]string `json:"provider_models"`
	ProviderPriority []string          `json:"provider_priority"`

	// LocalModelPath is the model directory used by the local LLM server
	// (sprout-local provider). When non-empty, sprout auto-starts llm_server
	// with this model before agent creation.
	LocalModelPath string `json:"local_model_path,omitempty"`

	// Language Server Override Configuration
	LanguageServers []LanguageServerOverride `json:"language_servers,omitempty"`

	// MCP Configuration
	MCP mcp.MCPConfig `json:"mcp"`

	// Preferences
	Preferences map[string]interface{} `json:"preferences,omitempty"`

	// DisableCoordinatorAutoActivate opts out of the automatic activation of the
	// coordinator persona (formerly Executive Assistant) when sprout starts in
	// the user's $HOME directory. When true, no persona is auto-activated and
	// the user must select one explicitly. Default false (auto-activate).
	DisableCoordinatorAutoActivate bool `json:"disable_coordinator_auto_activate,omitempty"`

	// AllowGitHistoryRewrite allows history-rewriting git commands
	// (reset --hard, rebase, branch -D, tag -d) via shell_command
	// without the git tool's approval flow. Default: false (gated).
	AllowGitHistoryRewrite bool `json:"allow_git_history_rewrite,omitempty"`

	// UnifiedRiskResolver enables the unified risk resolver. When true,
	// gating uses a single ResolveToolRisk assessment instead of the
	// legacy dual-gate path. Default: true.
	UnifiedRiskResolver bool `json:"unified_risk_resolver,omitempty"`

	// DaemonMultiSession enables concurrent browser windows in daemon mode.
	// Each connection gets its own chat session and agent. Default: true.
	DaemonMultiSession bool `json:"daemon_multi_session,omitempty"`

	// ResourceDirectory stores captured web/vision resources relative to the current working directory.
	// This can be overridden at runtime with --resource-directory.
	ResourceDirectory string `json:"resource_directory,omitempty"`

	// ReasoningEffort sets a global default reasoning effort for chat requests.
	// Valid values: "low", "medium", "high". Empty means automatic selection.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// DisableThinking disables thinking/reasoning mode for thinking-capable models.
	DisableThinking bool `json:"disable_thinking,omitempty"`

	// SystemPromptText overrides the main agent system prompt inline.
	// Empty means use the embedded default prompt.
	SystemPromptText string `json:"system_prompt_text,omitempty"`

	// RefreshSystemPromptOnModelChange re-derives the agent's system prompt
	// on every provider/model swap. Defaults to false.
	RefreshSystemPromptOnModelChange bool `yaml:"refresh_system_prompt_on_model_change,omitempty" json:"refresh_system_prompt_on_model_change,omitempty"`

	// SkipPrompt - for non-interactive mode
	SkipPrompt bool `json:"skip_prompt,omitempty"`

	// RiskProfile selects a named preset for the shell-command risk cascade:
	// readonly / cautious / default / permissive / unrestricted.
	RiskProfile string `json:"risk_profile,omitempty"`

	// ContextMode selects a named context-engine preset: "" (full default) | "full" | "low_context".
	ContextMode ContextMode `json:"context_mode,omitempty"`

	// RiskProfiles allows the user to override the baked-in rules for any named profile.
	RiskProfiles map[string]AutoApproveRules `json:"risk_profiles,omitempty"`

	// ApprovedShellCommands is the user's persistent allowlist of literal
	// shell command strings that auto-approve through the high-risk cascade.
	ApprovedShellCommands []string `json:"approved_shell_commands,omitempty"`

	// ApprovedShellCommandPatterns is the user's persistent allowlist of glob
	// patterns for shell commands that auto-approve through the high-risk cascade.
	ApprovedShellCommandPatterns []string `json:"approved_shell_command_patterns,omitempty"`

	// CommandPolicies is the unified command policy layer with three actions:
	// allow (auto-approve), ask (force prompt), deny (hard block).
	CommandPolicies *CommandPolicies `json:"command_policies,omitempty"`

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

	// Subagent Configuration
	SubagentProvider string `json:"subagent_provider,omitempty"` // Provider for subagents (defaults to LastUsedProvider)
	SubagentModel    string `json:"subagent_model,omitempty"`    // Model for subagents (defaults to provider's default model)
	// SubagentTypes is hydrated from the embedded catalog at config load time.
	// It is NOT persisted (json:"-"): personas are catalog-fixed and user
	// customization is intentionally not supported. Use DisabledPersonas to
	// hide specific personas from /persona list and from subagent spawning.
	SubagentTypes map[string]SubagentType `json:"-"`
	// explicitKeys records the dotted JSON paths this layer actually contained
	// on disk, letting MergeConfig tell "set to false" from "not set". It is
	// layer provenance rather than configuration, so it is unexported and never
	// serialized. See config_explicit_keys.go.
	explicitKeys map[string]bool
	// DisabledPersonas holds canonical persona IDs the user has hidden via
	// `/persona <id> disable`. The catalog entries themselves are never
	// mutated; resolution checks this list and treats disabled IDs as absent.
	DisabledPersonas []string `json:"disabled_personas,omitempty"`
	// DefaultSubagentPersona is the persona ID used when run_subagent is called
	// without a persona argument. Defaults to "general" if unset. Setting this
	// lets users redirect default spawns without editing the catalog.
	DefaultSubagentPersona  string `json:"default_subagent_persona,omitempty"`
	SubagentMaxParallel     int    `json:"subagent_max_parallel,omitempty"`     // Maximum number of parallel subagents (default: 2)
	SubagentParallelEnabled *bool  `json:"subagent_parallel_enabled,omitempty"` // Enable/disable parallel subagent execution (default: true)
	SubagentMaxDepth        int    `json:"subagent_max_depth,omitempty"`        // Maximum subagent nesting depth (default: 2)

	// Commit Configuration
	CommitProvider string `json:"commit_provider,omitempty"` // Provider for commit message generation (defaults to LastUsedProvider)
	CommitModel    string `json:"commit_model,omitempty"`    // Model for commit message generation (defaults to provider's default model)

	// Review Configuration
	ReviewProvider string `json:"review_provider,omitempty"` // Provider for review commands (defaults to LastUsedProvider)
	ReviewModel    string `json:"review_model,omitempty"`    // Model for review commands (defaults to provider's default model)

	// Completion Configuration
	CompletionProvider string `json:"completion_provider,omitempty"` // Provider for code completions (defaults to LastUsedProvider)
	CompletionModel    string `json:"completion_model,omitempty"`    // Model for code completions (defaults to provider's default model)

	// VisionFallbackToOCR enables transparent fallback to the OCR model when
	// the primary vision model fails after retries. Default: true.
	VisionFallbackToOCR bool `json:"vision_fallback_to_ocr,omitempty"`

	// PDF OCR Configuration
	PDFOCREnabled    bool   `json:"pdf_ocr_enabled,omitempty"`    // Enable PDF OCR processing
	PDFOCRProvider   string `json:"pdf_ocr_provider,omitempty"`   // Provider for PDF OCR (e.g., "ollama", "openai", "deepinfra")
	PDFOCRModel      string `json:"pdf_ocr_model,omitempty"`      // Model for PDF OCR (e.g., "glm-ocr", "llama3.2-vision")
	PDFOCRDownloaded bool   `json:"pdf_ocr_downloaded,omitempty"` // Whether the model has been downloaded

	// Embedding Index Configuration
	EmbeddingIndex *EmbeddingIndexConfig `json:"embedding_index,omitempty"`

	// Persistent Context Configuration
	PersistentContext *PersistentContextConfig `json:"persistent_context,omitempty"`

	// ComputerUse gates the computer_user persona's desktop-control tools. Off by default.
	ComputerUse *ComputerUseConfig `json:"computer_use,omitempty"`

	// Vision controls vision-pipeline runtime: parallel workers, concurrency cap, and batching.
	Vision *VisionConfig `json:"vision,omitempty"`

	// ChangeTracking gates the ChangeTracker shell-mutation snapshot walk.
	ChangeTracking *ChangeTrackingConfig `json:"change_tracking,omitempty"`

	// Skills Configuration
	Skills map[string]Skill `json:"skills,omitempty"` // Agent Skills that can be loaded into context

	// Zsh Command Execution
	EnableZshCommandDetection   bool `json:"enable_zsh_command_detection"`   // Enable zsh-aware command detection (default: true)
	AutoExecuteDetectedCommands bool `json:"auto_execute_detected_commands"` // Auto-execute detected commands without prompting (default: true)

	// Security Policy Configuration
	SecurityPolicy *SecurityPolicy `json:"security_policy,omitempty"`

	// Shell is the user-configurable shell permission policy.
	Shell ShellConfig `json:"shell,omitempty"`

	// MaxContextTokens caps the effective context window. Nil or 0 means no cap.
	MaxContextTokens *int `json:"max_context_tokens,omitempty"`

	// Notifications controls how the agent notifies the user when long-running turns complete.
	Notifications *NotificationsConfig `json:"notifications,omitempty"`

	// EditApproval controls the per-hunk diff approval gate for agent file writes.
	EditApproval *EditApprovalConfig `json:"edit_approval,omitempty"`

	// OutputVerbosity controls how much inter-tool-call narration and
	// streaming detail the UI shows. Valid values: "compact" (hide
	// interim model messages, show only tool results and final text),
	// "default" (show tool calls with results, show streaming final
	// text), "verbose" (show everything including interim narration).
	// Empty defaults to "default".
	OutputVerbosity string `json:"output_verbosity,omitempty"`

	// ShowToolInvocations controls whether the UI expands per-tool
	// invocation details in the conversation output. When false, tool
	// calls are collapsed/hidden. Defaults to true.
	ShowToolInvocations bool `json:"show_tool_invocations,omitempty"`

	// Wakeup controls auto-resume behavior for background task completions.
	Wakeup WakeupConfig `json:"wakeup,omitempty"`

	// Training controls opt-in session recording for training data collection. OFF by default.
	Training TrainingConfig `json:"training,omitempty"`

	// Other flags
	FromAgent bool `json:"-"` // Internal flag, not persisted

	// Conflict-detection metadata. Populated by Load(), compared in Save(). NOT serialized.
	loadedModTime time.Time
	loadedSize    int64
}

// WakeupConfig controls auto-resume behavior for background task completions.
type WakeupConfig struct {
	Enabled              bool `json:"enabled"`                 // Master switch; default true
	MaxTokensPerSession  int  `json:"max_tokens_per_session"`  // Hard cap on auto-resume token spend; default 5000
	MaxResumesPerSession int  `json:"max_resumes_per_session"` // Max auto-resumes before requiring user input; default 10
}

// DefaultWakeupConfig returns conservative defaults.
func DefaultWakeupConfig() WakeupConfig {
	return WakeupConfig{
		Enabled:              true,
		MaxTokensPerSession:  5000,
		MaxResumesPerSession: 10,
	}
}

// TrainingConfig controls opt-in session recording for training data
// collection. When enabled, PII-redacted conversation states are pushed
// to the configured endpoint after each session save.
type TrainingConfig struct {
	// Endpoint is the URL to push training data to (e.g. http://localhost:8190).
	// Sessions are POSTed to {Endpoint}/sessions as JSON.
	Endpoint string `json:"endpoint,omitempty"`

	// Enabled controls whether training data is collected and pushed.
	// ALWAYS false by default — must be explicitly enabled.
	Enabled bool `json:"enabled,omitempty"`

	// ExcludePaths is a list of working directory prefixes to exclude from
	// training data. Sessions whose working directory starts with any of
	// these paths are silently skipped.
	ExcludePaths []string `json:"exclude_paths,omitempty"`
}

// MCPConfig moved to pkg/mcp package for consolidation
// Import from there: github.com/sprout-foundry/sprout/pkg/mcp

// MCPServerConfig moved to pkg/mcp package for consolidation
// Import from there: github.com/sprout-foundry/sprout/pkg/mcp

type APIKeys map[string]string

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
			"ollama-cloud": "deepseek-v3.1:671b",
		},
		ProviderPriority: []string{
			"openrouter",
			"zai",
			"deepinfra",
			"ollama-cloud",
			"ollama-local",
			"openai",
		},
		CustomProviders:      make(map[string]CustomProviderConfig),
		CommandHistoryByPath: make(map[string][]string),
		HistoryIndexByPath:   make(map[string]int),
		MCP:                  mcp.DefaultMCPConfig(),
		Preferences:          make(map[string]interface{}),
		APITimeouts: &APITimeoutConfig{
			ConnectionTimeoutSec:    300,
			FirstChunkTimeoutSec:    600,
			ChunkTimeoutSec:         600,
			OverallTimeoutSec:       1800,
			CommitMessageTimeoutSec: 300, // 5 minutes for commit message generation
		},
		HistoryScope:                "project", // Default to project-scoped history
		EnableZshCommandDetection:   true,      // Enable zsh command detection by default
		AutoExecuteDetectedCommands: true,      // Auto-execute detected commands without prompting
		DaemonMultiSession:          true,      // SP-118 Phase 4: daemon default-on for multi-window
		SubagentTypes:               defaultSubagentTypes(),
		Skills:                      defaultSkills(),
		PDFOCREnabled:               true,
		PDFOCRProvider:              "ollama",
		PDFOCRModel:                 "glm-ocr",
		SubagentMaxParallel:         2,                                       // Default max parallel subagents
		SubagentParallelEnabled:     func() *bool { t := true; return &t }(), // Default to enabling parallel subagents
		EmbeddingIndex: &EmbeddingIndexConfig{
			Enabled:    boolPtr(false),
			AutoIndex:  boolPtr(false),
			MaxResults: 3,
		},
	}
}

// Validate checks the configuration for consistency and returns an error
// if any invalid settings are found. Returns the first error encountered.
func (c *Config) Validate() error {
	// Validate output verbosity
	switch c.OutputVerbosity {
	case "", OutputVerbosityCompact, OutputVerbosityDefault, OutputVerbosityVerbose:
	default:
		return fmt.Errorf("invalid output_verbosity %q: must be one of %q, %q, %q",
			c.OutputVerbosity, OutputVerbosityCompact, OutputVerbosityDefault, OutputVerbosityVerbose)
	}

	// Validate PDF OCR settings
	if c.PDFOCREnabled {
		if c.PDFOCRProvider == "" {
			return fmt.Errorf("PDF OCR provider cannot be empty when PDF OCR is enabled")
		}
		if c.PDFOCRModel == "" {
			return fmt.Errorf("PDF OCR model cannot be empty when PDF OCR is enabled")
		}
	}

	// Validate shell config
	if err := c.Shell.Validate(); err != nil {
		return err
	}

	return nil
}
