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
	ConfigVersion   = "2.0"
	ConfigDirName   = ".sprout"
	ConfigFileName  = "config.json"
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

	// AllowGitHistoryRewrite controls whether commands that can lose commit
	// history are accepted via shell_command without going through the git
	// tool's approval flow. Specifically: `git reset --hard <commit-ish>`,
	// `git rebase`, `git branch -D`, `git tag -d`.
	//
	// Working-tree-only destructive ops (`git checkout .`, `git restore`,
	// `git clean -fd`, `git reset --hard HEAD`, etc.) are always allowed
	// because the change tracker captures pre-mutation content and exposes
	// recover_file / recover_bulk for restoration. History rewrites can't
	// be recovered through the tracker — only via the reflog — so they
	// stay gated by default.
	//
	// Default: false (gated). Set true in environments where the agent
	// has tighter feedback loops (e.g. user-facing chat where every step
	// is confirmed) and the friction of going through the git tool isn't
	// worth it.
	AllowGitHistoryRewrite bool `json:"allow_git_history_rewrite,omitempty"`

	// UnifiedRiskResolver enables the unified risk resolver (SP-068 Phase 2).
	// When true (the default), gating decisions at call sites use a single
	// ResolveToolRisk assessment instead of the split Gate 1 (static
	// classifier) → Gate 2 (persona risk cascade) path. When false, the
	// legacy dual-gate code paths run (retained for compatibility). Set to
	// false explicitly to opt out of the unified resolver.
	UnifiedRiskResolver bool `json:"unified_risk_resolver,omitempty"`

	// DaemonMultiSession enables N concurrent browser windows per user
	// when the server runs in service/daemon mode (SPROUT_SERVICE=1).
	// When true, the WebSocket dispatcher routes new connections through
	// the multi-session path (handleWebSocket_Daemon); each connection
	// gets its own chat session and its own agent without colliding
	// with sibling windows. When false, the daemon falls back to the
	// single-active-session path (handleWebSocket_Agent) and the second
	// window triggers session_conflict + takeover — the pre-SP-118
	// behavior.
	//
	// Effective value at the dispatch site is
	//   effective = (agentEnforceSingleSession == false) && DaemonMultiSession
	// i.e. the agent path always uses Mode 1 regardless of this flag,
	// and the daemon path uses Mode 2 only when this flag is true.
	//
	// Default: true (the daemon opens with multi-session on, so three
	// browser windows on the same daemon each get their own chat
	// without a takeover prompt). Set to false to opt out without
	// editing code — useful for rolling forward / back during the
	// SP-118 rollout window. SP-118 Phase 4 will flip the rollout so
	// this default is the new normal.
	DaemonMultiSession bool `json:"daemon_multi_session,omitempty"`

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

	// RefreshSystemPromptOnModelChange (Spec B): when true, re-derive the
	// agent's system prompt on every provider/model swap instead of
	// freezing it at agent-creation time. Different models respond very
	// differently to the same prompt — a Claude-tuned prompt sent to
	// GPT-4 may be silently ignored. With this on, the agent re-runs
	// GetEmbeddedSystemPromptForProfile on each SetProvider / SelectProvider
	// so the prompt stays in sync with the active provider and context
	// window. Defaults to false so existing sessions are bit-identical.
	// Flip to true after observability shows the new behavior lands
	// cleanly. The agent's `SystemPromptText` override still wins — only
	// the embedded portion is re-derived, the configured override is
	// re-applied on top.
	RefreshSystemPromptOnModelChange bool `yaml:"refresh_system_prompt_on_model_change,omitempty" json:"refresh_system_prompt_on_model_change,omitempty"`

	// SkipPrompt - for non-interactive mode
	SkipPrompt bool `json:"skip_prompt,omitempty"`

	// RiskProfile selects a named preset for the shell-command risk
	// cascade: readonly / cautious / default / permissive /
	// unrestricted. Empty or unrecognized values resolve to "default"
	// via AutoApproveRulesForProfile. Per-persona AutoApproveRules
	// always win over the profile. The CLI's --risk-profile flag and
	// a workflow step's "risk_profile" field both override this
	// value.
	RiskProfile string `json:"risk_profile,omitempty"`

	// ContextMode selects a named context-engine preset: "" (full
	// default) | "full" | "low_context". Resolved into a ContextProfile
	// via ResolveContextProfile at agent creation; call sites read
	// fields off the resolved profile rather than this string directly.
	// Empty/unrecognized values fall through to auto-detection by the
	// resolver (which honors a small enough model window).
	ContextMode ContextMode `json:"context_mode,omitempty"`

	// RiskProfiles allows the user to override the baked-in rules
	// for any named profile. Keys are profile names (readonly,
	// cautious, default, permissive, unrestricted, or any
	// user-defined name); values replace the built-in rules entirely
	// for that name. Useful when the user wants a slightly different
	// definition of "cautious" or wants to add their own named
	// profile. See docs/SECURITY.md#risk-profiles.
	RiskProfiles map[string]AutoApproveRules `json:"risk_profiles,omitempty"`

	// ApprovedShellCommands is the user's persistent allowlist of
	// literal shell command strings that should auto-approve through
	// the high-risk cascade without prompting. Populated by the
	// "Always approve this command" choice on the approval dialog
	// (SP-058 follow-up). Stored as exact strings — matching is
	// command-literal equality, not pattern matching, so allow-listing
	// `rm -rf /tmp/build-cache` does NOT allow `rm -rf anything-else`.
	// The Critical tier still blocks regardless of this allowlist.
	// Users can edit this list directly in config.json to revoke an
	// entry, or remove all entries to reset.
	ApprovedShellCommands []string `json:"approved_shell_commands,omitempty"`

	// ApprovedShellCommandPatterns is the user's persistent allowlist of
	// glob patterns for shell commands that should auto-approve through
	// the high-risk cascade without prompting. Patterns use Go's path.Match
	// syntax: `*` matches any sequence of characters (but NOT `/`),
	// `?` matches any single character, `[abc]` matches a character class.
	// For example, `rm -rf /tmp/*` matches `rm -rf /tmp/build` but NOT
	// `rm -rf /home/x`. The Critical tier still blocks regardless of
	// pattern matches — patterns cannot override critical-tier gating,
	// which is enforced at the call site before this allowlist is consulted.
	// Users can edit this list directly in config.json to revoke an entry,
	// or remove all entries to reset.
	ApprovedShellCommandPatterns []string `json:"approved_shell_command_patterns,omitempty"`

	// CommandPolicies is the unified command policy layer (SP-123) with
	// three actions: allow (auto-approve), ask (force prompt), deny
	// (hard block). Rules are evaluated before the classifier and risk
	// profile, first-match-wins. Replaces the fragmented
	// approved_shell_commands / approved_shell_command_patterns /
	// security_policy.denied_commands surfaces. Legacy fields remain for
	// backward compatibility; MigrateCommandPolicies converts them on load.
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

	// Vision Fallback Configuration
	// VisionFallbackToOCR enables transparent fallback to the OCR model
	// when the primary vision model fails after retries. When true and
	// PDFOCRModel is configured, a single OCR attempt is made as a last
	// resort. Default: true (enabled). Controlled by VISION_FALLBACK_TO_OCR
	// env var (SPROUT_ / SPROUT_ prefixes).
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

	// Computer Use Configuration (SP-063) — gates the computer_user persona's
	// mouse/keyboard/screenshot tools. Off by default; the tools are never
	// available unless this is explicitly enabled.
	ComputerUse *ComputerUseConfig `json:"computer_use,omitempty"`

	// Vision Configuration (SP-103-C3) — controls vision-pipeline runtime
	// behavior: parallel worker pool size, global concurrency cap, and
	// multi-image batching. All fields have safe defaults via Resolve().
	Vision *VisionConfig `json:"vision,omitempty"`

	// Change Tracking Configuration — controls the shell-mutation
	// snapshot pass. Direct file-tool hooks (write_file, edit_file,
	// patch_structured_file) are always tracked; this struct only
	// gates the walker that detects shell_command mutations.
	ChangeTracking *ChangeTrackingConfig `json:"change_tracking,omitempty"`

	// Skills Configuration
	Skills map[string]Skill `json:"skills,omitempty"` // Agent Skills that can be loaded into context

	// Zsh Command Execution
	EnableZshCommandDetection   bool `json:"enable_zsh_command_detection"`   // Enable zsh-aware command detection (default: true)
	AutoExecuteDetectedCommands bool `json:"auto_execute_detected_commands"` // Auto-execute detected commands without prompting (default: true)

	// Security Policy Configuration
	SecurityPolicy *SecurityPolicy `json:"security_policy,omitempty"`

	// Shell Configuration — user-configurable shell permission policy
	// (SP-049 Phase 2). Lets users define safe/dangerous command patterns
	// and a workspace-overlay mode.
	Shell ShellConfig `json:"shell,omitempty"`

	// MaxContextTokens caps the effective context window used when building
	// requests. When set, the agent acts as if the model has at most this
	// many tokens of context, limiting how large an input (and therefore
	// completion budget) a single request can claim. Useful as a cost-control
	// measure when using models with very large native context windows
	// (e.g. 1M-token models billed per input token). Nil or 0 means no cap.
	MaxContextTokens *int `json:"max_context_tokens,omitempty"`

	// Notifications Configuration (SP-070) — controls how the agent
	// notifies the user when long-running turns complete.
	Notifications *NotificationsConfig `json:"notifications,omitempty"`

	// Edit Approval Configuration (SP-072) — controls the per-hunk
	// diff approval gate for agent file writes.
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

	// Wakeup controls auto-resume behavior for background task completions
	// (SP-108). When enabled, the daemon automatically processes pending
	// notifications by calling ProcessQueryWithContinuity so the agent can
	// act on completed background tasks without the user sending a manual
	// message. Budget controls prevent unattended token burn loops.
	Wakeup WakeupConfig `json:"wakeup,omitempty"`

	// Training controls opt-in session recording for training data
	// collection. When enabled, each saved session is PII-redacted and
	// pushed to the configured endpoint as JSON. OFF by default — must
	// be explicitly enabled via --train flag, SPROUT_TRAIN_ENABLED env,
	// or config.json.
	Training TrainingConfig `json:"training,omitempty"`

	// Other flags
	FromAgent bool `json:"-"` // Internal flag, not persisted

	// Conflict-detection metadata (SP-034-4a). Populated by Load() from
	// the on-disk file's stat. Save() compares the file's current stat
	// against these and returns ConfigConflictError when they differ —
	// catches the case where another agent process or another webui tab
	// modified the file while this Config was in memory. NOT serialized.
	loadedModTime time.Time
	loadedSize    int64
}

// WakeupConfig controls auto-resume behavior for background task completions
// (SP-108). Stored in config.json under the "wakeup" key.
type WakeupConfig struct {
	Enabled              bool `json:"enabled"`                 // Master switch; default false
	MaxTokensPerSession  int  `json:"max_tokens_per_session"`  // Hard cap on auto-resume token spend; default 5000
	MaxResumesPerSession int  `json:"max_resumes_per_session"` // Max auto-resumes before requiring user input; default 10
}

// DefaultWakeupConfig returns conservative defaults.
func DefaultWakeupConfig() WakeupConfig {
	return WakeupConfig{
		Enabled:              false,
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
			Enabled:             false,
			AutoIndex:           false,
			SimilarityThreshold: 0.90,
			MaxResults:          3,
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
