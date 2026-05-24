package envutil

import "testing"

func TestResolveColorPreference(t *testing.T) {
	tests := []struct {
		name       string
		noColor    string
		forceColor string
		want       bool
		expected   bool
	}{
		{"no env vars, want true", "", "", true, true},
		{"no env vars, want false", "", "", false, false},
		{"NO_COLOR=1, want true → false", "1", "", true, false},
		{"NO_COLOR=1, want false → false", "1", "", false, false},
		{"FORCE_COLOR=1, want false → true", "", "1", false, true},
		{"FORCE_COLOR=1, want true → true", "", "1", true, true},
		{"NO_COLOR beats FORCE_COLOR", "1", "1", true, false},
		{"empty env vars falls through", "", "", true, true},
		{"FORCE_COLOR=0 still enables (non-empty value)", "", "0", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("FORCE_COLOR", tt.forceColor)
			got := ResolveColorPreference(tt.want)
			if got != tt.expected {
				t.Errorf("ResolveColorPreference(%v) = %v, want %v", tt.want, got, tt.expected)
			}
		})
	}
}
