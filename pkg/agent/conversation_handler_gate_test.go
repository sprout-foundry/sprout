package agent

import "testing"

func TestIsSelfReviewGatePersonaEnabled(t *testing.T) {
	tests := []struct {
		name     string
		persona  string
		expected bool
	}{
		{name: "orchestrator", persona: "orchestrator", expected: true},
		{name: "coder", persona: "coder", expected: true},
		{name: "case normalized", persona: " Coder ", expected: true},
		{name: "general disabled", persona: "general", expected: false},
		{name: "web scraper disabled", persona: "web_scraper", expected: false},
		{name: "empty disabled", persona: "", expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSelfReviewGatePersonaEnabled(tc.persona); got != tc.expected {
				t.Fatalf("isSelfReviewGatePersonaEnabled(%q) = %v, expected %v", tc.persona, got, tc.expected)
			}
		})
	}
}

func TestHasCodeLikeTrackedFiles(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected bool
	}{
		{name: "go file", files: []string{"pkg/main.go"}, expected: true},
		{name: "dockerfile", files: []string{"Dockerfile"}, expected: true},
		{name: "markdown only", files: []string{"README.md"}, expected: false},
		{name: "text only", files: []string{"notes.txt"}, expected: false},
		{name: "mixed includes code", files: []string{"docs/plan.md", "api/schema.yaml"}, expected: true},
		{name: "empty list", files: nil, expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCodeLikeTrackedFiles(tc.files); got != tc.expected {
				t.Fatalf("hasCodeLikeTrackedFiles(%v) = %v, expected %v", tc.files, got, tc.expected)
			}
		})
	}
}
