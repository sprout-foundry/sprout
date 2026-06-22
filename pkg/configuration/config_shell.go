package configuration

import (
	"fmt"
)

// ShellPattern is a single user-defined shell classification pattern.
type ShellPattern struct {
	Match  string `json:"match"`            // prefix string or regex pattern
	Kind   string `json:"kind"`             // "prefix" or "regex"
	Reason string `json:"reason,omitempty"` // optional human-readable note
}

// WorkspaceOverlayConfig controls how workspace-rooted policy files are loaded.
type WorkspaceOverlayConfig struct {
	// Mode: "tighten_only" (default — only user_dangerous_patterns honored),
	// "trusted" (full overlay honored after `sprout policy trust`), or
	// "ignore" (workspace policy never loaded).
	Mode string `json:"mode,omitempty"`
}

// ShellConfig holds user-configurable shell permission policy.
type ShellConfig struct {
	UserSafePatterns      []ShellPattern         `json:"user_safe_patterns,omitempty"`
	UserDangerousPatterns []ShellPattern         `json:"user_dangerous_patterns,omitempty"`
	WorkspaceOverlay      WorkspaceOverlayConfig `json:"workspace_overlay,omitempty"`
}

// Validate checks the ShellConfig values and normalizes invalid ones.
// Returns an error for values that can't be normalized.
func (sc *ShellConfig) Validate() error {
	if sc == nil {
		return nil
	}

	// Validate pattern kinds
	for i, p := range sc.UserSafePatterns {
		err := validateShellPatternKind(p.Kind)
		if err != nil {
			return fmt.Errorf("user_safe_patterns[%d]: %w", i, err)
		}
		sc.UserSafePatterns[i].Kind = normalizeShellPatternKind(p.Kind)
	}
	for i, p := range sc.UserDangerousPatterns {
		err := validateShellPatternKind(p.Kind)
		if err != nil {
			return fmt.Errorf("user_dangerous_patterns[%d]: %w", i, err)
		}
		sc.UserDangerousPatterns[i].Kind = normalizeShellPatternKind(p.Kind)
	}

	// Validate workspace overlay mode
	sc.WorkspaceOverlay.Mode = normalizeWorkspaceOverlayMode(sc.WorkspaceOverlay.Mode)

	return nil
}

// validateShellPatternKind returns an error if kind is not a recognized pattern kind.
func validateShellPatternKind(kind string) error {
	switch kind {
	case "", "prefix", "regex":
		return nil
	}
	return fmt.Errorf("invalid pattern kind %q (allowed: prefix, regex)", kind)
}

// normalizeShellPatternKind returns "prefix" for empty/unknown kinds, preserving "prefix"/"regex".
func normalizeShellPatternKind(kind string) string {
	switch kind {
	case "prefix", "regex":
		return kind
	case "":
		return "prefix" // default
	default:
		return "prefix" // normalize invalid to default
	}
}

// normalizeWorkspaceOverlayMode returns a valid mode, defaulting to "tighten_only".
func normalizeWorkspaceOverlayMode(mode string) string {
	switch mode {
	case "", "tighten_only", "trusted", "ignore":
		if mode == "" {
			return "tighten_only" // default
		}
		return mode
	default:
		return "tighten_only" // normalize invalid to default
	}
}
