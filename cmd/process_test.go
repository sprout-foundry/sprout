package cmd

import (
	"strings"
	"testing"
)

// extractFirstJSON extracts the first JSON object from a string
func extractFirstJSON(input string) string {
	// Simple implementation - look for JSON patterns
	if strings.Contains(input, "{") && strings.Contains(input, "}") {
		// Find first { and last }
		start := strings.Index(input, "{")
		end := strings.LastIndex(input, "}") + 1
		if start >= 0 && end > start {
			return input[start:end]
		}
	}
	return ""
}

func TestExtractFirstJSON(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{`{"a":1}`, true},
		{"````json\n{\n \"a\": 1\n}\n```", true},
		{"no json here", false},
	}
	for _, c := range cases {
		out := extractFirstJSON(c.in)
		if c.ok && out == "" {
			t.Fatalf("expected json, got empty for %q", c.in)
		}
		if !c.ok && out != "" {
			t.Fatalf("expected empty, got %q", out)
		}
	}
}
