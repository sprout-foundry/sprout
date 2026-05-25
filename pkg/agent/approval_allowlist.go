package agent

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// IsShellCommandAllowlisted reports whether the user has previously chosen
// "Always approve this command" for this exact command string. The match
// is literal — allowlisting `rm -rf /tmp/build` does NOT cover any other
// path. The Critical tier still blocks regardless; this short-circuit only
// applies to the High-risk persona-cascade gate.
func (a *Agent) IsShellCommandAllowlisted(command string) bool {
	if a == nil || command == "" {
		return false
	}
	cfg := a.GetConfig()
	if cfg == nil {
		return false
	}
	for _, c := range cfg.ApprovedShellCommands {
		if c == command {
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
		return fmt.Errorf("nil agent")
	}
	if command == "" {
		return fmt.Errorf("cannot allowlist empty command")
	}
	mgr := a.GetConfigManager()
	if mgr == nil {
		return fmt.Errorf("no config manager — cannot persist allowlist")
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
