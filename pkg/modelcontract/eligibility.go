package modelcontract

import "fmt"

// Agentic-coding context thresholds for sprout. The thresholds carve the
// context-window spectrum into bands, each with different sprout behavior:
//
//	≥ PrimaryMinContext (128K): full mode — all tools, full prompt, no warning
//	≥ SubagentMinContext (64K): subagent-eligible, carries a context warning
//	≥ LowContextMinContext (16K): Low-Context Mode eligible (SP-125)
//	< LowContextMinContext: no eligible roles (hard block)
//
// Below ContextFloor (8K, defined in pkg/configuration) sprout refuses to
// start at all — that's enforced by ResolveContextProfile, not here. This
// function answers "which roles is this model recommended for?";
// ResolveContextProfile answers "given that the user chose it, how do we
// make it work — or do we refuse?"
const (
	// SubagentMinContext is the hard floor for full-context subagenting.
	SubagentMinContext = 64_000
	// PrimaryMinContext is required for the primary role, and is also the
	// threshold at/above which no context warning is attached.
	PrimaryMinContext = 128_000
	// LowContextMinContext (SP-125) is the minimum for Low-Context Mode —
	// the 8-tool, lite-prompt mode for small-context models. Below this,
	// no roles are eligible.
	LowContextMinContext = 16_000
)

// ClassifyEligibleRoles returns the agentic roles a model meets the minimum
// deterministic bar for. A model that is *known* to lack tool calling is never
// eligible (tool use is mandatory for agentic coding); unknown tool support
// gets the benefit of the doubt at this pre-filter stage — the probe decides.
//
// SP-125: models in the 16K–64K band are now eligible for RoleLowContext
// instead of returning nil (hard block). They get LCM (lite prompt, 8 tools)
// rather than being refused outright. Below 16K still returns nil.
func ClassifyEligibleRoles(m CanonicalModel) []string {
	if IsKnownFalse(m.Capabilities.Tools) {
		return nil
	}
	switch {
	case m.ContextWindow >= PrimaryMinContext:
		return []string{RolePrimary, RoleSubagent}
	case m.ContextWindow >= SubagentMinContext:
		return []string{RoleSubagent}
	case m.ContextWindow >= LowContextMinContext:
		return []string{RoleLowContext}
	default:
		return nil
	}
}

// ContextWarning returns a strong, non-blocking warning when a model's context
// window is below the recommended size for sprout. In the 64K–128K band it
// warns "usable only in a pinch"; in the 16K–64K band it notes that
// Low-Context Mode will auto-activate.
func ContextWarning(contextWindow int) string {
	if contextWindow >= PrimaryMinContext {
		return ""
	}
	if contextWindow >= SubagentMinContext {
		return fmt.Sprintf("context window %d is below the recommended %d for sprout; usable only in a pinch and may truncate context on larger tasks", contextWindow, PrimaryMinContext)
	}
	if contextWindow >= LowContextMinContext {
		return fmt.Sprintf("context window %d triggers Low-Context Mode (lite prompt, 8-tool allowlist); suitable for focused edits but not multi-file refactors", contextWindow)
	}
	return ""
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
