package cmd

import (
	"testing"
)

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
