// Package agent: risk evaluation and profile management (split from agent_getters.go)
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// EvaluateOperationRisk determines the risk level of a command for the
// currently active persona, using the persona's auto-approve rules.
// Returns RiskLevelCritical / High / Medium / Low.
//
// Resolution order (matches the SP-058 risk profile design):
//  1. Critical patterns (rm -rf root, fork bomb) — ALWAYS return Critical,
//     regardless of persona, profile, or active mode.
//  2. Active persona has its own AutoApproveRules → use them (preserves
//     EA autonomy and any other persona-specific carve-outs).
//  3. Otherwise → resolve the agent's active risk profile and use its
//     baked-in rules.
//  4. No persona at all → return Low (no cascade gating, classic
//     non-EA behavior).
func (a *Agent) EvaluateOperationRisk(command string) configuration.RiskLevel {
	// Step 1: Critical is absolute and orthogonal to persona/profile.
	if configuration.IsCriticalOperation(command) {
		return configuration.RiskLevelCritical
	}

	personaID := a.GetActivePersona()
	if personaID == "" {
		return configuration.RiskLevelLow
	}
	cfg := a.GetConfig()
	if cfg == nil {
		return configuration.RiskLevelLow
	}

	// Step 2: Persona-defined rules win when present.
	persona := cfg.GetSubagentType(personaID)
	if persona != nil && persona.AutoApproveRules != nil {
		return persona.EvaluateOperationRisk(command)
	}

	// Step 3: Fall back to the active risk profile. Use the
	// config-aware resolver so user overrides in Config.RiskProfiles
	// take precedence over baked-in defaults. A synthetic
	// SubagentType reuses the existing rule-matching code path.
	rules := configuration.ResolveRiskProfileRules(cfg, a.activeRiskProfile())
	synthetic := &configuration.SubagentType{AutoApproveRules: &rules}
	return synthetic.EvaluateOperationRisk(command)
}

// activeRiskProfile returns the risk profile that should apply for
// the next operation. Resolution: per-agent override (set by CLI
// flag / workflow step) → config.RiskProfile → "default".
func (a *Agent) activeRiskProfile() configuration.RiskProfile {
	if a.riskProfileOverride != "" {
		return a.riskProfileOverride
	}
	if cfg := a.GetConfig(); cfg != nil && cfg.RiskProfile != "" && configuration.IsValidRiskProfile(cfg.RiskProfile) {
		return configuration.RiskProfile(cfg.RiskProfile)
	}
	return configuration.RiskProfileDefault
}

// SetRiskProfileOverride installs a transient risk profile that
// overrides the config-level setting for the lifetime of this agent.
// Used by the --risk-profile CLI flag and per-step workflow overrides.
// Pass "" to clear.
func (a *Agent) SetRiskProfileOverride(profile configuration.RiskProfile) {
	a.riskProfileOverride = profile
}

// GetActiveRiskProfile returns the profile currently in effect for
// this agent (override > config > default). Exposed for status
// commands / debug logging.
func (a *Agent) GetActiveRiskProfile() configuration.RiskProfile {
	return a.activeRiskProfile()
}

// IsSessionElevated reports whether the user has elevated the session
// to a permissive or unrestricted risk profile. When true, all three
// security gates (static classifier, filesystem tier, shell risk
// cascade) must skip their interactive prompts and auto-approve —
// the user explicitly opted out of per-operation prompts for this
// session. Critical-tier operations (rm -rf /, fork bombs) are NOT
// covered by elevation and always block regardless.
func (a *Agent) IsSessionElevated() bool {
	profile := a.activeRiskProfile()
	return profile == configuration.RiskProfilePermissive || profile == configuration.RiskProfileUnrestricted
}

// IsSubagent returns true if this agent was spawned as a subagent (depth > 0).
// Used to prevent nested subagent spawning and skip interactive prompts.
func (a *Agent) IsSubagent() bool {
	return a.subagentDepth > 0
}

// SubagentDepth returns the nesting depth of this agent.
// 0 = primary agent (EA), 1 = orchestrator, 2 = coder/tester, etc.
func (a *Agent) SubagentDepth() int {
	return a.subagentDepth
}

// MaxSubagentDepth returns the configured maximum nesting depth.
// EA root gets 3 levels (max depth 2), non-EA root gets 2 levels (max depth 1).
func (a *Agent) MaxSubagentDepth() int {
	// Check config override first
	if cfg := a.GetConfig(); cfg != nil && cfg.SubagentMaxDepth > 0 {
		return cfg.SubagentMaxDepth
	}

	// Coordinator root: 3 levels (coordinator → orchestrator → coder)
	if a.rootPersonaID == personas.IDCoordinator {
		return 2
	}

	// Non-EA root: 2 levels (orchestrator → coder)
	return 1
}

// CanSpawnSubagents returns true if this agent is allowed to spawn subagents
// (i.e., current depth is less than the configured max depth).
func (a *Agent) CanSpawnSubagents() bool {
	if configuration.GetEnvSimple("NO_SUBAGENTS") == "1" {
		return false
	}
	return a.subagentDepth < a.MaxSubagentDepth()
}

// IsLocalMode returns true when the agent is running locally (CLI or local WebUI),
// not in a cloud environment. This controls whether LocalOnly personas (like the
// Executive Assistant) are available.
//
// Cloud mode is detected via the SPROUT_CLOUD environment variable.
// Local mode is the default when the variable is unset or empty.
func (a *Agent) IsLocalMode() bool {
	return configuration.GetEnvSimple("CLOUD") != "1"
}
