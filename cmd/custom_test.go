//go:build !js

package cmd

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestResolvePreferredCustomProviderModelAllowsNumericSelection(t *testing.T) {
	models := []configuration.ProviderDiscoveryModel{
		{ID: "qwen3.5-4b"},
		{ID: "qwen3.5-35-A3B"},
	}

	selected, err := resolvePreferredCustomProviderModel("2", models)
	if err != nil {
		t.Fatalf("expected numeric selection to succeed, got error: %v", err)
	}
	if selected != "qwen3.5-35-A3B" {
		t.Fatalf("expected second discovered model, got %q", selected)
	}
}

func TestResolvePreferredCustomProviderModelRejectsUnknownName(t *testing.T) {
	models := []configuration.ProviderDiscoveryModel{
		{ID: "qwen3.5-4b"},
	}

	if _, err := resolvePreferredCustomProviderModel("missing-model", models); err == nil {
		t.Fatal("expected unknown discovered model to fail validation")
	}
}

// TestRunCustomModelAdd_NonInteractiveGuard confirms the wizard fails
// fast with a non-interactive error when stdin isn't a terminal.
// `go test` pipes stdin, so this test naturally exercises the guard
// path without needing to fake a TTY.
func TestRunCustomModelAdd_NonInteractiveGuard(t *testing.T) {
	err := runCustomModelAdd()
	if err == nil {
		t.Fatal("expected non-interactive error from runCustomModelAdd, got nil")
	}
	if !strings.Contains(err.Error(), "non-interactive") &&
		!strings.Contains(err.Error(), "interactive terminal") {
		t.Errorf("error should mention interactive terminal requirement, got: %v", err)
	}
}
