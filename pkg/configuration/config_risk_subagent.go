// Package configuration: core risk-level and risk-profile types.
// (split from config_risk_subagent.go)
package configuration

// RiskLevel represents the risk classification of an operation for the EA approval cascade.
type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "low"      // Auto-approve (git status, read operations)
	RiskLevelMedium   RiskLevel = "medium"   // Reason and decide (git commit, git push)
	RiskLevelHigh     RiskLevel = "high"     // Prompt the user when interactive; reject when not
	RiskLevelCritical RiskLevel = "critical" // Never approvable: rm -rf root, fork bombs
)

// AutoApproveRules controls the EA's sliding risk cascade for operation approvals.
type AutoApproveRules struct {
	LowRiskOps    []string `json:"low_risk,omitempty"`        // Operations auto-approved by EA
	MediumRiskOps []string `json:"medium_risk,omitempty"`     // Operations the EA reasons about
	HighRiskNever []string `json:"high_risk_never,omitempty"` // Pattern names always gated (rm_recursive, force_flag, ...)
	// DefaultRisk is the level returned for operations that don't
	// match any of the above. Default (empty) is "medium" — the
	// classic EA behavior. Cautious profiles set this to "high"
	// so unrecognized commands route to a prompt. Permissive /
	// unrestricted set it to "low" so common operations auto-approve.
	DefaultRisk RiskLevel `json:"default_risk,omitempty"`
}

// DefaultAutoApproveRules returns the default risk cascade rules for the EA persona.
func DefaultAutoApproveRules() AutoApproveRules {
	return AutoApproveRulesForProfile(RiskProfileDefault)
}

// RiskProfile names a preset risk-cascade configuration. The active
// profile resolves to an AutoApproveRules via AutoApproveRulesForProfile.
// Persona-specified rules always take precedence over the profile.
type RiskProfile string

const (
	// RiskProfileReadonly — strictest. ONLY read operations (git
	// status / log / diff, read_file) are permitted. Every write,
	// edit, shell command, or destructive op is BLOCKED outright
	// (no prompt path) by promoting to the Critical tier. Use for
	// audits, code review, or sandboxed inspection where the agent
	// should never mutate anything.
	RiskProfileReadonly RiskProfile = "readonly"

	// RiskProfileCautious — most operations prompt the user. Suitable
	// for sensitive workspaces or unfamiliar agents. Low-risk reads
	// auto-approve; everything else gets routed to a prompt.
	RiskProfileCautious RiskProfile = "cautious"

	// RiskProfileDefault — sane defaults matching the historical EA
	// cascade. Reads auto-approve, common edits/commits auto-approve,
	// destructive operations (force flags, rm -rf, lossy git) prompt.
	RiskProfileDefault RiskProfile = "default"

	// RiskProfilePermissive — high trust. Almost everything passes
	// without prompting; only truly destructive patterns route to a
	// prompt. Use when the agent is well-trusted and the workspace
	// is recoverable (clean checkout, throwaway dir).
	RiskProfilePermissive RiskProfile = "permissive"

	// RiskProfileUnrestricted — no risk cascade gating at all. Only
	// the Critical tier (rm -rf root, fork bombs) blocks. Use with
	// extreme care; intended for sandboxed / disposable environments.
	RiskProfileUnrestricted RiskProfile = "unrestricted"
)
