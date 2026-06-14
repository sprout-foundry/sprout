package tools

import (
	"strings"
	"testing"
)

// --- RiskLevel constants ---

func TestRiskLevelConstants(t *testing.T) {
	tests := []struct {
		name string
		got  RiskLevel
		want string
	}{
		{"Low", RiskLevelLow, "Low"},
		{"Medium", RiskLevelMedium, "Medium"},
		{"High", RiskLevelHigh, "High"},
		{"Critical", RiskLevelCritical, "Critical"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.got) != tt.want {
				t.Fatalf("RiskLevel %s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// --- RiskSource constants ---

func TestRiskSourceConstants(t *testing.T) {
	tests := []struct {
		name string
		got  RiskSource
		want string
	}{
		{"Classifier", RiskSourceClassifier, "classifier"},
		{"Persona", RiskSourcePersona, "persona"},
		{"GitGate", RiskSourceGitGate, "git-gate"},
		{"FsTier", RiskSourceFsTier, "fs-tier"},
		{"WorkspacePolicy", RiskSourceWorkspacePolicy, "workspace-policy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.got) != tt.want {
				t.Fatalf("RiskSource %s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// --- RiskAssessment.String() ---

func TestRiskAssessmentString(t *testing.T) {
	tests := []struct {
		name    string
		ra      RiskAssessment
		wants   []string // substrings that must all appear in the output
	}{
		{
			name: "single source",
			ra: RiskAssessment{
				Level:   RiskLevelHigh,
				Sources: []RiskSource{RiskSourceClassifier},
				Reason:  "test",
			},
			wants: []string{"High", "classifier", "test"},
		},
		{
			name: "multiple sources",
			ra: RiskAssessment{
				Level:   RiskLevelCritical,
				Sources: []RiskSource{RiskSourceClassifier, RiskSourceGitGate},
				Reason:  "destructive",
			},
			wants: []string{"Critical", "classifier, git-gate", "destructive"},
		},
		{
			name: "empty sources",
			ra: RiskAssessment{
				Level:   RiskLevelLow,
				Sources: []RiskSource{},
				Reason:  "safe",
			},
			wants: []string{"Low", "safe"},
		},
		{
			name: "empty reason",
			ra: RiskAssessment{
				Level:   RiskLevelMedium,
				Sources: []RiskSource{RiskSourcePersona},
				Reason:  "",
			},
			wants: []string{"Medium", "persona"},
		},
		{
			name: "nil sources",
			ra: RiskAssessment{
				Level:   RiskLevelHigh,
				Sources: nil,
				Reason:  "reason",
			},
			wants: []string{"High", "reason"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ra.String()
			for _, want := range tt.wants {
				if !strings.Contains(got, want) {
					t.Fatalf("String() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

// --- MostRestrictiveLevel ---

func TestMostRestrictiveLevel(t *testing.T) {
	tests := []struct {
		name  string
		levels []RiskLevel
		want  RiskLevel
	}{
		{
			name:  "single level",
			levels: []RiskLevel{RiskLevelHigh},
			want:  RiskLevelHigh,
		},
		{
			name:  "two levels low and high",
			levels: []RiskLevel{RiskLevelLow, RiskLevelHigh},
			want:  RiskLevelHigh,
		},
		{
			name:  "multiple levels",
			levels: []RiskLevel{RiskLevelLow, RiskLevelMedium, RiskLevelCritical},
			want:  RiskLevelCritical,
		},
		{
			name:  "same levels",
			levels: []RiskLevel{RiskLevelMedium, RiskLevelMedium},
			want:  RiskLevelMedium,
		},
		{
			name:  "zero arguments",
			levels: nil,
			want:  RiskLevelLow,
		},
		{
			name:  "adjacent levels medium and high",
			levels: []RiskLevel{RiskLevelMedium, RiskLevelHigh},
			want:  RiskLevelHigh,
		},
		{
			name:  "unknown level alone returns critical",
			levels: []RiskLevel{RiskLevel("Unknown")},
			want:  RiskLevelCritical,
		},
		{
			name:  "empty string level mixed with known returns critical",
			levels: []RiskLevel{RiskLevelHigh, RiskLevel("")},
			want:  RiskLevelCritical,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MostRestrictiveLevel(tt.levels...)
			if got != tt.want {
				t.Fatalf("MostRestrictiveLevel(%v) = %q, want %q", tt.levels, got, tt.want)
			}
		})
	}
}


