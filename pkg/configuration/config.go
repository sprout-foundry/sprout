package configuration

import (
	"fmt"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// personaDefaultsWarningOnce guards the warning output when embedded persona
// definitions fail to load during defaultSubagentTypes initialization.
var personaDefaultsWarningOnce sync.Once

const (
	ConfigVersion  = "2.1"
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

	// Roles is the SP-150 §150a role-model section: named roles
	// (planner, coder, summarizer, reviewer, commit) map to a provider
	// and model; unset roles fall back to the conversation's
	// provider/model (ResolveRole). The existing per-setting model
	// fields (subagent_model, commit_model, review/completion models)
	// are read as aliases for their roles (item 150.2); they are not
	// yet the read path (150.2/150.3 rewires the getters).
	Roles map[string]RoleConfig `json:"roles,omitempty"`

	// Language Server Override Configuration
	LanguageServers []LanguageServerOverride `json:"language_servers,omitempty"`

	// MCP Configuration
	MCP mcp.MCPConfig `json:"mcp"`

	// Preferences
	Preferences map[string]interface{} `json:"preferences,omitempty"`

	// CoordinatorAutoActivate opts IN to automatic activation of the
	// coordinator persona when sprout starts in the user's $HOME directory.
	// Default false — coordinator is opt-in via '/persona coordinator'.
	// (Historical note: 'disable_coordinator_auto_activate: true' had the
	// same effect and is still honored as a no-op for config compatibility.)
	CoordinatorAutoActivate bool `json:"coordinator_auto_activate,omitempty"`

	// DisableCoordinatorAutoActivate is the legacy opt-out flag, kept as a
	// no-op for config compatibility. Coordinator activation is now opt-in
	// via CoordinatorAutoActivate.
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

	// DisableUpdateCheck opts out of the passive "new release available"
	// check (GitHub releases lookup at most once per day, notice at most
	// once per day). Default: false. SPROUT_NO_UPDATE_CHECK=1 and CI
	// environments also opt out.
	DisableUpdateCheck bool `json:"disable_update_check,omitempty"`

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

	// Verification controls the SP-149 verification run ("verified done"):
	// the turn-end gate that runs the plan's acceptance checks (149a) and
	// the repair loop (149c). Off by default in the CLI; any config layer —
	// global, project (workspace), or an embedding environment writing the
	// same layers — enables it through the same "verification" section
	// (149e). Nil means off with the default repair-attempt limit.
	Verification *VerificationConfig `json:"verification,omitempty"`

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

	// Language is the user's preferred conversation language, used by the
	// outbound language guard (SP-152 152.3) as a fallback when the user's
	// recent messages are too short or too mixed to resolve a majority
	// language. It is an ISO 639-1 code (or the 639-3 code for the few
	// languages without a 639-1 code), case-insensitive. Empty means no
	// configured fallback — the guard then treats the user's language as
	// undetermined and never guesses. Reading it as a langguard.Language is
	// langguard.ParseLanguage(cfg.Language).
	Language string `json:"language,omitempty"`

	// DisableLanguageGuard turns off the outbound language guard (SP-152
	// 152f), which is on by default everywhere, including the CLI. Any
	// config layer (global, workspace, session) may set this to true; the
	// layer merge tracks the key's presence, so an explicit false in a
	// narrower layer re-enables the guard even over a broader layer's
	// disable. Default: false (guard enabled). See LanguageGuardEnabled.
	DisableLanguageGuard bool `json:"disable_language_guard,omitempty"`

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
	MaxTokensPerSession  int  `json:"max_tokens_per_session"`  // Cap on auto-resume token spend between user messages; default 500000
	MaxResumesPerSession int  `json:"max_resumes_per_session"` // Max auto-resumes before requiring user input; default 10
}

// DefaultWakeupConfig returns defaults. Auto-resume is ON by default: a
// config without a wakeup block resolves Enabled=true through the NewConfig
// seed, and only an explicit "enabled": false disables it.
func DefaultWakeupConfig() WakeupConfig {
	return WakeupConfig{
		Enabled:              true,
		MaxTokensPerSession:  DefaultWakeupMaxTokens,
		MaxResumesPerSession: 10,
	}
}

// DefaultWakeupMaxTokens bounds the tokens auto-resume turns may spend
// between two user messages (the budget resets on each real user query).
// A resume turn resends the whole conversation, so one turn on a working
// context costs on the order of 100k tokens; the bound allows a few resumes
// per message while MaxResumesPerSession caps the count.
const DefaultWakeupMaxTokens = 500_000

// legacyWakeupMaxTokens is the previous default. It was too small for any
// real resume turn — the first resume always exhausted it, so later
// completions waited for the user's next message.
const legacyWakeupMaxTokens = 5000

// upgradeLegacyWakeupBudget raises a token budget still at the legacy
// default. Full config saves materialize the defaults, so the old value sits
// in most user configs without having been chosen; any other value is an
// explicit setting and is kept.
func upgradeLegacyWakeupBudget(c *Config) {
	if c != nil && c.Wakeup.MaxTokensPerSession == legacyWakeupMaxTokens {
		c.Wakeup.MaxTokensPerSession = DefaultWakeupMaxTokens
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

// defaultProviderModels is the offline fallback used when the provider
// catalog cannot serve a recommendation (test binaries, refresh failures,
// air-gapped installs). Kept deliberately minimal: the live defaults come
// from pkg/providercatalog via defaultProviderModelsCatalogDriven, so this
// map cannot silently drift from the catalog again.
var defaultProviderModels = map[string]string{
	"openai":       "gpt-5-mini",
	"zai":          "GLM-4.6",
	"deepinfra":    "deepseek-ai/DeepSeek-V4-Flash-0731",
	"openrouter":   "openai/gpt-5",
	"ollama-local": "qwen3-coder:30b",
	"ollama-cloud": "deepseek-v4-flash",
}

// defaultProviderModelsCatalogDriven overrides the static fallback with the
// provider catalog's curated default/recommended models. A provider keeps
// its static entry when the catalog has no model for it (local providers
// like ollama-local whose model IDs aren't curated).
func defaultProviderModelsCatalogDriven() map[string]string {
	out := make(map[string]string, len(defaultProviderModels))
	for k, v := range defaultProviderModels {
		out[k] = v
	}
	for id := range out {
		p, ok := providercatalog.FindProvider(id)
		if !ok {
			continue
		}
		if m := p.DefaultModel; m != "" {
			out[id] = m
		} else if m := p.RecommendedModel; m != "" {
			out[id] = m
		}
	}
	return out
}

// NewConfig creates a new configuration with sensible defaults
func NewConfig() *Config {
	return &Config{
		Version:          ConfigVersion,
		LastUsedProvider: "",
		ProviderModels:   defaultProviderModelsCatalogDriven(),
		ProviderPriority: []string{
			"deepinfra",
			"openrouter",
			"zai",
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
		SubagentMaxParallel:         2,                                       // Default max parallel subagents
		SubagentParallelEnabled:     func() *bool { t := true; return &t }(), // Default to enabling parallel subagents
		Wakeup:                      DefaultWakeupConfig(),
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

	// Validate shell config
	if err := c.Shell.Validate(); err != nil {
		return err
	}

	return nil
}
