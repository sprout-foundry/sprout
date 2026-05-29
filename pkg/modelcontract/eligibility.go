package modelcontract

import "fmt"

// Agentic-coding context thresholds for sprout. A context window below
// SubagentMinContext is a hard block — sprout's agentic loops (tool results,
// file reads, repo context) need room to work, and below ~64K a model is not
// usable. Between SubagentMinContext and PrimaryMinContext a model is usable
// only in a pinch and carries a strong warning. Eligibility ≠ recommendation;
// the capability probe provides the authoritative agentic-capable signal.
const (
	// SubagentMinContext is the hard floor: below this, no eligible roles.
	SubagentMinContext = 64_000
	// PrimaryMinContext is required for the primary role, and is also the
	// threshold at/above which no context warning is attached.
	PrimaryMinContext = 128_000
)

// ClassifyEligibleRoles returns the agentic roles a model meets the minimum
// deterministic bar for. A model that is *known* to lack tool calling is never
// eligible (tool use is mandatory for agentic coding); unknown tool support
// gets the benefit of the doubt at this pre-filter stage — the probe decides.
// Below SubagentMinContext (or unknown context) returns nil — a hard block.
func ClassifyEligibleRoles(m CanonicalModel) []string {
	if IsKnownFalse(m.Capabilities.Tools) {
		return nil
	}
	switch {
	case m.ContextWindow >= PrimaryMinContext:
		return []string{RolePrimary, RoleSubagent}
	case m.ContextWindow >= SubagentMinContext:
		return []string{RoleSubagent}
	default:
		return nil
	}
}

// ContextWarning returns a strong, non-blocking warning when a model's context
// window is usable but below the recommended size for sprout (the
// SubagentMinContext–PrimaryMinContext band). It returns empty when the context
// is adequate (>= PrimaryMinContext) or when the model is hard-blocked
// (< SubagentMinContext, which yields no eligible roles instead of a warning).
func ContextWarning(contextWindow int) string {
	if contextWindow >= PrimaryMinContext || contextWindow < SubagentMinContext {
		return ""
	}
	return fmt.Sprintf("context window %d is below the recommended %d for sprout; usable only in a pinch and may truncate context on larger tasks", contextWindow, PrimaryMinContext)
}

// FillEligibleRoles populates EligibleRoles and any derived Warnings for every
// model from its canonical fields. Always recomputed (deterministic) so it
// stays consistent with the current thresholds/capabilities.
func FillEligibleRoles(models []CanonicalModel) {
	for i := range models {
		models[i].EligibleRoles = ClassifyEligibleRoles(models[i])
		models[i].Warnings = nil
		if w := ContextWarning(models[i].ContextWindow); w != "" {
			models[i].Warnings = append(models[i].Warnings, w)
		}
	}
}
