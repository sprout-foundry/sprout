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