package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestLooksLikeProviderModelSpecifier(t *testing.T) {
	t.Parallel()
	mgr, err := configuration.NewManagerSilent()
	if err != nil {
		t.Skipf("skipping: %v", err)
	}

	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{name: "openai provider model", model: "openai:gpt-4o", expected: true},
		{name: "ollama provider model", model: "ollama:llama3", expected: true},
		{name: "no colon", model: "claude-sonnet-4", expected: false},
		{name: "empty string", model: "", expected: false},
		{name: "colon only", model: ":", expected: false},
		{name: "empty provider", model: ":claude", expected: false},
		{name: "empty model", model: "openai:", expected: false},
		{name: "unknown provider", model: "bogus:model", expected: false},
		{name: "just provider name", model: "openai", expected: false},
		{name: "multiple colons", model: "openai:sub:model", expected: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeProviderModelSpecifier(mgr, tc.model); got != tc.expected {
				t.Errorf("looksLikeProviderModelSpecifier(%q) = %v, expected %v", tc.model, got, tc.expected)
			}
		})
	}
}
