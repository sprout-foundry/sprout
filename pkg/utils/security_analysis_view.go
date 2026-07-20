package utils

import "fmt"

// SecurityAnalysisView is the leaf-level mirror of pkg/agent.SecurityAnalysis.
//
// pkg/utils is intentionally below pkg/agent in the dependency graph
// (pkg/agent imports pkg/utils, not the reverse), so the picker hook
// signature cannot take *pkg/agent.SecurityAnalysis directly without
// breaking the leaf-level contract. This struct duplicates just the four
// fields the CLI prompt needs to render the analysis panel above the
// 4-option picker.
//
// pkg/agent converts its own SecurityAnalysis to this view at the
// call site (see pkg/agent/approval_broker.go and
// pkg/agent/seed_tool_security.go). Adding fields here is safe — old
// callers pass nil, new callers populate what they have.
type SecurityAnalysisView struct {
	// Summary is the LLM's one-sentence plain-language description
	// of what the command does. Always populated when the analyzer
	// succeeded; the worst case is a short sentence.
	Summary string

	// Modifies is a short description of what the command would
	// modify (filesystem paths, network endpoints, system state).
	// May be empty if the LLM did not flag anything specific.
	Modifies string

	// RiskAssessment is the LLM's own rating: "low", "moderate", or
	// "high". Independent of the static classifier's risk level
	// (SAFE/CAUTION/DANGEROUS) shown elsewhere in the prompt — the
	// two often agree but can diverge on context-sensitive commands.
	RiskAssessment string

	// Recommendation is the LLM's suggested action: "approve",
	// "review", or "reject". Drives the color-coded tone badge in
	// the CLI panel ("✓ Looks safe" / "⚠ Review needed" /
	// "✗ Recommend reject").
	Recommendation string

	// ChainLength is the number of subcommands in the analyzed chain.
	// 0 for single-command analyses (regression-guard: legacy CLI/WebUI
	// callers pass nil/zero and see no chain UI). SP-124b Phase 2.
	ChainLength int

	// ChainSubcommands are the per-subcommand strings (in order). Rendered
	// as a stepper in CLI/WebUI when ChainLength > 1. SP-124b Phase 2.
	ChainSubcommands []string

	// ChainClassifications is the per-subcommand risk ("low"/"moderate"/
	// "high"), parallel to ChainSubcommands. Drives the colored dots on
	// the stepper. SP-124b Phase 2.
	ChainClassifications []string
}

// renderSecurityAnalysisFallbackLine returns a single-line plain-text
// rendering of an analysis, suitable for the legacy [y/n/a/s/e] prompt
// fallback. The arrow-key picker path uses its own color-coded panel;
// this function exists for callers that haven't registered pkg/console's
// hook. Safe to call with nil.
//
// Format: `[security analysis] <glyph> <summary>` where glyph is
//   - ✓ for "approve" (LLM suggests the operation is safe)
//   - ✗ for "reject"  (LLM suggests denying the operation)
//   - ! for everything else ("review", empty, unknown)
//
// Unknown recommendations collapse to "!" so we always surface *something*
// rather than rendering an empty field that the user might mis-read as
// "no analysis". Keeps the line scannable in non-TTY environments.
func renderSecurityAnalysisFallbackLine(sa *SecurityAnalysisView) string {
	if sa == nil {
		return ""
	}
	glyph := "!"
	switch sa.Recommendation {
	case "approve":
		glyph = "✓"
	case "reject":
		glyph = "✗"
	}
	return fmt.Sprintf("[security analysis] %s %s", glyph, sa.Summary)
}