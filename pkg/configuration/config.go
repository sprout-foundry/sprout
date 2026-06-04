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
	"github.com/sprout-foundry/sprout/pkg/skills"
)

var personaDefaultsWarningOnce sync.Once

func isDebugEnabled() bool {
	value := strings.TrimSpace(GetEnvSimple("DEBUG"))
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
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

	// Language Server Override Configuration
	LanguageServers []LanguageServerOverride `json:"language_servers,omitempty"`

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

	// RiskProfile selects a named preset for the shell-command risk
	// cascade: readonly / cautious / default / permissive /
	// unrestricted. Empty or unrecognized values resolve to "default"
	// via AutoApproveRulesForProfile. Per-persona AutoApproveRules
	// always win over the profile. The CLI's --risk-profile flag and
	// a workflow step's "risk_profile" field both override this
	// value.
	RiskProfile string `json:"risk_profile,omitempty"`

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
	// SubagentTypes is hydrated from the embedded catalog at config load time.
	// It is NOT persisted (json:"-"): personas are catalog-fixed and user
	// customization is intentionally not supported. Use DisabledPersonas to
	// hide specific personas from /persona list and from subagent spawning.
	SubagentTypes          map[string]SubagentType `json:"-"`
	// DisabledPersonas holds canonical persona IDs the user has hidden via
	// `/persona <id> disable`. The catalog entries themselves are never
	// mutated; resolution checks this list and treats disabled IDs as absent.
	DisabledPersonas       []string                `json:"disabled_personas,omitempty"`
	// DefaultSubagentPersona is the persona ID used when run_subagent is called
	// without a persona argument. Defaults to "general" if unset. Setting this
	// lets users redirect default spawns without editing the catalog.
	DefaultSubagentPersona string                  `json:"default_subagent_persona,omitempty"`
	SubagentMaxParallel    int                     `json:"subagent_max_parallel,omitempty"`     // Maximum number of parallel subagents (default: 2)
	SubagentParallelEnabled *bool                   `json:"subagent_parallel_enabled,omitempty"` // Enable/disable parallel subagent execution (default: true)
	SubagentMaxDepth       int                     `json:"subagent_max_depth,omitempty"`       // Maximum subagent nesting depth (default: 2)

	// EAMode controls how the Executive Assistant persona operates.
	// "interactive" = standard chat interface (default)
	// "queue" = autonomous task processing, exits when queue is empty
	EAMode string `json:"ea_mode,omitempty"`

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

	// Persistent Context Configuration
	PersistentContext *PersistentContextConfig `json:"persistent_context,omitempty"`

	// Change Tracking Configuration — controls the shell-mutation
	// snapshot pass. Direct file-tool hooks (write_file, edit_file,
	// patch_structured_file) are always tracked; this struct only
	// gates the walker that detects shell_command mutations.
	ChangeTracking *ChangeTrackingConfig `json:"change_tracking,omitempty"`

	// Skills Configuration
	Skills map[string]Skill `json:"skills,omitempty"` // Agent Skills that can be loaded into context

	// Zsh Command Execution
	EnableZshCommandDetection   bool `json:"enable_zsh_command_detection,omitempty"`   // Enable zsh-aware command detection (default: false)
	AutoExecuteDetectedCommands bool `json:"auto_execute_detected_commands,omitempty"` // Auto-execute detected commands without prompting (default: true)

	// Security Policy Configuration
	SecurityPolicy *SecurityPolicy `json:"security_policy,omitempty"`

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

// LanguageServerOverride allows users to customize or add language server
// configurations beyond the built-in defaults. When a matching ID exists
// in the default set, this override replaces it entirely. New IDs are
// appended to the merged list.
type LanguageServerOverride struct {
	ID          string   `json:"id" yaml:"id"`                              // Unique server ID (e.g. "go", "typescript")
	Binary      string   `json:"binary" yaml:"binary"`                      // Path to the binary (e.g. "gopls", "typescript-language-server")
	Args        []string `json:"args,omitempty" yaml:"args,omitempty"`      // Command-line arguments (e.g. ["--stdio"])
	LanguageIDs []string `json:"language_ids,omitempty" yaml:"language_ids,omitempty"` // Language IDs this server handles (e.g. ["go"])
	InstallHint string   `json:"install_hint,omitempty" yaml:"install_hint,omitempty"` // Installation instructions
}

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

// RiskLevel represents the risk classification of an operation for the EA approval cascade.
type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "low"      // Auto-approve (git status, read operations)
	RiskLevelMedium RiskLevel = "medium"   // Reason and decide (git commit, git push)
	RiskLevelHigh   RiskLevel = "high"     // Prompt the user when interactive; reject when not
	RiskLevelCritical RiskLevel = "critical" // Never approvable: rm -rf root, fork bombs
)

// AutoApproveRules controls the EA's sliding risk cascade for operation approvals.
type AutoApproveRules struct {
	LowRiskOps     []string `json:"low_risk,omitempty"`        // Operations auto-approved by EA
	MediumRiskOps  []string `json:"medium_risk,omitempty"`     // Operations the EA reasons about
	HighRiskNever  []string `json:"high_risk_never,omitempty"` // Pattern names always gated (rm_recursive, force_flag, ...)
	// DefaultRisk is the level returned for operations that don't
	// match any of the above. Default (empty) is "medium" — the
	// classic EA behavior. Cautious profiles set this to "high"
	// so unrecognized commands route to a prompt. Permissive /
	// unrestricted set it to "low" so common operations auto-approve.
	DefaultRisk RiskLevel `json:"default_risk,omitempty"`
}

// DefaultAutoApproveRules returns the default risk cascade rules for the EA persona.
func DefaultAutoApproveRules() AutoApproveRules {
	return AutoApproveRulesForProfile(RiskProfileDefault)
}

// RiskProfile names a preset risk-cascade configuration. The active
// profile resolves to an AutoApproveRules via AutoApproveRulesForProfile.
// Persona-specified rules always take precedence over the profile.
type RiskProfile string

const (
	// RiskProfileReadonly — strictest. ONLY read operations (git
	// status / log / diff, read_file) are permitted. Every write,
	// edit, shell command, or destructive op is BLOCKED outright
	// (no prompt path) by promoting to the Critical tier. Use for
	// audits, code review, or sandboxed inspection where the agent
	// should never mutate anything.
	RiskProfileReadonly RiskProfile = "readonly"

	// RiskProfileCautious — most operations prompt the user. Suitable
	// for sensitive workspaces or unfamiliar agents. Low-risk reads
	// auto-approve; everything else gets routed to a prompt.
	RiskProfileCautious RiskProfile = "cautious"

	// RiskProfileDefault — sane defaults matching the historical EA
	// cascade. Reads auto-approve, common edits/commits auto-approve,
	// destructive operations (force flags, rm -rf, lossy git) prompt.
	RiskProfileDefault RiskProfile = "default"

	// RiskProfilePermissive — high trust. Almost everything passes
	// without prompting; only truly destructive patterns route to a
	// prompt. Use when the agent is well-trusted and the workspace
	// is recoverable (clean checkout, throwaway dir).
	RiskProfilePermissive RiskProfile = "permissive"

	// RiskProfileUnrestricted — no risk cascade gating at all. Only
	// the Critical tier (rm -rf root, fork bombs) blocks. Use with
	// extreme care; intended for sandboxed / disposable environments.
	RiskProfileUnrestricted RiskProfile = "unrestricted"
)

// IsValidRiskProfile reports whether s names a known profile.
// User-defined profiles (added via Config.RiskProfiles) are NOT
// considered "valid" by this predicate — it only covers the baked-in
// names. Callers that need to accept user-defined profiles should
// check Config.RiskProfiles directly via ResolveRiskProfileRules.
func IsValidRiskProfile(s string) bool {
	switch RiskProfile(s) {
	case RiskProfileReadonly, RiskProfileCautious, RiskProfileDefault, RiskProfilePermissive, RiskProfileUnrestricted:
		return true
	}
	return false
}

// ResolveRiskProfileRules returns the AutoApproveRules that should
// apply for the given profile name, honoring user overrides in
// Config.RiskProfiles before falling back to the baked-in defaults.
//
// Resolution order:
//  1. cfg.RiskProfiles[name] — user override (replaces builtins
//     entirely; the user is the source of truth for any profile name
//     they list, including the five named built-ins).
//  2. AutoApproveRulesForProfile(name) — baked-in defaults for the
//     known profile names. Unknown names fall through to the Default
//     profile here.
//
// cfg may be nil (no config loaded); in that case the baked-in rules
// are always returned.
func ResolveRiskProfileRules(cfg *Config, profile RiskProfile) AutoApproveRules {
	if cfg != nil && len(cfg.RiskProfiles) > 0 {
		if custom, ok := cfg.RiskProfiles[string(profile)]; ok {
			return custom
		}
	}
	return AutoApproveRulesForProfile(profile)
}

// AutoApproveRulesForProfile returns the rules baked into each named
// profile. Unknown profiles fall back to RiskProfileDefault.
func AutoApproveRulesForProfile(profile RiskProfile) AutoApproveRules {
	switch profile {
	case RiskProfileReadonly:
		// Strictest profile: only reads permitted. Everything else
		// (writes, edits, shell, git, etc.) promotes to Critical so
		// even an interactive prompt cannot approve it. The Critical
		// tier is the only one with no prompt path — that's what
		// makes "readonly" actually readonly instead of "prompt for
		// every write".
		return AutoApproveRules{
			LowRiskOps: []string{
				"git_status", "git_log", "git_diff", "read_file",
			},
			MediumRiskOps: []string{},
			HighRiskNever: []string{},
			DefaultRisk:   RiskLevelCritical,
		}

	case RiskProfileCautious:
		// Only reads auto-approve. Everything else (including normal
		// edits and commits) hits the High → prompt path via
		// DefaultRisk = High.
		return AutoApproveRules{
			LowRiskOps: []string{
				"git_status", "git_log", "git_diff", "read_file",
			},
			MediumRiskOps: []string{},
			HighRiskNever: []string{
				"force_flag", "rm_recursive", "git_reset_hard",
				"git_clean", "docker_prune", "git_push_force",
				"git_checkout", "git_switch", "git_restore", "git_branch_delete",
			},
			DefaultRisk: RiskLevelHigh,
		}

	case RiskProfilePermissive:
		// Everything common is auto-approved; only force/recursive
		// destructive patterns route to a prompt. DefaultRisk = Low
		// covers anything not explicitly listed.
		return AutoApproveRules{
			LowRiskOps: []string{
				"git_add", "git_status", "git_log", "git_diff", "read_file",
				"git_commit", "git_push", "git_pull", "git_fetch",
				"write_file", "edit_file", "shell_command",
				"rm_command", "docker", "subagent_spawn", "cross_directory",
				"git_checkout", "git_switch",
			},
			MediumRiskOps: []string{},
			HighRiskNever: []string{
				"force_flag", "rm_recursive", "git_reset_hard",
				"git_push_force", "git_clean", "git_restore", "git_branch_delete",
			},
			DefaultRisk: RiskLevelLow,
		}

	case RiskProfileUnrestricted:
		// No gating beyond the Critical tier (handled separately by
		// IsCriticalOperation, not via these lists). Even
		// force_flag / rm_recursive route to Low — the deliberate
		// "I know what I'm doing" mode for sandboxed runs.
		return AutoApproveRules{
			LowRiskOps:    []string{},
			MediumRiskOps: []string{},
			HighRiskNever: []string{},
			DefaultRisk:   RiskLevelLow,
		}

	case RiskProfileDefault:
		fallthrough
	default:
		// Backward-compatible default. DefaultRisk = Medium matches
		// the historical behavior so existing personas with EA-style
		// rules continue to behave exactly as before.
		return AutoApproveRules{
			LowRiskOps: []string{
				"git_add", "git_status", "git_log", "git_diff",
				"read_file",
			},
			MediumRiskOps: []string{
				"git_commit", "git_push", "git_pull", "git_fetch",
				"write_file", "edit_file", "shell_command",
				"rm_command", "docker",
				"subagent_spawn", "cross_directory",
			},
			HighRiskNever: []string{
				"force_flag", "rm_recursive", "git_reset_hard",
				"git_clean", "docker_prune", "git_push_force",
				"git_checkout", "git_switch", "git_restore", "git_branch_delete",
			},
			DefaultRisk: RiskLevelMedium,
		}
	}
}

// IsCriticalOperation reports whether a command matches a pattern that
// is NEVER allowed regardless of profile, persona, or interactive
// approval. Reserved for operations that can permanently destroy the
// system or leave it in an unrecoverable state.
//
// This is the single source of truth for "critical" across every
// security gate: the static classifier (agent_tools.isCriticalSystemOperation
// delegates here) and the persona risk cascade (EvaluateOperationRisk)
// both consult it, so the two gates can't disagree on what's critical.
//
// Covers:
//   - rm -rf targeting root or the current dir (`/`, `/*`, `~`, `$HOME`, `.`, `*`)
//   - Classic fork-bomb pattern `:(){:|:&};:`
//   - Filesystem creation / raw disk overwrite (mkfs, dd to a block device)
//   - Mass process kills (killall -9 / -KILL)
//   - chmod 000 on a system root path
//   - Overwriting critical auth/system files (/etc/shadow, /etc/passwd, …)
//
// Tokenized matching matches invokesCommand semantics so a benign
// substring inside a path or argument can't trigger a false positive.
func IsCriticalOperation(command string) bool {
	cmdLower := strings.ToLower(strings.TrimSpace(command))

	// Fork bomb — the literal `:()` shell-function-named-colon is a
	// reliable signal. The token is unusual enough that false
	// positives are vanishingly rare.
	if strings.Contains(cmdLower, ":()") && strings.Contains(cmdLower, ":|:") {
		return true
	}

	fields := strings.Fields(cmdLower)

	// rm -rf <root-equivalent or cwd wildcard>. We need rm invoked as a
	// command, with a recursive flag, AND a destructive target.
	if invokesCommand(fields, "rm") {
		hasRecursive := false
		for _, f := range fields {
			if f == "-r" || f == "-R" || f == "--recursive" {
				hasRecursive = true
				break
			}
			if len(f) > 2 && f[0] == '-' && f[1] != '-' && strings.ContainsAny(f, "rR") {
				hasRecursive = true
				break
			}
		}
		if hasRecursive {
			for _, f := range fields {
				switch f {
				case "/", "/*", "~", "~/", "$home", "${home}", "${home}/", "$home/", ".", "*":
					return true
				}
			}
		}
	}

	// mkfs / mkfs.* — formatting a filesystem destroys everything on the target.
	if len(fields) > 0 && (fields[0] == "mkfs" || strings.HasPrefix(fields[0], "mkfs.")) {
		return true
	}

	// dd reading from / writing to a primary block device.
	if invokesCommand(fields, "dd") {
		for _, disk := range []string{"/dev/sda", "/dev/sdb", "/dev/nvme", "/dev/vda"} {
			if strings.Contains(cmdLower, "of="+disk) || strings.Contains(cmdLower, "if="+disk) {
				return true
			}
		}
	}

	// killall -9 / -KILL — mass process termination.
	if strings.HasPrefix(cmdLower, "killall -9") || strings.HasPrefix(cmdLower, "killall -kill") {
		return true
	}

	// chmod 000 on a system root path (locks everyone out).
	if strings.Contains(cmdLower, "chmod 000 /") {
		return true
	}

	// Overwriting critical auth/system files.
	for _, file := range []string{"/etc/shadow", "/etc/passwd", "/etc/sudoers", "/root/.ssh/authorized_keys"} {
		if strings.Contains(cmdLower, "> "+file) || strings.Contains(cmdLower, ">> "+file) || strings.Contains(cmdLower, "echo "+file) {
			return true
		}
	}

	return false
}

// SubagentType defines a specialized subagent persona with its own configuration
type SubagentType struct {
	ID                 string            `json:"id"`                             // Unique identifier (e.g., "coder", "tester", "debugger")
	Name               string            `json:"name"`                           // Human-readable name (e.g., "Coder", "Tester")
	Description        string            `json:"description"`                    // What this subagent specializes in
	Provider           string            `json:"provider"`                       // Provider for this subagent type (optional, falls back to SubagentProvider)
	Model              string            `json:"model"`                          // Model for this subagent type (optional, falls back to SubagentModel)
	SystemPrompt       string            `json:"system_prompt"`                  // Relative path to system prompt file (e.g., "subagent_prompts/coder.md")
	SystemPromptText   string            `json:"system_prompt_text,omitempty"`   // Optional inline system prompt text (replaces base prompt entirely)
	SystemPromptAppend string            `json:"system_prompt_append,omitempty"` // Optional inline text appended to the base or loaded system prompt (for composition)
	AllowedTools       []string          `json:"allowed_tools,omitempty"`        // Optional explicit tool allowlist for focused persona behavior
	Aliases            []string          `json:"aliases,omitempty"`              // Optional aliases (e.g., "web-scraper")
	Enabled            bool              `json:"enabled"`                        // Catalog-only: every shipped persona sets this true. Runtime "is this persona usable?" is determined by Config.DisabledPersonas (user) + LocalOnly (env). Kept for catalog hygiene + defense-in-depth in case a future variant ships with a deliberately-disabled entry.
	LocalOnly          bool              `json:"local_only,omitempty"`           // Only available in local mode (not cloud)
	Delegatable        bool              `json:"delegatable,omitempty"`          // Whether this persona can be used as a subagent (default: true for worker personas, false for orchestrator personas)
	AutoApproveRules   *AutoApproveRules `json:"auto_approve_rules,omitempty"`   // Risk cascade rules for the runtime auto-approve check
	// Capabilities is an explicit list of agency grants this persona holds
	// (e.g. "git_write"). Replaces sniffing AutoApproveRules to infer what a
	// persona is allowed to do. Use HasCapability to query.
	Capabilities []string `json:"capabilities,omitempty"`
	// CanSpawnNonDelegatable lists otherwise-undelegatable persona IDs that
	// this persona may spawn. Replaces the hardcoded EA-spawn-authority
	// special-case. The coordinator carries ["orchestrator"] to enable the
	// canonical coordinator→orchestrator→specialist chain.
	CanSpawnNonDelegatable []string `json:"can_spawn_non_delegatable,omitempty"`
}

// HasCapability reports whether the persona declares the given capability
// name. Comparison is case-insensitive and whitespace-tolerant.
func (st *SubagentType) HasCapability(name string) bool {
	if st == nil {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return false
	}
	for _, c := range st.Capabilities {
		if strings.ToLower(strings.TrimSpace(c)) == target {
			return true
		}
	}
	return false
}

// GetAutoApproveRules returns the auto-approve rules for this persona,
// falling back to defaults if none are configured.
// Callers MUST NOT modify the returned struct's slice fields,
// as they may share backing arrays with the original config.
func (st *SubagentType) GetAutoApproveRules() AutoApproveRules {
	if st.AutoApproveRules != nil {
		return *st.AutoApproveRules
	}
	return DefaultAutoApproveRules()
}

// EvaluateOperationRisk determines the risk level of a shell operation
// based on the persona's auto-approve rules.
// Returns RiskLevelCritical for absolute-block patterns, otherwise
// RiskLevelLow, RiskLevelMedium, or RiskLevelHigh per the rules.
func (st *SubagentType) EvaluateOperationRisk(command string) RiskLevel {
	// Critical patterns are ALWAYS blocked, regardless of persona
	// rules or profile. Checked first so no rule lookup can shadow
	// them.
	if IsCriticalOperation(command) {
		return RiskLevelCritical
	}

	rules := st.GetAutoApproveRules()

	cmdLower := strings.ToLower(command)

	// HighRiskNever patterns are gated. "force_flag" is one such
	// pattern that lives in the list for all gated profiles; the
	// Unrestricted profile has it removed so -f / --force passes
	// through. (Prior to SP-058 there was a hardcoded
	// containsForceFlag short-circuit before the loop; that's been
	// folded into the data-driven check so profiles can fully
	// control gating.)
	for _, pattern := range rules.HighRiskNever {
		if matchesRiskPattern(cmdLower, pattern) {
			return RiskLevelHigh
		}
	}

	// Determine the operation category for classification
	opCategory := categorizeCommand(cmdLower)

	// Check if the operation is explicitly in the low-risk list
	for _, pattern := range rules.LowRiskOps {
		if opCategory == pattern {
			return RiskLevelLow
		}
	}

	// Check if the operation is in the medium-risk list
	for _, pattern := range rules.MediumRiskOps {
		if opCategory == pattern {
			return RiskLevelMedium
		}
	}

	// Fall back to the profile's declared DefaultRisk for unknown
	// operations. Empty / unspecified default behaves as Medium for
	// backward compatibility with personas configured before SP-058.
	// DefaultRisk = Critical is legitimate for the readonly profile
	// (blocks all non-read ops outright).
	switch rules.DefaultRisk {
	case RiskLevelLow:
		return RiskLevelLow
	case RiskLevelHigh:
		return RiskLevelHigh
	case RiskLevelCritical:
		return RiskLevelCritical
	default:
		return RiskLevelMedium
	}
}

// containsForceFlag checks if a command string contains -f or --force flags.
// --force-with-lease is explicitly excluded as it is a safer alternative
// that verifies remote state before overwriting.
// For -f standalone, only treats it as force for commands that commonly use -f as a force flag:
// git, rm, mv, cp, and docker. This avoids false positives on commands like grep -f or tail -f.
func containsForceFlag(cmdLower string) bool {
	// Check for --force as an exact token, but NOT --force-with-lease
	for _, segment := range strings.Fields(cmdLower) {
		if segment == "--force" {
			return true
		}
	}

	// Get the first word (command name) to check if -f should be treated as force
	fields := strings.Fields(cmdLower)
	if len(fields) == 0 {
		return false
	}
	firstCmd := fields[0]

	// Check for -f as a standalone flag (not part of a word)
	// Only treat -f as force for commands that commonly use it as a force flag
	for idx, segment := range fields {
		if segment == "-f" {
			// Only -f for force-capable commands
			switch firstCmd {
			case "git":
				// For git, -f must appear AFTER the subcommand (not at position 1).
				// Position 1 is between git and the subcommand — that's a malformed
				// global flag position, not a force flag.
				// Exception: if -f is the last token (e.g. "git -f" with no subcommand),
				// treat it as force — bare git -f is unusual but should be flagged.
				if idx > 1 {
					return true
				}
				if idx == 1 && idx == len(fields)-1 {
					return true // "git -f" with nothing after — bare force flag
				}
				// idx == 1 and there are more tokens → malformed global flag, skip
			case "rm", "mv", "cp", "docker":
				return true
			}
		}
		// Handle combined short flags like -af, -rf (these are dangerous)
		// Only treat combined flags with 'f' as force for force-capable commands
		if len(segment) > 2 && segment[0] == '-' && segment[1] != '-' && strings.Contains(segment, "f") {
			switch firstCmd {
			case "git":
				// Same rule: for git, combined flags with 'f' at position 1 are
				// skipped if there's a subcommand after them.
				if idx == 1 && idx < len(fields)-1 {
					continue
				}
				// But not things like "diff" or "conf" that happen to contain f
				// Only flag combinations that include f
				isAllFlags := true
				for _, ch := range segment[1:] {
					if ch >= '0' && ch <= '9' {
						isAllFlags = false
						break
					}
				}
				if isAllFlags {
					return true
				}
			case "rm", "mv", "cp", "docker":
				// But not things like "diff" or "conf" that happen to contain f
				// Only flag combinations that include f
				isAllFlags := true
				for _, ch := range segment[1:] {
					if ch >= '0' && ch <= '9' {
						isAllFlags = false
						break
					}
				}
				if isAllFlags {
					return true
				}
			}
		}
	}
	return false
}

// categorizeCommand maps a command string to a risk-category identifier.
func categorizeCommand(cmdLower string) string {
	if strings.HasPrefix(cmdLower, "git ") {
		return categorizeGitCommand(cmdLower)
	}
	if strings.HasPrefix(cmdLower, "rm ") {
		return "rm_command"
	}
	if strings.HasPrefix(cmdLower, "docker ") {
		return "docker"
	}
	// Read-only file operations
	if strings.HasPrefix(cmdLower, "cat ") || strings.HasPrefix(cmdLower, "head ") ||
		strings.HasPrefix(cmdLower, "ls ") || strings.HasPrefix(cmdLower, "find ") ||
		strings.HasPrefix(cmdLower, "which ") || strings.HasPrefix(cmdLower, "file ") {
		return "read_file"
	}
	// Write operations
	if strings.HasPrefix(cmdLower, "write_file") || strings.HasPrefix(cmdLower, "edit_file") {
		return "write_file"
	}
	return "shell_command"
}

// categorizeGitCommand maps git subcommands to risk-category identifiers.
func categorizeGitCommand(cmdLower string) string {
	subcmd := firstFieldAfter(cmdLower, "git")
	switch subcmd {
	case "status":
		return "git_status"
	case "log":
		return "git_log"
	case "diff":
		return "git_diff"
	case "add":
		return "git_add"
	case "commit":
		return "git_commit"
	case "push":
		return "git_push"
	case "pull":
		return "git_pull"
	case "fetch":
		return "git_fetch"
	case "reset":
		return "git_reset_hard"
	case "clean":
		return "git_clean"
	case "branch":
		if strings.Contains(cmdLower, "-d") || strings.Contains(cmdLower, "--delete") {
			return "git_branch_delete" // Branch deletion is high risk
		}
		return "git_status" // Branch listing is low risk
	case "checkout":
		return "git_checkout" // Can discard changes
	case "switch":
		return "git_switch" // Can discard changes
	case "restore":
		return "git_restore" // Can discard changes
	case "stash":
		return "git_status" // Stash is relatively safe
	case "tag":
		return "git_add" // Tags are relatively safe
	case "merge", "rebase":
		return "git_commit" // Medium risk like commit
	default:
		return "shell_command" // Default to medium
	}
}

// matchesRiskPattern checks if a command matches a risk pattern identifier.
func matchesRiskPattern(cmdLower string, pattern string) bool {
	// Map pattern names to actual command matching. All patterns here
	// operate on tokenized fields rather than bare substring matches —
	// a path component like ".../platform &&" used to false-match
	// "rm " (the last two chars of "platform" + the space before "&&"),
	// and "-run" used to false-match "-r" — so a benign command like
	// `cd ~/.../platform && go test -run X` got classified as
	// high-risk rm_recursive. See the rm_recursive case below.
	fields := strings.Fields(cmdLower)
	hasToken := func(target string) bool {
		for _, f := range fields {
			if f == target {
				return true
			}
		}
		return false
	}
	switch pattern {
	case "force_flag":
		return containsForceFlag(cmdLower)
	case "rm_recursive":
		// Must actually invoke `rm` as a command — either at the very
		// start, after `sudo`, or after a `;` / `&&` / `||` / `|`
		// operator. A path component that happens to end in "rm"
		// (e.g. "platform") is NOT an invocation.
		if !invokesCommand(fields, "rm") {
			return false
		}
		// And a real recursive-mode flag must appear as its own token
		// or combined short flag (-r, -R, -rf, -fr, --recursive).
		for _, f := range fields {
			if f == "-r" || f == "-R" || f == "--recursive" {
				return true
			}
			// Combined short flag: -rf, -fr, -Rf, -fR (any order, any
			// length, must start with '-' and not be a long flag).
			if len(f) > 2 && f[0] == '-' && f[1] != '-' {
				hasR := strings.ContainsAny(f, "rR")
				hasF := strings.Contains(f, "f")
				if hasR && hasF {
					return true
				}
			}
		}
		return false
	case "git_reset_hard":
		return invokesGitSubcommand(fields, "reset") && hasToken("--hard")
	case "git_clean":
		return invokesGitSubcommand(fields, "clean")
	case "git_push_force":
		if !invokesGitSubcommand(fields, "push") {
			return false
		}
		// --force-with-lease is safer, don't match it
		for _, segment := range fields {
			if segment == "--force" || segment == "-f" {
				return true
			}
		}
		return false
	case "docker_prune":
		if !invokesCommand(fields, "docker") {
			return false
		}
		return hasToken("prune")
	case "git_checkout":
		return invokesGitSubcommand(fields, "checkout")
	case "git_switch":
		return invokesGitSubcommand(fields, "switch")
	case "git_restore":
		return invokesGitSubcommand(fields, "restore")
	case "git_branch_delete":
		if !invokesGitSubcommand(fields, "branch") {
			return false
		}
		return hasToken("-d") || hasToken("-D") || hasToken("--delete")
	default:
		return false
	}
}

// invokesCommand reports whether the tokenized command line actually
// invokes `name` as a command — i.e. as the first token, after `sudo`,
// or as the first token after a shell pipeline / chain operator
// (`;`, `&&`, `||`, `|`). This avoids substring matches inside paths,
// flag values, or arguments (the bug that made
// `cd .../platform && go test ... -run X` match `rm_recursive`).
func invokesCommand(fields []string, name string) bool {
	if len(fields) == 0 {
		return false
	}
	for i, f := range fields {
		if f != name {
			continue
		}
		if i == 0 {
			return true
		}
		// After a chain/pipe operator — first command in the next
		// segment. Walk backwards skipping `sudo`-like prefixes.
		prev := fields[i-1]
		switch prev {
		case ";", "&&", "||", "|":
			return true
		case "sudo":
			return true
		}
	}
	return false
}

// invokesGitSubcommand reports whether the tokenized command line
// invokes `git <subcmd>` — `git` must be invoked as a command (per
// invokesCommand) AND immediately followed by the subcommand token.
// Catches `cd /repo && git checkout main` but not `... grep 'git
// checkout' file` or path/argument substrings that happen to spell
// the same letters.
func invokesGitSubcommand(fields []string, subcmd string) bool {
	for i, f := range fields {
		if f != "git" {
			continue
		}
		if !invokesCommand(fields[i:i+1], "git") && !(i > 0 && isChainOperator(fields[i-1])) && i != 0 {
			continue
		}
		if i+1 < len(fields) && fields[i+1] == subcmd {
			return true
		}
	}
	return false
}

// isChainOperator reports whether a token is a shell pipeline / chain
// operator that ends one command and starts another.
func isChainOperator(tok string) bool {
	switch tok {
	case ";", "&&", "||", "|":
		return true
	}
	return false
}

// firstFieldAfter returns the first whitespace-delimited field after the given prefix.
func firstFieldAfter(s, prefix string) string {
	after := strings.TrimPrefix(s, prefix)
	after = strings.TrimSpace(after)
	fields := strings.Fields(after)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
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

// PersistentContextConfig configures persistent conversational context and memory retrieval.
type PersistentContextConfig struct {
	// ProactiveContextEnabled controls whether the system primes new sessions with
	// relevant past work via semantic retrieval from conversation history.
	// Default: true
	ProactiveContextEnabled bool `json:"proactiveContextEnabled,omitempty"`

	// MaxContextualResults is the maximum number of past turns to retrieve for context.
	// Default: 5
	MaxContextualResults int `json:"maxContextualResults,omitempty"`

	// MinRelevanceScore is the minimum time-decayed cosine similarity score for retrieval.
	// Range: 0.0 to 1.0. Results below this score are filtered out.
	// Default: 0.50
	MinRelevanceScore float64 `json:"minRelevanceScore,omitempty"`

	// MaxContextChars is the hard cap on total injected character count for context.
	// Default: 4000
	MaxContextChars int `json:"maxContextChars,omitempty"`

	// WorkspaceScopedRetrieval restricts retrieval to turns from the current workspace only.
	// When false (default), retrieval searches across all workspaces.
	// Default: false
	WorkspaceScopedRetrieval bool `json:"workspaceScopedRetrieval,omitempty"`

	// DriftDetectionEnabled controls whether conversational drift detection is active.
	// When enabled, the system checks if the conversation has drifted from its original intent.
	// Default: true
	DriftDetectionEnabled bool `json:"driftDetectionEnabled,omitempty"`

	// DriftThreshold is the cosine similarity threshold below which drift is flagged.
	// Range: 0.0 to 1.0. Lower values require more divergence before flagging.
	// Default: 0.60
	DriftThreshold float64 `json:"driftThreshold,omitempty"`

	// DriftCheckInterval is the number of turns between drift checks.
	// For example, 5 means drift is checked on turns 5, 10, 15, etc.
	// Default: 5
	DriftCheckInterval int `json:"driftCheckInterval,omitempty"`

	// RetentionDays controls how many days to keep persistent context entries.
	// Default: 0 (never expire). Set to a positive value to automatically clean
	// up entries older than the specified number of days at agent startup.
	RetentionDays int `json:"retentionDays,omitempty"`
}

// Resolve returns a copy of the config with default values filled in for any
// zero-value fields. Use this after loading from disk to ensure sensible defaults.
// If the receiver is nil, returns a fully-defaulted config.
func (c *PersistentContextConfig) Resolve() PersistentContextConfig {
	result := PersistentContextConfig{
		ProactiveContextEnabled:   true,
		MaxContextualResults:      5,
		MinRelevanceScore:         0.50,
		MaxContextChars:           4000,
		WorkspaceScopedRetrieval:  true,
		DriftDetectionEnabled:     true,
		DriftThreshold:            0.60,
		DriftCheckInterval:        5,
	}
	if c != nil {
		result.ProactiveContextEnabled = c.ProactiveContextEnabled
		if c.MaxContextualResults > 0 {
			result.MaxContextualResults = c.MaxContextualResults
		}
		if c.MinRelevanceScore > 0 {
			result.MinRelevanceScore = c.MinRelevanceScore
		}
		if c.MaxContextChars > 0 {
			result.MaxContextChars = c.MaxContextChars
		}
		result.WorkspaceScopedRetrieval = c.WorkspaceScopedRetrieval
		result.DriftDetectionEnabled = c.DriftDetectionEnabled
		if c.DriftThreshold > 0 {
			result.DriftThreshold = c.DriftThreshold
		}
		if c.DriftCheckInterval > 0 {
			result.DriftCheckInterval = c.DriftCheckInterval
		}
		if c.RetentionDays > 0 {
			result.RetentionDays = c.RetentionDays
		}
	}
	return result
}

// ChangeTrackingConfig gates and tunes the ChangeTracker's
// shell-mutation snapshot pass. Direct file-tool tracking (write_file,
// edit_file, patch_structured_file) is always on; this struct only
// touches the walker that runs before/after every shell_command.
type ChangeTrackingConfig struct {
	// ShellWalkEnabled controls whether the per-shell_command snapshot
	// walk runs at all. Disable for workspaces where the walk cost is
	// unacceptable (e.g., very-large monorepos with novel bloat
	// directories) or for users who don't care about recovering
	// shell-deleted untracked files. Direct file-tool tracking is
	// unaffected. Default: true.
	ShellWalkEnabled *bool `json:"shell_walk_enabled,omitempty"`

	// MaxFiles caps the number of files visited in a single walk.
	// Larger workspaces hit the cap and yield partial coverage with a
	// truncation log. Default: 50000.
	MaxFiles int `json:"max_files,omitempty"`

	// MaxTotalBytes caps cumulative content bytes captured per walk.
	// Files past this cap get path-only entries (still appear in
	// list_changes / FilesModified, but report recoverable=false).
	// Default: 32 MiB (33554432).
	MaxTotalBytes int64 `json:"max_total_bytes,omitempty"`

	// MaxDurationMs is the wall-clock budget for a single walk, in
	// milliseconds. Exceeding it aborts the walk with a partial-
	// coverage log. Default: 500 ms.
	MaxDurationMs int `json:"max_duration_ms,omitempty"`

	// AutoSkipFileCountThreshold is the per-directory immediate child
	// file count that triggers adaptive auto-skip. Default: 1500.
	AutoSkipFileCountThreshold int `json:"auto_skip_file_count_threshold,omitempty"`

	// RevisionRetention controls how the persistent revision store
	// (.sprout/revisions/ + .sprout/changes/) is compacted. Quantity-
	// based tiering: most recent N revisions are kept verbatim, next M
	// drop the conversation transcript, next K collapse to a one-line
	// summary, the rest are dropped. Position is by directory mtime,
	// so accessing an old revision via view_history / recover_file
	// promotes it back toward "hot" automatically.
	RevisionRetention *RevisionRetentionConfig `json:"revision_retention,omitempty"`
}

// RevisionRetentionConfig controls the quantity-based compaction of
// the persistent revision history. Two retained tiers plus a drop
// threshold:
//
//   - hot:   the most recent N revisions kept verbatim
//   - warm:  the next M revisions with conversation.json dropped
//   - drop:  anything older is removed entirely
//
// The ChangeTracker is a short-horizon stop-gap (recover from a bad
// sed -i, undo a hasty rm), not a long-term audit log — that's what
// git is for. Once a revision falls out of warm, the user has either
// committed the work or wasn't going to recover it anyway, and the
// disk space is better spent on hot data.
//
// See AGENTS.md "Change Tracking" for context.
type RevisionRetentionConfig struct {
	// HotCount: most recent N revisions kept verbatim (full
	// conversation.json + instructions + llm_response + all change
	// payloads). Fast view_history + full recovery. Default: 200.
	HotCount int `json:"hot_count,omitempty"`

	// WarmCount: next M revisions after the hot tier. conversation.json
	// dropped; instructions + response + change payloads kept. Recovery
	// still works; conversation context lost. Default: 500.
	WarmCount int `json:"warm_count,omitempty"`

	// MaxDirBytes is the long-stop cap on total revisions+changes
	// disk usage per workspace. If the count-based tiering still
	// leaves the directory over this size, trim oldest warm entries
	// until under cap. Default: 1 GiB (1073741824).
	MaxDirBytes int64 `json:"max_dir_bytes,omitempty"`

	// ArchiveFrozen: if true, dropped revisions are moved to
	// .sprout/revisions/_frozen/ instead of being deleted outright.
	// Opt-in safety net for users who want a recoverable record of
	// long-tail history at the cost of unbounded growth. Default: false.
	ArchiveFrozen bool `json:"archive_frozen,omitempty"`
}

// Resolve fills in defaults for any zero-value fields and returns a
// fully-populated config. Safe to call on nil — yields all-defaults.
func (c *ChangeTrackingConfig) Resolve() ChangeTrackingConfig {
	result := ChangeTrackingConfig{
		MaxFiles:                   50000,
		MaxTotalBytes:              32 * 1024 * 1024,
		MaxDurationMs:              500,
		AutoSkipFileCountThreshold: 1500,
	}
	enabled := true
	result.ShellWalkEnabled = &enabled
	if c == nil {
		return result
	}
	if c.ShellWalkEnabled != nil {
		flag := *c.ShellWalkEnabled
		result.ShellWalkEnabled = &flag
	}
	if c.MaxFiles > 0 {
		result.MaxFiles = c.MaxFiles
	}
	if c.MaxTotalBytes > 0 {
		result.MaxTotalBytes = c.MaxTotalBytes
	}
	if c.MaxDurationMs > 0 {
		result.MaxDurationMs = c.MaxDurationMs
	}
	if c.AutoSkipFileCountThreshold > 0 {
		result.AutoSkipFileCountThreshold = c.AutoSkipFileCountThreshold
	}
	if c.RevisionRetention != nil {
		resolved := c.RevisionRetention.Resolve()
		result.RevisionRetention = &resolved
	}
	return result
}

// Resolve fills in defaults for any zero-value fields and returns a
// fully-populated retention config. Safe to call on nil — yields
// all-defaults.
func (c *RevisionRetentionConfig) Resolve() RevisionRetentionConfig {
	result := RevisionRetentionConfig{
		HotCount:    200,
		WarmCount:   500,
		MaxDirBytes: 1024 * 1024 * 1024, // 1 GiB
	}
	if c == nil {
		return result
	}
	if c.HotCount > 0 {
		result.HotCount = c.HotCount
	}
	if c.WarmCount > 0 {
		result.WarmCount = c.WarmCount
	}
	if c.MaxDirBytes > 0 {
		result.MaxDirBytes = c.MaxDirBytes
	}
	result.ArchiveFrozen = c.ArchiveFrozen
	return result
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
		AllowOrchestratorGitWrite:   true, // SP-050: orchestrator gets git-write by default; flip to false to require the git tool for write ops
		SubagentTypes:               defaultSubagentTypes(),
		Skills:                      defaultSkills(),
		PDFOCREnabled:               true,
		PDFOCRProvider:              "ollama",
		PDFOCRModel:                 "glm-ocr",
		SubagentMaxParallel:         2,    // Default max parallel subagents
		SubagentParallelEnabled:     func() *bool { t := true; return &t }(), // Default to enabling parallel subagents
		EmbeddingIndex: &EmbeddingIndexConfig{
			Enabled:             false,
			AutoIndex:           false,
			SimilarityThreshold: 0.90,
			MaxResults:          3,
		},
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

	// SP-058: RiskProfile is a single-value selector; non-empty
	// override wins. RiskProfiles is a map of named overrides; we
	// merge per-key so a workspace can override just one profile
	// without wiping out user-defined profiles from the global
	// config.
	if override.RiskProfile != "" {
		result.RiskProfile = override.RiskProfile
	}
	if len(override.RiskProfiles) > 0 {
		if result.RiskProfiles == nil {
			result.RiskProfiles = make(map[string]AutoApproveRules, len(override.RiskProfiles))
		}
		for k, v := range override.RiskProfiles {
			result.RiskProfiles[k] = v
		}
	}
	// ApprovedShellCommands: union the two lists (override entries are
	// additive to base). De-dupe so a workspace config that re-lists a
	// command already in the global config doesn't grow the file.
	if len(override.ApprovedShellCommands) > 0 {
		seen := make(map[string]struct{}, len(result.ApprovedShellCommands)+len(override.ApprovedShellCommands))
		merged := make([]string, 0, len(result.ApprovedShellCommands)+len(override.ApprovedShellCommands))
		for _, cmd := range result.ApprovedShellCommands {
			if _, ok := seen[cmd]; ok {
				continue
			}
			seen[cmd] = struct{}{}
			merged = append(merged, cmd)
		}
		for _, cmd := range override.ApprovedShellCommands {
			if _, ok := seen[cmd]; ok {
				continue
			}
			seen[cmd] = struct{}{}
			merged = append(merged, cmd)
		}
		result.ApprovedShellCommands = merged
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
			if v.Metadata == nil {
				v.Metadata = make(map[string]string)
			}
			if _, has := v.Metadata["source"]; !has {
				v.Metadata["source"] = "user"
			}
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
	// SubagentTypes is intentionally tagged json:"-" (personas are catalog-fixed
	// and not persisted to disk). The JSON roundtrip strips it, so we copy it
	// directly from the source — preserving any in-memory mutations (e.g. test
	// fixtures that inject custom personas, workflow-automation overrides).
	// If the source map is empty, fall back to the catalog defaults so callers
	// never see a nil/empty SubagentTypes from a freshly loaded config.
	if len(cfg.SubagentTypes) > 0 {
		out.SubagentTypes = make(map[string]SubagentType, len(cfg.SubagentTypes))
		for id, st := range cfg.SubagentTypes {
			copied := st
			copied.AllowedTools = append([]string{}, st.AllowedTools...)
			copied.Aliases = append([]string{}, st.Aliases...)
			copied.Capabilities = append([]string{}, st.Capabilities...)
			copied.CanSpawnNonDelegatable = append([]string{}, st.CanSpawnNonDelegatable...)
			if st.AutoApproveRules != nil {
				rules := *st.AutoApproveRules
				rules.LowRiskOps = append([]string{}, rules.LowRiskOps...)
				rules.MediumRiskOps = append([]string{}, rules.MediumRiskOps...)
				rules.HighRiskNever = append([]string{}, rules.HighRiskNever...)
				copied.AutoApproveRules = &rules
			}
			out.SubagentTypes[id] = copied
		}
	} else {
		out.SubagentTypes = defaultSubagentTypes()
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

	// Snapshot the file's stat for SP-034-4b conflict detection. Stat
	// AFTER the read so a concurrent writer that landed in between sees
	// us as having the older view (we'll detect the conflict on next
	// Save). Using ModTime + Size — both must match on save or we treat
	// it as a divergence.
	loadStat, statErr := os.Stat(configPath)
	var loadedMod time.Time
	var loadedSize int64
	if statErr == nil {
		loadedMod = loadStat.ModTime()
		loadedSize = loadStat.Size()
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
	// Personas are catalog-fixed and never loaded from user config — hydrate
	// the in-memory map fresh from the embedded catalog every time so any
	// stale `subagent_types` data from a pre-removal config gets discarded.
	config.SubagentTypes = defaultSubagentTypes()
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	// Merge missing default skills so that skills added to embedded defaults
	// after the user's config was already at v2.0 are still available.
	mergeMissingDefaultSkills(&config)

	// Post-unmarshal operations that truly need struct-level access
	fileCustomProviders, err := MigrateLegacyCustomProviders(&config)
	if err != nil {
		return nil, fmt.Errorf("get config path: %w", err)
	}
	config.CustomProviders = fileCustomProviders

	if err := MigrateEmbeddedAPIKeys(config.CustomProviders); err != nil {
		log.Printf("[config] warning: credential migration failed: %v", err)
	}

	// Stamp the on-disk metadata BEFORE the discovery passes below so
	// that conflict detection only ever compares the canonical file
	// (skill discovery adds in-memory entries that aren't persisted).
	config.loadedModTime = loadedMod
	config.loadedSize = loadedSize

	// Discover user-level skills from ~/.config/sprout/skills/
	if os.Getenv("SPROUT_NO_USER_SKILLS") != "1" {
		if discovered := discoverUserSkills(&config); len(discovered) > 0 {
			log.Printf("[skills] Discovered %d user skill(s): %s",
				len(discovered), strings.Join(discovered, ", "))
		}
	}

	// Discover project-specific skills from .sprout/skills/
	if os.Getenv("SPROUT_NO_PROJECT_SKILLS") != "1" {
		if discovered := discoverProjectSkills(&config); len(discovered) > 0 {
			log.Printf("[skills] Discovered %d project-local skill(s): %s",
				len(discovered), strings.Join(discovered, ", "))
		}
	}

	// Self-heal a config that was poisoned by a leaky test run (e.g. a
	// past version of the codebase that wrote "test" to disk before
	// the Save-time guard existed). On the next CLI start the value
	// gets cleared and the user is prompted normally instead of
	// /commit silently routing to a no-op mock.
	sanitizeTestProvider(&config)

	return &config, nil
}

// Save saves the configuration to file
func (c *Config) Save() error {
	// Defense-in-depth: never persist "test" as the active provider.
	// The TestClientType sentinel is for in-process test fixtures only;
	// if it ever reaches disk, the next CLI run picks it up and tries
	// to route requests (including /commit) to a no-op mock. Strip it
	// here so even tests that bypass Manager.SetProvider can't poison
	// the real config.
	sanitizeTestProvider(c)

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

	// SP-034-4b: detect concurrent writers. Only enforce the check when
	// this Config was actually loaded from a file (loadedModTime set);
	// fresh-from-NewConfig() saves bypass the check by design — they're
	// either the first ever save, or an explicit "reset to defaults"
	// that should overwrite whatever's there.
	if !c.loadedModTime.IsZero() {
		if stat, statErr := os.Stat(configPath); statErr == nil {
			if !stat.ModTime().Equal(c.loadedModTime) || stat.Size() != c.loadedSize {
				return &ConfigConflictError{
					Path:           configPath,
					LoadedModTime:  c.loadedModTime,
					LoadedSize:     c.loadedSize,
					CurrentModTime: stat.ModTime(),
					CurrentSize:    stat.Size(),
				}
			}
		}
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

	// Refresh the loaded-stat snapshot so the NEXT Save's conflict
	// check compares against the file we just wrote, not the stale
	// pre-write state. Failure to re-stat is non-fatal — it just means
	// the next save's conflict check might false-positive once.
	if stat, statErr := os.Stat(configPath); statErr == nil {
		c.loadedModTime = stat.ModTime()
		c.loadedSize = stat.Size()
	}

	return nil
}

// SaveToDir saves the configuration to a specific directory, bypassing
// GetConfigPath() (which reads the SPROUT_CONFIG/LEDIT_CONFIG env vars).
// Use this when a Manager has an explicit configDir so that saves go to
// the correct location even after the env var has been restored.
func (c *Config) SaveToDir(dir string) error {
	// Same defense as Save() — refuse to persist the test sentinel even
	// when callers bypass GetConfigPath() and target an explicit dir.
	// See sanitizeTestProvider for context.
	sanitizeTestProvider(c)

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

// IsPersonaDisabled reports whether the given persona ID has been disabled
// by the user (via /persona <id> disable). The canonical ID after alias
// resolution is matched; a disabled persona is returned as nil by
// GetSubagentType and filtered from GetAvailablePersonaIDs.
func (c *Config) IsPersonaDisabled(id string) bool {
	if c == nil {
		return false
	}
	needle := normalizePersonaID(id)
	for _, disabled := range c.DisabledPersonas {
		if normalizePersonaID(disabled) == needle {
			return true
		}
	}
	return false
}

// SetPersonaDisabled adds or removes a persona ID from DisabledPersonas.
// Idempotent; aliases are normalized to the canonical form before storage.
func (c *Config) SetPersonaDisabled(id string, disabled bool) {
	if c == nil {
		return
	}
	needle := normalizePersonaID(id)
	filtered := c.DisabledPersonas[:0:0]
	already := false
	for _, existing := range c.DisabledPersonas {
		if normalizePersonaID(existing) == needle {
			already = true
			if !disabled {
				continue
			}
		}
		filtered = append(filtered, existing)
	}
	if disabled && !already {
		filtered = append(filtered, needle)
	}
	c.DisabledPersonas = filtered
}

// GetSubagentType retrieves a subagent type configuration by ID or alias.
// Personas are catalog-fixed (loaded from pkg/personas/configs/*.json at
// startup) — there is no user-override merge path. Returns nil if the
// persona does not exist or has been disabled via Config.DisabledPersonas.
func (c *Config) GetSubagentType(id string) *SubagentType {
	if c == nil || c.SubagentTypes == nil {
		return nil
	}

	normalizedID := normalizePersonaID(id)
	if normalizedID == "" {
		return nil
	}

	// Resolve the request to a canonical map entry by ID, ID-field, or alias.
	var found *SubagentType
	var canonicalID string
	for personaID, subagentType := range c.SubagentTypes {
		st := subagentType
		switch {
		case normalizePersonaID(personaID) == normalizedID,
			normalizePersonaID(st.ID) == normalizedID:
			found = &st
			canonicalID = normalizePersonaID(st.ID)
		default:
			for _, alias := range st.Aliases {
				if normalizePersonaID(alias) == normalizedID {
					found = &st
					canonicalID = normalizePersonaID(st.ID)
					break
				}
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		return nil
	}

	if c.IsPersonaDisabled(canonicalID) {
		return nil
	}

	// Deep copy slices so callers can't mutate the catalog-backed entry.
	result := *found
	result.AllowedTools = append([]string{}, found.AllowedTools...)
	result.Aliases = append([]string{}, found.Aliases...)
	result.Capabilities = append([]string{}, found.Capabilities...)
	result.CanSpawnNonDelegatable = append([]string{}, found.CanSpawnNonDelegatable...)
	if found.AutoApproveRules != nil {
		rules := *found.AutoApproveRules
		rules.LowRiskOps = append([]string{}, rules.LowRiskOps...)
		rules.MediumRiskOps = append([]string{}, rules.MediumRiskOps...)
		rules.HighRiskNever = append([]string{}, rules.HighRiskNever...)
		result.AutoApproveRules = &rules
	}
	return &result
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
		autoApprove := convertAutoApproveRules(definition.AutoApproveRules)
		types[normalizePersonaID(id)] = SubagentType{
			ID:                     normalizePersonaID(definition.ID),
			Name:                   definition.Name,
			Description:            definition.Description,
			Provider:               definition.Provider,
			Model:                  definition.Model,
			SystemPrompt:           definition.SystemPrompt,
			SystemPromptText:       definition.SystemPromptText,
			SystemPromptAppend:     definition.SystemPromptAppend,
			AllowedTools:           append([]string{}, definition.AllowedTools...),
			Aliases:                append([]string{}, definition.Aliases...),
			Enabled:                definition.Enabled,
			LocalOnly:              definition.LocalOnly,
			Delegatable:            definition.Delegatable,
			AutoApproveRules:       autoApprove,
			Capabilities:           append([]string{}, definition.Capabilities...),
			CanSpawnNonDelegatable: append([]string{}, definition.CanSpawnNonDelegatable...),
		}
	}

	return types
}

// convertAutoApproveRules converts the persona catalog's AutoApproveRules to
// the configuration package's AutoApproveRules type, returning nil if the
// source is nil.
func convertAutoApproveRules(src *personas.AutoApproveRules) *AutoApproveRules {
	if src == nil {
		return nil
	}
	return &AutoApproveRules{
		LowRiskOps:    append([]string{}, src.LowRiskOps...),
		MediumRiskOps: append([]string{}, src.MediumRiskOps...),
		HighRiskNever: append([]string{}, src.HighRiskNever...),
	}
}

// defaultSkills derives the built-in skill registry from the embedded
// pkg/skills library. Adding a new skill is a one-step process:
// create pkg/skills/library/<id>/SKILL.md with valid frontmatter and
// the registry, the `list_skills` tool output, and the `sprout skill
// list` CLI all pick it up automatically.
//
// repo-onboarding is preserved as a legacy alias for project-planning
// because user configs and prompts from older versions reference the
// old ID; the two point at the same content.
func defaultSkills() map[string]Skill {
	builtins := skills.Builtins()
	out := make(map[string]Skill, len(builtins)+1)
	for id, b := range builtins {
		out[id] = Skill{
			ID:          b.ID,
			Name:        b.Name,
			Description: b.Description,
			Path:        b.Path,
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		}
	}
	if pp, ok := out["project-planning"]; ok {
		alias := pp
		alias.ID = "repo-onboarding"
		alias.Metadata = map[string]string{"version": "2.0"}
		out["repo-onboarding"] = alias
	}
	return out
}

func mergeMissingDefaultSkills(config *Config) {
	if config == nil {
		return
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	for id, skill := range defaultSkills() {
		if existing, exists := config.Skills[id]; exists {
			// Update built-in skills in-place so description/path/metadata
			// stay current across versions.  Preserve user-set Enabled flag.
			wasEnabled := existing.Enabled
			skill.Metadata["source"] = "builtin"
			skill.Enabled = wasEnabled
			config.Skills[id] = skill
		} else {
			if skill.Metadata == nil {
				skill.Metadata = make(map[string]string)
			}
			skill.Metadata["source"] = "builtin"
			config.Skills[id] = skill
		}
	}

	// Prune stale built-in skills that were registered by a prior version
	// of defaultSkills() but are no longer present.  Accept both the
	// current builtin prefix and the pre-refactor location so legacy
	// configs migrate cleanly — entries with the old path get either
	// updated in-place above (still in defaults) or pruned here (no
	// longer in defaults).
	defaults := defaultSkills()
	for id, skill := range config.Skills {
		path := skill.Path
		if path == "" {
			continue
		}
		isBuiltin := strings.HasPrefix(path, skills.LogicalPath+"/") ||
			strings.HasPrefix(path, skills.LegacyLogicalPath+"/")
		if !isBuiltin {
			continue
		}
		if _, stillDefault := defaults[id]; !stillDefault {
			delete(config.Skills, id)
		}
	}
}

// discoverUserSkills scans the ~/.config/sprout/skills/ directory for user-level skills
// and adds them to the config. This allows users to create custom skills that are
// available across all projects. User skills are discovered before project skills,
// so project skills can override them.
func discoverUserSkills(config *Config) []string {
	var discovered []string
	if config == nil {
		return discovered
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	// Get user config directory
	configDir, err := GetConfigDir()
	if err != nil {
		return discovered
	}

	// Check for skills directory
	skillsDir := filepath.Join(configDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return discovered // No user skills directory, that's fine
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
			description = fmt.Sprintf("User-level skill: %s", skillID)
		}

		// Add to config (don't override if already exists)
		if _, exists := config.Skills[skillID]; !exists {
			config.Skills[skillID] = Skill{
				ID:          skillID,
				Name:        name,
				Description: description,
				Path:        filepath.Join("skills", skillID),
				Enabled:     true,
				Metadata:    map[string]string{"source": "user"},
			}
			discovered = append(discovered, name)
		}
	}

	return discovered
}

// discoverProjectSkills scans the .sprout/skills/ directory for project-specific skills
// and adds them to the config. This allows users to create custom skills without
// modifying the global config.
func discoverProjectSkills(config *Config) []string {
	var discovered []string
	if config == nil {
		return discovered
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return discovered
	}

	// Check for .sprout/skills directory
	skillsDir := filepath.Join(cwd, ".sprout", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return discovered // No project skills directory, that's fine
	}

	// Read the allowed_skills allowlist (if present)
	allowed := ReadAllowedSkills(cwd)

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
			// If an allowlist exists and the skill is not listed, load it disabled.
			enabled := true
			if allowed != nil && !allowed[skillID] {
				enabled = false
			}
			config.Skills[skillID] = Skill{
				ID:          skillID,
				Name:        name,
				Description: description,
				Path:        filepath.Join(".sprout", "skills", skillID),
				Enabled:     enabled,
				Metadata:    map[string]string{"source": "project"},
			}
			discovered = append(discovered, name)
		}
	}

	return discovered
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

// GetSubagentMaxDepth returns the maximum subagent nesting depth.
// Defaults to 2 if not configured or set to 0.
func (c *Config) GetSubagentMaxDepth() int {
	if c.SubagentMaxDepth > 0 {
		return c.SubagentMaxDepth
	}
	return 2 // Default
}

// GetEAMode returns the EA startup mode. Defaults to "interactive".
func (c *Config) GetEAMode() string {
	if c == nil || c.EAMode == "" {
		return "interactive"
	}
	return c.EAMode
}
