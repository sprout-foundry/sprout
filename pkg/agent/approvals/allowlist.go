// Package approvals: shell-command allowlist lookup + persistence (SP-141
// phase 3, increment 3).
//
// These were *Agent methods in pkg/agent/approval_allowlist.go. They only
// need a *configuration.Config (lookup) or a *configuration.Manager
// (persistence), so they live here without any *Agent dependency. The
// *Agent methods remain as one-line forwarders in pkg/agent, preserving
// the method-call surface used by the approval broker, the tool-security
// gates, and seed-time security checks.
//
// Error ordering note (behavior-preservation detail): the pre-move
// methods checked `agent == nil` first, then the empty-input validation,
// then the missing-manager error. The forwarders keep the `agent == nil`
// guard; the moved functions check empty-input *before* the nil-manager
// case, matching the pre-move order for any non-nil agent.

package approvals

import (
	"path"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ErrNilAgent is the error the *Agent forwarders return for the persist
// methods when the agent is nil (matching the pre-move "nil agent"
// Permission error).
var ErrNilAgent = agenterrors.NewPermission("nil agent", nil)

// IsShellCommandAllowlisted reports whether the command matches an approved
// literal or glob pattern. Critical-tier commands are still blocked
// regardless of allowlist matches.
func IsShellCommandAllowlisted(cfg *configuration.Config, command string) bool {
	if cfg == nil || command == "" {
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
func PersistShellCommandAllowlist(mgr *configuration.Manager, command string) error {
	if command == "" {
		return agenterrors.NewValidation("cannot allowlist empty command", nil)
	}
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
func PersistShellCommandPattern(mgr *configuration.Manager, pattern string) error {
	if pattern == "" {
		return agenterrors.NewValidation("cannot allowlist empty pattern", nil)
	}
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

// PersistShellCommandAskPolicy adds a "always ask" command policy rule for
// the given command.
func PersistShellCommandAskPolicy(mgr *configuration.Manager, command string) error {
	if command == "" {
		return agenterrors.NewValidation("cannot persist empty command as ask policy", nil)
	}
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
