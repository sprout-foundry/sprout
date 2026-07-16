package configuration

// CommandPolicyAction determines what happens when a command matches a rule.
type CommandPolicyAction string

const (
	// CommandPolicyAllow auto-approves the command, skipping classifier, risk
	// profile, and interactive prompt. Does not override Critical-tier blocks.
	CommandPolicyAllow CommandPolicyAction = "allow"
	// CommandPolicyAsk forces an interactive prompt, skipping allowlist and
	// classifier auto-approve. Classifier risk is still computed for display.
	CommandPolicyAsk CommandPolicyAction = "ask"
	// CommandPolicyDeny hard-blocks the command, returning an error immediately.
	CommandPolicyDeny CommandPolicyAction = "deny"
)

// CommandRule is a single user-defined command policy rule.
type CommandRule struct {
	// Pattern is a glob pattern (Go path.Match syntax) to match against
	// shell commands. For example: "git push*", "rm -rf /tmp/*".
	Pattern string `json:"pattern"`
	// Action determines the behavior when a command matches this rule.
	Action CommandPolicyAction `json:"action"`
	// Reason is an optional user note explaining why this rule exists.
	Reason string `json:"reason,omitempty"`
}

// CommandPolicies is the top-level config structure for user command policies
// (SP-123). Rules are evaluated first-match-wins before the classifier and
// risk profile. The three actions (allow/ask/deny) override the default
// approval cascade.
type CommandPolicies struct {
	Rules []CommandRule `json:"rules"`
}

// MigrateCommandPolicies converts legacy approved_shell_commands and
// approved_shell_command_patterns fields into the unified CommandPolicies
// format. It is a no-op when cfg.CommandPolicies is already non-nil.
//
// After migration, the old fields remain in the config for backward
// compatibility but are no longer consulted by the policy engine.
func MigrateCommandPolicies(cfg *Config) {
	if cfg.CommandPolicies != nil {
		return // Already migrated or explicitly configured
	}

	var rules []CommandRule

	for _, cmd := range cfg.ApprovedShellCommands {
		if cmd == "" {
			continue
		}
		rules = append(rules, CommandRule{
			Pattern: cmd,
			Action:  CommandPolicyAllow,
		})
	}

	for _, pattern := range cfg.ApprovedShellCommandPatterns {
		if pattern == "" {
			continue
		}
		rules = append(rules, CommandRule{
			Pattern: pattern,
			Action:  CommandPolicyAllow,
		})
	}

	if len(rules) > 0 {
		cfg.CommandPolicies = &CommandPolicies{Rules: rules}
	}
}
