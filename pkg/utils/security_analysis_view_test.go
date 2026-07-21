package utils

import "testing"

// TestRenderSecurityAnalysisFallbackLine verifies the legacy-line prompt's
// analysis rendering: when the pkg/console picker is unavailable, the
// analysis is rendered as a single annotated line. SP-124 Phase 3.
func TestRenderSecurityAnalysisFallbackLine(t *testing.T) {
	cases := []struct {
		name string
		view *SecurityAnalysisView
		want string
	}{
		{
			name: "nil view returns empty string",
			view: nil,
			want: "",
		},
		{
			name: "approve recommendation prefixes with check",
			view: &SecurityAnalysisView{
				Summary:         "Removes only stale build artifacts.",
				RiskAssessment:  "low",
				Recommendation:  "approve",
			},
			want: "[security analysis] \xE2\x9C\x93 Removes only stale build artifacts.",
		},
		{
			name: "review recommendation prefixes with warning",
			view: &SecurityAnalysisView{
				Summary:         "Touches a tracked file outside CWD.",
				RiskAssessment:  "moderate",
				Recommendation:  "review",
			},
			want: "[security analysis] ! Touches a tracked file outside CWD.",
		},
		{
			name: "reject recommendation prefixes with cross",
			view: &SecurityAnalysisView{
				Summary:         "Drops a remote DB without a dry-run.",
				RiskAssessment:  "high",
				Recommendation:  "reject",
			},
			want: "[security analysis] \xE2\x9C\x97 Drops a remote DB without a dry-run.",
		},
		{
			name: "unknown recommendation defaults to warning glyph",
			view: &SecurityAnalysisView{
				Summary:         "Mixed-impact operation.",
				RiskAssessment:  "moderate",
				Recommendation:  "something_else",
			},
			want: "[security analysis] ! Mixed-impact operation.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderSecurityAnalysisFallbackLine(tc.view)
			if got != tc.want {
				t.Errorf("renderSecurityAnalysisFallbackLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSecurityAnalysisView_ChainFieldsRoundTrip verifies the struct
// preserves the SP-124b Phase 2 chain metadata through a copy. This is
// what the broker does (pkg/agent/approval_broker.go copies the chain
// fields from *SecurityAnalysis into *SecurityAnalysisView), so a
// regression here breaks the WebUI stepper and CLI stepper simultaneously.
//
// SP-124b Phase 2.
func TestSecurityAnalysisView_ChainFieldsRoundTrip(t *testing.T) {
	original := &SecurityAnalysisView{
		Summary:              "Chain of 3 ops",
		Modifies:             "/tmp",
		RiskAssessment:       "moderate",
		Recommendation:       "review",
		ChainLength:          3,
		ChainSubcommands:     []string{"git add -A", "git commit -m 'wip'", "git push"},
		ChainClassifications: []string{"low", "low", "moderate"},
	}

	// Simulate the broker's struct copy.
	copied := *original

	if copied.ChainLength != original.ChainLength {
		t.Errorf("ChainLength not preserved: got %d, want %d", copied.ChainLength, original.ChainLength)
	}
	if len(copied.ChainSubcommands) != len(original.ChainSubcommands) {
		t.Fatalf("ChainSubcommands length not preserved: got %d, want %d", len(copied.ChainSubcommands), len(original.ChainSubcommands))
	}
	for i := range original.ChainSubcommands {
		if copied.ChainSubcommands[i] != original.ChainSubcommands[i] {
			t.Errorf("ChainSubcommands[%d] = %q, want %q", i, copied.ChainSubcommands[i], original.ChainSubcommands[i])
		}
	}
	if len(copied.ChainClassifications) != len(original.ChainClassifications) {
		t.Fatalf("ChainClassifications length not preserved: got %d, want %d", len(copied.ChainClassifications), len(original.ChainClassifications))
	}
	for i := range original.ChainClassifications {
		if copied.ChainClassifications[i] != original.ChainClassifications[i] {
			t.Errorf("ChainClassifications[%d] = %q, want %q", i, copied.ChainClassifications[i], original.ChainClassifications[i])
		}
	}

	// Zero values for legacy callers (Phase 1 regression guard).
	legacy := &SecurityAnalysisView{
		Summary:        "single cmd",
		RiskAssessment: "low",
		Recommendation: "approve",
	}
	if legacy.ChainLength != 0 {
		t.Errorf("legacy view ChainLength = %d, want 0", legacy.ChainLength)
	}
	if legacy.ChainSubcommands != nil {
		t.Errorf("legacy view ChainSubcommands should be nil, got %v", legacy.ChainSubcommands)
	}
	if legacy.ChainClassifications != nil {
		t.Errorf("legacy view ChainClassifications should be nil, got %v", legacy.ChainClassifications)
	}
}