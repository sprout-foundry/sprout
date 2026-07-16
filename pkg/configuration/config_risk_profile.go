// Package configuration: risk-profile resolution and rule factory.
// (split from config_risk_subagent.go)
package configuration

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
		//
		// git_checkout / git_switch / git_restore are intentionally
		// NOT in HighRiskNever: these are working-tree-mutating ops
		// fully recoverable via the ChangeTracker (recover_file /
		// revert_my_changes). AGENTS.md classifies them as restorable
		// local history ops. They fall through to categorizeGitCommand
		// → DefaultRisk = Medium. Only genuinely destructive ops
		// (force flags, recursive rm, hard reset, clean, force push,
		// branch deletion) are gated as High.
		return AutoApproveRules{
			LowRiskOps: []string{
				"git_add", "git_status", "git_log", "git_diff",
				"read_file", "build_test",
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
				"git_branch_delete",
			},
			DefaultRisk: RiskLevelMedium,
		}
	}
}
