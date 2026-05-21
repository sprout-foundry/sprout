package secretdetect

import "testing"

func TestDisplayName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"openai-api-key", "OpenAI API Key"},
		{"jwt", "JWT"},
		{"github-pat", "GitHub Personal Access Token"},
		{"generic-api-key", "API Key"},
		{"private-key", "Private Key"},
		// Unknown rule, hits fallback with abbreviation expansion.
		{"custom-aws-api-token", "Custom AWS API Token"},
		// Unknown rule, plain title-case fallback.
		{"foobar-token", "Foobar Token"},
		// Empty / degenerate input.
		{"", "Secret"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := DisplayName(c.in)
			if got != c.want {
				t.Errorf("DisplayName(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
