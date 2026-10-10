package configuration

import (
	"path/filepath"
)

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

// ComputerUseConfig gates the computer_user persona's desktop-control tools. Off by default.
type ComputerUseConfig struct {
	// Enabled is the master switch. When false the computer_user tools are never registered.
	Enabled bool `json:"enabled,omitempty"`

	// MaxActionsPerMinute caps the action rate. Default: 60. Set to 0 to disable.
	MaxActionsPerMinute int `json:"max_actions_per_minute,omitempty"`

	// AuditLogDir is the directory for computer_use per-session JSONL action
	// logs (default: ~/.config/sprout/computer_use_log when empty), and also
	// redirects the agent's security audit log (shell-audit.jsonl). When empty,
	// the security audit log lives under the state dir, which is where
	// `sprout audit tail` reads it.
	AuditLogDir string `json:"audit_log_dir,omitempty"`

	// WorkspaceAllowlist lists workspace roots where computer use is auto-approved.
	WorkspaceAllowlist []string `json:"workspace_allowlist,omitempty"`

	// PanicKeyChord is the key chord that triggers the panic key. Defaults to "ctrl+shift+escape".
	PanicKeyChord string `json:"panic_key_chord,omitempty"`

	// DestructiveAppGate controls whether the destructive-app denylist gate is active. Default: true.
	DestructiveAppGate bool `json:"destructive_app_gate,omitempty"`

	// OverrideFilePath is an optional override of the denylist override file location.
	OverrideFilePath string `json:"denylist_override_file,omitempty"`
}

// Resolve returns a copy with defaults filled in for zero-value fields.
func (c *ComputerUseConfig) Resolve() ComputerUseConfig {
	result := ComputerUseConfig{
		MaxActionsPerMinute: 60,
		PanicKeyChord:       "ctrl+shift+escape",
		DestructiveAppGate:  true,
	}
	if c != nil {
		result.Enabled = c.Enabled
		if c.MaxActionsPerMinute != 0 {
			result.MaxActionsPerMinute = c.MaxActionsPerMinute
		}
		result.AuditLogDir = c.AuditLogDir
		result.WorkspaceAllowlist = append([]string{}, c.WorkspaceAllowlist...)
		if c.PanicKeyChord != "" {
			result.PanicKeyChord = c.PanicKeyChord
		}
		result.DestructiveAppGate = true
		result.OverrideFilePath = c.OverrideFilePath
	}
	return result
}

// clampInt returns v clamped to [lo, hi]. Used by *Config.Resolve() helpers.
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// VisionConfig controls vision-pipeline runtime: parallel workers, concurrency cap, and batching.
type VisionConfig struct {
	// ParallelWorkers caps concurrent in-flight vision requests per session. Default: 3. Range: 1..32.
	ParallelWorkers int `json:"parallel_workers,omitempty"`

	// MaxParallelRequests caps the global number of in-flight vision
	// API calls across the entire process. This is independent of
	// ParallelWorkers (which is per-session). Default: 8.
	MaxParallelRequests int `json:"max_parallel_requests,omitempty"`

	// EnableBatchProcessing toggles the multi-image batching layer. Default: true.
	EnableBatchProcessing bool `json:"enable_batch_processing,omitempty"`

	// MaxBatchSize is the maximum number of images sent in a single batched call. Default: 4. Range: 1..8.
	MaxBatchSize int `json:"max_batch_size,omitempty"`
}

// Resolve returns a copy with defaults filled in for zero-value fields.
func (c *VisionConfig) Resolve() VisionConfig {
	result := VisionConfig{
		ParallelWorkers:       3,
		MaxParallelRequests:   8,
		EnableBatchProcessing: true,
		MaxBatchSize:          4,
	}
	if c == nil {
		return result
	}
	if c.ParallelWorkers > 0 {
		result.ParallelWorkers = clampInt(c.ParallelWorkers, 1, 32)
	}
	if c.MaxParallelRequests > 0 {
		result.MaxParallelRequests = clampInt(c.MaxParallelRequests, 1, 64)
	}
	if c.MaxBatchSize > 0 {
		result.MaxBatchSize = clampInt(c.MaxBatchSize, 1, 8)
	}
	result.EnableBatchProcessing = c.EnableBatchProcessing
	return result
}

// GetVisionConfig returns the raw VisionConfig from the on-disk config file.
func GetVisionConfig() VisionConfig {
	cfg, err := Load()
	if err != nil || cfg == nil || cfg.Vision == nil {
		return VisionConfig{}
	}
	return *cfg.Vision
}

// NotificationsConfig controls how the agent notifies the user when long-running turns complete.
type NotificationsConfig struct {
	// CLIBell emits a terminal bell (\a) on completion.
	CLIBell bool `json:"cli_bell,omitempty"`
	// OSNotify fires an OS-level desktop notification on completion.
	OSNotify bool `json:"os_notify,omitempty"`
	// Browser fires a browser notification (used by WebUI).
	Browser bool `json:"browser,omitempty"`
	// MinSeconds is the minimum turn duration before a notification is sent. Default: 10.
	MinSeconds float64 `json:"min_seconds,omitempty"`
}

// Resolve returns a copy with defaults filled in for zero-value fields.
func (c *NotificationsConfig) Resolve() NotificationsConfig {
	result := NotificationsConfig{
		MinSeconds: 10,
	}
	if c != nil {
		result.CLIBell = c.CLIBell
		result.OSNotify = c.OSNotify
		result.Browser = c.Browser
		if c.MinSeconds != 0 {
			result.MinSeconds = c.MinSeconds
		}
	}
	return result
}

// EditApprovalConfig controls the per-hunk diff approval gate.
type EditApprovalConfig struct {
	Mode  string   `json:"mode,omitempty"`
	Paths []string `json:"paths,omitempty"`

	// ShellCommand enables per-part shell approval prompts (SP-093-2).
	// When true, a multi-part shell command is split and each part is
	// approved individually via Agent.RequestShellApproval. Default: false,
	// which preserves the existing 4-option prompt for the whole command.
	ShellCommand bool `json:"shell_command,omitempty" yaml:"shell_command,omitempty"`
}

func (c *EditApprovalConfig) Resolve() EditApprovalConfig {
	result := EditApprovalConfig{Mode: "off"}
	if c != nil {
		result.Mode = c.Mode
		result.Paths = c.Paths
		result.ShellCommand = c.ShellCommand
	}
	if result.Mode == "" {
		result.Mode = "off"
	}
	return result
}

func (c *EditApprovalConfig) ShouldGate(path string) bool {
	r := c.Resolve()
	switch r.Mode {
	case "off", "":
		return false
	case "all":
		return true
	case "paths":
		for _, p := range r.Paths {
			if m, err := filepath.Match(p, path); err == nil && m {
				return true
			}
			if m, err := filepath.Match(p, filepath.Base(path)); err == nil && m {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// ChangeTrackingConfig gates and tunes the ChangeTracker.
type ChangeTrackingConfig struct {
	// Enabled controls whether the change tracking subsystem is active. Default: true.
	Enabled *bool `json:"enabled,omitempty"`

	// ShellWalkEnabled controls whether the per-shell_command snapshot walk runs. Default: true.
	ShellWalkEnabled *bool `json:"shell_walk_enabled,omitempty"`

	// MaxFiles caps the number of files visited in a single walk. Default: 50000.
	MaxFiles int `json:"max_files,omitempty"`

	// MaxTotalBytes caps cumulative content bytes captured per walk. Default: 32 MiB.
	MaxTotalBytes int64 `json:"max_total_bytes,omitempty"`

	// MaxDurationMs is the wall-clock budget for a single walk. Default: 500ms.
	MaxDurationMs int `json:"max_duration_ms,omitempty"`

	// AutoSkipFileCountThreshold is the per-directory child count that triggers auto-skip. Default: 1500.
	AutoSkipFileCountThreshold int `json:"auto_skip_file_count_threshold,omitempty"`

	// RevisionRetention controls how the persistent revision store is compacted.
	RevisionRetention *RevisionRetentionConfig `json:"revision_retention,omitempty"`
}

// RevisionRetentionConfig controls the quantity-based compaction of the persistent revision history.
type RevisionRetentionConfig struct {
	// HotCount: most recent N revisions kept verbatim. Default: 200.
	HotCount int `json:"hot_count,omitempty"`

	// WarmCount: next M revisions with conversation.json dropped. Default: 500.
	WarmCount int `json:"warm_count,omitempty"`

	// MaxDirBytes is the cap on total revisions+changes disk usage. Default: 1 GiB.
	MaxDirBytes int64 `json:"max_dir_bytes,omitempty"`

	// ArchiveFrozen: if true, dropped revisions are moved to _frozen/ instead of deleted. Default: false.
	ArchiveFrozen bool `json:"archive_frozen,omitempty"`

	// MaxChangesPerRevision caps the number of change records kept per revision. Default: 10000.
	MaxChangesPerRevision int `json:"max_changes_per_revision,omitempty"`

	// MaxChangesAgeDays drops change records older than this many days. Default: 30. Negative to disable.
	MaxChangesAgeDays int `json:"max_changes_age_days,omitempty"`
}

// Resolve fills in defaults for zero-value fields. Safe to call on nil.
func (c *ChangeTrackingConfig) Resolve() ChangeTrackingConfig {
	result := ChangeTrackingConfig{
		MaxFiles:                   50000,
		MaxTotalBytes:              32 * 1024 * 1024,
		MaxDurationMs:              500,
		AutoSkipFileCountThreshold: 1500,
	}
	// Change tracking is enabled by default.
	enabledDefault := true
	result.Enabled = &enabledDefault
	result.ShellWalkEnabled = &enabledDefault
	if c == nil {
		return result
	}
	if c.Enabled != nil {
		result.Enabled = c.Enabled
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

// Resolve fills in defaults for zero-value fields. Safe to call on nil.
func (c *RevisionRetentionConfig) Resolve() RevisionRetentionConfig {
	result := RevisionRetentionConfig{
		HotCount:              200,
		WarmCount:             500,
		MaxDirBytes:           1024 * 1024 * 1024, // 1 GiB
		MaxChangesPerRevision: 10000,
		MaxChangesAgeDays:     30,
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
	if c.MaxChangesPerRevision > 0 {
		result.MaxChangesPerRevision = c.MaxChangesPerRevision
	}
	if c.MaxChangesAgeDays != 0 {
		// Negative disables; positive overrides; zero = use default.
		result.MaxChangesAgeDays = c.MaxChangesAgeDays
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
