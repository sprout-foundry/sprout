//go:build !js

package cmd

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestProviderModelSpec(t *testing.T) {
	_, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	if got := providerModelSpec("openai", "gpt-x"); got != "openai:gpt-x" {
		t.Errorf("provider+model = %q, want openai:gpt-x", got)
	}
	if got := providerModelSpec("", "gpt-x"); got != "gpt-x" {
		t.Errorf("model only = %q, want gpt-x", got)
	}
	if got := providerModelSpec("", ""); got != "" {
		t.Errorf("neither = %q, want empty", got)
	}
	// A bare provider must not reach the constructors as a model name.
	got := providerModelSpec("openai", "")
	if !strings.HasPrefix(got, "openai:") || got == "openai:" {
		t.Errorf("provider only = %q, want openai:<default model>", got)
	}
}
