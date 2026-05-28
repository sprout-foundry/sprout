package modelcontract

// Agentic-coding eligibility thresholds (deterministic minimum bar per role).
// These are a placeholder for the capability probe, which provides the
// authoritative agentic-capable signal. Eligibility ≠ recommendation.
const (
	SubagentMinContext = 32_000
	PrimaryMinContext  = 128_000
)

// ClassifyEligibleRoles returns the agentic roles a model meets the minimum
// deterministic bar for. A model that is *known* to lack tool calling is never
// eligible (tool use is mandatory for agentic coding); unknown tool support
// gets the benefit of the doubt at this pre-filter stage — the probe decides.
// Below the subagent context threshold, or unknown context, returns nil.
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

// FillEligibleRoles populates EligibleRoles for every model from its canonical
// fields. Always recomputed (deterministic) so it stays consistent with the
// current thresholds/capabilities.
func FillEligibleRoles(models []CanonicalModel) {
	for i := range models {
		models[i].EligibleRoles = ClassifyEligibleRoles(models[i])
	}
}
