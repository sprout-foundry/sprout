// Package agent: shell-command allowlist + persistence — *Agent forwarders
// (SP-141 phase 3, increment 3). The implementation lives in
// pkg/agent/approvals (allowlist.go); these one-line methods keep the
// *Agent method surface used by the approval broker, the tool-security
// gates, and seed-time security checks.
//
// Behavior preservation: each forwarder keeps the pre-move
// `agent == nil` guard before resolving config/manager (GetConfigManager
// is not nil-safe on a nil *Agent). ElevateSessionToPermissive is not
// moved — it mutates agent-local risk-profile state.

package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/approvals"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// IsShellCommandAllowlisted reports whether the command matches an approved literal or glob pattern.
// Critical-tier commands are still blocked regardless of allowlist matches.
func (a *Agent) IsShellCommandAllowlisted(command string) bool {
	if a == nil {
		return false
	}
	return approvals.IsShellCommandAllowlisted(a.GetConfig(), command)
}

// PersistShellCommandAllowlist appends command to the user's persistent
// approved-commands list (Config.ApprovedShellCommands) and saves to disk.
// Used by the "Always approve this command" choice on the approval dialog.
// Idempotent: re-adding an existing entry is a no-op but still triggers
// a save so the file's mtime updates (cheap).
func (a *Agent) PersistShellCommandAllowlist(command string) error {
	if a == nil {
		return approvals.ErrNilAgent
	}
	return approvals.PersistShellCommandAllowlist(a.GetConfigManager(), command)
}

// PersistShellCommandPattern appends pattern to the user's persistent
// approved-command-pattern list (Config.ApprovedShellCommandPatterns) and
// saves to disk. Patterns use Go path.Match glob syntax (`*`, `?`, `[]`).
// Idempotent: re-adding an existing entry is a no-op but still triggers
// a save so the file's mtime updates (cheap).
func (a *Agent) PersistShellCommandPattern(pattern string) error {
	if a == nil {
		return approvals.ErrNilAgent
	}
	return approvals.PersistShellCommandPattern(a.GetConfigManager(), pattern)
}

// PersistShellCommandAskPolicy adds a "always ask" command policy rule for the given command.
func (a *Agent) PersistShellCommandAskPolicy(command string) error {
	if a == nil {
		return approvals.ErrNilAgent
	}
	return approvals.PersistShellCommandAskPolicy(a.GetConfigManager(), command)
}

// ElevateSessionToPermissive sets the transient risk-profile override to "permissive" for this session.
// Critical-tier ops still block; "permissive" only widens the auto-approved set.
func (a *Agent) ElevateSessionToPermissive() {
	if a == nil {
		return
	}
	a.SetRiskProfileOverride(configuration.RiskProfilePermissive)
}
