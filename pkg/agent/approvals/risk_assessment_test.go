package approvals

import (
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ============================================================================
// Phase 1 Golden Tests — DO NOT MODIFY
// These lock the SAFE/CAUTION/DANGEROUS → Low/Medium/High mapping plus the
// hard-block → Critical escalation. Phase 2 must not change these.

func TestAssessmentFromClassifier_Mapping(t *testing.T) {
	cases := []struct {
		name       string
		in         tools.SecurityResult
		wantLevel  configuration.RiskLevel
		wantHard   bool
		wantIntent bool
		wantSource RiskSource
	}{
		{
			name:       "safe maps to low",
			in:         tools.SecurityResult{Risk: tools.SecuritySafe, Reasoning: "read-only"},
			wantLevel:  configuration.RiskLevelLow,
			wantSource: RiskSourceClassifier,
		},
		{
			name:       "caution maps to medium",
			in:         tools.SecurityResult{Risk: tools.SecurityCaution, ShouldPrompt: true, Reasoning: "rm single file"},
			wantLevel:  configuration.RiskLevelMedium,
			wantSource: RiskSourceClassifier,
		},
		{
			name:       "dangerous maps to high",
			in:         tools.SecurityResult{Risk: tools.SecurityDangerous, ShouldBlock: true, Reasoning: "rm -rf"},
			wantLevel:  configuration.RiskLevelHigh,
			wantSource: RiskSourceClassifier,
		},
		{
			name:       "hard block escalates to critical regardless of tier",
			in:         tools.SecurityResult{Risk: tools.SecurityDangerous, ShouldBlock: true, IsHardBlock: true, Reasoning: "rm -rf /"},
			wantLevel:  configuration.RiskLevelCritical,
			wantHard:   true,
			wantSource: RiskSourceCriticalOp,
		},
		{
			name:       "intent confirmation is carried orthogonally on a safe op",
			in:         tools.SecurityResult{Risk: tools.SecuritySafe, IntentConfirmation: true, Reasoning: "run_automate"},
			wantLevel:  configuration.RiskLevelLow,
			wantIntent: true,
			wantSource: RiskSourceClassifier,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AssessmentFromClassifier(tc.in)
			if got.Level != tc.wantLevel {
				t.Errorf("Level = %q, want %q", got.Level, tc.wantLevel)
			}
			if got.IsHardBlock != tc.wantHard {
				t.Errorf("IsHardBlock = %v, want %v", got.IsHardBlock, tc.wantHard)
			}
			if got.RequiresIntentConfirmation != tc.wantIntent {
				t.Errorf("RequiresIntentConfirmation = %v, want %v", got.RequiresIntentConfirmation, tc.wantIntent)
			}
			if len(got.Sources) != 1 || got.Sources[0] != tc.wantSource {
				t.Errorf("Sources = %v, want [%q]", got.Sources, tc.wantSource)
			}
		})
	}
}

func TestCombine_MostRestrictiveWins(t *testing.T) {
	low := AssessmentFromClassifier(tools.SecurityResult{Risk: tools.SecuritySafe, Reasoning: "classifier says safe"})
	high := AssessmentFromPersonaCascade(configuration.RiskLevelHigh, "persona gates this")

	got := low.Combine(high)
	if got.Level != configuration.RiskLevelHigh {
		t.Fatalf("combined Level = %q, want High", got.Level)
	}
	if got.Reason != "persona gates this" {
		t.Errorf("headline Reason = %q, want the higher-risk side's reason", got.Reason)
	}
	if len(got.Sources) != 2 {
		t.Errorf("Sources = %v, want both contributors merged", got.Sources)
	}

	// Commutative on Level: order of combination must not change the verdict.
	if rev := high.Combine(low); rev.Level != got.Level {
		t.Errorf("combine is not order-stable on Level: %q vs %q", rev.Level, got.Level)
	}

	// A critical input forces hard-block on the merged result even if the
	// other side never set it.
	crit := AssessmentFromPersonaCascade(configuration.RiskLevelCritical, "rm -rf /")
	merged := low.Combine(crit)
	if merged.Level != configuration.RiskLevelCritical || !merged.IsHardBlock {
		t.Errorf("critical combine: Level=%q hard=%v, want Critical+hard-block", merged.Level, merged.IsHardBlock)
	}

	// Intent-confirmation survives a fold with a higher-risk, non-intent op.
	intent := AssessmentFromClassifier(tools.SecurityResult{Risk: tools.SecuritySafe, IntentConfirmation: true, Reasoning: "workflow"})
	if !intent.Combine(high).RequiresIntentConfirmation {
		t.Errorf("intent-confirmation should survive combination")
	}
}

func TestExplain_StableAndInformative(t *testing.T) {
	a := AssessmentFromClassifier(tools.SecurityResult{Risk: tools.SecurityDangerous, IsHardBlock: true, Reasoning: "rm -rf /"})
	got := a.Explain()
	for _, want := range []string{"CRITICAL", "hard-block", "critical-op", "rm -rf /"} {
		if !strings.Contains(got, want) {
			t.Errorf("Explain() = %q, missing %q", got, want)
		}
	}
}

// ============================================================================
// Phase 2 Tests — SP-068-2a: Unified Risk Resolver
// ============================================================================

// ---------------------------------------------------------------------------
// Config flag default behavior
// ---------------------------------------------------------------------------

func TestCombine_SourceDeduplication(t *testing.T) {
	a := RiskAssessment{
		Level:   configuration.RiskLevelHigh,
		Sources: []RiskSource{RiskSourceClassifier, RiskSourcePersonaCascade},
		Reason:  "source a",
	}
	b := RiskAssessment{
		Level:   configuration.RiskLevelMedium,
		Sources: []RiskSource{RiskSourceClassifier, RiskSourceFSTier},
		Reason:  "source b",
	}

	got := a.Combine(b)

	// RiskSourceClassifier appears in both — should be de-duplicated
	count := 0
	for _, src := range got.Sources {
		if src == RiskSourceClassifier {
			count++
		}
	}
	if count != 1 {
		t.Errorf("classifier source appears %d times, want 1 (should be de-duplicated)", count)
	}

	// Total unique sources: classifier, persona-cascade, fs-tier
	if len(got.Sources) != 3 {
		t.Errorf("should have 3 unique sources, got %d: %v", len(got.Sources), got.Sources)
	}
}

func TestCombine_HardBlockSurvives(t *testing.T) {
	a := RiskAssessment{
		Level:       configuration.RiskLevelHigh,
		IsHardBlock: true,
		Sources:     []RiskSource{RiskSourceClassifier},
	}
	b := RiskAssessment{
		Level:       configuration.RiskLevelMedium,
		IsHardBlock: false,
		Sources:     []RiskSource{RiskSourcePersonaCascade},
	}

	got := a.Combine(b)
	if !got.IsHardBlock {
		t.Error("IsHardBlock should survive combination via OR")
	}
}

func TestCombine_IntentionalOrderStability(t *testing.T) {
	// When both sides have the same Level, the first (ra) should win
	// as the headline reason.
	a := RiskAssessment{
		Level:   configuration.RiskLevelHigh,
		Sources: []RiskSource{RiskSourceClassifier},
		Reason:  "from a",
	}
	b := RiskAssessment{
		Level:   configuration.RiskLevelHigh,
		Sources: []RiskSource{RiskSourcePersonaCascade},
		Reason:  "from b",
	}

	got := a.Combine(b)
	if got.Reason != "from a" {
		t.Errorf("headline reason = %q, want 'from a' (first side wins ties)", got.Reason)
	}
}

// ---------------------------------------------------------------------------
// ResolveOldDecision — mapping tests
// ---------------------------------------------------------------------------

func TestResolveOldDecision_Mapping(t *testing.T) {
	cases := []struct {
		name string
		in   tools.SecurityResult
		want string
	}{
		{
			name: "shouldBlock maps to block",
			in:   tools.SecurityResult{ShouldBlock: true, Risk: tools.SecurityDangerous},
			want: "block",
		},
		{
			name: "shouldPrompt maps to prompt",
			in:   tools.SecurityResult{ShouldPrompt: true, Risk: tools.SecurityCaution},
			want: "prompt",
		},
		{
			name: "neither maps to allow",
			in:   tools.SecurityResult{Risk: tools.SecuritySafe},
			want: "allow",
		},
		{
			name: "shouldBlock takes precedence over shouldPrompt",
			in:   tools.SecurityResult{ShouldBlock: true, ShouldPrompt: true},
			want: "block",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveOldDecision(tc.in)
			if got != tc.want {
				t.Errorf("ResolveOldDecision() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ResolveUnifiedDecision — mapping tests
// ---------------------------------------------------------------------------

func TestResolveUnifiedDecision_Mapping(t *testing.T) {
	cases := []struct {
		name string
		in   RiskAssessment
		want string
	}{
		{
			name: "critical maps to block",
			in:   RiskAssessment{Level: configuration.RiskLevelCritical},
			want: "block",
		},
		{
			name: "hard-block maps to block regardless of level",
			in:   RiskAssessment{Level: configuration.RiskLevelHigh, IsHardBlock: true},
			want: "block",
		},
		{
			name: "high maps to prompt",
			in:   RiskAssessment{Level: configuration.RiskLevelHigh},
			want: "prompt",
		},
		{
			name: "medium maps to prompt",
			in:   RiskAssessment{Level: configuration.RiskLevelMedium},
			want: "prompt",
		},
		{
			name: "low maps to allow",
			in:   RiskAssessment{Level: configuration.RiskLevelLow},
			want: "allow",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveUnifiedDecision(tc.in)
			if got != tc.want {
				t.Errorf("ResolveUnifiedDecision() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Shadow-mode parity — common commands produce matching decisions
// ---------------------------------------------------------------------------

func TestMergeRiskSources_Deduplication(t *testing.T) {
	a := []RiskSource{RiskSourceClassifier, RiskSourcePersonaCascade}
	b := []RiskSource{RiskSourceClassifier, RiskSourceFSTier}

	got := MergeRiskSources(a, b)

	count := 0
	for _, src := range got {
		if src == RiskSourceClassifier {
			count++
		}
	}
	if count != 1 {
		t.Errorf("classifier appears %d times, want 1", count)
	}
	if len(got) != 3 {
		t.Errorf("got %d sources, want 3", len(got))
	}
}

func TestMergeRiskSources_EmptySlices(t *testing.T) {
	got := MergeRiskSources(nil, nil)
	if len(got) != 0 {
		t.Errorf("two nil slices should produce empty result, got %d", len(got))
	}

	a := []RiskSource{RiskSourceClassifier}
	got2 := MergeRiskSources(a, nil)
	if len(got2) != 1 || got2[0] != RiskSourceClassifier {
		t.Errorf("non-nil + nil = %v, want [classifier]", got2)
	}

	got3 := MergeRiskSources(nil, a)
	if len(got3) != 1 || got3[0] != RiskSourceClassifier {
		t.Errorf("nil + non-nil = %v, want [classifier]", got3)
	}
}

func TestMergeRiskSources_FilterEmptySources(t *testing.T) {
	a := []RiskSource{RiskSourceClassifier, "", RiskSourcePersonaCascade}
	b := []RiskSource{RiskSourceFSTier}

	got := MergeRiskSources(a, b)
	for _, src := range got {
		if src == "" {
			t.Error("empty source should be filtered out")
		}
	}
	if len(got) != 3 {
		t.Errorf("got %d sources, want 3 (empty filtered)", len(got))
	}
}

// ---------------------------------------------------------------------------
// AssessmentFromPersonaCascade — edge case: critical maps to hard-block
// ---------------------------------------------------------------------------

func TestAssessmentFromPersonaCascade_CriticalIsHardBlock(t *testing.T) {
	a := AssessmentFromPersonaCascade(configuration.RiskLevelCritical, "critical from cascade")
	if !a.IsHardBlock {
		t.Error("Critical from persona cascade should set IsHardBlock")
	}
	if a.Level != configuration.RiskLevelCritical {
		t.Errorf("Level = %q, want Critical", a.Level)
	}
}

func TestAssessmentFromPersonaCascade_LowIsNotHardBlock(t *testing.T) {
	a := AssessmentFromPersonaCascade(configuration.RiskLevelLow, "low risk")
	if a.IsHardBlock {
		t.Error("Low from persona cascade should NOT set IsHardBlock")
	}
}

// ---------------------------------------------------------------------------
// Explain — additional scenarios
// ---------------------------------------------------------------------------

func TestExplain_MultipleSources(t *testing.T) {
	a := RiskAssessment{
		Level:   configuration.RiskLevelHigh,
		Sources: []RiskSource{RiskSourceClassifier, RiskSourcePersonaCascade, RiskSourceFSTier},
		Reason:  "multiple checks contributed",
	}
	got := a.Explain()

	// Sources are sorted alphabetically: classifier, fs-tier, persona-cascade
	if !strings.Contains(got, "classifier") {
		t.Error("Explain should contain 'classifier'")
	}
	if !strings.Contains(got, "fs-tier") {
		t.Error("Explain should contain 'fs-tier'")
	}
	if !strings.Contains(got, "persona-cascade") {
		t.Error("Explain should contain 'persona-cascade'")
	}
}

func TestExplain_IntentConfirmationOnly(t *testing.T) {
	a := RiskAssessment{
		Level:                      configuration.RiskLevelLow,
		RequiresIntentConfirmation: true,
		Sources:                    []RiskSource{RiskSourceClassifier},
		Reason:                     "workflow launch",
	}
	got := a.Explain()
	if !strings.Contains(got, "intent-confirmation") {
		t.Errorf("Explain should contain 'intent-confirmation': %q", got)
	}
}

// ---------------------------------------------------------------------------
// ResolveToolRisk — edit_file tool triggers fs-tier
// ---------------------------------------------------------------------------
