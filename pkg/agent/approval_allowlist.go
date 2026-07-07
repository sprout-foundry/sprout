package agent

import (
	"path"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// IsShellCommandAllowlisted reports whether the user has previously chosen
// "Always approve this command" for this exact command string (literal
// match) or whether any user-defined glob pattern in
// ApprovedShellCommandPatterns matches the command. Pattern matching uses
// Go's path.Match glob syntax: `*` (any non-`/` sequence), `?` (single
// char), `[abc]` (char class).
//
// Caveat: while `*` does not match `/`, character classes can — e.g.
// `[^a-z]` or `[/.]` will match `/`. This is NOT a security hole because
// the Critical tier still blocks regardless of both literal and pattern
// matches; this short-circuit only applies to the High-risk
// persona-cascade gate. Critical-tier enforcement happens at the call
// site before this function is consulted (see risk_prompt.go and
// tool_security.go), so no pattern can bypass a hard-blocked command.
func (a *Agent) IsShellCommandAllowlisted(command string) bool {
	if a == nil || command == "" {
		return false
	}
	cfg := a.GetConfig()
	if cfg == nil {
		return false
	}
	// 1. Literal match against ApprovedShellCommands (unchanged behavior).
	for _, c := range cfg.ApprovedShellCommands {
		if c == command {
			return true
		}
	}
	// 2. Glob pattern match against ApprovedShellCommandPatterns.
	// path.Match uses glob syntax (not regexp) — safer and simpler.
	for _, pattern := range cfg.ApprovedShellCommandPatterns {
		if matched, err := path.Match(pattern, command); err == nil && matched {
			return true
		}
	}
	return false
}

// PersistShellCommandAllowlist appends command to the user's persistent
// approved-commands list (Config.ApprovedShellCommands) and saves to disk.
// Used by the "Always approve this command" choice on the approval dialog.
// Idempotent: re-adding an existing entry is a no-op but still triggers
// a save so the file's mtime updates (cheap).
func (a *Agent) PersistShellCommandAllowlist(command string) error {
	if a == nil {
		return agenterrors.NewPermission("nil agent", nil)
	}
	if command == "" {
		return agenterrors.NewValidation("cannot allowlist empty command", nil)
	}
	mgr := a.GetConfigManager()
	if mgr == nil {
		return agenterrors.NewPermission("no config manager — cannot persist allowlist", nil)
	}
	return mgr.UpdateConfig(func(cfg *configuration.Config) error {
		for _, c := range cfg.ApprovedShellCommands {
			if c == command {
				return nil
			}
		}
		cfg.ApprovedShellCommands = append(cfg.ApprovedShellCommands, command)
		return nil
	})
}

// PersistShellCommandPattern appends pattern to the user's persistent
// approved-command-pattern list (Config.ApprovedShellCommandPatterns) and
// saves to disk. Patterns use Go path.Match glob syntax (`*`, `?`, `[]`).
// Idempotent: re-adding an existing entry is a no-op but still triggers
// a save so the file's mtime updates (cheap).
func (a *Agent) PersistShellCommandPattern(pattern string) error {
	if a == nil {
		return agenterrors.NewPermission("nil agent", nil)
	}
	if pattern == "" {
		return agenterrors.NewValidation("cannot allowlist empty pattern", nil)
	}
	mgr := a.GetConfigManager()
	if mgr == nil {
		return agenterrors.NewPermission("no config manager — cannot persist allowlist pattern", nil)
	}
	return mgr.UpdateConfig(func(cfg *configuration.Config) error {
		for _, p := range cfg.ApprovedShellCommandPatterns {
			if p == pattern {
				return nil
			}
		}
		cfg.ApprovedShellCommandPatterns = append(cfg.ApprovedShellCommandPatterns, pattern)
		return nil
	})
}

// ElevateSessionToPermissive sets the agent's transient risk-profile
// override to "permissive" for the rest of this session. Used by the
// "Elevate permissions" choice on the approval dialog. Does NOT persist
// to disk — the user is expected to run `/risk-profile permissive` if
// they want this to survive restart.
//
// Critical-tier ops (rm -rf /, fork bombs) still block; "permissive"
// only widens the auto-approved set, it does not disable the cascade.
func (a *Agent) ElevateSessionToPermissive() {
	if a == nil {
		return
	}
	a.SetRiskProfileOverride(configuration.RiskProfilePermissive)
}
