package agent

import (
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestAssessmentFromClassifier_Mapping is the golden test locking the
// SAFE/CAUTION/DANGEROUS → Low/Medium/High mapping plus the hard-block →
// Critical escalation. Phase 2 must not change these.
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
			got := assessmentFromClassifier(tc.in)
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

// TestRiskLevelRank locks the severity ordering the combinator relies on.
func TestRiskLevelRank(t *testing.T) {
	order := []configuration.RiskLevel{
		configuration.RiskLevelLow,
		configuration.RiskLevelMedium,
		configuration.RiskLevelHigh,
		configuration.RiskLevelCritical,
	}
	for i := 1; i < len(order); i++ {
		if !(order[i].Rank() > order[i-1].Rank()) {
			t.Errorf("%q (rank %d) should outrank %q (rank %d)", order[i], order[i].Rank(), order[i-1], order[i-1].Rank())
		}
	}
	// Unknown ranks as Medium, never below Low.
	if configuration.RiskLevel("bogus").Rank() != configuration.RiskLevelMedium.Rank() {
		t.Errorf("unknown level should rank as Medium")
	}
	if !configuration.RiskLevelCritical.IsAtLeast(configuration.RiskLevelHigh) {
		t.Errorf("Critical should be at least High")
	}
	if configuration.RiskLevelLow.IsAtLeast(configuration.RiskLevelMedium) {
		t.Errorf("Low should not be at least Medium")
	}
}

// TestCombine_MostRestrictiveWins is the core resolver invariant: folding
// two assessments keeps the harder Level, ORs the flags, merges sources,
// and never lets a Critical lose its hard-block.
func TestCombine_MostRestrictiveWins(t *testing.T) {
	low := assessmentFromClassifier(tools.SecurityResult{Risk: tools.SecuritySafe, Reasoning: "classifier says safe"})
	high := assessmentFromPersonaCascade(configuration.RiskLevelHigh, "persona gates this")

	got := low.combine(high)
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
	if rev := high.combine(low); rev.Level != got.Level {
		t.Errorf("combine is not order-stable on Level: %q vs %q", rev.Level, got.Level)
	}

	// A critical input forces hard-block on the merged result even if the
	// other side never set it.
	crit := assessmentFromPersonaCascade(configuration.RiskLevelCritical, "rm -rf /")
	merged := low.combine(crit)
	if merged.Level != configuration.RiskLevelCritical || !merged.IsHardBlock {
		t.Errorf("critical combine: Level=%q hard=%v, want Critical+hard-block", merged.Level, merged.IsHardBlock)
	}

	// Intent-confirmation survives a fold with a higher-risk, non-intent op.
	intent := assessmentFromClassifier(tools.SecurityResult{Risk: tools.SecuritySafe, IntentConfirmation: true, Reasoning: "workflow"})
	if !intent.combine(high).RequiresIntentConfirmation {
		t.Errorf("intent-confirmation should survive combination")
	}
}

func TestExplain_StableAndInformative(t *testing.T) {
	a := assessmentFromClassifier(tools.SecurityResult{Risk: tools.SecurityDangerous, IsHardBlock: true, Reasoning: "rm -rf /"})
	got := a.Explain()
	for _, want := range []string{"CRITICAL", "hard-block", "critical-op", "rm -rf /"} {
		if !strings.Contains(got, want) {
			t.Errorf("Explain() = %q, missing %q", got, want)
		}
	}
}
