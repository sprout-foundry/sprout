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

// ComputerUseConfig gates the computer_user persona's desktop-control tools
// (SP-063). The feature is categorically more dangerous than file edits — a
// click can send an email or empty the trash — so it is off by default and
// every field is a safety lever.
type ComputerUseConfig struct {
	// Enabled is the master switch. When false (default) the computer_user
	// persona's mouse/keyboard/screenshot tools are never registered or
	// executed. Turning it on is a deliberate, one-time user choice.
	Enabled bool `json:"enabled,omitempty"`

	// MaxActionsPerMinute caps the action rate as a runaway-loop backstop.
	// Default: 60. Set to 0 to disable the cap (not recommended).
	MaxActionsPerMinute int `json:"max_actions_per_minute,omitempty"`

	// AuditLogDir is where per-session JSONL action logs are written.
	// Default: ~/.config/sprout/computer_use_log when empty.
	AuditLogDir string `json:"audit_log_dir,omitempty"`

	// WorkspaceAllowlist lists workspace roots where computer use is
	// auto-approved for the session without the per-session opt-in prompt.
	WorkspaceAllowlist []string `json:"workspace_allowlist,omitempty"`

	// PanicKeyChord is the key chord that triggers the panic key. Defaults
	// to "ctrl+shift+escape". Set to "disabled" to turn off the panic key
	// entirely.
	PanicKeyChord string `json:"panic_key_chord,omitempty"`

	// DestructiveAppGate controls whether the destructive-app denylist gate
	// is active. When true (default), actions targeting apps on the
	// denylist prompt the user for approval before proceeding.
	DestructiveAppGate bool `json:"destructive_app_gate,omitempty"`

	// OverrideFilePath is an optional override of the per-user denylist
	// override file location. When empty, the default path
	// (~/.config/sprout/computer_use_denylist_overrides.json) is used.
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
		// PanicKeyChord: if non-empty, use it. "disabled" is preserved as an
		// explicit-off sentinel. Empty string defaults to "ctrl+shift+escape".
		if c.PanicKeyChord != "" {
			result.PanicKeyChord = c.PanicKeyChord
		}
		// DestructiveAppGate defaults to true. Because it's a plain bool
		// (not a pointer), we can't distinguish "not set" from "set to
		// false" — so the default is always true when computer use is
		// enabled. Users who want to disable it must use a pointer-based
		// config override (out of scope for this change).
		result.DestructiveAppGate = true
		result.OverrideFilePath = c.OverrideFilePath
	}
	return result
}

// NotificationsConfig controls how the agent notifies the user when
// long-running turns complete (SP-070).
type NotificationsConfig struct {
	// CLIBell emits a terminal bell (\a) on completion.
	CLIBell bool `json:"cli_bell,omitempty"`
	// OSNotify fires an OS-level desktop notification on completion.
	OSNotify bool `json:"os_notify,omitempty"`
	// Browser fires a browser notification (used by WebUI, SP-070-4).
	Browser bool `json:"browser,omitempty"`
	// MinSeconds is the minimum turn duration (in seconds) before a
	// notification is sent.  Default: 10.0.  Turns completing in less
	// than this are considered too brief to warrant a notification.
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

// EditApprovalConfig controls the per-hunk diff approval gate (SP-072).
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
		ProactiveContextEnabled:  true,
		MaxContextualResults:     5,
		MinRelevanceScore:        0.50,
		MaxContextChars:          4000,
		WorkspaceScopedRetrieval: true,
		DriftDetectionEnabled:    true,
		DriftThreshold:           0.60,
		DriftCheckInterval:       5,
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

// ChangeTrackingConfig gates and tunes the ChangeTracker. When Enabled
// is false (the default) the entire subsystem is dormant — no file
// changes are recorded, no revision history is written, and the
// rollback/recover/view_history tools are no-ops. When Enabled is true
// the per-shell_command snapshot walk runs (tuned by the remaining
// fields) and direct file-tool tracking (write_file, edit_file,
// patch_structured_file) records changes.
type ChangeTrackingConfig struct {
	// Enabled controls whether the change tracking subsystem is active
	// at all. When false, no file changes are recorded, no revision
	// history is written, and the rollback/recover/view_history tools
	// are no-ops.
	//
	// Defaults to true. The git-awareness guards (IsRevertSafe) now
	// prevent the subsystem from reverting committed work, so tracking
	// stays on by default. Set to false to disable the entire subsystem.
	Enabled *bool `json:"enabled,omitempty"`

	// ShellWalkEnabled controls whether the per-shell_command snapshot
	// walk runs at all. Disable for workspaces where the walk cost is
	// unacceptable (e.g., very-large monorepos with novel bloat
	// directories) or for users who don't care about recovering
	// shell-deleted untracked files. Direct file-tool tracking is
	// unaffected. Default: true. Only meaningful when Enabled is true.
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

	// MaxChangesPerRevision caps the number of change records kept per
	// revision in .sprout/changes/. A single runaway session (e.g. an
	// agent that `cd`'d into $HOME so a shell walk classified
	// pre-existing files as creates) can produce tens of thousands of
	// records; without this cap, count bloat persists even when total
	// bytes are under MaxDirBytes. Default: 10000.
	MaxChangesPerRevision int `json:"max_changes_per_revision,omitempty"`

	// MaxChangesAgeDays drops change records older than this number of
	// days regardless of their parent revision's tier. Belt-and-
	// suspenders against changes/ accumulating inside the hot window.
	// Default: 30. Set to a negative value to disable.
	MaxChangesAgeDays int `json:"max_changes_age_days,omitempty"`
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
	// Change tracking is enabled by default. The git-awareness guards
	// (IsRevertSafe) prevent the subsystem from reverting committed
	// work, so it stays on unless the user explicitly sets
	// change_tracking.enabled = false.
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

// Resolve fills in defaults for any zero-value fields and returns a
// fully-populated retention config. Safe to call on nil — yields
// all-defaults.
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
