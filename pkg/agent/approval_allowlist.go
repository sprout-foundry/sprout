package agent

import (
	"path"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// IsShellCommandAllowlisted reports whether the command matches an approved literal or glob pattern.
// Critical-tier commands are still blocked regardless of allowlist matches.
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

// PersistShellCommandAskPolicy adds a "always ask" command policy rule for the given command.
func (a *Agent) PersistShellCommandAskPolicy(command string) error {
	if a == nil {
		return agenterrors.NewPermission("nil agent", nil)
	}
	if command == "" {
		return agenterrors.NewValidation("cannot persist empty command as ask policy", nil)
	}
	mgr := a.GetConfigManager()
	if mgr == nil {
		return agenterrors.NewPermission("no config manager — cannot persist ask policy", nil)
	}
	return mgr.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.CommandPolicies == nil {
			cfg.CommandPolicies = &configuration.CommandPolicies{}
		}
		for _, r := range cfg.CommandPolicies.Rules {
			if r.Pattern == command && r.Action == configuration.CommandPolicyAsk {
				return nil // already exists
			}
		}
		cfg.CommandPolicies.Rules = append(cfg.CommandPolicies.Rules, configuration.CommandRule{
			Pattern: command,
			Action:  configuration.CommandPolicyAsk,
		})
		return nil
	})
}

// ElevateSessionToPermissive sets the transient risk-profile override to "permissive" for this session.
// Critical-tier ops still block; "permissive" only widens the auto-approved set.
func (a *Agent) ElevateSessionToPermissive() {
	if a == nil {
		return
	}
	a.SetRiskProfileOverride(configuration.RiskProfilePermissive)
}
