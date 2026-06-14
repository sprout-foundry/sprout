// Risk assessment types for the unified risk vocabulary.
//
// This module defines RiskLevel, RiskSource, and RiskAssessment as the canonical
// representation of a risk decision. It is part of SP-068 Phase 1 (type
// definitions only) and is not yet wired into the classification pipeline.
// Phase 2 will integrate these types into the security classifier and persona
// risk cascade.
package tools

import (
	"fmt"
	"strings"
)

// RiskLevel indicates the severity of a risk assessment. Levels are ordered
// Low < Medium < High < Critical.
type RiskLevel string

const (
	// RiskLevelLow indicates minimal risk — operations that are read-only or
	// clearly confined to the workspace.
	RiskLevelLow RiskLevel = "Low"
	// RiskLevelMedium indicates moderate risk — operations that may write files
	// or affect state but are reversible.
	RiskLevelMedium RiskLevel = "Medium"
	// RiskLevelHigh indicates high risk — operations that may cause data loss
	// or require user confirmation.
	RiskLevelHigh RiskLevel = "High"
	// RiskLevelCritical indicates the highest risk — operations that are
	// hard-blocked or require explicit user authorization.
	RiskLevelCritical RiskLevel = "Critical"
)

// RiskSource identifies the component that contributed to a risk assessment.
type RiskSource string

const (
	// RiskSourceClassifier originates from the static security classifier
	// (shell command and file path heuristics).
	RiskSourceClassifier RiskSource = "classifier"
	// RiskSourcePersona originates from persona-specific risk overrides
	// (e.g., a cautious persona elevating the default level).
	RiskSourcePersona RiskSource = "persona"
	// RiskSourceGitGate originates from git operation risk analysis
	// (e.g., destructive reset, force push detection).
	RiskSourceGitGate RiskSource = "git-gate"
	// RiskSourceFsTier originates from filesystem tiering rules
	// (e.g., write operations targeting system directories).
	RiskSourceFsTier RiskSource = "fs-tier"
	// RiskSourceWorkspacePolicy originates from workspace-specific policies
	// (e.g., directory-level restrictions defined in project config).
	RiskSourceWorkspacePolicy RiskSource = "workspace-policy"
)

// RiskAssessment holds the result of evaluating a tool call or operation
// against the unified risk vocabulary. It aggregates risk from multiple
// sources into a single authoritative level.
type RiskAssessment struct {
	// Level is the final aggregated risk severity.
	Level RiskLevel `json:"level"`
	// Sources lists the components that contributed to this assessment.
	Sources []RiskSource `json:"sources"`
	// Reason is a human-readable explanation of the risk determination.
	Reason string `json:"reason"`
	// IsHardBlock indicates whether the operation must be denied outright
	// (no prompt or override allowed).
	IsHardBlock bool `json:"is_hard_block"`
}

// String returns a human-readable summary like "High [classifier, git-gate]: <reason>".
// When Sources is empty, the bracket portion is omitted: "High: <reason>".
func (r RiskAssessment) String() string {
	if len(r.Sources) == 0 {
		return fmt.Sprintf("%s: %s", r.Level, r.Reason)
	}
	srcs := make([]string, len(r.Sources))
	for i, s := range r.Sources {
		srcs[i] = string(s)
	}
	return fmt.Sprintf("%s [%s]: %s", r.Level, strings.Join(srcs, ", "), r.Reason)
}

// MostRestrictiveLevel returns the highest severity level from the inputs.
// The ordering is Low < Medium < High < Critical. If no levels are provided,
// it returns RiskLevelLow. If any input is an unknown/unrecognized level,
// it returns RiskLevelCritical — treating unknown inputs pessimistically
// since this is a security-critical decision.
func MostRestrictiveLevel(levels ...RiskLevel) RiskLevel {
	if len(levels) == 0 {
		return RiskLevelLow
	}

	rank := map[RiskLevel]int{
		RiskLevelLow:      0,
		RiskLevelMedium:   1,
		RiskLevelHigh:     2,
		RiskLevelCritical: 3,
	}

	// Validate all levels first — unknown values fail to Critical for safety.
	for _, lv := range levels {
		if _, ok := rank[lv]; !ok {
			return RiskLevelCritical
		}
	}

	best := levels[0]
	bestRank := rank[best]
	for _, lv := range levels[1:] {
		r := rank[lv]
		if r > bestRank {
			best = lv
			bestRank = r
		}
	}
	return best
}
