package agent

import (
	"fmt"
	"sort"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// SP-068 Phase 1 — One risk scale.
//
// Historically a tool call is judged by two independent vocabularies: the
// static classifier (pkg/agent_tools: SAFE/CAUTION/DANGEROUS) and the
// persona risk cascade (pkg/configuration: Low/Medium/High/Critical). This
// file introduces a single canonical representation — RiskAssessment, on
// the Low/Medium/High/Critical scale — and the mapping that folds the
// classifier's three tiers onto it.
//
// Phase 1 is deliberately behavior-preserving: these types and helpers are
// the vocabulary the Phase 2 resolver will consume. Nothing here changes a
// gating decision on its own; the golden tests lock the mapping so Phase 2
// can rewire the call sites without drift.

// RiskSource identifies which check contributed to an assessment, so a
// decision can be explained (Phase 3 `sprout explain`) instead of being an
// opaque "blocked".
type RiskSource string

const (
	// RiskSourceClassifier — the static, string-based classifier
	// (pkg/agent_tools.ClassifyToolCall).
	RiskSourceClassifier RiskSource = "classifier"
	// RiskSourcePersonaCascade — the persona / risk-profile cascade
	// (Agent.EvaluateOperationRisk).
	RiskSourcePersonaCascade RiskSource = "persona-cascade"
	// RiskSourceCriticalOp — the built-in critical-operation hard-block
	// (configuration.IsCriticalOperation).
	RiskSourceCriticalOp RiskSource = "critical-op"
)

// RiskAssessment is the canonical, single-vocabulary verdict for a tool
// call. Phase 2 makes it the single output of the unified resolver; Phase 1
// builds and tests it alongside the existing gates.
type RiskAssessment struct {
	// Level is the canonical risk on the Low/Medium/High/Critical scale.
	Level configuration.RiskLevel

	// IsHardBlock is true for critical-tier operations that no approval can
	// override (rm -rf /, fork bombs, mkfs).
	IsHardBlock bool

	// RequiresIntentConfirmation marks a safe-but-consequential operation
	// (e.g. launching an autonomous workflow) that needs explicit user
	// intent. It is orthogonal to Level — such an op is Low risk but still
	// gated on intent.
	RequiresIntentConfirmation bool

	// Sources lists every check that contributed, in precedence order.
	Sources []RiskSource

	// Reason is a human-readable explanation of the verdict.
	Reason string
}

// ResolveToolRisk produces the unified, single-vocabulary assessment for a
// tool call by folding the static classifier with (for shell_command) the
// persona / risk-profile cascade onto one Low/Medium/High/Critical scale.
//
// SP-068: this is the canonical risk view. Today it powers diagnostics (the
// gate's debug "[risk]" line and the future `sprout explain`) and runs
// alongside the existing gates without changing any decision; the eventual
// step is to make it the single gating authority so the two prompt paths and
// their approval bridges collapse into one.
func (a *Agent) ResolveToolRisk(toolName string, args map[string]interface{}) RiskAssessment {
	assessment := assessmentFromClassifier(tools.ClassifyToolCall(toolName, args))
	if toolName == "shell_command" && a != nil {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			level := a.EvaluateOperationRisk(cmd)
			assessment = assessment.combine(
				assessmentFromPersonaCascade(level, fmt.Sprintf("persona/profile risk cascade: %s", level)),
			)
		}
	}
	return assessment
}

// assessmentFromClassifier maps a static-classifier SecurityResult onto the
// canonical scale:
//
//	SAFE                 → Low
//	CAUTION              → Medium
//	DANGEROUS            → High
//	(IsHardBlock / crit) → Critical
//
// ShouldBlock/ShouldPrompt are not part of the canonical Level — they are
// downstream policy decisions the resolver derives from Level + context.
// IntentConfirmation is carried through as its own orthogonal flag.
func assessmentFromClassifier(res tools.SecurityResult) RiskAssessment {
	level := configuration.RiskLevelLow
	switch res.Risk {
	case tools.SecuritySafe:
		level = configuration.RiskLevelLow
	case tools.SecurityCaution:
		level = configuration.RiskLevelMedium
	case tools.SecurityDangerous:
		level = configuration.RiskLevelHigh
	}
	source := RiskSourceClassifier
	if res.IsHardBlock {
		level = configuration.RiskLevelCritical
		source = RiskSourceCriticalOp
	}
	return RiskAssessment{
		Level:                      level,
		IsHardBlock:                res.IsHardBlock,
		RequiresIntentConfirmation: res.IntentConfirmation,
		Sources:                    []RiskSource{source},
		Reason:                     res.Reasoning,
	}
}

// assessmentFromPersonaCascade builds an assessment from the persona/risk-
// profile cascade's RiskLevel verdict for a command.
func assessmentFromPersonaCascade(level configuration.RiskLevel, reason string) RiskAssessment {
	return RiskAssessment{
		Level:       level,
		IsHardBlock: level == configuration.RiskLevelCritical,
		Sources:     []RiskSource{RiskSourcePersonaCascade},
		Reason:      reason,
	}
}

// combine folds two assessments into one, taking the most restrictive Level
// (the resolver's "tighten, never silence" rule). The OR of hard-block and
// intent-confirmation flags is kept, and both sets of sources are merged so
// the result can explain every contributing check.
func (ra RiskAssessment) combine(other RiskAssessment) RiskAssessment {
	winner := ra
	loser := other
	// The higher-ranked Level wins; its Reason is the headline. Ties keep
	// ra as the winner (stable).
	if other.Level.Rank() > ra.Level.Rank() {
		winner = other
		loser = ra
	}

	merged := RiskAssessment{
		Level:                      winner.Level,
		IsHardBlock:                ra.IsHardBlock || other.IsHardBlock,
		RequiresIntentConfirmation: ra.RequiresIntentConfirmation || other.RequiresIntentConfirmation,
		Reason:                     winner.Reason,
		Sources:                    mergeRiskSources(winner.Sources, loser.Sources),
	}
	// A combined Critical Level always hard-blocks even if only one input
	// flagged it, keeping the invariant that Critical is unconditional.
	if merged.Level == configuration.RiskLevelCritical {
		merged.IsHardBlock = true
	}
	return merged
}

// mergeRiskSources concatenates two source lists, de-duplicating while
// preserving first-seen order so Explain() reads deterministically.
func mergeRiskSources(a, b []RiskSource) []RiskSource {
	seen := make(map[RiskSource]bool, len(a)+len(b))
	out := make([]RiskSource, 0, len(a)+len(b))
	for _, src := range append(append([]RiskSource{}, a...), b...) {
		if src == "" || seen[src] {
			continue
		}
		seen[src] = true
		out = append(out, src)
	}
	return out
}

// Explain renders a one-line human-readable summary of the assessment for
// diagnostics ("why was this gated?"). Sources are listed alphabetically for
// a stable rendering regardless of combination order.
func (ra RiskAssessment) Explain() string {
	srcs := make([]string, 0, len(ra.Sources))
	for _, s := range ra.Sources {
		srcs = append(srcs, string(s))
	}
	sort.Strings(srcs)

	level := string(ra.Level)
	if level == "" {
		level = "unknown"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "risk=%s", strings.ToUpper(level))
	if ra.IsHardBlock {
		b.WriteString(" (hard-block)")
	}
	if ra.RequiresIntentConfirmation {
		b.WriteString(" (intent-confirmation)")
	}
	if len(srcs) > 0 {
		fmt.Fprintf(&b, " source=%s", strings.Join(srcs, ","))
	}
	if strings.TrimSpace(ra.Reason) != "" {
		fmt.Fprintf(&b, " — %s", ra.Reason)
	}
	return b.String()
}
